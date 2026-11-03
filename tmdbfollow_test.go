package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// Several shows called "The Office": one episode matched by hand, the others follow,
// each with its own season and episode; the title is searched once, not once per file.
func TestTMDBMatchOneTheRestFollow(t *testing.T) {
	fakeTools(t)
	t.Setenv("LOCALAPPDATA", t.TempDir())
	var searches int32
	officeTMDB(t, &searches)

	dir := t.TempDir()
	var files []string
	for _, n := range []string{"The.Office.S01E01.mkv", "The.Office.S01E02.mkv", "The.Office.S02E03.mkv"} {
		p := filepath.Join(dir, n)
		_ = os.WriteFile(p, []byte("x"), 0o644)
		files = append(files, p)
	}
	g := newGUIServer()
	g.prefsPath = filepath.Join(t.TempDir(), "settings.json")
	_ = g.setTMDBKey("k3y")
	var raws []json.RawMessage
	for i, f := range files {
		raws = append(raws, itemSettings(f, filepath.Join(dir, "out"+itoa(i)+"_KTL.mkv")))
	}
	ids, errs := g.queueAdd(raws)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	note := func(id int) (string, *MetaEdit) {
		g.mu.Lock()
		defer g.mu.Unlock()
		var m struct {
			TMDBNote string    `json:"tmdbNote"`
			Meta     *MetaEdit `json:"meta"`
		}
		_ = json.Unmarshal(g.findItemLocked(id).Raw, &m)
		return m.TMDBNote, m.Meta
	}
	done := func(ids ...int) func() bool {
		return func() bool {
			for _, id := range ids {
				if n, _ := note(id); strings.Contains(n, "looking") {
					return false
				}
			}
			return true
		}
	}
	g.queueTMDB(nil)
	waitFor(t, "the first lookup", done(ids...))
	for _, id := range ids {
		if n, m := note(id); !strings.Contains(n, "needs a match") || m != nil {
			t.Errorf("two shows: %s %+v", n, m)
		}
	}
	if n := atomic.LoadInt32(&searches); n != 1 {
		t.Errorf("searched %d times, want once for the show", n)
	}

	// the first episode is matched by hand (Edit → TMDB → Use → Save)
	r := g.tmdbCall(context.Background(), "tmdbRemember", []byte(`{"path":`+jsonStr(files[0])+`,"kind":"tv","id":2316,"title":"The Office"}`))
	if r["ok"] != true {
		t.Fatalf("remember: %v", r)
	}
	f := g.queueTMDBFollow(ids[0])
	if f["files"] != 2 || f["title"] != "The Office" {
		t.Fatalf("follow: %v", f)
	}
	waitFor(t, "the others", done(ids[1], ids[2]))
	for i, want := range map[int]string{1: "S01E02", 2: "S02E03"} {
		n, m := note(ids[i])
		if m == nil || !strings.Contains(m.Tags["title"], want) || !strings.Contains(n, "as matched before") {
			t.Errorf("file %d: %s %+v", i, n, m)
		}
	}
	if n, _ := note(ids[0]); !strings.Contains(n, "needs a match") {
		t.Errorf("the file matched by hand is left to its own edit: %s", n)
	}
	// remembered: a new season's file matches straight away, without searching
	before := atomic.LoadInt32(&searches)
	if n, m := tmdbLookupFileWith(g, filepath.Join(dir, "The Office S03E01.mkv")); m == nil || !strings.Contains(m.Tags["title"], "S03E01") {
		t.Errorf("next season: %s %+v", n, m)
	}
	if atomic.LoadInt32(&searches) != before {
		t.Error("a remembered show was searched again")
	}
	g.tmdbCall(context.Background(), "tmdbForget", []byte(`{}`))
	if len(g.tmdbPicks()) != 0 {
		t.Error("not forgotten")
	}
	// films: the year is part of the title, so "Heat (1995)" does not answer for "Heat (1972)"
	if tmdbPickKey(tmdbGuess{Kind: "movie", Query: "Heat", Year: "1995"}) == tmdbPickKey(tmdbGuess{Kind: "movie", Query: "Heat", Year: "1972"}) {
		t.Error("films of the same name share a key")
	}
}

