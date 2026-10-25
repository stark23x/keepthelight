package main

import "fmt"

var textSubs = map[string]bool{
	"subrip": true, "srt": true, "ass": true, "ssa": true, "mov_text": true, "webvtt": true, "text": true, "ttml": true,
}

var bitmapSubs = map[string]bool{
	"hdmv_pgs_subtitle": true, "dvd_subtitle": true, "dvb_subtitle": true, "xsub": true, "dvb_teletext": false,
}

// attachmentArgs keeps fonts/cover images in MKV output only.
func attachmentArgs(in int, mp4 bool) []string {
	if mp4 {
		return nil
	}
	return []string{"-map", fmt.Sprintf("%d:t?", in), "-c:t", "copy"}
}
