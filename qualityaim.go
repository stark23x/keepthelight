package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

// ---------- aiming for a quality (VMAF) ----------
//
// "Constant quality" asks for a number (CRF) that means different things for
// different films: a clean animation looks perfect at a setting where a dark,
// grainy film falls apart. Aiming for a quality turns that round. Keep the
// Light cuts three short stretches from different parts of the film, converts
// them with the job's own encoder, filters and speed at a few settings, and
// measures each result against the original with VMAF (Netflix's score, 0–100,
// of how close a picture looks to its original). A try is judged by its worst
// stretch, so the hardest part of the film still reaches the target. The
// setting chosen is the smallest file that does.
//
// Two uses: "Aim for quality" (the job finds its own setting, then converts
// with it) and advice (the settings for Excellent / Very good / Good are found
// and shown with their estimated sizes; nothing is converted).

const (
	aimLo, aimHi = 12, 30 // the range of the quality setting, as in the window
	aimStretch   = 10     // seconds per stretch
	aimTol       = 0.75   // a try this close above the target is close enough
)

// aimPoint is one setting tried: its score (the worst stretch's VMAF) and how
// big the stretches came out, relative to the original's.
type aimPoint struct {
	Q     int     `json:"q"`
	Score float64 `json:"score"`
	Ratio float64 `json:"ratio"`
}

// aimer finds settings by trying them; every try is kept, so several targets
// share them.
type aimer struct {
	points   map[int]aimPoint
	measure  func(q int) (aimPoint, error)
	tries    int
	maxTries int
	slope    float64 // VMAF lost per step of the setting, until two tries say better
}

func newAimer(maxTries int, measure func(int) (aimPoint, error)) *aimer {
	return &aimer{points: map[int]aimPoint{}, measure: measure, maxTries: maxTries, slope: 0.5}
}

func (a *aimer) at(q int) (aimPoint, error) {
	if p, ok := a.points[q]; ok {
		return p, nil
	}
	p, err := a.measure(q)
	if err != nil {
		return p, err
	}
	a.tries++
	a.points[q] = p
	return p, nil
}

// bracket: the highest setting known to reach the target (-1: none), and the
// lowest one above it known not to (-1: none).
func (a *aimer) bracket(target float64) (pass, fail int) {
	pass, fail = -1, -1
	for q, p := range a.points {
		if p.Score >= target && q > pass {
			pass = q
		}
	}
	for q, p := range a.points {
		if p.Score < target && q > pass && (fail < 0 || q < fail) {
			fail = q
		}
	}
	return pass, fail
}

// find returns the highest setting (the smallest file) whose score reaches the
// target; reached=false when even the best setting tried falls short, and that
// one is returned.
func (a *aimer) find(target float64, start int) (best aimPoint, reached bool, err error) {
	clamp := func(q int) int { return max(aimLo, min(aimHi, q)) }
	for {
		pass, fail := a.bracket(target)
		switch {
		case pass >= 0 && (pass == aimHi || fail == pass+1 || a.points[pass].Score <= target+aimTol):
			return a.points[pass], true, nil
		case pass < 0 && fail == aimLo:
			return a.points[aimLo], false, nil
		case a.tries >= a.maxTries && len(a.points) > 0:
			if pass >= 0 {
				return a.points[pass], true, nil
			}
			return a.points[fail], false, nil
		}
		next := clamp(start)
		step := func(diff float64) int { // how far to move for a score this far from the target
			s := a.slopeNow()
			return max(1, min(6, int(math.Ceil(diff/s))))
		}
		switch {
		case pass >= 0 && fail >= 0: // between the two, where a straight line crosses the target
			p, f := a.points[pass], a.points[fail]
			x := float64(pass) + (p.Score-target)/(p.Score-f.Score)*float64(fail-pass)
			next = max(pass+1, min(fail-1, int(math.Floor(x))))
		case pass >= 0:
			next = clamp(pass + step(a.points[pass].Score-target))
		case fail >= 0:
			next = clamp(fail - step(target-a.points[fail].Score))
		}
		if _, err := a.at(next); err != nil {
			return aimPoint{}, false, err
		}
	}
}

