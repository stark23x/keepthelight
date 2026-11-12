package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The library scan reads every video in a folder (only the file information,
// nothing is converted) and shows what is worth converting, how much space it
// would save, and which files are likely to make Plex convert on the fly.

type scanItem struct {
	Path     string   `json:"path"`
	Name     string   `json:"name"`
	Folder   string   `json:"folder"` // relative to the scanned folder
	Size     int64    `json:"size"`
	Duration float64  `json:"duration"`
	Video    string   `json:"video"` // "HEVC 3840×2160 HDR10"
	Kbps     int      `json:"kbps"`  // the whole file's average bitrate
	Audio    string   `json:"audio"` // "TrueHD 7.1, AC-3 5.1"
	EstSize  int64    `json:"estSize"`
	Saving   int64    `json:"saving"`
	Worth    bool     `json:"worth"`
	Issues   []string `json:"issues"`
	Err      string   `json:"err,omitempty"`
	Done     string   `json:"done,omitempty"`    // already converted by Keep the Light: its tag
	Gap      float64  `json:"gap,omitempty"`     // picture and sound stored this far apart (s): fixable by a remux
	Fix      string   `json:"fix,omitempty"`     // the remux: "running", "done: …" or "failed: …"
	Mtime    int64    `json:"mtime,omitempty"`   // when the file was last changed (Unix ms), to tell later whether it still is the same
	Gone     bool     `json:"gone,omitempty"`    // saved results: the file is no longer there
	Changed  bool     `json:"changed,omitempty"` // saved results: the file has changed since the scan

	height     int    // for the interlacing check
	fieldOrder string // what the file says
}

type scanState struct {
	mu        sync.Mutex
	running   bool
	dir       string
	found     int // video files found so far
	done      int // read so far
	listing   bool
	extras    int // Plex extras left out
	items     []*scanItem
	cancel    context.CancelFunc
	startedAt time.Time
	finished  time.Time // when the results were made (shown as "Scanned on …")
	saved     bool      // the results were read back from the last scan
	stopped   bool      // the scan was stopped before the end
	checking  bool      // saved results: their files are being looked at again
	rev       int       // goes up when rows already sent change (the page then reloads them)
	opts      scanOpts
}

// scanOpts are the choices a scan ran with, kept with its results.
type scanOpts struct {
	Recursive  bool   `json:"recursive"`
	Tag        string `json:"tag,omitempty"`
	SkipExtras bool   `json:"skipExtras"`
}

// typicalHEVC is a bitrate (bits/s) that looks very good with x265 at
// constant quality around 18-20, for films at this height: the "after" of the
// estimate. Real results vary with grain and content; a test shows the truth.
func typicalHEVC(height int, hdr bool) float64 {
	var b float64
	switch {
	case height >= 2000:
		b = 14e6
	case height >= 1400:
		b = 8e6
	case height >= 1000:
		b = 5e6
	case height >= 700:
		b = 3e6
	default:
		b = 1.8e6
	}
	if hdr {
		b *= 1.15
	}
	return b
}

type scanStream struct {
	Type       string            `json:"codec_type"`
	Codec      string            `json:"codec_name"`
	Profile    string            `json:"profile"`
	Width      int               `json:"width"`
	Height     int               `json:"height"`
	Channels   int               `json:"channels"`
	Layout     string            `json:"channel_layout"`
	BitRate    string            `json:"bit_rate"`
	PixFmt     string            `json:"pix_fmt"`
	Transfer   string            `json:"color_transfer"`
	FieldOrder string            `json:"field_order"`
	Tags       map[string]string `json:"tags"`
	Disp       map[string]int    `json:"disposition"`
	SideData   []map[string]any  `json:"side_data_list"`
}

// scanProbe is ffprobe's information for one file (streams and format only:
// quick, even over the network).
type scanProbe struct {
	Streams []scanStream `json:"streams"`
	Format  struct {
		Duration string            `json:"duration"`
		Size     string            `json:"size"`
		BitRate  string            `json:"bit_rate"`
		Tags     map[string]string `json:"tags"`
	} `json:"format"`
}

// analyse turns ffprobe's information into a scan line: what the file is, an
// estimate of its size as HEVC, and the things likely to make Plex convert.
func analyse(it *scanItem, data []byte) {
	var p scanProbe
	if err := json.Unmarshal(data, &p); err != nil {
		it.Err = "could not read this file's information"
		return
	}
	it.Duration, _ = strconv.ParseFloat(p.Format.Duration, 64)
	if sz, err := strconv.ParseInt(p.Format.Size, 10, 64); err == nil && sz > 0 {
		it.Size = sz
	}
	total, _ := strconv.ParseFloat(p.Format.BitRate, 64)
	if total == 0 && it.Duration > 0 {
		total = float64(it.Size) * 8 / it.Duration
	}
	it.Kbps = int(total / 1000)

	bps := func(s scanStream) float64 {
		v, _ := strconv.ParseFloat(s.BitRate, 64)
		for k, t := range s.Tags {
			if v == 0 && strings.HasPrefix(strings.ToUpper(k), "BPS") {
				v, _ = strconv.ParseFloat(strings.TrimSpace(t), 64)
			}
		}
		return v
	}

	var vCodec, hdr string
	var w, h int
	var videoBps, audioBps float64
	var audios []string
	dvProfile := 0
	for _, s := range p.Streams {
		switch s.Type {
		case "video":
			if s.Disp["attached_pic"] == 1 || vCodec != "" {
				continue
			}
			vCodec, w, h, videoBps = s.Codec, s.Width, s.Height, bps(s)
			it.height, it.fieldOrder = s.Height, s.FieldOrder
			switch s.Transfer {
			case "smpte2084":
				hdr = "HDR10"
			case "arib-std-b67":
				hdr = "HLG"
			}
			for _, sd := range s.SideData {
				if t, _ := sd["side_data_type"].(string); strings.Contains(strings.ToLower(t), "dovi") {
					if v, ok := sd["dv_profile"].(float64); ok {
						dvProfile = int(v)
					}
				}
			}
		case "audio":
			audioBps += bps(s)
			t := srcTrack{Codec: s.Codec, Profile: s.Profile, Channels: s.Channels, Layout: s.Layout}
			audios = append(audios, strings.TrimSpace(codecName(t)+" "+layoutName(t)))
		}
	}
	if vCodec == "" {
		it.Err = "no video"
		return
	}
	if dvProfile > 0 {
		hdr = fmt.Sprintf("Dolby Vision %d", dvProfile)
	}
	for _, x := range plexIssues(plexSrcFromScan(&p, it.Kbps)) { // the same check as when files are added
		it.Issues = append(it.Issues, x.Text)
	}
	it.Video = strings.TrimSpace(fmt.Sprintf("%s %d×%d %s", codecDisplay(vCodec), w, h, hdr))
	if len(audios) > 4 {
		audios = append(audios[:4], fmt.Sprintf("+%d more", len(audios)-4))
	}
	it.Audio = strings.Join(audios, ", ")

	// the estimate: the video as HEVC at very good quality; everything else as it is
	if videoBps == 0 { // not recorded: the file's bitrate less the audio (or a share of it)
		videoBps = total - audioBps
		if audioBps == 0 {
			videoBps = total * 0.85
		}
	}
	if it.Done = ktlTagIn(p.Format.Tags); it.Done != "" {
		return // already converted: no saving is expected from doing it again
	}
	if it.Duration <= 0 || videoBps <= 0 || it.Size <= 0 {
		return
	}
	after := math.Min(videoBps*codecFactor(vCodec), typicalHEVC(h, hdr != ""))
	videoBytes := math.Min(videoBps*it.Duration/8, float64(it.Size))
	it.EstSize = it.Size - int64(videoBytes) + int64(after*it.Duration/8)
	it.Saving = it.Size - it.EstSize
	it.Worth = it.Saving >= 300<<20 && float64(it.Saving) >= 0.25*float64(it.Size) // at least 300 MB and a quarter
}

func appendOnce(list []string, s string) []string {
	for _, x := range list {
		if x == s {
			return list
		}
	}
	return append(list, s)
}

func codecDisplay(c string) string {
	if n, ok := map[string]string{"hevc": "HEVC", "h264": "H.264", "av1": "AV1", "vp9": "VP9", "mpeg2video": "MPEG-2", "vc1": "VC-1", "mpeg4": "MPEG-4"}[c]; ok {
		return n
	}
	return strings.ToUpper(c)
}

// ---------- running a scan ----------

// Plex's extras: files in these folders, or whose name ends in these words
// (https://support.plex.tv/articles/local-files-for-trailers-and-extras/), and
// Plex's own optimised copies ("Plex Versions").
var (
	plexExtraDirs = map[string]bool{"behind the scenes": true, "deleted scenes": true, "featurettes": true, "interviews": true,
		"scenes": true, "shorts": true, "trailers": true, "other": true, "plex versions": true}
	plexExtraSuffixes = []string{"-behindthescenes", "-deleted", "-featurette", "-interview", "-scene", "-short", "-trailer", "-other"}
)

// isPlexExtra: Plex treats the file (relative to the scanned folder) as an extra.
func isPlexExtra(rel string) bool {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for _, d := range parts[:len(parts)-1] {
		if plexExtraDirs[strings.ToLower(strings.TrimSpace(d))] {
			return true
		}
	}
	name := parts[len(parts)-1]
	stem := strings.ToLower(strings.TrimSuffix(name, filepath.Ext(name)))
	for _, s := range plexExtraSuffixes {
		if strings.HasSuffix(stem, s) {
			return true
		}
	}
	return false
}

func (g *guiServer) scanStart(dir string, recursive bool, tag string, skipExtras bool) map[string]any {
	st, err := os.Stat(dir)
	if err != nil || !st.IsDir() {
		return failResult("That folder is not available: " + dir)
	}
	ffprobe, err := findTool("ffprobe", "")
	if err != nil {
		return failResult("ffprobe was not found")
	}
	ffmpeg, _ := findTool("ffmpeg", "") // for the interlacing check
	s := &g.scan
	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.running, s.dir, s.found, s.done, s.items, s.cancel, s.listing, s.startedAt = true, dir, 0, 0, nil, cancel, true, time.Now()
	s.extras, s.saved, s.stopped, s.checking, s.opts = 0, false, false, false, scanOpts{recursive, tag, skipExtras}
	s.rev++
	s.mu.Unlock()

	go func() {
		type found struct {
			path string
			own  bool // a file Keep the Light made: shown only when it needs fixing
		}
		files := make(chan found, 64)
		go func() { // find the videos
			defer close(files)
			walk := func(path string, d os.DirEntry, err error) error {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				if err != nil {
					return nil // an unreadable folder: skip it
				}
				if d.IsDir() {
					n := d.Name()
					if path != dir && (!recursive || strings.HasPrefix(n, ".") || strings.HasPrefix(n, "$")) {
						return filepath.SkipDir
					}
					return nil
				}
				n := d.Name()
				stem := strings.TrimSuffix(n, filepath.Ext(n))
				if !videoExts[strings.ToLower(filepath.Ext(n))] || strings.HasSuffix(stem, ".partial") || strings.Contains(stem, " s test)") ||
					strings.Contains(stem, " s original)") || strings.HasPrefix(n, ".keepthelight") {
					return nil
				}
				own := isOwnOutput(stem, tag)
				if skipExtras {
					if rel, err := filepath.Rel(dir, path); err == nil && isPlexExtra(rel) {
						s.mu.Lock()
						s.extras++
						s.mu.Unlock()
						return nil
					}
				}
				if !own {
					s.mu.Lock()
					s.found++
					s.mu.Unlock()
				}
				files <- found{path, own}
				return nil
			}
			_ = filepath.WalkDir(dir, walk)
			s.mu.Lock()
			s.listing = false
			s.mu.Unlock()
		}()
		var wg sync.WaitGroup
		for w := 0; w < 4; w++ { // a few at a time: the network is the limit
			wg.Add(1)
			go func() {
				defer wg.Done()
				for f := range files {
					path := f.path
					if ctx.Err() != nil {
						continue
					}
					it := &scanItem{Path: path, Name: filepath.Base(path)}
					if rel, err := filepath.Rel(dir, filepath.Dir(path)); err == nil && rel != "." {
						it.Folder = rel
					}
					if st, err := os.Stat(path); err == nil {
						it.Size, it.Mtime = st.Size(), st.ModTime().UnixMilli()
					}
					pctx, pcancel := context.WithTimeout(ctx, 60*time.Second)
					out, err := toolCommand(pctx, ffprobe, "-v", "error", "-show_streams", "-show_format", "-of", "json", path).Output()
					pcancel()
					if err != nil {
						if ctx.Err() != nil {
							continue
						}
						it.Err = "could not be read as video"
					} else {
						analyse(it, out)
						if it.Done != "" && it.Err == "" && ctx.Err() == nil {
							checkScanInterleave(ctx, ffprobe, it)
						}
						if f.own && it.Gap == 0 {
							continue // Keep the Light's own new file, and nothing wrong with it
						}
						if it.Err == "" && ffmpeg != "" && ctx.Err() == nil {
							checkScanInterlace(ctx, ffmpeg, it)
						}
					}
					if f.own && it.Gap == 0 {
						continue
					}
					if f.own {
						s.mu.Lock()
						s.found++
						s.mu.Unlock()
					}
					s.mu.Lock()
					s.items = append(s.items, it)
					s.done++
					s.mu.Unlock()
				}
			}()
		}
		wg.Wait()
		s.mu.Lock()
		s.running, s.listing = false, false
		s.stopped, s.finished = ctx.Err() != nil, time.Now()
		s.mu.Unlock()
		g.scanSave()
	}()
	return okResult()
}

func (g *guiServer) scanStop() {
	s := &g.scan
	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
	}
	s.mu.Unlock()
}

