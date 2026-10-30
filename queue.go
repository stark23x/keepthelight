package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// The batch queue. Every transcode started from the window goes through it:
// a single "Start transcode" is a queue of one. Files run one at a time (an
// encode already uses the whole CPU or GPU), each with the settings it was
// added with. A file that fails does not stop the rest.

// Item states, as shown in the page.
const (
	stChecking = "checking" // being probed
	stWaiting  = "waiting"  // ready to run
	stProblem  = "problem"  // cannot be transcoded as set up (see Note)
	stRunning  = "running"
	stDone     = "done"
	stFailed   = "failed"
	stSkipped  = "skipped" // the output already existed
	stCanceled = "canceled"
)

type queueItem struct {
	ID       int             `json:"id"`
	Raw      json.RawMessage `json:"settings"` // the settings as the page sent them
	Name     string          `json:"name"`     // the input file's name
	Input    string          `json:"input"`
	Output   string          `json:"output"`
	Status   string          `json:"status"`
	Note     string          `json:"note"` // problem, warning or result detail
	Info     string          `json:"info"` // what the source is, e.g. "HDR10 · 3840×2160 · 2h 1m"
	Percent  float64         `json:"percent"`
	Duration float64         `json:"duration"`
	Editing  bool            `json:"editing"` // open in the window for changes: held back until saved or cancelled
	Eta      string          `json:"eta"`     // time left, the estimate for a waiting file, or how long a finished one took
	Took     float64         `json:"took"`    // seconds a finished file took

	started time.Time // when the file started (for Took)
	run     int       // the run the file was started in (0 = not started)

	s       settings
	cfg     *Config
	width   int
	height  int
	dv      bool
	exists  bool // the output already existed when last checked
	job     *job
	checked bool
}

func (it *queueItem) finished() bool {
	switch it.Status {
	case stDone, stFailed, stSkipped, stCanceled:
		return true
	}
	return false
}

func (it *queueItem) pending() bool { return it.Status == stWaiting || it.Status == stChecking }

type batch struct {
	items    []*queueItem
	nextID   int
	running  bool  // the runner is working through the queue
	stop     bool  // stop after the current file
	only     int   // >0: run only this file (a test), not the rest of the queue
	savedRun int64 // space saved by the files finished in this run (bytes; can be negative)
	index    int   // position of the current file in this run (1-based)
	total    int   // files in this run
	runSeq   int   // number of the current (or last) run

	after      string    // "", "sleep" or "shutdown": what to do when the queue is finished
	powerAt    time.Time // when the countdown for "after" ends (zero = no countdown)
	powerTimer *time.Timer

	speeds     map[string]float64 // measured encode speed by kind of job (video seconds per second)
	lastSpeed  float64
	lastHeight int

	checkOnce sync.Once
	checkKick chan struct{}
}

// powerDelay is the time to cancel a sleep or shutdown after the queue ends.
var powerDelay = 60 * time.Second

// validate checks a set of settings before it goes into the queue.
func validateSettings(s settings) (*Config, error) {
	cfg, err := s.toConfig()
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(cfg.Input); err != nil {
		return nil, errors.New("input file not found: " + cfg.Input)
	}
	if cfg.Output == "" {
		return nil, errors.New("choose an output file")
	}
	if st, err := os.Stat(filepath.Dir(cfg.Output)); err != nil || !st.IsDir() {
		return nil, errors.New("the output folder does not exist: " + filepath.Dir(cfg.Output))
	}
	switch strings.ToLower(filepath.Ext(cfg.Output)) {
	case ".mkv", ".mp4":
	default:
		return nil, errors.New("the output must end in .mkv or .mp4")
	}
	a1, _ := filepath.Abs(cfg.Input)
	a2, _ := filepath.Abs(cfg.Output)
	if strings.EqualFold(a1, a2) && !cfg.ReplaceOriginal {
		return nil, errors.New("the output must be a different file from the input")
	}
	if cfg.Tmp != "" {
		if st, err := os.Stat(cfg.Tmp); err != nil || !st.IsDir() {
			return nil, errors.New("the temp folder does not exist")
		}
	}
	return cfg, nil
}

// outputTaken: another file already has the output's name (the original
// itself does not count when the result is to replace it).
func outputTaken(cfg *Config) bool {
	if cfg == nil {
		return false
	}
	return fileExists(cfg.Output) && !(cfg.ReplaceOriginal && samePath(cfg.Output, cfg.Input))
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// partialName is where a job writes its output until it has finished, so a
// half-written file never sits under the final name (e.g. after a crash).
func partialName(output string) string {
	ext := filepath.Ext(output)
	return strings.TrimSuffix(output, ext) + ".partial" + ext
}

// ---- adding, removing, ordering ----

// queueAdd validates and appends items. It returns the new IDs and, per
// rejected item, a message.
func (g *guiServer) queueAdd(raws []json.RawMessage) (ids []int, errs []string) {
	g.mu.Lock()
	for _, raw := range raws {
		var s settings
		if err := json.Unmarshal(raw, &s); err != nil {
			errs = append(errs, "bad request")
			continue
		}
		name := filepath.Base(s.Input)
		cfg, err := validateSettings(s)
		if err != nil {
			errs = append(errs, name+": "+err.Error())
			continue
		}
		if dup := g.findOutput(cfg.Output); dup != nil {
			errs = append(errs, name+": already in the queue (same output file as "+dup.Name+")")
			continue
		}
		g.q.nextID++
		it := &queueItem{ID: g.q.nextID, Raw: raw, Name: name, Input: cfg.Input, Output: cfg.Output,
			Status: stChecking, s: s, cfg: cfg}
		g.q.items = append(g.q.items, it)
		ids = append(ids, it.ID)
		if g.q.running {
			g.q.total++ // added while the queue runs: it joins this run
		}
	}
	g.mu.Unlock()
	g.kickChecker()
	g.saveQueue()
	return ids, errs
}

// findOutput returns an unfinished item that writes the same output file.
// Caller holds g.mu.
func (g *guiServer) findOutput(out string) *queueItem {
	a, _ := filepath.Abs(out)
	for _, it := range g.q.items {
		if it.finished() || it.Status == stProblem {
			continue
		}
		if b, _ := filepath.Abs(it.Output); strings.EqualFold(a, b) {
			return it
		}
	}
	return nil
}

func (g *guiServer) itemByID(id int) (int, *queueItem) {
	for i, it := range g.q.items {
		if it.ID == id {
			return i, it
		}
	}
	return -1, nil
}

func (g *guiServer) queueRemove(id int) error {
	g.mu.Lock()
	i, it := g.itemByID(id)
	if it == nil {
		g.mu.Unlock()
		return errors.New("no such file in the queue")
	}
	if it.Status == stRunning {
		g.mu.Unlock()
		return errors.New("this file is being transcoded; cancel it first")
	}
	g.q.items = append(g.q.items[:i], g.q.items[i+1:]...)
	if g.q.running && it.pending() {
		g.q.total--
	}
	g.mu.Unlock()
	g.saveQueue()
	return nil
}

// queueMove moves a not-yet-started item up (-1) or down (+1) past its neighbour.
func (g *guiServer) queueMove(id, dir int) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	i, it := g.itemByID(id)
	if it == nil || it.Status == stRunning || it.finished() {
		return errors.New("only files that have not started can be moved")
	}
	j := i + dir
	if j < 0 || j >= len(g.q.items) || g.q.items[j].Status == stRunning || g.q.items[j].finished() {
		return nil // already at the end of the movable part
	}
	g.q.items[i], g.q.items[j] = g.q.items[j], g.q.items[i]
	go g.saveQueue()
	return nil
}

