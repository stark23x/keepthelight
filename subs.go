package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type subInfo struct {
	Codec string
	Lang  string
}

// probeSubs lists the subtitle streams of a file in order (index N here is
// ffmpeg's "0:s:N").
func probeSubs(ctx context.Context, ffprobe, path string) ([]subInfo, error) {
	out, err := toolCommand(ctx, ffprobe, "-v", "error", "-select_streams", "s",
		"-show_entries", "stream=codec_name:stream_tags=language", "-of", "json", path).Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe (subtitles) failed: %w", err)
	}
	return parseSubs(out)
}

func parseSubs(data []byte) ([]subInfo, error) {
	var j struct {
		Streams []struct {
			Codec string            `json:"codec_name"`
			Tags  map[string]string `json:"tags"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(data, &j); err != nil {
		return nil, err
	}
	var subs []subInfo
	for _, s := range j.Streams {
		subs = append(subs, subInfo{Codec: s.Codec, Lang: s.Tags["language"]})
	}
	return subs, nil
}

var textSubs = map[string]bool{
	"subrip": true, "srt": true, "ass": true, "ssa": true, "mov_text": true, "webvtt": true, "text": true, "ttml": true,
}

var bitmapSubs = map[string]bool{
	"hdmv_pgs_subtitle": true, "dvd_subtitle": true, "dvb_subtitle": true, "xsub": true, "dvb_teletext": false,
}

// subtitleArgs returns the ffmpeg arguments to carry the subtitle tracks of
// input number `in` into the output. Every track gets a codec its container can
// actually store; tracks that cannot be kept are dropped with a warning.
//
//	MKV: text tracks are copied, except mov_text (MP4's format) which MKV cannot
//	     store, so it is converted to SubRip. Bitmap tracks (PGS, VobSub) are copied.
//	MP4: text tracks become mov_text (styling is lost for ASS); bitmap tracks
//	     cannot be stored and are dropped.
func subtitleArgs(subs []subInfo, in int, mp4 bool) (args []string, warns []string) {
	n := 0
	for i, s := range subs {
		codec := strings.ToLower(s.Codec)
		var out string
		switch {
		case mp4 && textSubs[codec]:
			out = "mov_text"
			if codec != "mov_text" && codec != "text" {
				if codec == "ass" || codec == "ssa" {
					warns = append(warns, fmt.Sprintf("subtitle track %d (%s) is converted to plain MP4 text; styling is lost (use MKV to keep it)", i+1, codec))
				}
			}
		case mp4:
			warns = append(warns, fmt.Sprintf("subtitle track %d (%s) cannot be stored in MP4 and was dropped (use MKV to keep it)", i+1, orUnknown(codec)))
			continue
		case codec == "mov_text" || codec == "text":
			out = "srt"
		case textSubs[codec] || bitmapSubs[codec]:
			out = "copy"
		default:
			out = "copy" // unknown but non-MP4 target: let MKV try; a bad track is reported by ffmpeg
		}
		args = append(args, "-map", fmt.Sprintf("%d:s:%d", in, i), fmt.Sprintf("-c:s:%d", n), out)
		n++
	}
	return args, warns
}

// attachmentArgs keeps fonts/cover images in MKV output only.
func attachmentArgs(in int, mp4 bool) []string {
	if mp4 {
		return nil
	}
	return []string{"-map", fmt.Sprintf("%d:t?", in), "-c:t", "copy"}
}
