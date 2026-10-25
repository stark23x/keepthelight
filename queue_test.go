package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestMain lets the test binary stand in for the command-line program: the
// queue starts "this exe" with transcode options, which in a test is this binary.
func TestMain(m *testing.M) {
	if os.Getenv("KTL_AS_CLI") == "1" {
		os.Exit(run())
	}
	os.Exit(m.Run())
}

const fakeProbeJSON = `{"streams":[{"codec_name":"hevc","width":3840,"height":2160,"pix_fmt":"yuv420p10le",
 "color_space":"bt2020nc","color_transfer":"smpte2084","color_primaries":"bt2020","r_frame_rate":"24/1","avg_frame_rate":"24/1"}],
 "format":{"duration":"60.0"},"frames":[{"side_data_list":[]}]}`

// fakeTools puts stand-in ffmpeg/ffprobe on PATH. ffmpeg fails for inputs
// whose name contains "bad"; otherwise it writes the output file.
func fakeTools(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses shell scripts")
	}
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "probe.json"), []byte(fakeProbeJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	write("ffprobe", `for a in "$@"; do [ "$a" = "v:0" ] && exec cat "`+dir+`/probe.json"; done; echo '{"streams":[]}'`+"\n")
	write("ffmpeg", `case "$*" in -version|--version) echo "fake 1.0"; exit 0;; *-encoders*) echo " V..... libx265 x"; exit 0;; *"-h full"*) echo "-fps_mode"; exit 0;; *"-h muxer"*) echo "cues_to_front"; exit 0;; esac
for a in "$@"; do last="$a"; case "$a" in *bad*) echo "Error: broken file" >&2; exit 1;; *slow*) slow=1;; esac; done
[ -n "$slow" ] && sleep 30
echo "frame= 100 fps=50 q=28.0 size= 1kB time=00:00:30.00 bitrate=1k speed=1x" >&2
echo done > "$last"
`)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("KTL_AS_CLI", "1")
}

