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
