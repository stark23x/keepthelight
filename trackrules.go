package main

import (
	"strings"
)

// ---------- smarter track rules ----------

// audioRank orders audio formats by quality (higher is better).
var audioRank = map[string]int{
	"truehd": 90, "mlp": 85, "flac": 80, "alac": 80, "eac3": 60, "dts": 55, "ac3": 50, "aac": 40, "libopus": 35, "opus": 35,
	"vorbis": 30, "mp3": 20,
}

func audioScore(t srcTrack, mode string) int {
	r := audioRank[t.Codec]
	switch {
	case strings.HasPrefix(t.Codec, "pcm_"):
		r = 80
	case t.Codec == "dts" && t.Profile != "" && t.Profile != "DTS": // DTS-HD MA, DTS:X: lossless
		r = 82
	}
	if strings.Contains(strings.ToLower(t.Profile), "atmos") {
		r += 5
	}
	if mode == "compatible" && compatibleCodecs[t.Codec] {
		r += 1000 // plays everywhere first; quality decides among those
	}
	return r*10 + t.Channels
}

// onePerLanguage keeps one main track per language: the best quality, or the
// most compatible one. Commentary tracks are not counted (they are kept or
// dropped by their own rule), nor are tracks without a language.
func onePerLanguage(audio []outAudio, mode string) []outAudio {
	best := map[string]int{} // language -> index of the track kept
	for i, a := range audio {
		k := langKey(a.Src.Lang)
		if k == "" || k == "und" || isCommentary(a.Src) {
			continue
		}
		if j, ok := best[k]; !ok || audioScore(a.Src, mode) > audioScore(audio[j].Src, mode) {
			best[k] = i
		}
	}
	out := audio[:0:0]
	for i, a := range audio {
		k := langKey(a.Src.Lang)
		if j, ok := best[k]; ok && j != i && !isCommentary(a.Src) {
			continue
		}
		out = append(out, a)
	}
	return out
}

// parseWords reads "SDH, Signs & Songs" into lower-case words to look for.
func parseWords(s string) []string {
	var out []string
	for _, w := range strings.Split(s, ",") {
		if w = strings.ToLower(strings.TrimSpace(w)); w != "" {
			out = append(out, w)
		}
	}
	return out
}

// nameMatches: the track's name contains one of the words. "SDH", "forced"
// and "commentary" also match the track's flags.
func nameMatches(t srcTrack, words []string) bool {
	title := strings.ToLower(t.Title)
	for _, w := range words {
		switch {
		case strings.Contains(title, w):
			return true
		case (w == "sdh" || w == "cc" || w == "hearing impaired") && t.Disp["hearing_impaired"]:
			return true
		case w == "forced" && t.Disp["forced"]:
			return true
		case w == "commentary" && t.Disp["comment"]:
			return true
		}
	}
	return false
}

// ---------- naming untitled tracks ----------

// autoAudioName: "English AC-3 5.1", for the track as it is written.
func autoAudioName(a outAudio) string {
	var parts []string
	if k := langKey(a.Src.Lang); k != "" && k != "und" {
		parts = append(parts, langName(a.Src.Lang))
	}
	if a.Codec == "copy" {
		parts = append(parts, codecName(a.Src), layoutName(a.Src))
	} else {
		parts = append(parts, codecNames[a.Codec], outLayout(a))
	}
	if isCommentary(a.Src) {
		parts = append(parts, "Commentary")
	}
	return strings.Join(strings.Fields(strings.Join(parts, " ")), " ")
}

// autoSubName: "English SRT", "English PGS Forced", "English SRT SDH".
func autoSubName(s outSub) string {
	var parts []string
	if k := langKey(s.Src.Lang); k != "" && k != "und" {
		parts = append(parts, langName(s.Src.Lang))
	}
	switch {
	case s.ToSRT || s.Codec == "srt":
		parts = append(parts, "SRT")
	case s.Codec == "mov_text":
		parts = append(parts, "Text")
	default:
		parts = append(parts, codecName(s.Src))
	}
	if s.Forced {
		parts = append(parts, "Forced")
	}
	if s.Src.Disp["hearing_impaired"] {
		parts = append(parts, "SDH")
	}
	return strings.Join(parts, " ")
}

// nameUntitled gives tracks without a name one built from what they are.
// Names already set (in the file, by the user, or by a conversion) are kept.
func nameUntitled(audio []outAudio, subs []outSub) {
	for i := range audio {
		a := &audio[i]
		if a.Src.Title == "" && a.Title == "" && (a.Meta == nil || a.Meta.Title == nil) {
			a.Title = autoAudioName(*a)
		}
	}
	for i := range subs {
		s := &subs[i]
		auto := s.Title == "" || (s.Added && s.Src.Title == "") // the copy's plain "SRT"
		if s.Src.Title == "" && auto && (s.Meta == nil || s.Meta.Title == nil) {
			s.Title = autoSubName(*s)
		}
	}
}

// ---------- the same tracks for other files ----------

// trackLayout describes a file's tracks well enough to tell whether choices
// made by position ("audio 2", "subtitle 3") mean the same in another file.
func trackLayout(t *Tracks) string {
	var b strings.Builder
	for _, a := range t.Audio {
		b.WriteString("a:" + a.Codec + "/" + a.Profile + "/" + itoa(a.Channels) + "/" + langKey(a.Lang) + ";")
	}
	for _, s := range t.Subs {
		b.WriteString("s:" + s.Codec + "/" + langKey(s.Lang) + ";")
	}
	return b.String()
}
