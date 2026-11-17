package main

import (
	"fmt"
	"math"
	"testing"
)

// The search finds the highest setting (smallest file) that still reaches the
// target, in a handful of tries, for curves of different shapes.
func TestAimerFind(t *testing.T) {
	curves := map[string]func(q int) float64{
		"gentle": func(q int) float64 { return 99 - 0.25*float64(q-12) },                  // 99 … 94.5: an easy film
		"steep":  func(q int) float64 { return 98.5 - 0.9*float64(q-12) },                 // a hard one
		"curved": func(q int) float64 { return 99.5 - 0.02*math.Pow(float64(q-10), 2.1) }, // falls faster and faster
		"grainy": func(q int) float64 { return 94 - 0.6*float64(q-12) },                   // never excellent
	}
	for name, f := range curves {
		for _, target := range []float64{97, 95, 93} {
			a := newAimer(6, func(q int) (aimPoint, error) { return aimPoint{Q: q, Score: f(q), Ratio: 1.6 - float64(q)*0.05}, nil })
			got, reached, err := a.find(target, 22)
			if err != nil {
				t.Fatal(err)
			}
			want, wantReached := aimLo, false // the truth, by trying everything
			for q := aimLo; q <= aimHi; q++ {
				if f(q) >= target {
					want, wantReached = q, true
				}
			}
			if reached != wantReached {
				t.Errorf("%s, target %g: reached %v, want %v", name, target, reached, wantReached)
				continue
			}
			if !reached {
				if got.Q != aimLo {
					t.Errorf("%s, target %g: not reachable, so the best setting (%d) is expected, got %d", name, target, aimLo, got.Q)
				}
				continue
			}
			// reaches the target, and is the true answer or within "close enough" of it
			if got.Score < target || (got.Q != want && got.Score > target+aimTol) {
				t.Errorf("%s, target %g: CRF %d (VMAF %.2f), the best is CRF %d", name, target, got.Q, got.Score, want)
			}
			if a.tries > 6 {
				t.Errorf("%s, target %g: %d tries", name, target, a.tries)
			}
		}
	}
}

// Three targets share their tries (the advice table).
func TestAimerShared(t *testing.T) {
	f := func(q int) float64 { return 99.2 - 0.03*math.Pow(float64(q-10), 2) }
	a := newAimer(9, func(q int) (aimPoint, error) { return aimPoint{Q: q, Score: f(q)}, nil })
	mid, _, _ := a.find(95, 22)
	hi, _, _ := a.find(97, mid.Q-3)
	lo, _, _ := a.find(93, mid.Q+3)
	if !(hi.Q < mid.Q && mid.Q < lo.Q) || hi.Score < 97 || mid.Score < 95 || lo.Score < 93 {
		t.Errorf("rows: %+v %+v %+v", hi, mid, lo)
	}
	if a.tries > 9 {
		t.Errorf("%d tries for three targets", a.tries)
	}
	t.Log(fmt.Sprintf("97→CRF %d, 95→CRF %d, 93→CRF %d in %d tries", hi.Q, mid.Q, lo.Q, a.tries))
}

// "Never bigger than the original": the setting moves up until the result is smaller.
func TestAimerSmaller(t *testing.T) {
	a := newAimer(6, func(q int) (aimPoint, error) {
		return aimPoint{Q: q, Score: 99 - float64(q)*0.1, Ratio: 2.2 - float64(q)*0.06}, nil
	})
	p, _ := a.at(16) // ratio 1.24: bigger than the original
	got, err := a.smaller(p, 1)
	if err != nil || got.Ratio >= 1 || got.Q != 22 {
		t.Errorf("smaller: %+v %v", got, err)
	}
}

func TestAimStarts(t *testing.T) {
	if s := aimStarts(7200, 10); len(s) != 3 || s[0] != 1440 || s[1] != 3600 || s[2] != 5760 {
		t.Errorf("film: %v", s)
	}
	if s := aimStarts(45, 10); len(s) != 2 {
		t.Errorf("short: %v", s)
	}
	if s := aimStarts(12, 10); len(s) != 1 || s[0] != 0 {
		t.Errorf("very short: %v", s)
	}
	size, saved := estimate(aimPoint{Ratio: 0.4}, 10_000, 9_000)
	if size != 4600 || saved != 5400 {
		t.Errorf("estimate: %d %d", size, saved)
	}
}

// A film whose score tops out below the target does not get the lowest setting.
func TestAimerKnee(t *testing.T) {
	curve := map[int]float64{}
	for q := aimLo; q <= aimHi; q++ { // flat near the top: 92.8 at 12, falling slowly, then faster
		curve[q] = 92.8 - 0.02*float64((q-aimLo)*(q-aimLo))
	}
	a := newAimer(9, func(q int) (aimPoint, error) { return aimPoint{Q: q, Score: curve[q], Ratio: 1 / float64(q)}, nil })
	_, reached, err := a.find(97, 22)
	if err != nil || reached {
		t.Fatalf("reached=%v err=%v", reached, err)
	}
	k, top, err := a.knee()
	if err != nil || top != curve[aimLo] {
		t.Fatalf("top %.2f err %v", top, err)
	}
	if k.Q <= aimLo+2 || k.Score < top-aimKnee {
		t.Errorf("knee at CRF %d (VMAF %.2f), top %.2f", k.Q, k.Score, top)
	}
}