// ---------- the results kept until the next scan ----------
//
// When a scan ends, its results are saved in the app's data folder and shown
// again the next time the window opens; the next scan replaces them. Before
// they are shown, each file is looked at again (its size and date only, so
// it is quick even on a network drive): a file that is gone, or that has
// changed since (converted, replaced), is marked.

type savedScan struct {
	Dir     string      `json:"dir"`
	Opts    scanOpts    `json:"opts"`
	At      time.Time   `json:"at"`
	Stopped bool        `json:"stopped,omitempty"`
	Found   int         `json:"found"`
	Extras  int         `json:"extras,omitempty"`
	Items   []*scanItem `json:"items"`
}

func scanSavePath() string { return filepath.Join(appDataDir(), "library-scan.json") }

// scanSave writes the current results (nothing while a scan runs).
func (g *guiServer) scanSave() {
	s := &g.scan
	s.mu.Lock()
	if s.running || s.dir == "" {
		s.mu.Unlock()
		return
	}
	sv := savedScan{Dir: s.dir, Opts: s.opts, At: s.finished, Stopped: s.stopped, Found: s.found, Extras: s.extras}
	for _, it := range s.items {
		c := *it
		c.Gone, c.Changed = false, false // worked out again when they are read back
		if c.Fix == "running" {
			c.Fix = ""
		}
		sv.Items = append(sv.Items, &c)
	}
	s.mu.Unlock()
	b, err := json.Marshal(sv)
	if err != nil {
		return
	}
	_ = os.MkdirAll(appDataDir(), 0o755)
	tmp := scanSavePath() + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, scanSavePath())
	}
}

