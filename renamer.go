package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
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
	Minutes   float64  `json:"minutes,omitempty"`
	Season    int      `json:"season,omitempty"` // an episode: its season and its own number (from its name)
	FileEp    int      `json:"fileEp,omitempty"`
	FileEp2   int      `json:"fileEp2,omitempty"`
	Mapped    string   `json:"mapped,omitempty"` // what it is on TMDB, when that is not its own number
	EpMode    string   `json:"epMode,omitempty"` // double | split: marked here

	plexName string // from TMDB, with plexDetailsMark
	tvID     int    // the show's TMDB number, when known
	edited   string // the name typed on the tab
	probed   bool
	looking  bool
}

type renMove struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type renState struct {
	mu        sync.Mutex
	rows      []*renRow
	next      int
	shifts    map[string][]epShift  // double / split episodes, by show and season (episodes.go)
	suggest   []doubleSuggestion    // files that look like double episodes
	dismissed map[string]bool       // suggestions answered "No"
	spotted   map[string]bool       // seasons already checked (key + their files' numbers)
	fixed     map[string]bool       // files renamed to TMDB's numbers here (the marks no longer apply to them)
	numSug    []numberingSuggestion // seasons TMDB and TVmaze number differently (tvmaze.go)
	tvmEps    map[string][]tvmazeEp // TVmaze's episodes of the seasons checked, by shift key
	tvmStatus map[string]tvmStatus  // what TVmaze said about each season, by shift key
}

// renShifts is a copy of the marked double / split episodes, for a lookup.
func (g *guiServer) renShifts() map[string][]epShift {
	g.ren.mu.Lock()
	defer g.ren.mu.Unlock()
	out := map[string][]epShift{}
	for k, v := range g.ren.shifts {
		out[k] = append([]epShift(nil), v...)
	}
	for p := range g.ren.fixed { // "fixed|<path>": files already renumbered
		out["fixed|"+p] = nil
	}
	return out
}

// setEpisodeInfo fills in a row's season, number and show key from its name.
func (r *renRow) setEpisodeInfo() {
	gs := guessSearch(r.Path, "")
	r.Kind = gs.Kind
	r.Season, r.FileEp, r.FileEp2 = 0, 0, 0
	if gs.Kind == "tv" {
		r.Season, r.FileEp, r.FileEp2 = gs.Season, gs.Episode, gs.Episode2
	}
}

func (r *renRow) shiftKey() string {
	if r.Kind != "tv" || r.Season == 0 {
		return ""
	}
	pk := tmdbPickKey(guessSearch(r.Path, ""))
	if pk == "" {
		return ""
	}
	return shiftKey(pk, r.Season)
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
		r := &renRow{ID: g.ren.next, Path: p, Name: filepath.Base(p)}
		r.setEpisodeInfo()
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
		r.Path, r.Name = q.input, filepath.Base(q.input)
		r.setEpisodeInfo()
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
		details, plex, mins := "", []string(nil), 0.0
		if err == nil {
			details, plex, mins = probeForName(ffprobe, path)
		}
		g.ren.mu.Lock()
		if r.Path == path {
			r.Details, r.Plex, r.Minutes, r.probed = details, plex, mins, true
		}
		g.ren.mu.Unlock()
	}
}

