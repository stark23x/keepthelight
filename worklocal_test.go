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
