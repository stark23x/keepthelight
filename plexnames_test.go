package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlexNameFormats(t *testing.T) {
	if got := plexFilmName("Mission: Impossible", "1996", 954); got != "Mission- Impossible (1996) {tmdb-954}" {
		t.Errorf("film: %q", got)
	}
	if got := plexEpisodeName("The Office", "2005", 1, 2, "Diversity Day"); got != "The Office (2005) - S01E02 - Diversity Day" {
		t.Errorf("episode: %q", got)
	}
	if got := plexEpisodeName("What/If?", "", 3, 10, `"Pilot"`); got != "WhatIf - S03E10 - Pilot" {
		t.Errorf("cleaned: %q", got)
	}
	if plexEpisodeName("Show", "2001", 0, 0, "") != "" {
		t.Error("a show without an episode got a name")
	}
}

// Proposals are only applied once confirmed; clashes keep their names.
func TestPlexReviewAndApply(t *testing.T) {
	fakeTools(t)
	dir := t.TempDir()
	mk := func(n string) string { p := filepath.Join(dir, n); _ = os.WriteFile(p, []byte("x"), 0o644); return p }
	a, b, c := mk("the.office.s01e01.mkv"), mk("the.office.s01e02.mkv"), mk("heat.mkv")
	_ = os.WriteFile(filepath.Join(dir, "Heat (1995) {tmdb-949}.mkv"), []byte("there already"), 0o644)
	g := newGUIServer()
	g.prefsPath = filepath.Join(t.TempDir(), "settings.json")
	ids, errs := g.queueAdd([]json.RawMessage{itemSettings(a, filepath.Join(dir, "the.office.s01e01_KTL.mkv")),
		itemSettings(b, filepath.Join(dir, "the.office.s01e02_KTL.mkv")), itemSettings(c, filepath.Join(dir, "heat_KTL.mkv"))})
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	g.mu.Lock()
	for i, n := range []string{"The Office (2005) - S01E01 - Pilot", "The Office (2005) - S01E02 - Diversity Day", "Heat (1995) {tmdb-949}"} {
		setRawField(g.findItemLocked(ids[i]), "plexName", n)
	}
	g.mu.Unlock()
	props := g.plexProposals()
	if len(props) != 3 || props[0].To != "The Office (2005) - S01E01 - Pilot.mkv" || props[2].Why == "" {
		t.Fatalf("proposals: %+v", props)
	}
	g.mu.Lock()
	out := g.findItemLocked(ids[0]).Output
	g.mu.Unlock()
	if !strings.HasSuffix(out, "_KTL.mkv") {
		t.Fatal("renamed before being confirmed")
	}
	r := g.plexApply([]int{ids[0], ids[2]}) // the second episode is left unticked
	if r["renamed"] != 1 || len(r["skipped"].([]string)) != 1 {
		t.Fatalf("apply: %v", r)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if o := g.findItemLocked(ids[0]).Output; filepath.Base(o) != "The Office (2005) - S01E01 - Pilot.mkv" {
		t.Errorf("renamed: %s", o)
	}
	if o := g.findItemLocked(ids[1]).Output; !strings.HasSuffix(o, "s01e02_KTL.mkv") {
		t.Errorf("unticked file changed: %s", o)
	}
	if o := g.findItemLocked(ids[2]).Output; !strings.HasSuffix(o, "heat_KTL.mkv") {
		t.Errorf("clash renamed: %s", o)
	}
}
