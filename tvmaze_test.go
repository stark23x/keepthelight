package main

import (
	"context"
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

// fakeTVmaze answers like TVmaze for show 7 (TVDB 77777); eps nil: it knows nothing.
func fakeTVmaze(t *testing.T, eps []tvmazeEp) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case eps == nil:
			w.WriteHeader(404)
		case r.URL.Path == "/lookup/shows" && r.URL.Query().Get("thetvdb") == "77777":
			http.Redirect(w, r, "/shows/7", http.StatusMovedPermanently)
		case r.URL.Path == "/shows/7":
			_, _ = w.Write([]byte(`{"id":7,"name":"Laverne & Shirley"}`))
		case r.URL.Path == "/shows/7/episodes":
			b, _ := json.Marshal(eps)
			_, _ = w.Write(b)
		case r.URL.Path == "/singlesearch/shows" && strings.EqualFold(r.URL.Query().Get("q"), "Obscure Show"),
			r.URL.Path == "/shows/8":
			_, _ = w.Write([]byte(`{"id":8,"name":"Obscure Show","premiered":"2019-03-01","summary":"<p>Only on TVmaze.</p>","url":"https://www.tvmaze.com/shows/8"}`))
		case r.URL.Path == "/search/shows":
			_, _ = w.Write([]byte(`[{"score":0.9,"show":{"id":8,"name":"Obscure Show","premiered":"2019-03-01","summary":"<p>Only on TVmaze.</p>"}}]`))
		case r.URL.Path == "/shows/8/episodes":
			_, _ = w.Write([]byte(`[{"season":1,"number":1,"name":"Pilot","airdate":"2019-03-01","runtime":30,"summary":"<p>It starts.</p>"},{"season":1,"number":2,"name":"Second","airdate":"2019-03-08","runtime":30}]`))
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	old := tvmazeAPI
	tvmazeAPI = srv.URL
	tvmaze.mu.Lock()
	tvmaze.shows, tvmaze.eps, tvmaze.at, tvmaze.calls = map[string]int{}, map[int][]tvmazeEp{}, map[string]time.Time{}, nil // and no waiting for earlier tests' calls
	tvmaze.mu.Unlock()
	t.Cleanup(func() { tvmazeAPI = old })
}

// Laverne & Shirley season 5 the way TVmaze (like TVDB) has it: the hour-long
// "We're in the Army, Now" is one episode, so there are 23.
func laverneTVmaze() []tvmazeEp {
	var eps []tvmazeEp
	for n := 1; n <= 23; n++ {
		e := tvmazeEp{Season: 5, Number: n, Runtime: 25, Name: fmt.Sprintf("Episode %d", n), Summary: fmt.Sprintf("<p>Story %d.</p>", n)}
		switch {
		case n == 9:
			e.Name, e.Runtime = "We're in the Army, Now", 50
		case n == 10:
			e.Name = "Take Two, They're Small"
		case n > 10:
			e.Name = fmt.Sprintf("Episode %d", n+1) // TMDB's next ones, one number lower
		}
		eps = append(eps, e)
	}
	eps = append(eps, tvmazeEp{Season: 5, Number: 0, Name: "A special"}, tvmazeEp{Season: 4, Number: 9, Name: "Other season"})
	return eps
}

