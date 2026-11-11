package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseInterleave(t *testing.T) {
	streams := `"streams":[{"index":0,"codec_type":"video","disposition":{"attached_pic":0}},
		{"index":1,"codec_type":"audio","disposition":{"attached_pic":0}},
		{"index":2,"codec_type":"video","disposition":{"attached_pic":1}}]`
	// the Exhuma file: one picture (no time yet), then 16 s of sound, the cover
	var bad strings.Builder
	bad.WriteString(`{` + streams + `,"packets":[{"stream_index":0,"pts_time":"N/A","dts_time":"N/A"},{"stream_index":2,"pts_time":"0.000"}`)
	for i := 0; i < 160; i++ {
		bad.WriteString(`,{"stream_index":1,"pts_time":"` + ftoa(float64(i)/10) + `"}`)
	}
	bad.WriteString(`]}`)
	gap, err := parseInterleave([]byte(bad.String()))
	if err != nil || gap < 15 || gap > 16 {
		t.Fatalf("bad file: gap %v, %v", gap, err)
	}
	// well stored: picture and sound take turns
	var good strings.Builder
	good.WriteString(`{` + streams + `,"packets":[`)
	for i := 0; i < 200; i++ {
		if i > 0 {
			good.WriteString(",")
		}
		good.WriteString(`{"stream_index":` + map[bool]string{true: "0", false: "1"}[i%2 == 0] + `,"pts_time":"` + ftoa(float64(i/2)/25) + `"}`)
	}
	good.WriteString(`]}`)
	if gap, err := parseInterleave([]byte(good.String())); err != nil || gap > 0.1 {
		t.Fatalf("good file: gap %v, %v", gap, err)
	}
	// no sound: nothing to compare
	if gap, _ := parseInterleave([]byte(`{"streams":[{"index":0,"codec_type":"video"}],"packets":[{"stream_index":0,"pts_time":"5"}]}`)); gap != 0 {
		t.Fatalf("no sound: gap %v", gap)
	}
}

func ftoa(f float64) string { return offsetArg(f) }

// The remux keeps every track, its name and the cover, and the result reads as well stored.
func TestRemuxInterleaved(t *testing.T) {
	ffmpeg, e1 := exec.LookPath("ffmpeg")
	ffprobe, e2 := exec.LookPath("ffprobe")
	if e1 != nil || e2 != nil {
		t.Skip("ffmpeg/ffprobe not found")
	}
	dir := t.TempDir()
	cov := filepath.Join(dir, "c.jpg")
	in := filepath.Join(dir, "in.mkv")
	if b, err := exec.Command(ffmpeg, "-v", "error", "-f", "lavfi", "-i", "color=red:s=64x64", "-frames:v", "1", cov).CombinedOutput(); err != nil {
		t.Fatal(err, string(b))
	}
	if b, err := exec.Command(ffmpeg, "-v", "error", "-f", "lavfi", "-i", "testsrc2=s=160x120:r=25:d=4", "-f", "lavfi", "-i", "sine=d=4",
		"-map", "0", "-map", "1", "-map", "1", "-c:v", "mpeg4", "-c:a", "aac", "-metadata:s:a:1", "title=Commentary", "-disposition:a:1", "0",
		"-attach", cov, "-metadata:s:t", "mimetype=image/jpeg", "-metadata:s:t", "filename=cover.jpg", in).CombinedOutput(); err != nil {
		t.Fatal(err, string(b))
	}
	out := filepath.Join(dir, "out.mkv")
	if err := remuxInterleaved(context.Background(), ffmpeg, ffprobe, in, out, dir); err != nil {
		t.Fatal(err)
	}
	b, _ := exec.Command(ffprobe, "-v", "error", "-show_entries", "stream=codec_type:stream_tags=title,filename:stream_disposition=attached_pic,default",
		"-of", "compact", out).Output()
	s := string(b)
	for _, want := range []string{"title=Commentary", "filename=cover.jpg", "attached_pic=1"} {
		if !strings.Contains(s, want) {
			t.Errorf("the remux lost %q:\n%s", want, s)
		}
	}
	if n := strings.Count(s, "codec_type=audio"); n != 2 {
		t.Errorf("%d audio tracks, want 2", n)
	}
	if bad, gap := badInterleave(context.Background(), ffprobe, out); bad {
		t.Errorf("remux reads as badly stored (%v s)", gap)
	}
	if _, err := os.Stat(filepath.Join(dir, "ktl-cover0.jpg")); err == nil {
		t.Error("the saved cover was left behind")
	}
	// a file that is already fine is left alone
	lines, err := fixInterleaveFile(context.Background(), ffmpeg, ffprobe, out)
	if err != nil || len(lines) != 1 || !strings.Contains(lines[0], "nothing to fix") {
		t.Errorf("fixing a good file: %v %v", lines, err)
	}
}
