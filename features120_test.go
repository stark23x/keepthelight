package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestOnePerLanguageAndNameRules(t *testing.T) {
	tr := &Tracks{
		Audio: []srcTrack{
			{N: 1, Codec: "truehd", Lang: "eng", Channels: 8, Layout: "7.1"},
			{N: 2, Codec: "ac3", Lang: "eng", Channels: 6, Layout: "5.1(side)"},
			{N: 3, Codec: "ac3", Lang: "eng", Channels: 2, Layout: "stereo", Title: "Director's Commentary"},
			{N: 4, Codec: "aac", Lang: "jpn", Channels: 2, Layout: "stereo"},
			{N: 5, Codec: "eac3", Lang: "jpn", Channels: 6, Layout: "5.1(side)"},
		},
		Subs: []srcTrack{
			{N: 1, Codec: "subrip", Lang: "eng"},
			{N: 2, Codec: "subrip", Lang: "eng", Title: "English SDH"},
			{N: 3, Codec: "ass", Lang: "eng", Title: "Signs & Songs"},
			{N: 4, Codec: "subrip", Lang: "eng", Disp: map[string]bool{"hearing_impaired": true}},
		},
	}
	keptAudio := func(cfg *Config) string {
		a, _, _, err := selectTracks(cfg, tr, false)
		if err != nil {
			t.Fatal(err)
		}
		var n []string
		for _, x := range a {
			n = append(n, itoa(x.Src.N))
		}
		return strings.Join(n, ",")
	}
	cfg := baseCfg()
	cfg.AudioOnePerLang = "best"
	if got := keptAudio(cfg); got != "1,3,5" {
		t.Errorf("best: %s", got)
	}
	cfg.AudioOnePerLang = "compatible"
	if got := keptAudio(cfg); got != "2,3,5" {
		t.Errorf("compatible: %s", got)
	}
	cfg.DropCommentary = true
	if got := keptAudio(cfg); got != "2,5" {
		t.Errorf("compatible, no commentary: %s", got)
	}

	keptSubs := func(cfg *Config) string {
		_, s, _, _ := selectTracks(cfg, tr, false)
		var n []string
		for _, x := range s {
			n = append(n, itoa(x.Src.N))
		}
		return strings.Join(n, ",")
	}
	cfg = baseCfg()
	cfg.SubNameMode, cfg.SubNameWords = "drop", parseWords("SDH, signs")
	if got := keptSubs(cfg); got != "1" {
		t.Errorf("drop SDH/signs: %s", got)
	}
	cfg.SubNameMode = "keep"
	if got := keptSubs(cfg); got != "2,3,4" {
		t.Errorf("keep SDH/signs: %s", got)
	}
	cfg.SubSel = []trackSel{{N: 1}} // a file's own choice is not filtered
	if got := keptSubs(cfg); got != "1" {
		t.Errorf("own choice: %s", got)
	}
}

func TestNameUntitled(t *testing.T) {
	tr := &Tracks{
		Audio: []srcTrack{
			{N: 1, Codec: "ac3", Lang: "eng", Channels: 6, Layout: "5.1(side)"},
			{N: 2, Codec: "truehd", Profile: "Dolby TrueHD + Dolby Atmos", Lang: "eng", Channels: 8, Layout: "7.1"},
			{N: 3, Codec: "aac", Lang: "eng", Channels: 2, Layout: "stereo", Title: "Stereo mix"},
			{N: 4, Codec: "ac3", Lang: "", Channels: 2, Layout: "stereo", Disp: map[string]bool{"comment": true}},
		},
		Subs: []srcTrack{
			{N: 1, Codec: "hdmv_pgs_subtitle", Lang: "eng", Disp: map[string]bool{"forced": true}},
			{N: 2, Codec: "ass", Lang: "fre"},
			{N: 3, Codec: "subrip", Lang: "eng", Disp: map[string]bool{"hearing_impaired": true}},
			{N: 4, Codec: "subrip", Lang: "spa", Title: "Español"},
		},
	}
	cfg := baseCfg()
	cfg.NameTracks = true
	cfg.AudioSel = []trackSel{{N: 1}, {N: 2, Codec: "eac3", Channels: "5.1"}, {N: 3}, {N: 4}}
	cfg.SubSel = []trackSel{{N: 1, Forced: true}, {N: 2, SRT: "add"}, {N: 3}, {N: 4}}
	cfg.Meta = &MetaEdit{Tracks: []TrackMeta{{Kind: "a", N: 4, Title: strp("Commentary by the director")}}}
	a, s, _, err := selectTracks(cfg, tr, false)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, x := range a {
		got = append(got, x.Title)
	}
	if strings.Join(got, "|") != "English AC-3 5.1|English E-AC-3 5.1||" { // as written (converted); 3 and 4 keep their own names
		t.Errorf("audio: %q", got)
	}
	got = nil
	for _, x := range s {
		got = append(got, x.Title)
	}
	if strings.Join(got, "|") != "English PGS Forced|French ASS|French SRT|English SRT SDH|" {
		t.Errorf("subs: %q", got)
	}
	// a track without a name and nothing converted
	cfg.AudioSel = []trackSel{{N: 2}}
	a, _, _, _ = selectTracks(cfg, tr, false)
	if a[0].Title != "English TrueHD Atmos 7.1" {
		t.Errorf("atmos: %q", a[0].Title)
	}
	cfg.NameTracks = false
	if a, _, _, _ = selectTracks(cfg, tr, false); a[0].Title != "" {
		t.Errorf("off: %q", a[0].Title)
	}
}

