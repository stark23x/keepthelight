package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlexNameFormats(t *testing.T) {
	film := plexFinal(plexFilmName("Heat", "1995", 949), plexDetails(3840, 1606, "DV"))
	if film != "Heat (1995).2160p.HEVC.DV.{tmdb-949}" {
		t.Errorf("film: %q", film)
	}
	ep := plexFinal(plexEpisodeName("The Office", "2005", 1, 2, "Diversity Day"), plexDetails(1920, 1080, ""))
	if ep != "The Office (2005).S01E02.Diversity Day.1080p.HEVC" {
		t.Errorf("episode: %q", ep)
	}
	if got := plexFinal(plexEpisodeName("What/If?", "", 3, 10, `"Pilot"`), plexDetails(854, 356, "HDR10")); got != "WhatIf.S03E10.Pilot.480p.HEVC.HDR10" {
		t.Errorf("cleaned: %q", got)
	}
	if got := plexFinal(plexEpisodeName("The Office", "2005", 1, 2, ""), ""); got != "The Office (2005).S01E02" {
		t.Errorf("no details yet: %q", got)
	}
	if plexEpisodeName("Show", "2001", 0, 0, "") != "" {
		t.Error("a show without an episode got a name")
	}
	for _, c := range []struct {
		w, h int
		want string
	}{{3840, 2160, "2160p"}, {3840, 1600, "2160p"}, {2560, 1440, "1440p"}, {1920, 800, "1080p"}, {1280, 720, "720p"}, {1024, 576, "576p"}, {640, 480, "480p"}, {854, 356, "480p"}} {
		if got := plexResolution(c.w, c.h); got != c.want {
			t.Errorf("%dx%d: %s, want %s", c.w, c.h, got, c.want)
		}
	}
}

// Proposals are only applied once confirmed; clashes keep their names.
func TestPlexReviewAndApply(t *testing.T) {
	fakeTools(t)
	dir := t.TempDir()
	mk := func(n string) string { p := filepath.Join(dir, n); _ = os.WriteFile(p, []byte("x"), 0o644); return p }
	a, b, c := mk("the.office.s01e01.mkv"), mk("the.office.s01e02.mkv"), mk("heat.mkv")
	_ = os.WriteFile(filepath.Join(dir, "Heat (1995).1080p.HEVC.{tmdb-949}.mkv"), []byte("there already"), 0o644)
	g := newGUIServer()
	g.prefsPath = filepath.Join(t.TempDir(), "settings.json")
	ids, errs := g.queueAdd([]json.RawMessage{itemSettings(a, filepath.Join(dir, "the.office.s01e01_KTL.mkv")),
		itemSettings(b, filepath.Join(dir, "the.office.s01e02_KTL.mkv")), itemSettings(c, filepath.Join(dir, "heat_KTL.mkv"))})
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	g.mu.Lock()
	for i, n := range []string{plexEpisodeName("The Office", "2005", 1, 1, "Pilot"), plexEpisodeName("The Office", "2005", 1, 2, "Diversity Day"), plexFilmName("Heat", "1995", 949)} {
		it := g.findItemLocked(ids[i])
		setRawField(it, "plexName", n)
		it.PlexDetails = "1080p.HEVC"
	}
	g.mu.Unlock()
	props := g.plexProposals()
	if len(props) != 3 || props[0].To != "The Office (2005).S01E01.Pilot.1080p.HEVC.mkv" || props[2].Why == "" {
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
	if o := g.findItemLocked(ids[0]).Output; filepath.Base(o) != "The Office (2005).S01E01.Pilot.1080p.HEVC.mkv" {
		t.Errorf("renamed: %s", o)
	}
	if o := g.findItemLocked(ids[1]).Output; !strings.HasSuffix(o, "s01e02_KTL.mkv") {
		t.Errorf("unticked file changed: %s", o)
	}
	if o := g.findItemLocked(ids[2]).Output; !strings.HasSuffix(o, "heat_KTL.mkv") {
		t.Errorf("clash renamed: %s", o)
	}
}

func TestPlexHDRKind(t *testing.T) {
	for want, plan := range map[string]*Plan{"DV": {UseDV: true, Kind: "hdr10"}, "HDR10+": {UseHDR10Plus: true, Kind: "hdr10"},
		"HDR10": {Kind: "hdr10"}, "HLG": {Kind: "hlg"}, "": {Kind: "sdr"}} {
		if got := plexHDR(&Probe{}, plan); got != want {
			t.Errorf("%+v: %q, want %q", plan, got, want)
		}
	}
}