// queueClear removes finished items (and those with problems).
func (g *guiServer) queueClear() {
	g.mu.Lock()
	kept := g.q.items[:0]
	for _, it := range g.q.items {
		if !it.finished() && it.Status != stProblem {
			kept = append(kept, it)
		}
	}
	g.q.items = kept
	g.mu.Unlock()
	g.saveQueue()
}

// queueClearAll empties the queue: everything except the file being
// transcoded right now (which goes on; Stop queue cancels it).
func (g *guiServer) queueClearAll() int {
	g.mu.Lock()
	kept, n := g.q.items[:0], 0
	for _, it := range g.q.items {
		if it.Status == stRunning {
			kept = append(kept, it)
		} else {
			n++
		}
	}
	g.q.items = kept
	if g.q.running {
		g.q.total = g.q.index // nothing more to come in this run
	}
	g.mu.Unlock()
	g.saveQueue()
	return n
}

// queueCopyTracks gives the waiting files the track choices of one file (its
// audio and subtitle tracks, by position), when their tracks are laid out the
// same way. Files that differ are left as they are and named in the reply.
func (g *guiServer) queueCopyTracks(id int) map[string]any {
	ffprobe, err := findTool("ffprobe", "")
	if err != nil {
		return failResult("ffprobe was not found")
	}
	g.mu.Lock()
	src := g.findItemLocked(id)
	if src == nil {
		g.mu.Unlock()
		return failResult("That file is no longer in the queue.")
	}
	if src.s.AudioTracks == nil && src.s.SubTracks == nil {
		g.mu.Unlock()
		return failResult("This file has no track choices of its own: Edit it, choose its tracks, and save it first.")
	}
	srcIn, srcSettings := src.Input, src.s
	type target struct {
		it    *queueItem
		input string
	}
	var targets []target
	for _, it := range g.q.items {
		if it != src && (it.pending() || it.Status == stProblem) && !it.Editing {
			targets = append(targets, target{it, it.Input})
		}
	}
	g.mu.Unlock()
	if len(targets) == 0 {
		return failResult("There are no other waiting files.")
	}
	ctx := context.Background()
	st, err := probeTracks(ctx, ffprobe, srcIn)
	if err != nil {
		return failResult("Could not read the tracks of " + filepath.Base(srcIn) + ": " + err.Error())
	}
	want := trackLayout(st)
	layouts := make([]string, len(targets))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4) // a few at a time: the files may be on the network
	for i := range targets {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if t, err := probeTracks(ctx, ffprobe, targets[i].input); err == nil {
				layouts[i] = trackLayout(t)
			}
		}(i)
	}
	wg.Wait()

	var applied int
	var different []string
	g.mu.Lock()
	for i, tg := range targets {
		it := tg.it
		if layouts[i] != want {
			different = append(different, it.Name)
			continue
		}
		if !(it.pending() || it.Status == stProblem) || it.Editing || it.Input != tg.input {
			continue // started, or changed meanwhile
		}
		var m map[string]any
		_ = json.Unmarshal(it.Raw, &m)
		m["audioTracks"], m["subTracks"] = srcSettings.AudioTracks, srcSettings.SubTracks
		m["audioDefault"], m["subDefault"] = srcSettings.AudioDefault, srcSettings.SubDefault
		b, _ := json.Marshal(m)
		var s settings
		_ = json.Unmarshal(b, &s)
		cfg, err := validateSettings(s)
		if err != nil {
			different = append(different, it.Name+" ("+err.Error()+")")
			continue
		}
		it.Raw, it.s, it.cfg = b, s, cfg
		it.Status, it.Note, it.checked = stChecking, "", false
		applied++
	}
	g.mu.Unlock()
	g.kickChecker()
	g.saveQueue()
	return map[string]any{"ok": true, "applied": applied, "different": different}
}