// probeForName: "2160p.HEVC.DV" for the file as it is, its Plex check and its length (minutes).
func probeForName(ffprobe, path string) (string, []string, float64) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	data, err := toolCommand(ctx, ffprobe, "-v", "error", "-show_streams", "-show_format", "-of", "json", path).Output()
	if err != nil {
		return "", nil, 0
	}
	var p scanProbe
	if json.Unmarshal(data, &p) != nil {
		return "", nil, 0
	}
	secs, _ := strconv.ParseFloat(p.Format.Duration, 64)
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
		return "", plex, secs / 60
	}
	return plexDetailsFor(w, h, codec, hdr), plex, secs / 60
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
	// names freed in the same batch: a file that is itself renamed (S05E10 → S05E11 while S05E11 → S05E12)
	moving := map[string]bool{}
	for _, r := range g.ren.rows {
		if r.Proposed != "" && r.Proposed+filepath.Ext(r.Path) != filepath.Base(r.Path) {
			moving[strings.ToLower(r.Path)] = true
		}
	}
	for _, r := range g.ren.rows {
		r.State, r.Why = "", ""
		// what an episode is on TMDB, when the season has a double (or split) episode marked
		r.Mapped, r.EpMode = "", ""
		if k := r.shiftKey(); k != "" && r.FileEp > 0 && !g.ren.fixed[strings.ToLower(r.Path)] {
			sh := g.ren.shifts[k]
			for _, x := range sh {
				if x.At == r.FileEp && r.FileEp2 == 0 {
					r.EpMode = x.Kind
				}
			}
			if e := mapEpisode(r.FileEp, r.FileEp2, sh); e.First != r.FileEp || e.Part > 0 || (e.Last > e.First && r.FileEp2 == 0) {
				r.Mapped = "TMDB " + e.String()
			}
		}
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
		case !strings.EqualFold(to, r.Path) && fileExists(to) && !moving[strings.ToLower(to)]:
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
	sug := append([]doubleSuggestion(nil), g.ren.suggest...)
	nsug := append([]numberingSuggestion(nil), g.ren.numSug...)
	numbering := g.tvNumbering()
	type tvmSeason struct {
		Key    string `json:"key"`
		Season int    `json:"season"`
		Show   string `json:"show"`
	}
	var byTVmaze []tvmSeason // seasons on the list that use TVmaze's numbering (so it can be undone)
	var statuses []tvmStatus // what TVmaze said about each season on the list
	seen := map[string]bool{}
	for _, r := range g.ren.rows {
		k := r.shiftKey()
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		if numbering[k] == "tvmaze" {
			byTVmaze = append(byTVmaze, tvmSeason{k, r.Season, guessSearch(r.Path, "").Query})
		}
		if st, ok := g.ren.tvmStatus[k]; ok {
			statuses = append(statuses, st)
		}
	}
	sort.Slice(statuses, func(a, b int) bool {
		if statuses[a].Show != statuses[b].Show {
			return statuses[a].Show < statuses[b].Show
		}
		return statuses[a].Season < statuses[b].Season
	})
	busy := false
	for _, r := range g.ren.rows {
		busy = busy || r.looking || !r.probed
	}
	g.ren.mu.Unlock()
	if !busy {
		go g.renSpot() // look for double episodes (once per season and set of files)
	}
	undo := g.renUndoList()
	return map[string]any{"ok": true, "rows": rows, "undo": len(undo), "hasKey": g.tmdbKey() != "", "doubles": sug,
		"numbering": nsug, "byTVmaze": byTVmaze, "tvmazeOn": g.tvmazeOn(), "tvmaze": statuses}
}

// ---------- double episodes on the tab (episodes.go) ----------

// renMark marks (or unmarks, kind "") the row's file as a double episode, or as
// the first half of a split one, and looks the rest of its season up again.
func (g *guiServer) renMark(id int, kind string) map[string]any {
	if kind != "" && kind != "double" && kind != "split" {
		return failResult("unknown kind")
	}
	g.ren.mu.Lock()
	var row *renRow
	for _, r := range g.ren.rows {
		if r.ID == id {
			row = r
		}
	}
	if row == nil || row.shiftKey() == "" || row.FileEp == 0 {
		g.ren.mu.Unlock()
		return failResult("Only an episode with a season and episode number in its name (S05E09) can be marked.")
	}
	if row.FileEp2 > 0 {
		g.ren.mu.Unlock()
		return failResult("This file's name already says it is a double episode (" + fmt.Sprintf("E%02d-E%02d", row.FileEp, row.FileEp2) + ").")
	}
	k := row.shiftKey()
	g.renSetShiftLocked(k, row.FileEp, kind)
	ids := g.renSeasonRowsLocked(k)
	g.ren.mu.Unlock()
	if g.tmdbKey() == "" {
		return okResult()
	}
	g.renLookupAgain(ids, nil, true)
	return map[string]any{"ok": true, "files": len(ids)}
}