// slopeNow: VMAF lost per step, from the two tries furthest apart when there are two.
func (a *aimer) slopeNow() float64 {
	if len(a.points) < 2 {
		return a.slope
	}
	qs := make([]int, 0, len(a.points))
	for q := range a.points {
		qs = append(qs, q)
	}
	sort.Ints(qs)
	lo, hi := a.points[qs[0]], a.points[qs[len(qs)-1]]
	if s := (lo.Score - hi.Score) / float64(hi.Q-lo.Q); s > 0.05 {
		return s
	}
	return a.slope
}

// aimKnee: when the target cannot be reached, a setting within this much of
// the best score possible is taken rather than the very lowest setting.
const aimKnee = 1.0

// knee is for a film whose score tops out below the target (grain and noise
// do that): pushing the setting to its lowest then buys almost nothing and
// costs a great deal of space. It returns the highest setting whose score is
// within aimKnee of the best seen, looking once between it and the next try,
// and the best score.
func (a *aimer) knee() (aimPoint, float64, error) {
	top := math.Inf(-1)
	for _, p := range a.points {
		top = math.Max(top, p.Score)
	}
	pick := func() (k aimPoint, next int) {
		k.Q, next = -1, -1
		for q, p := range a.points {
			if p.Score >= top-aimKnee && q > k.Q {
				k = p
			}
		}
		for q := range a.points {
			if q > k.Q && (next < 0 || q < next) {
				next = q
			}
		}
		return k, next
	}
	k, next := pick()
	if next-k.Q >= 3 && a.tries < a.maxTries+2 {
		if _, err := a.at((k.Q + next) / 2); err != nil {
			return k, top, err
		}
		k, _ = pick()
	}
	return k, top, nil
}

// smaller steps the setting up from p until the result is smaller than the
// original (ratio under limit), for "never bigger than the original".
func (a *aimer) smaller(p aimPoint, limit float64) (aimPoint, error) {
	for p.Ratio >= limit && p.Q < aimHi && a.tries < a.maxTries+3 {
		q := min(aimHi, p.Q+2)
		n, err := a.at(q)
		if err != nil {
			return p, err
		}
		p = n
	}
	return p, nil
}

// ---------- the stretches and the tries ----------

// aimStarts: where the stretches start. Three, at 20%, 50% and 80% of the film;
// fewer for something very short.
func aimStarts(duration float64, secs int) []float64 {
	s := float64(secs)
	switch {
	case duration <= 0 || duration < 2*s:
		return []float64{0}
	case duration < 8*s:
		return []float64{math.Floor(duration * 0.2), math.Floor(duration * 0.6)}
	}
	return []float64{math.Floor(duration * 0.2), math.Floor(duration * 0.5), math.Floor(duration * 0.8)}
}

// aimJob is everything a try needs.
type aimJob struct {
	ctx      context.Context
	cfg      *Config
	p        *Probe
	plan     *Plan
	dir      string
	stretch  []string // the cut stretches of the original (video only)
	srcBytes int64    // their size
	w, h     int
	pre      string // what the original gets before it is compared (deinterlacing, cropping, profile 5 colours)
	post     string // and after resizing (the same noise reduction)
	global   []string
}