// queueApply gives every file that has not started the given settings (their
// own input file and output folder/name are kept; the format follows the new
// settings).
func (g *guiServer) queueApply(raw json.RawMessage) (n int, errs []string) {
	var tmpl map[string]any
	if err := json.Unmarshal(raw, &tmpl); err != nil {
		return 0, []string{"bad request"}
	}
	ext := strings.ToLower(filepath.Ext(fmt.Sprint(tmpl["output"])))
	keepOwn, _ := tmpl["keepOwn"].(bool)
	tag, setTag := tmpl["nameTag"].(string)
	g.mu.Lock()
	for _, it := range g.q.items {
		if (!it.pending() && it.Status != stProblem) || it.Editing {
			continue
		}
		m := map[string]any{}
		for k, v := range tmpl {
			m[k] = v
		}
		if keepOwn { // the file's own track choices and metadata stay
			var old map[string]any
			_ = json.Unmarshal(it.Raw, &old)
			for _, k := range ownKeys {
				if v, ok := old[k]; ok {
					m[k] = v
				}
			}
		}
		out := it.Output
		if ext == ".mkv" || ext == ".mp4" {
			out = strings.TrimSuffix(out, filepath.Ext(out)) + ext
		}
		if setTag { // a fresh name with this ending (a profile's name, for example)
			base := filepath.Base(it.Input)
			out = filepath.Join(filepath.Dir(out), strings.TrimSuffix(base, filepath.Ext(base))+cleanTag(tag)+filepath.Ext(out))
		}
		// where converted files go: "outDir" is a folder, or "" for next to each original
		if dir, ok := tmpl["outDir"].(string); ok {
			if dir == "" {
				dir = filepath.Dir(it.Input)
			}
			out = filepath.Join(dir, filepath.Base(out))
		}
		if r, _ := m["replaceOriginal"].(bool); r {
			out = replacedName(it.Input, filepath.Ext(out))
		}
		for _, k := range []string{"outDir", "keepOwn", "nameTag"} {
			delete(m, k)
		}
		m["input"], m["output"], m["overwrite"] = it.Input, out, false
		b, _ := json.Marshal(m)
		var s settings
		_ = json.Unmarshal(b, &s)
		cfg, err := validateSettings(s)
		if err != nil {
			errs = append(errs, it.Name+": "+err.Error())
			continue
		}
		it.Raw, it.s, it.cfg, it.Output = b, s, cfg, cfg.Output
		it.Status, it.Note, it.checked = stChecking, "", false
		n++
	}
	g.mu.Unlock()
	g.kickChecker()
	g.saveQueue()
	return n, errs
}

// startTest runs a short test of the form's settings straight away, on its own:
// files waiting in the queue are not started.
func (g *guiServer) startTest(raw json.RawMessage) map[string]any {
	var s settings
	if err := json.Unmarshal(raw, &s); err != nil || s.TestSeconds <= 0 {
		return failResult("bad request")
	}
	if g.running() {
		return failResult("A job is already running. Try the test when it has finished.")
	}
	s.Overwrite = true // a new test replaces the previous one
	if _, err := validateSettings(s); err != nil {
		return failResult(capitalize(err.Error()))
	}
	b, _ := json.Marshal(s)
	ids, errs := g.queueAdd([]json.RawMessage{b})
	if len(errs) > 0 {
		return failResult(errs[0])
	}
	g.mu.Lock()
	if g.q.running {
		g.mu.Unlock()
		return failResult("The queue is running. Try the test when it has finished.")
	}
	// put the test first and run only it
	for i, it := range g.q.items {
		if it.ID == ids[0] && i > 0 {
			copy(g.q.items[1:i+1], g.q.items[0:i])
			g.q.items[0] = it
		}
	}
	g.q.only = ids[0]
	g.mu.Unlock()
	if err := g.startQueue(); err != nil {
		g.mu.Lock()
		g.q.only = 0
		g.mu.Unlock()
		return failResult(capitalize(err.Error()))
	}
	return okResult()
}

// ownKeys are a file's own choices: its tracks and its metadata.
var ownKeys = []string{"audioTracks", "subTracks", "audioDefault", "subDefault", "meta"}

// ---- editing one queued file in the window ----

func (g *guiServer) findItemLocked(id int) *queueItem {
	for _, it := range g.q.items {
		if it.ID == id {
			return it
		}
	}
	return nil
}

// queueEditStart opens a waiting file for changes and returns its settings.
func (g *guiServer) queueEditStart(id int) map[string]any {
	g.mu.Lock()
	defer g.mu.Unlock()
	it := g.findItemLocked(id)
	switch {
	case it == nil:
		return failResult("That file is no longer in the queue.")
	case !it.pending() && it.Status != stProblem:
		return failResult("Only files that have not started can be changed.")
	}
	for _, x := range g.q.items {
		x.Editing = false // one at a time
	}
	it.Editing = true
	return map[string]any{"ok": true, "settings": it.Raw}
}

// queueEditSave puts the changed settings back and lets the file run again.
func (g *guiServer) queueEditSave(id int, raw json.RawMessage) map[string]any {
	var s settings
	if err := json.Unmarshal(raw, &s); err != nil {
		return failResult("bad request")
	}
	cfg, err := validateSettings(s)
	if err != nil {
		return failResult(capitalize(err.Error()))
	}
	g.mu.Lock()
	it := g.findItemLocked(id)
	if it == nil || (!it.pending() && it.Status != stProblem) {
		g.mu.Unlock()
		return failResult("That file has started or is no longer in the queue, so the changes could not be saved.")
	}
	it.Raw, it.s, it.cfg, it.Output, it.Input = raw, s, cfg, cfg.Output, cfg.Input
	it.Status, it.Note, it.checked, it.Editing = stChecking, "", false, false
	g.mu.Unlock()
	g.kickChecker()
	g.saveQueue()
	return okResult()
}

func (g *guiServer) queueEditCancel(id int) map[string]any {
	g.mu.Lock()
	if it := g.findItemLocked(id); it != nil {
		it.Editing = false
	}
	g.mu.Unlock()
	return okResult()
}

// ---- checking (probing) items ----

func (g *guiServer) kickChecker() {
	g.q.checkOnce.Do(func() {
		g.q.checkKick = make(chan struct{}, 1)
		go g.checker()
	})
	select {
	case g.q.checkKick <- struct{}{}:
	default:
	}
}

