package main

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestJoinCrops(t *testing.T) {
	// a 4:3 show in a 1080p frame: bars at the sides, the same throughout
	b, ok := joinCrops([]cropBox{{1440, 1080, 240, 0}, {1440, 1080, 240, 0}, {1436, 1080, 242, 0}}, 1920, 1080)
	if !ok || b.X != 238 || b.W != 1444 || b.Y != 0 || b.H != 1080 {
		t.Errorf("4:3 in 1080p: %+v %v", b, ok)
	}
	// the credits fill the frame: nothing can be cut without losing picture
	if _, ok := joinCrops([]cropBox{{1440, 1080, 240, 0}, {1920, 1080, 0, 0}}, 1920, 1080); ok {
		t.Error("cropped although one stretch fills the frame")
	}
	// a black stretch (a fade) says nothing
	b, ok = joinCrops([]cropBox{{1440, 1080, 240, 0}, {16, 16, 952, 532}}, 1920, 1080)
	if !ok || b.W != 1444 {
		t.Errorf("with a black stretch: %+v %v", b, ok)
	}
	// a thin edge (a few pixels) is not worth a crop
	if _, ok := joinCrops([]cropBox{{1912, 1076, 4, 2}}, 1920, 1080); ok {
		t.Error("cropped a thin edge")
	}
	if !cropAllowed("480p") || !cropAllowed("576p") || cropAllowed("720p") || cropAllowed("") {
		t.Error("cropping is for 576p and 480p only")
	}
	// the plan: bars cut, then fitted into 480p; the filter order is deinterlace, crop, resize
	cfg := &Config{Crop: true, Scale: "480p", Codec: "x265", Preset: "medium"}
	p := &Probe{Width: 1920, Height: 1080}
	plan := &Plan{Kind: "sdr", Deint: "smooth", FieldOrder: "bff"}
	applyCrop(cfg, p, plan, &cropBox{1440, 1080, 240, 0})
	if plan.ScaleW != 640 || plan.ScaleH != 480 {
		t.Errorf("fitted: %dx%d", plan.ScaleW, plan.ScaleH)
	}
	args, _ := buildVideoArgs(cfg, p, plan, "")
	if a := strings.Join(args, " "); !strings.Contains(a, "-vf bwdif=mode=send_field:parity=bff:deint=all,crop=1440:1080:240:0,scale=640:480") {
		t.Errorf("filters: %s", a)
	}
	dv := &Plan{UseDV: true}
	applyCrop(cfg, p, dv, &cropBox{1440, 1080, 240, 0})
	if dv.CropW != 0 {
		t.Error("Dolby Vision was cropped")
	}
	off := &Plan{}
	applyCrop(&Config{Crop: true, Scale: "720p"}, p, off, &cropBox{1440, 1080, 240, 0})
	if off.CropW != 0 {
		t.Error("cropped at 720p")
	}
}

// A real 4:3 picture inside a 1080p frame: the bars are found.
func TestDetectCropReal(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not found")
	}
	in := filepath.Join(t.TempDir(), "pillarbox.mkv")
	if out, err := exec.Command(ffmpeg, "-v", "error", "-f", "lavfi", "-i", "testsrc2=s=1440x1080:r=25:d=6", "-vf", "pad=1920:1080:240:0:black",
		"-c:v", "libx264", "-preset", "ultrafast", "-x264-params", "log-level=error", in).CombinedOutput(); err != nil {
		t.Skipf("cannot make the test video: %v %s", err, out)
	}
	c := detectCrop(context.Background(), ffmpeg, in, 1920, 1080, 6)
	if c == nil || c.W < 1440 || c.W > 1448 || c.H != 1080 || c.X < 236 || c.X > 240 {
		t.Errorf("found: %+v", c)
	}
}