// cutStretches copies the stretches' video out of the film, unchanged.
func (j *aimJob) cutStretches(input string, duration float64) error {
	for i, t := range aimStarts(duration, aimStretch) {
		name := filepath.Join(j.dir, fmt.Sprintf("aim-src%d.mkv", i))
		out, err := toolCommand(j.ctx, j.cfg.Ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-y", "-ss", strconv.FormatFloat(t, 'f', 3, 64),
			"-i", input, "-t", strconv.Itoa(aimStretch), "-map", "0:v:0", "-c", "copy", name).CombinedOutput()
		if err != nil {
			return fmt.Errorf("cutting a stretch at %s failed: %s", formatClock(t), lastLine(string(out)))
		}
		st, err := os.Stat(name)
		if err != nil || st.Size() < 1000 {
			continue // nothing there (past the end of a short file)
		}
		j.stretch = append(j.stretch, name)
		j.srcBytes += st.Size()
	}
	if len(j.stretch) == 0 {
		return fmt.Errorf("no stretch of the film could be cut for measuring")
	}
	return nil
}

// try converts every stretch at one setting and measures them.
func (j *aimJob) try(q int) (aimPoint, error) {
	pt := aimPoint{Q: q, Score: math.Inf(1)}
	var bytes int64
	for i, src := range j.stretch {
		out := filepath.Join(j.dir, fmt.Sprintf("aim-try%d.mkv", i))
		if err := j.encode(src, out, q); err != nil {
			return pt, err
		}
		st, err := os.Stat(out)
		if err != nil {
			return pt, err
		}
		bytes += st.Size()
		v, err := measureVMAF(j.ctx, j.cfg.Ffmpeg, out, src, j.w, j.h, j.pre, j.post, j.global)
		_ = os.Remove(out)
		if err != nil {
			return pt, err
		}
		pt.Score = math.Min(pt.Score, v)
	}
	pt.Ratio = float64(bytes) / float64(max(j.srcBytes, 1))
	return pt, nil
}

// encode converts one stretch's video with the job's settings at quality q.
// The frames go the normal way (not the full-GPU path, whose quick check was
// for the whole file); a profile 5 stretch falls back to the processor's
// decoding if the graphics card's fails.
func (j *aimJob) encode(src, out string, q int) error {
	tc := *j.cfg
	tc.CRF, tc.Bitrate, tc.TargetBytes, tc.Pass, tc.gpu = q, "", 0, 0, nil
	run := func(c *Config) ([]byte, error) {
		vargs, _ := buildVideoArgs(c, j.p, j.plan, "")
		args := append([]string{"-hide_banner", "-nostdin", "-v", "error", "-y"}, c.inputPrefix()...)
		args = append(args, "-i", src, "-map", "0:v:0")
		args = append(args, c.vsyncArgs...)
		args = append(args, "-an", "-sn", "-dn")
		args = append(args, vargs...)
		return toolCommand(j.ctx, c.Ffmpeg, append(args, out)...).CombinedOutput()
	}
	b, err := run(&tc)
	if err != nil && j.ctx.Err() == nil && tc.hwaccel != nil {
		tc.hwaccel, tc.p5OnGPU = nil, false
		b, err = run(&tc)
	}
	if err != nil {
		if j.ctx.Err() != nil {
			return j.ctx.Err()
		}
		return fmt.Errorf("converting a stretch failed: %s", lastLine(string(b)))
	}
	return nil
}

// measureVMAF scores a converted clip against its original (0–100). The
// original is first given what the conversion did besides encoding (pre, the
// resize to w×h, post), so the score is about the encoding alone.
func measureVMAF(ctx context.Context, ffmpeg, test, original string, w, h int, pre, post string, global []string) (float64, error) {
	if pre != "" {
		pre += ","
	}
	if post != "" {
		post = "," + post
	}
	f := fmt.Sprintf("[0:v]setpts=PTS-STARTPTS,format=yuv420p10le[d];[1:v]setpts=PTS-STARTPTS,%sscale=%d:%d:flags=lanczos%s,format=yuv420p10le[r];[d][r]libvmaf=n_subsample=3:n_threads=%d",
		pre, w, h, post, max(2, min(8, runtime.NumCPU())))
	args := append([]string{"-hide_banner", "-nostdin"}, global...)
	args = append(args, "-i", test, "-i", original, "-map", "0:v:0", "-map", "1:v:0", "-filter_complex", f, "-an", "-f", "null", "-")
	out, err := toolCommand(ctx, ffmpeg, args...).CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		return 0, fmt.Errorf("measuring failed: %s", lastLine(string(out)))
	}
	m := reVMAF.FindAllStringSubmatch(string(out), -1)
	if m == nil {
		return 0, fmt.Errorf("no VMAF score in ffmpeg's output")
	}
	return strconv.ParseFloat(m[len(m)-1][1], 64)
}

