package main

import (
	"fmt"
	"strings"
)

// The "already converted" mark: every file whose video Keep the Light encodes
// gets a tag naming the version and settings ("Keep the Light 1.29.0 · x265
// CRF 18 · 1920×1080"), so the library scan, the queue and the Convert tab can
// warn before a file is converted a second time (each time costs quality and
// saves little).

const ktlTagKey = "KEEP_THE_LIGHT"

// ktlTag is the tag's text for this job ("" when the video is only copied).
func ktlTag(cfg *Config, p *Probe, plan *Plan) string {
	if cfg.CopyVideo {
		return ""
	}
	enc := map[string]string{"x265": "x265", "nvenc": "NVENC", "qsv": "QuickSync"}[cfg.Codec]
	if enc == "" {
		enc = cfg.Codec
	}
	q := fmt.Sprintf("CRF %d", cfg.CRF)
	switch {
	case cfg.TargetBytes > 0:
		q = "target " + formatBytes(cfg.TargetBytes)
	case cfg.Bitrate != "":
		q = "bitrate " + cfg.Bitrate
	}
	parts := []string{"Keep the Light " + version, enc + " " + q}
	if p != nil {
		w, h := plan.outSize(p)
		if w > 0 {
			parts = append(parts, fmt.Sprintf("%d×%d", w, h))
		}
	}
	return strings.Join(parts, " · ")
}

// ktlTagIn finds the tag among a file's tags (MKV keeps the key's case, MP4 may not).
func ktlTagIn(tags map[string]string) string {
	for k, v := range tags {
		if strings.EqualFold(k, ktlTagKey) {
			return v
		}
	}
	return ""
}
