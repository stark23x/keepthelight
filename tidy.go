package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// ---------- tidying up originals after converting ----------
//
// A conversion leaves the original next to its new copy. A pair is found by
// name ("Film.mkv" and "Film_KTL.mkv") or, whatever the names, by content: a
// video carrying Keep the Light's mark next to one unmarked video of the same
// length (a copy given a Plex-style name, say). Tidying up lists those pairs, checks each copy (it has a
// picture, it has sound if the original has, it is as long as the original)
// and, when you say so, moves the originals to the Recycle Bin. On a drive
// without a Recycle Bin (a network drive) the original is renamed
// "... (original)" instead: nothing is ever deleted outright, and those
// renames can be undone. Subtitle files next to the original take the copy's
// name, so players still find them.

type tidyPair struct {
	Original string `json:"original"`
	Copy     string `json:"copy"`
	Name     string `json:"name"`     // the original's file name
	CopyName string `json:"copyName"` // the copy's
	Folder   string `json:"folder"`   // relative to the folder looked through
	OrigSize int64  `json:"origSize"`
	CopySize int64  `json:"copySize"`
	OK       bool   `json:"ok"`            // the copy passed its checks
	Why      string `json:"why,omitempty"` // not OK: what is wrong
	Bin      bool   `json:"bin"`           // the original's drive has a Recycle Bin
}

type tidyState struct {
	mu      sync.Mutex
	running bool
	dir     string
	looked  int // folders looked through
	pairs   []tidyPair
	err     string
	cancel  context.CancelFunc
}

// originalStem: the name an original would have, given a copy's name and the name tags in use.
func originalStem(stem, tag string) (string, bool) {
	for _, t := range []string{tag, "_KTL", "_hdr"} {
		if t != "" && len(stem) > len(t) && strings.EqualFold(stem[len(stem)-len(t):], t) {
			return stem[:len(stem)-len(t)], true
		}
	}
	return "", false
}

// tidyPairsIn finds the original + copy pairs among one folder's files.
func tidyPairsIn(dir string, names []string, tag string) [][2]string {
	byStem := map[string][]string{} // lower-case stem -> the videos with it
	for _, n := range names {
		if ext := strings.ToLower(filepath.Ext(n)); videoExts[ext] {
			stem := strings.ToLower(strings.TrimSuffix(n, filepath.Ext(n)))
			byStem[stem] = append(byStem[stem], n)
		}
	}
	var pairs [][2]string
	for _, n := range names {
		if !videoExts[strings.ToLower(filepath.Ext(n))] {
			continue
		}
		orig, ok := originalStem(strings.TrimSuffix(n, filepath.Ext(n)), tag)
		if !ok {
			continue
		}
		cands := byStem[strings.ToLower(orig)]
		if len(cands) != 1 { // none, or two files it could be: leave them alone
			continue
		}
		pairs = append(pairs, [2]string{filepath.Join(dir, cands[0]), filepath.Join(dir, n)})
	}
	return pairs
}

// tidyFound is a pair found in one folder; Unsure: more than one file could be
// the original (the names, for the list), so the copy is left alone.
type tidyFound struct {
	Original, Copy string
	Unsure         []string
}

// tidyVideo is what the content match needs to know about one video.
type tidyVideo struct {
	Name     string
	Marked   bool // carries Keep the Light's mark
	Duration float64
}

// sameLength: within 2 s, or 0.2% for very long files (as tidyCheck).
func sameLength(a, b float64) bool {
	return a > 0 && b > 0 && math.Abs(a-b) <= math.Max(2, a*0.002)
}

// setAside: names that are not originals to tidy (already set aside, or test clips).
func setAside(name string) bool {
	n := strings.ToLower(name)
	return strings.Contains(n, " (original") || strings.Contains(n, " test).") || strings.Contains(n, "(original clip)")
}

func episodeCode(name string) string {
	m := reEpisode.FindStringSubmatch(name)
	switch {
	case m == nil:
		return ""
	case m[1] != "":
		return strings.TrimLeft(m[1], "0") + "x" + strings.TrimLeft(m[2], "0")
	}
	return strings.TrimLeft(m[4], "0") + "x" + strings.TrimLeft(m[5], "0")
}

// tidyByMark pairs, whatever their names, each marked video with the one
// unmarked video of the same length. Several of the same length (episodes of
// a season, say): the one with the same episode number, else none.
func tidyByMark(dir string, vids []tidyVideo) []tidyFound {
	var res []tidyFound
	used := map[string]bool{}
	for _, c := range vids {
		if !c.Marked {
			continue
		}
		var cands []tidyVideo
		for _, o := range vids {
			if !o.Marked && !used[o.Name] && !setAside(o.Name) && sameLength(o.Duration, c.Duration) {
				cands = append(cands, o)
			}
		}
		if len(cands) > 1 {
			if code := episodeCode(c.Name); code != "" {
				var same []tidyVideo
				for _, o := range cands {
					if episodeCode(o.Name) == code {
						same = append(same, o)
					}
				}
				if len(same) == 1 {
					cands = same
				}
			}
		}
		switch len(cands) {
		case 0:
		case 1:
			used[cands[0].Name] = true
			res = append(res, tidyFound{Original: filepath.Join(dir, cands[0].Name), Copy: filepath.Join(dir, c.Name)})
		default:
			f := tidyFound{Copy: filepath.Join(dir, c.Name)}
			for _, o := range cands {
				f.Unsure = append(f.Unsure, o.Name)
			}
			res = append(res, f)
		}
	}
	return res
}