// ---------- the two uses ----------

// adviceRow is one line of the advice: a quality, the setting that gives it,
// and the size it is expected to make.
type adviceRow struct {
	Label     string  `json:"label"`
	Target    float64 `json:"target"`
	Q         int     `json:"q"`
	Score     float64 `json:"score"`
	Reached   bool    `json:"reached"`
	Bytes     int64   `json:"bytes"` // the whole new file, estimated
	Saved     int64   `json:"saved"` // against the original (negative: bigger)
	Suggested bool    `json:"suggested,omitempty"`
}

type qualityAdvice struct {
	Input    string      `json:"input"`
	Codec    string      `json:"codec"`
	Preset   string      `json:"preset"`
	SrcBytes int64       `json:"srcBytes"`
	Rows     []adviceRow `json:"rows"`
	Note     string      `json:"note,omitempty"`
	Tries    int         `json:"tries"`
}

var adviceTargets = []struct {
	Label  string
	Target float64
}{{"Excellent", 97}, {"Very good", 95}, {"Good", 93}}

// aimLabel names a target the way the window does.
func aimLabel(t float64) string {
	for _, a := range adviceTargets {
		if a.Target == t {
			return a.Label
		}
	}
	return ""
}

// aimStartFor: a first guess, in the middle of what usually looks very good.
func aimStartFor(codec string) int {
	if codec == "x265" {
		return 22
	}
	return 24
}

// aimSetup cuts the stretches and works out how the original is compared.
func aimSetup(ctx context.Context, cfg *Config, p *Probe, plan *Plan, tmp string) (*aimJob, float64, int64, error) {
	if !hasFilter(ctx, cfg.Ffmpeg, "libvmaf") {
		return nil, 0, 0, fmt.Errorf("aiming for a quality needs VMAF, which this FFmpeg does not have (libvmaf). " +
			"Use a \"full\" FFmpeg build, or choose Constant quality")
	}
	input, duration := cfg.Input, p.Duration
	if cfg.fullInput != "" { // a test: the setting is the film's, not the 30 seconds'
		input = cfg.fullInput
		if fp, err := probeFile(ctx, cfg.Ffprobe, input); err == nil {
			duration = fp.Duration
		}
	}
	dir := filepath.Join(tmp, "aim")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, 0, 0, err
	}
	j := &aimJob{ctx: ctx, cfg: cfg, p: p, plan: plan, dir: dir}
	j.w, j.h = plan.outSize(p)
	j.pre = plan.deintFilter()
	if plan.Reshape {
		j.pre = strings.Trim(reshapeFilter+","+j.pre, ",")
		j.global = append(j.global, cfg.vulkanArgs()...)
	}
	if c := plan.cropFilter(); c != "" {
		j.pre = strings.Trim(j.pre+","+c, ",")
	}
	if cfg.Denoise != "" { // compared with the cleaned-up original: removed noise is not a loss
		j.post = cfg.denoiseFilter()
		if cfg.hqDenoise != nil {
			j.global = append(j.global, cfg.hqDenoise.Global...)
		}
	}
	if err := j.cutStretches(input, duration); err != nil {
		return nil, 0, 0, err
	}
	var size int64
	if st, err := os.Stat(input); err == nil {
		size = st.Size()
	}
	return j, duration, size, nil
}

