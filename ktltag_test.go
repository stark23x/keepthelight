package main

import (
	"strings"
	"testing"
)

func TestKTLTag(t *testing.T) {
	cfg := &Config{Codec: "nvenc", CRF: 22}
	p := &Probe{Width: 3840, Height: 2160}
	if got := ktlTag(cfg, p, &Plan{ScaleW: 1920, ScaleH: 1080}); !strings.HasSuffix(got, "NVENC CRF 22 · 1920×1080") || !strings.HasPrefix(got, "Keep the Light ") {
		t.Errorf("tag: %q", got)
	}
	if ktlTag(&Config{CopyVideo: true}, p, &Plan{}) != "" {
		t.Error("a copied video was tagged")
	}
	if ktlTagIn(map[string]string{"keep_the_light": "x"}) != "x" {
		t.Error("the tag's key case matters")
	}
	// the scan: shown as done, with no saving expected
	it := &scanItem{Size: 8 << 30}
	analyse(it, []byte(`{"streams":[{"codec_type":"video","codec_name":"h264","width":1920,"height":1080,"bit_rate":"30000000"}],
		"format":{"duration":"6000","size":"8589934592","tags":{"KEEP_THE_LIGHT":"Keep the Light 1.29.0 · x265 CRF 18 · 1920×1080"}}}`))
	if it.Done == "" || it.Saving != 0 || it.Worth {
		t.Errorf("scan: %+v", it)
	}
	// the window and the queue are warned
	plan, err := makePlan(&Config{Codec: "x265", CRF: 18}, &Probe{Codec: "hevc", Width: 1920, Height: 1080, Converted: "Keep the Light 1.28.0"})
	if err != nil || !strings.Contains(strings.Join(plan.Warnings, " "), "already converted") {
		t.Errorf("plan: %v %v", plan, err)
	}
}
