package main

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// ---------- quality guide (before encoding) ----------
//
// A rule of thumb, not a measurement: how many bits each pixel of each frame
// gets, adjusted to "x265 at its medium preset" terms (GPU encoders and fast
// presets need more bits for the same look, noise reduction fewer), compared
// with thresholds for HEVC (HDR needs a little more: banding shows first in
// dark gradients). The real check is a 30-second test, which is measured.

type QualityGuide struct {
	BPP   float64 `json:"bpp"`   // bits per pixel per frame, in x265-medium terms
	Level string  `json:"level"` // excellent, verygood, good, fair, low, verylow (or crf levels)
	Text  string  `json:"text"`
	Warn  string  `json:"warn,omitempty"`
}

// encoderFactor: how far a bit goes with this encoder and preset, relative to x265 medium.
func encoderFactor(cfg *Config) float64 {
	switch cfg.Codec {
	case "nvenc":
		return 0.78
	case "qsv":
		return 0.75
	}
	switch cfg.Preset {
	case "veryslow", "slower":
		return 1.1
	case "slow":
		return 1.05
	case "fast", "faster":
		return 0.92
	case "veryfast", "superfast", "ultrafast":
		return 0.8
	}
	return 1.0
}

// denoiseFactor: with the noise taken out, each bit goes further.
func denoiseFactor(d string) float64 {
	if f, ok := map[string]float64{"light": 1.08, "medium": 1.15, "strong": 1.25, "hq": 1.15}[d]; ok {
		return f
	}
	return 1
}

// codecFactor: how many bits of HEVC a bit of the source's codec is worth.
func codecFactor(codec string) float64 {
	switch codec {
	case "hevc", "vp9":
		return 1.0
	case "av1":
		return 1.3
	case "h264":
		return 0.6
	case "vc1":
		return 0.55
	case "mpeg2video":
		return 0.4
	case "mpeg4":
		return 0.45
	}
	return 0.6
}

var bppLevels = []struct {
	min                   float64
	level, label, details string
}{
	{0.08, "excellent", "excellent", ""},
	{0.05, "verygood", "very good", ""},
	{0.035, "good", "good", "grainy or dark scenes may soften a little"},
	{0.025, "fair", "fair", "expect some softening, and some banding in dark scenes"},
	{0.015, "low", "low", "visible blocking and banding are likely, especially in dark scenes"},
	{0, "verylow", "very low", "expect heavy blocking and banding"},
}

// bitrateGuide judges a video bitrate (bits/s) for the output picture.
func bitrateGuide(bps float64, cfg *Config, p *Probe, plan *Plan) *QualityGuide {
	w, h := p.Width, p.Height
	if plan.ScaleW > 0 {
		w, h = plan.ScaleW, plan.ScaleH
	}
	fps, _ := parseRational(p.AvgFPS)
	if fps <= 0 {
		fps, _ = parseRational(p.RFPS)
	}
	if bps <= 0 || w <= 0 || h <= 0 || fps <= 0 {
		return nil
	}
	raw := bps / (float64(w) * float64(h) * fps)
	eff := raw * encoderFactor(cfg) * denoiseFactor(cfg.Denoise)
	hdr := plan.Kind != "sdr"
	judge := eff
	if hdr {
		judge /= 1.15 // HDR needs about 15% more for the same look
	}
	g := &QualityGuide{BPP: eff}
	label, details := "", ""
	for _, l := range bppLevels {
		if judge >= l.min {
			g.Level, label, details = l.level, l.label, l.details
			break
		}
	}
	g.Text = fmt.Sprintf("about %.3f bits per pixel at %dx%d: %s", raw, w, h, label)
	if details != "" {
		g.Text += "; " + details
	}
	if cfg.Codec != "x265" {
		g.Text += " (GPU encoders need about 25% more bitrate than x265 for the same look)"
	}
	if g.Level == "low" || g.Level == "verylow" {
		g.Warn = fmt.Sprintf("%.1f Mbps is %s for %dx%d%s video: %s. Raise the bitrate, lower the resolution, or try a 30-second test first.",
			bps/1e6, label, w, h, map[bool]string{true: " HDR"}[hdr], details)
	}
	// compared with the source, in HEVC terms (and for the output size)
	if src := p.VideoBps; src > 0 && p.Width > 0 {
		equiv := src * codecFactor(p.Codec) * float64(w*h) / float64(p.Width*p.Height)
		switch {
		case bps*encoderFactor(cfg) > equiv*1.15:
			g.Warn = strings.TrimSpace(g.Warn + fmt.Sprintf(" %.1f Mbps is more than this source needs (it carries about %.1f Mbps of HEVC-equivalent detail): the result will not look better than the original, only be bigger. Constant quality usually gives better value.",
				bps/1e6, equiv/1e6))
		case bps < equiv*0.25 && g.Level != "excellent" && g.Level != "verygood":
			g.Text += fmt.Sprintf(" (about %d%% of what the source uses)", int(math.Round(100*bps/equiv)))
		}
	}
	return g
}

