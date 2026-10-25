package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const sampleProbe = `{
 "streams":[{"codec_name":"hevc","width":3840,"height":2160,"pix_fmt":"yuv420p10le",
  "color_range":"tv","color_space":"bt2020nc","color_transfer":"smpte2084","color_primaries":"bt2020",
  "r_frame_rate":"24000/1001","avg_frame_rate":"24000/1001",
  "side_data_list":[{"side_data_type":"DOVI configuration record","dv_version_major":1,"dv_version_minor":0,
   "dv_profile":8,"dv_level":6,"rpu_present_flag":1,"el_present_flag":0,"bl_present_flag":1,"dv_bl_signal_compatibility_id":1}]}],
 "frames":[{"side_data_list":[
  {"side_data_type":"Mastering display metadata","red_x":"35400/50000","red_y":"14600/50000","green_x":"8500/50000","green_y":"39850/50000",
   "blue_x":"6550/50000","blue_y":"2300/50000","white_point_x":"15635/50000","white_point_y":"16450/50000",
   "min_luminance":"1/10000","max_luminance":"10000000/10000"},
  {"side_data_type":"Content light level metadata","max_content":1000,"max_average":400}]}]}`

func TestParseProbe(t *testing.T) {
	p, err := parseProbe([]byte(sampleProbe))
	if err != nil {
		t.Fatal(err)
	}
	if p.Kind() != "hdr10" || p.DV == nil || p.DV.Profile != 8 || p.DV.Compat != 1 {
		t.Fatalf("bad probe: %+v dv=%+v", p, p.DV)
	}
	md, err := p.Mastering.X265MasterDisplay()
	if err != nil {
		t.Fatal(err)
	}
	want := "G(8500,39850)B(6550,2300)R(35400,14600)WP(15635,16450)L(10000000,1)"
	if md != want {
		t.Fatalf("master-display\n got %s\nwant %s", md, want)
	}
	if p.CLL == nil || p.CLL.MaxCLL != 1000 || p.CLL.MaxFALL != 400 {
		t.Fatalf("bad CLL %+v", p.CLL)
	}
}

func baseCfg() *Config {
	return &Config{Input: "in.mkv", Output: "out.mkv", CRF: 18, Preset: "medium", Codec: "x265", Audio: "copy", DV: "auto"}
}

func TestHDR10Args(t *testing.T) {
	p, _ := parseProbe([]byte(sampleProbe))
	p.DV = nil
	cfg := baseCfg()
	plan, err := makePlan(cfg, p)
	if err != nil {
		t.Fatal(err)
	}
	args, _ := buildVideoArgs(cfg, p, plan, "")
	j := strings.Join(args, " ")
	for _, s := range []string{"libx265", "yuv420p10le", "hdr10=1", "transfer=smpte2084", "master-display=G(8500,39850)",
		"max-cll=1000,400", "repeat-headers=1", "-color_trc smpte2084", "-colorspace bt2020nc"} {
		if !strings.Contains(j, s) {
			t.Errorf("missing %q in %s", s, j)
		}
	}
}

func TestHLGArgs(t *testing.T) {
	p, _ := parseProbe([]byte(sampleProbe))
	p.DV, p.Mastering, p.CLL = nil, nil, nil
	p.Transfer = "arib-std-b67"
	cfg := baseCfg()
	plan, _ := makePlan(cfg, p)
	args, _ := buildVideoArgs(cfg, p, plan, "")
	j := strings.Join(args, " ")
	for _, s := range []string{"transfer=arib-std-b67", "atc-sei=18", "-color_trc arib-std-b67"} {
		if !strings.Contains(j, s) {
			t.Errorf("missing %q in %s", s, j)
		}
	}
	if strings.Contains(j, "master-display") {
		t.Error("HLG must not get master-display")
	}
}

func TestDVPlans(t *testing.T) {
	p, _ := parseProbe([]byte(sampleProbe))
	plan, err := makePlan(baseCfg(), p)
	if err != nil || !plan.UseDV || plan.DVProfile != "8.1" || plan.RPUMode != 0 {
		t.Fatalf("P8.1 plan wrong: %+v %v", plan, err)
	}
	p.DV = &DoviConfig{Profile: 7, Level: 6, Compat: 6, ELPresent: true}
	plan, err = makePlan(baseCfg(), p)
	if err != nil || plan.DVProfile != "8.1" || plan.RPUMode != 2 {
		t.Fatalf("P7 plan wrong: %+v %v", plan, err)
	}
	p.DV = &DoviConfig{Profile: 5, Compat: 0}
	if _, err = makePlan(baseCfg(), p); err == nil {
		t.Fatal("profile 5 must be refused for re-encode")
	}
	cfg := baseCfg()
	cfg.CopyVideo = true
	if plan, err = makePlan(cfg, p); err != nil || plan.UseDV {
		t.Fatalf("copy-video on P5 should be allowed: %+v %v", plan, err)
	}
	p.DV = &DoviConfig{Profile: 8, Compat: 4}
	p.Transfer = "arib-std-b67"
	if plan, err = makePlan(baseCfg(), p); err != nil || plan.DVProfile != "8.4" {
		t.Fatalf("P8.4 plan wrong: %+v %v", plan, err)
	}
}

