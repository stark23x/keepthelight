package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// ---------- is this encoder available on this PC? ----------
//
// NVENC needs an NVIDIA graphics card, QuickSync Intel graphics. Having the
// encoder in the FFmpeg build is not enough, so KTL encodes a few frames of a
// test picture to find out (once per run of the program).

var encoderChecks sync.Map // codec -> error ("" = works)

func encoderProblem(ctx context.Context, ffmpeg, codec string) string {
	enc := map[string]string{"nvenc": "hevc_nvenc", "qsv": "hevc_qsv"}[codec]
	if enc == "" {
		return ""
	}
	if v, ok := encoderChecks.Load(codec); ok {
		return v.(string)
	}
	tctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := toolCommand(tctx, ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-f", "lavfi", "-i", "color=c=gray:s=256x256:r=24:d=0.2",
		"-pix_fmt", "p010le", "-c:v", enc, "-profile:v", "main10", "-f", "null", "-").CombinedOutput()
	problem := ""
	if err != nil {
		reason := firstMeaningful(string(out))
		switch codec {
		case "nvenc":
			problem = "NVENC needs an NVIDIA graphics card (GeForce GTX 10-series or newer) with a current driver, and does not work on this PC"
		case "qsv":
			problem = "QuickSync needs Intel graphics (built into most Intel processors, or an Intel Arc card) with a current driver, and does not work on this PC"
		}
		if reason != "" {
			problem += " (" + reason + ")"
		}
		problem += ". Choose x265"
		if codec == "nvenc" {
			problem += " or QuickSync"
		} else {
			problem += " or NVENC"
		}
		problem += " instead."
	}
	encoderChecks.Store(codec, problem)
	return problem
}

// firstMeaningful picks the most telling line of ffmpeg's error output.
func firstMeaningful(out string) string {
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimSpace(l)
		if i := strings.Index(l, "] "); i >= 0 && strings.HasPrefix(l, "[") {
			l = strings.TrimSpace(l[i+2:])
		}
		low := strings.ToLower(l)
		if l == "" || strings.Contains(low, "error while opening encoder") || strings.Contains(low, "sending frames") || strings.Contains(low, "conversion failed") {
			continue
		}
		return strings.TrimSuffix(l, ".")
	}
	return ""
}

// ---------- the full-GPU path ----------
//
// With NVENC or QuickSync, the whole job can stay on the graphics card:
// decode, resize and encode, without frames going back to the CPU. KTL tests it
// on a few frames of each file first, and uses the normal way if it does not
// work. Noise reduction runs on the CPU (so it looks the same with every
// encoder), so a file with noise reduction goes the normal way.

type gpuPath struct {
	Input  []string // input options: decode into GPU memory
	Name   string   // for the log
	scaler func(w, h int) string
}

func gpuPathFor(codec string) *gpuPath {
	switch codec {
	case "nvenc":
		return &gpuPath{
			Input: []string{"-hwaccel", "cuda", "-hwaccel_output_format", "cuda"},
			Name:  "NVIDIA (CUDA)",
			scaler: func(w, h int) string {
				if w > 0 {
					return fmt.Sprintf("scale_cuda=w=%d:h=%d:interp_algo=lanczos:format=p010le", w, h)
				}
				return "scale_cuda=format=p010le" // 10-bit for the encoder, still on the GPU
			},
		}
	case "qsv":
		return &gpuPath{
			Input: []string{"-hwaccel", "qsv", "-hwaccel_output_format", "qsv"},
			Name:  "Intel (QuickSync)",
			scaler: func(w, h int) string {
				if w > 0 {
					return fmt.Sprintf("vpp_qsv=w=%d:h=%d:format=p010", w, h)
				}
				return "vpp_qsv=format=p010"
			},
		}
	}
	return nil
}

// gpuFilter is the -vf for the full-GPU path.
func (g *gpuPath) filter(plan *Plan) string { return g.scaler(plan.ScaleW, plan.ScaleH) }

// chooseGPUPath decides, for this file, whether the full-GPU path is used.
func chooseGPUPath(ctx context.Context, cfg *Config, p *Probe, plan *Plan) (*gpuPath, string) {
	g := gpuPathFor(cfg.Codec)
	if g == nil || cfg.CopyVideo {
		return nil, ""
	}
	if plan != nil && plan.Reshape {
		return nil, "Full-GPU path: not used, because the profile 5 colour conversion has its own way onto the graphics card"
	}
	if cfg.Denoise != "" {
		return nil, "Full-GPU path: not used, because noise reduction runs on the CPU (decoding on the GPU still applies if it is on)"
	}
	if plan.CropW > 0 {
		return nil, "Full-GPU path: not used, because cropping black bars runs on the CPU (decoding on the GPU still applies if it is on)"
	}
	if plan.Deint != "" {
		return nil, "Full-GPU path: not used, because deinterlacing runs on the CPU (decoding on the GPU still applies if it is on)"
	}
	if p != nil && p.Range == "pc" {
		// Full-range video: the encoder is asked for full range, and newer FFmpeg passes that to the GPU
		// scaler, which cannot convert the range ("Cannot convert from bt709 tv range to bt709 pc range").
		return nil, "Full-GPU path: not used, because this video is marked full range, which the graphics card's resizer cannot keep (decoding on the GPU still applies if it is on)"
	}
	enc := map[string]string{"nvenc": "hevc_nvenc", "qsv": "hevc_qsv"}[cfg.Codec]
	args := append([]string{"-hide_banner", "-nostdin", "-v", "error"}, g.Input...)
	args = append(args, "-i", cfg.Input, "-map", "0:v:0", "-frames:v", "5", "-vf", g.filter(plan),
		"-c:v", enc, "-profile:v", "main10")
	args = append(args, gpuTestColour(p, plan)...) // the real job's colour options: they change what the filters must do
	args = append(args, "-f", "null", "-")
	tctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	out, err := toolCommand(tctx, cfg.Ffmpeg, args...).CombinedOutput()
	if err != nil {
		reason := firstMeaningful(string(out))
		if reason == "" {
			reason = err.Error()
		}
		return nil, "Full-GPU path: not available for this file (" + reason + "), so frames go through the CPU as usual"
	}
	return g, "Full-GPU path: on, " + g.Name + " (decode, resize and encode all on the graphics card)"
}

// gpuTestColour is the colour part of the real job's video options, for the
// quick check (without them, a file can pass the check and fail the real job).
func gpuTestColour(p *Probe, plan *Plan) []string {
	if p == nil {
		return nil
	}
	prim, trc, space := p.Primaries, p.Transfer, p.Space
	switch plan.Kind {
	case "hdr10":
		trc, prim, space = "smpte2084", dflt(prim, "bt2020"), dflt(space, "bt2020nc")
	case "hlg":
		trc, prim, space = "arib-std-b67", dflt(prim, "bt2020"), dflt(space, "bt2020nc")
	}
	var a []string
	for _, kv := range [][2]string{{"-color_primaries", prim}, {"-color_trc", trc}, {"-colorspace", space}} {
		if kv[1] != "" {
			a = append(a, kv[0], kv[1])
		}
	}
	return append(a, "-color_range", dflt(p.Range, "tv"))
}
