package main

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestJudgeIdet(t *testing.T) {
	// the Carol Burnett episode: labelled progressive, every frame combed, bottom field first
	tff, bff, prog := parseIdet(`[Parsed_idet_0 @ 0x1] Multi frame detection: TFF:     0 BFF:     0 Progressive:     0 Undetermined:     0
[Parsed_idet_0 @ 0x2] Multi frame detection: TFF:     0 BFF:  3595 Progressive:     5 Undetermined:     0`)
	if tff != 0 || bff != 3595 || prog != 5 {
		t.Fatalf("parse: %d %d %d", tff, bff, prog)
	}
	if d := judgeIdet(tff, bff, prog); !d.Interlaced || d.Order != "bff" {
		t.Errorf("Carol Burnett: %+v", d)
	}
	if d := judgeIdet(2, 3, 590); d.Interlaced {
		t.Errorf("a progressive film: %+v", d)
	}
	if d := judgeIdet(200, 10, 300); !d.Interlaced || d.Order != "tff" {
		t.Errorf("a show with film inserts: %+v", d)
	}
	if d := judgeIdet(142, 111, 47); d.Interlaced {
		t.Errorf("fine detail, mixed orders: %+v", d)
	}
	for _, c := range []struct {
		h     int
		order string
		want  bool
	}{{480, "progressive", true}, {576, "", true}, {1080, "progressive", false}, {1080, "tt", true}, {1080, "", true}, {2160, "", false}, {720, "progressive", false}} {
		if worthChecking(c.h, c.order) != c.want {
			t.Errorf("worthChecking(%d, %q) = %v", c.h, c.order, !c.want)
		}
	}
}

func TestDeinterlacePlan(t *testing.T) {
	d := &interlaceInfo{Checked: true, Interlaced: true, Order: "bff"}
	plan := &Plan{}
	applyDeinterlace(&Config{}, plan, d)
	if plan.deintFilter() != "bwdif=mode=send_field:parity=bff:deint=all" {
		t.Errorf("smooth: %q", plan.deintFilter())
	}
	if got := outFPS(&Probe{AvgFPS: "30000/1001"}, plan); got < 59.9 || got > 60 {
		t.Errorf("fps: %v", got)
	}
	plan = &Plan{}
	applyDeinterlace(&Config{Deinterlace: "standard"}, plan, d)
	if !strings.Contains(plan.deintFilter(), "send_frame") {
		t.Errorf("standard: %q", plan.deintFilter())
	}
	for _, c := range []struct {
		cfg  Config
		plan Plan
	}{{Config{Deinterlace: "off"}, Plan{}}, {Config{CopyVideo: true}, Plan{}}, {Config{}, Plan{UseDV: true}}} {
		p := c.plan
		applyDeinterlace(&c.cfg, &p, d)
		if p.Deint != "" {
			t.Errorf("%+v: deinterlaced", c.cfg)
		}
	}
	p := Plan{UseHDR10Plus: true}
	applyDeinterlace(&Config{}, &p, d)
	if p.Deint != "standard" {
		t.Errorf("HDR10+: %q", p.Deint)
	}
	// the filter comes first, before resizing
	cfg := &Config{Codec: "x265", Preset: "medium", CRF: 18}
	plan = &Plan{Kind: "sdr", ScaleW: 640, ScaleH: 480}
	applyDeinterlace(cfg, plan, d)
	args, _ := buildVideoArgs(cfg, &Probe{}, plan, "")
	a := strings.Join(args, " ")
	if !strings.Contains(a, "-vf bwdif=mode=send_field:parity=bff:deint=all,scale=640:480") {
		t.Errorf("args: %s", a)
	}
	// the Plex check: fixed when deinterlaced, open with a hint when it is off
	src := plexSrc{VCodec: "h264", Interlaced: true, Audio: []plexTrack{{Codec: "aac"}}}
	if got := plexKeys(plexOutcome(src, plexAfter(src, cfg, plan), cfg)); got["interlaced"] != "fixed" {
		t.Errorf("plex: %v", got)
	}
	off := &Config{Deinterlace: "off"}
	res := plexOutcome(src, plexAfter(src, off, &Plan{}), off)
	if res[0].State != "open" || !strings.Contains(res[0].How, "Deinterlace") {
		t.Errorf("plex, off: %+v", res)
	}
}

// A real interlaced picture, labelled progressive: found, with its field order.
func TestDetectInterlaceReal(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not found")
	}
	dir := t.TempDir()
	in := filepath.Join(dir, "tv.mkv")
	// 60 fps motion woven into 30 fps interlaced frames (bottom field first), then labelled progressive
	out, err := exec.Command(ffmpeg, "-v", "error", "-f", "lavfi", "-i", "testsrc2=s=720x480:r=60:d=12", "-vf", "tinterlace=mode=interleave_bottom,setfield=prog",
		"-c:v", "libx264", "-crf", "12", "-flags", "-ildct-ilme", "-x264-params", "log-level=error", in).CombinedOutput()
	if err != nil {
		t.Skipf("cannot make the test video: %v %s", err, out)
	}
	d := detectInterlace(context.Background(), ffmpeg, in, 480, "progressive", 12)
	if !d.Interlaced {
		t.Errorf("not found: %+v", d)
	}
	prog := filepath.Join(dir, "film.mkv")
	if out, err := exec.Command(ffmpeg, "-v", "error", "-f", "lavfi", "-i", "testsrc2=s=720x480:r=30:d=12", "-c:v", "libx264", "-x264-params", "log-level=error", prog).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if d := detectInterlace(context.Background(), ffmpeg, prog, 480, "progressive", 12); d.Interlaced {
		t.Errorf("a progressive video: %+v", d)
	}
}
