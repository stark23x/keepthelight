package main

import (
	"context"
	"encoding/json"
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

type audioTrack struct {
	Codec    string
	Channels int
	Bytes    int64   // from container statistics tags, 0 if unknown
	Bitrate  float64 // bits per second, 0 if unknown
}

func probeAudioTracks(ctx context.Context, ffprobe, path string) ([]audioTrack, error) {
	out, err := toolCommand(ctx, ffprobe, "-v", "error", "-select_streams", "a",
		"-show_entries", "stream=codec_name,channels,bit_rate:stream_tags", "-of", "json", path).Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe (audio) failed: %w", err)
	}
	return parseAudioTracks(out)
}

func parseAudioTracks(data []byte) ([]audioTrack, error) {
	var j struct {
		Streams []struct {
			Codec    string            `json:"codec_name"`
			Channels int               `json:"channels"`
			BitRate  string            `json:"bit_rate"`
			Tags     map[string]string `json:"tags"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(data, &j); err != nil {
		return nil, err
	}
	var res []audioTrack
	for _, s := range j.Streams {
		a := audioTrack{Codec: s.Codec, Channels: s.Channels}
		a.Bitrate, _ = strconv.ParseFloat(s.BitRate, 64)
		for k, v := range s.Tags {
			uk := strings.ToUpper(k)
			if strings.HasPrefix(uk, "NUMBER_OF_BYTES") {
				if n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
					a.Bytes = n
				}
			} else if strings.HasPrefix(uk, "BPS") && a.Bitrate == 0 {
				a.Bitrate, _ = strconv.ParseFloat(strings.TrimSpace(v), 64)
			}
		}
		res = append(res, a)
	}
	return res, nil
}

// defaultAudioKbps is used when audio is converted and no bitrate was chosen,
// so the size of the converted audio is predictable. 0 = cannot be predicted.
func defaultAudioKbps(codec string) int {
	switch codec {
	case "aac":
		return 256
	case "eac3", "ac3":
		return 640
	case "libopus":
		return 192
	}
	return 0
}

// guessTrackBitrate is a typical bitrate (bits/s) for a track the container
// gives no size for. Only used for previews and when measuring fails.
func guessTrackBitrate(t audioTrack) float64 {
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

// quickAudioBytes estimates the size of the audio in the output from container
// metadata alone. exact=false means some track had no size information; with
// guess=true such tracks are filled in with a typical bitrate for their codec.
func quickAudioBytes(cfg *Config, dur float64, tracks []audioTrack, guess bool) (bytes int64, exact bool) {
	exact = true
	var src float64
	for _, t := range tracks {
		switch {
		case t.Bytes > 0:
			src += float64(t.Bytes)
		case t.Bitrate > 0:
			src += t.Bitrate * dur / 8
		default:
			exact = false
			if guess {
				src += guessTrackBitrate(t) * dur / 8
			}
		}
	}
	if cfg.Audio == "copy" || cfg.Audio == "" {
		return int64(src), exact
	}
	kbps := defaultAudioKbps(cfg.Audio)
	if v, ok := parseBitrateBps(cfg.AudioBitrate); ok && v > 0 {
		kbps = int(v / 1000)
	}
	if kbps == 0 { // FLAC etc.: roughly 60% of the source's audio size
		return int64(src * 0.6), false
	}
	return int64(float64(kbps) * 1000 * dur / 8 * float64(len(tracks))), true
}

var reMeasure = regexp.MustCompile(`audio:\s*(\d+)\s*(?:kB|KiB)`)

// measureAudioBytes reads the whole file once, copying audio to a null output,
// and reports how much audio there is. Used only when metadata is incomplete.
func measureAudioBytes(ctx context.Context, ffmpeg, path string) (int64, error) {
	cmd := toolCommand(ctx, ffmpeg, "-hide_banner", "-v", "info", "-nostats", "-i", path,
		"-map", "0:a?", "-vn", "-sn", "-dn", "-c", "copy", "-f", "null", "-")
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
	Width, Height int
	FPS           float64
	Warnings      []string
}

// NonVideo is everything in the output that is not the video stream. (The
// Dolby Vision RPU travels inside the video file, so it is not part of this.)
func (s *SizePlan) NonVideo() int64 { return s.AudioBytes + s.Overhead }

func subtitleOverhead(subs []subInfo, mp4 bool) int64 {
	var o int64
	for _, sub := range subs {
		c := strings.ToLower(sub.Codec)
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
	sp.Aim = int64(float64(sp.Target) * aimFraction)
	sp.FPS, _ = parseRational(p.AvgFPS)
	if sp.FPS <= 0 {
		sp.FPS, _ = parseRational(p.RFPS)
	}

	tracks, err := probeAudioTracks(ctx, cfg.Ffprobe, cfg.Input)
	if err != nil {
		tracks = nil
		sp.Warnings = append(sp.Warnings, "could not read the audio tracks; audio size is not accounted for, so the result may come out larger than the target")
	}
	if cfg.Audio != "copy" && cfg.AudioBitrate == "" && len(tracks) > 0 {
		if k := defaultAudioKbps(cfg.Audio); k > 0 {
			cfg.AudioBitrate = fmt.Sprintf("%dk", k) // make the encoder use the bitrate the plan assumed
		}
	}
	sp.AudioBytes, sp.AudioExact = quickAudioBytes(cfg, p.Duration, tracks, false)
	if !sp.AudioExact && cfg.Audio == "copy" && len(tracks) > 0 {
		measured := false
		if !cfg.DryRun { // the preview must stay instant, so it only guesses
			fmt.Println("Measuring audio size (reads the whole file once)...")
			if n, err := measureAudioBytes(ctx, cfg.Ffmpeg, cfg.Input); err == nil {
				sp.AudioBytes, sp.AudioExact, measured = n, true, true
			}
		}
		if !measured {
			sp.AudioBytes, _ = quickAudioBytes(cfg, p.Duration, tracks, true)
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
		if sp.BPP < 0.018 {
			sp.Warnings = append(sp.Warnings, fmt.Sprintf("%.1f Mbps is very low for %dx%d video; expect obvious blocking and banding. Consider a bigger target.",
				float64(sp.VideoKbps)/1000, sp.Width, sp.Height))
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
	if s.BPP > 0 {
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
