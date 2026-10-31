package main

import (
	"strings"
	"testing"
)

func plexKeys(list []plexIssue) map[string]string {
	m := map[string]string{}
	for _, x := range list {
		m[x.Key] = x.State
	}
	return m
}

func TestPlexIssues(t *testing.T) {
	src := plexSrc{VCodec: "h264", PixFmt: "yuv420p10le", DVProfile: 7, Kbps: 95000,
		Audio: []plexTrack{{Codec: "truehd"}, {Codec: "dts"}},
		Subs:  []plexTrack{{Codec: "hdmv_pgs_subtitle", Forced: true}, {Codec: "ass", Default: true}, {Codec: "dvd_subtitle", Forced: true}}}
	got := plexKeys(plexIssues(src))
	for _, k := range []string{"h264-10", "forced-pgs", "forced-bitmap", "ass-default", "dv7", "audio", "bitrate"} {
		if _, ok := got[k]; !ok {
			t.Errorf("missing %s: %v", k, got)
		}
	}
	clean := plexSrc{VCodec: "hevc", PixFmt: "yuv420p10le", Kbps: 20000, Audio: []plexTrack{{Codec: "truehd"}, {Codec: "eac3"}},
		Subs: []plexTrack{{Codec: "hdmv_pgs_subtitle"}, {Codec: "subrip", Forced: true}}}
	if l := plexIssues(clean); len(l) != 0 {
		t.Errorf("a clean file has problems: %v", l)
	}
}

func TestPlexOutcome(t *testing.T) {
	pgs := srcTrack{N: 1, Codec: "hdmv_pgs_subtitle", Disp: map[string]bool{"forced": true}}
	ass := srcTrack{N: 2, Codec: "ass", Disp: map[string]bool{"default": true}}
	thd := srcTrack{N: 1, Codec: "truehd", Channels: 8}
	src := plexSrc{VCodec: "h264", PixFmt: "yuv420p10le", DVProfile: 7, Kbps: 90000,
		Audio: []plexTrack{{Codec: "truehd"}},
		Subs:  []plexTrack{{Codec: pgs.Codec, Forced: true}, {Codec: ass.Codec, Default: true}}}

	// re-encode, PGS to SRT, ASS dropped, a compatible track added, DV kept as 8.1: everything fixed
	cfg := &Config{}
	plan := &Plan{UseDV: true, DVProfile: "8.1",
		Audio: []outAudio{{Src: thd, Codec: "copy", Default: true}, {Src: thd, Codec: "eac3"}},
		Subs:  []outSub{{Src: pgs, Forced: true, ToSRT: true, Codec: "srt"}}}
	res := plexOutcome(src, plexAfter(src, cfg, plan), cfg)
	if plexProblems(res) != 0 {
		t.Fatalf("all should be fixed: %+v", res)
	}
	for _, x := range res {
		if x.How == "" {
			t.Errorf("%s: no explanation", x.Key)
		}
	}
	for _, x := range res {
		if x.Key == "dv7" && x.How != "Dolby Vision becomes profile 8.1" {
			t.Errorf("dv7: %q", x.How)
		}
	}

	// metadata only: nothing changes, every problem stays, with a way to fix it
	cfg = &Config{CopyVideo: true, MetaOnly: true}
	plan = &Plan{Audio: []outAudio{{Src: thd, Codec: "copy"}},
		Subs: []outSub{{Src: pgs, Forced: true, Codec: "copy"}, {Src: ass, Default: true, Codec: "copy"}}}
	res = plexOutcome(src, plexAfter(src, cfg, plan), cfg)
	for _, x := range res {
		if x.State != "open" {
			t.Errorf("%s: %s, want open", x.Key, x.State)
		}
		if x.How == "" {
			t.Errorf("%s: no hint", x.Key)
		}
	}
	if !strings.Contains(res[0].How, "Re-encode") {
		t.Errorf("video hint: %q", res[0].How)
	}

	// an SRT copy next to the forced PGS track leaves the PGS track forced: still a problem
	cfg = &Config{}
	plan = &Plan{Audio: []outAudio{{Src: thd, Codec: "eac3"}},
		Subs: []outSub{{Src: pgs, Forced: true, Codec: "copy"}, {Src: pgs, ToSRT: true, Added: true, Codec: "srt"}}}
	got := plexKeys(plexOutcome(src, plexAfter(src, cfg, plan), cfg))
	if got["forced-pgs"] != "open" || got["ass-default"] != "fixed" || got["audio"] != "fixed" || got["h264-10"] != "fixed" || got["bitrate"] != "fixed" {
		t.Errorf("SRT added next to the original: %v", got)
	}

	// Dolby Vision profile 5 copied: cannot be fixed
	src5 := plexSrc{VCodec: "hevc", DVProfile: 5}
	cfg = &Config{CopyVideo: true}
	if got := plexKeys(plexOutcome(src5, plexAfter(src5, cfg, &Plan{}), cfg)); got["dv5"] != "cannot" {
		t.Errorf("profile 5: %v", got)
	}
}

// The library scan and the check when files are added use the same wording.
func TestPlexScanAgrees(t *testing.T) {
	it := &scanItem{Size: 1 << 30}
	analyse(it, []byte(`{"streams":[{"codec_type":"video","codec_name":"h264","pix_fmt":"yuv420p10le","width":1920,"height":1080},
		{"codec_type":"audio","codec_name":"dts","channels":6},
		{"codec_type":"subtitle","codec_name":"hdmv_pgs_subtitle","disposition":{"forced":1}}],
		"format":{"duration":"100","size":"1073741824","bit_rate":"20000000"}}`))
	want := plexIssues(plexSrc{VCodec: "h264", PixFmt: "yuv420p10le", Kbps: 20000, Audio: []plexTrack{{Codec: "dts"}},
		Subs: []plexTrack{{Codec: "hdmv_pgs_subtitle", Forced: true}}})
	if len(it.Issues) != len(want) || len(want) != 3 {
		t.Fatalf("scan %v, check %v", it.Issues, want)
	}
	for i := range want {
		if it.Issues[i] != want[i].Text {
			t.Errorf("%d: %q vs %q", i, it.Issues[i], want[i].Text)
		}
	}
}