// checker probes queued files one at a time, in order.
func (g *guiServer) checker() {
	for range g.q.checkKick {
		for {
			g.mu.Lock()
			var it *queueItem
			for _, x := range g.q.items {
				if x.Status == stChecking {
					it = x
					break
				}
			}
			var s settings
			if it != nil {
				s = it.s
			}
			g.mu.Unlock()
			if it == nil {
				break
			}
			r := g.probe(context.Background(), s)
			g.mu.Lock()
			if it.Status == stChecking { // not removed or changed meanwhile
				applyProbe(it, r)
			}
			g.mu.Unlock()
		}
	}
}

// applyProbe stores what the probe found. Caller holds g.mu.
func applyProbe(it *queueItem, r map[string]any) {
	it.checked = true
	if ok, _ := r["ok"].(bool); !ok {
		it.Status, it.Note = stProblem, fmt.Sprint(r["error"])
		return
	}
	it.Duration, _ = r["duration"].(float64)
	if it.cfg != nil && it.cfg.SampleSecs > 0 && it.Duration > float64(it.cfg.SampleSecs) {
		it.Duration = float64(it.cfg.SampleSecs) // a test: only that much is encoded
	}
	it.width, _ = r["width"].(int)
	it.height, _ = r["height"].(int)
	_, it.dv = r["dv"]
	parts := []string{map[string]string{"hdr10": "HDR10", "hlg": "HLG"}[fmt.Sprint(r["kind"])]}
	if parts[0] == "" {
		parts[0] = "SDR"
	}
	if dv, ok := r["dv"].(string); ok {
		parts = append(parts, "Dolby Vision "+dv)
	}
	if hp, _ := r["hdr10plus"].(bool); hp {
		parts = append(parts, "HDR10+")
	}
	parts = append(parts, fmt.Sprintf("%v×%v", r["width"], r["height"]))
	if it.Duration > 0 {
		parts = append(parts, shortDuration(it.Duration))
	}
	it.Info = strings.Join(parts, " · ")
	if b, ok := r["blocker"].(string); ok && b != "" {
		it.Status, it.Note = stProblem, b
		return
	}
	it.Status = stWaiting
	it.exists = outputTaken(it.cfg)
	it.Note = ""
	if it.exists && !it.s.Overwrite {
		it.Note = "The output file already exists, so this file will be skipped."
	}
}

func shortDuration(secs float64) string {
	m := int(secs+30) / 60
	if m >= 60 {
		return fmt.Sprintf("%dh %02dm", m/60, m%60)
	}
	return fmt.Sprintf("%d min", max(1, m))
}

// ---- running ----

// startQueue starts working through the waiting files.
func (g *guiServer) startQueue() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.q.running {
		return errors.New("the queue is already running")
	}
	n := 0
	for _, it := range g.q.items {
		if it.pending() {
			n++
		}
	}
	if n == 0 {
		return errors.New("there is nothing waiting in the queue")
	}
	g.cancelPowerLocked()
	if g.q.only > 0 {
		n = 1 // a test on its own
	}
	g.q.running, g.q.stop, g.q.index, g.q.total = true, false, 0, n
	g.q.runSeq++
	if g.q.only == 0 {
		g.q.savedRun = 0 // a new run
	}
	go g.runQueue()
	return nil
}

// stopQueue cancels the current file and does not start any more.
func (g *guiServer) stopQueue() {
	g.mu.Lock()
	g.q.stop = true
	g.mu.Unlock()
	g.cancel()
}

func (g *guiServer) runQueue() {
	stoppedByUser := false
	finished, failed := 0, 0 // files this run got to the end of (for the sound at the end)
	for {
		g.mu.Lock()
		if g.q.stop {
			stoppedByUser = true
			g.mu.Unlock()
			break
		}
		var it *queueItem
		waitingForCheck, waitingForEdit := false, false
		for _, x := range g.q.items {
			if g.q.only > 0 && x.ID != g.q.only {
				continue // a test runs on its own
			}
			if x.Editing && (x.Status == stWaiting || x.Status == stChecking) {
				waitingForEdit = true // held back; the queue goes on with the next file
				continue
			}
			if x.Status == stChecking {
				waitingForCheck = true // keep the order: wait for it to be checked
				break
			}
			if x.Status == stWaiting {
				it = x
				break
			}
		}
		if it == nil && (waitingForCheck || waitingForEdit) {
			g.mu.Unlock()
			time.Sleep(200 * time.Millisecond)
			continue
		}
		if it == nil {
			g.mu.Unlock()
			break
		}
		g.q.index++
		it.run = g.q.runSeq
		if outputTaken(it.cfg) && !it.s.Overwrite {
			it.Status, it.Note = stSkipped, "The output file already exists."
			g.mu.Unlock()
			g.saveQueue()
			continue
		}
		it.Status, it.Note, it.Percent, it.Took, it.started = stRunning, "", 0, 0, g.clock()
		cfg, duration := *it.cfg, it.Duration
		g.mu.Unlock()
		g.saveQueue()

		var inSize int64 // for "space saved": the original's size before anything happens to it
		if st, err := os.Stat(cfg.Input); err == nil {
			inSize = st.Size()
		}
		j, err := g.launchJob(&cfg, duration)
		if err != nil {
			g.mu.Lock()
			it.Status, it.Note = stFailed, err.Error()
			g.mu.Unlock()
			continue
		}
		g.mu.Lock()
		it.job = j
		g.mu.Unlock()
		<-j.done

		g.mu.Lock()
		switch {
		case j.canceled && g.closing:
			it.Status, it.Note = stWaiting, "Interrupted when Keep the Light was closed; it starts again from the beginning."
		case j.canceled:
			it.Status, it.Note = stCanceled, "Canceled."
		case j.exitCode == 0:
			it.Status, it.Note, it.Percent = stDone, "", 100
			it.Took = g.clock().Sub(it.started).Seconds()
			for _, l := range j.lines { // a test's measured quality
				if strings.HasPrefix(l, "Test quality:") {
					it.Note = l
				}
			}
			if cfg.SampleSecs == 0 && inSize > 0 { // space saved (not for tests)
				if st, err := os.Stat(it.Output); err == nil {
					saved := inSize - st.Size()
					it.Note = savedText(inSize, st.Size())
					g.q.savedRun += saved
					g.addSavedTotalLocked(saved)
				}
			}
			finished++
		default:
			it.Status, it.Note = stFailed, lastError(j.lines)
			finished++
			failed++
		}
		if sp := j.prog.speed; sp > 0 && !it.cfg.CopyVideo {
			if g.q.speeds == nil {
				g.q.speeds = map[string]float64{}
			}
			g.q.speeds[speedKey(it)] = sp
			g.q.lastSpeed, g.q.lastHeight = sp, it.height
		}
		g.mu.Unlock()
		g.saveQueue()
	}
	g.mu.Lock()
	g.q.running, g.q.only = false, 0
	closing := g.closing
	g.mu.Unlock()
	if !stoppedByUser && !closing && finished > 0 {
		g.finishedAlert(failed > 0) // before a sleep / shut-down countdown starts
	}
	g.mu.Lock()
	if g.q.after != "" && !stoppedByUser {
		g.schedulePowerLocked()
	}
	g.mu.Unlock()
	g.saveQueue()
}

