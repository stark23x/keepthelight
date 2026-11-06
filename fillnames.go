package main

import (
	"fmt"
	"regexp"
	"strings"
)

// Filling in track details from their names: many files carry "English",
// "Commentary" or "SDH" in a track's name but not in its language and flags,
// which are what Plex and players use to pick tracks. Only what is missing is
// filled in; a language or flag the file (or the user) has already set is kept.

var nameLangs = map[string]string{
	"english": "eng", "eng": "eng",
	"french": "fre", "français": "fre", "francais": "fre", "fre": "fre", "fra": "fre",
	"german": "ger", "deutsch": "ger", "ger": "ger", "deu": "ger",
	"spanish": "spa", "español": "spa", "espanol": "spa", "castellano": "spa", "latino": "spa", "spa": "spa",
	"italian": "ita", "italiano": "ita", "ita": "ita",
	"portuguese": "por", "português": "por", "portugues": "por", "brazilian": "por", "por": "por",
	"dutch": "dut", "nederlands": "dut", "dut": "dut", "nld": "dut",
	"swedish": "swe", "svenska": "swe", "swe": "swe",
	"norwegian": "nor", "norsk": "nor", "nor": "nor",
	"danish": "dan", "dansk": "dan", "dan": "dan",
	"finnish": "fin", "suomi": "fin", "fin": "fin",
	"polish": "pol", "polski": "pol", "pol": "pol",
	"russian": "rus", "русский": "rus", "rus": "rus",
	"japanese": "jpn", "日本語": "jpn", "jpn": "jpn",
	"korean": "kor", "한국어": "kor", "kor": "kor",
	"chinese": "chi", "mandarin": "chi", "cantonese": "chi", "中文": "chi", "chi": "chi", "zho": "chi",
	"hindi": "hin", "hin": "hin",
	"arabic": "ara", "ara": "ara",
	"hebrew": "heb", "heb": "heb",
	"turkish": "tur", "türkçe": "tur", "tur": "tur",
	"greek": "gre", "ελληνικά": "gre", "gre": "gre", "ell": "gre",
	"czech": "cze", "čeština": "cze", "cze": "cze", "ces": "cze",
	"hungarian": "hun", "magyar": "hun", "hun": "hun",
	"thai": "tha", "ไทย": "tha", "tha": "tha",
	"українська": "ukr", "ukrainian": "ukr", "ukr": "ukr",
	"हिन्दी": "hin", "العربية": "ara", "עברית": "heb",
}

// nativeNames are languages written in their own words. A track named only
// that ("日本語", "Deutsch") is given the English automatic name instead
// ("Japanese AC-3 5.1"). Words that say more than the language ("Latino",
// "Castellano", the Chinese dialect names) are not here, so those names stay.
var nativeNames = map[string]bool{
	"français": true, "francais": true, "deutsch": true, "español": true, "espanol": true, "italiano": true,
	"português": true, "portugues": true, "nederlands": true, "svenska": true, "norsk": true, "dansk": true,
	"suomi": true, "polski": true, "русский": true, "日本語": true, "한국어": true, "中文": true, "türkçe": true,
	"ελληνικά": true, "čeština": true, "magyar": true, "ไทย": true, "українська": true, "हिन्दी": true,
	"العربية": true, "עברית": true,
}

// nativeOnly says whether a track name is only a language in its own words
// (brackets and spaces aside), and that language is the track's own: when the
// track has no language, the name is the only place it is said, so it stays.
func nativeOnly(name, lang string) bool {
	low := strings.ToLower(strings.TrimSpace(name))
	words := reNameWords.FindAllString(low, -1)
	if len(words) != 1 || !nativeNames[words[0]] {
		return false
	}
	rest := strings.Trim(strings.Replace(low, words[0], "", 1), " ()[]【】「」-_.:|")
	return rest == "" && !missingLang(lang) && nameLangs[words[0]] == langKey(lang)
}

var reNameWords = regexp.MustCompile(`[\p{L}\p{M}]+`) // \p{M}: the vowel signs of Hindi and Thai

