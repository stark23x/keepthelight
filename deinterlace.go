package main

import (
	"context"
	"fmt"
	"math"
	"os"
	"regexp"
	"strconv"
	"sync"
	"time"
)

// Deinterlacing, for old TV material. An interlaced picture is two half
// pictures (fields) woven line by line, taken a split second apart; shown as one
// picture, anything that moves has striped edges ("combing").
//
// Files are often labelled wrong (a converted DVD or recording can say
// "progressive" when every frame is combed), so the picture itself is checked
// with FFmpeg's idet filter, which also finds the field order. Getting that
// wrong makes motion jitter back and forth.
//
// The check is for files where interlacing is likely: standard definition
// (576 lines or fewer), files labelled interlaced, and HD files with no label.
// Films on Blu-ray and streaming files are progressive and labelled so; 4K is
// never interlaced. This keeps the check from reading gigabytes over the
// network for a library of films.

// Deinterlace settings: "" or "smooth" (one frame per field: 50/60 fps, the
// fluid look of videotaped TV), "standard" (one frame per frame: 25/30 fps), "off".
func validDeinterlace(s string) bool {
	return s == "" || s == "smooth" || s == "standard" || s == "off"
}

var deintNames = map[string]string{"smooth": "smooth motion, a frame per field", "standard": "standard frame rate"}

type interlaceInfo struct {
	Checked    bool   // the picture was looked at
	Interlaced bool   // most frames are combed
	Order      string // "tff" or "bff"
	Combed     int    // frames that look interlaced, of
	Frames     int    // frames looked at
}

// orderName is the field order in words.
func (d *interlaceInfo) orderName() string {
	if d.Order == "tff" {
		return "top field first"
	}
	return "bottom field first"
}

// worthChecking says whether to look at the picture (see above).
func worthChecking(height int, fieldOrder string) bool {
	switch fieldOrder {
	case "tt", "bb", "tb", "bt":
		return true // labelled interlaced: check the order (labels are often wrong)
	}
	if height <= 0 || height > 1088 {
		return false
	}
	return height <= 576 || fieldOrder == "" || fieldOrder == "unknown"
}

var reIdet = regexp.MustCompile(`Multi frame detection:\s*TFF:\s*(\d+)\s*BFF:\s*(\d+)\s*Progressive:\s*(\d+)\s*Undetermined:\s*(\d+)`)

// parseIdet adds up idet's multi-frame counts (ffmpeg can print more than one
// summary; the largest is the real one).
func parseIdet(out string) (tff, bff, prog int) {
	best := -1
	for _, m := range reIdet.FindAllStringSubmatch(out, -1) {
		t, _ := strconv.Atoi(m[1])
		b, _ := strconv.Atoi(m[2])
		p, _ := strconv.Atoi(m[3])
		u, _ := strconv.Atoi(m[4])
		if t+b+p+u > best {
			best, tff, bff, prog = t+b+p+u, t, b, p
		}
	}
	return
}

// judgeIdet decides from the counts: interlaced when at least 30 frames and
// 30% of the frames that could be judged are combed (shows often mix
// interlaced studio video with progressive film inserts), and one field order
// clearly wins. Real interlaced video is consistent; fine detail in a
// progressive picture can fool idet, but then the "orders" are mixed.
func judgeIdet(tff, bff, prog int) *interlaceInfo {
	d := &interlaceInfo{Checked: true, Combed: tff + bff, Frames: tff + bff + prog}
	d.Interlaced = d.Combed >= 30 && float64(d.Combed) >= 0.3*float64(d.Frames) &&
		float64(max(tff, bff)) >= 0.8*float64(d.Combed)
	d.Order = "bff"
	if tff > bff {
		d.Order = "tff"
	}
	return d
}

var interlaceCache sync.Map // path|size|time -> *interlaceInfo: the window checks the same file many times

