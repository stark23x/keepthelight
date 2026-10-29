package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// ---------- picture subtitles (PGS) to text (SRT) ----------
//
// Blu-ray subtitles (PGS) are pictures of text. FFmpeg copies the track out
// unchanged (a .sup file); Keep the Light reads its pictures and timings itself,
// and Tesseract (free OCR, installed separately) reads the text. The result is
// an SRT file with the original timings, muxed in place of (or next to) the
// picture track.

// pgsEvent is one subtitle on screen: from Start to End (seconds).
type pgsEvent struct {
	Start, End float64
	Img        *image.Gray // black text on white, ready for OCR
	Forced     bool
	Text       string
}

type pgsObject struct {
	w, h int
	data []byte // RLE, possibly gathered from several segments
	ver  byte
}

type pgsComp struct {
	obj     uint16
	x, y    int
	crop    bool
	cx, cy  int
	cw, ch  int
	forced  bool
	version byte
}

// parseSUP reads a .sup file (PGS segments) into on-screen events.
func parseSUP(data []byte) ([]pgsEvent, error) {
	var (
		events  []pgsEvent
		palette = map[byte][256]color.NRGBA{}
		objects = map[uint16]*pgsObject{}
		comps   []pgsComp
		palID   byte
		width   int
		height  int
		open    = -1    // index of the event on screen, -1 = none
		openKey = ""    // what is on screen, to tell a repeat from a new subtitle
		pts     float64 // of the current display set
		haveSet bool    // a composition arrived in this display set
		started bool    // seen an epoch start (or anything usable)
		segs    int
	)
	key := func() string {
		var b strings.Builder
		for _, c := range comps {
			o := objects[c.obj]
			ver := byte(0)
			if o != nil {
				ver = o.ver
			}
			fmt.Fprintf(&b, "%d.%d@%d,%d;", c.obj, ver, c.x, c.y)
		}
		return b.String()
	}
	for pos := 0; pos+13 <= len(data); {
		if data[pos] != 'P' || data[pos+1] != 'G' {
			return events, fmt.Errorf("not a PGS subtitle stream (bad segment at byte %d)", pos)
		}
		segPTS := float64(binary.BigEndian.Uint32(data[pos+2:])) / 90000
		typ := data[pos+10]
		size := int(binary.BigEndian.Uint16(data[pos+11:]))
		body := data[pos+13:]
		if len(body) < size {
			break // a cut-off last segment
		}
		body = body[:size]
		pos += 13 + size
		segs++
		switch typ {
		case 0x16: // presentation composition
			if len(body) < 11 {
				continue
			}
			width, height = int(binary.BigEndian.Uint16(body)), int(binary.BigEndian.Uint16(body[2:]))
			state := body[7]
			palID = body[9]
			n := int(body[10])
			if state&0x80 != 0 { // epoch start: everything is defined anew
				objects = map[uint16]*pgsObject{}
				palette = map[byte][256]color.NRGBA{}
			}
			started = true
			comps = comps[:0]
			p := body[11:]
			for i := 0; i < n && len(p) >= 8; i++ {
				c := pgsComp{obj: binary.BigEndian.Uint16(p), x: int(binary.BigEndian.Uint16(p[4:])), y: int(binary.BigEndian.Uint16(p[6:]))}
				c.crop, c.forced = p[3]&0x80 != 0, p[3]&0x40 != 0
				p = p[8:]
				if c.crop && len(p) >= 8 {
					c.cx, c.cy = int(binary.BigEndian.Uint16(p)), int(binary.BigEndian.Uint16(p[2:]))
					c.cw, c.ch = int(binary.BigEndian.Uint16(p[4:])), int(binary.BigEndian.Uint16(p[6:]))
					p = p[8:]
				}
				comps = append(comps, c)
			}
			pts, haveSet = segPTS, true
		case 0x14: // palette
			if len(body) < 2 {
				continue
			}
			pal := palette[body[0]]
			for p := body[2:]; len(p) >= 5; p = p[5:] {
				pal[p[0]] = ycbcrA(p[1], p[3], p[2], p[4]) // stored as Y, Cr, Cb, A
			}
			palette[body[0]] = pal
		case 0x15: // object (a picture, possibly in several parts)
			if len(body) < 4 {
				continue
			}
			id, ver, seq := binary.BigEndian.Uint16(body), body[2], body[3]
			p := body[4:]
			if seq&0x80 != 0 { // first part: length (3 bytes), width, height
				if len(p) < 7 {
					continue
				}
				o := &pgsObject{w: int(binary.BigEndian.Uint16(p[3:])), h: int(binary.BigEndian.Uint16(p[5:])), ver: ver}
				o.data = append([]byte(nil), p[7:]...)
				objects[id] = o
			} else if o := objects[id]; o != nil {
				o.data = append(o.data, p...)
			}
		case 0x17: // window: not needed (objects carry their positions)
		case 0x80: // end of display set: what is now on screen
			if !haveSet || !started {
				continue
			}
			haveSet = false
			k := key()
			if len(comps) > 0 && k == openKey && open >= 0 {
				continue // the same subtitle again (a repeat for seeking, or a palette fade)
			}
			if open >= 0 {
				events[open].End = pts
				open, openKey = -1, ""
			}
			if len(comps) == 0 {
				continue
			}
			img, forced := renderComps(comps, objects, palette[palID], width, height)
			if img == nil {
				continue
			}
			events = append(events, pgsEvent{Start: pts, End: -1, Img: img, Forced: forced})
			open, openKey = len(events)-1, k
		}
	}
	if segs == 0 {
		return nil, fmt.Errorf("the subtitle track is empty")
	}
	if open >= 0 { // still on screen at the end: a few seconds
		events[open].End = events[open].Start + 4
	}
	return events, nil
}

