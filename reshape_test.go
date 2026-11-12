package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const p5Probe = `{"streams":[{"codec_name":"hevc","width":3840,"height":2080,"pix_fmt":"yuv420p10le","color_range":"pc",
  "r_frame_rate":"24000/1001","avg_frame_rate":"24000/1001",
  "side_data_list":[{"side_data_type":"DOVI configuration record","dv_profile":5,"dv_level":6,"dv_bl_signal_compatibility_id":0,"rpu_present_flag":1,"bl_present_flag":1}]}],
 "format":{"duration":"8907.5"},
 "frames":[{"side_data_list":[{"side_data_type":"Dolby Vision RPU Data"},{"side_data_type":"Dolby Vision Metadata"}]}]}`

// Profile 5 is converted: colours to HDR10 first, Dolby Vision to 8.1 (or dropped).
func TestProfile5Plan(t *testing.T) {
	p, err := parseProbe([]byte(p5Probe))
	if err != nil || p.DV == nil || p.DV.Profile != 5 {
		t.Fatalf("probe: %+v %v", p, err)
	}
	cfg := &Config{Input: "in.mkv", Output: "out.mkv", Codec: "x265", CRF: 18, Preset: "slow", MasterDisplay: p3D65Prims + "L(10000000,1)", MaxCLL: "933,171"}
	plan, err := makePlan(cfg, p)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Reshape || plan.Kind != "hdr10" || !plan.UseDV || plan.DVProfile != "8.1" || plan.RPUMode != 3 {
		t.Fatalf("plan: %+v", plan)
	}
	for _, w := range plan.Warnings {
		if strings.Contains(w, "untagged") || strings.Contains(w, "mastering-display") {
			t.Errorf("unexpected warning: %s", w)
		}
	}
	args, _ := buildVideoArgs(cfg, p, plan, "")
	a := strings.Join(args, " ")
	if !strings.Contains(a, "-vf "+reshapeFilter) {
		t.Errorf("the colour conversion must come first: %s", a)
	}
	for _, want := range []string{"-color_range tv", "-color_primaries bt2020", "-color_trc smpte2084", "-colorspace bt2020nc",
		"master-display=G(13250,34500)", "max-cll=933,171", "hdr10=1"} {
		if !strings.Contains(a, want) {
			t.Errorf("missing %q in %s", want, a)
		}
	}
	plan.ScaleW, plan.ScaleH = 1920, 1040
	args, _ = buildVideoArgs(cfg, p, plan, "")
	if a := strings.Join(args, " "); !strings.Contains(a, "-vf "+reshapeFilter+",scale=1920:1040") {
		t.Errorf("resize after the conversion: %s", a)
	}

	cfg.DV = "drop"
	plan, _ = makePlan(cfg, p)
	if !plan.Reshape || plan.UseDV || plan.Kind != "hdr10" {
		t.Fatalf("drop: %+v", plan)
	}
	cfg.DV, cfg.CopyVideo = "", true
	plan, err = makePlan(cfg, p)
	if err != nil || plan.Reshape || plan.UseDV {
		t.Fatalf("copy video: %+v %v", plan, err)
	}

	cfg = &Config{Codec: "nvenc", vulkan: true}
	if a := strings.Join(cfg.inputPrefix(), " "); a != "-init_hw_device vulkan" {
		t.Errorf("input prefix: %q", a)
	}
	if g, msg := chooseGPUPath(context.Background(), cfg, p, &Plan{Reshape: true}); g != nil || !strings.Contains(msg, "profile 5") {
		t.Errorf("full-GPU path with profile 5: %v %q", g, msg)
	}
}

func TestDoviStatic(t *testing.T) {
	s := `Summary:
  Frames: 564
  Profile: 5
  RPU mastering display: 0.0001/1000 nits
  RPU content light level (L1): MaxCLL: 265.19 nits, MaxFALL: 10.05 nits
  L6 metadata: Mastering display: 0.0001/1000 nits. MaxCLL: 933 nits, MaxFALL: 171 nits
  L9 MDP: DCI-P3 D65`
	md, cll := doviStatic(s)
	if md != p3D65Prims+"L(10000000,1)" || cll != "933,171" {
		t.Errorf("L6: %s %s", md, cll)
	}
	md, cll = doviStatic("RPU mastering display: 0.005/4000 nits\n  RPU content light level (L1): MaxCLL: 265.19 nits, MaxFALL: 10.05 nits\n  L9 MDP: BT.2020")
	if md != bt2020Prims+"L(40000000,50)" || cll != "" {
		t.Errorf("no L6: %s %q", md, cll)
	}
	if md, _ := doviStatic(""); md != p3D65Prims+"L(10000000,1)" {
		t.Errorf("nothing: %s", md)
	}
}

