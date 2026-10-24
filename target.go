package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// ---------- size parsing / formatting ----------

var reSize = regexp.MustCompile(`^\s*(\d+(?:\.\d+)?)\s*(kb|kib|mb|mib|gb|gib|tb|tib)\s*$`)

// parseSize reads sizes like "8GB", "8.5 GB", "700MB". Units are binary
// (1 GB = 1024^3 bytes), which matches what Windows Explorer calls "GB".
func parseSize(s string) (int64, error) {
	m := reSize.FindStringSubmatch(strings.ToLower(s))
	if m == nil {
		return 0, fmt.Errorf("size must look like 8GB or 700MB")
	}
	v, _ := strconv.ParseFloat(m[1], 64)
	mult := map[string]float64{"k": 1 << 10, "m": 1 << 20, "g": 1 << 30, "t": 1 << 40}[m[2][:1]]
	b := int64(v * mult)
	if b < 1<<20 {
		return 0, fmt.Errorf("target size is too small")
	}
	return b, nil
}

func fmtSize(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.2f GB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/(1<<20))
	}
	return fmt.Sprintf("%d KB", b>>10)
}

// ---------- audio size estimate ----------

type audioInfo struct {
	Codec    string
	Channels int
	BitRate  float64 // bits per second, 0 if unknown
	Bytes    float64 // exact stream size from container statistics, 0 if unknown
}

func probeAudio(ctx context.Context, ffprobe, path string) ([]audioInfo, error) {
	out, err := toolCommand(ctx, ffprobe, "-v", "error", "-select_streams", "a",
		"-show_entries", "stream=codec_name,channels,bit_rate:stream_tags=NUMBER_OF_BYTES,NUMBER_OF_BYTES-eng", "-of", "json", path).Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe (audio) failed: %w", err)
	}
	return parseAudio(out)
}

func parseAudio(data []byte) ([]audioInfo, error) {
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
	var res []audioInfo
	for _, s := range j.Streams {
		a := audioInfo{Codec: s.Codec, Channels: s.Channels}
		a.BitRate, _ = strconv.ParseFloat(s.BitRate, 64)
		for k, v := range s.Tags {
			if strings.HasPrefix(strings.ToUpper(k), "NUMBER_OF_BYTES") {
				a.Bytes, _ = strconv.ParseFloat(v, 64)
			}
		}
		res = append(res, a)
	}
	return res, nil
}