// ycbcrA converts a palette entry (limited-range BT.709 is usual for Blu-ray;
// only the brightness matters here).
func ycbcrA(y, cb, cr, a byte) color.NRGBA {
	r, g, b := color.YCbCrToRGB(y, cb, cr)
	return color.NRGBA{r, g, b, a}
}

// decodeRLE expands a PGS picture into palette indexes.
func decodeRLE(data []byte, w, h int) []byte {
	px := make([]byte, w*h)
	x, y := 0, 0
	put := func(n int, c byte) {
		for ; n > 0 && y < h; n-- {
			if x < w {
				px[y*w+x] = c
			}
			x++
		}
	}
	for i := 0; i < len(data) && y < h; {
		b := data[i]
		i++
		if b != 0 {
			put(1, b)
			continue
		}
		if i >= len(data) {
			break
		}
		f := data[i]
		i++
		switch {
		case f == 0: // end of line
			x, y = 0, y+1
		case f>>6 == 0:
			put(int(f&0x3f), 0)
		case f>>6 == 1:
			if i < len(data) {
				put(int(f&0x3f)<<8|int(data[i]), 0)
				i++
			}
		case f>>6 == 2:
			if i < len(data) {
				put(int(f&0x3f), data[i])
				i++
			}
		default:
			if i+1 < len(data) {
				put(int(f&0x3f)<<8|int(data[i]), data[i+1])
				i += 2
			}
		}
	}
	return px
}

// renderComps draws what is on screen as dark text on a white page, cropped to
// the text with a margin: the form OCR reads best. Bright, opaque pixels are
// the text; the dark outline and the transparent background become white.
func renderComps(comps []pgsComp, objects map[uint16]*pgsObject, pal [256]color.NRGBA, w, h int) (*image.Gray, bool) {
	if w <= 0 || h <= 0 || w > 8192 || h > 8192 {
		return nil, false
	}
	ink := make([]uint8, w*h) // 0 = nothing, 255 = full text
	minX, minY, maxX, maxY := w, h, -1, -1
	forced := false
	for _, c := range comps {
		o := objects[c.obj]
		if o == nil || o.w <= 0 || o.h <= 0 || o.w*o.h > 64<<20 {
			continue
		}
		forced = forced || c.forced
		px := decodeRLE(o.data, o.w, o.h)
		sx, sy, sw, sh := 0, 0, o.w, o.h
		if c.crop {
			sx, sy, sw, sh = c.cx, c.cy, c.cw, c.ch
		}
		for yy := 0; yy < sh; yy++ {
			for xx := 0; xx < sw; xx++ {
				ox, oy := sx+xx, sy+yy
				dx, dy := c.x+xx, c.y+yy
				if ox >= o.w || oy >= o.h || dx < 0 || dy < 0 || dx >= w || dy >= h {
					continue
				}
				p := pal[px[oy*o.w+ox]]
				lum := (299*int(p.R) + 587*int(p.G) + 114*int(p.B)) / 1000
				v := uint8(lum * int(p.A) / 255)
				if v < 96 { // outline, shadow and background
					continue
				}
				ink[dy*w+dx] = v
				minX, minY, maxX, maxY = min(minX, dx), min(minY, dy), max(maxX, dx), max(maxY, dy)
			}
		}
	}
	if maxX < 0 {
		return nil, forced
	}
	const pad = 16
	bw, bh := maxX-minX+1+2*pad, maxY-minY+1+2*pad
	img := image.NewGray(image.Rect(0, 0, bw, bh))
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			if v := ink[y*w+x]; v > 0 {
				img.Pix[(y-minY+pad)*bw+(x-minX+pad)] = 255 - v
			}
		}
	}
	return img, forced
}

// ---------- Tesseract ----------

