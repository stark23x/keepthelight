package main

import (
	"bytes"
	"testing"
)

func TestMP4BoxDVOption(t *testing.T) {
	gpac2 := "\x1b[32m:dvp\x1b[0m,\x1b[32m-dv-profile\x1b[0m (string):     \x1b[33mS\x1b[0m set the Dolby Vision profile on imported track\n"
	if !mp4boxKnowsDVP(gpac2) {
		t.Error("GPAC 2 knows dvp")
	}
	if mp4boxKnowsDVP("Option -dvp unknown, please check usage.\n") || mp4boxKnowsDVP("") {
		t.Error("an older MP4Box must keep dv-profile")
	}
}

func TestDropLines(t *testing.T) {
	var out bytes.Buffer
	d := &dropLines{w: &out, drop: "Reordering metadata"}
	for _, chunk := range []string{"Parsing...\nReorder", "ing metadata... Done.\n", "Writing RPU\n"} {
		_, _ = d.Write([]byte(chunk))
	}
	if out.String() != "Parsing...\nWriting RPU\n" {
		t.Errorf("%q", out.String())
	}
}
