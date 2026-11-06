package main

import "testing"

func TestNativeOnlyNames(t *testing.T) {
	for _, c := range []struct {
		name, lang string
		want       bool
	}{
		{"日本語", "jpn", true}, {"[日本語]", "ja", true}, {"Deutsch", "ger", true}, {"Русский", "rus", true},
		{"한국어", "kor", true}, {"हिन्दी", "hin", true},
		{"日本語", "", false},           // the name is the only place the language is said
		{"日本語", "eng", false},        // the name and the language disagree: leave it
		{"日本語 コメンタリー", "jpn", false}, // says more than the language
		{"Deutsch 5.1", "ger", false}, {"Latino", "spa", false}, {"Castellano", "spa", false},
		{"English", "eng", false}, {"Japanese", "jpn", false}, // already English
	} {
		if got := nativeOnly(c.name, c.lang); got != c.want {
			t.Errorf("nativeOnly(%q, %q) = %v, want %v", c.name, c.lang, got, c.want)
		}
	}
}

func TestNativeNameBecomesEnglish(t *testing.T) {
	audio := []outAudio{
		{Src: srcTrack{N: 1, Lang: "jpn", Title: "日本語", Codec: "ac3", Channels: 6, Layout: "5.1(side)"}, Codec: "copy"},
		{Src: srcTrack{N: 2, Lang: "jpn", Title: "日本語 コメンタリー", Codec: "aac", Channels: 2}, Codec: "copy"},
	}
	subs := []outSub{{Src: srcTrack{N: 3, Lang: "kor", Title: "한국어", Codec: "subrip"}, Codec: "copy"}}
	nameUntitled(audio, subs)
	if audio[0].Title != "Japanese AC-3 5.1" {
		t.Errorf("audio 1 named %q", audio[0].Title)
	}
	if audio[1].Title != "" {
		t.Errorf("audio 2 should keep its own name, got %q", audio[1].Title)
	}
	if subs[0].Title != "Korean SRT" {
		t.Errorf("subtitle named %q", subs[0].Title)
	}
}

func TestMoreLanguageNames(t *testing.T) {
	for code, want := range map[string]string{"yue": "Cantonese", "cmn": "Mandarin", "urd": "Urdu", "ur": "Urdu",
		"wel": "Welsh", "cy": "Welsh", "lao": "Lao", "zxx": "No language", "jpn": "Japanese", "zh-Hant": "Chinese"} {
		if got := langName(code); got != want {
			t.Errorf("langName(%q) = %q, want %q", code, got, want)
		}
	}
}