func (g *guiServer) renSetShiftLocked(k string, at int, kind string) {
	if g.ren.shifts == nil {
		g.ren.shifts = map[string][]epShift{}
	}
	var keep []epShift
	for _, x := range g.ren.shifts[k] {
		if x.At != at {
			keep = append(keep, x)
		}
	}
	if kind != "" {
		keep = append(keep, epShift{At: at, Kind: kind})
	}
	g.ren.shifts[k] = keep
	var sug []doubleSuggestion // a suggestion answered by marking is gone
	for _, s := range g.ren.suggest {
		if !(s.Key == k && s.At == at) {
			sug = append(sug, s)
		}
	}
	g.ren.suggest = sug
}

// renSeasonRowsLocked: the rows of one season (by shift key).
func (g *guiServer) renSeasonRowsLocked(k string) []int {
	var ids []int
	for _, r := range g.ren.rows {
		if r.shiftKey() == k {
			ids = append(ids, r.ID)
		}
	}
	return ids
}

// renCompareTVmaze asks TVmaze about one season and, when its numbering and
// TMDB's disagree (and no choice was made for that season yet), suggests it.
func (g *guiServer) renCompareTVmaze(ctx context.Context, key string, tmdbID int, show, k string, season int, eps []tmdbEpisode, files map[int]float64, chosen string) {
	tid, err := tvmazeShow(ctx, key, tmdbID, show)
	switch {
	case err != nil:
		g.renTVmStatus(k, season, show, "offline", "TVmaze could not be reached; TMDB alone is used")
		return
	case tid == 0:
		g.renTVmStatus(k, season, show, "notfound", "TVmaze does not have this show; TMDB alone is used")
		return
	}
	tvm, err := tvmazeSeason(ctx, tid, season)
	switch {
	case err != nil:
		g.renTVmStatus(k, season, show, "offline", "TVmaze could not be reached; TMDB alone is used")
		return
	case len(tvm) == 0:
		g.renTVmStatus(k, season, show, "notfound", fmt.Sprintf("TVmaze has the show but no season %d; TMDB alone is used", season))
		return
	}
	have := map[int]bool{}
	for n := range files {
		have[n] = true
	}
	differ, rows := compareNumbering(eps, tvm, have)
	switch {
	case chosen == "tvmaze":
		g.renTVmStatus(k, season, show, "chosen", "TVmaze's numbering is used for this season")
	case !differ:
		g.renTVmStatus(k, season, show, "agree", fmt.Sprintf("agrees with TMDB (%d episodes)", len(eps)))
	case chosen == "tmdb":
		g.renTVmStatus(k, season, show, "chosen", "numbers it differently; TMDB's numbering is kept, as you chose")
	default:
		g.renTVmStatus(k, season, show, "differ", "numbers it differently (see below)")
	}
	g.ren.mu.Lock()
	defer g.ren.mu.Unlock()
	if g.ren.tvmEps == nil {
		g.ren.tvmEps = map[string][]tvmazeEp{}
	}
	g.ren.tvmEps[k] = tvm
	if chosen != "" || !differ {
		return
	}
	for _, x := range g.ren.numSug {
		if x.Key == k {
			return
		}
	}
	sum := fmt.Sprintf("TMDB has %d episodes in this season and TVmaze %d", len(eps), len(tvm))
	if len(eps) == len(tvm) {
		n := 0
		for _, r := range rows {
			if r.Differ {
				n++
			}
		}
		sum = fmt.Sprintf("both have %d episodes, but %d of them have different titles at the same number (a different order)", len(eps), n)
	}
	g.ren.numSug = append(g.ren.numSug, numberingSuggestion{Key: k, Season: season, Show: show, TMDB: len(eps), TVmaze: len(tvm), Summary: sum, Rows: rows})
}

// tvmStatus is what TVmaze said about one season on the list.
type tvmStatus struct {
	Key    string `json:"key"`
	Season int    `json:"season"`
	Show   string `json:"show"`
	Kind   string `json:"kind"` // agree | differ | chosen | alone | notfound | offline
	Text   string `json:"text"`
}