// tesseractLangs: the 3-letter codes in files (ISO 639-2, often the "B" form)
// that Tesseract names differently.
var tesseractLangs = map[string]string{
	"fre": "fra", "ger": "deu", "dut": "nld", "cze": "ces", "slo": "slk", "rum": "ron", "gre": "ell", "chi": "chi_sim",
	"zho": "chi_sim", "per": "fas", "ice": "isl", "mac": "mkd", "may": "msa", "arm": "hye", "baq": "eus", "geo": "kat",
	"alb": "sqi", "bur": "mya", "wel": "cym", "tib": "bod", "nob": "nor", "nno": "nor",
}

var tessLangCache sync.Map // tesseract path -> map[string]bool

func tesseractHasLang(ctx context.Context, tess, lang string) bool {
	v, ok := tessLangCache.Load(tess)
	if !ok {
		m := map[string]bool{}
		if out, err := toolCommand(ctx, tess, "--list-langs").CombinedOutput(); err == nil {
			for _, l := range strings.Split(string(out), "\n") {
				m[strings.TrimSpace(l)] = true
			}
		}
		tessLangCache.Store(tess, m)
		v = m
	}
	return v.(map[string]bool)[lang]
}

// ocrLang picks Tesseract's language for a track ("eng" when unknown or not installed).
func ocrLang(ctx context.Context, tess, trackLang string) (lang, warn string) {
	l := strings.ToLower(strings.TrimSpace(trackLang))
	if t, ok := tesseractLangs[l]; ok {
		l = t
	}
	if l == "" || l == "und" {
		return "eng", ""
	}
	if tesseractHasLang(ctx, tess, l) {
		return l, ""
	}
	return "eng", fmt.Sprintf("Tesseract has no %s language data, so it reads these subtitles as English, which can garble accents and other letters. "+
		"To add it, run Tesseract's installer again and tick the language under \"Additional language data\" (%s)", langName(trackLang), l)
}

// ocrEvents reads the text of every event with one Tesseract run (a list of
// pictures, pages separated by form feeds); one run per picture if that fails.
func ocrEvents(ctx context.Context, tess, lang, dir string, events []pgsEvent) error {
	var list strings.Builder
	for i, e := range events {
		f := filepath.Join(dir, fmt.Sprintf("sub%05d.png", i))
		var b bytes.Buffer
		if err := png.Encode(&b, e.Img); err != nil {
			return err
		}
		if err := os.WriteFile(f, b.Bytes(), 0o644); err != nil {
			return err
		}
		list.WriteString(f + "\n")
	}
	lf := filepath.Join(dir, "pictures.txt")
	if err := os.WriteFile(lf, []byte(list.String()), 0o644); err != nil {
		return err
	}
	args := []string{"-l", lang, "--psm", "6", "-c", "page_separator=\f"}
	out, err := toolCommand(ctx, tess, append([]string{lf, "stdout"}, args...)...).Output()
	pages := strings.Split(string(out), "\f")
	if err == nil && len(pages) >= len(events) {
		for i := range events {
			events[i].Text = cleanOCR(pages[i])
		}
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	for i := range events { // one at a time
		f := filepath.Join(dir, fmt.Sprintf("sub%05d.png", i))
		o, err := toolCommand(ctx, tess, append([]string{f, "stdout"}, args...)...).Output()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("Tesseract could not read subtitle %d: %v", i+1, err)
		}
		events[i].Text = cleanOCR(string(o))
	}
	return nil
}

var (
	reOCRSpace = regexp.MustCompile(`[ \t]+`)
	reOCRPipe  = regexp.MustCompile(`(^|[\s"'(\-])\|(['’]|\s|$)`) // "|" standing for "I"
)

// cleanOCR tidies Tesseract's text: no empty lines, single spaces, and the
// commonest misreading of subtitle fonts ("|" for "I").
func cleanOCR(s string) string {
	var lines []string
	for _, l := range strings.Split(strings.ReplaceAll(s, "\r", ""), "\n") {
		l = strings.TrimSpace(reOCRSpace.ReplaceAllString(l, " "))
		if l == "" {
			continue
		}
		for i := 0; i < 2; i++ {
			l = reOCRPipe.ReplaceAllString(l, "${1}I${2}")
		}
		l = strings.ReplaceAll(l, "|", "I")
		lines = append(lines, l)
	}
	return strings.Join(lines, "\n")
}

// ---------- SRT ----------

func srtTime(s float64) string {
	ms := int64(s*1000 + 0.5)
	return fmt.Sprintf("%02d:%02d:%02d,%03d", ms/3600000, ms/60000%60, ms/1000%60, ms%1000)
}