func TestTMDBAutoMatch(t *testing.T) {
	res := []tmdbResult{{ID: 1, Title: "Heat", Year: "1995"}, {ID: 2, Title: "Heat", Year: "1986"}, {ID: 3, Title: "Heat Wave", Year: "1990"}}
	if m := tmdbAutoMatch(res, tmdbGuess{Query: "Heat", Year: "1995"}); m == nil || m.ID != 1 {
		t.Errorf("by year: %v", m)
	}
	if m := tmdbAutoMatch(res, tmdbGuess{Query: "Heat"}); m != nil {
		t.Errorf("two films called Heat: %v", m)
	}
	if m := tmdbAutoMatch([]tmdbResult{{ID: 9, Title: "Spider-Man: No Way Home", Year: "2021"}}, tmdbGuess{Query: "Spider Man No Way Home", Year: "2021"}); m == nil {
		t.Error("single result")
	}
	if m := tmdbAutoMatch([]tmdbResult{{ID: 9, Title: "Something else", Year: "2020"}}, tmdbGuess{Query: "x", Year: "2021"}); m != nil {
		t.Error("single result with another year")
	}
	if normTitle("The Lord of the Rings: The Return of the King") != "lord of the rings the return of the king" || normTitle("Fast & Furious") != "fast and furious" {
		t.Error(normTitle("Fast & Furious"))
	}
}

func TestProfileArgs(t *testing.T) {
	name, rest, found := takeProfileArg([]string{"-crf", "20", "-profile", "Plex 4K", "in.mkv"})
	if !found || name != "Plex 4K" || strings.Join(rest, " ") != "-crf 20 in.mkv" {
		t.Fatal(name, rest, found)
	}
	if n, _, f := takeProfileArg([]string{"--profile=A", "x"}); !f || n != "A" {
		t.Error(n)
	}
	dir := t.TempDir()
	prefs := filepath.Join(dir, "settings.json")
	ps := []map[string]any{{"name": "Plex 4K", "useNameTag": true, "settings": map[string]any{"mode": "encode", "codec": "nvenc", "quality": "crf",
		"crf": 22, "preset": "slow", "audio": "eac3", "dv": "auto", "scale": "1080p", "subToSRT": "replace", "audioOnePerLang": "best", "nameTracks": true, "format": ".mp4"}}}
	b, _ := json.Marshal(map[string]any{"profiles": ps})
	_ = os.WriteFile(prefs, b, 0o644)
	a, format, err := profileArgs(prefs, "plex 4k")
	if err != nil {
		t.Fatal(err)
	}
	s := strings.Join(a, " ")
	for _, want := range []string{"-crf 22", "-preset slow", "-codec nvenc", "-audio eac3", "-scale 1080p", "-subs-to-srt replace", "-audio-one-per-language best", "-name-tracks", "-tag _Plex 4K"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in %s", want, s)
		}
	}
	if format != ".mp4" || strings.Contains(s, "in.mkv") {
		t.Errorf("%s %s", format, s)
	}
	if _, _, err := profileArgs(prefs, "nope"); err == nil || !strings.Contains(err.Error(), "Plex 4K") {
		t.Errorf("unknown: %v", err)
	}
}