func tmdbLookupFileWith(g *guiServer, input string) (string, *MetaEdit) {
	lk := &tmdbLookup{key: g.tmdbKey(), coverDir: filepath.Join(appDataDir(), "covers"), picks: g.tmdbPicks(), found: map[string]*tmdbFound{}}
	return lk.file(input)
}

func jsonStr(s string) string { b, _ := json.Marshal(s); return string(b) }

// officeTMDB is a stand-in TMDB with two shows called "The Office" (searches are counted).
func officeTMDB(t *testing.T, searches *int32) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/3/search/tv":
			atomic.AddInt32(searches, 1)
			_, _ = w.Write([]byte(`{"results":[{"id":2316,"name":"The Office","first_air_date":"2005-03-24"},{"id":2996,"name":"The Office","first_air_date":"2001-07-09"}]}`))
		case r.URL.Path == "/3/tv/2316":
			_, _ = w.Write([]byte(`{"name":"The Office","first_air_date":"2005-03-24","overview":"Paper."}`))
		case strings.HasPrefix(r.URL.Path, "/3/tv/2316/season/") && strings.Contains(r.URL.Path, "/episode/"):
			ep := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
			_, _ = w.Write([]byte(`{"name":"Episode ` + ep + `","air_date":"2005-04-0` + ep + `","overview":"Things happen."}`))
		case strings.HasPrefix(r.URL.Path, "/3/tv/2316/season/"):
			_, _ = w.Write([]byte(`{}`))
		default:
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"status_message":"not found"}`))
		}
	}))
	t.Cleanup(srv.Close)
	oldAPI, oldImg := tmdbAPI, tmdbImages
	tmdbAPI, tmdbImages = srv.URL+"/3", srv.URL+"/img/"
	t.Cleanup(func() { tmdbAPI, tmdbImages = oldAPI, oldImg })
}

// The "Which one?" window: one question per title, and the choice fills in every file with it.
func TestTMDBWhichOne(t *testing.T) {
	fakeTools(t)
	t.Setenv("LOCALAPPDATA", t.TempDir())
	var searches int32
	officeTMDB(t, &searches)
	dir := t.TempDir()
	var raws []json.RawMessage
	for i, n := range []string{"The.Office.S01E01.mkv", "The.Office.S01E02.mkv", "The Office S03E04.mkv"} {
		p := filepath.Join(dir, n)
		_ = os.WriteFile(p, []byte("x"), 0o644)
		raws = append(raws, itemSettings(p, filepath.Join(dir, "o"+itoa(i)+"_KTL.mkv")))
	}
	g := newGUIServer()
	g.prefsPath = filepath.Join(t.TempDir(), "settings.json")
	_ = g.setTMDBKey("k3y")
	ids, _ := g.queueAdd(raws)
	g.queueTMDB(nil)
	waitFor(t, "the lookup", func() bool { a := g.tmdbUnsettled(); return len(a) == 1 && a[0].Files == 3 })
	ask := g.tmdbUnsettled()[0]
	if ask.Kind != "tv" || ask.Query != "The Office" || ask.None {
		t.Errorf("ask: %+v", ask)
	}
	if r := g.tmdbChoose("tv|nothing", tmdbPick{Kind: "tv", ID: 2316}); r["ok"] != false {
		t.Error("an unknown title was accepted")
	}
	r := g.tmdbChoose(ask.Key, tmdbPick{Kind: "tv", ID: 2316, Title: "The Office (US)"})
	if r["files"] != 3 || r["title"] != "The Office (US)" {
		t.Fatalf("choose: %v", r)
	}
	waitFor(t, "the files to be filled in", func() bool {
		g.mu.Lock()
		defer g.mu.Unlock()
		for _, it := range g.q.items {
			if strings.Contains(string(it.Raw), "looking up") {
				return false
			}
		}
		return true
	})
	g.mu.Lock()
	defer g.mu.Unlock()
	for i, want := range []string{"S01E01", "S01E02", "S03E04"} {
		it := g.findItemLocked(ids[i])
		var m struct {
			Meta *MetaEdit `json:"meta"`
		}
		_ = json.Unmarshal(it.Raw, &m)
		if m.Meta == nil || !strings.Contains(m.Meta.Tags["title"], want) {
			t.Errorf("file %d: %+v", i, m.Meta)
		}
	}
	if _, ok := g.tmdbPicks()[ask.Key]; !ok {
		t.Error("the choice was not remembered")
	}
}
