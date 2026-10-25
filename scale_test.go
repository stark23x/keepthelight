package main

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestFitSize(t *testing.T) {
	for _, c := range []struct{ w, h, bw, bh, ww, wh int }{
		{3840, 2160, 1920, 1080, 1920, 1080},
		{3840, 1600, 1920, 1080, 1920, 800}, // scope film: keeps its shape
		{3840, 1608, 1920, 1080, 1920, 804},
		{4096, 2160, 1920, 1080, 1920, 1012}, // DCI 4K: limited by the width, even height
		{3840, 2160, 1280, 720, 1280, 720},
		{1440, 1080, 1280, 720, 960, 720}, // 4:3: limited by the height
	} {
		w, h, ok := fitSize(c.w, c.h, c.bw, c.bh)
		if !ok || w != c.ww || h != c.wh {
			t.Errorf("%dx%d in %dx%d: got %dx%d %v, want %dx%d", c.w, c.h, c.bw, c.bh, w, h, ok, c.ww, c.wh)
		}
	}
	if _, _, ok := fitSize(1920, 1080, 1920, 1080); ok {
		t.Error("a video that already fits was resized")
	}
	if _, _, ok := fitSize(1280, 720, 1920, 1080); ok {
		t.Error("a smaller video was enlarged")
	}
}

