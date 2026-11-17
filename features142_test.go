package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The report sorts a job's log into warnings, things done another way, and the thorough check.
func TestJobFindings(t *testing.T) {
	w, f, c := jobFindings([]string{
		"Source: hevc",
		"WARNING: dropping Dolby Vision: output will be plain hdr10",
		"WARNING: the audio track has no language",
		"WARNING: the audio track has no language",
		"The GPU could not do this file. Trying again the normal way.",
		"Thorough check: passed (1,000 frames and all sound decode without errors)",
	})
	if len(w) != 2 || !strings.Contains(w[1], "no language") {
		t.Errorf("warnings: %q", w)
	}
	if len(f) != 1 {
		t.Errorf("fallbacks: %q", f)
	}
	if !strings.HasPrefix(c, "Thorough check: passed") {
		t.Errorf("check: %q", c)
	}
	w, _, c = jobFindings([]string{"WARNING: Thorough check: PROBLEMS FOUND: the picture stops early"})
	if len(w) != 1 || !strings.Contains(c, "PROBLEMS FOUND") {
		t.Errorf("a failed check is a warning: %q %q", w, c)
	}
}

func TestReportPage(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	rows := []reportRow{
		{Name: "A <b>.mkv", Input: "/in/A.mkv", Output: "/out/A_KTL.mkv", Status: stDone, InSize: 10e9, OutSize: 4e9, Took: 3700,
			Settings: "x265 · CRF 18 · medium", Warnings: []string{"something to look at"}, Fallbacks: []string{"decoded on the processor"}},
		{Name: "B.mkv", Status: stFailed, Note: "ffmpeg failed", Took: 20},
	}
	page := reportHTML(rows, time.Date(2026, 1, 2, 20, 0, 0, 0, time.UTC), time.Date(2026, 1, 2, 21, 2, 0, 0, time.UTC))
	for _, want := range []string{"A &lt;b&gt;.mkv", "saved (60%)", "1 h 01 min", "Done, with warnings", "Done another way: decoded on the processor",
		"⚠ something to look at", "Failed", "ffmpeg failed", "x265 · CRF 18 · medium"} {
		if !strings.Contains(page, want) {
			t.Errorf("the report lacks %q", want)
		}
	}
	// only the newest reports are kept
	var last string
	for i := 0; i < keepReports+3; i++ {
		last = writeReport(rows, time.Now(), time.Date(2026, 1, 2, 21, 0, i, 0, time.UTC))
	}
	m, _ := filepath.Glob(filepath.Join(reportsDir(), "*.html"))
	if len(m) != keepReports || !fileExists(last) {
		t.Errorf("%d reports kept, want %d (newest there: %v)", len(m), keepReports, fileExists(last))
	}
}

func TestTidyPairsIn(t *testing.T) {
	names := []string{"Film.mkv", "Film_KTL.mkv", "Film.en.srt", "Other_KTL.mkv", "Two.mkv", "Two.mp4", "Two_KTL.mkv",
		"Old.avi", "old_hdr.mp4", "Show S01E01.mkv", "Show S01E01-x.mkv", "_KTL.mkv", "Film_KTL (30 s test).mkv"}
	got := tidyPairsIn("d", names, "-x")
	want := map[string]string{"Film.mkv": "Film_KTL.mkv", "Old.avi": "old_hdr.mp4", "Show S01E01.mkv": "Show S01E01-x.mkv"}
	if len(got) != len(want) {
		t.Fatalf("pairs: %v", got)
	}
	for _, p := range got {
		if want[filepath.Base(p[0])] != filepath.Base(p[1]) {
			t.Errorf("unexpected pair %v", p)
		}
	}
}

func makeClip(t *testing.T, ffmpeg, path string, secs string, audio bool) {
	t.Helper()
	args := []string{"-v", "error", "-y", "-f", "lavfi", "-i", "testsrc2=s=160x120:r=25:d=" + secs}
	if audio {
		args = append(args, "-f", "lavfi", "-i", "sine=d="+secs, "-c:a", "aac")
	}
	args = append(args, "-c:v", "mpeg4", path)
	if b, err := exec.Command(ffmpeg, args...).CombinedOutput(); err != nil {
		t.Fatal(err, string(b))
	}
}