// scanLoad shows the last scan's results again, and looks at their files.
func (g *guiServer) scanLoad() {
	b, err := os.ReadFile(scanSavePath())
	if err != nil {
		return
	}
	var sv savedScan
	if json.Unmarshal(b, &sv) != nil || sv.Dir == "" {
		return
	}
	s := &g.scan
	s.mu.Lock()
	if s.running || len(s.items) > 0 {
		s.mu.Unlock()
		return
	}
	s.dir, s.opts, s.finished, s.stopped, s.found, s.done, s.extras = sv.Dir, sv.Opts, sv.At, sv.Stopped, sv.Found, len(sv.Items), sv.Extras
	s.items, s.saved, s.checking = sv.Items, true, true
	items := append([]*scanItem(nil), sv.Items...)
	s.rev++
	s.mu.Unlock()
	go func() {
		type state struct{ gone, changed bool }
		res := make([]state, len(items))
		var wg sync.WaitGroup
		next := make(chan int)
		for w := 0; w < 8; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := range next {
					res[i].gone, res[i].changed = fileChanged(items[i])
				}
			}()
		}
		for i := range items {
			next <- i
		}
		close(next)
		wg.Wait()
		s.mu.Lock()
		for i, it := range items {
			it.Gone, it.Changed = res[i].gone, res[i].changed
		}
		s.checking = false
		s.rev++
		s.mu.Unlock()
	}()
}

