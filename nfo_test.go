package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// What TMDB gives becomes the NFO Plex reads: a film, and an episode.
func TestNFOFromTMDB(t *testing.T) {
	newFakeTMDB(t, []byte("\xff\xd8\xff\xe0jpeg"))
	ctx := context.Background()
	f, err := tmdbDetails(ctx, "k", "movie", 949, 0, 0, t.TempDir())
	if err != nil || f.NFO == nil || f.NFO.Movie == nil {
		t.Fatalf("%+v %v", f, err)
	}
	b, err := nfoXML(f.NFO)
	if err != nil {
		t.Fatal(err)
	}
	x := string(b)
	for _, want := range []string{
		`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`, "<movie>", "<title>Heat</title>", "<plot>Cops and robbers.</plot>",
		"<tagline>A Los Angeles crime saga</tagline>", `<rating name="themoviedb" max="10" default="true">`, "<value>7.9</value>",
		"<votes>7000</votes>", "<mpaa>R</mpaa>", "<premiered>1995-12-15</premiered>", "<studio>Regency Enterprises</studio>",
		"<genre>Action</genre>", "<genre>Crime</genre>", "<country>United States of America</country>",
		`<uniqueid type="tmdb" default="true">949</uniqueid>`, `<uniqueid type="imdb">tt0113277</uniqueid>`,
		"<credits>Michael Mann</credits>", "<director>Michael Mann</director>",
	} {
		if !strings.Contains(x, want) {
			t.Errorf("film NFO lacks %s:\n%s", want, x)
		}
	}
	for _, not := range []string{"<actor>", "Al Pacino", "Art Linson", "<originaltitle>", "<set>"} {
		if strings.Contains(x, not) {
			t.Errorf("film NFO has %s:\n%s", not, x)
		}
	}

	f, err = tmdbDetails(ctx, "k", "tv", 136315, 2, 3, t.TempDir())
	if err != nil || f.NFO == nil || len(f.NFO.Episodes) != 1 {
		t.Fatalf("%+v %v", f, err)
	}
	b, _ = nfoXML(f.NFO)
	x = string(b)
	for _, want := range []string{"<episodedetails>", "<title>Sundae</title>", "<season>2</season>", "<episode>3</episode>",
		"<plot>Sydney eats.</plot>", "<aired>2023-06-22</aired>", "<director>Ramy Youssef</director>", "<credits>Catherine Schetina</credits>"} {
		if !strings.Contains(x, want) {
			t.Errorf("episode NFO lacks %s:\n%s", want, x)
		}
	}
	if strings.Contains(x, "Someone") || strings.Contains(x, "<actor>") {
		t.Errorf("episode NFO has the cast:\n%s", x)
	}
}

// A double episode: one block per episode; an NFO already there is kept.
func TestWriteNFO(t *testing.T) {
	dir := t.TempDir()
	video := filepath.Join(dir, "Show (2001).S05E09-E10.Name.1080p.HEVC.mkv")
	d := &nfoDoc{Episodes: []nfoEpisode{{Title: "Part 1", Season: 5, Episode: 9}, {Title: "Part 2", Season: 5, Episode: 10, Plot: "A & B <c>"}}}
	msg, err := writeNFO(video, d)
	if err != nil || !strings.Contains(msg, "2 episodes") {
		t.Fatalf("%q %v", msg, err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "Show (2001).S05E09-E10.Name.1080p.HEVC.nfo"))
	x := string(b)
	if strings.Count(x, "<episodedetails>") != 2 || strings.Count(x, "<?xml") != 1 || !strings.Contains(x, "<episode>10</episode>") ||
		!strings.Contains(x, "A &amp; B &lt;c&gt;") {
		t.Errorf("double episode:\n%s", x)
	}
	msg, err = writeNFO(video, &nfoDoc{Episodes: []nfoEpisode{{Title: "Other", Season: 1, Episode: 1}}})
	b2, _ := os.ReadFile(nfoPath(video))
	if err != nil || !strings.Contains(msg, "already there") || string(b2) != x {
		t.Errorf("an NFO already there was changed: %q %v", msg, err)
	}
	if msg, _ := writeNFO(filepath.Join(dir, "x.mkv"), nil); !strings.Contains(msg, "not written") {
		t.Errorf("no details: %q", msg)
	}
}

// The queue writes the NFO beside the finished file, only with the setting on.
func TestQueueWritesNFO(t *testing.T) {
	fakeTools(t)
	dir := t.TempDir()
	in := filepath.Join(dir, "heat.mkv")
	_ = os.WriteFile(in, []byte("video"), 0o644)
	for _, on := range []bool{false, true} {
		g := newGUIServer()
		g.prefsPath = filepath.Join(t.TempDir(), "settings.json")
		if on {
			_ = g.savePref("nfoFiles", json.RawMessage("true"))
		}
		out := filepath.Join(dir, map[bool]string{false: "off.mkv", true: "on.mkv"}[on])
		var m map[string]any
		_ = json.Unmarshal(itemSettings(in, out), &m)
		m["nfo"] = map[string]any{"movie": map[string]any{"title": "Heat", "tmdb": 949}}
		raw, _ := json.Marshal(m)
		if ids, errs := g.queueAdd([]json.RawMessage{raw}); len(ids) != 1 || len(errs) != 0 {
			t.Fatalf("add: %v %v", ids, errs)
		}
		waitFor(t, "checked", func() bool { return !strings.Contains(strings.Join(statuses(g), " "), stChecking) })
		if err := g.startQueue(); err != nil {
			t.Fatal(err)
		}
		waitFor(t, "the queue to finish", func() bool { g.mu.Lock(); defer g.mu.Unlock(); return !g.q.running })
		b, err := os.ReadFile(nfoPath(out))
		switch {
		case on && (err != nil || !strings.Contains(string(b), `<uniqueid type="tmdb" default="true">949</uniqueid>`)):
			t.Errorf("setting on: %v %s (%s)", err, b, strings.Join(statuses(g), " "))
		case !on && err == nil:
			t.Errorf("setting off, but an NFO was written")
		}
	}
}