// crfGuide describes a quality number (x265 CRF, or the GPU encoders' scale).
func crfGuide(cfg *Config, hdr bool) *QualityGuide {
	q := cfg.CRF
	g := &QualityGuide{}
	gpu := cfg.Codec != "x265"
	if gpu {
		q -= 2 // a GPU encoder's number looks roughly like x265's two steps better... at a bigger size
	}
	switch {
	case q <= 15:
		g.Level, g.Text = "excellent", "near-lossless; very large files"
	case q <= 19:
		g.Level, g.Text = "excellent", "visually transparent for most films"
	case q <= 22:
		g.Level, g.Text = "verygood", "very good; differences are hard to see"
	case q <= 25:
		g.Level, g.Text = "good", "good; fine grain may soften"
	case q <= 28:
		g.Level, g.Text = "fair", "fair; softening, and some banding in dark scenes"
	default:
		g.Level, g.Text = "low", "low; visible loss of detail and blocking"
	}
	if hdr && q >= 24 {
		g.Text += " (HDR shows banding in dark gradients first)"
	}
	if gpu {
		g.Text += " (GPU quality scale: the same number gives a bigger file than x265)"
	}
	if g.Level == "low" {
		g.Warn = fmt.Sprintf("quality %d is low: expect visible loss of detail and blocking. Lower the number for better quality.", cfg.CRF)
	}
	return g
}

// qualityGuide picks the guide for the chosen quality mode (size mode is done
// by the size plan, which knows the bitrate).
func qualityGuide(cfg *Config, p *Probe, plan *Plan) *QualityGuide {
	if cfg.CopyVideo || cfg.TargetBytes > 0 {
		return nil
	}
	if cfg.Bitrate != "" {
		bps, ok := parseBitrateBps(cfg.Bitrate)
		if !ok {
			return nil
		}
		return bitrateGuide(bps, cfg, p, plan)
	}
	return crfGuide(cfg, plan.Kind != "sdr")
}

// ---------- quality of a test (measured) ----------

var (
	reVMAF = regexp.MustCompile(`VMAF score[:=]\s*([0-9.]+)`)
	reSSIM = regexp.MustCompile(`SSIM .*All:([0-9.]+)`)
)

// hasFilter reports whether this ffmpeg has a filter (e.g. libvmaf).
func hasFilter(ctx context.Context, ffmpeg, name string) bool {
	out, err := toolCommand(ctx, ffmpeg, "-hide_banner", "-filters").Output()
	return err == nil && regexp.MustCompile(`(?m)^\s*\S+\s+`+regexp.QuoteMeta(name)+`\s`).Match(out)
}

// scoreTest compares the converted test with the same stretch of the original.
// VMAF (a perceptual score, 0-100) when ffmpeg has it, otherwise SSIM (0-1).
// The original is brought to the test's size and format first, so the score is
// about the encoding, not the resizing.
func scoreTest(ctx context.Context, ffmpeg, test, original string, w, h int, denoise string) (string, error) {
	vmaf := hasFilter(ctx, ffmpeg, "libvmaf")
	prep := fmt.Sprintf("[0:v]setpts=PTS-STARTPTS,format=yuv420p10le[d];[1:v]setpts=PTS-STARTPTS,scale=%d:%d:flags=lanczos,format=yuv420p10le[r];", w, h)
	f := prep + "[d][r]ssim"
	if vmaf {
		f = prep + "[d][r]libvmaf=n_subsample=3:n_threads=4"
	}
	out, err := toolCommand(ctx, ffmpeg, "-hide_banner", "-nostdin", "-i", test, "-i", original, "-map", "0:v:0", "-map", "1:v:0",
		"-filter_complex", f, "-an", "-f", "null", "-").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("measuring failed: %s", lastLine(string(out)))
	}
	note := ""
	if denoise != "" {
		note = " Noise reduction lowers the score: removed noise and grain count as differences from the original."
	}
	if vmaf {
		m := reVMAF.FindAllStringSubmatch(string(out), -1)
		if m == nil {
			return "", fmt.Errorf("no VMAF score in ffmpeg's output")
		}
		v, _ := strconv.ParseFloat(m[len(m)-1][1], 64)
		return fmt.Sprintf("Test quality: VMAF %.1f of 100 (%s).%s", v, vmafLabel(v), note), nil
	}
	m := reSSIM.FindAllStringSubmatch(string(out), -1)
	if m == nil {
		return "", fmt.Errorf("no SSIM score in ffmpeg's output")
	}
	v, _ := strconv.ParseFloat(m[len(m)-1][1], 64)
	return fmt.Sprintf("Test quality: SSIM %.4f of 1 (%s; this FFmpeg has no VMAF, which judges more like a viewer).%s", v, ssimLabel(v), note), nil
}

func vmafLabel(v float64) string {
	switch {
	case v >= 95:
		return "excellent: practically indistinguishable from the original"
	case v >= 90:
		return "very good: differences are hard to see"
	case v >= 80:
		return "good: small differences on close inspection"
	case v >= 70:
		return "fair: visible loss of detail"
	}
	return "poor: clearly visible loss"
}

func ssimLabel(v float64) string {
	switch {
	case v >= 0.98:
		return "excellent"
	case v >= 0.96:
		return "very good"
	case v >= 0.93:
		return "good"
	case v >= 0.90:
		return "fair"
	}
	return "poor"
}
