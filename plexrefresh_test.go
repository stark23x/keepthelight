package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestPlexScansFor(t *testing.T) {
	secs := []plexSection{
		{Key: "1", Title: "Movies", Locations: []string{`D:\PlexRaid01\Movies`}},
		{Key: "2", Title: "TV Shows", Locations: []string{`D:\PlexRaid02\TV`}},
		{Key: "3", Title: "Old Movies", Locations: []string{`E:\Movies`}},
	}
	// the share name and folder both match the first library: it wins over "E:\Movies"
	got := plexScansFor(secs, []string{`\\DELL\PlexRaid01\Movies\Heat (1995)\Heat (1995).mkv`})
	if len(got) != 1 || got[0].Key != "1" || got[0].Path != `D:\PlexRaid01\Movies\Heat (1995)` {
		t.Errorf("by share: %+v", got)
	}
	// a mapped drive (W:) only says "Movies": both Movies libraries are asked
	got = plexScansFor(secs, []string{`W:\Movies\Heat (1995)\Heat.mkv`})
	if len(got) != 2 {
		t.Errorf("by folder name: %+v", got)
	}
	// episodes of one season: one scan
	got = plexScansFor(secs, []string{`T:\TV\The Office\Season 1\a.mkv`, `T:\TV\The Office\Season 1\b.mkv`})
	if len(got) != 1 || got[0].Path != `D:\PlexRaid02\TV\The Office\Season 1` {
		t.Errorf("a season: %+v", got)
	}
	if len(plexScansFor(secs, []string{`C:\Users\me\Desktop\clip.mkv`})) != 0 {
		t.Error("a file outside every library matched")
	}
	if normPlexURL("192.168.1.10") != "http://192.168.1.10:32400" || normPlexURL("https://plex.local:32400/") != "https://plex.local:32400" {
		t.Error("addresses")
	}
}

// A finished queue asks Plex to scan the new files' folders.
func TestQueueRefreshesPlex(t *testing.T) {
	fakeTools(t)
	var mu sync.Mutex
	var refreshed []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Plex-Token") != "tok" {
			w.WriteHeader(401)
			return
		}
		switch {
		case r.URL.Path == "/":
			_, _ = w.Write([]byte(`{"MediaContainer":{"friendlyName":"DELL"}}`))
		case r.URL.Path == "/library/sections":
			_, _ = w.Write([]byte(`{"MediaContainer":{"Directory":[{"key":"7","title":"Films","type":"movie","Location":[{"path":"/srv/media/` + "q" + `"}]}]}}`))
		case strings.HasSuffix(r.URL.Path, "/refresh"):
			mu.Lock()
			refreshed = append(refreshed, r.URL.Path+"?"+r.URL.Query().Get("path"))
			mu.Unlock()
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	g := newGUIServer()
	g.prefsPath = filepath.Join(t.TempDir(), "settings.json")
	if err := g.setPlexServer(srv.URL, "tok"); err != nil {
		t.Fatal(err)
	}
	if msg, err := plexCheck(context.Background(), srv.URL, "tok"); err != nil || !strings.Contains(msg, "DELL: 1 libraries (Films)") {
		t.Errorf("check: %q %v", msg, err)
	}
	if _, err := plexCheck(context.Background(), srv.URL, "wrong"); err == nil || !strings.Contains(err.Error(), "token") {
		t.Errorf("a wrong token: %v", err)
	}
	// the files are written to a folder called "q", which the "Films" library holds
	dir := filepath.Join(t.TempDir(), "q")
	runToEndIn(t, g, dir, "one", "two")
	waitFor(t, "the refresh", func() bool { mu.Lock(); defer mu.Unlock(); return len(refreshed) > 0 })
	mu.Lock()
	defer mu.Unlock()
	if len(refreshed) != 1 || refreshed[0] != "/library/sections/7/refresh?/srv/media/q" {
		t.Errorf("refreshed: %v", refreshed)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if !strings.Contains(g.q.notice, "Plex is scanning the new files (Films)") {
		t.Errorf("notice: %q", g.q.notice)
	}
}
