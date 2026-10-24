package main

import (
	"fmt"
	"path/filepath"
	"strings"
)

type Config struct {
	Input, Output                                string
	CRF                                          int
	Preset, Codec, Bitrate                       string
	Audio, AudioBitrate                          string
	DV                                           string // auto | drop
	CopyVideo                                    bool
	MasterDisplay, MaxCLL                        string
	Ffmpeg, Ffprobe, DoviTool, MP4Box, HDR10Plus string
	Tmp                                          string
	DryRun, Keep, Verbose, Pause                 bool
	TargetBytes                                  int64    // >0: aim for this output file size
	Retries                                      int      // hardware encoders: extra attempts to land on the target
	Pass                                         int      // x265 two-pass: 0 off, 1 or 2
	vsyncArgs                                    []string // keep every source frame exactly once (set at start-up)
}

type Plan struct {
	Kind         string // sdr | hdr10 | hlg
	UseDV        bool   // run the Dolby Vision RPU pipeline
	DVProfile    string // profile string for MP4Box, e.g. "8.1"
	RPUMode      int    // dovi_tool -m value (0 = leave RPU as is)
	UseHDR10Plus bool
	Warnings     []string
	Subs         []subInfo
	Size         *SizePlan
}

func isMP4(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp4", ".m4v", ".mov":
		return true
	}
	return false
}

func makePlan(cfg *Config, p *Probe) (*Plan, error) {
	plan := &Plan{Kind: p.Kind()}
	if cfg.TargetBytes > 0 && cfg.CopyVideo {
		return nil, fmt.Errorf("a target file size cannot be used with -copy-video (the video is not re-encoded)")
	}
	if cfg.TargetBytes > 0 && cfg.Bitrate != "" {
		return nil, fmt.Errorf("use either a target file size or a video bitrate, not both")
	}
	warn := func(f string, a ...any) { plan.Warnings = append(plan.Warnings, fmt.Sprintf(f, a...)) }

	if p.DV != nil {
		// A source tagged only by its DV record: infer the base layer type.
		if plan.Kind == "sdr" {
			switch p.DV.Compat {
			case 4:
				plan.Kind = "hlg"
			case 1, 0:
				plan.Kind = "hdr10"
			}
			if plan.Kind != "sdr" {
				warn("source has Dolby Vision but untagged colour info; assuming %s base layer", plan.Kind)
			}
		}
		if p.DV.Profile == 5 && !cfg.CopyVideo {
			return nil, fmt.Errorf("Dolby Vision profile 5 uses the IPT-PQ-C2 colour space and has no HDR10-compatible base layer. " +
				"Re-encoding it without a Dolby Vision-aware encoder gives wrong (green/purple) colours, so this tool refuses. " +
				"Use -copy-video to keep the video untouched (you can still change audio/container)")
		}
		if !cfg.CopyVideo && cfg.DV != "drop" {
			switch p.DV.Profile {
			case 7:
				plan.UseDV, plan.DVProfile, plan.RPUMode = true, "8.1", 2
				if p.DV.ELPresent {
					warn("profile 7 enhancement layer is discarded; RPU is converted to profile 8.1 (same approach as most 'P7 to P8' tools)")
				}
			case 8:
				if p.DV.Compat != 1 && p.DV.Compat != 2 && p.DV.Compat != 4 {
					return nil, fmt.Errorf("unsupported Dolby Vision profile 8 compatibility id %d", p.DV.Compat)
				}
				plan.UseDV, plan.DVProfile = true, fmt.Sprintf("8.%d", p.DV.Compat)
			default:
				return nil, fmt.Errorf("Dolby Vision profile %d is not supported (supported: 7, 8.x; use -copy-video or -dv drop)", p.DV.Profile)
			}
		}
		if cfg.DV == "drop" && !cfg.CopyVideo {
			warn("dropping Dolby Vision: output will be plain %s", plan.Kind)
		}
	}

	if plan.UseDV && isMP4(cfg.Output) == false && !strings.EqualFold(filepath.Ext(cfg.Output), ".mkv") {
		warn("Dolby Vision output works best as .mkv or .mp4")
	}
	if plan.UseDV && p.AvgFPS != "" && p.RFPS != "" && p.AvgFPS != p.RFPS {
		warn("source looks variable-frame-rate (avg %s vs r %s); Dolby Vision mode rebuilds timestamps at %s constant fps", p.AvgFPS, p.RFPS, p.AvgFPS)
	}
	if p.HDR10Plus && !cfg.CopyVideo && !plan.UseDV {
		if cfg.Codec == "x265" || cfg.Codec == "" {
			plan.UseHDR10Plus = true
		} else {
			warn("source has HDR10+ dynamic metadata, which hardware encoders cannot keep; output will be plain HDR10 (use x265 to keep HDR10+)")
		}
	}

	if p.Mastering == nil && cfg.MasterDisplay == "" && plan.Kind == "hdr10" && !cfg.CopyVideo {
		warn("no mastering-display metadata found in source; output will have HDR10 colour tags only (use -master-display to supply it)")
	}
	return plan, nil
}

