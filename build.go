package main

import (
	"fmt"
	"path/filepath"
	"strings"
)

type Config struct {
	LowPriority                                  bool    // run (and start the tools) at below-normal priority
	HWDecode                                     bool    // decode the source on the GPU (falls back to the CPU)
	Scale                                        string  // "" keeps the size; "1080p" etc. shrinks to fit that frame
	Denoise                                      string  // noise reduction: "" off, light, medium, strong, hq
	SampleSecs                                   int     // >0: encode only a test of this many seconds
	SampleStart                                  float64 // where the test starts (seconds; <0 = automatic)
	SampleOriginal                               string  // where the same stretch of the original is kept, for comparing ("" = automatic)
	WorkLocal                                    string  // a folder on this PC: network files are copied here, converted, and copied back
	Input, Output                                string
	CRF                                          int
	Preset, Codec, Bitrate                       string
	Audio, AudioBitrate                          string     // how kept audio tracks are written, unless chosen per track
	AudioSel, SubSel                             []trackSel // tracks chosen for this file; nil = decided by the rules below
	AudioDefault, SubDefault                     int        // track number to mark as default; 0 = as in the source; SubDefault -1 = none
	AudioLangs, SubLangs                         []string   // rules: languages to keep (empty = all)
	DropCommentary                               bool       // rule: leave out commentary audio tracks
	AddCompatAudio                               bool       // rule: add an E-AC-3 copy when no track plays everywhere
	AudioChannels                                string     // converted audio: "" keeps the channels, "5.1" or "stereo" mixes down
	Meta                                         *MetaEdit  // metadata changes (nil = none)
	MetaOnly                                     bool       // copy every stream unchanged, only the metadata changes
	ReplaceOriginal                              bool       // metadata only: the result takes the original's place
	SubMode                                      string     // rule: "" (all matching), "forced" (forced only) or "none"
	SubsToSRT                                    string     // rule: ASS/SSA and PGS subtitles to SRT: "", "replace" or "add"
	Tesseract                                    string     // where tesseract.exe is ("" = look for it)
	DV                                           string     // auto | drop
	CopyVideo                                    bool
	MasterDisplay, MaxCLL                        string
	Ffmpeg, Ffprobe, DoviTool, MP4Box, HDR10Plus string
	Tmp                                          string
	DryRun, Keep, Verbose, Pause, Sound          bool
	TargetBytes                                  int64          // >0: aim for this output file size
	Retries                                      int            // hardware encoders: extra attempts to land on the target
	Pass                                         int            // x265 two-pass: 0 off, 1 or 2
	vsyncArgs                                    []string       // keep every source frame exactly once (set at start-up)
	hwaccel                                      []string       // input options for GPU decoding (set at start-up)
	cuesFront                                    bool           // this ffmpeg can put an MKV's index at the front (set at start-up)
	hqDenoise                                    *denoiseChoice // "high quality" noise reduction as chosen at start-up
	gpu                                          *gpuPath       // the full-GPU path, when it works for this file
	cleanup                                      []string       // files to remove when the job ends (a test's sample)
	sample                                       string         // a test's cut of the original
	replaceFinal                                 string         // command line -replace-original: the name the result finally gets
}

type Plan struct {
	Kind         string // sdr | hdr10 | hlg
	UseDV        bool   // run the Dolby Vision RPU pipeline
	DVProfile    string // profile string for MP4Box, e.g. "8.1"
	RPUMode      int    // dovi_tool -m value (0 = leave RPU as is)
	UseHDR10Plus bool
	Warnings     []string
	Notes        []string // information, not warnings
	Audio        []outAudio
	Subs         []outSub
	Size         *SizePlan
	Src          *Tracks // the source's tracks, tags, chapters and covers
	Quality      *QualityGuide
	ScaleW       int // >0: resize the video to ScaleW x ScaleH
	ScaleH       int
	SrcW, SrcH   int
}

