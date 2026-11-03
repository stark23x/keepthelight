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

// Cropping black bars, for the standard-definition sizes (576p and 480p) only:
// old TV and DVD material is often stored inside a bigger frame (a 4:3 show in
// a 1920×1080 file, with bars at the sides). The bars are cut, and the picture
// is then shrunk to fit, so the new file holds only the picture and the TV adds
// bars where its screen needs them.

// cropAllowed: only with 576p or 480p (the owner's choice).
func cropAllowed(scale string) bool { return scale == "576p" || scale == "480p" }

type cropBox struct{ W, H, X, Y int }

var reCrop = regexp.MustCompile(`crop=(\d+):(\d+):(\d+):(\d+)`)

// lastCrop is cropdetect's final suggestion in a stretch of the film.
func lastCrop(out string) (cropBox, bool) {
	m := reCrop.FindAllStringSubmatch(out, -1)
	if m == nil {
		return cropBox{}, false
	}
	l := m[len(m)-1]
	w, _ := strconv.Atoi(l[1])
	h, _ := strconv.Atoi(l[2])
	x, _ := strconv.Atoi(l[3])
	y, _ := strconv.Atoi(l[4])
	return cropBox{w, h, x, y}, w > 0 && h > 0
}

// joinCrops combines what several stretches showed into the one box that keeps
// the picture of every one of them (so a scene with less black, such as the
// credits, is never cut). Stretches that were (nearly) all black say nothing.
// It returns false when there is nothing worth cutting: bars of less than 2% of
// a side are left alone.
func joinCrops(boxes []cropBox, fw, fh int) (cropBox, bool) {
	x0, y0, x1, y1 := fw, fh, 0, 0
	n := 0
	for _, b := range boxes {
		if b.W < fw/4 || b.H < fh/4 { // a dark or black stretch: no picture to measure
			continue
		}
		n++
		x0, y0 = min(x0, b.X), min(y0, b.Y)
		x1, y1 = max(x1, b.X+b.W), max(y1, b.Y+b.H)
	}
	if n == 0 {
		return cropBox{}, false
	}
	// keep a little more rather than less, on even numbers (needed for 4:2:0 video)
	x0, y0 = max(0, (x0-2)&^1), max(0, (y0-2)&^1)
	x1, y1 = min(fw, (x1+3)&^1), min(fh, (y1+3)&^1)
	w, h := x1-x0, y1-y0
	if float64(fw-w) < 0.02*float64(fw) {
		x0, w = 0, fw
	}
	if float64(fh-h) < 0.02*float64(fh) {
		y0, h = 0, fh
	}
	if w == fw && h == fh {
		return cropBox{}, false
	}
	return cropBox{w &^ 1, h &^ 1, x0, y0}, true
}

var cropCache sync.Map // path|size|time -> *cropBox (nil: no bars)

// detectCrop looks at five stretches of the film (10% to 90% of the way in)
// with FFmpeg's cropdetect. Results are remembered for this run of the program.
func detectCrop(ctx context.Context, ffmpeg, input string, fw, fh int, duration float64) *cropBox {
	key := input
	if st, err := os.Stat(input); err == nil {
		key = fmt.Sprintf("%s|%d|%d", input, st.Size(), st.ModTime().UnixNano())
	}
	if v, ok := cropCache.Load(key); ok {
		return v.(*cropBox)
	}
	starts := []float64{0}
	if duration > 120 {
		starts = nil
		for _, f := range []float64{0.1, 0.3, 0.5, 0.7, 0.9} {
			starts = append(starts, math.Floor(duration*f))
		}
	}
	var boxes []cropBox
	for _, ss := range starts {
		cctx, cancel := context.WithTimeout(ctx, 90*time.Second)
		out, _ := toolCommand(cctx, ffmpeg, "-hide_banner", "-nostdin", "-ss", strconv.FormatFloat(ss, 'f', 0, 64), "-i", input,
			"-map", "0:v:0", "-frames:v", "90", "-vf", "cropdetect=limit=24:round=2:reset=0", "-an", "-sn", "-f", "null", "-").CombinedOutput()
		cancel()
		if b, ok := lastCrop(string(out)); ok {
			boxes = append(boxes, b)
		}
	}
	if len(boxes) == 0 {
		return nil // could not tell (not remembered: a later try may work)
	}
	var res *cropBox
	if b, ok := joinCrops(boxes, fw, fh); ok {
		res = &b
	}
	cropCache.Store(key, res)
	return res
}

// applyCrop puts the crop into the plan, and fits the cropped picture into the
// chosen size again (it is now smaller than the frame).
func applyCrop(cfg *Config, p *Probe, plan *Plan, c *cropBox) {
	if c == nil || !cfg.Crop || !cropAllowed(cfg.Scale) || cfg.CopyVideo {
		return
	}
	if plan.UseDV {
		plan.Notes = append(plan.Notes, "black bars are not cropped in Dolby Vision video (its scene data would not match)")
		return
	}
	plan.CropW, plan.CropH, plan.CropX, plan.CropY = c.W, c.H, c.X, c.Y
	plan.ScaleW, plan.ScaleH = 0, 0
	box := scaleBoxes[cfg.Scale]
	if w, h, ok := fitSize(c.W, c.H, box[0], box[1]); ok {
		plan.ScaleW, plan.ScaleH, plan.SrcW, plan.SrcH = w, h, c.W, c.H
	}
}

// cropFilter cuts the bars (after deinterlacing, before resizing).
func (plan *Plan) cropFilter() string {
	if plan.CropW <= 0 {
		return ""
	}
	return fmt.Sprintf("crop=%d:%d:%d:%d", plan.CropW, plan.CropH, plan.CropX, plan.CropY)
}

// outSize is the size of the new file's picture.
func (plan *Plan) outSize(p *Probe) (int, int) {
	switch {
	case plan.ScaleW > 0:
		return plan.ScaleW, plan.ScaleH
	case plan.CropW > 0:
		return plan.CropW, plan.CropH
	}
	return p.Width, p.Height
}

// cropText describes it for the log and the window.
func cropText(p *Probe, plan *Plan) string {
	if plan.CropW <= 0 {
		return ""
	}
	w, h := plan.outSize(p)
	return fmt.Sprintf("a %d×%d picture in a %d×%d frame: cropped, then %d×%d", plan.CropW, plan.CropH, p.Width, p.Height, w, h)
}
