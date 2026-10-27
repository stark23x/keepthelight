package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAnalyse(t *testing.T) {
	// a 2-hour 4K H.264 at 40 Mbps with TrueHD only and forced PGS subtitles
	h264 := `{"streams":[{"codec_type":"video","codec_name":"h264","width":3840,"height":2160,"pix_fmt":"yuv420p","bit_rate":"40000000"},
		{"codec_type":"audio","codec_name":"truehd","channels":8,"channel_layout":"7.1","bit_rate":"4000000"},
		{"codec_type":"subtitle","codec_name":"hdmv_pgs_subtitle","disposition":{"forced":1}}],
		"format":{"duration":"7200","size":"39600000000","bit_rate":"44000000"}}`
	it := &scanItem{}
	analyse(it, []byte(h264))
	if it.Video != "H.264 3840×2160" || !it.Worth || it.Saving < 20e9 || len(it.Issues) != 2 {
		t.Errorf("%+v", it)
	}
	if !strings.Contains(it.Audio, "TrueHD 7.1") {
		t.Errorf("audio %q", it.Audio)
	}

	// an HEVC Dolby Vision 7 remux with E-AC-3 already small: not worth it, but profile 7 flagged
	dv := `{"streams":[{"codec_type":"video","codec_name":"hevc","width":1920,"height":1080,"color_transfer":"smpte2084","tags":{"BPS":"4500000"},
		"side_data_list":[{"side_data_type":"DOVI configuration record","dv_profile":7}]},
		{"codec_type":"audio","codec_name":"eac3","channels":6,"channel_layout":"5.1(side)"}],
		"format":{"duration":"6000","size":"3600000000"}}`
	it = &scanItem{}
	analyse(it, []byte(dv))
	if it.Worth || !strings.Contains(it.Video, "Dolby Vision 7") || len(it.Issues) != 1 || !strings.Contains(it.Issues[0], "profile 7") {
		t.Errorf("%+v", it)
	}
	if it.Kbps != 4800 { // from the size and duration
		t.Errorf("kbps %d", it.Kbps)
	}
	if !isOwnOutput("Film_KTL", "") || !isOwnOutput("Film_4K", "_4k") || isOwnOutput("Film", "") {
		t.Error("own outputs")
	}
}

func TestScanFolder(t *testing.T) {
	ffmpeg, err1 := exec.LookPath("ffmpeg")
	_, err2 := exec.LookPath("ffprobe")
	if err1 != nil || err2 != nil {
		t.Skip("needs ffmpeg")
	}
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "Season 1"), 0o755)
	mk := func(out string, extra ...string) {
		args := append([]string{"-v", "error", "-y", "-f", "lavfi", "-i", "testsrc=s=320x240:r=24:d=2", "-f", "lavfi", "-i", "sine=d=2"}, extra...)
		if b, err := exec.Command(ffmpeg, append(args, out)...).CombinedOutput(); err != nil {
			t.Fatalf("%v %s", err, b)
		}
	}
	mk(filepath.Join(dir, "a.mkv"), "-c:v", "mpeg4", "-b:v", "8M", "-c:a", "flac")
	mk(filepath.Join(dir, "Season 1", "b.mkv"), "-c:v", "mpeg4", "-c:a", "ac3")
	mk(filepath.Join(dir, "a_KTL.mkv"), "-c:v", "mpeg4", "-c:a", "ac3") // an output: skipped
	_ = os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o644)

	for _, rec := range []bool{false, true} {
		g := newGUIServer()
		if r := g.scanStart(dir, rec, "_KTL"); r["ok"] != true {
			t.Fatal(r)
		}
		var st map[string]any
		for i := 0; i < 200; i++ {
			if st = g.scanStatus(0); st["running"] == false {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		items := st["items"].([]*scanItem)
		want := map[bool]int{false: 1, true: 2}[rec]
		if len(items) != want || st["done"] != want {
			t.Fatalf("recursive=%v: %d items %v", rec, len(items), st)
		}
		for _, it := range items {
			if it.Err != "" || !strings.HasPrefix(it.Video, "MPEG-4 320×240") {
				t.Errorf("%+v", it)
			}
			if it.Name == "a.mkv" && (len(it.Issues) != 1 || !strings.Contains(it.Issues[0], "No AC-3")) {
				t.Errorf("FLAC only not flagged: %+v", it)
			}
			if it.Name == "b.mkv" && it.Folder != "Season 1" {
				t.Errorf("folder %q", it.Folder)
			}
		}
		if more := g.scanStatus(len(items))["items"].([]*scanItem); len(more) != 0 {
			t.Errorf("items after the last: %v", more)
		}
	}
}
