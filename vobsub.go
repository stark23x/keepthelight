package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"math"
	"strconv"
	"strings"
)

// ---------- DVD picture subtitles (VobSub) to text ----------
//
// DVD subtitles are small four-colour pictures. Each packet holds the picture
// (two interlaced fields, run-length coded) and a few commands: when to show
// and hide it, its position, which four of the track's 16 palette colours it
// uses and how see-through each is. The palette is in the track's header
// (the ".idx" text: "palette: 000000, f0f0f0, …").
//
// FFprobe hands over every packet with its time (no decoding needed); Keep the
// Light draws each picture as black text on white, enlarges it (Tesseract reads
// DVD-sized text much better at 2–3×) and passes it on to the same OCR as PGS.

type vobPacket struct {
	PTS  float64
	Data []byte
}

// readVobSub reads subtitle track n (1-based among the subtitles) of a file.
func readVobSub(ctx context.Context, ffprobe, input string, n int) ([]pgsEvent, error) {
	out, err := toolCommand(ctx, ffprobe, "-v", "error", "-select_streams", fmt.Sprintf("s:%d", n-1),
		"-show_entries", "stream=width,height,extradata:packet=pts_time,data", "-show_data", "-of", "json", input).Output()
	if err != nil {
		return nil, fmt.Errorf("reading the DVD subtitles failed: %v", err)
	}
	var j struct {
		Streams []struct {
			Width, Height int
			Extradata     string `json:"extradata"`
		} `json:"streams"`
		Packets []struct {
			PTS  string `json:"pts_time"`
			Data string `json:"data"`
		} `json:"packets"`
	}
	if err := json.Unmarshal(out, &j); err != nil {
		return nil, fmt.Errorf("reading the DVD subtitles failed: %v", err)
	}
	var pal [16][3]uint8
	havePal := false
	if len(j.Streams) > 0 {
		pal, havePal = vobPalette(string(hexBytes(j.Streams[0].Extradata)))
	}
	var events []pgsEvent
	for _, p := range j.Packets {
		t, err := strconv.ParseFloat(p.PTS, 64)
		if err != nil {
			continue
		}
		if e, ok := decodeSPU(hexBytes(p.Data), pal, havePal); ok {
			e.Start += t
			if e.End > 0 {
				e.End += t
			}
			events = append(events, e)
		}
	}
	// a picture without its own end lasts until the next one (at most 6 s)
	for i := range events {
		if events[i].End > events[i].Start {
			continue
		}
		events[i].End = events[i].Start + 6
		if i+1 < len(events) && events[i+1].Start > events[i].Start && events[i+1].Start < events[i].End {
			events[i].End = events[i+1].Start
		}
	}
	return events, nil
}

// hexBytes reads FFprobe's hex dump ("00000000: 0123 4567 …  ascii").
func hexBytes(dump string) []byte {
	var out []byte
	for _, line := range strings.Split(dump, "\n") {
		i := strings.Index(line, ": ")
		if i < 0 {
			continue
		}
		hexPart := line[i+2:]
		if len(hexPart) > 40 {
			hexPart = hexPart[:40] // 8 groups of 4 digits and their spaces; the text after is the ascii column
		}
		b, err := hex.DecodeString(strings.ReplaceAll(strings.TrimSpace(hexPart), " ", ""))
		if err != nil {
			continue
		}
		out = append(out, b...)
	}
	return out
}

// vobPalette reads the 16 colours of the track's header ("palette: 000000, …").
func vobPalette(idx string) ([16][3]uint8, bool) {
	var pal [16][3]uint8
	for _, line := range strings.Split(idx, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok || !strings.EqualFold(strings.TrimSpace(k), "palette") {
			continue
		}
		n := 0
		for _, c := range strings.Split(v, ",") {
			c = strings.TrimSpace(c)
			x, err := strconv.ParseUint(c, 16, 32)
			if err != nil || len(c) != 6 || n >= 16 {
				continue
			}
			pal[n] = [3]uint8{uint8(x >> 16), uint8(x >> 8), uint8(x)}
			n++
		}
		return pal, n == 16
	}
	return pal, false
}