func TestQueueClearAllAndCopyTracks(t *testing.T) {
	ffmpeg, e1 := exec.LookPath("ffmpeg")
	_, e2 := exec.LookPath("ffprobe")
	if e1 != nil || e2 != nil {
		t.Skip("needs ffmpeg")
	}
	dir := t.TempDir()
	mk := func(name string, audios int) string {
		p := filepath.Join(dir, name)
		args := []string{"-v", "error", "-y", "-f", "lavfi", "-i", "testsrc=s=64x64:r=10:d=1"}
		for i := 0; i < audios; i++ {
			args = append(args, "-f", "lavfi", "-i", "sine=d=1")
		}
		args = append(args, "-map", "0")
		for i := 0; i < audios; i++ {
			args = append(args, "-map", itoa(i+1))
		}
		args = append(args, "-c:v", "mpeg4", "-c:a", "ac3", "-metadata:s:a", "language=eng", p)
		if b, err := exec.Command(ffmpeg, args...).CombinedOutput(); err != nil {
			t.Fatalf("%v %s", err, b)
		}
		return p
	}
	e1f, e2f, odd := mk("Show S01E01.mkv", 2), mk("Show S01E02.mkv", 2), mk("Other.mkv", 1)
	g := newGUIServer()
	g.prefsPath = filepath.Join(t.TempDir(), "settings.json")
	ids, errs := g.queueAdd([]json.RawMessage{
		itemWith(e1f, filepath.Join(dir, "o1.mkv"), map[string]any{"audioTracks": []map[string]any{{"n": 2}}, "audioDefault": 2, "subTracks": []any{}}),
		itemSettings(e2f, filepath.Join(dir, "o2.mkv")), itemSettings(odd, filepath.Join(dir, "o3.mkv"))})
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	r := g.queueCopyTracks(ids[0])
	if r["applied"] != 1 || len(r["different"].([]string)) != 1 || r["different"].([]string)[0] != "Other.mkv" {
		t.Fatalf("%v", r)
	}
	g.mu.Lock()
	it := g.findItemLocked(ids[1])
	if len(it.s.AudioTracks) != 1 || it.s.AudioTracks[0].N != 2 || it.s.AudioDefault != 2 || it.s.SubTracks == nil {
		t.Errorf("copied: %+v", it.s)
	}
	g.mu.Unlock()
	if r := g.queueCopyTracks(ids[1] + 100); r["ok"] != false {
		t.Error("unknown id")
	}
	if n := g.queueClearAll(); n != 3 || len(g.q.items) != 0 {
		t.Errorf("clear all: %d left %d", n, len(g.q.items))
	}
}

func TestQueueTMDB(t *testing.T) {
	fakeTools(t)
	t.Setenv("LOCALAPPDATA", t.TempDir())
	jpeg := []byte("\xff\xd8\xff\xe0\x00\x10JFIF\x00poster")
	srv := newFakeTMDB(t, jpeg)
	defer srv.Close()
	dir := t.TempDir()
	heat, other := filepath.Join(dir, "Heat.1995.2160p.mkv"), filepath.Join(dir, "Heat.mkv")
	for _, p := range []string{heat, other} {
		_ = os.WriteFile(p, []byte("x"), 0o644)
	}
	g := newGUIServer()
	g.prefsPath = filepath.Join(t.TempDir(), "settings.json")
	if r := g.queueTMDB(nil); r["ok"] != false {
		t.Error("no key accepted")
	}
	_ = g.setTMDBKey("k3y")
	ids, _ := g.queueAdd([]json.RawMessage{itemSettings(heat, filepath.Join(dir, "a_KTL.mkv")), itemSettings(other, filepath.Join(dir, "b_KTL.mkv"))})
	if r := g.queueTMDB(nil); r["files"] != 2 {
		t.Fatalf("%v", r)
	}
	note := func(id int) (string, *MetaEdit) {
		g.mu.Lock()
		defer g.mu.Unlock()
		it := g.findItemLocked(id)
		var m struct {
			TMDBNote string    `json:"tmdbNote"`
			Meta     *MetaEdit `json:"meta"`
		}
		_ = json.Unmarshal(it.Raw, &m)
		return m.TMDBNote, m.Meta
	}
	waitFor(t, "the lookups", func() bool {
		a, _ := note(ids[0])
		b, _ := note(ids[1])
		return !strings.Contains(a, "looking") && !strings.Contains(b, "looking")
	})
	n, m := note(ids[0])
	if !strings.Contains(n, "Heat (1995)") || m == nil || m.Tags["title"] != "Heat" || m.Tags["date"] != "1995-12-15" || !strings.HasSuffix(m.Cover, "tmdb-movie-949.jpg") {
		t.Errorf("matched: %s %+v", n, m)
	}
	if n, m := note(ids[1]); !strings.Contains(n, "needs a match") || m != nil {
		t.Errorf("two films called Heat: %s %+v", n, m)
	}
	g.mu.Lock()
	if it := g.findItemLocked(ids[0]); it.s.Meta == nil || it.s.Meta.Tags["title"] != "Heat" {
		t.Errorf("settings not updated: %+v", it.s.Meta)
	}
	g.mu.Unlock()
}
