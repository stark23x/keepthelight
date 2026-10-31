package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Each kind of dialog opens where it was last used; what is typed in the box wins.
func TestBrowseRemembersFolders(t *testing.T) {
	dir := t.TempDir()
	g := newGUIServer()
	g.prefsPath = filepath.Join(dir, "settings.json")
	shows, films, gone := filepath.Join(dir, "TV"), filepath.Join(dir, "Films"), filepath.Join(dir, "unplugged")
	for _, d := range []string{shows, films} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if got := g.startAt("videos", "", false); got != "" {
		t.Errorf("nothing remembered yet: %q", got)
	}
	g.rememberFolder("videos", shows)
	g.rememberFolder("scan", films)
	if got := filepath.Dir(g.startAt("videos", "", false)); got != shows {
		t.Errorf("videos: %q", got)
	}
	if got := g.startAt("scan", "", true); got != films {
		t.Errorf("scan: %q", got)
	}
	if got := g.startAt("output", "", true); got != "" {
		t.Errorf("each kind has its own folder: %q", got)
	}
	typed := filepath.Join(films, "Heat (1995).mkv")
	if got := g.startAt("videos", typed, false); got != typed {
		t.Errorf("a typed path wins: %q", got)
	}
	if got := filepath.Dir(g.startAt("videos", filepath.Join(gone, "x.mkv"), false)); got != shows {
		t.Errorf("a typed path in a missing folder: %q", got)
	}
	if got := g.startAt("output", "", false); got != "" {
		t.Errorf("output: %q", got)
	}
	g.rememberFolder("output", films)
	if got := g.startAt("output", filepath.Join(gone, "film_KTL.mkv"), false); got != filepath.Join(films, "film_KTL.mkv") {
		t.Errorf("a save dialog keeps its name: %q", got)
	}
	g.rememberFolder("profiles", gone) // remembered, but the drive is gone
	if got := g.startAt("profiles", "", false); got != "" {
		t.Errorf("missing remembered folder: %q", got)
	}
	// remembered after a restart
	g2 := newGUIServer()
	g2.prefsPath = g.prefsPath
	if got := g2.lastFolder("scan"); got != films {
		t.Errorf("after a restart: %q", got)
	}
	for kind, want := range map[string]string{"openMulti": "videos", "folderFiles": "videos", "save": "output", "wav": "sounds", "saveJson": "profiles"} {
		if got := browseGroup(kind, ""); got != want {
			t.Errorf("%s: %q", kind, got)
		}
	}
	if browseGroup("folder", "scan") != "scan" || browseGroup("folder", "") != "folders" {
		t.Error("folder groups")
	}
}
