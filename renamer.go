package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The Plex names tab: a file renamer. It lists the queue's waiting files and
// files added on the tab itself (Add files… / Add folder…), each with a
// Plex-style name from its TMDB match, and can
//   - rename the original file straight away ("Rename now"), with the subtitle
//     files beside it, and undo the last batch; or
//   - give a queued file's converted file that name (what the queue's Review
//     names does).
// For Rename now the name describes the file as it is (a 4K H.264 file says
// "2160p.H264"); the converted file's name describes the new file. Folders are
// never renamed or created (the owner's choice).

type renRow struct {
	ID        int      `json:"id"`
	Path      string   `json:"path"`
	Name      string   `json:"name"`
	QueueID   int      `json:"queueId,omitempty"` // 0: added on the tab
	Kind      string   `json:"kind"`              // movie | tv | "" (from the file name)
	Details   string   `json:"details"`           // the file's own details ("1080p.H264")
	Proposed  string   `json:"proposed"`          // the new name, without the extension ("" = none yet)
	Own       bool     `json:"own,omitempty"`     // typed on the tab
	Converted string   `json:"converted,omitempty"`
	Note      string   `json:"note,omitempty"` // what the TMDB lookup said
	State     string   `json:"state"`          // checking looking new needs none ready warn clash same busy error
	Why       string   `json:"why,omitempty"`
	Plex      []string `json:"plex,omitempty"` // the Plex check of the file as it is

	plexName string // from TMDB, with plexDetailsMark
	edited   string // the name typed on the tab
	probed   bool
	looking  bool
}

type renMove struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type renState struct {
	mu   sync.Mutex
	rows []*renRow
	next int
}

// subtitle (and other) files that belong to a video by name: "Film.en.srt"
var sidecarExts = map[string]bool{".srt": true, ".ass": true, ".ssa": true, ".sub": true, ".idx": true, ".sup": true,
	".vtt": true, ".smi": true}

// ---------- rows ----------

func (g *guiServer) renAddPaths(paths []string) map[string]any {
	g.ren.mu.Lock()
	have := map[string]bool{}
	for _, r := range g.ren.rows {
		have[strings.ToLower(r.Path)] = true
	}
	var added []*renRow
	for _, p := range paths {
		if p == "" || have[strings.ToLower(p)] || !videoExts[strings.ToLower(filepath.Ext(p))] {
			continue
		}
		have[strings.ToLower(p)] = true
		g.ren.next++
		r := &renRow{ID: g.ren.next, Path: p, Name: filepath.Base(p), Kind: guessSearch(p, "").Kind}
		g.ren.rows = append(g.ren.rows, r)
		added = append(added, r)
	}
	g.ren.mu.Unlock()
	if len(added) > 0 {
		go g.renProbe(added)
		if g.tmdbKey() != "" {
			var ids []int
			for _, r := range added {
				ids = append(ids, r.ID)
			}
			g.renLookup(ids, nil)
		}
	}
	return map[string]any{"ok": true, "added": len(added)}
}

