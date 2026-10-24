package main

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// progressTracker turns the job's output (step lines and ffmpeg progress lines)
// into a whole-job percentage and an estimate of the time left.
//
// The encode passes are nearly all of the work, so the estimate is: what is
// left of the current pass at the speed measured so far, plus any passes still
// to come at that speed, plus a small allowance for Dolby Vision packaging.
type progressTracker struct {
	duration  float64   // length of the video in seconds (0 = unknown)
	dv        bool      // Dolby Vision job: packaging steps follow the encode
	phase     string    // "pre" (before encoding), "encode", "post" (packaging), "done"
	step      int       // current [k/n] step number, if any
	pass      int       // current encode pass, 1-based (0 = not encoding yet)
	passes    int       // encode passes in total (2 for two-pass x265)
	passStart time.Time // when the current pass started
	pos       float64   // video seconds done in the current pass
	speed     float64   // measured video seconds per second (0 = not known yet)
}

// settleTime is how long a pass runs before its speed is trusted: ffmpeg is
// slow for the first seconds while it fills its buffers.
const settleTime = 30 * time.Second

var (
	reStepNum = regexp.MustCompile(`^\[(\d)/(\d)\]`)
	rePassNum = regexp.MustCompile(`pass (\d) of (\d)`)
	reEncStep = regexp.MustCompile(`^\[\d/\d\]\s*Encoding`)
)

func newProgressTracker(duration float64) *progressTracker {
	return &progressTracker{duration: duration, phase: "pre", passes: 1}
}

// stageLine handles a step line such as "Encoding (pass 1 of 2)" or
// "[4/5] Packaging Dolby Vision video track".
func (p *progressTracker) stageLine(line string, now time.Time) {
	isEncode := false
	if m := reStepNum.FindStringSubmatch(line); m != nil {
		p.step, _ = strconv.Atoi(m[1])
		if m[2] == "5" {
			p.dv = true
		}
		isEncode = reEncStep.MatchString(line)
	} else if strings.HasPrefix(line, "Encoding") {
		isEncode = true
	}
	if !isEncode {
		if p.pass > 0 {
			p.phase = "post"
		}
		return
	}
	// A new encode pass (or a new attempt, for hardware encoders aiming at a size).
	// The previous pass's speed was kept with its last progress line.
	p.pass, p.passes = 1, 1
	if m := rePassNum.FindStringSubmatch(line); m != nil {
		p.pass, _ = strconv.Atoi(m[1])
		p.passes, _ = strconv.Atoi(m[2])
	}
	p.phase, p.passStart, p.pos = "encode", now, 0
}

// frameLine handles an ffmpeg progress line; pos is its time= value in seconds.
func (p *progressTracker) frameLine(pos float64, now time.Time) {
	if p.phase != "encode" {
		return
	}
	p.pos = pos
	p.keepSpeed(now)
}

// keepSpeed records the current pass's average speed once it has settled.
func (p *progressTracker) keepSpeed(now time.Time) {
	if el := now.Sub(p.passStart); el >= settleTime && p.pos > 0 {
		p.speed = p.pos / el.Seconds()
	}
}

func (p *progressTracker) finish() { p.phase = "done" }

// liveSpeed is the current encode speed (video seconds per second), or the
// previous pass's while this one settles; 0 if not known yet.
func (p *progressTracker) liveSpeed(now time.Time) float64 {
	if p.phase == "encode" {
		if el := now.Sub(p.passStart); el >= settleTime && p.pos > 0 {
			return p.pos / el.Seconds()
		}
	}
	return p.speed
}

// percent is the whole job's progress, 0-100.
func (p *progressTracker) percent() float64 {
	encodeShare := 100.0
	if p.dv {
		encodeShare = 97 // the last few percent are the packaging steps
	}
	switch p.phase {
	case "done":
		return 100
	case "post":
		if p.dv && p.step >= 3 {
			return math.Min(99.5, encodeShare+float64(p.step-3)+0.5)
		}
		return 99.5
	case "encode":
		if p.duration <= 0 || p.passes <= 0 {
			return 0
		}
		frac := math.Max(0, math.Min(1, p.pos/p.duration))
		return math.Min(99.5, (float64(p.pass-1)+frac)/float64(p.passes)*encodeShare)
	}
	return 0
}

// remaining estimates the seconds left. ok is false while there is no
// estimate yet; then state says why ("estimating" or "finishing").
func (p *progressTracker) remaining(now time.Time) (secs float64, state string, ok bool) {
	switch p.phase {
	case "post":
		return 0, "finishing", false
	case "encode":
	default:
		return 0, "estimating", false
	}
	speed := p.speed // the previous pass's speed until this one settles
	if el := now.Sub(p.passStart); el >= settleTime && p.pos > 0 {
		speed = p.pos / el.Seconds()
	}
	if p.duration <= 0 || speed <= 0 {
		return 0, "estimating", false
	}
	encodeTotal := float64(p.passes) * p.duration / speed
	left := (p.duration-math.Min(p.pos, p.duration))/speed + float64(p.passes-p.pass)*p.duration/speed
	if p.dv { // inject + package + mux each read and write the whole video once
		left += math.Max(20, 0.03*encodeTotal)
	}
	return left, "", true
}

// etaText is the short form shown in the title bar and under the progress bar.
func (p *progressTracker) etaText(now time.Time) string {
	secs, state, ok := p.remaining(now)
	if !ok {
		if p.phase == "done" {
			return ""
		}
		return state
	}
	return formatRemaining(secs)
}

func formatRemaining(secs float64) string {
	switch mins := secs / 60; {
	case mins < 1:
		return "less than a minute left"
	case mins < 59.5:
		return fmt.Sprintf("about %d min left", int(math.Round(math.Max(1, mins))))
	default:
		total := int(math.Round(mins/5) * 5) // over an hour: nearest 5 minutes
		h, m := total/60, total%60
		if m == 0 {
			return fmt.Sprintf("about %d h left", h)
		}
		return fmt.Sprintf("about %d h %d min left", h, m)
	}
}
