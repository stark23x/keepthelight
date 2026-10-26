package main

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func guideFor(t *testing.T, codec string, w, h int, srcMbps, outMbps float64, enc string, hdr bool) *QualityGuide {
	t.Helper()
	p := &Probe{Codec: codec, Width: w, Height: h, AvgFPS: "24000/1001", VideoBps: srcMbps * 1e6}
	cfg := baseCfg()
	cfg.Codec = enc
	plan := &Plan{Kind: map[bool]string{true: "hdr10", false: "sdr"}[hdr]}
	return bitrateGuide(outMbps*1e6, cfg, p, plan)
}

func TestBitrateGuide(t *testing.T) {
	// the example: 50 Mbps down to 10 Mbps
	g4k := guideFor(t, "hevc", 3840, 2160, 50, 10, "x265", true)
	g1080 := guideFor(t, "h264", 1920, 1080, 50, 10, "x265", false)
	t.Logf("4K HDR:   %s | %s", g4k.Text, g4k.Warn)
	t.Logf("1080p:    %s | %s", g1080.Text, g1080.Warn)
	if g4k.Level != "good" || g4k.Warn != "" {
		t.Errorf("4K HDR HEVC at 10 Mbps: %+v", g4k)
	}
	if g1080.Level != "excellent" || g1080.Warn != "" {
		t.Errorf("1080p from H.264 at 10 Mbps: %+v", g1080)
	}
	if g := guideFor(t, "hevc", 3840, 2160, 50, 3, "x265", true); g.Level != "verylow" || g.Warn == "" {
		t.Errorf("4K at 3 Mbps must warn: %+v", g)
	}
	if g := guideFor(t, "hevc", 3840, 2160, 50, 10, "nvenc", true); g.Level != "fair" || !strings.Contains(g.Text, "GPU encoders") {
		t.Errorf("the same with NVENC is a step lower: %+v", g)
	}
	if g := guideFor(t, "h264", 1920, 1080, 20, 30, "x265", false); !strings.Contains(g.Warn, "more than this source needs") {
		t.Errorf("more than the source: %+v", g)
	}
	cfg := baseCfg()
	if g := crfGuide(cfg, true); g.Level != "excellent" || g.Warn != "" { // CRF 18
		t.Errorf("CRF 18: %+v", g)
	}
	cfg.CRF = 30
	if g := crfGuide(cfg, true); g.Level != "low" || g.Warn == "" {
		t.Errorf("CRF 30: %+v", g)
	}
}

func TestScoreTest(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("needs ffmpeg")
	}
	dir := t.TempDir()
	orig, good, bad := filepath.Join(dir, "o.mkv"), filepath.Join(dir, "g.mkv"), filepath.Join(dir, "b.mkv")
	run := func(args ...string) {
		if b, err := exec.Command(ffmpeg, append([]string{"-v", "error", "-y"}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("%v %s", err, b)
		}
	}
	run("-f", "lavfi", "-i", "testsrc2=s=640x360:r=24:d=2", "-c:v", "ffv1", orig)
	run("-i", orig, "-c:v", "libx265", "-x265-params", "log-level=error", "-crf", "16", "-pix_fmt", "yuv420p10le", good)
	run("-i", orig, "-c:v", "libx265", "-x265-params", "log-level=error", "-crf", "45", "-pix_fmt", "yuv420p10le", bad)
	sg, err1 := scoreTest(context.Background(), ffmpeg, good, orig, 640, 360, "")
	sb, err2 := scoreTest(context.Background(), ffmpeg, bad, orig, 640, 360, "light")
	if err1 != nil || err2 != nil {
		t.Fatal(err1, err2)
	}
	t.Log(sg)
	t.Log(sb)
	if !strings.HasPrefix(sg, "Test quality: ") || sg == sb || !strings.Contains(sb, "Noise reduction lowers the score") {
		t.Errorf("%q / %q", sg, sb)
	}
}