// renSync brings the queue's waiting files into the list (and takes out the
// ones that left the queue). Called with neither lock held.
func (g *guiServer) renSync() {
	type qf struct {
		id                         int
		input, plexName, note, out string
		details                    string
	}
	var qs []qf
	g.mu.Lock()
	for _, it := range g.q.items {
		if it.Editing || !(it.pending() || it.Status == stProblem) {
			continue
		}
		var m struct {
			PlexName string `json:"plexName"`
			Note     string `json:"tmdbNote"`
		}
		_ = json.Unmarshal(it.Raw, &m)
		qs = append(qs, qf{it.ID, it.Input, m.PlexName, m.Note, it.Output, it.PlexDetails})
	}
	g.mu.Unlock()
	g.ren.mu.Lock()
	byQ := map[int]*renRow{}
	var keep []*renRow
	for _, r := range g.ren.rows {
		if r.QueueID == 0 {
			keep = append(keep, r)
		} else {
			byQ[r.QueueID] = r
		}
	}
	var queued, fresh []*renRow
	for _, q := range qs {
		r := byQ[q.id]
		if r == nil || !strings.EqualFold(r.Path, q.input) {
			g.ren.next++
			r = &renRow{ID: g.ren.next, QueueID: q.id}
			fresh = append(fresh, r)
		}
		r.Path, r.Name, r.Kind = q.input, filepath.Base(q.input), guessSearch(q.input, "").Kind
		r.plexName, r.Note = q.plexName, q.note
		r.looking = strings.Contains(q.note, "looking up")
		r.Converted = ""
		if q.plexName != "" && q.out != "" && (!strings.Contains(q.plexName, plexDetailsMark) || q.details != "") {
			r.Converted = plexFinal(q.plexName, q.details) + filepath.Ext(q.out)
		}
		queued = append(queued, r)
	}
	g.ren.rows = append(queued, keep...)
	g.ren.mu.Unlock()
	if len(fresh) > 0 {
		go g.renProbe(fresh)
	}
}

// renProbe reads what each file is: its size, video format and HDR (for the
// name) and the Plex check.
func (g *guiServer) renProbe(rows []*renRow) {
	ffprobe, err := findTool("ffprobe", "")
	for _, r := range rows {
		g.ren.mu.Lock()
		path := r.Path
		g.ren.mu.Unlock()
		details, plex := "", []string(nil)
		if err == nil {
			details, plex = probeForName(ffprobe, path)
		}
		g.ren.mu.Lock()
		if r.Path == path {
			r.Details, r.Plex, r.probed = details, plex, true
		}
		g.ren.mu.Unlock()
	}
}

// probeForName: "2160p.HEVC.DV" for the file as it is, and its Plex check.
func probeForName(ffprobe, path string) (string, []string) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	data, err := toolCommand(ctx, ffprobe, "-v", "error", "-show_streams", "-show_format", "-of", "json", path).Output()
	if err != nil {
		return "", nil
	}
	var p scanProbe
	if json.Unmarshal(data, &p) != nil {
		return "", nil
	}
	var w, h int
	codec, hdr := "", ""
	for _, s := range p.Streams {
		if s.Type != "video" || s.Disp["attached_pic"] == 1 || codec != "" {
			continue
		}
		w, h, codec = s.Width, s.Height, nameCodec(s.Codec)
		switch s.Transfer {
		case "smpte2084":
			hdr = "HDR10"
		case "arib-std-b67":
			hdr = "HLG"
		}
		for _, sd := range s.SideData {
			if t, _ := sd["side_data_type"].(string); strings.Contains(strings.ToLower(t), "dovi") {
				hdr = "DV"
			}
		}
	}
	if hdr == "HDR10" && hasHDR10Plus(ctx, ffprobe, path) {
		hdr = "HDR10+"
	}
	total, _ := strconv.ParseFloat(p.Format.BitRate, 64)
	var plex []string
	for _, x := range plexIssues(plexSrcFromScan(&p, int(total/1000))) {
		plex = append(plex, x.Short)
	}
	if codec == "" {
		return "", plex
	}
	return plexDetailsFor(w, h, codec, hdr), plex
}

// nameCodec is the video format as release names write it.
func nameCodec(c string) string {
	if n, ok := map[string]string{"hevc": "HEVC", "h264": "H264", "av1": "AV1", "vp9": "VP9", "mpeg2video": "MPEG2",
		"vc1": "VC1", "mpeg4": "MPEG4"}[c]; ok {
		return n
	}
	return strings.ToUpper(c)
}

// hasHDR10Plus looks at the first frame for HDR10+ data (it is not in the
// stream's description).
func hasHDR10Plus(ctx context.Context, ffprobe, path string) bool {
	out, err := toolCommand(ctx, ffprobe, "-v", "error", "-select_streams", "v:0", "-read_intervals", "%+#1",
		"-show_frames", "-show_entries", "frame=side_data_list", "-of", "json", path).Output()
	return err == nil && strings.Contains(string(out), "SMPTE2094-40")
}