// lastError picks the most useful line of a failed job's log: the tool's own
// error message if there is one, rather than just "ffmpeg failed: exit status 1".
func lastError(lines []string) string {
	generic := ""
	for i := len(lines) - 1; i >= 0 && i >= len(lines)-40; i-- {
		l := strings.TrimSpace(lines[i])
		low := strings.ToLower(l)
		if strings.HasPrefix(l, "ERROR:") {
			msg := strings.TrimSpace(strings.TrimPrefix(l, "ERROR:"))
			if !strings.Contains(msg, "failed: exit status") {
				return msg
			}
			if generic == "" {
				generic = msg
			}
			continue
		}
		if generic != "" && strings.Contains(low, "error") {
			return l + " (" + strings.SplitN(generic, " ", 2)[0] + ")"
		}
	}
	if generic != "" {
		return generic
	}
	return "Failed - see the log."
}

// launchJob starts one transcode: this same exe in command-line mode, so a
// crash in the transcode cannot take the window down with it. The output is
// written to a ".partial" file and renamed when it is complete.
func (g *guiServer) launchJob(cfg *Config, duration float64) (*job, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	final := cfg.Output
	run := *cfg
	run.Output = partialName(final)
	g.mu.Lock()
	run.LowPriority, run.HWDecode = g.lowPrio, g.hwDec
	if g.workLocal { // the temp folder, if it is on this PC; else Windows' own temp folder
		run.WorkLocal = os.TempDir()
		if run.Tmp != "" && !isNetworkPath(run.Tmp) {
			run.WorkLocal = run.Tmp
		}
	}
	g.mu.Unlock()
	_ = removeFile(run.Output) // left over from an interrupted run

	g.mu.Lock()
	g.jobSeq++
	j := &job{running: true, output: final, partial: run.Output, stage: "Starting", id: g.jobSeq,
		prog: newProgressTracker(duration), done: make(chan struct{})}
	g.job = j
	g.mu.Unlock()

	cmd := exec.Command(exe, run.cliArgs()...)
	hideWindow(cmd)
	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw
	cmd.Stdin = nil
	j.cmd = cmd
	if err := cmd.Start(); err != nil {
		g.mu.Lock()
		j.running, j.exitCode, j.stage = false, -1, "Failed"
		j.lines = append(j.lines, "Could not start: "+err.Error())
		g.mu.Unlock()
		close(j.done)
		return nil, err
	}
	go g.readOutput(j, pr)
	go func() {
		err := cmd.Wait()
		_ = pw.Close()
		code := 0
		if err != nil {
			code = 1
			if ee, ok := err.(*exec.ExitError); ok {
				code = ee.ExitCode()
			}
		}
		g.mu.Lock()
		canceled := j.canceled
		if code == 0 && !canceled && cfg.ReplaceOriginal {
			j.stage = "Replacing the original"
		}
		g.mu.Unlock()

		// put the finished file in place (outside the lock: replacing checks the file first)
		var lines []string
		var finishErr error
		if code == 0 && !canceled {
			if cfg.ReplaceOriginal {
				ffprobe, err := findTool("ffprobe", "")
				if err != nil {
					finishErr = err
				} else {
					lines, finishErr = replaceOriginal(context.Background(), ffprobe, cfg.Input, j.partial, j.output)
				}
			} else if err := renameFile(j.partial, j.output); err != nil {
				finishErr = fmt.Errorf("could not rename the finished file to %s: %v", j.output, err)
			} else {
				lines = []string{"Saved as " + j.output}
			}
		}

		g.mu.Lock()
		defer close(j.done)
		defer g.mu.Unlock()
		j.running, j.exitCode = false, code
		switch {
		case canceled:
			j.stage = "Canceled"
			_ = removeFile(j.partial)
		case code == 0 && finishErr != nil:
			j.exitCode, j.stage = 1, "Failed"
			j.lines = append(j.lines, "ERROR: "+finishErr.Error())
		case code == 0:
			j.stage = "Done"
			j.lines = append(j.lines, lines...)
			j.prog.finish()
		default:
			j.stage = "Failed"
		}
	}()
	return j, nil
}