// scaleBoxes are the sizes offered for resizing: the video is shrunk to fit
// inside the frame, keeping its shape (a 3840x1600 film becomes 1920x800 at
// 1080p). Nothing is ever enlarged.
var scaleBoxes = map[string][2]int{"2160p": {3840, 2160}, "1440p": {2560, 1440}, "1080p": {1920, 1080}, "720p": {1280, 720}}

// normScale accepts "1080p", "1080" or "" (keep).
func normScale(s string) (string, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" || s == "original" || s == "keep" {
		return "", true
	}
	if !strings.HasSuffix(s, "p") {
		s += "p"
	}
	_, ok := scaleBoxes[s]
	return s, ok
}

// fitSize shrinks w x h to fit inside bw x bh, keeping the shape, with even
// dimensions (needed for 4:2:0 video). ok is false if it already fits.
func fitSize(w, h, bw, bh int) (int, int, bool) {
	f := min(float64(bw)/float64(w), float64(bh)/float64(h))
	if f >= 1 {
		return w, h, false
	}
	even := func(v float64) int { return max(2, 2*int(v/2+0.5)) }
	return even(float64(w) * f), even(float64(h) * f), true
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

	if cfg.Scale != "" {
		box, ok := scaleBoxes[cfg.Scale]
		switch {
		case !ok:
			return nil, fmt.Errorf("unknown resolution %q (use 2160p, 1440p, 1080p or 720p)", cfg.Scale)
		case cfg.CopyVideo:
			return nil, fmt.Errorf("the video cannot be resized when it is copied unchanged; choose Re-encode")
		case p.Width <= 0 || p.Height <= 0:
			return nil, fmt.Errorf("cannot read the size of the video, so it cannot be resized")
		}
		if w, h, ok := fitSize(p.Width, p.Height, box[0], box[1]); ok {
			plan.ScaleW, plan.ScaleH, plan.SrcW, plan.SrcH = w, h, p.Width, p.Height
		} else {
			warn("the video is %dx%d, which already fits %s, so it is not resized (Keep the Light never enlarges)", p.Width, p.Height, cfg.Scale)
		}
	}

	if g := qualityGuide(cfg, p, plan); g != nil {
		plan.Quality = g
		if g.Warn != "" {
			warn("%s", g.Warn)
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
	var args, filters []string
	if plan.ScaleW > 0 {
		// Same pixel format and colour space in and out: only the size changes,
		// so the HDR picture (PQ/HLG, BT.2020, 10-bit) is untouched.
		filters = append(filters, fmt.Sprintf("scale=%d:%d:flags=lanczos", plan.ScaleW, plan.ScaleH))
	}
	if f := cfg.denoiseFilter(); f != "" { // after resizing: fewer pixels to clean, and resizing already smooths a little
		filters = append(filters, f)
	}
	if cfg.gpu != nil && (cfg.Codec == "nvenc" || cfg.Codec == "qsv") {
		filters = []string{cfg.gpu.filter(plan)} // resize and 10-bit on the graphics card
	}
	if len(filters) > 0 {
		args = append(args, "-vf", strings.Join(filters, ","))
	}
	pixFmt := []string{"-pix_fmt", "p010le"}
	if cfg.gpu != nil {
		pixFmt = nil // the frames are already 10-bit, in GPU memory
	}
	switch cfg.Codec {
	case "nvenc":
		args = append(args, "-c:v", "hevc_nvenc", "-preset", nvPreset(cfg.Preset), "-tune", "hq", "-profile:v", "main10")
		args = append(args, pixFmt...)
		if cfg.Bitrate != "" {
			args = append(args, "-rc", "vbr", "-b:v", cfg.Bitrate)
		} else {
			args = append(args, "-rc", "vbr", "-cq", fmt.Sprint(cfg.CRF), "-b:v", "0")
		}
		if plan.Kind != "sdr" {
			warns = append(warns, "NVENC takes HDR10 static metadata from the decoded frames; check the verification line at the end")
		}
	case "qsv":
		args = append(args, "-c:v", "hevc_qsv", "-preset", qsvPreset(cfg.Preset), "-profile:v", "main10")
		args = append(args, pixFmt...)
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
