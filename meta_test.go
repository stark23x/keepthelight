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

func strp(s string) *string { return &s }
func boolp(b bool) *bool    { return &b }

func TestParseMetaJSON(t *testing.T) {
	m, err := parseMetaJSON(`{"tags":{"title":"X","date":"2019"},"tracks":[{"kind":"a","n":2,"title":"Main","lang":"en"}],"chapters":{"names":{"1":"Opening"}}}`)
	if err != nil || m.Tags["title"] != "X" || *m.Tracks[0].Title != "Main" || m.Chapters.Names["1"] != "Opening" {
		t.Fatalf("%+v %v", m, err)
	}
	for _, bad := range []string{`{"tags":{"artist":"x"}}`, `{"tracks":[{"kind":"v","n":1}]}`, `{"cover":"x.gif"}`, `{"cover":"/nope/x.jpg"}`,
		`{"chapters":{"names":{"zero":"x"}}}`, `{bad`} {
		if _, err := parseMetaJSON(bad); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
	if m, _ := parseMetaJSON(""); m != nil || !m.empty() {
		t.Error("no metadata")
	}
}

func TestChapterFile(t *testing.T) {
	f := ffmetadataChapters([]chapter{{N: 1, Start: 0, End: 61.5, Title: "Chapter 1"}, {N: 2, Start: 61.5, End: 120, Title: "Chapter 2"}},
		map[string]string{"2": "A=B; #1"})
	want := ";FFMETADATA1\n[CHAPTER]\nTIMEBASE=1/1000000\nSTART=0\nEND=61500000\ntitle=Chapter 1\n" +
		"[CHAPTER]\nTIMEBASE=1/1000000\nSTART=61500000\nEND=120000000\ntitle=A\\=B\\; \\#1\n"
	if f != want {
		t.Errorf("got\n%s\nwant\n%s", f, want)
	}
}

func TestPrepareMeta(t *testing.T) {
	r := &runner{ctx: context.Background(), dry: true}
	src := &Tracks{Chapters: []chapter{{N: 1, End: 10, Title: "One"}}, Covers: []cover{{VIndex: 1, Filename: "cover.jpg", Mime: "image/jpeg"}}, Attachments: 2}
	plan := &Plan{Src: src}
	cfg := baseCfg()

	// nothing edited: tags and chapters from the original, fonts copied, the cover re-attached after them
	mp, err := prepareMeta(r, cfg, plan, 1, 2, false, "/tmp/x")
	if err != nil {
		t.Fatal(err)
	}
	a := strings.Join(mp.Args, " ")
	for _, want := range []string{"-map_metadata 1", "-map_chapters 1", "-map 1:t? -c:t copy",
		"-attach /tmp/x/cover0.jpg -metadata:s:t:2 mimetype=image/jpeg -metadata:s:t:2 filename=cover.jpg"} {
		if !strings.Contains(a, want) {
			t.Errorf("missing %q in %s", want, a)
		}
	}
	if len(mp.Inputs) != 0 {
		t.Errorf("inputs: %v", mp.Inputs)
	}
	if !metaNeedsFiles(cfg, plan) {
		t.Error("re-attaching a cover needs a temp folder")
	}

	// edits: tags, renamed chapters (an extra input), a new cover replacing the old one
	cfg.Meta = &MetaEdit{Tags: map[string]string{"title": "New", "date": ""}, Chapters: &ChapterEdit{Names: map[string]string{"1": "Opening"}}, Cover: "/pics/c.PNG"}
	mp, _ = prepareMeta(r, cfg, plan, 0, 1, false, "/tmp/x")
	a = strings.Join(mp.Args, " ")
	if strings.Join(mp.Inputs, " ") != "-f ffmetadata -i /tmp/x/chapters.txt" || !strings.Contains(a, "-map_chapters 1") {
		t.Errorf("chapter names: %v %s", mp.Inputs, a)
	}
	if !strings.Contains(a, "-metadata date= -metadata title=New") || strings.Contains(a, "cover0") ||
		!strings.Contains(a, "-attach /pics/c.PNG -metadata:s:t:2 mimetype=image/png -metadata:s:t:2 filename=cover.png") {
		t.Errorf("edits: %s", a)
	}

	// removing: no chapters, no cover; MP4 has no attachments at all
	cfg.Meta = &MetaEdit{Chapters: &ChapterEdit{Remove: true}, RemoveCover: true}
	mp, _ = prepareMeta(r, cfg, plan, 0, 1, true, "/tmp/x")
	if a = strings.Join(mp.Args, " "); a != "-map_metadata 0 -map_chapters -1" {
		t.Errorf("remove / mp4: %s", a)
	}
}

func TestTrackMetaAndMetaOnly(t *testing.T) {
	tr := testTracks(t)
	cfg := baseCfg()
	cfg.Meta = &MetaEdit{Tracks: []TrackMeta{
		{Kind: "a", N: 1, Title: strp("Main"), Lang: strp("English")},
		{Kind: "a", N: 2, Commentary: boolp(true)},
		{Kind: "s", N: 1, SDH: boolp(false), Title: strp("")},
	}}
	cfg.AudioSel = []trackSel{{N: 1, Codec: "eac3"}, {N: 2}} // the user's name wins over the automatic "E-AC-3 5.1"
	cfg.SubSel = []trackSel{{N: 1}}
	a, s, _, err := selectTracks(cfg, tr, false)
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Join(audioArgs(a, 0), " ")
	if !strings.Contains(args, "-metadata:s:a:0 title=Main -metadata:s:a:0 language=eng") || strings.Contains(args, "E-AC-3 5.1") ||
		!strings.Contains(args, "-disposition:a:1 comment") {
		t.Errorf("audio: %s", args)
	}
	if sa := strings.Join(subtitleArgs(s, 0), " "); !strings.Contains(sa, "-disposition:s:0 0") || !strings.Contains(sa, "-metadata:s:s:0 title=") {
		t.Errorf("subs: %s", sa) // SDH cleared, name removed
	}

	// metadata only: every track is copied, and MP4 cannot take TrueHD without converting
	cfg = baseCfg()
	cfg.MetaOnly, cfg.CopyVideo = true, true
	cfg.AudioSel = []trackSel{{N: 3, Codec: "aac"}}
	if a, _, _, _ := selectTracks(cfg, tr, false); a[0].Codec != "copy" {
		t.Errorf("metadata only converted: %+v", a[0])
	}
	cfg.AudioSel = nil
	if _, _, _, err := selectTracks(cfg, tr, true); err == nil || !strings.Contains(err.Error(), "metadata only does not convert") {
		t.Errorf("TrueHD to MP4: %v", err)
	}
}

func TestMetaSettingsFromWindow(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "film.mkv")
	_ = os.WriteFile(in, []byte("x"), 0o644)
	s := &settings{Input: in, Output: filepath.Join(dir, "film_KTL.mp4"), Mode: "meta", Audio: "eac3", CRF: 18,
		ReplaceOriginal: true, Meta: &MetaEdit{Tags: map[string]string{"title": "T"}}}
	cfg, err := s.toConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.MetaOnly || !cfg.CopyVideo || cfg.Audio != "copy" || cfg.Output != filepath.Join(dir, "film.mp4") {
		t.Errorf("%+v", cfg)
	}
	a := strings.Join(cfg.cliArgs(), " ")
	if !strings.Contains(a, "-metadata-only") || !strings.Contains(a, `-meta-json {"tags":{"title":"T"}}`) || strings.Contains(a, "replace") {
		t.Errorf("cli: %s", a) // the window replaces the original itself, after the job
	}
	if _, err := validateSettings(*s); err != nil {
		t.Errorf("validate: %v", err)
	}
	s.Output = in // same format: the output is the original itself, which is allowed here
	cfg, _ = s.toConfig()
	if outputTaken(cfg) {
		t.Error("the original counted as an existing output")
	}
	s.Mode = "encode"
	if _, err := s.toConfig(); err == nil {
		t.Error("replacing the original while re-encoding accepted")
	}

	g := newGUIServer()
	raw, _ := json.Marshal(settings{Input: in, Output: filepath.Join(dir, "film_KTL.mkv"), Mode: "meta", CRF: 18})
	r := g.spaceCheck(raw)
	if r["metaJobs"] != 1 || r["enough"] != true || r["total"] != "1 bytes" && r["total"] == nil {
		t.Errorf("space check: %v", r)
	}
	raw, _ = json.Marshal(settings{Input: in, Output: filepath.Join(dir, "x.mkv"), Mode: "encode", CRF: 18})
	if r := g.spaceCheck(raw); r["metaJobs"] != 0 {
		t.Errorf("no metadata-only job: %v", r)
	}
}