// fileChanged: is the scanned file gone, or not the same any more?
func fileChanged(it *scanItem) (gone, changed bool) {
	st, err := os.Stat(it.Path)
	if err != nil {
		return os.IsNotExist(err), false // a drive that is not connected right now is not "gone"
	}
	return false, st.Size() != it.Size || (it.Mtime != 0 && st.ModTime().UnixMilli() != it.Mtime)
}

// scanClear forgets the results (and the saved copy).
func (g *guiServer) scanClear() {
	s := &g.scan
	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
	}
	s.running, s.dir, s.found, s.done, s.items, s.extras, s.saved, s.stopped, s.checking = false, "", 0, 0, nil, 0, false, false, false
	s.finished = time.Time{}
	s.rev++
	s.mu.Unlock()
	_ = os.Remove(scanSavePath())
}

// scanStatus returns the progress, the totals and the items found since "since".
func (g *guiServer) scanStatus(since int) map[string]any {
	s := &g.scan
	s.mu.Lock()
	defer s.mu.Unlock()
	var size, saving int64
	worth, trouble := 0, 0
	for _, it := range s.items {
		size += it.Size
		if it.Worth {
			worth++
			saving += it.Saving
		}
		if len(it.Issues) > 0 {
			trouble++
		}
	}
	since = max(0, min(since, len(s.items)))
	items := append([]*scanItem(nil), s.items[since:]...)
	sort.SliceStable(items, func(a, b int) bool { return items[a].Saving > items[b].Saving })
	r := map[string]any{"ok": true, "running": s.running, "listing": s.listing, "dir": s.dir, "found": s.found, "done": s.done,
		"next": len(s.items), "items": items, "size": formatBytes(size), "worth": worth, "saving": formatBytes(saving), "trouble": trouble, "extras": s.extras,
		"rev": s.rev, "saved": s.saved, "checking": s.checking, "stopped": s.stopped, "opts": s.opts}
	if !s.finished.IsZero() {
		r["at"] = s.finished.Format("2 January 2006, 15:04")
	}
	gone, changed := 0, 0
	for _, it := range s.items {
		if it.Gone {
			gone++
		} else if it.Changed {
			changed++
		}
	}
	r["gone"], r["changed"] = gone, changed
	return r
}

