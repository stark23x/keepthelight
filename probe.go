package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
)

type MasteringDisplay struct {
	RX, RY, GX, GY, BX, BY, WPX, WPY, MinL, MaxL string // ffprobe rationals, e.g. "35400/50000"
}

type ContentLight struct{ MaxCLL, MaxFALL int }

// DVLabelOnly (on Probe): the file is labelled Dolby Vision, but its video holds no Dolby Vision data.
type DoviConfig struct {
	Profile   int
	Level     int
	Compat    int
	ELPresent bool
}

type Probe struct {
	Codec, PixFmt                     string
	Width, Height                     int
	VideoBps                          float64 // the source video's bitrate (bits/s), 0 if unknown
	VideoBpsExact                     bool    // VideoBps is the file's own figure (not worked out)
	TotalBps                          float64 // the whole file's bitrate (bits/s)
	Primaries, Transfer, Space, Range string
	AvgFPS, RFPS                      string
	FieldOrder                        string // what the file says: progressive, tt, bb, tb, bt (often wrong in old material)
	Converted                         string // Keep the Light's "already converted" tag, if the file has one
	Mastering                         *MasteringDisplay
	CLL                               *ContentLight
	DV                                *DoviConfig
	DVLabelOnly                       *DoviConfig // labelled Dolby Vision, but no Dolby Vision data in the video
	noRPU                             bool        // a Dolby Vision label, but the first frame holds no Dolby Vision data
	HDR10Plus                         bool
	Duration                          float64
}

// Kind returns "hdr10", "hlg" or "sdr" based on the transfer characteristic.
func (p *Probe) Kind() string {
	switch p.Transfer {
	case "smpte2084":
		return "hdr10"
	case "arib-std-b67":
		return "hlg"
	}
	return "sdr"
}

func (p *Probe) Summary() string {
	s := fmt.Sprintf("%s %dx%d %s, primaries=%s transfer=%s matrix=%s",
		p.Codec, p.Width, p.Height, p.PixFmt, orUnknown(p.Primaries), orUnknown(p.Transfer), orUnknown(p.Space))
	if p.DV != nil {
		s += fmt.Sprintf(", Dolby Vision profile %d.%d level %d", p.DV.Profile, p.DV.Compat, p.DV.Level)
	}
	if p.Mastering != nil {
		s += ", mastering-display"
	}
	if p.CLL != nil {
		s += fmt.Sprintf(", MaxCLL/FALL %d/%d", p.CLL.MaxCLL, p.CLL.MaxFALL)
	}
	if p.HDR10Plus {
		s += ", HDR10+"
	}
	return s
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

type ffprobeOut struct {
	Streams []struct {
		CodecName     string            `json:"codec_name"`
		Width         int               `json:"width"`
		Height        int               `json:"height"`
		PixFmt        string            `json:"pix_fmt"`
		ColorRange    string            `json:"color_range"`
		ColorSpace    string            `json:"color_space"`
		ColorTransfer string            `json:"color_transfer"`
		ColorPrim     string            `json:"color_primaries"`
		RFrameRate    string            `json:"r_frame_rate"`
		AvgFrameRate  string            `json:"avg_frame_rate"`
		FieldOrder    string            `json:"field_order"`
		BitRate       string            `json:"bit_rate"`
		Tags          map[string]string `json:"tags"`
		SideData      []map[string]any  `json:"side_data_list"`
	} `json:"streams"`
	Format struct {
		Duration string            `json:"duration"`
		BitRate  string            `json:"bit_rate"`
		Tags     map[string]string `json:"tags"`
	} `json:"format"`
	Frames []struct {
		SideData []map[string]any `json:"side_data_list"`
	} `json:"frames"`
}

func probeFile(ctx context.Context, ffprobe, path string) (*Probe, error) {
	out, err := toolCommand(ctx, ffprobe, "-v", "error", "-select_streams", "v:0",
		"-show_streams", "-show_format", "-show_frames", "-read_intervals", "%+#1", "-of", "json", path).Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe failed on %q: %w", path, err)
	}
	p, err := parseProbe(out)
	if err == nil {
		p.labelOnly(ctx, ffprobe)
	}
	return p, err
}

