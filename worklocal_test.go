package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkLocally(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("needs ffmpeg")
	}
	net, local := t.TempDir(), t.TempDir() // "net" plays the network drive
	old := isNetworkPath
	isNetworkPath = func(p string) bool { return strings.HasPrefix(p, net) }
	t.Cleanup(func() { isNetworkPath = old })

	src := filepath.Join(net, "film.mkv")
	if b, err := exec.Command(ffmpeg, "-v", "error", "-y", "-f", "lavfi", "-i", "testsrc2=s=320x180:r=24:d=2", "-c:v", "libx264", src).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, b)
	}
	cfg := baseCfg()
	cfg.Input, cfg.Output, cfg.Preset, cfg.WorkLocal = src, filepath.Join(net, "film_KTL.mkv"), "ultrafast", local
	if err := transcode(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if !fileExists(filepath.Join(net, "film_KTL.mkv")) {
		t.Fatal("the result did not come back to the network drive")
	}
	if entries, _ := os.ReadDir(local); len(entries) != 0 {
		t.Errorf("the local working folder was not cleaned up: %v", entries)
	}

	// a test or metadata-only job, or files already on this PC: nothing is copied
	cfg2 := baseCfg()
	cfg2.Input, cfg2.Output, cfg2.WorkLocal = filepath.Join(local, "a.mkv"), filepath.Join(local, "b.mkv"), local
	if lw, err := startLocal(cfg2); lw != nil || err != nil {
		t.Errorf("local files: %v %v", lw, err)
	}
	cfg2.Input, cfg2.MetaOnly = src, true
	if lw, _ := startLocal(cfg2); lw != nil {
		t.Error("metadata only must not copy")
	}
}

// The copy back is proved by its fingerprint: a bad copy is made once more, and
// if it is bad twice the file made here is kept.
func TestCopyBackChecked(t *testing.T) {
	net, local := t.TempDir(), t.TempDir()
	made := filepath.Join(local, "film_KTL.partial.mkv")
	if err := os.WriteFile(made, []byte(strings.Repeat("picture ", 100000)), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { copyBackHook = nil })
	spoil := func(bad map[int]bool) {
		copyBackHook = func(p string, try int) {
			if bad[try] {
				_ = os.WriteFile(p, []byte("spoilt"), 0o644)
			}
		}
	}
	for _, c := range []struct {
		name string
		bad  map[int]bool
		ok   bool
	}{{"good", nil, true}, {"bad once", map[int]bool{1: true}, true}, {"bad twice", map[int]bool{1: true, 2: true}, false}} {
		spoil(c.bad)
		final := filepath.Join(net, c.name+".mkv")
		cfg := &Config{Output: made}
		lw := &localWork{dir: local, finalOut: final}
		err := lw.finish(cfg)
		b, _ := os.ReadFile(final)
		switch {
		case c.ok && (err != nil || cfg.Output != final || len(b) != 800000):
			t.Errorf("%s: %v, output %s, %d bytes", c.name, err, cfg.Output, len(b))
		case !c.ok && (err == nil || !lw.keep || cfg.Output != made || fileExists(final) || !strings.Contains(err.Error(), "kept at")):
			t.Errorf("%s: %v keep=%v final there=%v", c.name, err, lw.keep, fileExists(final))
		}
	}
}