// ---------- the names and their checks ----------

var (
	reSxxEyy    = regexp.MustCompile(`(?i)\bS\d{1,2}E\d{1,3}`)
	reYearParen = regexp.MustCompile(`\((19|20)\d\d\)`)
)

// renCompute fills in each row's proposed name and state. Holds g.ren.mu.
func (g *guiServer) renComputeLocked() {
	taken := map[string]int{}
	for _, r := range g.ren.rows {
		r.Proposed, r.Own = "", r.edited != ""
		switch {
		case r.edited != "":
			r.Proposed = r.edited
		case r.plexName != "" && r.probed && (r.Details != "" || !strings.Contains(r.plexName, plexDetailsMark)):
			r.Proposed = plexFinal(r.plexName, r.Details)
		}
		if r.Proposed != "" {
			taken[strings.ToLower(filepath.Join(filepath.Dir(r.Path), r.Proposed+filepath.Ext(r.Path)))]++
		}
	}
	for _, r := range g.ren.rows {
		r.State, r.Why = "", ""
		to := filepath.Join(filepath.Dir(r.Path), r.Proposed+filepath.Ext(r.Path))
		switch {
		case !r.probed:
			r.State = "checking"
		case r.looking:
			r.State = "looking"
		case r.Proposed == "":
			switch {
			case strings.HasPrefix(r.Note, "TMDB: needs a match"):
				r.State, r.Why = "needs", "several possible matches"
			case strings.HasPrefix(r.Note, "TMDB: nothing found"):
				r.State, r.Why = "none", "nothing found on TMDB"
			case strings.HasPrefix(r.Note, "TMDB:") && r.Note != "":
				r.State, r.Why = "error", strings.TrimPrefix(r.Note, "TMDB: ")
			default:
				r.State, r.Why = "new", "not looked up yet"
			}
		case to == r.Path:
			r.State, r.Why = "same", "already named so"
		case !strings.EqualFold(to, r.Path) && fileExists(to):
			r.State, r.Why = "clash", "a file of that name already exists"
		case taken[strings.ToLower(to)] > 1:
			r.State, r.Why = "clash", "another file here would get the same name"
		case plexClean(r.Proposed) != r.Proposed:
			r.State, r.Why = "clash", `a file name cannot hold \ / : * ? " < > |`
		case r.Kind == "tv" && !reSxxEyy.MatchString(r.Proposed):
			r.State, r.Why = "warn", "Plex may not recognise this: there is no S01E02 in the name"
		case r.Kind == "movie" && !reYearParen.MatchString(r.Proposed):
			r.State, r.Why = "warn", "Plex may pick the wrong film: there is no (year) in the name"
		default:
			r.State = "ready"
		}
	}
}

func (g *guiServer) renList() map[string]any {
	g.renSync()
	g.ren.mu.Lock()
	g.renComputeLocked()
	rows := make([]renRow, 0, len(g.ren.rows))
	for _, r := range g.ren.rows {
		rows = append(rows, *r)
	}
	g.ren.mu.Unlock()
	undo := g.renUndoList()
	return map[string]any{"ok": true, "rows": rows, "undo": len(undo), "hasKey": g.tmdbKey() != ""}
}

// renSetName: a name typed on the tab ("" goes back to the TMDB one).
func (g *guiServer) renSetName(id int, name string) map[string]any {
	name = strings.TrimSpace(name)
	g.ren.mu.Lock()
	defer g.ren.mu.Unlock()
	for _, r := range g.ren.rows {
		if r.ID == id {
			name = strings.TrimSuffix(name, filepath.Ext(r.Path)) // the extension never changes
			if name == strings.TrimSuffix(r.Name, filepath.Ext(r.Name)) && r.plexName == "" {
				name = ""
			}
			r.edited = name
			return okResult()
		}
	}
	return failResult("that file is no longer in the list")
}