// isOwnOutput: a file Keep the Light made (its name ends with the name tag).
func isOwnOutput(stem, tag string) bool {
	for _, t := range []string{tag, "_KTL", "_hdr"} {
		if t != "" && strings.HasSuffix(strings.ToLower(stem), strings.ToLower(t)) {
			return true
		}
	}
	return false
}

// interleaveIssue is the scan's note for a file whose picture and sound are stored far apart.
const interleaveIssue = "Picture and sound are stored far apart in this file: streaming players (Plex on a TV box, for example) may show a black picture. Fix it with a remux (no re-encoding)."

// checkScanInterleave: files Keep the Light made are checked for the black-picture problem of versions before 1.35.1.
func checkScanInterleave(ctx context.Context, ffprobe string, it *scanItem) {
	pctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if bad, gap := badInterleave(pctx, ffprobe, it.Path); bad {
		it.Gap = math.Round(gap)
		it.Issues = append(it.Issues, interleaveIssue)
	}
}

// scanFix remuxes a scanned file whose picture and sound are stored far
// apart, in the background; scanStatus reports how it went (the item's Fix).
func (g *guiServer) scanFix(path string) map[string]any {
	ffmpeg, err := findTool("ffmpeg", "")
	if err != nil {
		return failResult("ffmpeg was not found")
	}
	ffprobe, err := findTool("ffprobe", "")
	if err != nil {
		return failResult("ffprobe was not found")
	}
	s := &g.scan
	s.mu.Lock()
	var it *scanItem
	for _, x := range s.items {
		if samePath(x.Path, path) {
			it = x
		}
	}
	if it == nil || it.Gap == 0 {
		s.mu.Unlock()
		return failResult("That file is not in the scan's list of files to fix.")
	}
	if it.Fix == "running" {
		s.mu.Unlock()
		return okResult()
	}
	it.Fix = "running"
	s.mu.Unlock()
	go func() {
		lines, err := fixInterleaveFile(context.Background(), ffmpeg, ffprobe, path)
		s.mu.Lock()
		defer s.mu.Unlock()
		if err != nil {
			it.Fix = "failed: " + err.Error()
			return
		}
		it.Fix = "done: " + strings.Join(lines, " ")
		it.Gap = 0
		var keep []string
		for _, x := range it.Issues {
			if x != interleaveIssue {
				keep = append(keep, x)
			}
		}
		it.Issues = keep
		if st, err := os.Stat(path); err == nil {
			it.Size, it.Mtime, it.Changed = st.Size(), st.ModTime().UnixMilli(), false
		}
		s.rev++
		go g.scanSave()
	}()
	return okResult()
}

// scanFixStatus lists the scanned files being fixed or fixed, for the page to update their rows.
func (g *guiServer) scanFixStatus() map[string]any {
	s := &g.scan
	s.mu.Lock()
	defer s.mu.Unlock()
	var fixes []*scanItem
	for _, it := range s.items {
		if it.Fix != "" {
			c := *it
			fixes = append(fixes, &c)
		}
	}
	return map[string]any{"ok": true, "fixes": fixes}
}
