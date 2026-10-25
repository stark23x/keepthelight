package main

import (
	"strings"
	"testing"
)

// A film with three audio tracks (English 5.1, English commentary, Japanese 7.1
// FLAC) and four subtitle tracks (English, English forced, Japanese ASS, PGS).
const trackProbe = `{"streams":[
 {"codec_type":"video","codec_name":"hevc"},
 {"codec_type":"audio","codec_name":"truehd","profile":"Dolby TrueHD + Dolby Atmos","channels":8,"channel_layout":"7.1",
  "tags":{"language":"eng","title":"TrueHD 7.1 Atmos","NUMBER_OF_BYTES-eng":"123456789"},"disposition":{"default":1,"forced":0}},
 {"codec_type":"audio","codec_name":"ac3","channels":2,"channel_layout":"stereo","bit_rate":"192000",
  "tags":{"language":"eng","title":"Director's Commentary"},"disposition":{"default":0}},
 {"codec_type":"audio","codec_name":"flac","channels":6,"channel_layout":"5.1(side)","tags":{"language":"jpn"},"disposition":{"default":0}},
 {"codec_type":"subtitle","codec_name":"subrip","tags":{"language":"en"},"disposition":{"default":0,"forced":0,"hearing_impaired":1}},
 {"codec_type":"subtitle","codec_name":"subrip","tags":{"language":"eng","title":"Forced"},"disposition":{"default":0,"forced":1}},
 {"codec_type":"subtitle","codec_name":"ass","tags":{"language":"jpn"},"disposition":{"default":1}},
 {"codec_type":"subtitle","codec_name":"hdmv_pgs_subtitle","tags":{"language":"und"},"disposition":{}}
]}`

