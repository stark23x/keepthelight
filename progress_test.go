package main

import (
	"math"
	"testing"
	"time"
)

// simulate feeds an encode pass of a video of the given length at a steady
// speed (video seconds per second), checking along the way.
func simulate(p *progressTracker, now *time.Time, videoSecs, speed float64, check func(pos float64)) {
	for pos := 0.0; pos <= videoSecs; pos += 10 * speed {
		p.frameLine(pos, *now)
		if check != nil {
			check(pos)
		}
		*now = now.Add(10 * time.Second)
	}
}

func near(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

func TestProgressTwoPass(t *testing.T) {
	const dur = 7200.0 // a 2-hour film
	const speed = 0.5  // encoding at half real time: 4 h per pass
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	p := newProgressTracker(dur)

	if _, state, ok := p.remaining(now); ok || state != "estimating" {
		t.Fatalf("before encoding: ok=%v state=%q", ok, state)
	}
	p.stageLine("Encoding (pass 1 of 2)", now)
	start := now
	simulate(p, &now, dur, speed, func(pos float64) {
		el := now.Sub(start)
		left, _, ok := p.remaining(now)
		if el < settleTime {
			if ok {
				t.Fatalf("estimate before the speed settled (after %v)", el)
			}
			return
		}
		want := (dur-pos)/speed + dur/speed // rest of pass 1 + all of pass 2
		if !ok || !near(left, want, 60) {
			t.Fatalf("pass 1 at %.0fs: left=%.0f want %.0f ok=%v", pos, left, want, ok)
		}
		if pc := p.percent(); !near(pc, pos/dur*50, 0.01) {
			t.Fatalf("pass 1 percent %.2f at pos %.0f", pc, pos)
		}
	})
	// Pass 2: the estimate continues at once from pass 1's speed (no "estimating" gap)
	p.stageLine("Encoding (pass 2 of 2)", now)
	if left, _, ok := p.remaining(now); !ok || !near(left, dur/speed, 60) {
		t.Fatalf("start of pass 2: left=%.0f ok=%v", left, ok)
	}
	if pc := p.percent(); pc != 50 {
		t.Fatalf("start of pass 2: percent %.2f, want 50", pc)
	}
	simulate(p, &now, dur/2, speed, nil)
	if pc := p.percent(); !near(pc, 75, 0.1) {
		t.Fatalf("half way through pass 2: percent %.2f", pc)
	}
	p.finish()
	if p.percent() != 100 || p.etaText(now) != "" {
		t.Fatal("done should be 100% with no estimate")
	}
}

func TestProgressDolbyVision(t *testing.T) {
	const dur, speed = 3600.0, 1.0
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	p := newProgressTracker(dur)
	p.stageLine("[1/5] Extracting Dolby Vision RPU", now)
	if p.percent() != 0 || p.etaText(now) != "estimating" {
		t.Fatal("RPU extraction: should be estimating at 0%")
	}
	now = now.Add(2 * time.Minute)
	p.stageLine("[2/5] Encoding HDR base layer", now)
	simulate(p, &now, dur/2, speed, nil)
	left, _, ok := p.remaining(now)
	// half the encode (1800 s) plus the packaging allowance (3% of 3600 s)
	if !ok || !near(left, 1800+108, 30) {
		t.Fatalf("DV half way: left=%.0f ok=%v", left, ok)
	}
	if pc := p.percent(); !near(pc, 48.5, 0.5) { // encoding is 97% of a DV job
		t.Fatalf("DV half way: percent %.2f", pc)
	}
	simulate(p, &now, dur, speed, nil)
	for i, step := range []string{"[3/5] Injecting RPU into the new stream", "[4/5] Packaging Dolby Vision video track", "[5/5] Muxing with original audio/subtitles/chapters"} {
		p.stageLine(step, now)
		if p.etaText(now) != "finishing" {
			t.Fatalf("%s: eta %q", step, p.etaText(now))
		}
		if pc := p.percent(); pc < 97 || pc >= 100 || (i > 0 && pc <= 97) {
			t.Fatalf("%s: percent %.2f", step, pc)
		}
	}
	// the mux step prints ffmpeg progress lines too; they must not move the bar back
	p.frameLine(10, now)
	if p.percent() < 99 {
		t.Fatalf("mux progress line changed the percent to %.2f", p.percent())
	}
}

func TestProgressHardwareRetry(t *testing.T) {
	const dur, speed = 1200.0, 4.0
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	p := newProgressTracker(dur)
	p.stageLine("Encoding (attempt 1 of up to 3)", now)
	simulate(p, &now, dur, speed, nil)
	p.stageLine("Encoding (attempt 2 of up to 3)", now)
	if p.percent() != 0 {
		t.Fatalf("a retry starts its own bar at 0, got %.2f", p.percent())
	}
	if left, _, ok := p.remaining(now); !ok || !near(left, dur/speed, 5) {
		t.Fatalf("retry estimate from the first attempt's speed: left=%.0f ok=%v", left, ok)
	}
}

func TestProgressUnknownDuration(t *testing.T) {
	now := time.Now()
	p := newProgressTracker(0)
	p.stageLine("Encoding", now)
	p.frameLine(100, now.Add(time.Minute))
	if p.percent() != 0 || p.etaText(now.Add(time.Minute)) != "estimating" {
		t.Fatal("without a duration there is no percentage or estimate")
	}
}

func TestFormatRemaining(t *testing.T) {
	for _, c := range []struct {
		secs float64
		want string
	}{
		{20, "less than a minute left"},
		{61, "about 1 min left"},
		{14*60 + 40, "about 15 min left"},
		{59 * 60, "about 59 min left"},
		{59*60 + 50, "about 1 h left"},
		{95 * 60, "about 1 h 35 min left"},
		{3*3600 + 2*60, "about 3 h left"},
	} {
		if got := formatRemaining(c.secs); got != c.want {
			t.Errorf("%.0fs: got %q want %q", c.secs, got, c.want)
		}
	}
	if got := titleText(true, 42.7, "about 55 min left"); got != "42% · about 55 min left - "+windowTitle {
		t.Errorf("title: %q", got)
	}
	if got := titleText(false, 0, ""); got != windowTitle {
		t.Errorf("idle title: %q", got)
	}
}