// renRemove takes files added on the tab out of the list (queue files stay
// as long as they are in the queue).
func (g *guiServer) renRemove(ids []int, all bool) map[string]any {
	want := map[int]bool{}
	for _, id := range ids {
		want[id] = true
	}
	g.ren.mu.Lock()
	defer g.ren.mu.Unlock()
	var keep []*renRow
	for _, r := range g.ren.rows {
		if r.QueueID != 0 || !(all || want[r.ID]) {
			keep = append(keep, r)
		}
	}
	g.ren.rows = keep
	return okResult()
}

// ---------- TMDB ----------

// renLookup looks up rows on TMDB in the background: files added on the tab
// with the tab's own lookup, queue files with the queue's (they share the
// remembered matches and the "Which one?" window). extra: a match chosen just now.
func (g *guiServer) renLookup(ids []int, extra map[string]tmdbPick) map[string]any {
	key := g.tmdbKey()
	if key == "" {
		return failResult("Add your free TMDB API key in Settings (⚙) first.")
	}
	want := map[int]bool{}
	for _, id := range ids {
		want[id] = true
	}
	g.ren.mu.Lock()
	var mine []*renRow
	var queued []int
	for _, r := range g.ren.rows {
		if len(ids) > 0 && !want[r.ID] {
			continue
		}
		if r.QueueID != 0 {
			queued = append(queued, r.QueueID)
		} else if !r.looking {
			r.looking, r.Note = true, "TMDB: looking up…"
			mine = append(mine, r)
		}
	}
	g.ren.mu.Unlock()
	n := len(mine)
	if len(queued) > 0 {
		if r := g.queueTMDB(queued); r["ok"] == true {
			n += r["files"].(int)
		}
	}
	if len(mine) > 0 {
		go func() {
			dir := filepath.Join(appDataDir(), "covers")
			picks := g.tmdbPicks()
			for k, v := range extra {
				picks[k] = v
			}
			lk := &tmdbLookup{key: key, coverDir: dir, picks: picks, found: map[string]*tmdbFound{}}
			for _, r := range mine {
				g.ren.mu.Lock()
				path := r.Path
				g.ren.mu.Unlock()
				note, meta := lk.file(path)
				g.ren.mu.Lock()
				if r.Path == path {
					r.Note, r.looking, r.plexName = note, false, ""
					if meta != nil {
						r.plexName = meta.plexName
					}
				}
				g.ren.mu.Unlock()
			}
		}()
	}
	if n == 0 {
		return failResult("Nothing to look up (queue files with a title of their own are skipped).")
	}
	return map[string]any{"ok": true, "files": n}
}

// renAsks: the tab's files still needing a match, by title, for "Which one?".
// Holds g.mu (called from tmdbUnsettled); takes g.ren.mu.
func (g *guiServer) renAsksInto(list []tmdbAsk) []tmdbAsk {
	at := map[string]int{}
	for i, a := range list {
		at[a.Key] = i
	}
	g.ren.mu.Lock()
	defer g.ren.mu.Unlock()
	for _, r := range g.ren.rows {
		if r.QueueID != 0 || r.edited != "" {
			continue
		}
		none := strings.HasPrefix(r.Note, "TMDB: nothing found")
		if !none && !strings.HasPrefix(r.Note, "TMDB: needs a match") {
			continue
		}
		gs := guessSearch(r.Path, "")
		key := tmdbPickKey(gs)
		if key == "" {
			key = "file|" + r.Path
		}
		i, ok := at[key]
		if !ok {
			i, at[key] = len(list), len(list)
			list = append(list, tmdbAsk{Key: key, Kind: gs.Kind, Query: gs.Query, Year: gs.Year, Example: r.Name, None: none})
		}
		list[i].Files++
		list[i].Rows = append(list[i].Rows, r.ID)
	}
	return list
}

// ---------- renaming ----------

