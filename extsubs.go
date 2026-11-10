package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// ---------- your own subtitle files ----------
//
// An SRT (or ASS/SSA) file is added to the new file as an extra subtitle track,
// after the file's own. Its language, name and marks are set like any track's;
// the language is guessed from the file name ("Film.en.srt", "Film.eng.forced.srt").
// Before the job, the file is checked (it must hold subtitles), turned into
// UTF-8 when it is in an older text encoding (so accents show correctly in
// Plex), and can be moved in time (Offset, seconds).

// ExtSub is one subtitle file to add.
type ExtSub struct {
	Path    string  `json:"path"`
	Lang    string  `json:"lang,omitempty"`
	Title   string  `json:"title,omitempty"`
	Default bool    `json:"default,omitempty"`
	Forced  bool    `json:"forced,omitempty"`
	SDH     bool    `json:"sdh,omitempty"`
	Offset  float64 `json:"offset,omitempty"` // seconds; + shows the subtitles later
}

var extSubExts = map[string]string{".srt": "subrip", ".ass": "ass", ".ssa": "ssa"}

// extSubCodec is the subtitle format of a file, by its extension ("" = not one Keep the Light adds).
func extSubCodec(path string) string { return extSubExts[strings.ToLower(filepath.Ext(path))] }

// guessSubFile reads what a subtitle file's name says beyond the video's name:
// "Film.en.forced.srt" → English, forced.
func guessSubFile(path, videoStem string) ExtSub {
	e := ExtSub{Path: path}
	stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	rest := stem
	if len(stem) >= len(videoStem) && strings.EqualFold(stem[:len(videoStem)], videoStem) {
		rest = stem[len(videoStem):]
	}
	for _, part := range strings.FieldsFunc(rest, func(r rune) bool {
		return r == '.' || r == '_' || r == ' ' || r == '-' || r == '[' || r == ']' || r == '(' || r == ')'
	}) {
		p := strings.ToLower(part)
		switch p {
		case "forced", "foreign":
			e.Forced = true
			continue
		case "sdh", "cc", "hi":
			e.SDH = true
			continue
		case "default":
			e.Default = true
			continue
		}
		if e.Lang == "" {
			if _, ok := languages[langKey(p)]; ok {
				e.Lang = langKey(p)
				continue
			}
			if l, _, sdh, forced := nameDetails(part); l != "" {
				e.Lang, e.SDH, e.Forced = l, e.SDH || sdh, e.Forced || forced
			}
		}
	}
	return e
}

// findSubFiles lists the subtitle files beside a video that are named after it
// ("Film.mkv": "Film.srt", "Film.en.srt", "Film.English.forced.srt").
func findSubFiles(video string) []ExtSub {
	dir := filepath.Dir(video)
	stem := strings.TrimSuffix(filepath.Base(video), filepath.Ext(video))
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []ExtSub
	for _, en := range ents {
		n := en.Name()
		if en.IsDir() || extSubCodec(n) == "" {
			continue
		}
		s := strings.TrimSuffix(n, filepath.Ext(n))
		if !strings.EqualFold(s, stem) && !(len(s) > len(stem) && strings.EqualFold(s[:len(stem)], stem) && strings.ContainsRune("._ -[(", rune(s[len(stem)]))) {
			continue
		}
		out = append(out, guessSubFile(filepath.Join(dir, n), stem))
	}
	return out
}

// ---------- reading and checking ----------

// cp1252 holds the characters Windows' Western encoding puts at 0x80–0x9F
// (the rest of it is the same as Latin-1).
var cp1252 = [32]rune{'€', 0x81, '‚', 'ƒ', '„', '…', '†', '‡', 'ˆ', '‰', 'Š', '‹', 'Œ', 0x8d, 'Ž', 0x8f,
	0x90, '‘', '’', '“', '”', '•', '–', '—', '˜', '™', 'š', '›', 'œ', 0x9d, 'ž', 'Ÿ'}