// buildVideoArgs returns the ffmpeg video-encoding arguments for the plan.
// hdr10plusJSON, if non-empty, is a dynamic-metadata file for x265.
func buildVideoArgs(cfg *Config, p *Probe, plan *Plan, hdr10plusJSON string) ([]string, []string) {
	var warns []string
	if cfg.CopyVideo {
		return []string{"-c:v", "copy"}, nil
	}

	// Colour tags: carry the source's through, defaulting to BT.2020 for HDR.
	prim, trc, space := p.Primaries, p.Transfer, p.Space
	switch plan.Kind {
	case "hdr10":
		trc = "smpte2084"
		prim, space = dflt(prim, "bt2020"), dflt(space, "bt2020nc")
	case "hlg":
		trc = "arib-std-b67"
		prim, space = dflt(prim, "bt2020"), dflt(space, "bt2020nc")
	}
	var args []string
	switch cfg.Codec {
	case "nvenc":
		args = append(args, "-c:v", "hevc_nvenc", "-preset", nvPreset(cfg.Preset), "-tune", "hq", "-profile:v", "main10", "-pix_fmt", "p010le")
		if cfg.Bitrate != "" {
			args = append(args, "-rc", "vbr", "-b:v", cfg.Bitrate)
		} else {
			args = append(args, "-rc", "vbr", "-cq", fmt.Sprint(cfg.CRF), "-b:v", "0")
		}
		if plan.Kind != "sdr" {
			warns = append(warns, "NVENC takes HDR10 static metadata from the decoded frames; check the verification line at the end")
		}
	case "qsv":
		args = append(args, "-c:v", "hevc_qsv", "-preset", qsvPreset(cfg.Preset), "-profile:v", "main10", "-pix_fmt", "p010le")
		if cfg.Bitrate != "" {
			args = append(args, "-b:v", cfg.Bitrate)
		} else {
			args = append(args, "-global_quality", fmt.Sprint(cfg.CRF))
		}
		if plan.Kind != "sdr" {
			warns = append(warns, "QuickSync takes HDR10 static metadata from the decoded frames; check the verification line at the end")
		}
	default:
		args = append(args, "-c:v", "libx265", "-preset", cfg.Preset, "-pix_fmt", "yuv420p10le")
		if cfg.Bitrate != "" {
			args = append(args, "-b:v", cfg.Bitrate)
		} else {
			args = append(args, "-crf", fmt.Sprint(cfg.CRF))
		}
		xp := []string{"repeat-headers=1"}
		if cfg.Pass > 0 {
			// stats file is relative: the process runs inside the temp folder
			xp = append(xp, fmt.Sprintf("pass=%d", cfg.Pass), "stats=x265_2pass.log")
		}
		switch plan.Kind {
		case "hdr10":
			xp = append(xp, "hdr10=1", "hdr10-opt=1", "colorprim="+prim, "transfer=smpte2084", "colormatrix="+space)
			md := cfg.MasterDisplay
			if md == "" && p.Mastering != nil {
				if s, err := p.Mastering.X265MasterDisplay(); err == nil {
					md = s
				} else {
					warns = append(warns, "could not parse mastering display metadata: "+err.Error())
				}
			}
			if md != "" {
				xp = append(xp, "master-display="+md)
			}
			cll := cfg.MaxCLL
			if cll == "" && p.CLL != nil {
				cll = fmt.Sprintf("%d,%d", p.CLL.MaxCLL, p.CLL.MaxFALL)
			}
			if cll != "" {
				xp = append(xp, "max-cll="+cll)
			}
		case "hlg":
			xp = append(xp, "colorprim="+prim, "transfer=arib-std-b67", "colormatrix="+space, "atc-sei=18", "pic-struct=0")
		}
		if plan.UseHDR10Plus && hdr10plusJSON != "" {
			xp = append(xp, "dhdr10-info="+hdr10plusJSON, "dhdr10-opt=1")
		}
		args = append(args, "-x265-params", strings.Join(xp, ":"))
	}
	if prim != "" {
		args = append(args, "-color_primaries", prim)
	}
	if trc != "" {
		args = append(args, "-color_trc", trc)
	}
	if space != "" {
		args = append(args, "-colorspace", space)
	}
	args = append(args, "-color_range", dflt(p.Range, "tv"))
	return args, warns
}

// peakArgs lets a hardware encoder's bitrate swing around the average (needed to
// land near a target size) while still bounding the peaks.
func peakArgs(cfg *Config) []string {
	if cfg.TargetBytes <= 0 {
		return nil
	}
	bps, ok := parseBitrateBps(cfg.Bitrate)
	if !ok {
		return nil
	}
	return []string{"-maxrate", fmt.Sprintf("%dk", int(bps*1.5/1000)), "-bufsize", fmt.Sprintf("%dk", int(bps*3/1000))}
}

func dflt(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

// qsvPreset maps an x265 preset name onto hevc_qsv's target-usage presets.
func qsvPreset(x265 string) string {
	switch x265 {
	case "ultrafast", "superfast", "veryfast":
		return "veryfast"
	case "faster", "fast", "medium", "slow", "slower", "veryslow":
		return x265
	}
	return "medium"
}

func nvPreset(x265 string) string {
	switch x265 {
	case "ultrafast", "superfast", "veryfast", "faster":
		return "p2"
	case "fast":
		return "p3"
	case "medium":
		return "p5"
	case "slow":
		return "p6"
	default:
		return "p7"
	}
}

func audioArgs(cfg *Config) []string {
	a := []string{"-c:a", cfg.Audio}
	if cfg.Audio != "copy" && cfg.AudioBitrate != "" {
		a = append(a, "-b:a", cfg.AudioBitrate)
	}
	return a
}