// tidyCheck: is the copy a whole, playable version of the original?
func tidyCheck(ctx context.Context, ffprobe, original, cp string) error {
	a, err := probeFile(ctx, ffprobe, original)
	if err != nil {
		return fmt.Errorf("the original could not be read to compare")
	}
	b, err := probeFile(ctx, ffprobe, cp)
	if err != nil {
		return fmt.Errorf("the copy cannot be read")
	}
	if b.Codec == "" || b.Width == 0 {
		return fmt.Errorf("the copy has no picture")
	}
	if a.Duration <= 0 || b.Duration <= 0 {
		return fmt.Errorf("the lengths could not be read")
	}
	if tol := math.Max(2, a.Duration*0.002); math.Abs(a.Duration-b.Duration) > tol {
		return fmt.Errorf("the copy is %s long, the original %s", formatClock(b.Duration), formatClock(a.Duration))
	}
	if audioCount(ctx, ffprobe, original) > 0 && audioCount(ctx, ffprobe, cp) == 0 {
		return fmt.Errorf("the copy has no sound")
	}
	return nil
}

func audioCount(ctx context.Context, ffprobe, path string) int {
	out, err := toolCommand(ctx, ffprobe, "-v", "error", "-select_streams", "a", "-show_entries", "stream=index", "-of", "csv=p=0", path).Output()
	if err != nil {
		return 0
	}
	return len(strings.Fields(string(out)))
}

// tidyFind looks through a folder in the background; tidyStatus reports.
func (g *guiServer) tidyFind(dir string, recursive bool, tag string) map[string]any {
	st, err := os.Stat(dir)
	if err != nil || !st.IsDir() {
		return failResult("That folder is not available: " + dir)
	}
	ffprobe, err := findTool("ffprobe", "")
	if err != nil {
		return failResult("ffprobe was not found")
	}
	t := &g.tidy
	t.mu.Lock()
	if t.cancel != nil {
		t.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.running, t.dir, t.looked, t.pairs, t.err, t.cancel = true, dir, 0, nil, "", cancel
	t.mu.Unlock()

	go func() {
		var found []tidyFound
		look := func(d string) {
			ents, err := os.ReadDir(d)
			if err != nil {
				return
			}
			var names []string
			for _, e := range ents {
				if !e.IsDir() {
					names = append(names, e.Name())
				}
			}
			paired := map[string]bool{}
			for _, p := range tidyPairsIn(d, names, tag) { // by name first
				found = append(found, tidyFound{Original: p[0], Copy: p[1]})
				paired[filepath.Base(p[0])], paired[filepath.Base(p[1])] = true, true
			}
			var rest []string // then by content, among the videos left
			for _, n := range names {
				if videoExts[strings.ToLower(filepath.Ext(n))] && !paired[n] {
					rest = append(rest, n)
				}
			}
			if len(rest) >= 2 {
				var vids []tidyVideo
				for _, n := range rest {
					if ctx.Err() != nil {
						return
					}
					if pr, err := probeFile(ctx, ffprobe, filepath.Join(d, n)); err == nil {
						vids = append(vids, tidyVideo{Name: n, Marked: pr.Converted != "", Duration: pr.Duration})
					}
				}
				found = append(found, tidyByMark(d, vids)...)
			}
			t.mu.Lock()
			t.looked++
			t.mu.Unlock()
		}
		if recursive {
			_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				if err == nil && d.IsDir() {
					look(p)
				}
				return nil
			})
		} else {
			look(dir)
		}
		for _, f := range found {
			if ctx.Err() != nil {
				break
			}
			p := tidyPair{Original: f.Original, Copy: f.Copy, Name: filepath.Base(f.Original), CopyName: filepath.Base(f.Copy), Bin: hasRecycleBin(f.Copy)}
			if rel, err := filepath.Rel(dir, filepath.Dir(f.Copy)); err == nil && rel != "." {
				p.Folder = rel
			}
			if st, err := os.Stat(f.Copy); err == nil {
				p.CopySize = st.Size()
			}
			if len(f.Unsure) > 0 {
				p.Name = strings.Join(f.Unsure, " or ")
				p.Why = "Not sure which file is the original (" + fmt.Sprint(len(f.Unsure)) + " are the same length as the copy)"
				t.mu.Lock()
				t.pairs = append(t.pairs, p)
				t.mu.Unlock()
				continue
			}
			if st, err := os.Stat(f.Original); err == nil {
				p.OrigSize = st.Size()
			}
			if err := tidyCheck(ctx, ffprobe, f.Original, f.Copy); err != nil {
				if ctx.Err() != nil {
					break
				}
				p.Why = capitalize(err.Error())
			} else {
				p.OK = true
			}
			t.mu.Lock()
			t.pairs = append(t.pairs, p)
			t.mu.Unlock()
		}
		t.mu.Lock()
		if ctx.Err() == nil {
			t.running = false
		}
		t.mu.Unlock()
	}()
	return okResult()
}