// spuTime: DVD subtitle times are in units of 1024/90000 s.
func spuTime(d int) float64 { return float64(d) * 1024 / 90000 }

// decodeSPU turns one subtitle packet into an event (times from the packet's
// start), drawn as black text on white.
func decodeSPU(b []byte, pal [16][3]uint8, havePal bool) (pgsEvent, bool) {
	var e pgsEvent
	if len(b) < 4 {
		return e, false
	}
	be := func(i int) int {
		if i+1 >= len(b) {
			return 0
		}
		return int(b[i])<<8 | int(b[i+1])
	}
	var colors, alphas [4]int
	alphas = [4]int{0, 15, 15, 15}
	colors = [4]int{0, 1, 2, 3}
	x1, x2, y1, y2 := -1, -1, -1, -1
	off1, off2 := -1, -1
	start, stop := -1.0, -1.0
	seq := be(2)
	for guard := 0; seq+4 <= len(b) && guard < 64; guard++ {
		date, next := be(seq), be(seq+2)
		i := seq + 4
	cmds:
		for i < len(b) {
			c := b[i]
			i++
			switch c {
			case 0x00: // forced start
				e.Forced = true
				start = spuTime(date)
			case 0x01:
				start = spuTime(date)
			case 0x02:
				stop = spuTime(date)
			case 0x03:
				if i+2 > len(b) {
					break cmds
				}
				colors = [4]int{int(b[i+1] & 0xf), int(b[i+1] >> 4), int(b[i] & 0xf), int(b[i] >> 4)}
				i += 2
			case 0x04:
				if i+2 > len(b) {
					break cmds
				}
				alphas = [4]int{int(b[i+1] & 0xf), int(b[i+1] >> 4), int(b[i] & 0xf), int(b[i] >> 4)}
				i += 2
			case 0x05:
				if i+6 > len(b) {
					break cmds
				}
				x1, x2 = int(b[i])<<4|int(b[i+1])>>4, int(b[i+1]&0xf)<<8|int(b[i+2])
				y1, y2 = int(b[i+3])<<4|int(b[i+4])>>4, int(b[i+4]&0xf)<<8|int(b[i+5])
				i += 6
			case 0x06:
				if i+4 > len(b) {
					break cmds
				}
				off1, off2 = be(i), be(i+2)
				i += 4
			default: // 0xff (end) or something unknown
				break cmds
			}
		}
		if next <= seq || next == seq {
			break
		}
		seq = next
	}
	w, h := x2-x1+1, y2-y1+1
	if start < 0 || w <= 0 || h <= 0 || w > 4096 || h > 4096 || off1 < 0 || off2 < 0 {
		return e, false
	}
	// the picture: colour index (0–3) per pixel
	px := make([]uint8, w*h)
	for field, off := range []int{off1, off2} {
		pos := off * 2 // in nibbles
		nib := func() int {
			if pos/2 >= len(b) {
				pos++
				return 0
			}
			v := b[pos/2]
			if pos%2 == 0 {
				v >>= 4
			}
			pos++
			return int(v & 0xf)
		}
		for y := field; y < h; y += 2 {
			for x := 0; x < w; {
				v := nib()
				if v < 0x4 {
					v = v<<4 | nib()
					if v < 0x10 {
						v = v<<4 | nib()
						if v < 0x40 {
							v = v<<4 | nib()
						}
					}
				}
				run, col := v>>2, uint8(v&3)
				if run == 0 || x+run > w {
					run = w - x
				}
				for k := 0; k < run; k++ {
					px[y*w+x+k] = col
				}
				x += run
			}
			if pos%2 == 1 { // each line starts on a whole byte
				pos++
			}
			if pos/2 > len(b) {
				break
			}
		}
	}
	text := spuTextColour(px, colors, alphas, pal, havePal)
	if text < 0 {
		return e, false
	}
	e.Img = spuImage(px, w, h, text)
	if e.Img == nil {
		return e, false
	}
	if stop <= start || stop-start > 60 { // none, or "until further notice" (FFmpeg writes the longest time there is)
		stop = 0
	}
	e.Start, e.End = start, stop
	return e, true
}