func TestCompareNumbering(t *testing.T) {
	var tmdb []tmdbEpisode
	for n := 1; n <= 24; n++ {
		name := fmt.Sprintf("Episode %d", n)
		switch n {
		case 9, 10:
			name = fmt.Sprintf("We're in the Army, Now (%d)", n-8)
		case 11:
			name = "Take Two, They're Small"
		}
		tmdb = append(tmdb, tmdbEpisode{Number: n, Name: name})
	}
	var tvm []tvmazeEp
	for _, e := range laverneTVmaze() {
		if e.Season == 5 && e.Number > 0 {
			tvm = append(tvm, e)
		}
	}
	differ, rows := compareNumbering(tmdb, tvm, map[int]bool{9: true, 10: true})
	if !differ || len(rows) != 24 {
		t.Fatalf("differ %v, %d rows", differ, len(rows))
	}
	if !rows[9].Differ || !rows[9].File || rows[9].TMDB != "We're in the Army, Now (2)" || rows[9].TVmaze != "Take Two, They're Small" {
		t.Errorf("row 10: %+v", rows[9])
	}
	if rows[8].Differ { // "We're in the Army, Now (1)" and "We're in the Army, Now" are the same episode
		t.Errorf("row 9: %+v", rows[8])
	}
	// the same numbering, small differences in the titles: no disagreement
	same := []tvmazeEp{{Number: 1, Name: "Pilot"}, {Number: 2, Name: "The Army, Part 1"}, {Number: 3, Name: "Chapter 3"}}
	if d, _ := compareNumbering([]tmdbEpisode{{Number: 1, Name: "Pilot!"}, {Number: 2, Name: "The Army (1)"}, {Number: 3, Name: "Goodbye"}}, same, nil); d {
		t.Error("the same numbering reported as different")
	}
	if got := plainText("<p>Laverne &amp; Shirley <b>join</b> the army.</p>"); got != "Laverne & Shirley join the army." {
		t.Errorf("plainText: %q", got)
	}
	if f := tvmazeLengthFactor(tvm, 9); f != 2 {
		t.Errorf("length factor of the hour-long one: %v", f)
	}
	if f := tvmazeLengthFactor(tvm, 10); f != 0 {
		t.Errorf("length factor of a normal one: %v", f)
	}
}

// The Plex names tab: TMDB and TVmaze disagree about season 5; with TVmaze's
// numbering, the files keep their numbers and get TVmaze's titles.
func TestTVmazeNumbering(t *testing.T) {
	ffmpeg, e1 := exec.LookPath("ffmpeg")
	_, e2 := exec.LookPath("ffprobe")
	if e1 != nil || e2 != nil {
		t.Skip("needs ffmpeg")
	}
	t.Setenv("LOCALAPPDATA", t.TempDir())
	laverneTMDB(t)
	fakeTVmaze(t, laverneTVmaze())
	dir := t.TempDir()
	var paths []string
	for n := 7; n <= 12; n++ {
		p := filepath.Join(dir, fmt.Sprintf("Laverne & Shirley S05E%02d.mkv", n))
		d := "2"
		if n == 9 {
			d = "4"
		}
		if b, err := exec.Command(ffmpeg, "-v", "error", "-y", "-f", "lavfi", "-i", "testsrc=s=640x480:r=10:d="+d, "-c:v", "mpeg4", p).CombinedOutput(); err != nil {
			t.Fatalf("%v %s", err, b)
		}
		paths = append(paths, p)
	}
	g := newGUIServer()
	g.prefsPath = filepath.Join(t.TempDir(), "settings.json")
	_ = g.setTMDBKey("k3y")
	g.renAddPaths(paths)
	var sug []numberingSuggestion
	for i := 0; i < 200 && len(sug) == 0; i++ {
		renWait(t, g)
		sug, _ = g.renList()["numbering"].([]numberingSuggestion)
		time.Sleep(30 * time.Millisecond)
	}
	if len(sug) != 1 || sug[0].Season != 5 || sug[0].TMDB != 24 || sug[0].TVmaze != 23 {
		t.Fatalf("suggestion: %+v", sug)
	}
	if st, _ := g.renList()["tvmaze"].([]tvmStatus); len(st) != 1 || st[0].Kind != "differ" {
		t.Errorf("status: %+v", st)
	}
	if r := g.renNumbering(sug[0].Key, "tvmaze"); r["ok"] != true {
		t.Fatalf("choose: %v", r)
	}
	time.Sleep(50 * time.Millisecond)
	rows := renWait(t, g)
	want := map[string]string{
		"Laverne & Shirley S05E09.mkv": "Laverne & Shirley (1976).S05E09.We're in the Army, Now.480p.MPEG4",
		"Laverne & Shirley S05E10.mkv": "Laverne & Shirley (1976).S05E10.Take Two, They're Small.480p.MPEG4",
		"Laverne & Shirley S05E12.mkv": "Laverne & Shirley (1976).S05E12.Episode 13.480p.MPEG4",
	}
	for _, r := range rows {
		if w, ok := want[r.Name]; ok && r.Proposed != w {
			t.Errorf("%s → %q, want %q (%s)", r.Name, r.Proposed, w, r.Note)
		}
		if r.Name == "Laverne & Shirley S05E10.mkv" && !strings.Contains(r.Note, "TVmaze") {
			t.Errorf("note: %q", r.Note)
		}
	}
	l := g.renList()
	if d, _ := l["doubles"].([]doubleSuggestion); len(d) != 0 {
		t.Errorf("no double episode to suggest with TVmaze's numbering: %+v", d)
	}
	if n, _ := l["numbering"].([]numberingSuggestion); len(n) != 0 {
		t.Errorf("answered: %+v", n)
	}
	if f := g.renSizeFactor(paths[2]); f != 2 { // S05E09: one long episode on TVmaze
		t.Errorf("size factor: %v", f)
	}
	for i := 0; i < 100; i++ { // looked at again: the status follows the choice
		if st, _ := g.renList()["tvmaze"].([]tvmStatus); len(st) == 1 && st[0].Kind == "chosen" {
			break
		} else if i == 99 {
			t.Errorf("status after choosing: %+v", st)
		}
		time.Sleep(20 * time.Millisecond)
	}
	// the choice is remembered, and can be undone
	if g.tvNumbering()[sug[0].Key] != "tvmaze" {
		t.Error("not remembered")
	}
	g.renNumbering(sug[0].Key, "tmdb")
	time.Sleep(50 * time.Millisecond)
	for _, r := range renWait(t, g) {
		if r.Name == "Laverne & Shirley S05E10.mkv" && !strings.Contains(r.Proposed, "S05E10.We're in the Army, Now") {
			t.Errorf("back to TMDB: %q", r.Proposed)
		}
	}
	_ = os.Remove(dir)
}

