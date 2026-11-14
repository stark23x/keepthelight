package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A 30-second test from the middle of a film shows the added subtitle file's
// lines of that stretch (its clock starts at 0, so the file moves with it).
func TestAddedSubsInATest(t *testing.T) {
	ffmpeg, e1 := exec.LookPath("ffmpeg")
	_, e2 := exec.LookPath("ffprobe")
	if e1 != nil || e2 != nil {
		t.Skip("needs ffmpeg")
	}
	dir := t.TempDir()
	in := filepath.Join(dir, "Film.mkv")
	if b, err := exec.Command(ffmpeg, "-v", "error", "-y", "-f", "lavfi", "-i", "testsrc=s=320x180:r=24:d=40", "-g", "24", "-pix_fmt", "yuv420p",
		"-c:v", "libx265", "-x265-params", "log-level=error", in).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, b)
	}
	srt := filepath.Join(dir, "Film.en.srt")
	_ = os.WriteFile(srt, []byte("1\n00:00:05,000 --> 00:00:06,000\nEarly line\n\n2\n00:00:31,000 --> 00:00:33,000\nIn the test\n"), 0o644)
	cfg := baseCfg()
	cfg.Input, cfg.Output, cfg.CopyVideo = in, filepath.Join(dir, "Film_KTL.mkv"), false
	cfg.Ffmpeg, cfg.Ffprobe = ffmpeg, "ffprobe"
	cfg.Codec, cfg.Preset, cfg.CRF = "x265", "ultrafast", 30
	cfg.SampleSecs, cfg.SampleStart = 5, 30
	cfg.ExtSubs = []ExtSub{guessSubFile(srt, "Film")}
	if err := transcode(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	out := cfg.Output
	if _, err := os.Stat(out); err != nil {
		matches, _ := filepath.Glob(filepath.Join(dir, "*30 s test*.mkv"))
		if len(matches) == 0 {
			matches, _ = filepath.Glob(filepath.Join(dir, "*test*.mkv"))
		}
		if len(matches) == 0 {
			t.Fatal("no test file")
		}
		out = matches[0]
	}
	b, err := exec.Command(ffmpeg, "-v", "error", "-i", out, "-map", "0:s:0", "-f", "srt", "-").Output()
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, "In the test") || !strings.Contains(s, "00:00:01,000 --> 00:00:03,000") || strings.Contains(s, "Early line") {
		t.Errorf("the test's subtitles:\n%s", s)
	}
}

func TestCutSubText(t *testing.T) {
	srt := "1\r\n00:00:05,000 --> 00:00:06,000\r\nGone\r\n\r\n2\r\n00:45:59,500 --> 00:46:02,250\r\nAcross the start\r\n\r\n3\r\n01:00:10,040 --> 01:00:12,000\r\nTwo\r\nlines\r\n"
	got := string(cutSubText([]byte(srt), "subrip", 2760))
	want := "1\n00:00:00,000 --> 00:00:02,250\nAcross the start\n\n2\n00:14:10,040 --> 00:14:12,000\nTwo\r\nlines\n"
	if got != want {
		t.Errorf("srt:\n%q\nwant\n%q", got, want)
	}
	ass := "[Events]\nFormat: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\nDialogue: 0,0:00:05.00,0:00:06.00,Default,,0,0,0,,Gone\nDialogue: 0,1:00:10.04,1:00:12.00,Default,,0,0,0,,Kept, with a comma"
	got = string(cutSubText([]byte(ass), "ass", 2760))
	if strings.Contains(got, "Gone") || !strings.Contains(got, "Dialogue: 0,0:14:10.04,0:14:12.00,Default,,0,0,0,,Kept, with a comma") || !strings.Contains(got, "[Events]") {
		t.Errorf("ass:\n%s", got)
	}
}
