package main

import (
	"fmt"
	"math"
	"strings"
)

// The file's bitrate, shown in its card: the whole file's, and the video's
// own. The video's comes from the file when it says (the stream, or MKV's
// statistics tags); otherwise it is the whole file's less the audio.

// audioGuess is a lossless or unknown audio track's likely bitrate (bits/s):
// such tracks often carry no figure.
func audioGuess(t srcTrack) float64 {
	ch := float64(max(t.Channels, 2))
	c := strings.ToLower(t.Codec)
	switch {
	case c == "truehd" || (c == "dts" && strings.Contains(strings.ToLower(t.Profile), "ma")):
		return 4e6 * ch / 6
	case c == "flac" || strings.HasPrefix(c, "pcm"):
		return 0.8e6 * ch / 2
	case c == "dts":
		return 1.5e6
	case c == "ac3":
		return 0.448e6
	case c == "eac3":
		return 0.64e6
	}
	return 0.25e6
}

// videoRate is the video's bitrate, and whether it is the file's own figure.
func videoRate(p *Probe, audio []srcTrack) (float64, bool) {
	if p.VideoBpsExact || p.TotalBps <= 0 {
		return p.VideoBps, p.VideoBpsExact
	}
	v := p.TotalBps
	for _, a := range audio {
		if a.BitRate > 0 {
			v -= a.BitRate
		} else {
			v -= audioGuess(a)
		}
	}
	return max(v, p.TotalBps*0.2), false
}

// mbps writes a bitrate for people: "29.4 Mb/s", "850 kb/s".
func mbps(bps float64) string {
	if bps < 1e6 {
		return fmt.Sprintf("%.0f kb/s", bps/1e3)
	}
	return fmt.Sprintf("%.1f Mb/s", bps/1e6)
}

// rateCompare: the chosen target bitrate against the video's bitrate now.
// warn: the target saves nothing.
func rateCompare(target, now float64, exact bool) (string, bool) {
	if target <= 0 || now <= 0 {
		return "", false
	}
	about := ""
	if !exact {
		about = "about "
	}
	pct := int(math.Round(100 * (1 - target/now)))
	if pct <= 0 {
		return fmt.Sprintf("Your target of %s is not lower than this video's own bitrate (%s%s): the new file would not be smaller. Choose a lower target, or Constant quality.",
			mbps(target), about, mbps(now)), true
	}
	return fmt.Sprintf("Your target of %s is %d%% less than this video's own bitrate (%s%s).", mbps(target), pct, about, mbps(now)), false
}
