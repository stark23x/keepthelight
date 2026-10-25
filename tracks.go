package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Audio and subtitle track selection.
//
// Either the user picked tracks for this file (Config.AudioSel / SubSel, from
// the window or -audio-tracks / -sub-tracks), or rules decide: languages to
// keep, dropping commentary tracks, forced subtitles only. Rules are what the
// queue uses, because every file has different tracks. Tracks are numbered
// from 1 within their kind (audio 1, 2, ...; subtitle 1, 2, ...), as the window
// shows them.

type srcTrack struct {
	N                   int // 1-based within its kind
	Codec, Profile      string
	Layout, Lang, Title string
	Channels            int
	BitRate             float64 // bits/s, 0 if unknown
	Bytes               int64   // from container statistics, 0 if unknown
	Disp                map[string]bool
}

func (t srcTrack) Default() bool { return t.Disp["default"] }
func (t srcTrack) Forced() bool  { return t.Disp["forced"] }

type Tracks struct {
	Audio, Subs []srcTrack
}

// trackSel is one chosen track: for audio, how to write it (Codec "copy" or
// an encoder, "" = the general audio setting); for subtitles, its forced flag.
type trackSel struct {
	N       int    `json:"n"`
	Codec   string `json:"codec,omitempty"`
	Bitrate string `json:"bitrate,omitempty"`
	Forced  bool   `json:"forced,omitempty"`
}

type outAudio struct {
	Src     srcTrack
	Codec   string // "copy" or an ffmpeg encoder
	Bitrate string // for converted tracks
	Default bool
	Title   string // replaces the source title when it describes the old format
}

type outSub struct {
	Src             srcTrack
	Default, Forced bool
	Codec           string // ffmpeg subtitle codec for the output container ("copy", "srt", "mov_text")
}

// ---------- probing ----------

func probeTracks(ctx context.Context, ffprobe, path string) (*Tracks, error) {
	out, err := toolCommand(ctx, ffprobe, "-v", "error", "-show_entries",
		"stream=codec_type,codec_name,profile,channels,channel_layout,bit_rate:stream_tags:stream_disposition", "-of", "json", path).Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe (tracks) failed: %w", err)
	}
	return parseTracks(out)
}