func TestNormScale(t *testing.T) {
	for in, want := range map[string]string{"": "", "1080": "1080p", "1080P": "1080p", " 720p ": "720p", "original": ""} {
		if got, ok := normScale(in); !ok || got != want {
			t.Errorf("%q: got %q %v", in, got, ok)
		}
	}
	for _, bad := range []string{"480p", "big", "1080i"} {
		if _, ok := normScale(bad); ok {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestScalePlanAndArgs(t *testing.T) {
	p, _ := parseProbe([]byte(sampleProbe)) // 3840x2160 HDR10 + Dolby Vision 8.1
	cfg := baseCfg()
	cfg.Scale = "1080p"
	plan, err := makePlan(cfg, p)
	if err != nil {
		t.Fatal(err)
	}
	if plan.ScaleW != 1920 || plan.ScaleH != 1080 || !plan.UseDV {
		t.Fatalf("plan: %+v", plan)
	}
	if m := describeMode(cfg, plan); !strings.Contains(m, "Dolby Vision 8.1") || !strings.Contains(m, "3840x2160 -> 1920x1080") {
		t.Errorf("mode: %s", m)
	}
	for _, codec := range []string{"x265", "nvenc", "qsv"} {
		cfg.Codec = codec
		args, _ := buildVideoArgs(cfg, p, plan, "")
		j := strings.Join(args, " ")
		if !strings.Contains(j, "-vf scale=1920:1080:flags=lanczos") || !strings.Contains(j, "-color_trc smpte2084") {
			t.Errorf("%s: %s", codec, j)
		}
	}
	cfg.Codec = "x265"
	args, _ := buildVideoArgs(cfg, p, plan, "")
	if j := strings.Join(args, " "); !strings.Contains(j, "master-display=G(8500,39850)") || !strings.Contains(j, "max-cll=1000,400") {
		t.Errorf("HDR10 metadata lost when resizing: %s", j)
	}

	cfg.Scale = "2160p" // already that size: not resized, with a note
	if plan, _ := makePlan(cfg, p); plan.ScaleW != 0 || !strings.Contains(strings.Join(plan.Warnings, " "), "not resized") {
		t.Errorf("2160p of a 2160p source: %+v", plan)
	}
	cfg.Scale, cfg.CopyVideo = "1080p", true
	if _, err := makePlan(cfg, p); err == nil {
		t.Error("resize with copy video accepted")
	}
}

func TestScaleL5(t *testing.T) {
	// dovi_tool 2.3.4 "export -d level5=" output: a scope scene, a full-frame scene, a pillarboxed one
	exported := `{"mode":0,"remove_cmv4":false,"remove_mapping":false,"active_area":{"crop":true,
	 "presets":[{"id":0,"left":0,"right":0,"top":276,"bottom":276},{"id":1,"left":0,"right":0,"top":0,"bottom":0},
	            {"id":2,"left":240,"right":240,"top":0,"bottom":0}],
	 "edits":{"0-39":0,"40-69":1,"70-99":2}}}`
	out, changed, err := scaleL5([]byte(exported), 3840, 2160, 1920, 1080)
	if err != nil || !changed {
		t.Fatal(changed, err)
	}
	var got struct {
		ActiveArea struct {
			Crop    bool
			Presets []l5Preset
			Edits   map[string]int
		} `json:"active_area"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	aa := got.ActiveArea
	want := []l5Preset{{0, 0, 0, 138, 138}, {1, 0, 0, 0, 0}, {2, 120, 120, 0, 0}}
	if aa.Crop || len(aa.Presets) != 3 || aa.Edits["40-69"] != 1 || len(aa.Edits) != 3 {
		t.Fatalf("config: %s", out)
	}
	for i := range want {
		if aa.Presets[i] != want[i] {
			t.Errorf("preset %d: got %+v want %+v", i, aa.Presets[i], want[i])
		}
	}
	if strings.Contains(string(out), `"mode"`) {
		t.Error("the editor config must not convert the RPU again")
	}
	// odd sizes round to the nearest pixel
	out, _, _ = scaleL5([]byte(exported), 3840, 2160, 1280, 720)
	if !strings.Contains(string(out), `"top": 92`) || !strings.Contains(string(out), `"left": 80`) {
		t.Errorf("720p: %s", out)
	}
	// no bars: nothing to edit
	none := `{"active_area":{"crop":true,"presets":[{"id":0,"left":0,"right":0,"top":0,"bottom":0}],"edits":{"0-9":0}}}`
	if _, changed, err := scaleL5([]byte(none), 3840, 2160, 1920, 1080); changed || err != nil {
		t.Errorf("no bars: changed=%v err=%v", changed, err)
	}
	if _, _, err := scaleL5([]byte("{bad"), 3840, 2160, 1920, 1080); err == nil {
		t.Error("bad JSON accepted")
	}
}

func TestHWDecodeProblem(t *testing.T) {
	for _, c := range []struct {
		out  string
		err  error
		want string
	}{
		{"", nil, ""},
		{"[hevc @ 0x1] Application provided invalid, non monotonically increasing dts\n", nil, ""}, // unrelated warning
		{"[hevc @ 0x1] Failed setup for format d3d11: hwaccel initialisation returned error.\n", nil, "Failed setup for format d3d11"},
		{"Device creation failed: -1.\n[vist#0:0] Hardware device setup failed for decoder\n", errors.New("exit status 1"), "Device creation failed"},
		{"something else went wrong\n", errors.New("exit status 1"), "something else went wrong"},
	} {
		got := hwDecodeProblem(c.out, c.err)
		if (c.want == "") != (got == "") || !strings.Contains(got, c.want) {
			t.Errorf("%q: got %q, want %q", c.out, got, c.want)
		}
	}
}

func TestScaleAndHWDecodeSettings(t *testing.T) {
	s := &settings{Input: "a.mkv", Output: "b.mkv", Mode: "encode", Quality: "crf", CRF: 18, Scale: "1080"}
	cfg, err := s.toConfig()
	if err != nil || cfg.Scale != "1080p" {
		t.Fatalf("%v %+v", err, cfg)
	}
	cfg.HWDecode = true
	a := strings.Join(cfg.cliArgs(), " ")
	if !strings.Contains(a, "-scale 1080p") || !strings.Contains(a, "-hw-decode") {
		t.Errorf("cli args: %s", a)
	}
	s.Scale = "480p"
	if _, err := s.toConfig(); err == nil {
		t.Error("unknown resolution accepted")
	}
	s.Scale, s.Mode = "1080p", "copy" // copying ignores the size
	if cfg, err := s.toConfig(); err != nil || cfg.Scale != "" {
		t.Errorf("copy: %v %+v", err, cfg)
	}

	// GPU decoding is remembered and reaches the next transcode
	g := newGUIServer()
	g.prefsPath = filepath.Join(t.TempDir(), "settings.json")
	callJSON(t, g, "hwdecode", map[string]any{"on": true})
	g2 := newGUIServer()
	g2.prefsPath = g.prefsPath
	g2.restoreQueue()
	if !g2.hwDec {
		t.Fatal("GPU decoding not remembered")
	}
	if m := callJSON(t, g2, "prefs", nil); m["prefs"].(map[string]any)["hwDecode"] != true {
		t.Errorf("prefs: %v", m)
	}
}
