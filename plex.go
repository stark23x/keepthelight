package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// The Plex check: things that are likely to make Plex convert a file while it
// is being watched. The library scan, the check when files are added and the
// check of each finished file all use plexIssues, so they always agree.

// plexSrc is what the Plex check looks at.
type plexSrc struct {
	VCodec, PixFmt string
	DVProfile      int
	Kbps           int // the whole file's average bitrate
	Audio, Subs    []plexTrack
}

type plexTrack struct {
	Codec           string
	Default, Forced bool
}

// plexIssue is one problem. Text is the full explanation (the library scan's
// wording); Short names it. When a file is added, State says what the current
// settings do about it and How says how (fixed) or what would fix it (open).
type plexIssue struct {
	Key   string `json:"key"`
	Short string `json:"short"`
	Text  string `json:"text"`
	State string `json:"state,omitempty"` // "fixed", "open" or "cannot"
	How   string `json:"how,omitempty"`
}

func plexIssues(s plexSrc) []plexIssue {
	var list []plexIssue
	add := func(key, short, text string) {
		for _, x := range list {
			if x.Key == key {
				return
			}
		}
		list = append(list, plexIssue{Key: key, Short: short, Text: text})
	}
	if s.VCodec == "h264" && strings.Contains(s.PixFmt, "10") {
		add("h264-10", "H.264 10-bit video", "H.264 10-bit: many TVs and streamers cannot play it, so Plex has to convert the video")
	}
	if name := map[string]string{"vc1": "VC-1", "mpeg2video": "MPEG-2"}[s.VCodec]; name != "" {
		add("old-video", name+" video", fmt.Sprintf("%s: an older format some TVs and streamers cannot play, so Plex may have to convert the video", strings.ToUpper(name)))
	}
	for _, t := range s.Subs {
		if t.Forced && bitmapSubs[t.Codec] {
			if t.Codec == "hdmv_pgs_subtitle" {
				add("forced-pgs", "Forced picture subtitles (PGS)", "Forced picture subtitles (PGS/VobSub): Plex burns them into the picture, which forces a video conversion (converting them to SRT fixes this)")
			} else {
				add("forced-bitmap", "Forced picture subtitles (VobSub)", "Forced picture subtitles (PGS/VobSub): Plex burns them into the picture, which forces a video conversion")
			}
		}
		if (t.Codec == "ass" || t.Codec == "ssa") && (t.Forced || t.Default) {
			add("ass-default", "Styled subtitles (ASS/SSA) shown by default", "Styled subtitles (ASS/SSA) shown by default: many Plex players cannot draw them, so Plex burns them in and converts the video (converting them to SRT fixes this)")
		}
	}
	switch s.DVProfile {
	case 7:
		add("dv7", "Dolby Vision profile 7", "Dolby Vision profile 7 (dual layer): most Plex players cannot show its Dolby Vision; KTL converts it to profile 8.1, which they can")
	case 5:
		add("dv5", "Dolby Vision profile 5", "Dolby Vision profile 5: Keep the Light cannot re-encode it (it can only copy it)")
	}
	compatible := false
	for _, a := range s.Audio {
		if compatibleCodecs[a.Codec] {
			compatible = true
		}
	}
	if len(s.Audio) > 0 && !compatible {
		add("audio", "No AC-3, E-AC-3 or AAC audio", "No AC-3, E-AC-3 or AAC audio: many TVs and streamers need Plex to convert the sound (the \"Add a compatible audio track\" rule fixes this)")
	}
	if s.Kbps > 80000 {
		add("bitrate", fmt.Sprintf("Very high bitrate (%d Mbps)", s.Kbps/1000), fmt.Sprintf("Very high bitrate (%d Mbps): may stutter over Wi-Fi or when watching away from home", s.Kbps/1000))
	}
	return list
}

// plexSrcFromScan reads ffprobe's -show_streams -show_format output.
func plexSrcFromScan(p *scanProbe, kbps int) plexSrc {
	s := plexSrc{Kbps: kbps}
	video := false
	for _, st := range p.Streams {
		switch st.Type {
		case "video":
			if st.Disp["attached_pic"] == 1 || video {
				continue
			}
			video = true
			s.VCodec, s.PixFmt = st.Codec, st.PixFmt
			for _, sd := range st.SideData {
				if t, _ := sd["side_data_type"].(string); strings.Contains(strings.ToLower(t), "dovi") {
					if v, ok := sd["dv_profile"].(float64); ok {
						s.DVProfile = int(v)
					}
				}
			}
		case "audio":
			s.Audio = append(s.Audio, plexTrack{Codec: st.Codec, Default: st.Disp["default"] == 1, Forced: st.Disp["forced"] == 1})
		case "subtitle":
			s.Subs = append(s.Subs, plexTrack{Codec: st.Codec, Default: st.Disp["default"] == 1, Forced: st.Disp["forced"] == 1})
		}
	}
	return s
}

// plexSrcFromProbe builds the source's view from what the window's check reads.
func plexSrcFromProbe(p *Probe, t *Tracks, fileSize int64) plexSrc {
	s := plexSrc{VCodec: p.Codec, PixFmt: p.PixFmt}
	if p.DV != nil {
		s.DVProfile = p.DV.Profile
	}
	if p.Duration > 0 && fileSize > 0 {
		s.Kbps = int(float64(fileSize) * 8 / p.Duration / 1000)
	}
	for _, a := range t.Audio {
		s.Audio = append(s.Audio, plexTrack{Codec: a.Codec, Default: a.Default(), Forced: a.Forced()})
	}
	for _, x := range t.Subs {
		s.Subs = append(s.Subs, plexTrack{Codec: x.Codec, Default: x.Default(), Forced: x.Forced()})
	}
	return s
}

