package main

import (
	"strings"
	"testing"
)

func TestNameDetails(t *testing.T) {
	for name, want := range map[string][4]any{
		"English":                     {"eng", false, false, false},
		"Director's Commentary (Eng)": {"eng", true, false, false},
		"English SDH":                 {"eng", false, true, false},
		"Français - Forced":           {"fre", false, false, true},
		"English / Spanish":           {"", false, false, false}, // two languages: says nothing
		"Signs & Songs":               {"", false, false, false},
		"Japanese (hearing impaired)": {"jpn", false, true, false},
		"engine sounds":               {"", false, false, false}, // whole words only
	} {
		l, c, s, f := nameDetails(name)
		if l != want[0] || c != want[1] || s != want[2] || f != want[3] {
			t.Errorf("%q: %q %v %v %v", name, l, c, s, f)
		}
	}
}

func TestFillFromNames(t *testing.T) {
	und := srcTrack{N: 1, Codec: "ac3", Lang: "und", Title: "English Commentary", Disp: map[string]bool{}}
	tagged := srcTrack{N: 2, Codec: "ac3", Lang: "fre", Title: "English", Disp: map[string]bool{}} // the file's language wins
	sub := srcTrack{N: 1, Codec: "subrip", Title: "English SDH Forced", Disp: map[string]bool{}}
	user := "jpn"
	audio := []outAudio{{Src: und, Codec: "copy"}, {Src: tagged, Codec: "copy"}}
	subs := []outSub{{Src: sub, Codec: "copy", Meta: &TrackMeta{Kind: "s", N: 1, Lang: &user}}} // the user's language wins
	notes := fillFromNames(audio, subs, true)
	if audio[0].Meta == nil || *audio[0].Meta.Lang != "eng" || !*audio[0].Meta.Commentary {
		t.Errorf("audio 1: %+v", audio[0].Meta)
	}
	if audio[1].Meta != nil {
		t.Errorf("audio 2 (already French): %+v", audio[1].Meta)
	}
	if *subs[0].Meta.Lang != "jpn" || subs[0].Meta.SDH == nil || !*subs[0].Meta.SDH || !subs[0].Forced {
		t.Errorf("subtitle: %+v forced %v", subs[0].Meta, subs[0].Forced)
	}
	if len(notes) != 2 || !strings.Contains(notes[0], "language English, commentary") {
		t.Errorf("notes: %q", notes)
	}
	// names then use what was filled in
	if n := autoAudioName(audio[0]); n != "English AC-3 Commentary" && !strings.HasPrefix(n, "English AC-3") {
		t.Errorf("name: %q", n)
	}
	// subtitles chosen by hand keep their forced flag as chosen
	subs = []outSub{{Src: sub, Codec: "copy"}}
	fillFromNames(nil, subs, false)
	if subs[0].Forced {
		t.Error("a hand-made choice was changed")
	}
}