func TestDisposeOriginal(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "film.mkv")
	for i := 0; i < 2; i++ {
		_ = os.WriteFile(p, []byte("x"), 0o644)
		where, err := disposeOriginal(p) // no Recycle Bin here: renamed, never deleted
		if err != nil || fileExists(p) || !strings.Contains(where, "renamed") {
			t.Fatalf("%s %v", where, err)
		}
	}
	for _, n := range []string{"film (original).mkv", "film (original 2).mkv"} {
		if !fileExists(filepath.Join(dir, n)) {
			t.Errorf("%s missing", n)
		}
	}
}

func TestReplaceOriginalChecksFirst(t *testing.T) {
	ffmpeg, err1 := exec.LookPath("ffmpeg")
	ffprobe, err2 := exec.LookPath("ffprobe")
	if err1 != nil || err2 != nil {
		t.Skip("needs ffmpeg")
	}
	dir := t.TempDir()
	orig, short := filepath.Join(dir, "film.mkv"), filepath.Join(dir, "film.partial.mkv")
	mk := func(out, dur string) {
		if b, err := exec.Command(ffmpeg, "-v", "error", "-y", "-f", "lavfi", "-i", "testsrc=s=64x64:r=10:d="+dur, "-c:v", "mpeg4", out).CombinedOutput(); err != nil {
			t.Fatalf("%v %s", err, b)
		}
	}
	mk(orig, "10")
	mk(short, "3") // an incomplete copy
	if _, err := replaceOriginal(context.Background(), ffprobe, orig, short, orig); err == nil || !strings.Contains(err.Error(), "NOT replaced") {
		t.Fatalf("an incomplete copy replaced the original: %v", err)
	}
	if !fileExists(orig) || !fileExists(short) {
		t.Fatal("files were touched")
	}
	mk(short, "10")
	lines, err := replaceOriginal(context.Background(), ffprobe, orig, short, orig)
	if err != nil || fileExists(short) || !fileExists(orig) || !fileExists(filepath.Join(dir, "film (original).mkv")) {
		t.Fatalf("%v %v", lines, err)
	}
}
