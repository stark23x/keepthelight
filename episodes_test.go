package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMapEpisode(t *testing.T) {
	d := []epShift{{At: 9, Kind: "double"}}
	for n, want := range map[int]tmdbEp{8: {First: 8}, 9: {First: 9, Last: 10}, 10: {First: 11}, 22: {First: 23}} {
		if got := mapEpisode(n, 0, d); got != want {
			t.Errorf("double: file %d → %+v, want %+v", n, got, want)
		}
	}
	sp := []epShift{{At: 5, Kind: "split"}}
	for n, want := range map[int]tmdbEp{4: {First: 4}, 5: {First: 5, Part: 1}, 6: {First: 5, Part: 2}, 7: {First: 6}} {
		if got := mapEpisode(n, 0, sp); got != want {
			t.Errorf("split: file %d → %+v, want %+v", n, got, want)
		}
	}
	// a file named S05E11-E12 after the marked double: both numbers move on
	if got := mapEpisode(11, 12, d); got != (tmdbEp{First: 12, Last: 13}) {
		t.Errorf("range after double: %+v", got)
	}
	if got := joinTitles([]string{"We're in the Army, Now: Part 1", "We're in the Army, Now: Part 2"}); got != "We're in the Army, Now (Parts 1 & 2)" {
		t.Errorf("joined: %q", got)
	}
	if got := joinTitles([]string{"The Wedding (1)", "The Wedding (2)"}); got != "The Wedding (Parts 1 & 2)" {
		t.Errorf("joined (1)(2): %q", got)
	}
	if got := joinTitles([]string{"Arrival", "Departure"}); got != "Arrival / Departure" {
		t.Errorf("joined different: %q", got)
	}
	n := plexEpisodeRangeName("Laverne & Shirley", "1976", 5, tmdbEp{First: 9, Last: 10}, "We're in the Army, Now")
	if plexFinal(n, "480p.H264") != "Laverne & Shirley (1976).S05E09-E10.We're in the Army, Now.480p.H264" {
		t.Errorf("plex name: %q", n)
	}
	if plexFinal(plexEpisodeRangeName("Show", "", 2, tmdbEp{First: 5, Part: 2}, "Big"), "1080p.HEVC") != "Show.S02E05.Big.1080p.HEVC-pt2" {
		t.Errorf("split name")
	}
}

func TestSpotDoubles(t *testing.T) {
	eps := []tmdbEpisode{{Number: 8, Name: "Bad Girls", Runtime: 25}, {Number: 9, Name: "We're in the Army, Now (1)", Runtime: 25},
		{Number: 10, Name: "We're in the Army, Now (2)", Runtime: 25}, {Number: 11, Name: "Take Two, They're Small", Runtime: 25}}
	files := map[int]float64{8: 25, 9: 50, 10: 25, 11: 24}
	if got := spotDoubles(files, eps, nil); len(got) != 1 || got[0].At != 9 {
		t.Errorf("spotted: %+v", got)
	}
	if got := spotDoubles(files, eps, []epShift{{At: 9, Kind: "double"}}); len(got) != 0 {
		t.Errorf("spotted a marked one again: %+v", got)
	}
	files[9] = 26 // not long: nothing
	if got := spotDoubles(files, eps, nil); len(got) != 0 {
		t.Errorf("spotted a normal file: %+v", got)
	}
}

