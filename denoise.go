package main

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ---------- noise reduction ----------
//
// Only filters that work in 10-bit are used: one that converts to 8-bit would
// put banding into HDR. (FFmpeg's CPU nlmeans does exactly that, so it is not
// used.) Noise reduction keeps every frame, so Dolby Vision data still lines up.

var denoiseLevels = map[string]string{ // hqdn3d: luma spatial, chroma spatial, luma temporal, chroma temporal
	"light":  "hqdn3d=2:1.5:3:2.25",
	"medium": "hqdn3d=4:3:6:4.5",
	"strong": "hqdn3d=7:5.25:10.5:7.9",
}

func validDenoise(s string) bool { return s == "" || s == "hq" || denoiseLevels[s] != "" }

var denoiseNames = map[string]string{"light": "light", "medium": "medium", "strong": "strong", "hq": "high quality"}

// hqCandidates are tried in order for "high quality": nlmeans on the GPU
// (Vulkan, then OpenCL), else fftdnoiz on the CPU. GPU filters cannot convert
// formats on their own, so if one does not take 10-bit it fails the test
// instead of quietly lowering the bit depth.
type denoiseChoice struct {
	Global []string // options placed before the input (the GPU device)
	Filter string
	Where  string // for the log
}

func hqCandidates() []denoiseChoice {
	gpu := func(api, dev, filter string) denoiseChoice {
		return denoiseChoice{
			Global: []string{"-init_hw_device", api + "=" + dev, "-filter_hw_device", dev},
			Filter: "format=p010,hwupload," + filter + ",hwdownload,format=p010",
			Where:  "on the GPU (" + map[string]string{"vulkan": "Vulkan", "opencl": "OpenCL"}[api] + ")",
		}
	}
	return []denoiseChoice{
		gpu("vulkan", "ktlvk", "nlmeans_vulkan=s=2.5:p=7:r=9"),
		gpu("opencl", "ktlcl", "nlmeans_opencl=s=2.5:p=7:r=9"),
		{Filter: "fftdnoiz=sigma=4:amount=0.9", Where: "on the CPU (no GPU filter available)"},
	}
}

// chooseHQDenoise tests the candidates on a few frames of this file and
// returns the first that works.
func chooseHQDenoise(ctx context.Context, ffmpeg, input string, hwaccel []string) (denoiseChoice, string) {
	cands := hqCandidates()
	for _, c := range cands[:len(cands)-1] {
		args := append([]string{"-hide_banner", "-nostdin", "-v", "error"}, c.Global...)
		args = append(args, hwaccel...)
		args = append(args, "-i", input, "-map", "0:v:0", "-frames:v", "3", "-vf", c.Filter, "-f", "null", "-")
		tctx, cancel := context.WithTimeout(ctx, 60*time.Second)
		err := toolCommand(tctx, ffmpeg, args...).Run()
		cancel()
		if err == nil {
			return c, "Noise reduction: high quality, " + c.Where
		}
	}
	c := cands[len(cands)-1]
	return c, "Noise reduction: high quality, " + c.Where + "; this is slower"
}

// denoiseFilter is the filter for a level ("" = off). For "hq" it is the one
// chosen at start-up (cfg.hqDenoise).
func (cfg *Config) denoiseFilter() string {
	if cfg.Denoise == "hq" {
		if cfg.hqDenoise != nil {
			return cfg.hqDenoise.Filter
		}
		return hqCandidates()[len(hqCandidates())-1].Filter // dry run: the CPU one
	}
	return denoiseLevels[cfg.Denoise]
}

// ---------- 30-second test ----------

// parseTimeArg reads "1:02:30", "62:30", "3750" (seconds) or "" (automatic).
func parseTimeArg(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return -1, nil
	}
	parts := strings.Split(s, ":")
	if len(parts) > 3 {
		return 0, fmt.Errorf("bad time %q (use h:mm:ss, m:ss or seconds)", s)
	}
	var t float64
	for _, p := range parts {
		v, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil || v < 0 {
			return 0, fmt.Errorf("bad time %q (use h:mm:ss, m:ss or seconds)", s)
		}
		t = t*60 + v
	}
	return t, nil
}

// sampleStart is where a test starts: the chosen time, or 40% into the film
// (past the opening, usually a typical scene); always leaving room for the test.
func sampleStart(want, duration float64, secs int) float64 {
	start := want
	if start < 0 {
		start = duration * 0.4
	}
	if duration > 0 {
		start = math.Min(start, math.Max(0, duration-float64(secs)))
	}
	return math.Max(0, math.Floor(start))
}

// testOutputName is where a test goes: next to the output, "Film_KTL (30 s test).mkv".
func testOutputName(output string, secs int) string {
	ext := filepath.Ext(output)
	return strings.TrimSuffix(output, ext) + fmt.Sprintf(" (%d s test)", secs) + ext
}