// detectInterlace looks at the picture in two places (a quarter and 60% of the
// way in), 300 frames each. Results are remembered for this run of the program.
func detectInterlace(ctx context.Context, ffmpeg, input string, height int, fieldOrder string, duration float64) *interlaceInfo {
	if !worthChecking(height, fieldOrder) {
		return &interlaceInfo{}
	}
	key := input
	if st, err := os.Stat(input); err == nil {
		key = fmt.Sprintf("%s|%d|%d", input, st.Size(), st.ModTime().UnixNano())
	}
	if v, ok := interlaceCache.Load(key); ok {
		return v.(*interlaceInfo)
	}
	starts := []float64{0}
	if duration > 60 {
		starts = []float64{math.Floor(duration * 0.25), math.Floor(duration * 0.6)}
	}
	var tff, bff, prog int
	for _, ss := range starts {
		cctx, cancel := context.WithTimeout(ctx, 90*time.Second)
		out, _ := toolCommand(cctx, ffmpeg, "-hide_banner", "-nostdin", "-ss", strconv.FormatFloat(ss, 'f', 0, 64), "-i", input,
			"-map", "0:v:0", "-frames:v", "300", "-vf", "idet", "-an", "-sn", "-f", "null", "-").CombinedOutput()
		cancel()
		t, b, p := parseIdet(string(out))
		tff, bff, prog = tff+t, bff+b, prog+p
	}
	if tff+bff+prog == 0 {
		return &interlaceInfo{} // could not tell (not cached: a later try may work)
	}
	d := judgeIdet(tff, bff, prog)
	interlaceCache.Store(key, d)
	return d
}

// applyDeinterlace decides what the job does with an interlaced source.
func applyDeinterlace(cfg *Config, plan *Plan, d *interlaceInfo) {
	plan.Interlace = d
	if d == nil || !d.Interlaced || cfg.Deinterlace == "off" || cfg.CopyVideo {
		return
	}
	if plan.UseDV {
		plan.Warnings = append(plan.Warnings, "the picture looks interlaced, but Dolby Vision video is not deinterlaced (its scene data must match frame for frame)")
		return
	}
	mode := cfg.Deinterlace
	if mode == "" {
		mode = "smooth"
	}
	if mode == "smooth" && plan.UseHDR10Plus {
		mode = "standard" // HDR10+ has data for each frame; doubling the frames would misalign it
		plan.Notes = append(plan.Notes, "deinterlaced at the standard frame rate, which keeps HDR10+ in step")
	}
	plan.Deint, plan.FieldOrder = mode, d.Order
}

// deintFilter is the FFmpeg filter, applied before anything else (resizing an
// interlaced picture first would blend its fields). bwdif works in 10-bit, so
// HDR keeps its smooth gradients. deint=all: the label may say progressive.
func (plan *Plan) deintFilter() string {
	if plan.Deint == "" {
		return ""
	}
	mode := "send_field"
	if plan.Deint == "standard" {
		mode = "send_frame"
	}
	return fmt.Sprintf("bwdif=mode=%s:parity=%s:deint=all", mode, plan.FieldOrder)
}

// outFPS is the frame rate of the new file (smooth deinterlacing doubles it).
func outFPS(p *Probe, plan *Plan) float64 {
	fps, _ := parseRational(p.AvgFPS)
	if fps <= 0 {
		fps, _ = parseRational(p.RFPS)
	}
	if plan != nil && plan.Deint == "smooth" {
		fps *= 2
	}
	return fps
}

// deintText describes it for the log and the window.
func deintText(plan *Plan, fps float64) string {
	if plan.Deint == "" {
		return ""
	}
	s := fmt.Sprintf("deinterlaced (%s, %s)", plan.Interlace.orderName(), deintNames[plan.Deint])
	if fps > 0 {
		s = fmt.Sprintf("deinterlaced to %s fps (%s, %s)", strconv.FormatFloat(math.Round(fps*100)/100, 'f', -1, 64), plan.Interlace.orderName(), deintNames[plan.Deint])
	}
	return s
}