func parseProbe(data []byte) (*Probe, error) {
	var j ffprobeOut
	if err := json.Unmarshal(data, &j); err != nil {
		return nil, fmt.Errorf("bad ffprobe output: %w", err)
	}
	if len(j.Streams) == 0 {
		return nil, fmt.Errorf("no video stream found")
	}
	s := j.Streams[0]
	p := &Probe{
		Codec: s.CodecName, PixFmt: s.PixFmt, Width: s.Width, Height: s.Height,
		Primaries: unk(s.ColorPrim), Transfer: unk(s.ColorTransfer), Space: unk(s.ColorSpace), Range: unk(s.ColorRange),
		AvgFPS: s.AvgFrameRate, RFPS: s.RFrameRate, FieldOrder: s.FieldOrder, Converted: ktlTagIn(j.Format.Tags),
	}
	p.Duration, _ = strconv.ParseFloat(j.Format.Duration, 64)
	// the video's own bitrate: from the stream, or MKV's statistics tags; else
	// most of the whole file's (a rough figure: the file also holds audio)
	p.VideoBps, _ = strconv.ParseFloat(s.BitRate, 64)
	for k, v := range s.Tags {
		if p.VideoBps == 0 && strings.HasPrefix(strings.ToUpper(k), "BPS") {
			p.VideoBps, _ = strconv.ParseFloat(strings.TrimSpace(v), 64)
		}
	}
	p.VideoBpsExact = p.VideoBps > 0
	p.TotalBps, _ = strconv.ParseFloat(j.Format.BitRate, 64)
	if p.VideoBps == 0 && p.TotalBps > 0 {
		p.VideoBps = p.TotalBps * 0.8 // refined by videoRate once the audio tracks are known
	}
	all := append([]map[string]any{}, s.SideData...)
	rpu := false // the first frame carries Dolby Vision data
	if len(j.Frames) > 0 {
		all = append(all, j.Frames[0].SideData...)
		for _, sd := range j.Frames[0].SideData {
			t, _ := sd["side_data_type"].(string)
			rpu = rpu || strings.Contains(strings.ToLower(t), "dolby vision")
		}
	}
	for _, sd := range all {
		t, _ := sd["side_data_type"].(string)
		lt := strings.ToLower(t)
		switch {
		case strings.Contains(lt, "mastering display"):
			if p.Mastering == nil {
				p.Mastering = &MasteringDisplay{
					RX: str(sd, "red_x"), RY: str(sd, "red_y"), GX: str(sd, "green_x"), GY: str(sd, "green_y"),
					BX: str(sd, "blue_x"), BY: str(sd, "blue_y"), WPX: str(sd, "white_point_x"), WPY: str(sd, "white_point_y"),
					MinL: str(sd, "min_luminance"), MaxL: str(sd, "max_luminance"),
				}
			}
		case strings.Contains(lt, "content light level"):
			if p.CLL == nil {
				p.CLL = &ContentLight{MaxCLL: int(num(sd, "max_content")), MaxFALL: int(num(sd, "max_average"))}
			}
		case strings.Contains(lt, "dovi configuration"):
			if p.DV == nil {
				p.DV = &DoviConfig{
					Profile: int(num(sd, "dv_profile")), Level: int(num(sd, "dv_level")),
					Compat: int(num(sd, "dv_bl_signal_compatibility_id")), ELPresent: num(sd, "el_present_flag") != 0,
				}
			}
		case strings.Contains(lt, "2094-40") || strings.Contains(lt, "hdr10+"):
			p.HDR10Plus = true
		}
	}
	// A Dolby Vision label (the container's configuration record) with no Dolby Vision data in the video: a
	// re-encode that kept the original's label. The file is what its own colour labels say.
	p.noRPU = p.DV != nil && len(j.Frames) > 0 && !rpu
	return p, nil
}