// start is "Start transcode" for the file in the form: a queue of one (plus
// anything else already waiting).
func (g *guiServer) start(ctx context.Context, raw json.RawMessage) map[string]any {
	var s settings
	if err := json.Unmarshal(raw, &s); err != nil {
		return failResult("bad request")
	}
	cfg, err := validateSettings(s)
	if err != nil {
		return failResult(capitalize(err.Error()))
	}
	if g.running() {
		return failResult("A job is already running")
	}
	if outputTaken(cfg) && !s.Overwrite {
		return map[string]any{"ok": false, "exists": true, "error": cfg.Output + " already exists."}
	}
	if _, errs := g.queueAdd([]json.RawMessage{raw}); len(errs) > 0 {
		return failResult(errs[0])
	}
	if err := g.startQueue(); err != nil {
		return failResult(capitalize(err.Error()))
	}
	return okResult()
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// ---- status, estimate, title ----

// queueStatus is the queue part of the status the page polls. Caller holds g.mu.
func (g *guiServer) queueStatusLocked() map[string]any {
	now := g.clock()
	cur, curSpeed := g.currentLocked(now)
	items := make([]queueItem, 0, len(g.q.items)) // copies: they are encoded after the lock is released
	counts := map[string]int{}
	for _, it := range g.q.items {
		if it.Status == stRunning && it.job != nil {
			it.Percent = it.job.prog.percent()
		}
		counts[it.Status]++
		c := *it
		c.Eta = g.itemEtaLocked(it, cur, curSpeed, now)
		items = append(items, c)
	}
	res := map[string]any{"items": items, "counts": counts, "running": g.q.running, "index": g.q.index,
		"total": g.q.total, "after": g.q.after, "savedRun": g.q.savedRun, "savedRunText": signedBytes(g.q.savedRun),
		"savedTotal": g.savedTotal, "savedTotalText": signedBytes(g.savedTotal)}
	if g.q.running && g.q.total > 1 {
		res["batch"] = map[string]any{"percent": g.batchPercentLocked(), "eta": g.batchEtaLocked(now)}
	}
	if !g.q.powerAt.IsZero() {
		res["powerIn"] = int(time.Until(g.q.powerAt).Seconds() + 0.5)
	}
	return res
}

// itemEtaLocked is the time text shown on a file's row: the time left for the
// running file, an estimate for a waiting one, how long a finished one took.
// Caller holds g.mu.
func (g *guiServer) itemEtaLocked(it, cur *queueItem, curSpeed float64, now time.Time) string {
	switch {
	case it.Status == stRunning && it.job != nil:
		return it.job.prog.etaText(now)
	case it.Status == stDone && it.Took > 0:
		return "took " + shortDuration(it.Took)
	case it.Status == stWaiting && !it.willSkip():
		if secs, ok := g.estimateLocked(it, cur, curSpeed); ok {
			return formatAbout(secs)
		}
		return "estimate after the first minutes"
	}
	return ""
}

// willSkip is true for a file that will be skipped because its output exists.
func (it *queueItem) willSkip() bool {
	return it.cfg != nil && !it.s.Overwrite && fileExists(it.Output)
}

// inRunLocked is true for a file that has not started but will in this run.
// Caller holds g.mu.
func (g *guiServer) inRunLocked(it *queueItem) bool {
	return it.pending() && (g.q.only == 0 || it.ID == g.q.only) && !it.willSkip()
}

// currentLocked returns the running file and its encode speed so far (0 = not
// known yet). Caller holds g.mu.
func (g *guiServer) currentLocked(now time.Time) (*queueItem, float64) {
	var cur *queueItem
	for _, it := range g.q.items {
		if it.Status == stRunning {
			cur = it
		}
	}
	if j := g.job; cur != nil && j != nil && j.running {
		return cur, j.prog.liveSpeed(now)
	}
	return cur, 0
}

// speedKey groups jobs whose encode speed is comparable: same encoder, preset
// and resolution. The width tells 4K, 1080p and 720p apart even when the
// picture is cropped to a wide film shape.
func speedKey(it *queueItem) string {
	size := "hd"
	switch {
	case it.width >= 3000 || it.height >= 1600:
		size = "uhd"
	case it.width >= 1700 || it.height >= 1000:
		size = "fhd"
	}
	return it.cfg.Codec + "/" + it.cfg.Preset + "/" + size
}

// speedFor guesses how fast an item will encode: the measured speed of a
// similar job if there was one, otherwise the latest speed adjusted for the
// picture size. 0 = no idea yet. Caller holds g.mu.
func (g *guiServer) speedFor(it *queueItem, cur *queueItem, curSpeed float64) float64 {
	if cur != nil && curSpeed > 0 && speedKey(cur) == speedKey(it) {
		return curSpeed
	}
	if sp := g.q.speeds[speedKey(it)]; sp > 0 {
		return sp
	}
	base, h := g.q.lastSpeed, g.q.lastHeight
	if cur != nil && curSpeed > 0 {
		base, h = curSpeed, cur.height
	}
	if base <= 0 || h <= 0 || it.height <= 0 {
		return 0
	}
	r := float64(h) / float64(it.height) // speed scales roughly with the number of pixels
	return base * r * r
}

// work is how much video a file has to get through: its length, times the
// encode passes, plus Dolby Vision packaging. A stream copy is quick.
func (it *queueItem) work() float64 {
	if it.cfg == nil {
		return 0
	}
	if it.cfg.CopyVideo {
		return it.Duration / 50
	}
	w := it.Duration
	if it.cfg.TargetBytes > 0 && it.cfg.Codec == "x265" {
		w *= 2 // two passes
	}
	if it.dv && it.cfg.DV != "drop" {
		w *= 1.03
	}
	return w
}

// estimateLocked is the time a file that has not started should take, in
// seconds. ok is false while there is nothing to base it on. Caller holds g.mu.
func (g *guiServer) estimateLocked(it, cur *queueItem, curSpeed float64) (float64, bool) {
	if it.Duration <= 0 {
		return 0, false
	}
	if it.cfg.CopyVideo {
		return it.work(), true
	}
	sp := g.speedFor(it, cur, curSpeed)
	if sp <= 0 {
		return 0, false
	}
	return it.work() / sp, true
}

// batchPercentLocked is how much of the run is done, 0-100: each file counts
// for its length, so a film weighs more than an episode. Files that are
// skipped are left out. Caller holds g.mu.
func (g *guiServer) batchPercentLocked() float64 {
	type part struct{ w, frac float64 }
	var parts []part
	known, nKnown := 0.0, 0
	for _, it := range g.q.items {
		var frac float64
		switch {
		case it.run == g.q.runSeq && it.Status == stRunning:
			frac = it.Percent / 100
		case it.run == g.q.runSeq && it.finished() && it.Status != stSkipped:
			frac = 1
		case g.inRunLocked(it):
		default:
			continue
		}
		w := it.work()
		if w > 0 {
			known += w
			nKnown++
		}
		parts = append(parts, part{w, frac})
	}
	avg := 1.0 // a file not checked yet counts as an average one
	if nKnown > 0 {
		avg = known / float64(nKnown)
	}
	sum, done := 0.0, 0.0
	for _, p := range parts {
		if p.w <= 0 {
			p.w = avg
		}
		sum += p.w
		done += p.w * p.frac
	}
	if sum <= 0 {
		return 0
	}
	return math.Min(100, 100*done/sum)
}

// restLocked estimates the files still to come in this run: their total time,
// whether every one could be estimated, and whether there are any. Caller holds g.mu.
func (g *guiServer) restLocked(now time.Time) (rest float64, known, more bool) {
	cur, curSpeed := g.currentLocked(now)
	known = true
	for _, it := range g.q.items {
		if !g.inRunLocked(it) {
			continue
		}
		more = true
		if t, ok := g.estimateLocked(it, cur, curSpeed); ok {
			rest += t
		} else {
			known = false
		}
	}
	return rest, known, more
}

// remainingLocked is the time-left text for the window: the current file plus,
// in a batch, the files still waiting. Caller holds g.mu.
func (g *guiServer) remainingLocked(now time.Time) string {
	j := g.job
	if j == nil || !j.running {
		return ""
	}
	secs, state, ok := j.prog.remaining(now)
	rest, restKnown, more := 0.0, true, false
	if g.q.running {
		rest, restKnown, more = g.restLocked(now)
	}
	if !more {
		if ok {
			return formatRemaining(secs)
		}
		return state
	}
	if !ok && state == "finishing" {
		secs, ok = 0, true
	}
	if !ok || !restKnown {
		if ok {
			return formatRemaining(secs) + " for this file"
		}
		return state
	}
	return strings.Replace(formatRemaining(secs+rest), " left", " left in total", 1)
}

// batchEtaLocked is the time left for the whole run, shown over the Queue tab.
// Caller holds g.mu.
func (g *guiServer) batchEtaLocked(now time.Time) string {
	rest, restKnown, _ := g.restLocked(now)
	secs, state, ok := 0.0, "", true
	if j := g.job; j != nil && j.running {
		secs, state, ok = j.prog.remaining(now)
		if !ok && state == "finishing" {
			secs, ok = 0, true
		}
	}
	if !ok || !restKnown {
		return "estimate after the first minutes"
	}
	return formatRemaining(secs + rest)
}

// titleLocked is the window title: progress first, so it shows on the taskbar.
// Caller holds g.mu.
func (g *guiServer) titleLocked(now time.Time) string {
	if !g.q.powerAt.IsZero() {
		what := map[string]string{"sleep": "Going to sleep", "shutdown": "Shutting down"}[g.q.after]
		return fmt.Sprintf("%s in %d s - %s", what, int(time.Until(g.q.powerAt).Seconds()+0.5), windowTitle)
	}
	j := g.job
	if j == nil || !j.running {
		return windowTitle
	}
	t := titleText(true, j.prog.percent(), g.remainingLocked(now))
	if g.q.running && g.q.total > 1 {
		t = fmt.Sprintf("File %d of %d · %s", g.q.index, g.q.total, t)
	}
	return t
}

func (g *guiServer) windowTitleNow() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.titleLocked(g.clock())
}