// sourceVideoBytes: how much of the file is its video.
func sourceVideoBytes(ctx context.Context, cfg *Config, p *Probe, input string, duration float64, size int64) int64 {
	bps := p.VideoBps
	if !p.VideoBpsExact {
		if t, err := probeTracks(ctx, cfg.Ffprobe, input); err == nil {
			bps, _ = videoRate(p, t.Audio)
		}
	}
	v := int64(bps * duration / 8)
	if v <= 0 || (size > 0 && v > size) {
		v = size * 9 / 10
	}
	return v
}

// estimate: the whole new file's size for a try (the video scales by the
// stretches' ratio; sound and the rest are taken as they are).
func estimate(pt aimPoint, size, video int64) (newSize, saved int64) {
	nv := int64(float64(video) * pt.Ratio)
	return size - video + nv, video - nv
}

// aimQuality finds the job's setting for cfg.QualityTarget.
func aimQuality(ctx context.Context, cfg *Config, p *Probe, plan *Plan, tmp string) (aimPoint, error) {
	target := cfg.QualityTarget
	fmt.Printf("\nAiming for quality: VMAF %g%s. Trying %s of the film at different settings\n", target, labelNote(target), stretchWords(cfg, p))
	j, duration, size, err := aimSetup(ctx, cfg, p, plan, tmp)
	if err != nil {
		return aimPoint{}, err
	}
	defer removeTemp(j.dir)
	a := newAimer(6, func(q int) (aimPoint, error) {
		pt, err := j.try(q)
		if err == nil {
			fmt.Printf("  CRF %d: VMAF %.1f at its hardest stretch, %s of the original's size\n", q, pt.Score, percent(pt.Ratio))
		}
		return pt, err
	})
	best, reached, err := a.find(target, aimStartFor(cfg.Codec))
	if err != nil {
		return best, err
	}
	if !reached {
		k, top, err := a.knee()
		if err != nil {
			return best, err
		}
		best = k
		fmt.Printf("WARNING: VMAF %g cannot be reached for this film: its score tops out at about %.1f (grain or noise does that), so CRF %d is used, "+
			"which comes within a point of that (VMAF %.1f)\n", target, top, best.Q, best.Score)
	}
	if best.Ratio >= 1 { // never bigger than the original
		was := best
		if best, err = a.smaller(best, 1); err != nil {
			return best, err
		}
		if best.Q != was.Q {
			fmt.Printf("WARNING: CRF %d would make the video bigger than the original, so CRF %d is used instead (VMAF %.1f). "+
				"This file is already compressed well; \"Copy video untouched\" may be the better choice\n", was.Q, best.Q, best.Score)
		}
		if best.Ratio >= 1 {
			fmt.Println("WARNING: the new video is expected to be about as big as the original, or bigger; \"Copy video untouched\" may be the better choice")
		}
	}
	video := sourceVideoBytes(ctx, cfg, p, cfg.Input, duration, size)
	if cfg.fullInput != "" {
		video = sourceVideoBytes(ctx, cfg, p, cfg.fullInput, duration, size)
	}
	est, _ := estimate(best, size, video)
	fmt.Printf("Quality aim: CRF %d gives VMAF %.1f at its hardest stretch (%d tries). Estimated size: about %s (the original is %s)\n",
		best.Q, best.Score, a.tries, fmtSize(est), fmtSize(size))
	if cfg.Denoise != "" {
		fmt.Println("Note: with noise reduction, the score compares the result with the cleaned-up original")
	}
	return best, nil
}

func labelNote(t float64) string {
	if l := aimLabel(t); l != "" {
		return " (" + strings.ToLower(l) + ")"
	}
	return ""
}

func percent(r float64) string { return fmt.Sprintf("%.0f%%", r*100) }

func stretchWords(cfg *Config, p *Probe) string {
	n := len(aimStarts(p.Duration, aimStretch))
	if cfg.fullInput != "" {
		n = 3
	}
	if n == 1 {
		return fmt.Sprintf("a %d-second stretch", aimStretch)
	}
	return fmt.Sprintf("%s %d-second stretches", map[int]string{2: "two", 3: "three"}[n], aimStretch)
}

