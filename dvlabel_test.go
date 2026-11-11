package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// A re-encode that kept the original's Dolby Vision label but holds no Dolby
// Vision data is converted as what it is (here SDR), not as Dolby Vision.
func TestDVLabelWithoutData(t *testing.T) {
	label := `{"streams":[{"codec_name":"hevc","width":1920,"height":1080,"pix_fmt":"yuv420p10le",
	  "color_space":"bt709","color_transfer":"bt709","color_primaries":"bt709","avg_frame_rate":"24/1",
	  "side_data_list":[{"side_data_type":"DOVI configuration record","dv_profile":7,"dv_level":6,"dv_bl_signal_compatibility_id":6}]}],
	 "format":{"duration":"100"},"frames":[%s]}`
	none := fmt.Sprintf(label, `{"side_data_list":[]}`)
	with := fmt.Sprintf(label, `{"side_data_list":[{"side_data_type":"Dolby Vision RPU Data"},{"side_data_type":"Dolby Vision Metadata"}]}`)

	rpuVersions.Store("new-ffprobe", true)
	rpuVersions.Store("old-ffprobe", false)
	ctx := context.Background()

	p, err := parseProbe([]byte(none))
	if err != nil || p.DV == nil || !p.noRPU {
		t.Fatalf("parse: %+v %v", p, err)
	}
	p.labelOnly(ctx, "old-ffprobe") // an old FFprobe cannot show the data: the label is believed
	if p.DV == nil || p.DVLabelOnly != nil {
		t.Fatal("old ffprobe: the label should be kept")
	}
	p.labelOnly(ctx, "new-ffprobe")
	if p.DV != nil || p.DVLabelOnly == nil || p.DVLabelOnly.Profile != 7 {
		t.Fatalf("label only: DV %+v, DVLabelOnly %+v", p.DV, p.DVLabelOnly)
	}
	cfg := &Config{Input: "x.mkv", Output: "y.mkv", Codec: "x265", CRF: 18}
	plan, err := makePlan(cfg, p)
	if err != nil {
		t.Fatal(err)
	}
	if plan.UseDV || !strings.Contains(strings.Join(plan.Notes, "\n"), "labelled Dolby Vision (profile 7.6), but its video holds no Dolby Vision data") {
		t.Fatalf("plan: UseDV %v, notes %q", plan.UseDV, plan.Notes)
	}

	p, _ = parseProbe([]byte(with))
	p.labelOnly(ctx, "new-ffprobe")
	if p.DV == nil || p.DVLabelOnly != nil {
		t.Fatal("real Dolby Vision was not kept")
	}
}

func TestFFmpegMajor(t *testing.T) {
	for v, want := range map[string]bool{
		"ffprobe version 6.1.1-3ubuntu5 Copyright":   true,
		"ffprobe version 4.4.2-0ubuntu0.22.04.1":     false,
		"ffprobe version n7.0.2-gpl":                 true,
		"ffprobe version N-117000-g1234 Copyright":   true,
		"ffprobe version 2024-05-01-git-abcdef-full": true,
		"ffprobe version 5.0":                        true,
		"something else":                             true,
	} {
		if got := ffmpegMajorAtLeast(v, 5); got != want {
			t.Errorf("%q: %v, want %v", v, got, want)
		}
	}
}

// A file whose Dolby Vision data cannot be read is not retried "the normal way":
// the full-GPU path is not what failed.
func TestNotGPUError(t *testing.T) {
	err := fmt.Errorf("[2/5]: %w", notGPUError{errors.New("the Dolby Vision data could not be read")})
	var ng notGPUError
	if !errors.As(err, &ng) || !strings.Contains(err.Error(), "could not be read") {
		t.Fatal("notGPUError lost when wrapped")
	}
}
