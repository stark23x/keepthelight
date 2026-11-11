package main

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ---------- Dolby Vision profile 5 ----------
//
// Profile 5 has no ordinary picture underneath: its colours are stored in
// Dolby's own format and only look right once the Dolby Vision data has been
// applied ("reshaping"); anything else shows them green and purple. Keep the
// Light applies it first with FFmpeg's libplacebo filter (on the graphics card,
// through Vulkan), which turns each frame into a normal HDR10 picture (BT.2020,
// PQ, limited range), and then encodes that like any HDR10 file. The Dolby
// Vision data is converted to profile 8.1 (dovi_tool mode 3: the reshaping is
// reset, since the picture is now HDR10) and put back, so Dolby Vision players
// still get Dolby Vision; "Drop" makes plain HDR10.

// reshapeFilter turns a profile 5 frame into HDR10 using its Dolby Vision data.
const reshapeFilter = "libplacebo=colorspace=bt2020nc:color_primaries=bt2020:color_trc=smpte2084:range=tv:format=yuv420p10le"

// vulkanInit is the FFmpeg input option libplacebo needs (a Vulkan device).
var vulkanInit = []string{"-init_hw_device", "vulkan"}

// checkReshape converts one frame, to make sure this FFmpeg has libplacebo
// and the PC can run it, before hours are spent on the job.
func checkReshape(ctx context.Context, ffmpeg, input string) error {
	if !hasFilter(ctx, ffmpeg, "libplacebo") {
		return fmt.Errorf("this file is Dolby Vision profile 5, whose colours can only be converted with FFmpeg's libplacebo filter, " +
			"and this FFmpeg does not have it. Use a \"full\" FFmpeg build (for example ffmpeg-release-full.7z from gyan.dev) in the tools folder, " +
			"or choose \"Copy video untouched\"")
	}
	tctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	args := append([]string{"-hide_banner", "-nostdin", "-v", "error"}, vulkanInit...)
	args = append(args, "-i", input, "-map", "0:v:0", "-frames:v", "1", "-vf", reshapeFilter, "-f", "null", "-")
	if out, err := toolCommand(tctx, ffmpeg, args...).CombinedOutput(); err != nil {
		reason := firstMeaningful(string(out))
		if reason == "" {
			reason = err.Error()
		}
		return fmt.Errorf("this file is Dolby Vision profile 5, whose colours are converted on the graphics card (libplacebo, through Vulkan), "+
			"and that did not work on this PC (%s). Updating the graphics driver usually fixes it; otherwise choose \"Copy video untouched\"", reason)
	}
	return nil
}

// p5Static fills in the HDR10 information (mastering display, MaxCLL/MaxFALL)
// a profile 5 file only has inside its Dolby Vision data, unless it was given
// (Advanced, or -master-display / -max-cll). It reads the first minute with
// dovi_tool; without dovi_tool (or data) it uses a typical display (P3, 1000 nits).
func p5Static(r *runner, cfg *Config, dovi, tmp string) {
	if cfg.MasterDisplay != "" && cfg.MaxCLL != "" {
		return
	}
	summary := ""
	if dovi != "" && tmp != "" {
		rpu := filepath.Join(tmp, "rpu_start.bin")
		quiet := &runner{ctx: r.ctx}
		if err := quiet.pipe(cfg.Ffmpeg, []string{"-v", "error", "-i", cfg.Input, "-map", "0:v:0", "-t", "60", "-c:v", "copy",
			"-bsf:v", "hevc_mp4toannexb", "-f", "hevc", "-"}, dovi, []string{"extract-rpu", "-", "-o", rpu}); err == nil {
			if out, err := toolCommand(r.ctx, dovi, "info", "-i", rpu, "-s").Output(); err == nil {
				summary = string(out)
			}
		}
		removeTemp(rpu)
	}
	md, cll := doviStatic(summary)
	if cfg.MasterDisplay == "" {
		cfg.MasterDisplay = md
	}
	if cfg.MaxCLL == "" && cll != "" {
		cfg.MaxCLL = cll
	}
	if summary == "" {
		fmt.Println("HDR10 information: not found in the Dolby Vision data; using a typical display (P3, 1000 nits)")
	} else {
		fmt.Println("HDR10 information from the Dolby Vision data: mastering display", cfg.MasterDisplay, "· MaxCLL,MaxFALL", dflt(cfg.MaxCLL, "not given"))
	}
}

var (
	reL6Display = regexp.MustCompile(`L6 metadata: Mastering display: ([\d.]+)/([\d.]+) nits`)
	reL6Light   = regexp.MustCompile(`MaxCLL: (\d+) nits, MaxFALL: (\d+) nits`)
	reRPUDisp   = regexp.MustCompile(`RPU mastering display: ([\d.]+)/([\d.]+) nits`)
)

// x265 mastering-display colours (G, B, R, white point, in 0.00002 units).
const (
	p3D65Prims  = "G(13250,34500)B(7500,3000)R(34000,16000)WP(15635,16450)"
	bt2020Prims = "G(8500,39850)B(6550,2300)R(35400,14600)WP(15635,16450)"
)

// doviStatic reads dovi_tool's summary ("info -s") as x265's master-display
// and max-cll values: L6 when the file has it, else the RPU's own display.
func doviStatic(summary string) (md, cll string) {
	prims := p3D65Prims
	if strings.Contains(summary, "L9 MDP: BT.2020") {
		prims = bt2020Prims
	}
	lo, hi := 0.0001, 1000.0
	m := reL6Display.FindStringSubmatch(summary)
	if m == nil {
		m = reRPUDisp.FindStringSubmatch(summary)
	}
	if m != nil {
		a, e1 := strconv.ParseFloat(m[1], 64)
		b, e2 := strconv.ParseFloat(m[2], 64)
		if e1 == nil && e2 == nil && b > 0 && b <= 10000 && a < b {
			lo, hi = a, b
		}
	}
	md = fmt.Sprintf("%sL(%d,%d)", prims, int(math.Round(hi*10000)), max(1, int(math.Round(lo*10000))))
	if i := strings.Index(summary, "L6 metadata:"); i >= 0 {
		if l := reL6Light.FindStringSubmatch(summary[i:]); l != nil && l[1] != "0" {
			cll = l[1] + "," + l[2]
		}
	}
	return md, cll
}