func itemSettings(in, out string) json.RawMessage {
	b, _ := json.Marshal(map[string]any{"input": in, "output": out, "mode": "encode", "codec": "x265",
		"quality": "crf", "crf": 18, "preset": "medium", "audio": "copy", "dv": "auto"})
	return b
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for end := time.Now().Add(20 * time.Second); time.Now().Before(end); time.Sleep(50 * time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

func statuses(g *guiServer) []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	var s []string
	for _, it := range g.q.items {
		s = append(s, it.Name+"="+it.Status)
	}
	return s
}

func TestQueueRunsBatch(t *testing.T) {
	fakeTools(t)
	dir := t.TempDir()
	mk := func(name string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("video"), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	a, b, c := mk("a.mkv"), mk("b.mkv"), mk("bad.mkv")
	if err := os.WriteFile(filepath.Join(dir, "b_hdr.mkv"), []byte("earlier result"), 0o644); err != nil {
		t.Fatal(err)
	}
	g := newGUIServer()
	g.prefsPath = filepath.Join(t.TempDir(), "settings.json")
	out := func(p string) string { return strings.TrimSuffix(p, ".mkv") + "_hdr.mkv" }
	ids, errs := g.queueAdd([]json.RawMessage{itemSettings(a, out(a)), itemSettings(b, out(b)), itemSettings(c, out(c))})
	if len(ids) != 3 || len(errs) != 0 {
		t.Fatalf("add: ids=%v errs=%v", ids, errs)
	}
	if _, errs := g.queueAdd([]json.RawMessage{itemSettings(a, out(a))}); len(errs) != 1 {
		t.Fatal("the same file was added twice")
	}
	waitFor(t, "files to be checked", func() bool {
		s := strings.Join(statuses(g), " ")
		return !strings.Contains(s, stChecking)
	})
	g.mu.Lock()
	note := g.q.items[1].Note
	g.mu.Unlock()
	if !strings.Contains(note, "already exists") {
		t.Errorf("existing output not flagged when added: %q", note)
	}
	if err := g.startQueue(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the queue to finish", func() bool { g.mu.Lock(); defer g.mu.Unlock(); return !g.q.running })
	got := strings.Join(statuses(g), " ")
	if got != "a.mkv=done b.mkv=skipped bad.mkv=failed" {
		t.Fatalf("statuses: %s", got)
	}
	if b, _ := os.ReadFile(out(b)); string(b) != "earlier result" {
		t.Error("an existing output was overwritten")
	}
	if !fileExists(out(a)) || fileExists(partialName(out(a))) {
		t.Error("finished file not renamed from .partial")
	}
	if fileExists(out(c)) || fileExists(partialName(out(c))) {
		t.Error("a failed file left output behind")
	}
	g.mu.Lock()
	failNote := g.q.items[2].Note
	g.mu.Unlock()
	if failNote == "" {
		t.Error("failed file has no reason")
	}
	if log := g.queueLog(ids[2]); log["ok"] != true || len(log["lines"].([]string)) == 0 {
		t.Error("no log for the failed file")
	}
	// nothing unfinished is remembered
	var saved []json.RawMessage
	_ = json.Unmarshal(g.loadPrefs()["queue"], &saved)
	if len(saved) != 0 {
		t.Errorf("finished queue still saved: %d items", len(saved))
	}
}

func TestQueueOrderAndPersistence(t *testing.T) {
	dir := t.TempDir()
	var raws []json.RawMessage
	for _, n := range []string{"one", "two", "three"} {
		p := filepath.Join(dir, n+".mkv")
		_ = os.WriteFile(p, []byte("x"), 0o644)
		raws = append(raws, itemSettings(p, filepath.Join(dir, n+"_hdr.mkv")))
	}
	prefs := filepath.Join(t.TempDir(), "settings.json")
	g := newGUIServer()
	g.prefsPath = prefs
	ids, _ := g.queueAdd(raws)
	if err := g.queueMove(ids[2], -1); err != nil {
		t.Fatal(err)
	}
	if err := g.queueRemove(ids[0]); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond) // queueMove saves in the background
	g.saveQueue()
	names := func(g *guiServer) string {
		g.mu.Lock()
		defer g.mu.Unlock()
		var n []string
		for _, it := range g.q.items {
			n = append(n, it.Name)
		}
		return strings.Join(n, ",")
	}
	if got := names(g); got != "three.mkv,two.mkv" {
		t.Fatalf("order after move+remove: %s", got)
	}
	g2 := newGUIServer() // next run of the program
	g2.prefsPath = prefs
	g2.restoreQueue()
	if got := names(g2); got != "three.mkv,two.mkv" {
		t.Fatalf("restored queue: %q", got)
	}
	g2.mu.Lock()
	running := g2.q.running
	g2.mu.Unlock()
	if running {
		t.Fatal("a restored queue must not start by itself")
	}
}

func TestQueueApplySettings(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "film.mkv")
	_ = os.WriteFile(p, []byte("x"), 0o644)
	g := newGUIServer()
	g.prefsPath = filepath.Join(t.TempDir(), "settings.json")
	g.queueAdd([]json.RawMessage{itemSettings(p, filepath.Join(dir, "film_hdr.mkv"))})
	tmpl, _ := json.Marshal(map[string]any{"input": "other.mkv", "output": "x.mp4", "codec": "nvenc", "crf": 20,
		"preset": "slow", "mode": "encode", "quality": "crf", "audio": "aac", "dv": "auto"})
	if n, errs := g.queueApply(tmpl); n != 1 || len(errs) != 0 {
		t.Fatalf("apply: n=%d errs=%v", n, errs)
	}
	g.mu.Lock()
	it := g.q.items[0]
	g.mu.Unlock()
	if it.Input != p || it.Output != filepath.Join(dir, "film_hdr.mp4") || it.cfg.Codec != "nvenc" || it.cfg.Audio != "aac" {
		t.Fatalf("applied: input=%s output=%s codec=%s audio=%s", it.Input, it.Output, it.cfg.Codec, it.cfg.Audio)
	}
}

func TestBatchRemaining(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	g := newGUIServer()
	g.now = func() time.Time { return now }
	mkItem := func(status string, dur float64) *queueItem {
		return &queueItem{Status: status, Duration: dur, height: 2160, Output: "/nonexistent/x_hdr.mkv",
			cfg: &Config{Codec: "x265", Preset: "medium"}}
	}
	cur := mkItem(stRunning, 3600)
	g.q.items = []*queueItem{cur, mkItem(stWaiting, 3600), mkItem(stWaiting, 1800)}
	g.q.running, g.q.index, g.q.total = true, 1, 3
	j := &job{running: true, prog: newProgressTracker(3600)}
	g.job = j
	j.prog.stageLine("Encoding", now)
	g.mu.Lock()
	if got := g.remainingLocked(now); got != "estimating" {
		t.Errorf("at the start: %q", got)
	}
	g.mu.Unlock()
	now = now.Add(time.Hour)
	j.prog.frameLine(1800, now) // half of the first file in an hour: 0.5x
	g.mu.Lock()
	got := g.remainingLocked(now)
	title := g.titleLocked(now)
	g.mu.Unlock()
	// 1 h left of this file + 2 h + 1 h for the others = about 4 h
	if got != "about 4 h left in total" {
		t.Errorf("batch estimate: %q", got)
	}
	if !strings.HasPrefix(title, "File 1 of 3 · 50% · about 4 h left in total - ") {
		t.Errorf("title: %q", title)
	}
}

func TestAfterQueueCountdown(t *testing.T) {
	g := newGUIServer()
	var msg string
	done := make(chan struct{})
	g.notify = func(s string) { msg = s; close(done) }
	if err := g.setAfter("reboot"); err == nil {
		t.Fatal("unknown action accepted")
	}
	_ = g.setAfter("shutdown")
	old := powerDelay
	powerDelay = 50 * time.Millisecond
	defer func() { powerDelay = old }()

	// canceled in time: nothing happens
	g.mu.Lock()
	g.schedulePowerLocked()
	g.cancelPowerLocked()
	g.mu.Unlock()
	time.Sleep(150 * time.Millisecond)
	if msg != "" {
		t.Fatal("canceled shutdown still ran")
	}
	// not canceled: it runs (here it reports that it is not supported) and resets
	g.mu.Lock()
	g.schedulePowerLocked()
	g.mu.Unlock()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("countdown did not fire")
	}
	if runtime.GOOS != "windows" && !strings.Contains(msg, "shut down") {
		t.Errorf("message: %q", msg)
	}
	g.mu.Lock()
	after := g.q.after
	g.mu.Unlock()
	if after != "" {
		t.Error("the action must not carry over to the next queue")
	}
}

