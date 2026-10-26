package main

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Target-file-size planning: turn "I want about 8 GB" into a video bitrate by
// subtracting everything that is not video (audio, subtitles, container).

const (
	minVideoKbps  = 150  // below this the picture is unusable
	aimFraction   = 0.99 // aim 1% under the target so the result lands at or below it
	windowLow     = 0.97 // a result between 97% and 100% of the target counts as on target
	containerFrac = 0.006
	dvRPUFrac     = 0.004 // Dolby Vision RPU data added after the encode
)

var reTargetSize = regexp.MustCompile(`^(\d+(?:\.\d+)?)\s*([kKmMgGtT])(?:[iI]?[bB])?$`)

// parseTargetSize reads sizes like 8G, 8GB, 7.5GiB, 4500M, 700 MB. Units are binary
// (1 GB = 1024^3 bytes) because that is how Windows Explorer reports file sizes.
func parseTargetSize(s string) (int64, error) {
	m := reTargetSize.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0, fmt.Errorf("target size %q is not valid; use a number with a unit, like 8G or 4500M", s)
	}
	v, _ := strconv.ParseFloat(m[1], 64)
	mult := map[string]float64{"k": 1 << 10, "m": 1 << 20, "g": 1 << 30, "t": 1 << 40}[strings.ToLower(m[2])]
	n := int64(v * mult)
	if n < 5<<20 {
		return 0, fmt.Errorf("target size %q is too small (minimum 5 MB)", s)
	}
	return n, nil
}

func formatBytes(b int64) string {
	f := float64(b)
	switch {
	case f >= 1<<30:
		return fmt.Sprintf("%.2f GB", f/(1<<30))
	case f >= 1<<20:
		return fmt.Sprintf("%.1f MB", f/(1<<20))
	}
	return fmt.Sprintf("%.0f KB", f/(1<<10))
}