func parseTracks(data []byte) (*Tracks, error) {
	var j struct {
		Streams []struct {
			Type     string            `json:"codec_type"`
			Codec    string            `json:"codec_name"`
			Profile  string            `json:"profile"`
			Channels int               `json:"channels"`
			Layout   string            `json:"channel_layout"`
			BitRate  string            `json:"bit_rate"`
			Tags     map[string]string `json:"tags"`
			Disp     map[string]int    `json:"disposition"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(data, &j); err != nil {
		return nil, err
	}
	t := &Tracks{}
	for _, s := range j.Streams {
		if s.Type != "audio" && s.Type != "subtitle" {
			continue
		}
		tr := srcTrack{Codec: s.Codec, Profile: s.Profile, Channels: s.Channels, Layout: s.Layout, Disp: map[string]bool{}}
		tr.BitRate, _ = strconv.ParseFloat(s.BitRate, 64)
		for k, v := range s.Tags {
			uk := strings.ToUpper(k)
			switch {
			case uk == "LANGUAGE":
				tr.Lang = v
			case uk == "TITLE":
				tr.Title = v
			case strings.HasPrefix(uk, "NUMBER_OF_BYTES"):
				tr.Bytes, _ = strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			case strings.HasPrefix(uk, "BPS") && tr.BitRate == 0:
				tr.BitRate, _ = strconv.ParseFloat(strings.TrimSpace(v), 64)
			}
		}
		for k, v := range s.Disp {
			if v == 1 {
				tr.Disp[k] = true
			}
		}
		if s.Type == "audio" {
			tr.N = len(t.Audio) + 1
			t.Audio = append(t.Audio, tr)
		} else {
			tr.N = len(t.Subs) + 1
			t.Subs = append(t.Subs, tr)
		}
	}
	return t, nil
}

// ---------- describing ----------

var codecNames = map[string]string{
	"truehd": "TrueHD", "eac3": "E-AC-3", "ac3": "AC-3", "dts": "DTS", "aac": "AAC", "flac": "FLAC", "opus": "Opus",
	"libopus": "Opus", "mp3": "MP3", "vorbis": "Vorbis", "alac": "ALAC", "mlp": "MLP",
	"hdmv_pgs_subtitle": "PGS", "subrip": "SRT", "srt": "SRT", "ass": "ASS", "ssa": "SSA", "dvd_subtitle": "VobSub",
	"dvb_subtitle": "DVB", "mov_text": "MP4 text", "webvtt": "WebVTT", "text": "text",
}

func codecName(t srcTrack) string {
	n := codecNames[t.Codec]
	switch {
	case strings.HasPrefix(t.Codec, "pcm_"):
		n = "PCM"
	case t.Codec == "dts" && t.Profile != "" && t.Profile != "DTS":
		n = t.Profile // DTS-HD MA, DTS:X ...
	case n == "":
		n = t.Codec
	}
	if strings.Contains(strings.ToLower(t.Profile), "atmos") {
		n += " Atmos"
	}
	return n
}

func layoutName(t srcTrack) string {
	l := t.Layout
	if i := strings.Index(l, "("); i > 0 {
		l = l[:i] // 5.1(side) -> 5.1
	}
	switch {
	case l == "mono" || (l == "" && t.Channels == 1):
		return "mono"
	case l == "stereo" || (l == "" && t.Channels == 2):
		return "stereo"
	case l != "":
		return l
	case t.Channels > 0:
		return fmt.Sprintf("%d ch", t.Channels)
	}
	return ""
}

// describe is the one-line description the window shows for a track.
func describe(t srcTrack, audio bool) string {
	parts := []string{langName(t.Lang)}
	if audio {
		parts = append(parts, strings.TrimSpace(codecName(t)+" "+layoutName(t)))
	} else {
		parts = append(parts, codecName(t))
	}
	if t.Title != "" {
		parts = append(parts, "“"+t.Title+"”")
	}
	var flags []string
	// default and forced are not listed: the window shows them as controls
	for _, f := range []struct{ key, text string }{{"hearing_impaired", "SDH"}, {"comment", "commentary"}} {
		if t.Disp[f.key] {
			flags = append(flags, f.text)
		}
	}
	if len(flags) > 0 {
		parts = append(parts, strings.Join(flags, ", "))
	}
	return strings.Join(parts, " · ")
}

// isCommentary: flagged as a commentary, or called one in its title.
func isCommentary(t srcTrack) bool {
	if t.Disp["comment"] {
		return true
	}
	low := strings.ToLower(t.Title)
	for _, w := range []string{"comment", "kommentar", "commento", "comentario", "commentaire"} {
		if strings.Contains(low, w) {
			return true
		}
	}
	return false
}

// ---------- languages ----------

// languages maps the ways a language is written (ISO 639-1, both ISO 639-2
// forms, the English name) to one key.
var languages = map[string][]string{
	"eng": {"en", "english"}, "jpn": {"ja", "japanese"}, "fre": {"fr", "fra", "french"}, "ger": {"de", "deu", "german"},
	"spa": {"es", "spanish", "castilian"}, "ita": {"it", "italian"}, "por": {"pt", "portuguese"}, "rus": {"ru", "russian"},
	"chi": {"zh", "zho", "chinese", "mandarin", "cantonese"}, "kor": {"ko", "korean"}, "dut": {"nl", "nld", "dutch"},
	"swe": {"sv", "swedish"}, "dan": {"da", "danish"}, "nor": {"no", "nob", "nno", "nb", "nn", "norwegian"},
	"fin": {"fi", "finnish"}, "pol": {"pl", "polish"}, "cze": {"cs", "ces", "czech"}, "hun": {"hu", "hungarian"},
	"tur": {"tr", "turkish"}, "ara": {"ar", "arabic"}, "heb": {"he", "hebrew"}, "hin": {"hi", "hindi"},
	"tha": {"th", "thai"}, "gre": {"el", "ell", "greek"}, "ukr": {"uk", "ukrainian"}, "rum": {"ro", "ron", "romanian"},
	"bul": {"bg", "bulgarian"}, "hrv": {"hr", "croatian"}, "srp": {"sr", "serbian"}, "slv": {"sl", "slovenian"},
	"slo": {"sk", "slk", "slovak"}, "ice": {"is", "isl", "icelandic"}, "ind": {"id", "indonesian"}, "may": {"ms", "msa", "malay"},
	"vie": {"vi", "vietnamese"}, "tam": {"ta", "tamil"}, "tel": {"te", "telugu"}, "per": {"fa", "fas", "persian", "farsi"},
	"cat": {"ca", "catalan"}, "baq": {"eu", "eus", "basque"}, "glg": {"gl", "galician"}, "est": {"et", "estonian"},
	"lav": {"lv", "latvian"}, "lit": {"lt", "lithuanian"}, "fil": {"tl", "tgl", "filipino", "tagalog"},
}

var langKeys = func() map[string]string {
	m := map[string]string{}
	for k, alts := range languages {
		m[k] = k
		for _, a := range alts {
			m[a] = k
		}
	}
	return m
}()

// langKey normalises a language ("en", "eng", "en-US", "English") for comparing.
func langKey(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if i := strings.IndexAny(s, "-_"); i > 0 {
		s = s[:i]
	}
	if k, ok := langKeys[s]; ok {
		return k
	}
	return s
}

func langName(code string) string {
	k := langKey(code)
	if k == "" || k == "und" {
		return "Unknown language"
	}
	if alts, ok := languages[k]; ok {
		n := alts[len(alts)-1]
		for _, a := range alts {
			if len(a) > 3 { // the English name
				n = a
				break
			}
		}
		return strings.ToUpper(n[:1]) + n[1:]
	}
	return code
}

// parseLangs reads a list like "eng, jpn" or "English Japanese". Empty = all.
func parseLangs(s string) []string {
	var out []string
	for _, f := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ';' || r == ' ' || r == '/' }) {
		if k := langKey(f); k != "" && k != "all" {
			out = append(out, k)
		}
	}
	return out
}

// langMatch: no list keeps every language; a track without a language is
// kept too (there is no telling what it is, and losing it would be worse).
func langMatch(langs []string, lang string) bool {
	k := langKey(lang)
	if len(langs) == 0 || k == "" || k == "und" {
		return true
	}
	for _, l := range langs {
		if l == k {
			return true
		}
	}
	return false
}

// ---------- choosing ----------

// mp4Audio reports whether MP4 can hold a copy of this audio codec.
func mp4Audio(codec string) bool {
	switch codec {
	case "aac", "ac3", "eac3", "mp3", "alac", "flac", "opus":
		return true
	}
	return false
}

// autoBitrate is used for a converted track when no bitrate was chosen.
func autoBitrate(codec string, channels int) string {
	surround := channels > 2
	switch codec {
	case "eac3":
		if surround {
			return "640k"
		}
		return "224k"
	case "ac3":
		if surround {
			return "640k"
		}
		return "192k"
	case "aac":
		if channels > 6 {
			return "512k"
		} else if surround {
			return "384k"
		}
		return "192k"
	case "libopus":
		if channels > 6 {
			return "384k"
		} else if surround {
			return "256k"
		}
		return "128k"
	}
	return "" // flac: lossless, no bitrate
}

// outLayout: what a converted track's channel layout becomes (AC-3 and E-AC-3
// hold at most 5.1, so 7.1 is mixed down).
func outLayout(codec string, t srcTrack) string {
	if (codec == "ac3" || codec == "eac3") && t.Channels > 6 {
		return "5.1"
	}
	return layoutName(t)
}

// retitle: a converted track whose title names the old format ("TrueHD 7.1
// Atmos") gets a title naming the new one, so players do not mislabel it.
func retitle(t srcTrack, codec string) string {
	low := strings.ToLower(t.Title)
	for _, w := range []string{"truehd", "dts", "atmos", "flac", "pcm", "lpcm", "dolby digital", "e-ac-3", "eac3", "ac-3", "ac3", "aac", "7.1", "5.1"} {
		if strings.Contains(low, w) {
			return strings.TrimSpace(codecNames[codec] + " " + outLayout(codec, t))
		}
	}
	return ""
}

// selectTracks decides which tracks go into the output and how.
func selectTracks(cfg *Config, t *Tracks, mp4 bool) (audio []outAudio, subs []outSub, warns []string, err error) {
	if t == nil {
		t = &Tracks{}
	}
	warn := func(f string, a ...any) { warns = append(warns, fmt.Sprintf(f, a...)) }
	general := cfg.Audio
	if general == "" {
		general = "copy"
	}

	// audio
	if cfg.AudioSel != nil {
		seen := map[int]bool{}
		for _, s := range cfg.AudioSel {
			if s.N < 1 || s.N > len(t.Audio) {
				return nil, nil, nil, fmt.Errorf("audio track %d does not exist in this file (it has %d)", s.N, len(t.Audio))
			}
			if seen[s.N] {
				continue
			}
			seen[s.N] = true
			codec := s.Codec
			if codec == "" {
				codec = general
			}
			audio = append(audio, outAudio{Src: t.Audio[s.N-1], Codec: codec, Bitrate: s.Bitrate})
		}
		if len(audio) == 0 && len(t.Audio) > 0 {
			warn("no audio track is kept: the output will be silent")
		}
	} else {
		for _, a := range t.Audio {
			if langMatch(cfg.AudioLangs, a.Lang) && !(cfg.DropCommentary && isCommentary(a)) {
				audio = append(audio, outAudio{Src: a, Codec: general})
			}
		}
		if len(audio) == 0 && len(t.Audio) > 0 { // never leave a file silent by accident
			keep := t.Audio[0]
			for _, a := range t.Audio {
				if a.Default() {
					keep = a
					break
				}
			}
			audio = append(audio, outAudio{Src: keep, Codec: general})
			warn("no audio track matches the chosen languages, so audio track %d (%s) is kept", keep.N, langName(keep.Lang))
		}
	}
	for i := range audio {
		a := &audio[i]
		if mp4 && a.Codec == "copy" && !mp4Audio(a.Src.Codec) {
			a.Codec, a.Bitrate = "eac3", ""
			warn("audio track %d (%s) cannot be stored in MP4, so it is converted to E-AC-3 (choose MKV to keep it as it is)", a.Src.N, codecName(a.Src))
		}
		if a.Codec != "copy" {
			if a.Bitrate == "" {
				a.Bitrate = cfg.AudioBitrate
			}
			if a.Bitrate == "" {
				a.Bitrate = autoBitrate(a.Codec, a.Src.Channels)
			}
			if a.Codec == "flac" {
				a.Bitrate = ""
			}
			a.Title = retitle(a.Src, a.Codec)
		}
	}
	if len(audio) > 0 {
		def := -1
		if cfg.AudioDefault > 0 {
			for i, a := range audio {
				if a.Src.N == cfg.AudioDefault {
					def = i
				}
			}
			if def < 0 {
				warn("audio track %d was chosen as the default but is not kept", cfg.AudioDefault)
			}
		}
		if def < 0 {
			for i, a := range audio {
				if a.Src.Default() {
					def = i
					break
				}
			}
		}
		if def < 0 {
			def = 0
		}
		audio[def].Default = true
	}

	// subtitles
	if cfg.SubSel != nil {
		seen := map[int]bool{}
		for _, s := range cfg.SubSel {
			if s.N < 1 || s.N > len(t.Subs) {
				return nil, nil, nil, fmt.Errorf("subtitle track %d does not exist in this file (it has %d)", s.N, len(t.Subs))
			}
			if !seen[s.N] {
				seen[s.N] = true
				subs = append(subs, outSub{Src: t.Subs[s.N-1], Forced: s.Forced})
			}
		}
	} else if cfg.SubMode != "none" {
		for _, s := range t.Subs {
			if langMatch(cfg.SubLangs, s.Lang) && (cfg.SubMode != "forced" || s.Forced()) {
				subs = append(subs, outSub{Src: s, Forced: s.Forced()})
			}
		}
	}
	kept := subs[:0]
	for _, s := range subs {
		codec, ok, w := subCodec(s.Src, mp4)
		if w != "" {
			warns = append(warns, w)
		}
		if ok {
			s.Codec = codec
			kept = append(kept, s)
		}
	}
	subs = kept
	switch {
	case cfg.SubDefault > 0:
		found := false
		for i := range subs {
			if subs[i].Src.N == cfg.SubDefault {
				subs[i].Default, found = true, true
			}
		}
		if !found {
			warn("subtitle track %d was chosen as the default but is not kept", cfg.SubDefault)
		}
	case cfg.SubDefault == 0:
		for i := range subs {
			if subs[i].Src.Default() {
				subs[i].Default = true
				break
			}
		}
	} // -1: no default subtitle
	return audio, subs, warns, nil
}

// subCodec picks how a subtitle track is written to the output container.
//
//	MKV: text tracks are copied, except mov_text (MP4's format) which MKV cannot
//	     store, so it is converted to SubRip. Bitmap tracks (PGS, VobSub) are copied.
//	MP4: text tracks become mov_text (styling is lost for ASS); bitmap tracks
//	     cannot be stored and are dropped.
func subCodec(s srcTrack, mp4 bool) (codec string, ok bool, warn string) {
	c := strings.ToLower(s.Codec)
	switch {
	case mp4 && textSubs[c]:
		if c == "ass" || c == "ssa" {
			warn = fmt.Sprintf("subtitle track %d (%s) is converted to plain MP4 text; styling is lost (use MKV to keep it)", s.N, c)
		}
		return "mov_text", true, warn
	case mp4:
		return "", false, fmt.Sprintf("subtitle track %d (%s) cannot be stored in MP4 and is dropped (use MKV to keep it)", s.N, codecName(s))
	case c == "mov_text" || c == "text":
		return "srt", true, ""
	}
	return "copy", true, "" // unknown but non-MP4 target: let MKV try; a bad track is reported by ffmpeg
}

// ---------- ffmpeg arguments ----------

// disposition writes a track's flags as an absolute value, so it works with
// every FFmpeg version: the source's other flags (SDH, commentary...) are
// kept, default and forced are set as chosen.
func disposition(src map[string]bool, def, forced, setForced bool) string {
	var flags []string
	for k, v := range src {
		if v && k != "default" && !(setForced && k == "forced") {
			flags = append(flags, k)
		}
	}
	if def {
		flags = append(flags, "default")
	}
	if setForced && forced {
		flags = append(flags, "forced")
	}
	if len(flags) == 0 {
		return "0"
	}
	sort.Strings(flags)
	return strings.Join(flags, "+")
}

// audioArgs maps and encodes the chosen audio tracks of input number `in`.
func audioArgs(audio []outAudio, in int) []string {
	var a []string
	for n, t := range audio {
		a = append(a, "-map", fmt.Sprintf("%d:a:%d", in, t.Src.N-1), fmt.Sprintf("-c:a:%d", n), t.Codec)
		if t.Codec != "copy" && t.Bitrate != "" {
			a = append(a, fmt.Sprintf("-b:a:%d", n), t.Bitrate)
		}
		if t.Codec != "copy" && (t.Codec == "ac3" || t.Codec == "eac3") && t.Src.Channels > 6 {
			a = append(a, fmt.Sprintf("-ac:a:%d", n), "6") // these formats hold at most 5.1
		}
		if t.Title != "" {
			a = append(a, fmt.Sprintf("-metadata:s:a:%d", n), "title="+t.Title)
		}
		a = append(a, fmt.Sprintf("-disposition:a:%d", n), disposition(t.Src.Disp, t.Default, false, false))
	}
	return a
}

// subtitleArgs maps the chosen subtitle tracks of input number `in`.
func subtitleArgs(subs []outSub, in int) []string {
	var a []string
	for n, s := range subs {
		a = append(a, "-map", fmt.Sprintf("%d:s:%d", in, s.Src.N-1), fmt.Sprintf("-c:s:%d", n), s.Codec,
			fmt.Sprintf("-disposition:s:%d", n), disposition(s.Src.Disp, s.Default, s.Forced, true))
	}
	return a
}

// ---------- command line ----------

// parseAudioSel reads -audio-tracks: "all" (nil), "none" (empty), or a list
// like "1,2:eac3,3:aac:192k".
func parseAudioSel(s string) ([]trackSel, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "", "all":
		return nil, nil
	case "none":
		return []trackSel{}, nil
	}
	sel := []trackSel{}
	for _, item := range strings.Split(s, ",") {
		f := strings.Split(strings.TrimSpace(item), ":")
		n, err := strconv.Atoi(f[0])
		if err != nil || n < 1 {
			return nil, fmt.Errorf("bad audio track %q (use numbers like 1,2:eac3,3:aac:192k)", item)
		}
		t := trackSel{N: n}
		if len(f) > 1 {
			c, ok := audios[f[1]]
			if !ok {
				return nil, fmt.Errorf("unknown audio format %q for track %d (copy, aac, eac3, ac3, flac, opus)", f[1], n)
			}
			t.Codec = c
		}
		if len(f) > 2 {
			if !reBitrate.MatchString(f[2]) {
				return nil, fmt.Errorf("bad bitrate %q for audio track %d", f[2], n)
			}
			t.Bitrate = f[2]
		}
		if len(f) > 3 {
			return nil, fmt.Errorf("bad audio track %q", item)
		}
		sel = append(sel, t)
	}
	return sel, nil
}

// parseSubSel reads -sub-tracks: "all" (nil), "none" (empty), or "1,3:forced".
func parseSubSel(s string) ([]trackSel, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "", "all":
		return nil, nil
	case "none":
		return []trackSel{}, nil
	}
	sel := []trackSel{}
	for _, item := range strings.Split(s, ",") {
		f := strings.Split(strings.TrimSpace(item), ":")
		n, err := strconv.Atoi(f[0])
		if err != nil || n < 1 || len(f) > 2 || (len(f) == 2 && f[1] != "forced") {
			return nil, fmt.Errorf("bad subtitle track %q (use numbers like 1,3:forced)", item)
		}
		sel = append(sel, trackSel{N: n, Forced: len(f) == 2})
	}
	return sel, nil
}

func audioSelString(sel []trackSel) string {
	if sel == nil {
		return "all"
	}
	if len(sel) == 0 {
		return "none"
	}
	var p []string
	for _, t := range sel {
		s := strconv.Itoa(t.N)
		if t.Codec != "" {
			c := t.Codec
			if c == "libopus" {
				c = "opus"
			}
			s += ":" + c
			if t.Bitrate != "" {
				s += ":" + t.Bitrate
			}
		}
		p = append(p, s)
	}
	return strings.Join(p, ",")
}

func subSelString(sel []trackSel) string {
	if sel == nil {
		return "all"
	}
	if len(sel) == 0 {
		return "none"
	}
	var p []string
	for _, t := range sel {
		s := strconv.Itoa(t.N)
		if t.Forced {
			s += ":forced"
		}
		p = append(p, s)
	}
	return strings.Join(p, ",")
}

// chooseTracks reads the file's tracks and fills in plan.Audio and plan.Subs.
func chooseTracks(ctx context.Context, ffprobe string, cfg *Config, plan *Plan) error {
	t, err := probeTracks(ctx, ffprobe, cfg.Input)
	if err != nil {
		return fmt.Errorf("could not read the audio and subtitle tracks: %w", err)
	}
	audio, subs, warns, err := selectTracks(cfg, t, isMP4(cfg.Output))
	if err != nil {
		return err
	}
	plan.Audio, plan.Subs = audio, subs
	plan.Warnings = append(plan.Warnings, warns...)
	return nil
}

// ---------- for the window ----------

func trackList(ts []srcTrack, audio bool) []map[string]any {
	out := []map[string]any{}
	for _, t := range ts {
		m := map[string]any{"n": t.N, "desc": describe(t, audio), "codec": t.Codec, "lang": t.Lang,
			"default": t.Default(), "forced": t.Forced(), "commentary": isCommentary(t), "channels": t.Channels}
		if audio {
			m["mp4ok"] = mp4Audio(t.Codec)
		} else {
			m["mp4ok"] = textSubs[strings.ToLower(t.Codec)]
		}
		out = append(out, m)
	}
	return out
}

// audioChoice / subChoice are what was decided, for the window to show.
func audioChoice(audio []outAudio) map[string]any {
	sel, def := []map[string]any{}, 0
	for _, a := range audio {
		c := a.Codec
		if c == "libopus" {
			c = "opus"
		}
		sel = append(sel, map[string]any{"n": a.Src.N, "codec": c, "bitrate": a.Bitrate})
		if a.Default {
			def = a.Src.N
		}
	}
	return map[string]any{"tracks": sel, "default": def}
}

func subChoice(subs []outSub) map[string]any {
	sel, def := []map[string]any{}, -1
	for _, s := range subs {
		sel = append(sel, map[string]any{"n": s.Src.N, "forced": s.Forced})
		if s.Default {
			def = s.Src.N
		}
	}
	return map[string]any{"tracks": sel, "default": def}
}