func TestQueueTotalFollowsChanges(t *testing.T) {
	dir := t.TempDir()
	g := newGUIServer()
	g.prefsPath = filepath.Join(t.TempDir(), "settings.json")
	g.q.running, g.q.total = true, 2 // a run in progress
	p := filepath.Join(dir, "late.mkv")
	_ = os.WriteFile(p, []byte("x"), 0o644)
	ids, _ := g.queueAdd([]json.RawMessage{itemSettings(p, filepath.Join(dir, "late_hdr.mkv"))})
	if g.q.total != 3 {
		t.Fatalf("total after adding during a run: %d", g.q.total)
	}
	_ = g.queueRemove(ids[0])
	if g.q.total != 2 {
		t.Fatalf("total after removing during a run: %d", g.q.total)
	}
}

func TestLastError(t *testing.T) {
	for _, c := range []struct {
		lines []string
		want  string
	}{
		{[]string{"Encoding", "Error: broken file", "ERROR: ffmpeg failed: exit status 1"}, "Error: broken file (ffmpeg)"},
		{[]string{"Source: x", "ERROR: dovi_tool not found. Put dovi_tool next to Keep the Light.exe"}, "dovi_tool not found. Put dovi_tool next to Keep the Light.exe"},
		{[]string{"Encoding", "ERROR: ffmpeg failed: exit status 1"}, "ffmpeg failed: exit status 1"},
		{[]string{"nothing useful"}, "Failed - see the log."},
	} {
		if got := lastError(c.lines); got != c.want {
			t.Errorf("got %q want %q", got, c.want)
		}
	}
}