// plexAfter is what the new file will be, from the plan.
func plexAfter(src plexSrc, cfg *Config, plan *Plan) plexSrc {
	out := plexSrc{VCodec: "hevc", PixFmt: "yuv420p10le"}
	if cfg.CopyVideo {
		out.VCodec, out.PixFmt, out.DVProfile, out.Kbps = src.VCodec, src.PixFmt, src.DVProfile, src.Kbps
	} else if plan.UseDV {
		out.DVProfile, _ = strconv.Atoi(strings.SplitN(plan.DVProfile, ".", 2)[0])
	}
	for _, a := range plan.Audio {
		c := a.Codec
		if c == "copy" {
			c = a.Src.Codec
		}
		out.Audio = append(out.Audio, plexTrack{Codec: c, Default: a.Default})
	}
	for _, x := range plan.Subs {
		c := x.Src.Codec
		if x.ToSRT || (x.Codec != "copy" && x.Codec != "") {
			c = "subrip" // converted to text
		}
		out.Subs = append(out.Subs, plexTrack{Codec: c, Default: x.Default, Forced: x.Forced})
	}
	return out
}

// plexOutcome compares the source's problems with the new file's: each is
// fixed by the settings, still there (with what would fix it), or cannot be
// fixed by Keep the Light.
func plexOutcome(src, out plexSrc, cfg *Config) []plexIssue {
	before, after := plexIssues(src), plexIssues(out)
	still := map[string]bool{}
	for _, x := range after {
		still[x.Key] = true
	}
	list := []plexIssue{} // never null: an empty list means "checked, nothing found"
	open := func(x plexIssue) plexIssue {
		x.State, x.How = "open", plexFixHint(x.Key, cfg)
		if x.Key == "dv5" {
			x.State, x.How = "cannot", "Keep the Light can only copy this video, so this stays as it is."
		}
		return x
	}
	for _, x := range before {
		if still[x.Key] {
			list = append(list, open(x))
			continue
		}
		x.State, x.How = "fixed", plexFixedHow(x.Key, out)
		list = append(list, x)
	}
	seen := map[string]bool{}
	for _, x := range before {
		seen[x.Key] = true
	}
	for _, x := range after { // a problem only the new file would have (a setting causes it)
		if !seen[x.Key] {
			list = append(list, open(x))
		}
	}
	return list
}

func plexFixedHow(key string, out plexSrc) string {
	switch key {
	case "h264-10", "old-video":
		return "the video is converted to HEVC"
	case "forced-pgs", "forced-bitmap", "ass-default":
		return "the subtitle settings take care of it (converted to SRT or left out)"
	case "dv7":
		if out.DVProfile == 8 {
			return "Dolby Vision becomes profile 8.1"
		}
		return "Dolby Vision is left out"
	case "audio":
		for _, a := range out.Audio {
			if compatibleCodecs[a.Codec] {
				return "the new file has " + strings.ToUpper(map[string]string{"eac3": "E-AC-3", "ac3": "AC-3", "aac": "AAC"}[a.Codec]) + " audio"
			}
		}
		return "the new file has no audio"
	case "bitrate":
		return "the video is re-encoded at a much lower bitrate"
	}
	return "fixed by the settings"
}

func plexFixHint(key string, cfg *Config) string {
	reencode := `Choose "Re-encode" under Video to fix it.`
	switch key {
	case "h264-10", "old-video", "bitrate", "dv7":
		if cfg.CopyVideo {
			return reencode
		}
		if key == "dv7" {
			return `Set Dolby Vision to "Keep Dolby Vision" to fix it.`
		}
		return ""
	case "forced-pgs", "ass-default":
		if cfg.MetaOnly {
			return `"Metadata only" cannot convert subtitles: leave that track out, or choose "Copy video untouched" and set it to SRT.`
		}
		if key == "ass-default" {
			return "Set that subtitle track to SRT (replacing it)."
		}
		return "Set that subtitle track to SRT (replacing it), or leave it out."
	case "forced-bitmap":
		return "Leave that subtitle track out (only PGS pictures can be turned into SRT)."
	case "audio":
		if cfg.MetaOnly {
			return `"Metadata only" cannot add audio: choose "Copy video untouched" or "Re-encode" and turn on "Add a compatible audio track when needed" under Audio.`
		}
		return `Turn on "Add a compatible audio track when needed" under Audio.`
	}
	return ""
}

// plexProblems counts what the new file will still have.
func plexProblems(list []plexIssue) int {
	n := 0
	for _, x := range list {
		if x.State != "fixed" {
			n++
		}
	}
	return n
}

// plexCheckFile checks a finished file and prints the result in the log: one
// "Plex check:" line per problem, or one saying it is clean.
func plexCheckFile(ctx context.Context, ffprobe, path string) []string {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	data, err := toolCommand(ctx, ffprobe, "-v", "error", "-show_streams", "-show_format", "-of", "json", path).Output()
	if err != nil {
		return []string{"Plex check: could not read the new file (" + err.Error() + ")"}
	}
	var p scanProbe
	if err := json.Unmarshal(data, &p); err != nil {
		return []string{"Plex check: could not read the new file"}
	}
	total, _ := strconv.ParseFloat(p.Format.BitRate, 64)
	if total == 0 {
		dur, _ := strconv.ParseFloat(p.Format.Duration, 64)
		if st, err := os.Stat(path); err == nil && dur > 0 {
			total = float64(st.Size()) * 8 / dur
		}
	}
	issues := plexIssues(plexSrcFromScan(&p, int(total/1000)))
	if len(issues) == 0 {
		return []string{"Plex check: no problems found in the new file."}
	}
	var lines []string
	for _, x := range issues {
		lines = append(lines, "Plex check: "+x.Text)
	}
	return lines
}