// renApply renames the chosen files now (and the subtitle files beside them).
// Queue files follow their new names. The batch can be undone.
func (g *guiServer) renApply(ids []int) map[string]any {
	want := map[int]bool{}
	for _, id := range ids {
		want[id] = true
	}
	g.renSync()
	g.ren.mu.Lock()
	g.renComputeLocked()
	type job struct {
		r        *renRow
		from, to string
	}
	var jobs []job
	var skipped []string
	for _, r := range g.ren.rows {
		if !want[r.ID] {
			continue
		}
		if r.State != "ready" && r.State != "warn" {
			why := r.Why
			if why == "" {
				why = map[string]string{"checking": "still being checked", "looking": "still being looked up"}[r.State]
			}
			skipped = append(skipped, r.Name+": "+why)
			continue
		}
		jobs = append(jobs, job{r, r.Path, filepath.Join(filepath.Dir(r.Path), r.Proposed+filepath.Ext(r.Path))})
	}
	g.ren.mu.Unlock()

	var moves []renMove
	renamed := 0
	for _, j := range jobs {
		if qid := j.r.QueueID; qid != 0 && !g.queueCanMove(qid) {
			skipped = append(skipped, j.r.Name+": it is being converted or edited")
			continue
		}
		if err := os.Rename(j.from, j.to); err != nil {
			skipped = append(skipped, j.r.Name+": "+renameErr(err))
			continue
		}
		renamed++
		moves = append(moves, renMove{j.from, j.to})
		moves = append(moves, moveSidecars(j.from, j.to, &skipped)...)
		g.renMoved(j.r.ID, j.from, j.to)
	}
	if len(moves) > 0 {
		g.renSaveUndo(moves)
		renLog(moves, "renamed")
	}
	return map[string]any{"ok": true, "renamed": renamed, "skipped": skipped}
}

func renameErr(err error) string {
	if le, ok := err.(*os.LinkError); ok {
		err = le.Err
	}
	s := err.Error()
	if strings.Contains(strings.ToLower(s), "used by another process") || strings.Contains(strings.ToLower(s), "sharing violation") {
		return "another program has it open (left as it is)"
	}
	return s
}

// moveSidecars renames "Old.en.srt" to "New.en.srt" when "Old.mkv" became "New.mkv".
func moveSidecars(from, to string, skipped *[]string) []renMove {
	dir := filepath.Dir(from)
	oldStem := strings.TrimSuffix(filepath.Base(from), filepath.Ext(from))
	newStem := strings.TrimSuffix(filepath.Base(to), filepath.Ext(to))
	ents, _ := os.ReadDir(dir)
	var moves []renMove
	for _, e := range ents {
		n := e.Name()
		if e.IsDir() || !sidecarExts[strings.ToLower(filepath.Ext(n))] || len(n) <= len(oldStem) ||
			!strings.EqualFold(n[:len(oldStem)], oldStem) || n[len(oldStem)] != '.' {
			continue
		}
		src, dst := filepath.Join(dir, n), filepath.Join(dir, newStem+n[len(oldStem):])
		if fileExists(dst) {
			*skipped = append(*skipped, n+": a file named "+filepath.Base(dst)+" already exists (left as it is)")
			continue
		}
		if err := os.Rename(src, dst); err != nil {
			*skipped = append(*skipped, n+": "+renameErr(err))
			continue
		}
		moves = append(moves, renMove{src, dst})
	}
	return moves
}

// queueCanMove: a queue file may be renamed while it waits.
func (g *guiServer) queueCanMove(qid int) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	it := g.findItemLocked(qid)
	return it == nil || (!it.Editing && (it.pending() || it.Status == stProblem))
}

// renMoved updates the list and the queue after a file was renamed (or put back).
func (g *guiServer) renMoved(rowID int, from, to string) {
	g.ren.mu.Lock()
	for _, r := range g.ren.rows {
		if r.ID == rowID || strings.EqualFold(r.Path, from) {
			r.Path, r.Name, r.edited = to, filepath.Base(to), ""
		}
	}
	g.ren.mu.Unlock()
	g.queueFollowMove(from, to)
}