// nameDetails reads a track name: its language (only when exactly one is named:
// "English / Spanish" says nothing), and whether it says commentary, SDH or forced.
func nameDetails(name string) (lang string, commentary, sdh, forced bool) {
	low := strings.ToLower(name)
	langs := map[string]bool{}
	for _, w := range reNameWords.FindAllString(low, -1) {
		if l, ok := nameLangs[w]; ok {
			langs[l] = true
		}
		switch w {
		case "commentary", "commentaries":
			commentary = true
		case "sdh":
			sdh = true
		case "forced":
			forced = true
		}
	}
	if strings.Contains(low, "hearing impaired") || strings.Contains(low, "hard of hearing") {
		sdh = true
	}
	if len(langs) == 1 {
		for l := range langs {
			lang = l
		}
	}
	return
}

func missingLang(l string) bool { k := langKey(l); return k == "" || k == "und" }

// fillFromNames fills in missing languages and flags from track names, and
// returns notes saying what was filled in. Subtitles' forced flag is only set
// when the subtitles were chosen by the rules (a hand-made choice stays as made).
func fillFromNames(audio []outAudio, subs []outSub, subsByRules bool) []string {
	var notes []string
	set := func(m **TrackMeta, kind string, n int) *TrackMeta {
		if *m == nil {
			*m = &TrackMeta{Kind: kind, N: n}
		}
		return *m
	}
	describe := func(what []string) string { return strings.Join(what, ", ") }
	for i := range audio {
		a := &audio[i]
		if a.Src.Title == "" {
			continue
		}
		lang, comm, _, _ := nameDetails(a.Src.Title)
		var what []string
		if lang != "" && missingLang(a.Src.Lang) && (a.Meta == nil || a.Meta.Lang == nil) {
			l := lang
			set(&a.Meta, "a", a.Src.N).Lang = &l
			what = append(what, "language "+langName(l))
		}
		if comm && !a.Src.Disp["comment"] && (a.Meta == nil || a.Meta.Commentary == nil) {
			t := true
			set(&a.Meta, "a", a.Src.N).Commentary = &t
			what = append(what, "commentary")
		}
		if len(what) > 0 {
			notes = append(notes, fmt.Sprintf("note: audio track %d (%q): %s, from its name", a.Src.N, a.Src.Title, describe(what)))
		}
	}
	for i := range subs {
		s := &subs[i]
		if s.Src.Title == "" || s.Added {
			continue
		}
		lang, _, sdh, forced := nameDetails(s.Src.Title)
		var what []string
		if lang != "" && missingLang(s.Src.Lang) && (s.Meta == nil || s.Meta.Lang == nil) {
			l := lang
			set(&s.Meta, "s", s.Src.N).Lang = &l
			what = append(what, "language "+langName(l))
		}
		if sdh && !s.Src.Disp["hearing_impaired"] && (s.Meta == nil || s.Meta.SDH == nil) {
			t := true
			set(&s.Meta, "s", s.Src.N).SDH = &t
			what = append(what, "SDH")
		}
		if forced && !s.Src.Forced() && !s.Forced && subsByRules {
			s.Forced = true
			what = append(what, "forced")
		}
		if len(what) > 0 {
			notes = append(notes, fmt.Sprintf("note: subtitle track %d (%q): %s, from its name", s.Src.N, s.Src.Title, describe(what)))
		}
	}
	// an SRT copy made next to a track shares what was filled in for it
	for i := range subs {
		if subs[i].Added {
			for j := range subs {
				if !subs[j].Added && subs[j].Src.N == subs[i].Src.N && subs[j].Meta != nil {
					m := *subs[j].Meta
					subs[i].Meta = &m
				}
			}
		}
	}
	return notes
}

// trackLang is a track's language as it will be written.
func trackLang(src srcTrack, m *TrackMeta) string {
	if m != nil && m.Lang != nil {
		return *m.Lang
	}
	return src.Lang
}