// toUTF8 returns the text as UTF-8, and what it was in ("" when it already was).
func toUTF8(b []byte) ([]byte, string) {
	switch {
	case bytes.HasPrefix(b, []byte{0xEF, 0xBB, 0xBF}):
		return b[3:], ""
	case bytes.HasPrefix(b, []byte{0xFF, 0xFE}), bytes.HasPrefix(b, []byte{0xFE, 0xFF}):
		le := b[0] == 0xFF
		b = b[2:]
		u := make([]uint16, len(b)/2)
		for i := range u {
			if le {
				u[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
			} else {
				u[i] = uint16(b[2*i])<<8 | uint16(b[2*i+1])
			}
		}
		return []byte(string(utf16.Decode(u))), "UTF-16"
	case utf8.Valid(b):
		return b, ""
	}
	var out strings.Builder
	for _, c := range b {
		if c >= 0x80 && c < 0xA0 {
			out.WriteRune(cp1252[c-0x80])
		} else {
			out.WriteRune(rune(c))
		}
	}
	return []byte(out.String()), "Windows-1252 (Western)"
}

var reSRTCue = regexp.MustCompile(`(?m)^\s*\d{1,2}:\d{2}:\d{2}[,.]\d{1,3}\s*-->\s*\d{1,2}:\d{2}:\d{2}[,.]\d{1,3}`)

// checkSubFile reads a subtitle file: its text as UTF-8, how many subtitles it
// holds, and a note about its encoding. An error says why it cannot be used.
func checkSubFile(path string) (text []byte, cues int, note string, err error) {
	codec := extSubCodec(path)
	if codec == "" {
		return nil, 0, "", fmt.Errorf("%s is not a subtitle file Keep the Light can add (SRT, ASS or SSA)", filepath.Base(path))
	}
	st, err := os.Stat(path)
	if err != nil {
		return nil, 0, "", fmt.Errorf("%s cannot be read: %v", filepath.Base(path), err)
	}
	if st.Size() > 20<<20 {
		return nil, 0, "", fmt.Errorf("%s is too big to be a subtitle file (%s)", filepath.Base(path), fmtSize(st.Size()))
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, "", fmt.Errorf("%s cannot be read: %v", filepath.Base(path), err)
	}
	text, enc := toUTF8(raw)
	if enc != "" {
		note = fmt.Sprintf("in the %s text encoding: converted to UTF-8 so accents show correctly", enc)
	}
	if codec == "subrip" {
		cues = len(reSRTCue.FindAll(text, -1))
	} else {
		cues = strings.Count(string(text), "\nDialogue:")
	}
	if cues == 0 {
		return nil, 0, note, fmt.Errorf("%s holds no subtitles (it is not a working %s file)", filepath.Base(path), strings.ToUpper(strings.TrimPrefix(filepath.Ext(path), ".")))
	}
	return text, cues, note, nil
}

// ---------- in the plan and the job ----------

// addExtSubs appends the subtitle files to the chosen tracks.
func addExtSubs(cfg *Config, subs []outSub, mp4 bool) ([]outSub, []string) {
	var warns []string
	for i, e := range cfg.ExtSubs {
		codec := extSubCodec(e.Path)
		if codec == "" {
			warns = append(warns, fmt.Sprintf("%s is not a subtitle file Keep the Light can add (SRT, ASS or SSA); it is left out", filepath.Base(e.Path)))
			continue
		}
		disp := map[string]bool{"default": e.Default, "forced": e.Forced, "hearing_impaired": e.SDH}
		o := outSub{Src: srcTrack{N: 1000 + i, Codec: codec, Lang: e.Lang, Title: e.Title, Disp: disp},
			Default: e.Default, Forced: e.Forced, Codec: "copy", External: true, ExtPath: e.Path, Offset: e.Offset}
		if mp4 {
			o.Codec = "mov_text"
		}
		if e.Default {
			for j := range subs {
				subs[j].Default = false
			}
		}
		subs = append(subs, o)
	}
	return subs, warns
}

// prepareExtSubs checks each subtitle file and writes it as UTF-8 into tmp.
func prepareExtSubs(cfg *Config, plan *Plan, tmp string) error {
	for i := range plan.Subs {
		s := &plan.Subs[i]
		if !s.External {
			continue
		}
		text, cues, note, err := checkSubFile(s.ExtPath)
		if err != nil {
			return err
		}
		if note != "" {
			fmt.Printf("Note: %s is %s\n", filepath.Base(s.ExtPath), note)
		}
		f := filepath.Join(tmp, fmt.Sprintf("added%d%s", i, strings.ToLower(filepath.Ext(s.ExtPath))))
		if err := os.WriteFile(f, text, 0o644); err != nil {
			return err
		}
		shift := ""
		if s.Offset != 0 {
			shift = fmt.Sprintf(", moved %+gs", s.Offset)
		}
		n := fmt.Sprintf("%d subtitles", cues)
		if cues == 1 {
			n = "1 subtitle"
		}
		fmt.Printf("Adding subtitles: %s (%s, %s%s)\n", filepath.Base(s.ExtPath), n, langName(s.Src.Lang), shift)
		s.SRTFile = f
	}
	return nil
}

func (plan *Plan) hasExtSubs() bool {
	for _, s := range plan.Subs {
		if s.External {
			return true
		}
	}
	return false
}

// fromFile: the track comes from a file of its own (read by OCR, or added).
func (s outSub) fromFile() bool { return (s.needsOCR() || s.External) && s.SRTFile != "" }

func offsetArg(o float64) string { return strconv.FormatFloat(o, 'f', 3, 64) }
