package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLooksGPUStuck(t *testing.T) {
	for _, s := range []string{
		"[hevc_nvenc @ 0x1] OpenEncodeSessionEx failed: no encode device (1): (no details)",
		"[hevc_qsv @ 0x2] Error creating a MFX session: -9.",
	} {
		if !looksGPUStuck(s) {
			t.Errorf("not seen: %q", s)
		}
	}
	for _, s := range []string{
		"[hevc_nvenc @ 0x1] Error while opening encoder - maybe incorrect parameters such as bit_rate, rate, width or height.",
		"Cannot load nvcuda.dll", // no NVIDIA card or driver: not a stuck card
		"No capable devices found",
	} {
		if looksGPUStuck(s) {
			t.Errorf("wrongly seen: %q", s)
		}
	}
	if hwEncoderIn([]string{"-c:v", "hevc_nvenc"}) != "nvenc" || hwEncoderIn([]string{"-c:v", "hevc_qsv"}) != "qsv" || hwEncoderIn([]string{"-c:v", "libx265"}) != "" {
		t.Error("hwEncoderIn")
	}
}

func TestWithGPURetry(t *testing.T) {
	old := gpuRetryWait
	gpuRetryWait = 0
	t.Cleanup(func() { gpuRetryWait = old })
	fail := errors.New("ffmpeg failed")
	stuckStep := func(n *int, okOn int) func(io.Writer) error {
		return func(w io.Writer) error {
			*n++
			if *n == okOn {
				return nil
			}
			_, _ = io.WriteString(w, "[hevc_nvenc @ 0x1] OpenEncodeSessionEx ")
			_, _ = io.WriteString(w, "failed: no encode device (1)\n") // split across writes
			return fail
		}
	}
	var n int
	err := withGPURetry(context.Background(), "nvenc", stuckStep(&n, 0), io.Discard)
	var stuck *gpuStuckError
	if !errors.As(err, &stuck) || n != 2 || !strings.Contains(err.Error(), "Restarting Windows") {
		t.Errorf("twice stuck: n=%d err=%v", n, err)
	}
	n = 0
	if err := withGPURetry(context.Background(), "nvenc", stuckStep(&n, 2), io.Discard); err != nil || n != 2 {
		t.Errorf("stuck then fine: n=%d err=%v", n, err)
	}
	n = 0
	if err := withGPURetry(context.Background(), "", stuckStep(&n, 0), io.Discard); err != fail || n != 1 {
		t.Errorf("no hardware encoder: no retry: n=%d err=%v", n, err)
	}
	n = 0
	plain := func(io.Writer) error { n++; return fail }
	if err := withGPURetry(context.Background(), "nvenc", plain, io.Discard); err != fail || n != 1 {
		t.Errorf("other failure: no retry: n=%d err=%v", n, err)
	}
}

// The start-up check: a card that does not answer is checked again, then reported as stuck.
func TestCheckEncoderStuck(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script")
	}
	old := gpuRetryWait
	gpuRetryWait = 0
	t.Cleanup(func() { gpuRetryWait = old; encoderChecks.Clear(); encoderStuck.Clear() })
	dir := t.TempDir()
	fake := filepath.Join(dir, "ffmpeg")
	_ = os.WriteFile(fake, []byte("#!/bin/sh\necho '[hevc_nvenc @ 0x1] OpenEncodeSessionEx failed: no encode device (1)' >&2\nexit 1\n"), 0o755)
	encoderChecks.Clear()
	encoderStuck.Clear()
	var stuck *gpuStuckError
	if err := checkEncoder(context.Background(), fake, "nvenc"); !errors.As(err, &stuck) {
		t.Errorf("stuck card: %v", err)
	}
	_ = os.WriteFile(fake, []byte("#!/bin/sh\necho 'Cannot load nvcuda.dll' >&2\nexit 1\n"), 0o755)
	encoderChecks.Clear()
	encoderStuck.Clear()
	if err := checkEncoder(context.Background(), fake, "nvenc"); err == nil || errors.As(err, &stuck) || !strings.Contains(err.Error(), "NVENC needs") {
		t.Errorf("no NVIDIA card: %v", err)
	}
}