// queueFollowMove points a queued file at its new name. Its converted file's
// name follows when it was made from the old name ("Old_KTL.mkv" → "New_KTL.mkv").
func (g *guiServer) queueFollowMove(from, to string) {
	oldStem := strings.TrimSuffix(filepath.Base(from), filepath.Ext(from))
	newStem := strings.TrimSuffix(filepath.Base(to), filepath.Ext(to))
	g.mu.Lock()
	changed := false
	for _, it := range g.q.items {
		if !strings.EqualFold(it.Input, from) || it.Editing || !(it.pending() || it.Status == stProblem) {
			continue
		}
		setRawField(it, "input", to)
		if out := filepath.Base(it.Output); strings.HasPrefix(out, oldStem) {
			setRawField(it, "output", filepath.Join(filepath.Dir(it.Output), newStem+out[len(oldStem):]))
		}
		var s settings
		if json.Unmarshal(it.Raw, &s) == nil {
			if cfg, err := validateSettings(s); err == nil {
				it.s, it.cfg, it.Input, it.Output, it.Name = s, cfg, cfg.Input, cfg.Output, filepath.Base(cfg.Input)
				it.Status, it.Note, it.checked = stChecking, "", false
				changed = true
			}
		}
	}
	g.mu.Unlock()
	if changed {
		g.kickChecker()
		g.saveQueue()
	}
}

// ---------- undo and the log ----------

func (g *guiServer) renUndoList() []renMove {
	var m []renMove
	_ = json.Unmarshal(g.loadPrefs()["renameUndo"], &m)
	return m
}

func (g *guiServer) renSaveUndo(m []renMove) {
	if b, err := json.Marshal(m); err == nil {
		_ = g.savePref("renameUndo", b)
	}
}

// renUndo puts the last batch back, newest first. A file that has been moved
// or replaced since is left alone and reported.
func (g *guiServer) renUndo() map[string]any {
	moves := g.renUndoList()
	if len(moves) == 0 {
		return failResult("There is nothing to undo.")
	}
	var back []renMove
	var skipped []string
	n := 0
	for i := len(moves) - 1; i >= 0; i-- {
		m := moves[i]
		switch {
		case !fileExists(m.To):
			skipped = append(skipped, filepath.Base(m.To)+": no longer there")
			continue
		case fileExists(m.From) && !strings.EqualFold(m.From, m.To):
			skipped = append(skipped, filepath.Base(m.To)+": a file named "+filepath.Base(m.From)+" exists again")
			continue
		}
		if err := os.Rename(m.To, m.From); err != nil {
			skipped = append(skipped, filepath.Base(m.To)+": "+renameErr(err))
			continue
		}
		back = append(back, renMove{m.To, m.From})
		if videoExts[strings.ToLower(filepath.Ext(m.From))] {
			n++
			g.renMoved(-1, m.To, m.From)
		}
	}
	g.renSaveUndo(nil)
	if len(back) > 0 {
		renLog(back, "undone")
	}
	return map[string]any{"ok": true, "restored": n, "skipped": skipped}
}

// renLog keeps a plain record of every rename (old → new), in the program's
// folder under AppData, so a name can always be found again.
func renLog(moves []renMove, what string) {
	p := renLogPath()
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if st, err := os.Stat(p); err == nil && st.Size() > 2<<20 { // keep it small: start again after 2 MB
		_ = os.Rename(p, p+".old")
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	stamp := time.Now().Format("2006-01-02 15:04")
	for _, m := range moves {
		fmt.Fprintf(f, "%s  %s  %s  →  %s\r\n", stamp, what, m.From, m.To)
	}
}

// renLogPath is shown by "Rename log".
func renLogPath() string { return filepath.Join(appDataDir(), "rename-log.txt") }