// labelOnly applies what parseProbe found: a Dolby Vision label without Dolby
// Vision data counts as no Dolby Vision. Only for an FFprobe that shows the
// data at all (FFmpeg 5 and newer), so an old one cannot hide real Dolby Vision.
func (p *Probe) labelOnly(ctx context.Context, ffprobe string) {
	if p.noRPU && showsRPU(ctx, ffprobe) {
		p.DVLabelOnly, p.DV = p.DV, nil
	}
}

var rpuVersions sync.Map // ffprobe path → bool

// showsRPU reports whether this FFprobe shows Dolby Vision data on frames
// (version 5 or newer; a build with a date or git name counts as new).
func showsRPU(ctx context.Context, ffprobe string) bool {
	if v, ok := rpuVersions.Load(ffprobe); ok {
		return v.(bool)
	}
	ok := true
	if out, err := toolCommand(ctx, ffprobe, "-version").Output(); err == nil {
		ok = ffmpegMajorAtLeast(string(out), 5)
	}
	rpuVersions.Store(ffprobe, ok)
	return ok
}

// ffmpegMajorAtLeast reads "ffprobe version 6.1.1-…" (or "n7.0", "N-11234-g…", "2024-05-01-git-…").
func ffmpegMajorAtLeast(version string, major int) bool {
	f := strings.Fields(version)
	if len(f) < 3 {
		return true
	}
	v := strings.TrimPrefix(f[2], "n")
	n, err := strconv.Atoi(strings.SplitN(strings.SplitN(v, ".", 2)[0], "-", 2)[0])
	if err != nil || n > 1000 { // a git build or a date: recent
		return true
	}
	return n >= major
}

func unk(s string) string {
	if s == "unknown" || s == "unspecified" {
		return ""
	}
	return s
}

func str(m map[string]any, k string) string {
	switch v := m[k].(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	return ""
}

func num(m map[string]any, k string) float64 {
	switch v := m[k].(type) {
	case float64:
		return v
	case string:
		f, _ := parseRational(v)
		return f
	}
	return 0
}

// parseRational parses "a/b" or a plain decimal.
func parseRational(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty value")
	}
	if a, b, ok := strings.Cut(s, "/"); ok {
		n, err1 := strconv.ParseFloat(a, 64)
		d, err2 := strconv.ParseFloat(b, 64)
		if err1 != nil || err2 != nil || d == 0 {
			return 0, fmt.Errorf("bad rational %q", s)
		}
		return n / d, nil
	}
	return strconv.ParseFloat(s, 64)
}

// X265MasterDisplay renders the x265 --master-display string
// (chromaticity in 0.00002 units, luminance in 0.0001 cd/m2 units).
func (m *MasteringDisplay) X265MasterDisplay() (string, error) {
	conv := func(s string, scale float64) (int, error) {
		v, err := parseRational(s)
		if err != nil {
			return 0, err
		}
		return int(math.Round(v * scale)), nil
	}
	vals := make([]int, 10)
	for i, f := range []struct {
		s     string
		scale float64
	}{
		{m.GX, 50000}, {m.GY, 50000}, {m.BX, 50000}, {m.BY, 50000}, {m.RX, 50000}, {m.RY, 50000},
		{m.WPX, 50000}, {m.WPY, 50000}, {m.MaxL, 10000}, {m.MinL, 10000},
	} {
		v, err := conv(f.s, f.scale)
		if err != nil {
			return "", err
		}
		vals[i] = v
	}
	return fmt.Sprintf("G(%d,%d)B(%d,%d)R(%d,%d)WP(%d,%d)L(%d,%d)",
		vals[0], vals[1], vals[2], vals[3], vals[4], vals[5], vals[6], vals[7], vals[8], vals[9]), nil
}