func TestQSVArgs(t *testing.T) {
	p, _ := parseProbe([]byte(sampleProbe))
	p.DV = nil
	cfg := baseCfg()
	cfg.Codec = "qsv"
	cfg.CRF = 22
	cfg.Preset = "superfast"
	plan, _ := makePlan(cfg, p)
	args, _ := buildVideoArgs(cfg, p, plan, "")
	j := strings.Join(args, " ")
	for _, s := range []string{"hevc_qsv", "-preset veryfast", "-profile:v main10", "p010le", "-global_quality 22", "-color_trc smpte2084"} {
		if !strings.Contains(j, s) {
			t.Errorf("missing %q in %s", s, j)
		}
	}
	cfg.Bitrate = "20M"
	args, _ = buildVideoArgs(cfg, p, plan, "")
	if j = strings.Join(args, " "); !strings.Contains(j, "-b:v 20M") || strings.Contains(j, "global_quality") {
		t.Errorf("bitrate mode wrong: %s", j)
	}
}

func TestHDR10PlusOnlyWithX265(t *testing.T) {
	p, _ := parseProbe([]byte(sampleProbe))
	p.DV, p.HDR10Plus = nil, true
	cfg := baseCfg()
	if plan, _ := makePlan(cfg, p); !plan.UseHDR10Plus {
		t.Error("x265 should keep HDR10+")
	}
	cfg.Codec = "qsv"
	plan, _ := makePlan(cfg, p)
	if plan.UseHDR10Plus || len(plan.Warnings) == 0 {
		t.Errorf("hardware encoder must drop HDR10+ with a warning: %+v", plan)
	}
}

