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
func checkReshape(ctx context.Context, ffmpeg, input string, dev []string) error {
	if !hasFilter(ctx, ffmpeg, "libplacebo") {
		return fmt.Errorf("this file is Dolby Vision profile 5, whose colours can only be converted with FFmpeg's libplacebo filter, " +
			"and this FFmpeg does not have it. Use a \"full\" FFmpeg build (for example ffmpeg-release-full.7z from gyan.dev) in the tools folder, " +
			"or choose \"Copy video untouched\"")
	}
	tctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	args := append([]string{"-hide_banner", "-nostdin", "-v", "error"}, dev...)
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

// ---------- faster profile 5 conversion ----------
//
// The conversion runs on a Vulkan graphics device. On a PC with two (a laptop
// with Intel graphics and an NVIDIA card) Keep the Light picks the one that
// matches the encoder, instead of whichever Vulkan lists first. Decoding can
// also move to the graphics card, but only after a check on this very file
// shows that the result is the same as decoding on the processor (the
// conversion needs the Dolby Vision data of every frame, and a decoding path
// that lost it would give wrong colours). If anything about the faster way
// fails, Keep the Light uses the processor as before.

// vkDevice is one graphics device Vulkan lists.
type vkDevice struct {
	Index int
	Name  string
	Kind  string // "discrete", "integrated", "software", …
}

var reVkDevice = regexp.MustCompile(`^\s*(\d+): (.+) \((discrete|integrated|virtual|software|cpu|unknown)\)( \(0x[0-9a-f]+\))?\s*$`)

// vulkanDevices lists the Vulkan devices FFmpeg sees.
func vulkanDevices(ctx context.Context, ffmpeg string) []vkDevice {
	tctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	out, _ := toolCommand(tctx, ffmpeg, "-hide_banner", "-nostdin", "-v", "verbose", "-init_hw_device", "vulkan=ktlvkls",
		"-f", "lavfi", "-i", "nullsrc=s=16x16", "-frames:v", "1", "-f", "null", "-").CombinedOutput()
	return parseVulkanDevices(string(out))
}

func parseVulkanDevices(log string) []vkDevice {
	var devs []vkDevice
	listing := false
	for _, l := range strings.Split(log, "\n") {
		if i := strings.Index(l, "] "); i >= 0 && strings.HasPrefix(l, "[") {
			l = l[i+2:]
		}
		if strings.Contains(l, "GPU listing:") {
			listing = true
			continue
		}
		if !listing {
			continue
		}
		m := reVkDevice.FindStringSubmatch(strings.TrimRight(l, "\r"))
		if m == nil {
			break
		}
		n, _ := strconv.Atoi(m[1])
		devs = append(devs, vkDevice{Index: n, Name: m[2], Kind: m[3]})
	}
	return devs
}

// pickVulkan chooses the device (its place in devs) for the encoder: the NVIDIA card for NVENC,
// Intel graphics for QuickSync, otherwise the first graphics card (not one
// built into the processor, nor a software one). -1: leave it to FFmpeg.
func pickVulkan(devs []vkDevice, codec string) int {
	if len(devs) < 2 {
		return -1
	}
	want := map[string]string{"nvenc": "nvidia", "qsv": "intel"}[codec]
	for i, d := range devs {
		if want != "" && strings.Contains(strings.ToLower(d.Name), want) {
			return i
		}
	}
	for _, kind := range []string{"discrete", "integrated"} {
		for i, d := range devs {
			if d.Kind == kind {
				return i
			}
		}
	}
	return -1
}

// vulkanArgs are the FFmpeg options that set up the conversion's device.
func (cfg *Config) vulkanArgs() []string {
	if cfg.vulkanDev == "" {
		return vulkanInit
	}
	return []string{"-init_hw_device", "vulkan=ktlvk:" + cfg.vulkanDev, "-filter_hw_device", "ktlvk"}
}

// reshapeChain is the conversion as it runs in this job (with frames brought
// back from the graphics card when they were decoded there).
func (cfg *Config) reshapeChain() string {
	if cfg.p5OnGPU {
		return reshapeFilter + ",hwdownload,format=yuv420p10le"
	}
	return reshapeFilter
}

// p5Way is one way to decode a profile 5 file for the conversion.
type p5Way struct {
	hw    []string // input options
	onGPU bool     // frames stay on the graphics card until after the conversion
	desc  string
}

func p5Ways(cfg *Config) []p5Way {
	var ways []p5Way
	if cfg.vulkanDev != "" { // decoded by the same device that converts: no copies until the end
		ways = append(ways, p5Way{[]string{"-hwaccel", "vulkan", "-hwaccel_device", "ktlvk", "-hwaccel_output_format", "vulkan"}, true,
			"decoded and converted on the graphics card"})
	}
	return append(ways, p5Way{hwaccelArgs(), false, "decoded on the graphics card"})
}

// chooseP5Way checks the faster ways on a stretch of this file against the
// processor's decoding (the way that is known to be right), frame by frame,
// and returns the first that gives the same picture. When none does, the
// processor decodes (hw nil).
func chooseP5Way(ctx context.Context, ffmpeg string, cfg *Config, duration float64) (p5Way, string) {
	at := 0.0
	if duration > 120 {
		at = math.Min(duration*0.1, 600)
	}
	var reasons []string
	for _, w := range p5Ways(cfg) {
		if !w.onGPU { // ffmpeg quietly decodes on the processor when the graphics card cannot: find out first
			if hw, _ := checkHWDecode(ctx, ffmpeg, cfg.Input); hw == nil {
				reasons = append(reasons, w.desc+": not available for this file")
				continue
			}
		}
		ok, why := sameAsCPU(ctx, ffmpeg, cfg, w, at)
		if ok {
			return w, "Dolby Vision profile 5: " + w.desc + " (checked on this file: the same picture as decoding on the processor)"
		}
		reasons = append(reasons, w.desc+": "+why)
	}
	return p5Way{}, "Dolby Vision profile 5: decoded on the processor (the faster ways did not pass the check on this file: " + strings.Join(reasons, "; ") + ")"
}

// p5CheckFrames is how many frames the check compares.
const p5CheckFrames = 48

// sameAsCPU converts the same frames both ways in one run and compares them:
// every frame must be there and match closely (a frame that lost its Dolby
// Vision data has wrong colours and fails by far).
func sameAsCPU(ctx context.Context, ffmpeg string, cfg *Config, w p5Way, at float64) (bool, string) {
	tctx, cancel := context.WithTimeout(ctx, 180*time.Second)
	defer cancel()
	ss := strconv.FormatFloat(at, 'f', 3, 64)
	fast := reshapeFilter
	if w.onGPU {
		fast += ",hwdownload,format=yuv420p10le"
	}
	args := append([]string{"-hide_banner", "-nostdin", "-v", "error"}, cfg.vulkanArgs()...)
	args = append(args, w.hw...)
	args = append(args, "-ss", ss, "-i", cfg.Input, "-ss", ss, "-i", cfg.Input,
		"-filter_complex", "[0:v:0]"+fast+"[a];[1:v:0]"+reshapeFilter+"[b];[a][b]psnr=stats_file=-[o]",
		"-map", "[o]", "-frames:v", strconv.Itoa(p5CheckFrames), "-f", "null", "-")
	out, err := toolCommand(tctx, ffmpeg, args...).CombinedOutput()
	if err != nil {
		return false, firstMeaningful(string(out))
	}
	return p5Compare(string(out))
}

// p5Compare reads the psnr filter's per-frame lines.
func p5Compare(out string) (bool, string) {
	frames, worst := 0, math.Inf(1)
	for _, l := range strings.Split(out, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(l), "n:") {
			continue
		}
		frames++
		for _, f := range strings.Fields(l) {
			k, v, _ := strings.Cut(f, ":")
			if k != "psnr_y" && k != "psnr_u" && k != "psnr_v" {
				continue
			}
			if v == "inf" {
				continue
			}
			x, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return false, "unreadable comparison"
			}
			worst = math.Min(worst, x)
		}
	}
	switch {
	case frames < p5CheckFrames:
		return false, fmt.Sprintf("only %d of %d frames came through", frames, p5CheckFrames)
	case worst < 45:
		return false, fmt.Sprintf("the picture differs (%.0f dB)", worst)
	}
	return true, ""
}