// adviseQuality finds the settings for Excellent / Very good / Good and prints
// them as a table and as a line for the window ("QUALITY-ADVICE {json}").
func adviseQuality(ctx context.Context, cfg *Config, p *Probe, plan *Plan, tmp string) error {
	fmt.Printf("\nFinding the right setting: trying %s of the film at different settings\n", stretchWords(cfg, p))
	j, duration, size, err := aimSetup(ctx, cfg, p, plan, tmp)
	if err != nil {
		return err
	}
	defer removeTemp(j.dir)
	a := newAimer(9, func(q int) (aimPoint, error) {
		pt, err := j.try(q)
		if err == nil {
			fmt.Printf("  CRF %d: VMAF %.1f at its hardest stretch, %s of the original's size\n", q, pt.Score, percent(pt.Ratio))
		}
		return pt, err
	})
	video := sourceVideoBytes(ctx, cfg, p, cfg.Input, duration, size)
	adv := qualityAdvice{Input: cfg.Input, Codec: cfg.Codec, Preset: cfg.Preset, SrcBytes: size}
	// Very good first (the suggestion), then the two either side of it, from what was learnt
	order := []int{1, 0, 2}
	top := 0.0
	rows := make([]adviceRow, len(adviceTargets))
	start := aimStartFor(cfg.Codec)
	for _, i := range order {
		t := adviceTargets[i]
		s := start
		if i == 0 {
			s = rows[1].Q - 3
		} else if i == 2 {
			s = rows[1].Q + 3
		}
		pt, reached, err := a.find(t.Target, s)
		if err != nil {
			return err
		}
		if !reached {
			if pt, top, err = a.knee(); err != nil {
				return err
			}
		}
		est, saved := estimate(pt, size, video)
		rows[i] = adviceRow{Label: t.Label, Target: t.Target, Q: pt.Q, Score: pt.Score, Reached: reached, Bytes: est, Saved: saved}
	}
	// the suggestion: Very good, unless that is not smaller than the original and Good is
	sug := 1
	if rows[1].Saved <= 0 && rows[2].Saved > 0 {
		sug = 2
	}
	rows[sug].Suggested = true
	adv.Rows, adv.Tries = rows, a.tries
	switch {
	case rows[2].Q == aimHi && rows[2].Score >= 96:
		adv.Note = "This film compresses very well: even the smallest setting looks excellent."
	case rows[2].Saved <= 0:
		adv.Note = "This file is already compressed well: converting it again saves nothing at these qualities. \"Copy video untouched\" may be the better choice."
	case !rows[0].Reached:
		adv.Note = fmt.Sprintf("This film's score tops out at about VMAF %.1f (grain or noise does that), so %g cannot be reached. "+
			"Where a row falls short, its setting comes within a point of the best possible; a lower setting would only make the file bigger.", top, rows[0].Target)
	}
	if cfg.Denoise != "" {
		adv.Note = strings.TrimSpace(adv.Note + " With noise reduction, the score compares the result with the cleaned-up original.")
	}
	fmt.Println()
	for _, r := range rows {
		mark := ""
		if r.Suggested {
			mark = "  <- suggested"
		}
		if !r.Reached {
			mark = fmt.Sprintf("  (short of %g: near the best possible)", r.Target) + mark
		}
		fmt.Printf("%-10s CRF %d  VMAF %.1f  about %s (%s)%s\n", r.Label+":", r.Q, r.Score, fmtSize(r.Bytes), savedWords(r.Saved), mark)
	}
	if adv.Note != "" {
		fmt.Println("Note:", adv.Note)
	}
	b, _ := json.Marshal(adv)
	fmt.Println("QUALITY-ADVICE " + string(b))
	return nil
}

func savedWords(saved int64) string {
	if saved <= 0 {
		return "no saving"
	}
	return "saves " + fmtSize(saved)
}