func TestParseTargetSize(t *testing.T) {
	good := map[string]int64{"8G": 8 << 30, "8gb": 8 << 30, "4500M": 4500 << 20, "700 MB": 700 << 20, "7.5GB": int64(7.5 * (1 << 30)), "2 GiB": 2 << 30}
	for in, want := range good {
		got, err := parseTargetSize(in)
		if err != nil || got != want {
			t.Errorf("%q: got %d, %v; want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"8", "abc", "0.1M", "-3G", "", "8 parsecs"} {
		if _, err := parseTargetSize(in); err == nil {
			t.Errorf("%q should be rejected", in)
		}
	}
}

func testSizePlan(audio int64, dv bool) *SizePlan {
	sp := &SizePlan{Target: 8 << 30, Duration: 6540, AudioBytes: audio, AudioExact: true}
	sp.Aim = int64(float64(sp.Target) * aimFraction)
	sp.Overhead = int64(float64(sp.Aim) * containerFrac)
	if dv {
		sp.DVExtra = int64(float64(sp.Aim) * dvRPUFrac)
	}
	vb := sp.Aim - sp.NonVideo() - sp.DVExtra
	sp.VideoKbps = int(float64(vb) * 8 / sp.Duration / 1000)
	return sp
}

func TestSizeBudgetAddsUp(t *testing.T) {
	sp := testSizePlan(1500<<20, false)
	total := float64(sp.VideoKbps)*125*sp.Duration + float64(sp.NonVideo())
	if d := float64(sp.Aim) - total; d < 0 || d > 125*sp.Duration { // within one kbps of the aim
		t.Errorf("video + audio + overhead should equal the aim; off by %.0f bytes", d)
	}
	if dv := testSizePlan(1500<<20, true); dv.VideoKbps >= sp.VideoKbps {
		t.Error("Dolby Vision must leave less for video (RPU data)")
	}
}

// simulated hardware encoder: produces `bias` times the bitrate it was asked for
func fakeEncoder(nonVideo int64, dur, bias float64) func(n, kbps int) (int64, string, error) {
	return func(n, kbps int) (int64, string, error) {
		return nonVideo + int64(float64(kbps)*125*dur*bias), fmt.Sprintf("a%d", n+1), nil
	}
}

func TestRetryLoopConverges(t *testing.T) {
	sp := testSizePlan(1700<<20, false)
	for _, bias := range []float64{1.0, 1.15, 0.80, 1.4} {
		best, all, err := runAttempts(sp, 2, fakeEncoder(sp.NonVideo(), sp.Duration, bias), func(string, ...any) {})
		if err != nil {
			t.Fatal(err)
		}
		if len(all) > 3 {
			t.Errorf("bias %.2f: %d attempts, max is 3", bias, len(all))
		}
		if !inWindow(best.Total, sp.Target) {
			t.Errorf("bias %.2f: best %s not within 97-100%% of target (attempts %+v)", bias, formatBytes(best.Total), all)
		}
		if bias == 1.0 && len(all) != 1 {
			t.Errorf("an on-target first attempt must not be retried (got %d)", len(all))
		}
	}
	if _, all, _ := runAttempts(sp, 0, fakeEncoder(sp.NonVideo(), sp.Duration, 1.3), func(string, ...any) {}); len(all) != 1 {
		t.Errorf("retries=0 gave %d attempts", len(all))
	}
}

func TestRetryLoopPrefersUnderTarget(t *testing.T) {
	sp := testSizePlan(1<<30, false)
	sizes := []int64{int64(float64(sp.Target) * 1.05), int64(float64(sp.Target) * 0.90), int64(float64(sp.Target) * 0.92)}
	enc := func(n, kbps int) (int64, string, error) { return sizes[n], fmt.Sprintf("a%d", n), nil }
	best, _, _ := runAttempts(sp, 2, enc, func(string, ...any) {})
	if best.Total > sp.Target || best.Total != sizes[2] {
		t.Errorf("should pick the closest under-target attempt, got %s", formatBytes(best.Total))
	}
	if _, _, err := runAttempts(sp, 2, func(int, int) (int64, string, error) { return 0, "", fmt.Errorf("boom") }, func(string, ...any) {}); err == nil {
		t.Error("encode error must be returned")
	}
}

func TestPeakArgsOnlyForTargetSize(t *testing.T) {
	cfg := baseCfg()
	cfg.Bitrate = "10000k"
	if peakArgs(cfg) != nil {
		t.Error("no peak limits without a target size")
	}
	cfg.TargetBytes = 8 << 30
	if j := strings.Join(peakArgs(cfg), " "); j != "-maxrate 15000k -bufsize 30000k" {
		t.Errorf("got %q", j)
	}
}

func TestX265PassArgs(t *testing.T) {
	p, _ := parseProbe([]byte(sampleProbe))
	p.DV = nil
	cfg := baseCfg()
	cfg.Bitrate, cfg.Pass = "9000k", 1
	plan, _ := makePlan(cfg, p)
	args, _ := buildVideoArgs(cfg, p, plan, "")
	j := strings.Join(args, " ")
	for _, s := range []string{"-b:v 9000k", "pass=1", "stats=x265_2pass.log", "hdr10=1"} {
		if !strings.Contains(j, s) {
			t.Errorf("missing %q in %s", s, j)
		}
	}
	cfg.Pass = 0
	args, _ = buildVideoArgs(cfg, p, plan, "")
	if strings.Contains(strings.Join(args, " "), "pass=") {
		t.Error("no pass flags in single-pass mode")
	}
}

func TestTargetConflicts(t *testing.T) {
	p, _ := parseProbe([]byte(sampleProbe))
	p.DV = nil
	cfg := baseCfg()
	cfg.TargetBytes, cfg.CopyVideo = 8<<30, true
	if _, err := makePlan(cfg, p); err == nil {
		t.Error("target size with copy-video must be refused")
	}
	cfg = baseCfg()
	cfg.TargetBytes, cfg.Bitrate = 8<<30, "10M"
	if _, err := makePlan(cfg, p); err == nil {
		t.Error("target size with an explicit bitrate must be refused")
	}
}

// A copy of MP4Box that cannot start (e.g. mp4box.exe copied without its DLLs)
// must be skipped in favour of a working GPAC install, and reported if it is the only one.
func TestFindToolSkipsBrokenCopies(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses Unix executables")
	}
	mk := func(dir string) string {
		_ = os.MkdirAll(dir, 0o755)
		p := filepath.Join(dir, map[bool]string{true: "mp4box", false: "MP4Box"}[strings.Contains(dir, "GPAC")])
		_ = os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755)
		return p
	}
	onPath := mk(filepath.Join(t.TempDir(), "bin"))
	pf := t.TempDir()
	gpac := mk(filepath.Join(pf, "GPAC"))
	t.Setenv("PATH", filepath.Dir(onPath))
	t.Setenv("ProgramW6432", pf)
	old := toolStartProblem
	defer func() { toolStartProblem = old }()
	broken := map[string]bool{onPath: true}
	toolStartProblem = func(p string) string {
		if broken[p] {
			return "a DLL it needs is missing, error 0xc0000135"
		}
		return ""
	}
	if p, err := findTool("MP4Box", ""); err != nil || p != gpac {
		t.Fatalf("got %q, %v; want the GPAC install %q", p, err, gpac)
	}
	broken[gpac] = true // now nothing works
	_, err := findTool("MP4Box", "")
	if err == nil || !strings.Contains(err.Error(), "0xc0000135") || !strings.Contains(err.Error(), onPath) || !strings.Contains(err.Error(), "Install GPAC") {
		t.Fatalf("unhelpful error: %v", err)
	}
	if _, err := need("MP4Box", ""); err == nil || strings.Contains(err.Error(), "not found.") {
		t.Fatalf("need() hid the reason: %v", err)
	}
	if p := loaderProblem(int(int32(-1073741515))); !strings.Contains(p, "0xc0000135") { // how Windows reports it
		t.Fatalf("loaderProblem: %q", p)
	}
}
