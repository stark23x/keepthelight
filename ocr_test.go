package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ---- a small PGS writer, to make test subtitles ----

type supEvent struct {
	start, end float64
	lines      []string // rendered with ffmpeg's drawtext
	forced     bool
	img        *image.Gray // white text on black
}

func supSeg(b *bytes.Buffer, pts float64, typ byte, body []byte) {
	h := make([]byte, 13)
	h[0], h[1] = 'P', 'G'
	binary.BigEndian.PutUint32(h[2:], uint32(pts*90000))
	h[10] = typ
	binary.BigEndian.PutUint16(h[11:], uint16(len(body)))
	b.Write(h)
	b.Write(body)
}

func rleEncode(img *image.Gray) []byte {
	var out []byte
	w, h := img.Rect.Dx(), img.Rect.Dy()
	idx := func(x, y int) byte {
		if img.Pix[y*img.Stride+x] > 128 {
			return 1
		}
		return 0
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; {
			c := idx(x, y)
			n := 1
			for x+n < w && idx(x+n, y) == c && n < 16000 {
				n++
			}
			switch {
			case c != 0 && n < 3:
				for i := 0; i < n; i++ {
					out = append(out, c)
				}
			case c == 0 && n < 64:
				out = append(out, 0, byte(n))
			case c == 0:
				out = append(out, 0, 0x40|byte(n>>8), byte(n))
			case n < 64:
				out = append(out, 0, 0x80|byte(n), c)
			default:
				out = append(out, 0, 0xC0|byte(n>>8), byte(n), c)
			}
			x += n
		}
		out = append(out, 0, 0)
	}
	return out
}

func writeSUP(events []supEvent, split bool) []byte {
	var b bytes.Buffer
	for i, e := range events {
		w, h := e.img.Rect.Dx(), e.img.Rect.Dy()
		pcs := []byte{0x07, 0x80, 0x04, 0x38, 0x10, 0, byte(i), 0x80, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0}
		if e.forced {
			pcs[14] = 0x40
		}
		binary.BigEndian.PutUint16(pcs[15:], uint16(100))
		binary.BigEndian.PutUint16(pcs[17:], uint16(900))
		supSeg(&b, e.start, 0x16, pcs)
		supSeg(&b, e.start, 0x17, []byte{1, 0, 0, 100 >> 8, 100, 900 >> 8, 900 & 0xff, byte(w >> 8), byte(w), byte(h >> 8), byte(h)})
		// palette: 0 transparent, 1 white (Y=235)
		supSeg(&b, e.start, 0x14, []byte{0, 0, 0, 16, 128, 128, 0, 1, 235, 128, 128, 255})
		data := rleEncode(e.img)
		l := len(data) + 4
		head := []byte{0, 0, 0, 0, byte(l >> 16), byte(l >> 8), byte(l), byte(w >> 8), byte(w), byte(h >> 8), byte(h)}
		if split && len(data) > 10 { // the picture in two parts
			half := len(data) / 2
			head[3] = 0x80
			supSeg(&b, e.start, 0x15, append(head, data[:half]...))
			supSeg(&b, e.start, 0x15, append([]byte{0, 0, 0, 0x40}, data[half:]...))
		} else {
			head[3] = 0xC0
			supSeg(&b, e.start, 0x15, append(head, data...))
		}
		supSeg(&b, e.start, 0x80, nil)
		// a repeat of the same subtitle (an acquisition point): not a new one
		rep := append([]byte(nil), pcs...)
		rep[7] = 0x40
		supSeg(&b, e.start+0.5, 0x16, rep)
		supSeg(&b, e.start+0.5, 0x80, nil)
		// cleared
		supSeg(&b, e.end, 0x16, []byte{0x07, 0x80, 0x04, 0x38, 0x10, 0, byte(i), 0, 0, 0, 0})
		supSeg(&b, e.end, 0x80, nil)
	}
	return b.Bytes()
}

func renderText(t *testing.T, ffmpeg string, lines []string) *image.Gray {
	dir := t.TempDir()
	out := filepath.Join(dir, "t.png")
	var vf []string
	for i, l := range lines {
		tf := filepath.Join(dir, "line"+itoa(i)+".txt")
		_ = os.WriteFile(tf, []byte(l), 0o644)
		vf = append(vf, "drawtext=textfile="+tf+":fontcolor=white:fontsize=44:x=20:y="+itoa(10+i*56))
	}
	if b, err := exec.Command(ffmpeg, "-v", "error", "-y", "-f", "lavfi", "-i", "color=black:s=1100x"+itoa(20+56*len(lines)),
		"-vf", strings.Join(vf, ","), "-frames:v", "1", "-pix_fmt", "gray", out).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, b)
	}
	f, _ := os.Open(out)
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	g, ok := img.(*image.Gray)
	if !ok {
		t.Fatalf("%T", img)
	}
	return g
}

