package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestGPUPathArgs(t *testing.T) {
	p, _ := parseProbe([]byte(sampleProbe))
	p.DV = nil
	cfg := baseCfg()
	cfg.Codec, cfg.Scale = "nvenc", "1080p"
	plan, _ := makePlan(cfg, p)
	cfg.gpu = gpuPathFor("nvenc")
	a, _ := buildVideoArgs(cfg, p, plan, "")
	j := strings.Join(a, " ")
	if !strings.Contains(j, "-vf scale_cuda=w=1920:h=1080:interp_algo=lanczos:format=p010le") || strings.Contains(j, "-pix_fmt") || !strings.Contains(j, "hevc_nvenc") {
		t.Errorf("NVENC on the GPU: %s", j)
	}
	if pre := strings.Join(cfg.inputPrefix(), " "); pre != "-hwaccel cuda -hwaccel_output_format cuda" {
		t.Errorf("input: %s", pre)
	}
	cfg.Codec, cfg.Scale, plan.ScaleW, plan.ScaleH = "qsv", "", 0, 0
	cfg.gpu = gpuPathFor("qsv")
	a, _ = buildVideoArgs(cfg, p, plan, "")
	if j = strings.Join(a, " "); !strings.Contains(j, "-vf vpp_qsv=format=p010") || strings.Contains(j, "-pix_fmt") {
		t.Errorf("QuickSync on the GPU: %s", j)
	}
	cfg.gpu = nil // the normal way: 10-bit conversion on the CPU, as before
	a, _ = buildVideoArgs(cfg, p, plan, "")
	if j = strings.Join(a, " "); !strings.Contains(j, "-pix_fmt p010le") || strings.Contains(j, "vpp_qsv") {
		t.Errorf("normal path: %s", j)
	}
	if gpuPathFor("x265") != nil {
		t.Error("x265 has no GPU path")
	}
	cfg.Denoise, cfg.Ffmpeg, cfg.Codec = "light", "ffmpeg", "nvenc"
	if g, msg := chooseGPUPath(context.Background(), cfg, plan); g != nil || !strings.Contains(msg, "noise reduction") {
		t.Errorf("noise reduction keeps the normal path: %v %s", g, msg)
	}
}

func TestEncoderProblem(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script")
	}
	dir := t.TempDir()
	fake := filepath.Join(dir, "ffmpeg")
	// NVENC fails the way it does without an NVIDIA card; QuickSync works
	_ = os.WriteFile(fake, []byte(`#!/bin/sh
case "$*" in *hevc_nvenc*) echo "[hevc_nvenc @ 0x1] Cannot load nvcuda.dll" >&2; echo "[vost#0:0/hevc_nvenc @ 0x2] Error while opening encoder" >&2; exit 1;; esac
exit 0
`), 0o755)
	encoderChecks.Clear()
	t.Cleanup(encoderChecks.Clear)
	p := encoderProblem(context.Background(), fake, "nvenc")
	if !strings.Contains(p, "NVENC needs an NVIDIA graphics card") || !strings.Contains(p, "Cannot load nvcuda.dll") || !strings.Contains(p, "Choose x265 or QuickSync") {
		t.Errorf("nvenc: %q", p)
	}
	if p := encoderProblem(context.Background(), fake, "qsv"); p != "" {
		t.Errorf("qsv: %q", p)
	}
	if p := encoderProblem(context.Background(), fake, "x265"); p != "" {
		t.Errorf("x265: %q", p)
	}
}

func TestSpaceSaved(t *testing.T) {
	if s := savedText(20<<30, 9<<30); s != "20.00 GB → 9.00 GB · saved 11.00 GB (55%)" && !strings.Contains(s, "saved") {
		t.Errorf("%s", s)
	}
	if s := savedText(5<<30, 6<<30); !strings.Contains(s, "bigger than the original") {
		t.Errorf("%s", s)
	}
	fakeTools(t) // its ffmpeg writes "done\n" (5 bytes) as the result
	g := newGUIServer()
	g.prefsPath = filepath.Join(t.TempDir(), "settings.json")
	dir := t.TempDir()
	var raws []json.RawMessage
	for _, n := range []string{"a", "b"} {
		p := filepath.Join(dir, n+".mkv")
		_ = os.WriteFile(p, make([]byte, 105), 0o644) // 105 bytes in, 5 out: 100 saved each
		raws = append(raws, itemSettings(p, filepath.Join(dir, n+"_KTL.mkv")))
	}
	g.queueAdd(raws)
	waitFor(t, "check", func() bool { return !strings.Contains(strings.Join(statuses(g), " "), stChecking) })
	if err := g.startQueue(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the queue", func() bool { g.mu.Lock(); defer g.mu.Unlock(); return !g.q.running })
	g.mu.Lock()
	run, total, note := g.q.savedRun, g.savedTotal, g.q.items[0].Note
	g.mu.Unlock()
	if run != 200 || total != 200 || !strings.Contains(note, "saved") {
		t.Fatalf("run %d total %d note %q", run, total, note)
	}
	g2 := newGUIServer() // the running total is remembered
	g2.prefsPath = g.prefsPath
	g2.restoreQueue()
	if g2.savedTotal != 200 {
		t.Errorf("total after restart: %d", g2.savedTotal)
	}
}