func (g *guiServer) tidyStatus() map[string]any {
	t := &g.tidy
	t.mu.Lock()
	defer t.mu.Unlock()
	pairs := append([]tidyPair{}, t.pairs...)
	sort.SliceStable(pairs, func(a, b int) bool { return pairs[a].OrigSize > pairs[b].OrigSize })
	var regain int64
	for _, p := range pairs {
		if p.OK {
			regain += p.OrigSize
		}
	}
	return map[string]any{"ok": true, "running": t.running, "dir": t.dir, "looked": t.looked, "pairs": pairs,
		"regain": formatBytes(regain), "undo": len(tidyUndoList())}
}

// tidyApply moves the chosen originals away. Each pair is checked again
// first: only a pair found by tidyFind, with both files still there and the
// copy still passing, is touched.
func (g *guiServer) tidyApply(originals []string) map[string]any {
	ffprobe, err := findTool("ffprobe", "")
	if err != nil {
		return failResult("ffprobe was not found")
	}
	t := &g.tidy
	t.mu.Lock()
	known := map[string]tidyPair{}
	for _, p := range t.pairs {
		known[strings.ToLower(p.Original)] = p
	}
	t.mu.Unlock()

	var skipped []string
	var moves []renMove
	var regained int64
	binned, renamed := 0, 0
	gone := map[string]bool{}
	for _, o := range originals {
		p, ok := known[strings.ToLower(o)]
		if !ok || !p.OK {
			continue
		}
		if g.inQueue(p.Original) || g.inQueue(p.Copy) {
			skipped = append(skipped, p.Name+": it is waiting or running in the queue")
			continue
		}
		if err := tidyCheck(context.Background(), ffprobe, p.Original, p.Copy); err != nil {
			skipped = append(skipped, p.Name+": "+err.Error()+" (left as it is)")
			continue
		}
		_, kept, err := disposeOriginalTo(p.Original)
		if err != nil {
			skipped = append(skipped, p.Name+": "+renameErr(err))
			continue
		}
		if kept != "" {
			renamed++
			moves = append(moves, renMove{p.Original, kept})
		} else {
			binned++
		}
		regained += p.OrigSize
		gone[strings.ToLower(p.Original)] = true
		moves = append(moves, moveSidecars(p.Original, p.Copy, &skipped)...)
	}
	if len(moves) > 0 {
		tidySaveUndo(append(tidyUndoList(), moves...))
		renLog(moves, "tidied up")
	}
	t.mu.Lock()
	var left []tidyPair
	for _, p := range t.pairs {
		if !gone[strings.ToLower(p.Original)] {
			left = append(left, p)
		}
	}
	t.pairs = left
	t.mu.Unlock()
	return map[string]any{"ok": true, "binned": binned, "renamed": renamed, "regained": formatBytes(regained), "skipped": skipped}
}

// inQueue: a file is the input or the output of something still to do.
func (g *guiServer) inQueue(path string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, it := range g.q.items {
		if it.finished() {
			continue
		}
		if samePath(it.Input, path) || samePath(it.Output, path) {
			return true
		}
	}
	return false
}

// ---- undo (for the renames; the Recycle Bin has its own "Restore") ----

func tidyUndoPath() string { return filepath.Join(appDataDir(), "tidy-undo.json") }

func tidyUndoList() []renMove {
	var m []renMove
	if b, err := os.ReadFile(tidyUndoPath()); err == nil {
		_ = json.Unmarshal(b, &m)
	}
	return m
}

func tidySaveUndo(m []renMove) {
	if len(m) == 0 {
		_ = os.Remove(tidyUndoPath())
		return
	}
	if len(m) > 2000 {
		m = m[len(m)-2000:]
	}
	b, _ := json.Marshal(m)
	_ = os.MkdirAll(appDataDir(), 0o755)
	_ = os.WriteFile(tidyUndoPath(), b, 0o644)
}

// tidyUndo gives the renamed originals (and subtitle files) their names back.
func (g *guiServer) tidyUndo() map[string]any {
	moves := tidyUndoList()
	if len(moves) == 0 {
		return failResult("There is nothing to undo. Originals moved to the Recycle Bin are restored from there.")
	}
	var back []renMove
	var skipped []string
	n := 0
	for i := len(moves) - 1; i >= 0; i-- {
		m := moves[i]
		switch {
		case !fileExists(m.To):
			continue // deleted since, or already put back
		case fileExists(m.From):
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
		}
	}
	tidySaveUndo(nil)
	if len(back) > 0 {
		renLog(back, "tidying undone")
	}
	return map[string]any{"ok": true, "restored": n, "skipped": skipped}
}
