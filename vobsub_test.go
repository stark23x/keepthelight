package main

import (
	"context"
	"image"
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// vobSample makes a film with DVD subtitles: the PGS test pictures, turned
// into VobSub by FFmpeg. size: the frame ("720x480" for a DVD).
func vobSample(t *testing.T, size string) string { return vobSampleScaled(t, size, 1) }

// shrink makes a picture smaller by a whole factor (DVD-sized letters).
func shrink(g *image.Gray, f int) *image.Gray {
	b := g.Bounds()
	out := image.NewGray(image.Rect(0, 0, b.Dx()/f, b.Dy()/f))
	for y := 0; y < out.Rect.Dy(); y++ {
		for x := 0; x < out.Rect.Dx(); x++ {
			sum := 0
			for dy := 0; dy < f; dy++ {
				for dx := 0; dx < f; dx++ {
					sum += int(g.GrayAt(x*f+dx, y*f+dy).Y)
				}
			}
			out.SetGray(x, y, color.Gray{Y: uint8(sum / (f * f))})
		}
	}
	return out
}

func vobSampleScaled(t *testing.T, size string, f int) string {
	ffmpeg, e1 := exec.LookPath("ffmpeg")
	_, e2 := exec.LookPath("ffprobe")
	if e1 != nil || e2 != nil {
		t.Skip("needs ffmpeg")
	}
	dir := t.TempDir()
	sup := filepath.Join(dir, "s.sup")
	ev := testEvents(t, ffmpeg)
	if f > 1 {
		for i := range ev {
			ev[i].img = shrink(ev[i].img, f)
		}
	}
	_ = os.WriteFile(sup, writeSUP(ev, false), 0o644)
	out := filepath.Join(dir, "dvd.mkv")
	args := []string{"-v", "error", "-y", "-f", "lavfi", "-i", "testsrc=s=" + size + ":r=25:d=10", "-f", "sup", "-i", sup,
		"-map", "0", "-map", "1", "-c:v", "mpeg4", "-c:s", "dvdsub", "-metadata:s:s:0", "language=eng"}
	if b, err := exec.Command(ffmpeg, append(args, out)...).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, b)
	}
	return out
}

func TestReadVobSub(t *testing.T) {
	for _, f := range []int{1, 2, 3} { // as drawn, and with letters half the size (as on a DVD)
		t.Run(itoa(f), func(t *testing.T) { testReadVobSub(t, f) })
	}
}

func testReadVobSub(t *testing.T, f int) {
	in := vobSampleScaled(t, "720x480", f)
	ev, err := readVobSub(context.Background(), "ffprobe", in, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(ev) != 3 {
		t.Fatalf("%d events", len(ev))
	}
	// the sample's own times (FFmpeg moved them to start at 0, and its DVD subtitles have no end: each lasts until the next, at most 6 s)
	for i, want := range [][2]float64{{0, 3.25}, {3.25, 7}, {7, 13}} {
		if d := ev[i].Start - want[0]; d < -0.1 || d > 0.1 {
			t.Errorf("event %d starts at %.2f, want %.2f", i, ev[i].Start, want[0])
		}
		if d := ev[i].End - want[1]; d < -0.1 || d > 0.1 {
			t.Errorf("event %d ends at %.2f, want %.2f", i, ev[i].End, want[1])
		}
	}
	tess, err := exec.LookPath("tesseract")
	if err != nil {
		t.Skip("no tesseract")
	}
	if err := ocrEvents(context.Background(), tess, "eng", t.TempDir(), ev); err != nil {
		t.Fatal(err)
	}
	all := ev[0].Text + "|" + ev[1].Text + "|" + ev[2].Text
	for _, s := range []string{"Where did you put the keys?", "next to the lamp.", "I don't believe you."} {
		if !strings.Contains(all, s) {
			t.Errorf("OCR missed %q in %q", s, all)
		}
	}
}

func TestVobPaletteAndHex(t *testing.T) {
	b := hexBytes("\n00000000: 7061 6c65 7474 653a 2030 3030 3030 302c  palette: 000000,\n00000010: 2066 6666 6666 66                        ffffff\n")
	if string(b) != "palette: 000000, ffffff" {
		t.Errorf("hex: %q", b)
	}
	if _, ok := vobPalette("size: 720x480\npalette: 000000, ffffff"); ok {
		t.Error("a short palette was taken")
	}
	p, ok := vobPalette("palette: 000000, 0000ff, 00ff00, ff0000, ffff00, ff00ff, 00ffff, ffffff, 808000, 8080ff, 800080, 80ff80, 008080, ff8080, 555555, aaaaaa\n")
	if !ok || p[7] != [3]uint8{255, 255, 255} || p[1] != [3]uint8{0, 0, 255} {
		t.Errorf("palette: %v %v", p, ok)
	}
}

func TestVobSubTranscode(t *testing.T) {
	if _, err := exec.LookPath("tesseract"); err != nil {
		t.Skip("needs tesseract")
	}
	in := vobSampleScaled(t, "720x480", 2)
	cfg := baseCfg()
	cfg.Input, cfg.Output, cfg.CopyVideo = in, filepath.Join(t.TempDir(), "dvd_KTL.mkv"), true
	cfg.Ffmpeg, cfg.Ffprobe = "ffmpeg", "ffprobe"
	cfg.SubSel = []trackSel{{N: 1, SRT: "replace"}}
	if err := transcode(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	tr, err := probeTracks(context.Background(), "ffprobe", cfg.Output)
	if err != nil || len(tr.Subs) != 1 || tr.Subs[0].Codec != "subrip" || tr.Subs[0].Lang != "eng" {
		t.Fatalf("tracks: %+v %v", tr, err)
	}
	b, _ := exec.Command("ffmpeg", "-v", "error", "-i", cfg.Output, "-map", "0:s:0", "-f", "srt", "-").Output()
	if !strings.Contains(string(b), "Where did you put the keys?") || !strings.Contains(string(b), "00:00:03,250 -->") {
		t.Errorf("srt:\n%s", b)
	}
}
