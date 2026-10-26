package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDenoiseArgs(t *testing.T) {
	p, _ := parseProbe([]byte(sampleProbe))
	cfg := baseCfg()
	cfg.Denoise, cfg.Scale = "medium", "1080p"
	plan, _ := makePlan(cfg, p)
	args := strings.Join(func() []string { a, _ := buildVideoArgs(cfg, p, plan, ""); return a }(), " ")
	if !strings.Contains(args, "-vf scale=1920:1080:flags=lanczos,hqdn3d=4:3:6:4.5") || !strings.Contains(args, "yuv420p10le") {
		t.Errorf("resize, then noise reduction, 10-bit: %s", args)
	}
	cfg.Denoise, cfg.Scale, plan.ScaleW = "hq", "", 0
	cfg.hqDenoise = &hqCandidates()[0] // as if the Vulkan test had worked
	a, _ := buildVideoArgs(cfg, p, plan, "")
	if j := strings.Join(a, " "); !strings.Contains(j, "-vf format=p010,hwupload,nlmeans_vulkan") || !strings.Contains(j, "hwdownload,format=p010") {
		t.Errorf("GPU: %s", j)
	}
	if pre := strings.Join(cfg.inputPrefix(), " "); pre != "-init_hw_device vulkan=ktlvk -filter_hw_device ktlvk" {
		t.Errorf("GPU device: %s", pre)
	}
	for _, c := range hqCandidates() {
		if strings.Contains(c.Filter, "nlmeans=") || strings.Contains(c.Filter, "dctdnoiz") { // these turn 10-bit into 8-bit
			t.Errorf("an 8-bit filter is offered: %s", c.Filter)
		}
	}
	if !validDenoise("") || !validDenoise("hq") || validDenoise("extreme") {
		t.Error("validDenoise")
	}
}

func TestTestTimes(t *testing.T) {
	for in, want := range map[string]float64{"": -1, "90": 90, "1:30": 90, "1:02:30": 3750, " 2:00 ": 120} {
		if got, err := parseTimeArg(in); err != nil || got != want {
			t.Errorf("%q: %v %v", in, got, err)
		}
	}
	for _, bad := range []string{"x", "1:2:3:4", "-5"} {
		if _, err := parseTimeArg(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if s := sampleStart(-1, 7200, 30); s != 2880 { // 40% in
		t.Errorf("auto: %v", s)
	}
	if s := sampleStart(7190, 7200, 30); s != 7170 { // always room for the whole test
		t.Errorf("near the end: %v", s)
	}
	if testOutputName(`D:\Films\Film_KTL.mkv`, 30) != `D:\Films\Film_KTL (30 s test).mkv` {
		t.Error(testOutputName(`D:\Films\Film_KTL.mkv`, 30))
	}
	s := &settings{Input: "a.mkv", Output: "/x/a_KTL.mkv", Mode: "encode", Quality: "crf", CRF: 18, Denoise: "light", TestSeconds: 30, TestStart: "1:00"}
	cfg, err := s.toConfig()
	if err != nil || cfg.Output != "/x/a_KTL (30 s test).mkv" {
		t.Fatalf("%v %+v", err, cfg)
	}
	if a := strings.Join(cfg.cliArgs(), " "); !strings.Contains(a, "-denoise light") || !strings.Contains(a, "-sample 30 -sample-start 60") {
		t.Errorf("cli: %s", a)
	}
	s.Mode = "copy"
	if _, err := s.toConfig(); err == nil {
		t.Error("a test of a copy accepted")
	}
}

func TestTestRunsAlone(t *testing.T) {
	fakeTools(t)
	dir := t.TempDir()
	g := newGUIServer()
	g.prefsPath = filepath.Join(t.TempDir(), "settings.json")
	waiting, film := filepath.Join(dir, "waiting.mkv"), filepath.Join(dir, "film.mkv")
	for _, p := range []string{waiting, film} {
		_ = os.WriteFile(p, []byte("x"), 0o644)
	}
	g.queueAdd([]json.RawMessage{itemSettings(waiting, filepath.Join(dir, "waiting_KTL.mkv"))})
	raw := itemWith(film, filepath.Join(dir, "film_KTL.mkv"), map[string]any{"testSeconds": 30})
	if r := g.startTest(raw); r["ok"] != true {
		t.Fatalf("%v", r)
	}
	waitFor(t, "the test", func() bool { g.mu.Lock(); defer g.mu.Unlock(); return !g.q.running })
	s := statuses(g)
	if s[0] != "film.mkv="+stDone || s[1] != "waiting.mkv="+stWaiting || !fileExists(filepath.Join(dir, "film_KTL (30 s test).mkv")) {
		t.Fatalf("%v", s)
	}
}