func testTracks(t *testing.T) *Tracks {
	t.Helper()
	tr, err := parseTracks([]byte(trackProbe))
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

func nums[T any](xs []T, n func(T) int) []int {
	var out []int
	for _, x := range xs {
		out = append(out, n(x))
	}
	return out
}

func aN(a outAudio) int { return a.Src.N }
func sN(s outSub) int   { return s.Src.N }

func TestParseAndDescribeTracks(t *testing.T) {
	tr := testTracks(t)
	if len(tr.Audio) != 3 || len(tr.Subs) != 4 || tr.Audio[2].N != 3 || tr.Subs[0].N != 1 {
		t.Fatalf("%+v", tr)
	}
	if tr.Audio[0].Bytes != 123456789 || tr.Audio[1].BitRate != 192000 || !tr.Audio[0].Default() || !tr.Subs[1].Forced() {
		t.Fatalf("details: %+v", tr.Audio)
	}
	for i, want := range []string{"English · TrueHD Atmos 7.1 · “TrueHD 7.1 Atmos”", "English · AC-3 stereo · “Director's Commentary”", "Japanese · FLAC 5.1"} {
		if got := describe(tr.Audio[i], true); got != want {
			t.Errorf("audio %d: %q, want %q", i+1, got, want)
		}
	}
	if got := describe(tr.Subs[0], false); got != "English · SRT · SDH" {
		t.Errorf("sub 1: %q", got)
	}
	if got := describe(tr.Subs[3], false); got != "Unknown language · PGS" {
		t.Errorf("sub 4: %q", got)
	}
	if !isCommentary(tr.Audio[1]) || isCommentary(tr.Audio[0]) {
		t.Error("commentary detection")
	}
}

func TestLanguages(t *testing.T) {
	for in, want := range map[string]string{"en": "eng", "ENG": "eng", "en-US": "eng", "English": "eng", "fra": "fre", "fr": "fre", "de": "ger", "deu": "ger", "xyz": "xyz"} {
		if got := langKey(in); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
	if l := parseLangs("eng, Japanese;fr  all"); strings.Join(l, ",") != "eng,jpn,fre" {
		t.Errorf("parseLangs: %v", l)
	}
	if !langMatch(nil, "jpn") || !langMatch([]string{"eng"}, "") || !langMatch([]string{"eng"}, "und") || langMatch([]string{"eng"}, "jpn") || !langMatch([]string{"eng"}, "en-GB") {
		t.Error("langMatch")
	}
}

func TestSelectByRules(t *testing.T) {
	tr := testTracks(t)
	cfg := baseCfg()
	// no rules: everything, as before
	a, s, _, err := selectTracks(cfg, tr, false)
	if err != nil || len(a) != 3 || len(s) != 4 {
		t.Fatalf("all: %d %d %v", len(a), len(s), err)
	}
	if !a[0].Default || a[1].Default || !s[2].Default || !s[1].Forced {
		t.Errorf("source flags not kept: %+v %+v", a, s)
	}

	cfg.AudioLangs, cfg.DropCommentary = []string{"eng"}, true
	cfg.SubLangs, cfg.SubMode = []string{"eng"}, "forced"
	a, s, _, _ = selectTracks(cfg, tr, false)
	if got := nums(a, aN); len(got) != 1 || got[0] != 1 {
		t.Errorf("audio eng, no commentary: %v", got)
	}
	if got := nums(s, sN); len(got) != 1 || got[0] != 2 {
		t.Errorf("forced English subtitles: %v", got)
	}
	cfg.SubMode = "" // all English: the SDH one, the forced one, and the one with no language (kept: unknown)
	if _, s, _, _ = selectTracks(cfg, tr, false); len(s) != 3 || s[2].Src.N != 4 {
		t.Errorf("English subtitles: %v", nums(s, sN))
	}

	cfg.SubMode = "none"
	if _, s, _, _ = selectTracks(cfg, tr, false); len(s) != 0 {
		t.Errorf("no subtitles: %v", nums(s, sN))
	}

	// nothing matches: the default track is kept, never a silent file
	cfg.AudioLangs = []string{"ger"}
	a, _, w, _ := selectTracks(cfg, tr, false)
	if got := nums(a, aN); len(got) != 1 || got[0] != 1 || !strings.Contains(strings.Join(w, " "), "no audio track matches") {
		t.Errorf("fallback: %v %v", got, w)
	}
}

func TestSelectExplicit(t *testing.T) {
	tr := testTracks(t)
	cfg := baseCfg()
	cfg.AudioSel = []trackSel{{N: 3, Codec: "eac3"}, {N: 1}}
	cfg.AudioDefault = 3
	cfg.SubSel = []trackSel{{N: 3, Forced: true}}
	cfg.SubDefault = -1
	a, s, _, err := selectTracks(cfg, tr, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 2 || a[0].Src.N != 3 || a[0].Codec != "eac3" || a[0].Bitrate != "640k" || !a[0].Default || a[1].Codec != "copy" || a[1].Default {
		t.Fatalf("audio: %+v", a)
	}
	if len(s) != 1 || !s[0].Forced || s[0].Default {
		t.Fatalf("subs: %+v", s)
	}
	args := strings.Join(audioArgs(a, 1), " ")
	for _, want := range []string{"-map 1:a:2 -c:a:0 eac3 -b:a:0 640k", "-disposition:a:0 default", "-map 1:a:0 -c:a:1 copy", "-disposition:a:1 0"} {
		if !strings.Contains(args, want) {
			t.Errorf("audio args miss %q: %s", want, args)
		}
	}
	if sa := strings.Join(subtitleArgs(s, 0), " "); sa != "-map 0:s:2 -c:s:0 copy -disposition:s:0 forced" {
		t.Errorf("subtitle args: %s", sa)
	}

	// a 7.1 track converted to E-AC-3 is mixed to 5.1 and retitled
	cfg.AudioSel = []trackSel{{N: 1, Codec: "eac3", Bitrate: "768k"}}
	a, _, _, _ = selectTracks(cfg, tr, false)
	args = strings.Join(audioArgs(a, 0), " ")
	if !strings.Contains(args, "-b:a:0 768k") || !strings.Contains(args, "-ac:a:0 6") || !strings.Contains(args, "title=E-AC-3 5.1") {
		t.Errorf("7.1 to E-AC-3: %s", args)
	}

	// none kept: allowed, with a warning
	cfg.AudioSel = []trackSel{}
	if a, _, w, _ := selectTracks(cfg, tr, false); len(a) != 0 || len(w) == 0 {
		t.Errorf("no audio: %v %v", a, w)
	}
	cfg.AudioSel = []trackSel{{N: 9}}
	if _, _, _, err := selectTracks(cfg, tr, false); err == nil {
		t.Error("a track that does not exist was accepted")
	}
}

func TestSelectMP4(t *testing.T) {
	tr := testTracks(t)
	a, s, w, _ := selectTracks(baseCfg(), tr, true)
	if a[0].Codec != "eac3" || a[1].Codec != "copy" || a[2].Codec != "copy" { // TrueHD cannot go into MP4; AC-3 and FLAC can
		t.Errorf("mp4 audio: %+v", a)
	}
	if got := nums(s, sN); len(got) != 3 || s[0].Codec != "mov_text" { // PGS dropped, text becomes mov_text
		t.Errorf("mp4 subs: %v", got)
	}
	if len(w) < 3 {
		t.Errorf("warnings: %v", w)
	}
}

func TestDisposition(t *testing.T) {
	src := map[string]bool{"default": true, "hearing_impaired": true, "forced": true}
	if d := disposition(src, false, false, true); d != "hearing_impaired" {
		t.Error(d)
	}
	if d := disposition(src, true, true, true); d != "default+forced+hearing_impaired" {
		t.Error(d)
	}
	if d := disposition(map[string]bool{}, false, false, false); d != "0" {
		t.Error(d)
	}
}

func TestTrackSelCommandLine(t *testing.T) {
	sel, err := parseAudioSel("1,2:eac3,3:aac:192k,4:opus")
	if err != nil || len(sel) != 4 || sel[1].Codec != "eac3" || sel[2].Bitrate != "192k" || sel[3].Codec != "libopus" {
		t.Fatalf("%+v %v", sel, err)
	}
	if s := audioSelString(sel); s != "1,2:eac3,3:aac:192k,4:opus" {
		t.Errorf("round trip: %s", s)
	}
	if sel, _ := parseAudioSel("all"); sel != nil {
		t.Error("all")
	}
	if sel, _ := parseAudioSel("none"); sel == nil || len(sel) != 0 {
		t.Error("none")
	}
	for _, bad := range []string{"x", "0", "1:mp9", "1:aac:fast", "1:aac:1k:x"} {
		if _, err := parseAudioSel(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	ss, err := parseSubSel("1,3:forced")
	if err != nil || len(ss) != 2 || ss[0].Forced || !ss[1].Forced || subSelString(ss) != "1,3:forced" {
		t.Errorf("%+v %v", ss, err)
	}
	if _, err := parseSubSel("2:big"); err == nil {
		t.Error("bad subtitle option accepted")
	}
}

func TestTrackSettingsFromWindow(t *testing.T) {
	s := &settings{Input: "a.mkv", Output: "b.mkv", Mode: "encode", Quality: "crf", CRF: 18, Audio: "copy",
		AudioTracks: []trackSel{{N: 1, Codec: "copy"}, {N: 3, Codec: "opus"}}, AudioDefault: 3,
		SubTracks: []trackSel{{N: 2, Forced: true}}, SubDefault: -1,
		AudioLang: "eng, jpn", DropCommentary: true, SubLang: "eng", SubMode: "forced"}
	cfg, err := s.toConfig()
	if err != nil {
		t.Fatal(err)
	}
	a := strings.Join(cfg.cliArgs(), " ")
	for _, want := range []string{"-audio-tracks 1:copy,3:opus", "-sub-tracks 2:forced", "-audio-default 3", "-sub-default none",
		"-audio-lang eng,jpn", "-sub-lang eng", "-drop-commentary", "-subs forced"} {
		if !strings.Contains(a, want) {
			t.Errorf("missing %q in %s", want, a)
		}
	}
	s.AudioTracks, s.SubTracks = nil, nil // rules decide: no track lists passed on
	cfg, _ = s.toConfig()
	if a := strings.Join(cfg.cliArgs(), " "); strings.Contains(a, "-audio-tracks") || strings.Contains(a, "-sub-tracks") {
		t.Errorf("rules only: %s", a)
	}
	s.SubMode = "some"
	if _, err := s.toConfig(); err == nil {
		t.Error("bad subtitle mode accepted")
	}
}

func TestAudioSizeOfChosenTracks(t *testing.T) {
	tr := testTracks(t)
	cfg := baseCfg()
	cfg.AudioSel = []trackSel{{N: 1}, {N: 2, Codec: "aac", Bitrate: "128k"}} // TrueHD copied (size known), commentary converted
	a, _, _, _ := selectTracks(cfg, tr, false)
	b, exact := quickAudioBytes(a, 100, false)
	if !exact || b != 123456789+128000*100/8 {
		t.Errorf("chosen tracks: %d %v", b, exact)
	}
	cfg.AudioSel = []trackSel{{N: 3}} // FLAC copy, no size information
	a, _, _, _ = selectTracks(cfg, tr, false)
	if _, exact := quickAudioBytes(a, 100, false); exact {
		t.Error("a track without size information must make the estimate inexact")
	}
}