// Tidying up: only originals with a whole copy are moved, never deleted, and the renames can be undone.
func TestTidyApplyAndUndo(t *testing.T) {
	ffmpeg, e1 := exec.LookPath("ffmpeg")
	_, e2 := exec.LookPath("ffprobe")
	if e1 != nil || e2 != nil {
		t.Skip("ffmpeg/ffprobe not found")
	}
	t.Setenv("LOCALAPPDATA", t.TempDir())
	dir := t.TempDir()
	p := func(n string) string { return filepath.Join(dir, n) }
	makeClip(t, ffmpeg, p("Good.mkv"), "4", true)
	makeClip(t, ffmpeg, p("Good_KTL.mkv"), "4", true)
	makeClip(t, ffmpeg, p("Short.mkv"), "8", true)
	makeClip(t, ffmpeg, p("Short_KTL.mkv"), "4", true) // the copy stops early
	makeClip(t, ffmpeg, p("Mute.mkv"), "4", true)
	makeClip(t, ffmpeg, p("Mute_KTL.mkv"), "4", false) // the copy lost its sound
	_ = os.WriteFile(p("Good.en.srt"), []byte("1\n00:00:01,000 --> 00:00:02,000\nHi\n"), 0o644)

	g := &guiServer{}
	if r := g.tidyFind(dir, false, "_KTL"); r["ok"] != true {
		t.Fatal(r)
	}
	var st map[string]any
	for i := 0; i < 200; i++ {
		if st = g.tidyStatus(); st["running"] == false {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	pairs := st["pairs"].([]tidyPair)
	if len(pairs) != 3 {
		t.Fatalf("pairs: %+v", pairs)
	}
	var all []string
	for _, x := range pairs {
		all = append(all, x.Original)
		if ok := x.Name == "Good.mkv"; x.OK != ok {
			t.Errorf("%s: ok=%v (%s)", x.Name, x.OK, x.Why)
		}
	}
	r := g.tidyApply(append(all, p("NotAPair.mkv")))
	if r["ok"] != true || r["renamed"] != 1 || r["binned"] != 0 {
		t.Fatalf("apply: %v", r)
	}
	for _, f := range []string{"Good (original).mkv", "Good_KTL.mkv", "Good_KTL.en.srt", "Short.mkv", "Mute.mkv"} {
		if !fileExists(p(f)) {
			t.Errorf("%s should be there", f)
		}
	}
	if fileExists(p("Good.mkv")) || fileExists(p("Good.en.srt")) {
		t.Error("the original and its subtitle file should have been renamed")
	}
	if u := g.tidyUndo(); u["ok"] != true || u["restored"] != 1 {
		t.Fatalf("undo: %v", u)
	}
	if !fileExists(p("Good.mkv")) || !fileExists(p("Good.en.srt")) || fileExists(p("Good (original).mkv")) {
		t.Error("undo did not put the names back")
	}
}

// The thorough check passes a whole file and notices one that is cut short.
func TestDeepCheck(t *testing.T) {
	ffmpeg, e1 := exec.LookPath("ffmpeg")
	ffprobe, e2 := exec.LookPath("ffprobe")
	if e1 != nil || e2 != nil {
		t.Skip("ffmpeg/ffprobe not found")
	}
	dir := t.TempDir()
	src, good, short := filepath.Join(dir, "src.mkv"), filepath.Join(dir, "good.mkv"), filepath.Join(dir, "short.mkv")
	makeClip(t, ffmpeg, src, "8", true)
	makeClip(t, ffmpeg, good, "8", true)
	makeClip(t, ffmpeg, short, "4", true)
	cfg := &Config{Ffmpeg: ffmpeg, Ffprobe: ffprobe}
	sp, err := probeFile(context.Background(), ffprobe, src)
	if err != nil {
		t.Fatal(err)
	}
	res := deepCheck(context.Background(), cfg, &Plan{}, sp, good, dir)
	if len(res.Problems) > 0 || res.Frames != 200 || !strings.HasPrefix(res.line(), "Thorough check: passed (200 frames") {
		t.Errorf("a good file: %+v", res)
	}
	res = deepCheck(context.Background(), cfg, &Plan{}, sp, short, dir)
	if len(res.Problems) == 0 || !strings.Contains(res.line(), "PROBLEMS FOUND") {
		t.Errorf("a short file passed: %+v", res)
	}
	// a file cut off in the middle
	b, _ := os.ReadFile(good)
	cut := filepath.Join(dir, "cut.mkv")
	_ = os.WriteFile(cut, b[:len(b)/2], 0o644)
	if res = deepCheck(context.Background(), cfg, &Plan{}, sp, cut, dir); len(res.Problems) == 0 {
		t.Errorf("a cut-off file passed: %+v", res)
	}
}

// Advice and "aim for quality" travel from the page's settings to the job's command line.
func TestQualityAimSettings(t *testing.T) {
	base := map[string]any{"input": "in.mkv", "output": "out.mkv", "mode": "encode", "codec": "x265", "quality": "vmaf", "qualityTarget": 95,
		"crf": 18, "preset": "medium", "audio": "copy", "dv": "auto"}
	conf := func(m map[string]any) (*Config, error) {
		var s settings
		b, _ := json.Marshal(m)
		if err := json.Unmarshal(b, &s); err != nil {
			t.Fatal(err)
		}
		return s.toConfig()
	}
	cfg, err := conf(base)
	if err != nil || cfg.QualityTarget != 95 {
		t.Fatalf("aim: %v %+v", err, cfg)
	}
	if a := strings.Join(cfg.cliArgs(), " "); !strings.Contains(a, "-quality-target 95") {
		t.Errorf("command line: %s", a)
	}
	base["qualityTarget"] = 50
	if _, err := conf(base); err == nil {
		t.Error("a target of 50 should be refused")
	}
	base["advice"], base["quality"] = true, "size"
	cfg, err = conf(base)
	if err != nil || !cfg.QualityAdvice || cfg.TargetBytes != 0 || cfg.Output != "out (advice).mkv" {
		t.Fatalf("advice: %v %+v", err, cfg)
	}
	if a := strings.Join(cfg.cliArgs(), " "); !strings.Contains(a, "-quality-advice") {
		t.Errorf("command line: %s", a)
	}
	base["mode"] = "copy"
	if _, err := conf(base); err == nil {
		t.Error("advice with copy video should be refused")
	}
	raw := withKeys(json.RawMessage(`{"input":"a","Overwrite":false,"crf":20}`), map[string]any{"overwrite": true})
	if s := string(raw); strings.Contains(s, "Overwrite") || !strings.Contains(s, `"overwrite":true`) || !strings.Contains(s, `"crf":20`) {
		t.Errorf("withKeys: %s", s)
	}
}
