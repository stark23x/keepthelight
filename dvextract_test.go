package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// A file with a glitch near the start: the first way fails, the second works;
// when both fail, the message says what to do.
func TestExtractRPUSecondTry(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses shell scripts")
	}
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	ffmpeg := write("ffmpeg", `for a in "$@"; do last="$a"; done
if [ "$last" = "-" ]; then echo "hevc data"; exit 0; fi
case "$*" in *setts=pts=N:dts=N*) echo "hevc data" > "$last"; exit 0;; esac
exit 1
`)
	// fails when reading from the pipe ("Invalid PPS index"), works on a file
	dovi := write("dovi_tool", `case "$*" in *"extract-rpu - "*) cat >/dev/null; echo "Error: Invalid PPS index" >&2; exit 1;; esac
for a in "$@"; do prev="$last"; last="$a"; done
echo rpu > "$last"
`)
	tmp := t.TempDir()
	rpu := filepath.Join(tmp, "rpu.bin")
	r := &runner{ctx: context.Background()}
	cfg := &Config{Ffmpeg: ffmpeg, Input: "/films/glitch.mkv"}
	if err := extractRPU(r, cfg, dovi, 2, rpu, tmp); err != nil {
		t.Fatalf("second try: %v", err)
	}
	if b, _ := os.ReadFile(rpu); strings.TrimSpace(string(b)) != "rpu" {
		t.Errorf("rpu: %q", b)
	}
	if _, err := os.Stat(filepath.Join(tmp, "source.hevc")); err == nil {
		t.Error("the temp copy of the video was left behind")
	}
	// both ways fail: a plain message
	bad := write("dovi_bad", `cat >/dev/null 2>&1; echo "Error: Invalid PPS index" >&2; exit 1
`)
	err := extractRPU(r, cfg, bad, 0, rpu, tmp)
	if err == nil || !strings.Contains(err.Error(), "Drop (plain HDR10)") || !strings.Contains(err.Error(), "MKVToolNix") {
		t.Errorf("both failed: %v", err)
	}
}
