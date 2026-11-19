package main

import (
	"context"
	"strings"
	"sync"
	"time"
)

// ---------- "This is animation" ----------
//
// Anime and cartoons are flat colour and clean lines, and a drawing is often
// held for two or three frames, with long still shots. Two things save space:
//   - x265's animation tune: fewer bits for fine texture, more for edges and
//     flat areas (it also deblocks a little more);
//   - more frames that say "same as before, with these changes" (B-frames), and
//     for the graphics cards a longer look ahead and AQ that follows motion.
// Not every graphics card can do all of it (NVENC on a GTX 10-series has no
// B-frames in HEVC), so the options are tried on a test picture first and the
// fullest set that works is used.

const animX265BFrames = 8 // x265's default is 4; the animation tune alone makes it 6

// animHWOptions are the hardware encoders' options, fullest first.
var animHWOptions = map[string][][]string{
	"nvenc": {
		{"-bf", "4", "-b_ref_mode", "middle", "-rc-lookahead", "32", "-temporal-aq", "1"},
		{"-bf", "4", "-rc-lookahead", "32", "-temporal-aq", "1"},
		{"-rc-lookahead", "32", "-temporal-aq", "1"},
		{"-rc-lookahead", "32"},
	},
	"qsv": {
		{"-bf", "7", "-adaptive_b", "1"},
		{"-bf", "7"},
	},
}

var animChecks sync.Map // codec -> animChoice

type animChoice struct {
	args []string
	msg  string
}

// animationOptions finds the animation options this PC's encoder accepts
// (once per run of the program), with a line saying what is used.
func animationOptions(ctx context.Context, ffmpeg, codec string) ([]string, string) {
	if codec != "nvenc" && codec != "qsv" {
		return nil, "Animation: x265's animation tune, up to 8 B-frames"
	}
	if v, ok := animChecks.Load(codec); ok {
		c := v.(animChoice)
		return c.args, c.msg
	}
	enc := map[string]string{"nvenc": "hevc_nvenc", "qsv": "hevc_qsv"}[codec]
	c := animChoice{msg: "Animation: this graphics card takes none of the extra options, so the file is encoded as usual"}
	for _, opts := range animHWOptions[codec] {
		tctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		args := []string{"-hide_banner", "-nostdin", "-v", "error", "-f", "lavfi", "-i", "color=c=gray:s=256x256:r=24:d=0.5",
			"-pix_fmt", "p010le", "-c:v", enc, "-profile:v", "main10"}
		args = append(append(args, opts...), "-f", "null", "-")
		err := toolCommand(tctx, ffmpeg, args...).Run()
		cancel()
		if err == nil {
			c = animChoice{args: opts, msg: "Animation: " + animWords(opts)}
			break
		}
	}
	animChecks.Store(codec, c)
	return c.args, c.msg
}

// animWords says in plain words what a set of options does.
func animWords(opts []string) string {
	s := " " + strings.Join(opts, " ") + " "
	var w []string
	if strings.Contains(s, " -bf ") {
		bf := opts[1]
		w = append(w, "up to "+bf+" B-frames")
		if strings.Contains(s, " -b_ref_mode ") {
			w[len(w)-1] += ", also used as references"
		}
	} else {
		w = append(w, "no B-frames (this graphics card cannot use them in HEVC)")
	}
	if strings.Contains(s, " -adaptive_b ") {
		w = append(w, "placed where they help")
	}
	if strings.Contains(s, " -rc-lookahead ") {
		w = append(w, "a longer look ahead")
	}
	if strings.Contains(s, " -temporal-aq ") {
		w = append(w, "AQ that follows motion")
	}
	return strings.Join(w, ", ")
}