// spuTextColour picks which of the four colours is the text: of those that
// can be seen and cover a fair share of the picture, the brightest (subtitles
// are light letters with a dark edge). Without a palette, colour 1 (the usual
// "pattern" colour).
func spuTextColour(px []uint8, colors, alphas [4]int, pal [16][3]uint8, havePal bool) int {
	var count [4]int
	for _, c := range px {
		count[c]++
	}
	seen := 0
	for k := 0; k < 4; k++ {
		if alphas[k] > 0 {
			seen += count[k]
		}
	}
	if seen == 0 {
		return -1
	}
	if !havePal {
		if alphas[1] > 0 && count[1] > 0 {
			return 1
		}
	}
	best, bestL := -1, -1.0
	for k := 0; k < 4; k++ {
		if alphas[k] == 0 || count[k]*20 < seen { // invisible, or a few stray pixels
			continue
		}
		c := pal[colors[k]]
		l := 0.299*float64(c[0]) + 0.587*float64(c[1]) + 0.114*float64(c[2])
		if !havePal {
			l = float64(k) // no colours known: any visible one will do
		}
		if l > bestL {
			best, bestL = k, l
		}
	}
	return best
}

// spuImage draws the text colour black on white, trimmed to the text with a
// margin, and enlarged when the letters are small.
func spuImage(px []uint8, w, h, text int) *image.Gray {
	minX, minY, maxX, maxY := w, h, -1, -1
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if int(px[y*w+x]) == text {
				minX, minY = min(minX, x), min(minY, y)
				maxX, maxY = max(maxX, x), max(maxY, y)
			}
		}
	}
	if maxX < 0 {
		return nil
	}
	// enlarge small text (DVD subtitles are often only 15–25 pixels tall): judged by the first line's height
	lh := 0
	for y := minY; y <= maxY; y++ {
		ink := false
		for x := minX; x <= maxX && !ink; x++ {
			ink = int(px[y*w+x]) == text
		}
		if !ink && lh > 0 {
			break
		}
		if ink {
			lh++
		}
	}
	scale := 1
	switch {
	case lh < 18:
		scale = 3
	case lh < 30:
		scale = 2
	}
	const pad = 12
	cw, ch := maxX-minX+1, maxY-minY+1
	ink := func(x, y int) float64 { // 1 inside the letters, 0 outside (and beyond the edges)
		if x < 0 || y < 0 || x >= cw || y >= ch || int(px[(y+minY)*w+x+minX]) != text {
			return 0
		}
		return 1
	}
	img := image.NewGray(image.Rect(0, 0, cw*scale+2*pad, ch*scale+2*pad))
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	// enlarged smoothly (bilinear), so the letters keep round edges instead of steps
	for Y := 0; Y < ch*scale; Y++ {
		fy := (float64(Y)+0.5)/float64(scale) - 0.5
		y0 := int(math.Floor(fy))
		ty := fy - float64(y0)
		for X := 0; X < cw*scale; X++ {
			fx := (float64(X)+0.5)/float64(scale) - 0.5
			x0 := int(math.Floor(fx))
			tx := fx - float64(x0)
			v := (ink(x0, y0)*(1-tx)+ink(x0+1, y0)*tx)*(1-ty) + (ink(x0, y0+1)*(1-tx)+ink(x0+1, y0+1)*tx)*ty
			img.Pix[(pad+Y)*img.Stride+pad+X] = uint8(255 - math.Round(v*255))
		}
	}
	return img
}
