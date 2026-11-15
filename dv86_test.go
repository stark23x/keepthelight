package main

import (
	"strings"
	"testing"
)

// Profile 8.6 (an HDR10 picture underneath, labelled the UHD Blu-ray way) becomes 8.1, like a disc's profile 7.
func TestDolbyVision86(t *testing.T) {
	p := &Probe{Codec: "hevc", Width: 3840, Height: 2160, PixFmt: "yuv420p10le", Primaries: "bt2020", Transfer: "smpte2084", Space: "bt2020nc",
		DV: &DoviConfig{Profile: 8, Compat: 6, Level: 6}}
	plan, err := makePlan(baseCfg(), p)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.UseDV || plan.DVProfile != "8.1" || plan.RPUMode != 2 || !strings.Contains(strings.Join(plan.Notes, " "), "8.6") {
		t.Fatalf("plan: UseDV %v %q mode %d notes %q", plan.UseDV, plan.DVProfile, plan.RPUMode, plan.Notes)
	}
	// untagged: the 6 says HDR10
	p.Primaries, p.Transfer, p.Space = "", "", ""
	if plan, err = makePlan(baseCfg(), p); err != nil || plan.Kind != "hdr10" || plan.DVProfile != "8.1" {
		t.Fatalf("untagged: %+v %v", plan, err)
	}
	// an id with no recognisable picture underneath: a clear refusal
	p.DV.Compat = 3
	if _, err := makePlan(baseCfg(), p); err == nil || !strings.Contains(err.Error(), "Copy video untouched") {
		t.Errorf("unknown id: %v", err)
	}
	// the usual ones are unchanged
	p.DV.Compat, p.Transfer = 1, "smpte2084"
	if plan, _ = makePlan(baseCfg(), p); plan.DVProfile != "8.1" || plan.RPUMode != 0 {
		t.Errorf("8.1: %q mode %d", plan.DVProfile, plan.RPUMode)
	}
}