// guessAudioBitrate is a last-resort estimate when the container reports nothing.
func guessAudioBitrate(a audioInfo) float64 {
	ch := float64(max(a.Channels, 2))
	switch a.Codec {
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

// defaultConvertBitrate picks a bitrate per track when audio is converted and
// the user did not choose one.
func defaultConvertBitrate(subs []audioInfo) int {
	maxCh := 2
	for _, a := range subs {
		maxCh = max(maxCh, a.Channels)
	}
	switch {
	case maxCh <= 2:
		return 192_000
	case maxCh <= 6:
		return 448_000
	}
	return 640_000
}

func parseBitrate(s string) (float64, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	mult := 1.0
	switch {
	case strings.HasSuffix(s, "k"):
		mult, s = 1e3, s[:len(s)-1]
	case strings.HasSuffix(s, "m"):
		mult, s = 1e6, s[:len(s)-1]
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v <= 0 {
		return 0, fmt.Errorf("bad bitrate %q", s)
	}
	return v * mult, nil
}

// audioBytes returns the estimated total audio size and a note when it is a guess.
func audioBytes(cfg *Config, tracks []audioInfo, duration float64) (total float64, guessed bool) {
	if cfg.Audio == "copy" {
		for _, a := range tracks {
			switch {
			case a.Bytes > 0:
				total += a.Bytes
			case a.BitRate > 0:
				total += a.BitRate * duration / 8
			default:
				total += guessAudioBitrate(a) * duration / 8
				guessed = true
			}
		}
		return
	}
	br := float64(defaultConvertBitrate(tracks))
	if cfg.AudioBitrate != "" {
		if v, err := parseBitrate(cfg.AudioBitrate); err == nil {
			br = v
		}
	}
	for _, a := range tracks {
		if cfg.Audio == "flac" {
			total += guessAudioBitrate(audioInfo{Codec: "flac", Channels: a.Channels}) * duration / 8
			guessed = true
		} else {
			total += br * duration / 8
		}
	}
	return
}

// ---------- the bitrate calculation ----------

const (
	overheadFrac   = 0.015 // container, subtitles, chapters
	dvOverheadFrac = 0.005 // Dolby Vision RPU data added after the encode
)

// videoBitrateFor returns the video bitrate (bits/s) that should make the whole
// file come out at targetBytes.
func videoBitrateFor(targetBytes int64, duration, audio float64, dv bool) (float64, error) {
	if duration <= 0 {
		return 0, fmt.Errorf("could not determine the video duration")
	}
	over := overheadFrac
	if dv {
		over += dvOverheadFrac
	}
	videoBytes := float64(targetBytes)*(1-over) - audio
	if videoBytes <= 0 {
		return 0, fmt.Errorf("the target size (%s) is smaller than the audio alone (%s); raise the target or convert the audio to a lower bitrate",
			fmtSize(targetBytes), fmtSize(int64(audio)))
	}
	return videoBytes * 8 / duration, nil
}

func parseFPS(s string) float64 {
	f, err := parseRational(s)
	if err != nil || f <= 0 || math.IsInf(f, 0) {
		return 24
	}
	return f
}

type TargetPlan struct {
	VideoKbps  int
	AudioBytes int64
	BPP        float64 // bits per pixel per frame
	Warnings   []string
}

// planTarget turns cfg.TargetBytes into cfg.Bitrate. It does nothing when no
// target size was requested.
func planTarget(ctx context.Context, cfg *Config, p *Probe, plan *Plan) (*TargetPlan, error) {
	if cfg.TargetBytes <= 0 {
		return nil, nil
	}
	if cfg.CopyVideo {
		return nil, fmt.Errorf("a target size needs the video to be re-encoded; turn off copy-video")
	}
	if cfg.Bitrate != "" {
		return nil, fmt.Errorf("use either a target size or a bitrate, not both")
	}
	tp := &TargetPlan{}
	tracks, err := probeAudio(ctx, cfg.Ffprobe, cfg.Input)
	if err != nil {
		tracks = nil
		tp.Warnings = append(tp.Warnings, "could not read the audio tracks; audio size not accounted for, so the result may come out larger than the target")
	}
	ab, guessed := audioBytes(cfg, tracks, p.Duration)
	if guessed {
		tp.Warnings = append(tp.Warnings, "audio size is estimated for some tracks, so the result can differ a little more from the target")
	}
	tp.AudioBytes = int64(ab)
	bps, err := videoBitrateFor(cfg.TargetBytes, p.Duration, ab, plan.UseDV)
	if err != nil {
		return nil, err
	}
	tp.VideoKbps = int(bps / 1000)
	if tp.VideoKbps < 200 {
		return nil, fmt.Errorf("the target size leaves only %d kbps for video, which is too low to be useful; choose a larger size", tp.VideoKbps)
	}
	cfg.Bitrate = fmt.Sprintf("%dk", tp.VideoKbps)
	if p.Width > 0 && p.Height > 0 {
		tp.BPP = bps / (float64(p.Width*p.Height) * parseFPS(p.AvgFPS))
		switch {
		case tp.BPP < 0.015:
			tp.Warnings = append(tp.Warnings, fmt.Sprintf("%.1f Mbps is very low for %dx%d video; expect obvious blocking and banding. Consider a bigger target.", bps/1e6, p.Width, p.Height))
		case tp.BPP < 0.03:
			tp.Warnings = append(tp.Warnings, fmt.Sprintf("%.1f Mbps is on the low side for %dx%d video; fine detail and dark scenes may suffer.", bps/1e6, p.Width, p.Height))
		}
	}
	if st, err := os.Stat(cfg.Input); err == nil && float64(cfg.TargetBytes) > float64(st.Size())*0.98 {
		tp.Warnings = append(tp.Warnings, fmt.Sprintf("the target is about as large as the source (%s); the result will not be bigger than the source needs, so use a quality (CRF) setting instead for best value", fmtSize(st.Size())))
	}
	if cfg.Codec != "x265" {
		tp.Warnings = append(tp.Warnings, "hardware encoders only approximate a bitrate; expect the file to land within roughly 5-10% of the target (x265 two-pass is much closer)")
	}
	return tp, nil
}

// scaleBitrate multiplies a bitrate string such as "9800k".
func scaleBitrate(b string, f float64) string {
	v, err := parseBitrate(b)
	if err != nil {
		return b
	}
	return fmt.Sprintf("%dk", int(v*f/1000))
}