func (g *guiServer) renTVmStatus(k string, season int, show, kind, text string) {
	g.ren.mu.Lock()
	defer g.ren.mu.Unlock()
	if g.ren.tvmStatus == nil {
		g.ren.tvmStatus = map[string]tvmStatus{}
	}
	g.ren.tvmStatus[k] = tvmStatus{Key: k, Season: season, Show: show, Kind: kind, Text: text}
}

// renTVmazeAlone: a season TMDB has nothing for; TVmaze's answer is shown.
func (g *guiServer) renTVmazeAlone(ctx context.Context, k string, season int, show string) {
	id, err := tvmazeShow(ctx, "", 0, show)
	switch {
	case err != nil:
		g.renTVmStatus(k, season, show, "offline", "TMDB has nothing for it, and TVmaze could not be reached")
	case id == 0:
		g.renTVmStatus(k, season, show, "notfound", "neither TMDB nor TVmaze has this show (use Edit to search)")
	default:
		eps, err := tvmazeSeason(ctx, id, season)
		if err != nil || len(eps) == 0 {
			g.renTVmStatus(k, season, show, "notfound", fmt.Sprintf("TMDB has nothing for it; TVmaze has the show but no season %d", season))
			return
		}
		g.renTVmStatus(k, season, show, "alone", fmt.Sprintf("TMDB has nothing for it; filled in from TVmaze (%d episodes)", len(eps)))
	}
}

// renNumbering sets how a season is numbered: "tvmaze" (titles from TVmaze, by
// the files' own numbers; double / split marks for it are dropped) or "tmdb"
// (as before). The season's files are looked up again.
func (g *guiServer) renNumbering(k, source string) map[string]any {
	if source != "tvmaze" && source != "tmdb" {
		return failResult("unknown numbering")
	}
	g.setTVNumbering(k, source)
	g.ren.mu.Lock()
	for sig := range g.ren.spotted { // look at the season again (what TVmaze says is shown for the new choice)
		if strings.HasPrefix(sig, k+"|map[") {
			delete(g.ren.spotted, sig)
		}
	}
	var sug []numberingSuggestion
	for _, x := range g.ren.numSug {
		if x.Key != k {
			sug = append(sug, x)
		}
	}
	g.ren.numSug = sug
	if source == "tvmaze" {
		delete(g.ren.shifts, k)
		var ds []doubleSuggestion
		for _, x := range g.ren.suggest {
			if x.Key != k {
				ds = append(ds, x)
			}
		}
		g.ren.suggest = ds
	}
	ids := g.renSeasonRowsLocked(k)
	g.ren.mu.Unlock()
	if g.tmdbKey() == "" || len(ids) == 0 {
		return okResult()
	}
	g.renLookupAgain(ids, nil, true)
	return map[string]any{"ok": true, "files": len(ids)}
}

// renAnswer answers a suggestion: yes marks the double episode, no forgets it.
func (g *guiServer) renAnswer(key string, at int, yes bool) map[string]any {
	g.ren.mu.Lock()
	if !yes {
		if g.ren.dismissed == nil {
			g.ren.dismissed = map[string]bool{}
		}
		g.ren.dismissed[fmt.Sprintf("%s|%d", key, at)] = true
		var sug []doubleSuggestion
		for _, s := range g.ren.suggest {
			if !(s.Key == key && s.At == at) {
				sug = append(sug, s)
			}
		}
		g.ren.suggest = sug
		g.ren.mu.Unlock()
		return okResult()
	}
	var id int
	for _, r := range g.ren.rows {
		if r.shiftKey() == key && r.FileEp == at && r.FileEp2 == 0 {
			id = r.ID
		}
	}
	g.ren.mu.Unlock()
	if id == 0 {
		return failResult("that file is no longer in the list")
	}
	return g.renMark(id, "double")
}

var renSpotting sync.Mutex