func TestClosingDuringQueue(t *testing.T) {
	fakeTools(t)
	dir := t.TempDir()
	var raws []json.RawMessage
	for _, n := range []string{"slow", "next"} {
		p := filepath.Join(dir, n+".mkv")
		_ = os.WriteFile(p, []byte("x"), 0o644)
		raws = append(raws, itemSettings(p, filepath.Join(dir, n+"_hdr.mkv")))
	}
	g := newGUIServer()
	g.prefsPath = filepath.Join(t.TempDir(), "settings.json")
	g.queueAdd(raws)
	waitFor(t, "check", func() bool { return !strings.Contains(strings.Join(statuses(g), " "), stChecking) })
	if err := g.startQueue(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the slow file to start", func() bool { return strings.Contains(strings.Join(statuses(g), " "), "slow.mkv=running") })
	time.Sleep(300 * time.Millisecond)
	g.cancelAndWait(10 * time.Second) // the window closes
	got := strings.Join(statuses(g), " ")
	if got != "slow.mkv=waiting next.mkv=waiting" {
		t.Fatalf("after closing: %s (the next file must not start, the interrupted one must wait)", got)
	}
	var saved []json.RawMessage
	_ = json.Unmarshal(g.loadPrefs()["queue"], &saved)
	if len(saved) != 2 {
		t.Fatalf("remembered %d files, want 2", len(saved))
	}
	if fileExists(filepath.Join(dir, "slow_hdr.partial.mkv")) {
		t.Error("partial file left behind")
	}
}

func TestBatchOutputFolder(t *testing.T) {
	src, outDir := t.TempDir(), t.TempDir()
	p := filepath.Join(src, "film.mkv")
	_ = os.WriteFile(p, []byte("x"), 0o644)
	g := newGUIServer()
	g.prefsPath = filepath.Join(t.TempDir(), "settings.json")
	// an output folder that does not exist is refused when adding
	if _, errs := g.queueAdd([]json.RawMessage{itemSettings(p, filepath.Join(src, "gone", "film_hdr.mkv"))}); len(errs) != 1 ||
		!strings.Contains(errs[0], "output folder does not exist") {
		t.Fatalf("missing output folder: %v", errs)
	}
	g.queueAdd([]json.RawMessage{itemSettings(p, filepath.Join(src, "film_hdr.mkv"))})
	tmpl := func(dir string) json.RawMessage {
		b, _ := json.Marshal(map[string]any{"output": "x.mkv", "codec": "x265", "crf": 18, "preset": "medium", "mode": "encode",
			"quality": "crf", "audio": "copy", "dv": "auto", "outDir": dir})
		return b
	}
	out := func() string { g.mu.Lock(); defer g.mu.Unlock(); return g.q.items[0].Output }
	g.queueApply(tmpl(outDir)) // move waiting files to the chosen folder
	if out() != filepath.Join(outDir, "film_hdr.mkv") {
		t.Fatalf("to folder: %s", out())
	}
	g.queueApply(tmpl("")) // and back next to the original
	if out() != filepath.Join(src, "film_hdr.mkv") {
		t.Fatalf("back next to the original: %s", out())
	}
	// the remembered folder is reported when it has gone
	gone := filepath.Join(outDir, "removed")
	callJSON(t, g, "setPref", map[string]any{"name": "batchOut", "value": map[string]any{"mode": "dir", "dir": gone}})
	if m := callJSON(t, g, "prefs", nil); m["batchOutMissing"] != gone {
		t.Fatalf("missing batch folder not reported: %v", m)
	}
	callJSON(t, g, "setPref", map[string]any{"name": "batchOut", "value": map[string]any{"mode": "dir", "dir": outDir}})
	if m := callJSON(t, g, "prefs", nil); m["batchOutMissing"] != nil {
		t.Fatalf("existing batch folder reported missing: %v", m)
	}
}

func TestLowPriorityReachesTheTools(t *testing.T) {
	fakeTools(t)
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	// a stand-in ffmpeg that records its own priority ("nice" value) in the output file
	bin := filepath.Join(t.TempDir(), "ffmpeg")
	_ = os.WriteFile(bin, []byte(`#!/bin/sh
case "$*" in -version|--version) echo "fake 1.0"; exit 0;; *-encoders*) echo " V..... libx265 x"; exit 0;; *"-h full"*) echo "-fps_mode"; exit 0;; *"-h muxer"*) echo "cues_to_front"; exit 0;; esac
for a in "$@"; do last="$a"; done
awk '{print $19}' /proc/$$/stat > "$last"
`), 0o755)
	t.Setenv("PATH", filepath.Dir(bin)+string(os.PathListSeparator)+os.Getenv("PATH"))

	dir := t.TempDir()
	in := filepath.Join(dir, "film.mkv")
	_ = os.WriteFile(in, []byte("x"), 0o644)
	g := newGUIServer()
	g.prefsPath = filepath.Join(t.TempDir(), "settings.json")
	callJSON(t, g, "priority", map[string]any{"low": true})
	g.queueAdd([]json.RawMessage{itemSettings(in, filepath.Join(dir, "film_hdr.mkv"))})
	waitFor(t, "check", func() bool { return !strings.Contains(strings.Join(statuses(g), " "), stChecking) })
	if err := g.startQueue(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the queue to finish", func() bool { g.mu.Lock(); defer g.mu.Unlock(); return !g.q.running })
	got, _ := os.ReadFile(filepath.Join(dir, "film_hdr.mkv"))
	if strings.TrimSpace(string(got)) != "10" {
		t.Fatalf("ffmpeg ran with nice %q, want 10 (low priority); statuses %v", strings.TrimSpace(string(got)), statuses(g))
	}
	// remembered for the next run
	g2 := newGUIServer()
	g2.prefsPath = g.prefsPath
	g2.restoreQueue()
	if !g2.lowPrio {
		t.Fatal("low priority not remembered")
	}
}