func testEvents(t *testing.T, ffmpeg string) []supEvent {
	ev := []supEvent{
		{start: 1.0, end: 3.5, lines: []string{"Where did you put the keys?"}},
		{start: 4.25, end: 7, lines: []string{"I left them on the table,", "next to the lamp."}, forced: true},
		{start: 8, end: 9.5, lines: []string{"I don't believe you."}},
	}
	for i := range ev {
		ev[i].img = renderText(t, ffmpeg, ev[i].lines)
	}
	return ev
}

func TestParseSUPAndOCR(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("needs ffmpeg")
	}
	ev := testEvents(t, ffmpeg)
	for _, split := range []bool{false, true} {
		got, err := parseSUP(writeSUP(ev, split))
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 3 {
			t.Fatalf("split=%v: %d events", split, len(got))
		}
		for i, e := range got {
			if e.Start != ev[i].start || e.End != ev[i].end || e.Forced != ev[i].forced || e.Img == nil {
				t.Errorf("event %d: %+v", i, e)
			}
		}
		// dark text on white, cropped with a margin
		img := got[0].Img
		if img.Pix[0] != 255 || img.Rect.Dx() > 1000 || img.Rect.Dy() > 90 {
			t.Errorf("image %v corner %d", img.Rect, img.Pix[0])
		}
	}
	tess, err := exec.LookPath("tesseract")
	if err != nil {
		t.Skip("needs tesseract")
	}
	got, _ := parseSUP(writeSUP(ev, false))
	if err := ocrEvents(context.Background(), tess, "eng", t.TempDir(), got); err != nil {
		t.Fatal(err)
	}
	for i, e := range got {
		if want := strings.Join(ev[i].lines, "\n"); e.Text != want {
			t.Errorf("OCR %d: %q, want %q", i, e.Text, want)
		}
	}
	f := filepath.Join(t.TempDir(), "x.srt")
	if n, err := writeSRT(f, got); err != nil || n != 3 {
		t.Fatal(n, err)
	}
	b, _ := os.ReadFile(f)
	if !strings.HasPrefix(string(b), "1\n00:00:01,000 --> 00:00:03,500\nWhere did you put the keys?\n\n2\n00:00:04,250 --> 00:00:07,000\nI left them") {
		t.Errorf("srt:\n%s", b)
	}
	if l, w := ocrLang(context.Background(), tess, "fre"); l != "fra" && (l != "eng" || w == "") {
		t.Errorf("fre: %s %s", l, w)
	}
	if l, _ := ocrLang(context.Background(), tess, ""); l != "eng" {
		t.Error(l)
	}
}

func TestCleanOCR(t *testing.T) {
	for in, want := range map[string]string{
		"  | don't  know.\n\n|'m here \n": "I don't know.\nI'm here",
		"He said \"| will\"":              "He said \"I will\"",
	} {
		if got := cleanOCR(in); got != want {
			t.Errorf("%q: %q", in, got)
		}
	}
}

func TestConvertSubsSelection(t *testing.T) {
	tr := &Tracks{Subs: []srcTrack{
		{N: 1, Codec: "ass", Lang: "eng", Title: "Signs", Disp: map[string]bool{"default": true}},
		{N: 2, Codec: "hdmv_pgs_subtitle", Lang: "eng", Disp: map[string]bool{"forced": true}},
		{N: 3, Codec: "subrip", Lang: "fre"},
		{N: 4, Codec: "dvd_subtitle", Lang: "ger"},
	}}
	cfg := baseCfg()
	cfg.SubSel = []trackSel{{N: 1, SRT: "add"}, {N: 2, Forced: true, SRT: "replace"}, {N: 3, SRT: "replace"}, {N: 4, SRT: "replace"}}
	_, subs, warns, err := selectTracks(cfg, tr, false)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, s := range subs {
		got = append(got, itoa(s.Src.N)+":"+s.Codec+map[bool]string{true: "+"}[s.Added]+map[bool]string{true: "*"}[s.Default])
	}
	if strings.Join(got, " ") != "1:copy* 1:srt+ 2:srt 3:copy 4:copy" {
		t.Errorf("mkv: %v", got)
	}
	if !strings.Contains(strings.Join(warns, "|"), "track 4 (VobSub") && !strings.Contains(strings.Join(warns, "|"), "subtitle track 4") {
		t.Errorf("warns: %v", warns)
	}
	if subs[1].Title != "Signs (SRT)" || !subs[2].needsOCR() || subs[1].needsOCR() {
		t.Errorf("%+v", subs)
	}
	subs[2].SRTFile, subs[2].input = "/x.srt", 3
	a := strings.Join(subtitleArgs(subs, 0), " ")
	for _, want := range []string{"-map 0:s:0 -c:s:1 srt", "-metadata:s:s:1 title=Signs (SRT)", "-map 3:s:0 -c:s:2 srt", "-metadata:s:s:2 language=eng",
		"-disposition:s:2 forced"} {
		if !strings.Contains(a, want) {
			t.Errorf("missing %q in %s", want, a)
		}
	}
	ch := subChoice(subs)["tracks"].([]map[string]any)
	if len(ch) != 4 || ch[0]["srt"] != "add" || ch[1]["srt"] != "replace" || ch[2]["srt"] != nil {
		t.Errorf("choice: %v", ch)
	}

	// MP4: "add" converts the track itself; the rule applies when tracks are not chosen one by one
	cfg = baseCfg()
	cfg.SubsToSRT = "add"
	_, subs, _, _ = selectTracks(cfg, tr, true)
	got = nil
	for _, s := range subs {
		got = append(got, itoa(s.Src.N)+":"+s.Codec)
	}
	if strings.Join(got, " ") != "1:mov_text 2:mov_text 3:mov_text" {
		t.Errorf("mp4 rule: %v", got)
	}
	// metadata only: nothing is converted
	cfg.MetaOnly = true
	if _, subs, _, _ = selectTracks(cfg, tr, false); len(subs) != 4 || subs[0].ToSRT {
		t.Errorf("metadata only: %+v", subs)
	}
	if sel, err := parseSubSel("1:srt+,2:forced:srt,3"); err != nil || sel[0].SRT != "add" || !sel[1].Forced || sel[1].SRT != "replace" || subSelString(sel) != "1:srt+,2:forced:srt,3" {
		t.Errorf("%+v %v", sel, err)
	}
	if _, err := parseSubSel("1:srt:srt"); err == nil {
		t.Error("bad accepted")
	}
}

