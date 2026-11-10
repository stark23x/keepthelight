package main

import (
	"strings"
	"testing"
)

func TestVideoRate(t *testing.T) {
	// Heat: 29.4 Mb/s in all, DTS-HD MA 5.1 (no figure) and E-AC-3 at 256 kb/s
	p := &Probe{TotalBps: 29.4e6, VideoBps: 29.4e6 * 0.8}
	v, exact := videoRate(p, []srcTrack{{Codec: "dts", Profile: "DTS-HD MA", Channels: 6}, {Codec: "eac3", Channels: 6, BitRate: 256000}})
	if exact || v < 24.5e6 || v > 25.5e6 {
		t.Errorf("Heat video: %.2f Mb/s exact=%v", v/1e6, exact)
	}
	p = &Probe{TotalBps: 4.2e6, VideoBps: 3.9e6, VideoBpsExact: true}
	if v, exact := videoRate(p, nil); !exact || v != 3.9e6 {
		t.Errorf("own figure: %v %v", v, exact)
	}
	if s, warn := rateCompare(3e6, 3.9e6, true); warn || !strings.Contains(s, "23% less") || !strings.Contains(s, "(3.9 Mb/s)") {
		t.Errorf("compare: %q", s)
	}
	if s, warn := rateCompare(5e6, 3.9e6, false); !warn || !strings.Contains(s, "about 3.9 Mb/s") {
		t.Errorf("higher target: %q", s)
	}
}