func thousands(n int) string {
	s := strconv.Itoa(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

var reBitrateVal = regexp.MustCompile(`^(\d+(?:\.\d+)?)([kKmM]?)$`)

// parseBitrateBps returns bits per second for values like 640k, 20M, 8000.
func parseBitrateBps(s string) (float64, bool) {
	m := reBitrateVal.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0, false
	}
	v, _ := strconv.ParseFloat(m[1], 64)
	switch strings.ToLower(m[2]) {
	case "k":
		v *= 1e3
	case "m":
		v *= 1e6
	}
	return v, true
}

// ---------- audio ----------

// guessTrackBitrate is a typical bitrate (bits/s) for a track the container
// gives no size for. Only used for previews and when measuring fails.
func guessTrackBitrate(t srcTrack) float64 {
	ch := float64(max(t.Channels, 2))
	switch t.Codec {
	case "truehd":
		return 3_500_000
	case "dts":
		return 1_509_000
	case "flac", "pcm_s16le", "pcm_s24le":
		return 700_000 * ch / 2
	case "eac3":
		return 640_000
	case "ac3":
		return 448_000
	}
	return 128_000 * ch / 2
}

// srcAudioBytes is a track's size in the source, from container metadata.
// ok=false: no size information (with guess=true, a typical size is used).
func srcAudioBytes(t srcTrack, dur float64, guess bool) (float64, bool) {
	switch {
	case t.Bytes > 0:
		return float64(t.Bytes), true
	case t.BitRate > 0:
		return t.BitRate * dur / 8, true
	case guess:
		return guessTrackBitrate(t) * dur / 8, false
	}
	return 0, false
}

// quickAudioBytes estimates the size of the kept audio in the output from
// container metadata alone. exact=false means some track had no size
// information; with guess=true such tracks are filled in with a typical size.
func quickAudioBytes(audio []outAudio, dur float64, guess bool) (bytes int64, exact bool) {
	exact = true
	var total float64
	for _, a := range audio {
		if a.Codec != "copy" && a.Bitrate != "" {
			if v, ok := parseBitrateBps(a.Bitrate); ok && v > 0 {
				total += v * dur / 8
				continue
			}
		}
		src, ok := srcAudioBytes(a.Src, dur, guess)
		if !ok {
			exact = false
		}
		if a.Codec != "copy" { // FLAC: roughly 60% of the source's size
			src *= 0.6
			exact = false
		}
		total += src
	}
	return int64(total), exact
}

var reMeasure = regexp.MustCompile(`audio:\s*(\d+)\s*(?:kB|KiB)`)

// measureAudioBytes reads the whole file once, copying audio to a null output,
// and reports how much audio there is. Used only when metadata is incomplete.
func measureAudioBytes(ctx context.Context, ffmpeg, path string, tracks []srcTrack) (int64, error) {
	args := []string{"-hide_banner", "-v", "info", "-nostats", "-i", path}
	for _, t := range tracks {
		args = append(args, "-map", fmt.Sprintf("0:a:%d", t.N-1))
	}
	args = append(args, "-vn", "-sn", "-dn", "-c", "copy", "-f", "null", "-")
	cmd := toolCommand(ctx, ffmpeg, args...)
	out, _ := cmd.CombinedOutput()
	m := reMeasure.FindAllStringSubmatch(string(out), -1)
	if len(m) == 0 {
		return 0, fmt.Errorf("could not measure audio size")
	}
	kb, _ := strconv.ParseInt(m[len(m)-1][1], 10, 64)
	return kb * 1024, nil
}

// ---------- the plan ----------

type SizePlan struct {
	Target, Aim   int64
	Duration      float64
	AudioBytes    int64
	AudioExact    bool
	Overhead      int64 // container + subtitles
	DVExtra       int64 // Dolby Vision RPU, added after the encode
	VideoKbps     int
	BPP           float64 // bits per pixel per frame
	Guide         *QualityGuide
	Width, Height int
	FPS           float64
	Warnings      []string
}

// NonVideo is everything in the output that is not the video stream. (The
// Dolby Vision RPU travels inside the video file, so it is not part of this.)
func (s *SizePlan) NonVideo() int64 { return s.AudioBytes + s.Overhead }

func subtitleOverhead(subs []outSub, mp4 bool) int64 {
	var o int64
	for _, sub := range subs {
		c := strings.ToLower(sub.Src.Codec)
		switch {
		case textSubs[c]:
			o += 1 << 20
		case bitmapSubs[c] && !mp4:
			o += 25 << 20
		}
	}
	return o
}

// qualityLabel is a rough guide for HEVC at this many bits per pixel per frame.
func qualityLabel(bpp float64) string {
	switch {
	case bpp <= 0:
		return ""
	case bpp >= 0.08:
		return "excellent"
	case bpp >= 0.045:
		return "good"
	case bpp >= 0.03:
		return "acceptable"
	case bpp >= 0.018:
		return "low - expect visible compression artefacts"
	}
	return "very low - expect heavy artefacts"
}

// planSize works out the video bitrate that makes the whole file come out at
// cfg.TargetBytes, and stores it in cfg.Bitrate. cfg.Ffprobe/Ffmpeg must be set.
func planSize(ctx context.Context, cfg *Config, p *Probe, plan *Plan, srcBytes int64) (*SizePlan, error) {
	if p.Duration <= 0 {
		return nil, fmt.Errorf("cannot read the duration of this file, so a target size cannot be planned")
	}
	if cfg.CopyVideo {
		return nil, fmt.Errorf("a target file size cannot be used when the video is copied unchanged")
	}
	sp := &SizePlan{Target: cfg.TargetBytes, Duration: p.Duration, Width: p.Width, Height: p.Height}
	if plan.ScaleW > 0 { // judge the bitrate against the resized picture
		sp.Width, sp.Height = plan.ScaleW, plan.ScaleH
	}
	sp.Aim = int64(float64(sp.Target) * aimFraction)
	sp.FPS, _ = parseRational(p.AvgFPS)
	if sp.FPS <= 0 {
		sp.FPS, _ = parseRational(p.RFPS)
	}

	sp.AudioBytes, sp.AudioExact = quickAudioBytes(plan.Audio, p.Duration, false)
	if !sp.AudioExact {
		// copied tracks without size information: measure them (converted ones are predictable)
		var copied, converted []outAudio
		var copiedSrc []srcTrack
		for _, a := range plan.Audio {
			if a.Codec == "copy" {
				copied = append(copied, a)
				copiedSrc = append(copiedSrc, a.Src)
			} else {
				converted = append(converted, a)
			}
		}
		_, copyExact := quickAudioBytes(copied, p.Duration, false)
		measured := false
		if !copyExact && !cfg.DryRun { // the preview must stay instant, so it only guesses
			fmt.Println("Measuring audio size (reads the whole file once)...")
			if n, err := measureAudioBytes(ctx, cfg.Ffmpeg, cfg.Input, copiedSrc); err == nil {
				conv, convExact := quickAudioBytes(converted, p.Duration, true)
				sp.AudioBytes, sp.AudioExact, measured = n+conv, convExact, true
			}
		}
		if !measured {
			sp.AudioBytes, _ = quickAudioBytes(plan.Audio, p.Duration, true)
		}
	}
	if !sp.AudioExact {
		sp.Warnings = append(sp.Warnings, "the size of some audio tracks is estimated, so the result can differ a little more from the target")
	}
	sp.Overhead = int64(float64(sp.Aim)*containerFrac) + subtitleOverhead(plan.Subs, isMP4(cfg.Output))
	if plan.UseDV {
		sp.DVExtra = int64(float64(sp.Aim) * dvRPUFrac)
	}

	vb := sp.Aim - sp.NonVideo() - sp.DVExtra
	if vb <= 0 {
		return nil, fmt.Errorf("the target %s is too small: audio and overhead alone take about %s; raise the target or convert the audio to a lower bitrate",
			formatBytes(sp.Target), formatBytes(sp.NonVideo()+sp.DVExtra))
	}
	sp.VideoKbps = int(float64(vb) * 8 / sp.Duration / 1000)
	if sp.VideoKbps < minVideoKbps {
		return nil, fmt.Errorf("the target %s leaves only %d kbps for video (audio and overhead take about %s); choose a larger target or convert the audio to a lower bitrate",
			formatBytes(sp.Target), sp.VideoKbps, formatBytes(sp.NonVideo()+sp.DVExtra))
	}
	if sp.Width > 0 && sp.Height > 0 && sp.FPS > 0 {
		sp.BPP = float64(sp.VideoKbps) * 1000 / (float64(sp.Width) * float64(sp.Height) * sp.FPS)
	}
	if g := bitrateGuide(float64(sp.VideoKbps)*1000, cfg, p, plan); g != nil {
		sp.Guide, plan.Quality = g, g
		if g.Warn != "" {
			sp.Warnings = append(sp.Warnings, g.Warn+" (or choose a bigger target)")
		}
	}
	if srcBytes > 0 && sp.Target > srcBytes {
		sp.Warnings = append(sp.Warnings, fmt.Sprintf("the target (%s) is larger than the source file (%s); the result will be bigger than it needs to be - a quality (CRF) encode is usually better value",
			formatBytes(sp.Target), formatBytes(srcBytes)))
	}
	if cfg.Codec != "x265" {
		if cfg.Retries > 0 {
			sp.Warnings = append(sp.Warnings, fmt.Sprintf("hardware encoders only approximate a bitrate; the video will be re-encoded up to %d more time(s) with a corrected bitrate if the first result misses the target (x265 two-pass is more precise)", cfg.Retries))
		} else {
			sp.Warnings = append(sp.Warnings, "hardware encoders only approximate a bitrate; expect the file to land within roughly 5-10% of the target (x265 two-pass is much closer)")
		}
	}
	cfg.Bitrate = fmt.Sprintf("%dk", sp.VideoKbps)
	return sp, nil
}

// Describe is the one-line summary shown to the user.
func (s *SizePlan) Describe() string {
	d := fmt.Sprintf("Target %s: about %s kbps for video after ~%s of audio/overhead",
		formatBytes(s.Target), thousands(s.VideoKbps), formatBytes(s.NonVideo()+s.DVExtra))
	if s.Guide != nil {
		d += "; " + s.Guide.Text + " (rough guide)"
	} else if s.BPP > 0 {
		d += fmt.Sprintf("; %.3f bits/pixel, %s (rough guide)", s.BPP, qualityLabel(s.BPP))
	}
	return d
}

// ---------- hardware-encoder retry loop ----------

type attempt struct {
	N     int
	Kbps  int
	Total int64 // estimated size of the finished output
	Path  string
}

func inWindow(total, target int64) bool {
	return total <= target && float64(total) >= windowLow*float64(target)
}

// score: lower is better; going over the target is penalised heavily.
func score(total, target int64) float64 {
	if total <= target {
		return float64(target - total)
	}
	return float64(total-target) * 10
}

// runAttempts encodes at kbps and, for encoders that do not hit the bitrate
// precisely, re-encodes with a corrected bitrate until the size lands inside
// the window or the retries run out. encode returns the (estimated) final
// output size and the file it wrote; all attempts are returned so the caller
// can delete the ones it does not keep.
func runAttempts(sp *SizePlan, retries int, encode func(n, kbps int) (int64, string, error), logf func(string, ...any)) (best attempt, all []attempt, err error) {
	kbps, bi := sp.VideoKbps, -1
	for n := 0; n <= retries; n++ {
		total, path, e := encode(n, kbps)
		if e != nil {
			return best, all, e
		}
		all = append(all, attempt{N: n, Kbps: kbps, Total: total, Path: path})
		logf("Attempt %d: video %s kbps -> output about %s (target %s)", n+1, thousands(kbps), formatBytes(total), formatBytes(sp.Target))
		if bi < 0 || score(total, sp.Target) < score(all[bi].Total, sp.Target) {
			bi = len(all) - 1
		}
		if inWindow(total, sp.Target) || n == retries {
			break
		}
		video, want := total-sp.NonVideo(), sp.Aim-sp.NonVideo()
		if video <= 0 || want <= 0 {
			break
		}
		f := max(0.4, min(2.5, float64(want)/float64(video)))
		next := int(float64(kbps) * f)
		if next < minVideoKbps || next == kbps {
			break
		}
		kbps = next
	}
	return all[bi], all, nil
}