// TMDB has no such show: TVmaze fills the file in by itself, and its shows are
// offered in the search.
func TestTVmazeFallback(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/3/search/tv" {
			_, _ = w.Write([]byte(`{"results":[]}`))
			return
		}
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"status_message":"not found"}`))
	}))
	t.Cleanup(srv.Close)
	oldAPI := tmdbAPI
	tmdbAPI = srv.URL + "/3"
	t.Cleanup(func() { tmdbAPI = oldAPI })
	fakeTVmaze(t, laverneTVmaze())

	lk := &tmdbLookup{key: "k3y", found: map[string]*tmdbFound{}, tvmaze: true}
	note, m := lk.file(filepath.Join(t.TempDir(), "Obscure Show S01E02.mkv"))
	if m == nil || m.plexName != "Obscure Show (2019).S01E02.Second."+plexDetailsMark || !strings.Contains(note, "from TVmaze") {
		t.Fatalf("fallback: %q %+v", note, m)
	}
	if m.Tags["title"] != "Obscure Show – S01E02 – Second" || m.Tags["date"] != "2019-03-08" {
		t.Errorf("tags: %v", m.Tags)
	}
	// TVmaze turned off: TMDB alone, as before
	lk = &tmdbLookup{key: "k3y", found: map[string]*tmdbFound{}}
	if note, m := lk.file(filepath.Join(t.TempDir(), "Obscure Show S01E02.mkv")); m != nil || !strings.HasPrefix(note, "TMDB: nothing found") {
		t.Errorf("off: %q %+v", note, m)
	}
	// the search offers TVmaze's shows (marked), and they can be filled in from
	g := newGUIServer()
	g.prefsPath = filepath.Join(t.TempDir(), "settings.json")
	_ = g.setTMDBKey("k3y")
	r := g.tmdbCall(context.Background(), "tmdbSearch", []byte(`{"kind":"tv","query":"Obscure Show"}`))
	res, _ := r["results"].([]tmdbResult)
	if r["ok"] != true || len(res) != 1 || res[0].Kind != "tvmaze" || res[0].ID != 8 || res[0].Year != "2019" {
		t.Fatalf("search: %v", r)
	}
	d := g.tmdbCall(context.Background(), "tmdbDetails", []byte(`{"kind":"tvmaze","id":8,"season":1,"episode":1}`))
	if f, _ := d["fill"].(*tmdbFill); d["ok"] != true || f == nil || f.Title != "Obscure Show – S01E01 – Pilot" || f.Description != "It starts." {
		t.Errorf("details: %v", d)
	}
	if c := g.tmdbChoose("x", tmdbPick{Kind: "tvmaze", ID: 8}); c["error"] == "nothing chosen" {
		t.Error("a TVmaze show cannot be chosen")
	}
}