// QuickSync: picture widths that are multiples of 16 (626×480 crashed on the Dell).
func TestAlignForQSV(t *testing.T) {
	qsv := &Config{Codec: "qsv"}
	p := &Probe{Width: 1280, Height: 720}
	// crop + 480p: 938×720 cropped, then 626×480 → 624×480
	plan := &Plan{}
	applyCrop(&Config{Codec: "qsv", Crop: true, Scale: "480p"}, p, plan, &cropBox{938, 720, 172, 0})
	if plan.ScaleW != 624 || plan.ScaleH != 480 || plan.CropW != 938 {
		t.Errorf("crop + 480p: crop %dx%d scale %dx%d", plan.CropW, plan.CropH, plan.ScaleW, plan.ScaleH)
	}
	// cropped, already small enough (a 576p DVD): a little more of the frame is kept, centred
	dvd := &Probe{Width: 720, Height: 576}
	plan = &Plan{}
	applyCrop(&Config{Codec: "qsv", Crop: true, Scale: "576p"}, dvd, plan, &cropBox{690, 432, 14, 72})
	if plan.CropW != 704 || plan.CropX != 6 || plan.ScaleW != 0 {
		t.Errorf("crop only: crop %dx%d+%d scale %d", plan.CropW, plan.CropH, plan.CropX, plan.ScaleW)
	}
	// a crop near the right-hand edge is kept inside the frame
	plan = &Plan{}
	applyCrop(&Config{Codec: "qsv", Crop: true, Scale: "576p"}, dvd, plan, &cropBox{690, 432, 30, 72})
	if plan.CropW != 704 || plan.CropX+plan.CropW > 720 || plan.CropX%2 != 0 {
		t.Errorf("crop at the edge: crop %dx%d+%d", plan.CropW, plan.CropH, plan.CropX)
	}
	// already fine: nothing changes
	plan = &Plan{ScaleW: 1920, ScaleH: 800}
	alignForQSV(qsv, p, plan)
	if plan.ScaleW != 1920 || len(plan.Notes) != 0 {
		t.Errorf("1920 wide changed: %+v", plan)
	}
	// a source whose own width is not a multiple of 16
	plan = &Plan{}
	alignForQSV(qsv, &Probe{Width: 854, Height: 480}, plan)
	if plan.ScaleW != 848 || plan.ScaleH != 480 {
		t.Errorf("854 wide source: %+v", plan)
	}
	// other encoders, and copied video, are left alone
	for _, c := range []*Config{{Codec: "nvenc"}, {Codec: "x265"}, {Codec: "qsv", CopyVideo: true}} {
		plan = &Plan{ScaleW: 626, ScaleH: 480}
		alignForQSV(c, p, plan)
		if plan.ScaleW != 626 {
			t.Errorf("%+v: changed to %d", c, plan.ScaleW)
		}
	}
	// the whole path: makePlan with 480p on a 1280×720 source gives 848×480 (854 is not a multiple of 16)
	pr, _ := parseProbe([]byte(sampleProbe))
	pr.DV, pr.Width, pr.Height = nil, 1280, 720
	cfg := baseCfg()
	cfg.Codec, cfg.Scale = "qsv", "480p"
	if pl, err := makePlan(cfg, pr); err != nil || pl.ScaleW%16 != 0 {
		t.Errorf("makePlan 480p qsv: %v %+v", err, pl)
	}
}

// A hardware encode that crashes gets a plain message (and is not retried).
func TestHWCrash(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script")
	}
	n := 0
	err := withGPURetry(context.Background(), "qsv", func(io.Writer) error {
		n++
		return toolCommand(context.Background(), "sh", "-c", "kill -SEGV $$").Run()
	}, io.Discard)
	var hc *hwCrashError
	if !errors.As(err, &hc) || n != 1 || !strings.Contains(err.Error(), "Try x265") || !strings.Contains(err.Error(), "QuickSync") {
		t.Errorf("n=%d err=%v", n, err)
	}
	// an ordinary failure stays as it is
	err = withGPURetry(context.Background(), "qsv", func(io.Writer) error {
		return toolCommand(context.Background(), "sh", "-c", "exit 1").Run()
	}, io.Discard)
	if errors.As(err, &hc) {
		t.Errorf("exit 1 called a crash: %v", err)
	}
}