// laverneTMDB: a stand-in TMDB with season 5 of Laverne & Shirley (episodes 9 and 10 are one show).
func laverneTMDB(t *testing.T) {
	name := func(n int) string {
		switch n {
		case 9:
			return "We're in the Army, Now (1)"
		case 10:
			return "We're in the Army, Now (2)"
		case 11:
			return "Take Two, They're Small"
		}
		return fmt.Sprintf("Episode %d", n)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case p == "/3/search/tv":
			_, _ = w.Write([]byte(`{"results":[{"id":1985,"name":"Laverne & Shirley","first_air_date":"1976-01-27"}]}`))
		case p == "/3/tv/1985/external_ids":
			_, _ = w.Write([]byte(`{"tvdb_id":77777,"imdb_id":"tt0074016"}`))
		case p == "/3/tv/1985":
			_, _ = w.Write([]byte(`{"name":"Laverne & Shirley","first_air_date":"1976-01-27","overview":"Milwaukee."}`))
		case strings.HasPrefix(p, "/3/tv/1985/season/5/episode/"):
			var n int
			fmt.Sscan(p[strings.LastIndex(p, "/")+1:], &n)
			b, _ := json.Marshal(map[string]any{"name": name(n), "air_date": "1979-11-15", "overview": "Ep " + itoa(n) + "."})
			_, _ = w.Write(b)
		case p == "/3/tv/1985/season/5":
			var eps []map[string]any
			for n := 1; n <= 24; n++ {
				eps = append(eps, map[string]any{"episode_number": n, "name": name(n), "runtime": 25})
			}
			b, _ := json.Marshal(map[string]any{"episodes": eps})
			_, _ = w.Write(b)
		default:
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"status_message":"not found"}`))
		}
	}))
	t.Cleanup(srv.Close)
	oldAPI, oldImg := tmdbAPI, tmdbImages
	tmdbAPI, tmdbImages = srv.URL+"/3", srv.URL+"/img/"
	t.Cleanup(func() { tmdbAPI, tmdbImages = oldAPI, oldImg })
	fakeTVmaze(t, nil) // TVmaze knows nothing: TMDB alone, as before
}

func TestDoubleEpisodeRenumbering(t *testing.T) {
	ffmpeg, e1 := exec.LookPath("ffmpeg")
	_, e2 := exec.LookPath("ffprobe")
	if e1 != nil || e2 != nil {
		t.Skip("needs ffmpeg")
	}
	t.Setenv("LOCALAPPDATA", t.TempDir())
	laverneTMDB(t)
	dir := t.TempDir()
	var paths []string
	for n := 7; n <= 12; n++ {
		p := filepath.Join(dir, fmt.Sprintf("Laverne & Shirley S05E%02d.mkv", n))
		d := "2"
		if n == 9 {
			d = "4" // the hour-long one
		}
		if b, err := exec.Command(ffmpeg, "-v", "error", "-y", "-f", "lavfi", "-i", "testsrc=s=640x480:r=10:d="+d, "-c:v", "mpeg4", p).CombinedOutput(); err != nil {
			t.Fatalf("%v %s", err, b)
		}
		paths = append(paths, p)
	}
	_ = os.WriteFile(filepath.Join(dir, "Laverne & Shirley S05E10.en.srt"), []byte("1\n00:00:01,000 --> 00:00:02,000\nHi\n"), 0o644)
	g := newGUIServer()
	g.prefsPath = filepath.Join(t.TempDir(), "settings.json")
	_ = g.setTMDBKey("k3y")
	g.renAddPaths(paths)
	var sug []doubleSuggestion
	for i := 0; i < 200 && len(sug) == 0; i++ {
		renWait(t, g)
		sug, _ = g.renList()["doubles"].([]doubleSuggestion)
		time.Sleep(30 * time.Millisecond)
	}
	if len(sug) != 1 || sug[0].At != 9 || sug[0].Later != 3 || sug[0].Title != "We're in the Army, Now (Parts 1 & 2)" {
		t.Fatalf("suggestion: %+v", sug)
	}
	if r := g.renAnswer(sug[0].Key, 9, true); r["ok"] != true {
		t.Fatalf("answer: %v", r)
	}
	rows := renWait(t, g)
	want := map[string]string{
		"Laverne & Shirley S05E08.mkv": "Laverne & Shirley (1976).S05E08.Episode 8.480p.MPEG4",
		"Laverne & Shirley S05E09.mkv": "Laverne & Shirley (1976).S05E09-E10.We're in the Army, Now.480p.MPEG4",
		"Laverne & Shirley S05E10.mkv": "Laverne & Shirley (1976).S05E11.Take Two, They're Small.480p.MPEG4",
		"Laverne & Shirley S05E12.mkv": "Laverne & Shirley (1976).S05E13.Episode 13.480p.MPEG4",
	}
	var ids []int
	for _, r := range rows {
		if w, ok := want[r.Name]; ok && r.Proposed != w {
			t.Errorf("%s → %q, want %q", r.Name, r.Proposed, w)
		}
		if r.Name == "Laverne & Shirley S05E10.mkv" && r.Mapped != "TMDB episode 11" {
			t.Errorf("mapped: %q", r.Mapped)
		}
		if r.State != "ready" && r.State != "same" {
			t.Errorf("%s: %s %s", r.Name, r.State, r.Why)
		}
		ids = append(ids, r.ID)
	}
	// renaming the season never overwrites: S05E10 → S05E11 waits for S05E11 → S05E12
	if r := g.renApply(ids); r["renamed"] != 6 || len(r["skipped"].([]string)) != 0 {
		t.Fatalf("apply: %v", r)
	}
	for _, n := range []string{"Laverne & Shirley (1976).S05E09-E10.We're in the Army, Now.480p.MPEG4.mkv",
		"Laverne & Shirley (1976).S05E11.Take Two, They're Small.480p.MPEG4.mkv",
		"Laverne & Shirley (1976).S05E11.Take Two, They're Small.480p.MPEG4.en.srt",
		"Laverne & Shirley (1976).S05E13.Episode 13.480p.MPEG4.mkv"} {
		if !fileExists(filepath.Join(dir, n)) {
			t.Errorf("missing: %s", n)
		}
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "Laverne & Shirley S05*")); len(left) != 0 {
		t.Errorf("left with old names: %v", left)
	}
	// renamed: each file's own name now has TMDB's numbers, so nothing is shifted any more
	g.renLookup(nil, nil)
	for _, r := range renWait(t, g) {
		if r.State != "same" || r.Mapped != "" {
			t.Errorf("after renaming %s: %s %q %s", r.Name, r.State, r.Mapped, r.Proposed)
		}
	}
	if r := g.renUndo(); r["restored"] != 6 {
		t.Fatalf("undo: %v", r)
	}
	for _, p := range paths {
		if !fileExists(p) {
			t.Errorf("not put back: %s", filepath.Base(p))
		}
	}
	// after renaming, the names say it themselves: "S05E09-E10" is read as both episodes
	if gs := guessSearch(filepath.Join(dir, "Laverne & Shirley (1976).S05E09-E10.We're in the Army, Now.480p.MPEG4.mkv"), ""); gs.Episode != 9 || gs.Episode2 != 10 {
		t.Errorf("range name: %+v", gs)
	}
}

func TestTargetSizeForDoubles(t *testing.T) {
	cfg := &Config{Input: filepath.Join("TV", "Laverne & Shirley (1976).S05E09-E10.We're in the Army, Now.mkv"), TargetBytes: 500 << 20}
	if note := scaleTarget(cfg); cfg.TargetBytes != 1000<<20 || !strings.Contains(note, "doubled") || !strings.Contains(note, "E09-E10") {
		t.Errorf("range name: %d %q", cfg.TargetBytes, note)
	}
	cfg = &Config{Input: filepath.Join("TV", "Laverne & Shirley S05E09.mkv"), TargetBytes: 500 << 20}
	if note := scaleTarget(cfg); note != "" || cfg.TargetBytes != 500<<20 {
		t.Errorf("single episode changed: %q", note)
	}
	cfg.SizeFactor = 2 // marked on the Plex names tab
	if note := scaleTarget(cfg); cfg.TargetBytes != 1000<<20 || !strings.Contains(note, "marked as a double") {
		t.Errorf("marked: %d %q", cfg.TargetBytes, note)
	}
	cfg = &Config{Input: filepath.Join("TV", "Show.S02E05.Big.1080p.HEVC-pt2.mkv"), TargetBytes: 800 << 20}
	if scaleTarget(cfg); cfg.TargetBytes != 400<<20 {
		t.Errorf("split half: %d", cfg.TargetBytes)
	}
	g := newGUIServer()
	g.ren.shifts = map[string][]epShift{shiftKey(tmdbPickKey(guessSearch("Laverne & Shirley S05E09.mkv", "")), 5): {{At: 9, Kind: "double"}}}
	if f := g.renSizeFactor("Laverne & Shirley S05E09.mkv"); f != 2 {
		t.Errorf("marked factor: %v", f)
	}
	if f := g.renSizeFactor("Laverne & Shirley S05E10.mkv"); f != 0 {
		t.Errorf("next file's factor: %v", f)
	}
	if a := strings.Join((&Config{Input: "x.mkv", Output: "y.mkv", TargetBytes: 1 << 30, SizeFactor: 2}).cliArgs(), " "); !strings.Contains(a, "-size-episodes 2") {
		t.Errorf("cli: %s", a)
	}
}
