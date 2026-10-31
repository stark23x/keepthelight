package main

import (
	"strings"
	"testing"
)

func TestIsPlexExtra(t *testing.T) {
	for rel, want := range map[string]bool{
		"Heat (1995)/Heat (1995).mkv":                              false,
		"Heat (1995)/Featurettes/Making of.mkv":                    true,
		"Heat (1995)/behind the scenes/x.mkv":                      true,
		"Heat (1995)/Trailers/Teaser.mp4":                          true,
		"Heat (1995)/Teaser Trailer-trailer.mp4":                   true,
		"Heat (1995)/Alternate ending-Deleted.mkv":                 true,
		"Show/Season 01/Show S01E01.mkv":                           false,
		"Show/Season 01/Other/Bloopers.mkv":                        true,
		"The Other (1972)/The Other (1972).mkv":                    false,
		"Trailer Park Boys/Season 01/Trailer Park Boys S01E01.mkv": false,
		"Heat (1995)/Plex Versions/Optimized for TV/Heat.mp4":      true,
		"Film - Other Side.mkv":                                    false,
	} {
		if got := isPlexExtra(rel); got != want {
			t.Errorf("%s: %v", rel, got)
		}
	}
}

func TestEmptyTracks(t *testing.T) {
	data := `{"streams":[
	 {"codec_type":"video","codec_name":"hevc"},
	 {"codec_type":"audio","codec_name":"truehd","channels":8,"tags":{"language":"eng","NUMBER_OF_FRAMES":"7000000","NUMBER_OF_BYTES":"4000000000"}},
	 {"codec_type":"audio","codec_name":"ac3","channels":2,"tags":{"language":"eng","NUMBER_OF_FRAMES-eng":"0","NUMBER_OF_BYTES-eng":"0"}},
	 {"codec_type":"subtitle","codec_name":"hdmv_pgs_subtitle","tags":{"language":"eng","NUMBER_OF_FRAMES":"0","NUMBER_OF_BYTES":"0"}},
	 {"codec_type":"subtitle","codec_name":"subrip","tags":{"language":"eng","NUMBER_OF_FRAMES":"1500","NUMBER_OF_BYTES":"60000"}},
	 {"codec_type":"subtitle","codec_name":"subrip","tags":{"language":"fre"}}]}`
	tr, err := parseTracks([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if tr.Audio[0].Empty || !tr.Audio[1].Empty || !tr.Subs[0].Empty || tr.Subs[1].Empty || tr.Subs[2].Empty {
		t.Fatalf("empty flags: %+v %+v", tr.Audio, tr.Subs)
	}
	if !strings.Contains(describe(tr.Subs[0], false), "EMPTY") {
		t.Error(describe(tr.Subs[0], false))
	}
	cfg := baseCfg()
	a, s, warns, err := selectTracks(cfg, tr, false)
	if err != nil || len(a) != 1 || a[0].Src.N != 1 || len(s) != 2 || s[0].Src.N != 2 {
		t.Fatalf("rules: %v %v %v", a, s, err)
	}
	if w := strings.Join(warns, "|"); !strings.Contains(w, "note: subtitle track 1 is empty") || !strings.Contains(w, "note: audio track 2 is empty") {
		t.Errorf("notes: %v", warns)
	}
	ch := subChoice(s)["tracks"].([]map[string]any)
	if len(ch) != 2 {
		t.Errorf("the window's starting choice should leave the empty track out: %v", ch)
	}
	// chosen on purpose: kept, with a warning
	cfg.SubSel = []trackSel{{N: 1}}
	_, s, warns, _ = selectTracks(cfg, tr, false)
	if len(s) != 1 || !strings.Contains(strings.Join(warns, "|"), "subtitle track 1 is empty; keeping it") {
		t.Errorf("explicit: %v %v", s, warns)
	}
}