// ---- when the queue is finished: sleep / shut down ----

func (g *guiServer) setAfter(action string) error {
	if action != "" && action != "sleep" && action != "shutdown" {
		return errors.New("unknown action")
	}
	g.mu.Lock()
	g.q.after = action
	if action == "" {
		g.cancelPowerLocked()
	}
	g.mu.Unlock()
	return nil
}

// schedulePowerLocked starts the countdown; the page offers Cancel meanwhile.
func (g *guiServer) schedulePowerLocked() {
	action := g.q.after
	g.q.powerAt = time.Now().Add(powerDelay)
	g.q.powerTimer = time.AfterFunc(powerDelay, func() {
		g.mu.Lock()
		if g.q.powerAt.IsZero() { // canceled
			g.mu.Unlock()
			return
		}
		g.q.powerAt, g.q.after = time.Time{}, "" // one-off: it must never carry over
		g.mu.Unlock()
		if err := powerAction(action); err != nil && g.notify != nil {
			g.notify("Could not " + map[string]string{"sleep": "put the PC to sleep", "shutdown": "shut down"}[action] + ": " + err.Error())
		}
	})
}

func (g *guiServer) cancelPowerLocked() {
	if g.q.powerTimer != nil {
		g.q.powerTimer.Stop()
	}
	g.q.powerAt, g.q.powerTimer = time.Time{}, nil
}

// ---- remembering the queue ----

// saveQueue stores the files that have not been transcoded yet (or could not
// be, yet), so the queue
// comes back after the window is closed or the PC restarts.
func (g *guiServer) saveQueue() {
	g.mu.Lock()
	var raws []json.RawMessage
	for _, it := range g.q.items {
		// files with a problem are kept too: after e.g. installing GPAC they pass the check next time
		if it.pending() || it.Status == stRunning || it.Status == stProblem {
			raws = append(raws, it.Raw)
		}
	}
	g.mu.Unlock()
	b, _ := json.Marshal(raws)
	if raws == nil {
		b = []byte("[]")
	}
	_ = g.savePref("queue", b)
}