// A whole job: an MKV with a PGS and an ASS track, both converted.
func TestOCRTranscode(t *testing.T) {
	ffmpeg, e1 := exec.LookPath("ffmpeg")
	_, e2 := exec.LookPath("ffprobe")
	_, e3 := exec.LookPath("tesseract")
	if e1 != nil || e2 != nil || e3 != nil {
		t.Skip("needs ffmpeg and tesseract")
	}
	dir := t.TempDir()
	sup := filepath.Join(dir, "s.sup")
	_ = os.WriteFile(sup, writeSUP(testEvents(t, ffmpeg), false), 0o644)
	ass := filepath.Join(dir, "s.ass")
	_ = os.WriteFile(ass, []byte("[Script Info]\nScriptType: v4.00+\n\n[V4+ Styles]\nFormat: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding\nStyle: Default,Arial,20,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,2,0,2,10,10,10,1\n\n[Events]\nFormat: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\nDialogue: 0,0:00:01.00,0:00:03.00,Default,,0,0,0,,{\\i1}Hello{\\i0} there\n"), 0o644)
	in := filepath.Join(dir, "film.mkv")
	if b, err := exec.Command(ffmpeg, "-v", "error", "-y", "-f", "lavfi", "-i", "testsrc=s=320x180:r=24:d=10", "-f", "sup", "-i", sup, "-i", ass,
		"-map", "0", "-map", "1", "-map", "2", "-c:v", "mpeg4", "-c:s", "copy", "-metadata:s:s:0", "language=eng", "-metadata:s:s:1", "language=eng", in).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, b)
	}
	if keep := os.Getenv("KTL_KEEP_SAMPLE"); keep != "" { // a sample for trying the window
		b, _ := os.ReadFile(in)
		_ = os.WriteFile(keep, b, 0o644)
	}
	cfg := baseCfg()
	cfg.Input, cfg.Output, cfg.CopyVideo = in, filepath.Join(dir, "film_KTL.mkv"), true
	cfg.Ffmpeg, cfg.Ffprobe = ffmpeg, "ffprobe"
	cfg.SubSel = []trackSel{{N: 1, SRT: "add"}, {N: 2, SRT: "replace"}}
	if err := transcode(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	tr, err := probeTracks(context.Background(), "ffprobe", cfg.Output)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, s := range tr.Subs {
		got = append(got, s.Codec+"/"+s.Lang+"/"+s.Title)
	}
	if strings.Join(got, " ") != "hdmv_pgs_subtitle/eng/ subrip/eng/SRT subrip/eng/" {
		t.Errorf("tracks: %v", got)
	}
	b, _ := exec.Command(ffmpeg, "-v", "error", "-i", cfg.Output, "-map", "0:s:1", "-f", "srt", "-").Output()
	if !strings.Contains(string(b), "Where did you put the keys?") || !strings.Contains(string(b), "next to the lamp.") {
		t.Errorf("ocr track:\n%s", b)
	}
	b, _ = exec.Command(ffmpeg, "-v", "error", "-i", cfg.Output, "-map", "0:s:2", "-f", "srt", "-").Output()
	if !strings.Contains(string(b), "Hello") || strings.Contains(string(b), "\\i1") {
		t.Errorf("ass track:\n%s", b)
	}
}