// cutSample copies a stretch of the source, unchanged, into a file the normal
// steps then use as their input. Copying (rather than seeking in each step)
// gives every step, including the Dolby Vision RPU, exactly the same frames.
func cutSample(r *runner, cfg *Config, start float64, dir string) (string, error) {
	f, err := os.CreateTemp(dir, ".keepthelight-sample-*.mkv")
	if err != nil {
		return "", fmt.Errorf("cannot create the test sample: %w", err)
	}
	name := f.Name()
	f.Close()
	fmt.Printf("\nCutting a %d-second test from %s\n", cfg.SampleSecs, formatClock(start))
	err = r.run(cfg.Ffmpeg, "-hide_banner", "-v", "error", "-y", "-ss", strconv.FormatFloat(start, 'f', 3, 64), "-i", cfg.Input,
		"-t", strconv.Itoa(cfg.SampleSecs), "-map", "0:v", "-map", "0:a?", "-map", "0:s?", "-map", "0:t?", "-c", "copy", name)
	if err != nil {
		os.Remove(name)
		return "", err
	}
	return name, nil
}

// originalClipName is where the unchanged 30 seconds of the original go:
// "Film_KTL (30 s original).mkv" next to "Film_KTL (30 s test).mkv".
func originalClipName(testOutput string, secs int) string {
	ext := filepath.Ext(testOutput)
	base := strings.TrimSuffix(testOutput, ext)
	base = strings.TrimSuffix(base, ".partial")
	if t := fmt.Sprintf(" (%d s test)", secs); strings.HasSuffix(base, t) {
		return strings.TrimSuffix(base, t) + fmt.Sprintf(" (%d s original).mkv", secs)
	}
	return base + " (original clip).mkv"
}

// keepOriginalClip keeps the cut stretch of the original (it is copied, not
// re-encoded) next to the test, so the two can be compared side by side.
func keepOriginalClip(cfg *Config) {
	if cfg.sample == "" {
		return
	}
	dst := cfg.SampleOriginal
	if dst == "" {
		dst = originalClipName(cfg.Output, cfg.SampleSecs)
	}
	_ = os.Remove(dst) // the previous test's
	if err := os.Rename(cfg.sample, dst); err != nil {
		fmt.Println("WARNING: could not keep the original clip for comparing:", err)
		return
	}
	_ = os.Chmod(dst, 0o644) // made as a private temporary file
	for i, f := range cfg.cleanup {
		if f == cfg.sample {
			cfg.cleanup = append(cfg.cleanup[:i], cfg.cleanup[i+1:]...)
			break
		}
	}
	fmt.Println("Original clip for comparing (the same stretch, unchanged): " + dst)
}

func formatClock(s float64) string {
	t := int(s)
	return fmt.Sprintf("%d:%02d:%02d", t/3600, t/60%60, t%60)
}

// inputPrefix: options before the source input (GPU filter device, GPU decoding).
func (cfg *Config) inputPrefix() []string {
	if cfg.gpu != nil {
		return cfg.gpu.Input // decoded straight into GPU memory
	}
	var a []string
	if cfg.Denoise == "hq" && cfg.hqDenoise != nil {
		a = append(a, cfg.hqDenoise.Global...)
	}
	return append(a, cfg.hwaccel...)
}

// prepareSample turns the job into a short test: a stretch of the source is
// cut (copied unchanged) and used as the input. A target file size, which is
// meant for the whole film, becomes the bitrate it works out to.
func prepareSample(ctx context.Context, cfg *Config) error {
	if cfg.CopyVideo {
		return fmt.Errorf("a test is only useful when the video is re-encoded")
	}
	cfg.ReplaceOriginal = false
	p, err := probeFile(ctx, cfg.Ffprobe, cfg.Input)
	if err != nil {
		return err
	}
	start := sampleStart(cfg.SampleStart, p.Duration, cfg.SampleSecs)
	if cfg.TargetBytes > 0 {
		pc := *cfg
		pc.DryRun = true // no measuring pass for a test
		plan, err := makePlan(&pc, p)
		if err == nil {
			err = chooseTracks(ctx, cfg.Ffprobe, &pc, plan)
		}
		var srcBytes int64
		if st, e := os.Stat(cfg.Input); e == nil {
			srcBytes = st.Size()
		}
		if err == nil {
			var sp *SizePlan
			if sp, err = planSize(ctx, &pc, p, plan, srcBytes); err == nil {
				cfg.Bitrate, cfg.TargetBytes = fmt.Sprintf("%dk", sp.VideoKbps), 0
				fmt.Printf("Test: the target size works out to %d kbps for video; the test uses that bitrate\n", sp.VideoKbps)
			}
		}
		if err != nil {
			return err
		}
	}
	r := &runner{ctx: ctx, dry: cfg.DryRun, verbose: cfg.Verbose}
	if cfg.DryRun {
		r.show(fmt.Sprintf("(test: %d s from %s)", cfg.SampleSecs, formatClock(start)))
		return nil
	}
	sample, err := cutSample(r, cfg, start, filepath.Dir(cfg.Output))
	if err != nil {
		return err
	}
	cfg.cleanup = append(cfg.cleanup, sample)
	cfg.sample = sample
	cfg.Input = sample
	return nil
}