// With a real profile 5 file (KTL_P5_SAMPLE) and an FFmpeg with libplacebo:
// the whole job as plain HDR10 (no MP4Box needed), checked for its colour tags.
func TestProfile5Job(t *testing.T) {
	in := os.Getenv("KTL_P5_SAMPLE")
	ffmpeg, e1 := exec.LookPath("ffmpeg")
	ffprobe, e2 := exec.LookPath("ffprobe")
	if in == "" || e1 != nil || e2 != nil {
		t.Skip("no profile 5 sample (KTL_P5_SAMPLE) or no ffmpeg")
	}
	if err := checkReshape(context.Background(), ffmpeg, in, vulkanInit); err != nil {
		t.Skip(err)
	}
	out := filepath.Join(t.TempDir(), "out.mkv")
	cfg := &Config{Input: in, Output: out, Codec: "x265", CRF: 28, Preset: "ultrafast", DV: "drop", Ffmpeg: ffmpeg, Ffprobe: ffprobe}
	if err := transcode(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	b, _ := exec.Command(ffprobe, "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=color_range,color_transfer,color_primaries",
		"-show_frames", "-read_intervals", "%+#1", "-of", "compact", out).Output()
	s := string(b)
	for _, want := range []string{"color_range=tv", "color_transfer=smpte2084", "color_primaries=bt2020", "Mastering display"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q:\n%s", want, s)
		}
	}
}

func TestVulkanDevices(t *testing.T) {
	log := `[Vulkan @ 000001] GPU listing:
[Vulkan @ 000001]     0: Intel(R) UHD Graphics 770 (integrated) (0xa780)
[Vulkan @ 000001]     1: NVIDIA GeForce RTX 4070 Laptop GPU (discrete) (0x2820)
[Vulkan @ 000001] Device 0 selected: Intel(R) UHD Graphics 770 (integrated) (0xa780)`
	devs := parseVulkanDevices(log)
	if len(devs) != 2 || devs[1].Name != "NVIDIA GeForce RTX 4070 Laptop GPU" || devs[0].Kind != "integrated" {
		t.Fatalf("%+v", devs)
	}
	for codec, want := range map[string]int{"nvenc": 1, "qsv": 0, "x265": 1} {
		if got := pickVulkan(devs, codec); got != want {
			t.Errorf("%s: device %d, want %d", codec, got, want)
		}
	}
	if pickVulkan(devs[:1], "nvenc") != -1 {
		t.Error("one device: leave it to FFmpeg")
	}
	cfg := &Config{}
	if a := strings.Join(cfg.vulkanArgs(), " "); a != "-init_hw_device vulkan" {
		t.Errorf("default device: %s", a)
	}
	cfg.vulkanDev = "1"
	if a := strings.Join(cfg.vulkanArgs(), " "); a != "-init_hw_device vulkan=ktlvk:1 -filter_hw_device ktlvk" {
		t.Errorf("chosen device: %s", a)
	}
}

func TestP5Compare(t *testing.T) {
	line := func(n int, y string) string {
		return fmt.Sprintf("n:%d mse_avg:0.00 mse_y:0.00 mse_u:0.00 mse_v:0.00 psnr_avg:inf psnr_y:%s psnr_u:inf psnr_v:62.10\n", n, y)
	}
	var good, bad, short strings.Builder
	for i := 1; i <= p5CheckFrames; i++ {
		good.WriteString(line(i, "inf"))
		y := "inf"
		if i == 30 {
			y = "14.2" // one frame without its Dolby Vision data
		}
		bad.WriteString(line(i, y))
		if i < 40 {
			short.WriteString(line(i, "inf"))
		}
	}
	if ok, why := p5Compare(good.String()); !ok {
		t.Errorf("good: %s", why)
	}
	if ok, why := p5Compare(bad.String()); ok || !strings.Contains(why, "differs") {
		t.Errorf("bad: %v %s", ok, why)
	}
	if ok, why := p5Compare(short.String()); ok || !strings.Contains(why, "39 of 48") {
		t.Errorf("short: %v %s", ok, why)
	}
}

// With a real profile 5 file: the check passes for a way that decodes the
// same, and fails (falling back) for one that cannot run.
func TestChooseP5Way(t *testing.T) {
	in := os.Getenv("KTL_P5_SAMPLE")
	ffmpeg, err := exec.LookPath("ffmpeg")
	if in == "" || err != nil {
		t.Skip("no profile 5 sample (KTL_P5_SAMPLE) or no ffmpeg")
	}
	if err := checkReshape(context.Background(), ffmpeg, in, vulkanInit); err != nil {
		t.Skip(err)
	}
	cfg := &Config{Input: in, vulkanDev: "0"}
	ok, why := sameAsCPU(context.Background(), ffmpeg, cfg, p5Way{hw: nil, desc: "processor"}, 0)
	if !ok {
		t.Fatalf("the same way must match itself: %s", why)
	}
	w, msg := chooseP5Way(context.Background(), ffmpeg, cfg, 6)
	t.Log(msg)
	if w.onGPU && !strings.Contains(msg, "checked on this file") {
		t.Errorf("%+v %s", w, msg)
	}
}