// restoreQueue loads the saved queue (not started: the user decides) and the
// low-priority setting.
func (g *guiServer) restoreQueue() {
	var low bool
	if b, ok := g.loadPrefs()["lowPriority"]; ok && json.Unmarshal(b, &low) == nil {
		g.mu.Lock()
		g.lowPrio = low
		g.mu.Unlock()
	}
	var wl bool
	if b, ok := g.loadPrefs()["workLocal"]; ok && json.Unmarshal(b, &wl) == nil {
		g.mu.Lock()
		g.workLocal = wl
		g.mu.Unlock()
	}
	var total int64
	if b, ok := g.loadPrefs()["spaceSaved"]; ok && json.Unmarshal(b, &total) == nil {
		g.mu.Lock()
		g.savedTotal = total
		g.mu.Unlock()
	}
	var hw bool
	if b, ok := g.loadPrefs()["hwDecode"]; ok && json.Unmarshal(b, &hw) == nil {
		g.mu.Lock()
		g.hwDec = hw
		g.mu.Unlock()
	}
	var lang string
	if b, ok := g.loadPrefs()["tmdbLang"]; ok && json.Unmarshal(b, &lang) == nil && tmdbLanguages[lang] {
		tmdbLang.Store(lang)
	}
	var raws []json.RawMessage
	if b, ok := g.loadPrefs()["queue"]; ok {
		_ = json.Unmarshal(b, &raws)
	}
	if len(raws) > 0 {
		g.queueAdd(raws)
	}
}

// queueLog returns the log of a file's transcode (for failed files).
func (g *guiServer) queueLog(id int) map[string]any {
	g.mu.Lock()
	defer g.mu.Unlock()
	_, it := g.itemByID(id)
	if it == nil || it.job == nil {
		return failResult("no log for this file")
	}
	return map[string]any{"ok": true, "lines": append([]string(nil), it.job.lines...)}
}

// videoExts are the files "Add folder" picks up.
var videoExts = map[string]bool{".mkv": true, ".mp4": true, ".m4v": true, ".mov": true, ".ts": true, ".m2ts": true,
	".mts": true, ".webm": true, ".avi": true, ".hevc": true, ".h265": true}

// videosIn lists the video files directly in a folder (not in subfolders),
// leaving out our own results (names ending in tag, or in "_hdr" from before 1.8)
// and unfinished files.
func videosIn(dir, tag string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		n := e.Name()
		stem := strings.TrimSuffix(n, filepath.Ext(n))
		if e.IsDir() || !videoExts[strings.ToLower(filepath.Ext(n))] ||
			strings.HasSuffix(stem, "_hdr") || strings.HasSuffix(stem, "_KTL") || (tag != "" && strings.HasSuffix(stem, tag)) || strings.HasSuffix(stem, ".partial") {
			continue
		}
		out = append(out, filepath.Join(dir, n))
	}
	return out, nil
}

// spaceCheck reports, before metadata-only jobs start, how much they will
// write and whether the drives have room. form is the file in the form when it
// is started on its own; without it, the waiting queue is checked.
func (g *guiServer) spaceCheck(form json.RawMessage) map[string]any {
	type job struct {
		cfg  *Config
		size int64
	}
	var jobs []job
	add := func(cfg *Config) {
		if cfg == nil || !cfg.MetaOnly {
			return
		}
		var n int64
		if st, err := os.Stat(cfg.Input); err == nil {
			n = st.Size()
		}
		jobs = append(jobs, job{cfg, n})
	}
	if len(form) > 0 && string(form) != "null" {
		var s settings
		if json.Unmarshal(form, &s) == nil {
			if cfg, err := s.toConfig(); err == nil {
				add(cfg)
			}
		}
	} else {
		g.mu.Lock()
		for _, it := range g.q.items {
			if it.Status == stWaiting || it.Status == stChecking {
				add(it.cfg)
			}
		}
		g.mu.Unlock()
	}
	res := map[string]any{"ok": true, "metaJobs": len(jobs)}
	if len(jobs) == 0 {
		return res
	}
	type drive struct {
		Name       string `json:"name"`
		Need, Free int64  `json:"-"`
		NeedText   string `json:"need"`
		FreeText   string `json:"free"`
		Enough     bool   `json:"enough"`
	}
	drives := map[string]*drive{}
	var order []string
	var total int64
	replace := 0
	var files []string
	for _, j := range jobs {
		total += j.size
		if j.cfg.ReplaceOriginal {
			replace++
		}
		if len(files) < 3 {
			files = append(files, j.cfg.Output)
		}
		dir := filepath.Dir(j.cfg.Output)
		k := volumeKey(dir)
		d := drives[k]
		if d == nil {
			d = &drive{Name: k, Free: -1}
			if f, err := diskFree(dir); err == nil {
				d.Free = int64(f)
			}
			drives[k] = d
			order = append(order, k)
		}
		d.Need += j.size
	}
	ok := true
	var list []*drive
	for _, k := range order {
		d := drives[k]
		d.NeedText, d.Enough = formatBytes(d.Need), d.Free < 0 || d.Free > d.Need+(64<<20)
		if d.Free >= 0 {
			d.FreeText = formatBytes(d.Free)
		}
		ok = ok && d.Enough
		list = append(list, d)
	}
	res["total"], res["drives"], res["enough"], res["replace"], res["files"] = formatBytes(total), list, ok, replace, files
	return res
}

// ---- space saved ----

// savedText: "18.7 GB → 9.2 GB · saved 9.5 GB (51%)", or how much bigger it got.
func savedText(in, out int64) string {
	s := formatBytes(in) + " → " + formatBytes(out)
	switch d := in - out; {
	case d > 0:
		return fmt.Sprintf("%s · saved %s (%d%%)", s, formatBytes(d), int(float64(d)*100/float64(in)+0.5))
	case d < 0:
		return fmt.Sprintf("%s · %s bigger than the original", s, formatBytes(-d))
	}
	return s + " · the same size"
}

func signedBytes(b int64) string {
	if b < 0 {
		return "-" + formatBytes(-b)
	}
	return formatBytes(b)
}

// addSavedTotalLocked adds to the running total of space saved (one number in
// the settings, no history). Caller holds g.mu.
func (g *guiServer) addSavedTotalLocked(d int64) {
	g.savedTotal += d
	raw, _ := json.Marshal(g.savedTotal)
	_ = g.savePref("spaceSaved", raw) // its own lock: safe while g.mu is held
}
