package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAnimationArgs(t *testing.T) {
	p, _ := parseProbe([]byte(sampleProbe))
	p.DV = nil
	cfg := baseCfg()
	plan, _ := makePlan(cfg, p)
	a, _ := buildVideoArgs(cfg, p, plan, "")
	if j := strings.Join(a, " "); strings.Contains(j, "animation") || strings.Contains(j, "bframes") {
		t.Errorf("x265 without Animation: %s", j)
	}
	cfg.Animation = true
	a, _ = buildVideoArgs(cfg, p, plan, "")
	if j := strings.Join(a, " "); !strings.Contains(j, "-tune animation") || !strings.Contains(j, "bframes=8") || !strings.Contains(j, "hdr10=1") {
		t.Errorf("x265 with Animation: %s", j)
	}
	for _, codec := range []string{"nvenc", "qsv"} {
		cfg.Codec, cfg.animHW = codec, animHWOptions[codec][0]
		a, _ = buildVideoArgs(cfg, p, plan, "")
		j := strings.Join(a, " ")
		if !strings.Contains(j, strings.Join(animHWOptions[codec][0], " ")) || strings.Contains(j, "tune animation") {
			t.Errorf("%s with Animation: %s", codec, j)
		}
		cfg.Animation = false
		a, _ = buildVideoArgs(cfg, p, plan, "")
		if j := strings.Join(a, " "); strings.Contains(j, " -bf ") {
			t.Errorf("%s without Animation: %s", codec, j)
		}
		cfg.Animation = true
	}
}

func TestAnimationSettings(t *testing.T) {
	s := &settings{Input: "a.mkv", Output: "b.mkv", Codec: "x265", Preset: "medium", CRF: 18, Animation: true}
	cfg, err := s.toConfig()
	if err != nil {
		t.Fatal(err)
	}
	if a := strings.Join(cfg.cliArgs(), " "); !strings.Contains(a, "-animation") {
		t.Errorf("cliArgs: %s", a)
	}
	s.Mode = "copy" // only for re-encoding
	if cfg, _ = s.toConfig(); cfg.Animation {
		t.Error("copy video: animation kept")
	}
}

// NVENC on a card without B-frames in HEVC: the fullest set it takes is used.
func TestAnimationOptionsFallBack(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script")
	}
	dir := t.TempDir()
	fake := filepath.Join(dir, "ffmpeg")
	_ = os.WriteFile(fake, []byte(`#!/bin/sh
case "$*" in *hevc_nvenc*-bf*) echo "[hevc_nvenc @ 0x1] Max B-frames 4 exceeds 0" >&2; exit 1;; esac
case "$*" in *hevc_qsv*) exit 1;; esac
exit 0
`), 0o755)
	animChecks.Clear()
	t.Cleanup(animChecks.Clear)
	args, msg := animationOptions(context.Background(), fake, "nvenc")
	if strings.Join(args, " ") != "-rc-lookahead 32 -temporal-aq 1" || !strings.Contains(msg, "no B-frames") || !strings.Contains(msg, "AQ that follows motion") {
		t.Errorf("nvenc: %v %q", args, msg)
	}
	if args, msg := animationOptions(context.Background(), fake, "qsv"); args != nil || !strings.Contains(msg, "encoded as usual") {
		t.Errorf("qsv: %v %q", args, msg)
	}
	if _, msg := animationOptions(context.Background(), fake, "x265"); !strings.Contains(msg, "animation tune") {
		t.Errorf("x265: %q", msg)
	}
}

func TestAnimWords(t *testing.T) {
	if w := animWords(animHWOptions["nvenc"][0]); w != "up to 4 B-frames, also used as references, a longer look ahead, AQ that follows motion" {
		t.Error(w)
	}
	if w := animWords(animHWOptions["qsv"][0]); w != "up to 7 B-frames, placed where they help" {
		t.Error(w)
	}
}

func TestHasAnimation(t *testing.T) {
	if !hasAnimation([]tmdbGenre{{ID: 18}, {ID: 16}}) || hasAnimation([]tmdbGenre{{ID: 18}}) || hasAnimation(nil) {
		t.Error("genre 16 is Animation")
	}
}
