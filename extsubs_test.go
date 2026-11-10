package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGuessSubFile(t *testing.T) {
	for name, want := range map[string]ExtSub{
		"Film.en.srt":             {Lang: "eng"},
		"Film.eng.forced.srt":     {Lang: "eng", Forced: true},
		"Film.English SDH.srt":    {Lang: "eng", SDH: true},
		"Film.fr.srt":             {Lang: "fre"},
		"Film.srt":                {},
		"Film.Deutsch.ass":        {Lang: "ger"},
		"Film [Spanish] (CC).srt": {Lang: "spa", SDH: true},
	} {
		g := guessSubFile(filepath.Join("x", name), "Film")
		if g.Lang != want.Lang || g.Forced != want.Forced || g.SDH != want.SDH {
			t.Errorf("%s: %+v, want %+v", name, g, want)
		}
	}
}

func TestFindSubFiles(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"Film.mkv", "Film.en.srt", "Film.srt", "Film 2.srt", "Filmography.srt", "Film.en.sub", "Other.srt"} {
		_ = os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644)
	}
	var got []string
	for _, f := range findSubFiles(filepath.Join(dir, "Film.mkv")) {
		got = append(got, filepath.Base(f.Path))
	}
	if strings.Join(got, ",") != "Film 2.srt,Film.en.srt,Film.srt" {
		t.Errorf("found: %v", got) // "Film 2.srt" counts: it starts with the video's name and a space
	}
}

func TestToUTF8(t *testing.T) {
	if b, enc := toUTF8([]byte("Caf\xe9 \x93hi\x94")); string(b) != "Café “hi”" || !strings.Contains(enc, "1252") {
		t.Errorf("cp1252: %q %q", b, enc)
	}
	if b, enc := toUTF8([]byte("\xef\xbb\xbfCafé")); string(b) != "Café" || enc != "" {
		t.Errorf("bom: %q %q", b, enc)
	}
	if b, enc := toUTF8([]byte{0xff, 0xfe, 'H', 0, 0xe9, 0}); string(b) != "Hé" || enc != "UTF-16" {
		t.Errorf("utf16: %q %q", b, enc)
	}
}

func TestCheckSubFile(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "a.srt")
	_ = os.WriteFile(good, []byte("1\r\n00:00:01,000 --> 00:00:02,500\r\nCaf\xe9\r\n\r\n2\r\n00:00:03,000 --> 00:00:04,000\r\nTwo\r\n"), 0o644)
	if _, n, note, err := checkSubFile(good); err != nil || n != 2 || !strings.Contains(note, "Windows-1252") {
		t.Errorf("good: %d %q %v", n, note, err)
	}
	bad := filepath.Join(dir, "b.srt")
	_ = os.WriteFile(bad, []byte("not subtitles"), 0o644)
	if _, _, _, err := checkSubFile(bad); err == nil || !strings.Contains(err.Error(), "holds no subtitles") {
		t.Errorf("bad: %v", err)
	}
}

func TestAddSubFileTranscode(t *testing.T) {
	ffmpeg, e1 := exec.LookPath("ffmpeg")
	_, e2 := exec.LookPath("ffprobe")
	if e1 != nil || e2 != nil {
		t.Skip("needs ffmpeg")
	}
	dir := t.TempDir()
	in := filepath.Join(dir, "Film.mkv")
	if b, err := exec.Command(ffmpeg, "-v", "error", "-y", "-f", "lavfi", "-i", "testsrc=s=320x180:r=24:d=6", "-c:v", "libx265", "-x265-params", "log-level=error", in).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, b)
	}
	srt := filepath.Join(dir, "Film.fr.srt")
	_ = os.WriteFile(srt, []byte("1\n00:00:01,000 --> 00:00:02,000\nCaf\xe9 cr\xe8me\n"), 0o644)
	for _, ext := range []string{".mkv", ".mp4"} {
		cfg := baseCfg()
		cfg.Input, cfg.Output, cfg.CopyVideo = in, filepath.Join(dir, "Film_KTL"+ext), true
		cfg.Ffmpeg, cfg.Ffprobe = ffmpeg, "ffprobe"
		cfg.NameTracks = true
		e := guessSubFile(srt, "Film")
		e.Offset, e.Forced = 1.5, true
		cfg.ExtSubs = []ExtSub{e}
		if err := transcode(context.Background(), cfg); err != nil {
			t.Fatal(err)
		}
		tr, err := probeTracks(context.Background(), "ffprobe", cfg.Output)
		if err != nil || len(tr.Subs) != 1 || tr.Subs[0].Lang != "fre" || !tr.Subs[0].Forced() || (ext == ".mkv" && tr.Subs[0].Title != "French SRT Forced") {
			t.Fatalf("%s tracks: %+v %v", ext, tr, err)
		}
		b, _ := exec.Command(ffmpeg, "-v", "error", "-i", cfg.Output, "-map", "0:s:0", "-f", "srt", "-").Output()
		if !strings.Contains(string(b), "Café crème") || !strings.Contains(string(b), "00:00:02,500 -->") {
			t.Errorf("%s srt:\n%s", ext, b)
		}
	}
	// the queue finds the file by itself (Settings: on), unless the file has its own list
	raw, _ := json.Marshal(map[string]any{"input": in, "findSubs": true})
	var s settings
	_ = json.Unmarshal(withFoundSubs([]json.RawMessage{raw})[0], &s)
	if len(s.ExtSubs) != 1 || s.ExtSubs[0].Lang != "fre" {
		t.Errorf("queue: %+v", s.ExtSubs)
	}
	raw, _ = json.Marshal(map[string]any{"input": in, "findSubs": true, "extSubs": []any{}})
	s = settings{}
	_ = json.Unmarshal(withFoundSubs([]json.RawMessage{raw})[0], &s)
	if len(s.ExtSubs) != 0 {
		t.Errorf("own list replaced: %+v", s.ExtSubs)
	}
}

func TestExtSubsWindowToJob(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "film.mkv")
	_ = os.WriteFile(in, []byte("x"), 0o644)
	s := &settings{Input: in, Output: filepath.Join(dir, "film_KTL.mkv"), Mode: "copy", Audio: "copy", CRF: 18,
		ExtSubs: []ExtSub{{Path: filepath.Join(dir, "film.fr.srt"), Lang: "fre", Forced: true, Offset: -0.5}, {Path: " "}}}
	cfg, err := s.toConfig()
	if err != nil || len(cfg.ExtSubs) != 1 {
		t.Fatalf("%v %+v", err, cfg)
	}
	old := os.Args
	defer func() { os.Args = old }()
	os.Args = append([]string{"keepthelight"}, append(cfg.cliArgs(), "-add-sub", filepath.Join(dir, "film.Spanish.srt"))...)
	got, ok := parseArgs()
	if !ok || len(got.ExtSubs) != 2 || got.ExtSubs[0].Lang != "fre" || !got.ExtSubs[0].Forced || got.ExtSubs[0].Offset != -0.5 || got.ExtSubs[1].Lang != "spa" {
		t.Errorf("parsed: %v %+v", ok, got)
	}
}