// chooseHQDenoiseP5: "high quality" noise reduction for profile 5 shares the
// conversion's graphics device (two devices would leave the conversion
// without one); without it, the processor does it.
func chooseHQDenoiseP5(ctx context.Context, ffmpeg string, cfg *Config) (denoiseChoice, string) {
	cands := hqCandidates()
	gpu, cpu := cands[0], cands[len(cands)-1]
	if cfg.vulkanDev != "" {
		args := append([]string{"-hide_banner", "-nostdin", "-v", "error"}, cfg.vulkanArgs()...)
		args = append(args, cfg.hwaccel...)
		args = append(args, "-i", cfg.Input, "-map", "0:v:0", "-frames:v", "3", "-vf", cfg.reshapeChain()+","+gpu.Filter, "-f", "null", "-")
		tctx, cancel := context.WithTimeout(ctx, 60*time.Second)
		err := toolCommand(tctx, ffmpeg, args...).Run()
		cancel()
		if err == nil {
			gpu.Global = nil // the conversion's device
			return gpu, "Noise reduction: high quality, " + gpu.Where
		}
	}
	return cpu, "Noise reduction: high quality, " + cpu.Where + "; this is slower"
}

// setupP5 prepares a profile 5 job: the graphics device, a one-frame check
// of the conversion, and (with GPU decoding on) the fastest way that passes
// the check on this file.
func setupP5(ctx context.Context, ffmpeg string, cfg *Config, duration float64) error {
	cfg.vulkanDev = ""
	devs := vulkanDevices(ctx, ffmpeg)
	name := ""
	if i := pickVulkan(devs, cfg.Codec); i >= 0 {
		cfg.vulkanDev, name = strconv.Itoa(devs[i].Index), devs[i].Name
		if err := checkReshape(ctx, ffmpeg, cfg.Input, cfg.vulkanArgs()); err != nil {
			fmt.Printf("Dolby Vision profile 5 colour conversion: did not work on %s (%v); leaving the choice of device to FFmpeg\n", name, err)
			cfg.vulkanDev = ""
		}
	}
	if cfg.vulkanDev == "" {
		if err := checkReshape(ctx, ffmpeg, cfg.Input, cfg.vulkanArgs()); err != nil {
			return err
		}
		fmt.Println("Dolby Vision profile 5 colour conversion: works on this PC (libplacebo)")
	} else {
		fmt.Printf("Dolby Vision profile 5 colour conversion: works on this PC (libplacebo, on %s)\n", name)
	}
	if !cfg.HWDecode {
		return nil
	}
	w, msg := chooseP5Way(ctx, ffmpeg, cfg, duration)
	cfg.hwaccel, cfg.p5OnGPU = w.hw, w.onGPU
	fmt.Println(msg)
	return nil
}