// writeSRT writes the events that have text, in time order.
func writeSRT(path string, events []pgsEvent) (int, error) {
	sort.SliceStable(events, func(a, b int) bool { return events[a].Start < events[b].Start })
	var b strings.Builder
	n := 0
	for i, e := range events {
		if e.Text == "" {
			continue
		}
		end := e.End
		if end <= e.Start {
			end = e.Start + 4
		}
		if i+1 < len(events) && end > events[i+1].Start && events[i+1].Start > e.Start {
			end = events[i+1].Start
		}
		n++
		fmt.Fprintf(&b, "%d\n%s --> %s\n%s\n\n", n, srtTime(e.Start), srtTime(end), e.Text)
	}
	return n, os.WriteFile(path, []byte(b.String()), 0o644)
}

// ---------- in the job ----------

// subNeedsOCR: picture subtitles that are to be read as text.
func (s outSub) needsOCR() bool {
	return s.ToSRT && strings.EqualFold(s.Src.Codec, "hdmv_pgs_subtitle")
}

// prepareOCR copies the PGS tracks that are converted out of the source (one
// pass), reads them, and leaves an SRT file for each in tmp.
func prepareOCR(r *runner, cfg *Config, plan *Plan, tmp string) error {
	var idx []int
	for i, s := range plan.Subs {
		if s.needsOCR() {
			idx = append(idx, i)
		}
	}
	if len(idx) == 0 {
		return nil
	}
	tess, err := findTool("tesseract", cfg.Tesseract)
	if err != nil {
		return fmt.Errorf("converting picture subtitles (PGS) to SRT needs Tesseract OCR: %v. Install it (free) from https://github.com/UB-Mannheim/tesseract/wiki, or keep those subtitles as they are", err)
	}
	fmt.Printf("\nReading subtitles: copying %d picture subtitle track(s) out of the source\n", len(idx))
	args := []string{"-hide_banner", "-v", "error", "-y", "-i", cfg.Input}
	sup := map[int]string{} // source track -> file (a track converted twice is read once)
	for _, i := range idx {
		n := plan.Subs[i].Src.N
		if _, ok := sup[n]; ok {
			continue
		}
		sup[n] = filepath.Join(tmp, fmt.Sprintf("subs%d.sup", n))
		args = append(args, "-map", fmt.Sprintf("0:s:%d", n-1), "-c", "copy", "-f", "sup", sup[n])
	}
	if err := r.run(cfg.Ffmpeg, args...); err != nil {
		return fmt.Errorf("copying the picture subtitles out failed: %w", err)
	}
	srt := map[int]string{}
	for _, i := range idx {
		s := &plan.Subs[i]
		if f, ok := srt[s.Src.N]; ok {
			s.SRTFile = f
			continue
		}
		f := filepath.Join(tmp, fmt.Sprintf("subs%d.srt", s.Src.N))
		if r.dry {
			r.show(fmt.Sprintf("(read subtitle track %d with Tesseract into %s)", s.Src.N, f))
			s.SRTFile, srt[s.Src.N] = f, f
			continue
		}
		data, err := os.ReadFile(sup[s.Src.N])
		if err != nil {
			return err
		}
		events, err := parseSUP(data)
		if err != nil {
			return fmt.Errorf("subtitle track %d: %w", s.Src.N, err)
		}
		lang, warn := ocrLang(r.ctx, tess, s.Src.Lang)
		if warn != "" {
			fmt.Println("WARNING: subtitle track", s.Src.N, ":", warn)
		}
		fmt.Printf("Reading subtitles: track %d, %d subtitles (%s), with Tesseract\n", s.Src.N, len(events), lang)
		started := time.Now()
		dir, err := os.MkdirTemp(tmp, "ocr-")
		if err != nil {
			return err
		}
		if err := ocrEvents(r.ctx, tess, lang, dir, events); err != nil {
			return fmt.Errorf("reading subtitle track %d: %w", s.Src.N, err)
		}
		_ = os.RemoveAll(dir)
		n, err := writeSRT(f, events)
		if err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("no text was found in subtitle track %d; keep it as it is instead", s.Src.N)
		}
		fmt.Printf("Subtitle track %d: %d lines of text read in %s. Skim them once: OCR can mistake l / I / 1 and italics.\n",
			s.Src.N, n, time.Since(started).Round(time.Second))
		s.SRTFile, srt[s.Src.N] = f, f
	}
	return nil
}

// ocrInputs adds the SRT files as inputs, numbered from first.
func ocrInputs(subs []outSub, first int) []string {
	var a []string
	n := first
	for i := range subs {
		if subs[i].needsOCR() && subs[i].SRTFile != "" {
			a = append(a, "-i", subs[i].SRTFile)
			subs[i].input = n
			n++
		}
	}
	return a
}

// countInputs counts the -i in an argument list.
func countInputs(args []string) int {
	n := 0
	for _, a := range args {
		if a == "-i" {
			n++
		}
	}
	return n
}

func (p *Plan) needsOCR() bool {
	for _, s := range p.Subs {
		if s.needsOCR() {
			return true
		}
	}
	return false
}