// renSpot checks each season in the list for a file that looks like two TMDB
// episodes (by its length and TMDB's titles and running times).
func (g *guiServer) renSpot() {
	if !renSpotting.TryLock() {
		return
	}
	defer renSpotting.Unlock()
	key := g.tmdbKey()
	if key == "" {
		return
	}
	type season struct {
		key, input string
		season     int
		tvID       int
		files      map[int]float64
		names      map[int]string
	}
	g.ren.mu.Lock()
	seasons := map[string]*season{}
	for _, r := range g.ren.rows {
		k := r.shiftKey()
		if k == "" || r.FileEp == 0 {
			continue
		}
		s := seasons[k]
		if s == nil {
			s = &season{key: k, input: r.Path, season: r.Season, files: map[int]float64{}, names: map[int]string{}}
			seasons[k] = s
		}
		if r.FileEp2 > 0 { // a double already named so: both numbers are files' (not a candidate for spotting)
			for n := r.FileEp; n <= r.FileEp2; n++ {
				if _, ok := s.files[n]; !ok {
					s.files[n] = -1
				}
			}
		} else {
			s.files[r.FileEp], s.names[r.FileEp] = r.Minutes, r.Name
		}
		if r.tvID != 0 {
			s.tvID = r.tvID
		}
	}
	var todo []*season
	for k, s := range seasons {
		sig := fmt.Sprintf("%s|%v", k, s.files)
		if g.ren.spotted == nil {
			g.ren.spotted = map[string]bool{}
		}
		if !g.ren.spotted[sig] {
			g.ren.spotted[sig] = true
			todo = append(todo, s)
		}
	}
	shifts := map[string][]epShift{}
	for k, v := range g.ren.shifts {
		shifts[k] = v
	}
	g.ren.mu.Unlock()
	for _, s := range todo {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		id, show := s.tvID, ""
		if id == 0 { // a queue file: the show from the remembered match, or a search
			lk := &tmdbLookup{key: key, picks: g.tmdbPicks(), found: map[string]*tmdbFound{}}
			if hit, _ := lk.showFor(ctx, s.input); hit != nil {
				id, show = hit.ID, hit.Title
			}
		}
		if show == "" {
			show = guessSearch(s.input, "").Query
		}
		if id == 0 { // TMDB has no such show (or it was matched on TVmaze): what TVmaze says
			if g.tvmazeOn() {
				g.renTVmazeAlone(ctx, s.key, s.season, show)
			}
			cancel()
			continue
		}
		eps, err := tmdbSeason(ctx, key, id, s.season)
		if err != nil {
			if g.tvmazeOn() {
				g.renTVmazeAlone(ctx, s.key, s.season, show)
			}
			cancel()
			continue
		}
		numbering := g.tvNumbering()[s.key]
		if g.tvmazeOn() || numbering == "tvmaze" {
			g.renCompareTVmaze(ctx, key, id, show, s.key, s.season, eps, s.files, numbering)
		}
		cancel()
		if numbering == "tvmaze" {
			continue // the files keep their own numbers, TVmaze's way: no double episodes to look for
		}
		for n, m := range s.files { // spotting a double needs lengths, and single-episode files
			if m <= 0 {
				delete(s.files, n)
			}
		}
		if len(s.files) < 3 {
			continue
		}
		byNum := map[int]tmdbEpisode{}
		for _, e := range eps {
			byNum[e.Number] = e
		}
		for _, d := range spotDoubles(s.files, eps, shifts[s.key]) {
			later := 0
			for n := range s.files {
				if n > d.At {
					later++
				}
			}
			e := mapEpisode(d.At, 0, append(shifts[s.key], d))
			title := joinTitles([]string{byNum[e.First].Name, byNum[e.First+1].Name})
			g.ren.mu.Lock()
			dup := g.ren.dismissed[fmt.Sprintf("%s|%d", s.key, d.At)]
			for _, x := range g.ren.suggest {
				dup = dup || (x.Key == s.key && x.At == d.At)
			}
			if !dup {
				g.ren.suggest = append(g.ren.suggest, doubleSuggestion{Key: s.key, At: d.At, Season: s.season, Show: show,
					File: s.names[d.At], Title: title, Later: later,
					Why: fmt.Sprintf("it is %.0f minutes long, about twice the others, and TMDB has its episodes %d and %d as \"%s\"", s.files[d.At], e.First, e.Last, baseTitle(byNum[e.First].Name))})
			}
			g.ren.mu.Unlock()
		}
	}
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
	return g.renLookupAgain(ids, extra, false)
}

