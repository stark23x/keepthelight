package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

	s       settings
	cfg     *Config
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
	items   []*queueItem
	nextID  int
	running bool // the runner is working through the queue
	stop    bool // stop after the current file
	index   int  // position of the current file in this run (1-based)
	total   int  // files in this run

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
	if strings.EqualFold(a1, a2) {
		return nil, errors.New("the output must be a different file from the input")
	}
	if cfg.Tmp != "" {
		if st, err := os.Stat(cfg.Tmp); err != nil || !st.IsDir() {
			return nil, errors.New("the temp folder does not exist")
		}
	}
	return cfg, nil
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

// queueApply gives every file that has not started the given settings (their
// own input file and output folder/name are kept; the format follows the new
// settings).
func (g *guiServer) queueApply(raw json.RawMessage) (n int, errs []string) {
	var tmpl map[string]any
	if err := json.Unmarshal(raw, &tmpl); err != nil {
		return 0, []string{"bad request"}
	}
	ext := strings.ToLower(filepath.Ext(fmt.Sprint(tmpl["output"])))
	g.mu.Lock()
	for _, it := range g.q.items {
		if !it.pending() && it.Status != stProblem {
			continue
		}
		m := map[string]any{}
		for k, v := range tmpl {
			m[k] = v
		}
		out := it.Output
		if ext == ".mkv" || ext == ".mp4" {
			out = strings.TrimSuffix(out, filepath.Ext(out)) + ext
		}
		// where converted files go: "outDir" is a folder, or "" for next to each original
		if dir, ok := tmpl["outDir"].(string); ok {
			if dir == "" {
				dir = filepath.Dir(it.Input)
			}
			out = filepath.Join(dir, filepath.Base(out))
		}
		delete(m, "outDir")
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
	it.exists = fileExists(it.Output)
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
	g.q.running, g.q.stop, g.q.index, g.q.total = true, false, 0, n
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
	for {
		g.mu.Lock()
		if g.q.stop {
			stoppedByUser = true
			g.mu.Unlock()
			break
		}
		var it *queueItem
		waitingForCheck := false
		for _, x := range g.q.items {
			if x.Status == stChecking {
				waitingForCheck = true // keep the order: wait for it to be checked
				break
			}
			if x.Status == stWaiting {
				it = x
				break
			}
		}
		if it == nil && waitingForCheck {
			g.mu.Unlock()
			time.Sleep(200 * time.Millisecond)
			continue
		}
		if it == nil {
			g.mu.Unlock()
			break
		}
		g.q.index++
		if fileExists(it.Output) && !it.s.Overwrite {
			it.Status, it.Note = stSkipped, "The output file already exists."
			g.mu.Unlock()
			g.saveQueue()
			continue
		}
		it.Status, it.Note, it.Percent = stRunning, "", 0
		cfg, duration := *it.cfg, it.Duration
		g.mu.Unlock()
		g.saveQueue()

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
		default:
			it.Status, it.Note = stFailed, lastError(j.lines)
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
	g.q.running = false
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
	_ = os.Remove(run.Output) // left over from an interrupted run

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
		g.mu.Lock()
		defer close(j.done)
		defer g.mu.Unlock()
		j.running = false
		if err != nil {
			j.exitCode = 1
			if ee, ok := err.(*exec.ExitError); ok {
				j.exitCode = ee.ExitCode()
			}
		}
		switch {
		case j.canceled:
			j.stage = "Canceled"
			_ = os.Remove(j.partial)
		case j.exitCode == 0:
			if err := os.Rename(j.partial, j.output); err != nil {
				j.exitCode, j.stage = 1, "Failed"
				j.lines = append(j.lines, "ERROR: could not rename the finished file to "+j.output+": "+err.Error())
				return
			}
			j.stage = "Done"
			j.lines = append(j.lines, "Saved as "+j.output)
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
	if fileExists(cfg.Output) && !s.Overwrite {
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
	items := make([]queueItem, 0, len(g.q.items)) // copies: they are encoded after the lock is released
	counts := map[string]int{}
	for _, it := range g.q.items {
		if it.Status == stRunning && it.job != nil {
			it.Percent = it.job.prog.percent()
		}
		counts[it.Status]++
		items = append(items, *it)
	}
	res := map[string]any{"items": items, "counts": counts, "running": g.q.running, "index": g.q.index,
		"total": g.q.total, "after": g.q.after}
	if !g.q.powerAt.IsZero() {
		res["powerIn"] = int(time.Until(g.q.powerAt).Seconds() + 0.5)
	}
	return res
}

// speedKey groups jobs whose encode speed is comparable.
func speedKey(it *queueItem) string {
	size := "hd"
	if it.height >= 1600 {
		size = "uhd"
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

// remainingLocked is the time-left text for the window: the current file plus,
// in a batch, the files still waiting. Caller holds g.mu.
func (g *guiServer) remainingLocked(now time.Time) string {
	j := g.job
	if j == nil || !j.running {
		return ""
	}
	secs, state, ok := j.prog.remaining(now)
	if !g.q.running {
		if ok {
			return formatRemaining(secs)
		}
		return state
	}
	var cur *queueItem
	for _, it := range g.q.items {
		if it.Status == stRunning {
			cur = it
		}
	}
	curSpeed := j.prog.liveSpeed(now)
	rest, restKnown, more := 0.0, true, false
	for _, it := range g.q.items {
		if !it.pending() || (fileExists(it.Output) && !it.s.Overwrite) {
			continue
		}
		more = true
		if it.cfg.CopyVideo {
			rest += it.Duration / 50 // a stream copy is quick
			continue
		}
		sp := g.speedFor(it, cur, curSpeed)
		if sp <= 0 || it.Duration <= 0 {
			restKnown = false
			continue
		}
		passes := 1.0
		if it.cfg.TargetBytes > 0 && it.cfg.Codec == "x265" {
			passes = 2
		}
		t := it.Duration * passes / sp
		if it.dv && it.cfg.DV != "drop" {
			t *= 1.03
		}
		rest += t
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

// restoreQueue loads the saved queue (not started: the user decides).
func (g *guiServer) restoreQueue() {
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
// leaving out our own results and unfinished files.
func videosIn(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		n := e.Name()
		stem := strings.TrimSuffix(n, filepath.Ext(n))
		if e.IsDir() || !videoExts[strings.ToLower(filepath.Ext(n))] ||
			strings.HasSuffix(stem, "_hdr") || strings.HasSuffix(stem, ".partial") {
			continue
		}
		out = append(out, filepath.Join(dir, n))
	}
	return out, nil
}