// renLookupAgain: again=true also redoes queue files already filled in from TMDB.
func (g *guiServer) renLookupAgain(ids []int, extra map[string]tmdbPick, again bool) map[string]any {
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
		if r := g.queueTMDBAgain(queued, again); r["ok"] == true {
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
			lk := &tmdbLookup{key: key, coverDir: dir, picks: picks, found: map[string]*tmdbFound{}, shifts: g.renShifts(), numbering: g.tvNumbering(), tvmaze: g.tvmazeOn()}
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
						if meta.tvID != 0 {
							r.tvID = meta.tvID
						}
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
		r              *renRow
		from, to, orig string // orig: its name before (from may be a temporary name)
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
		jobs = append(jobs, job{r, r.Path, filepath.Join(filepath.Dir(r.Path), r.Proposed+filepath.Ext(r.Path)), r.Path})
	}
	g.ren.mu.Unlock()

	var moves []renMove
	renamed := 0
	move := func(j job, from string) bool {
		if err := os.Rename(from, j.to); err != nil {
			skipped = append(skipped, j.r.Name+": "+renameErr(err))
			return false
		}
		moves = append(moves, renMove{from, j.to})
		moves = append(moves, moveSidecars(j.orig, j.to, &skipped)...)
		g.renMoved(j.r.ID, j.orig, j.to)
		renamed++
		return true
	}
	var todo []job
	for _, j := range jobs {
		if qid := j.r.QueueID; qid != 0 && !g.queueCanMove(qid) {
			skipped = append(skipped, j.r.Name+": it is being converted or edited")
			continue
		}
		todo = append(todo, j)
	}
	// a season renumbered (S05E10 → S05E11 while S05E11 → S05E12): rename each file once its new name is free,
	// so nothing is ever overwritten; a loop (A → B, B → A) goes through a temporary name
	for len(todo) > 0 {
		var wait []job
		for _, j := range todo {
			if fileExists(j.to) && !strings.EqualFold(j.to, j.from) {
				wait = append(wait, j)
				continue
			}
			move(j, j.from)
		}
		if len(wait) == len(todo) { // stuck: each target is another's source, or is someone else's file
			busy := map[string]bool{}
			for _, j := range wait {
				busy[strings.ToLower(j.from)] = true
			}
			var next []job
			for _, j := range wait {
				if !busy[strings.ToLower(j.to)] {
					skipped = append(skipped, j.r.Name+": a file named "+filepath.Base(j.to)+" already exists")
					continue
				}
				tmp := j.from + ".ktl-renaming"
				if err := os.Rename(j.from, tmp); err != nil {
					skipped = append(skipped, j.r.Name+": "+renameErr(err))
					continue
				}
				moves = append(moves, renMove{j.from, tmp})
				j.from = tmp
				next = append(next, j)
			}
			if len(next) == 0 {
				break
			}
			wait = next
		}
		todo = wait
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
	mapped := false
	for _, r := range g.ren.rows {
		if r.ID == rowID || strings.EqualFold(r.Path, from) {
			mapped = mapped || r.Mapped != ""
			r.Path, r.Name, r.edited = to, filepath.Base(to), ""
			r.setEpisodeInfo()
		}
	}
	// a file renumbered for a double episode now carries TMDB's numbers in its name: the season's marks no
	// longer apply to it (and do again if it is put back)
	if g.ren.fixed == nil {
		g.ren.fixed = map[string]bool{}
	}
	if g.ren.fixed[strings.ToLower(from)] {
		delete(g.ren.fixed, strings.ToLower(from))
	} else if mapped {
		g.ren.fixed[strings.ToLower(to)] = true
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
