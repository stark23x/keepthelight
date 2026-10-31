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
	Type     string            `json:"codec_type"`
	Codec    string            `json:"codec_name"`
	Profile  string            `json:"profile"`
	Width    int               `json:"width"`
	Height   int               `json:"height"`
	Channels int               `json:"channels"`
	Layout   string            `json:"channel_layout"`
	BitRate  string            `json:"bit_rate"`
	PixFmt   string            `json:"pix_fmt"`
	Transfer string            `json:"color_transfer"`
	Tags     map[string]string `json:"tags"`
	Disp     map[string]int    `json:"disposition"`
	SideData []map[string]any  `json:"side_data_list"`
}

// scanProbe is ffprobe's information for one file (streams and format only:
// quick, even over the network).
type scanProbe struct {
	Streams []scanStream `json:"streams"`
	Format  struct {
		Duration string `json:"duration"`
		Size     string `json:"size"`
		BitRate  string `json:"bit_rate"`
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
	compatible, anyAudio := false, false
	dvProfile := 0
	for _, s := range p.Streams {
		switch s.Type {
		case "video":
			if s.Disp["attached_pic"] == 1 || vCodec != "" {
				continue
			}
			vCodec, w, h, videoBps = s.Codec, s.Width, s.Height, bps(s)
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
			if s.Codec == "h264" && strings.Contains(s.PixFmt, "10") {
				it.Issues = append(it.Issues, "H.264 10-bit: many TVs and streamers cannot play it, so Plex has to convert the video")
			}
			if s.Codec == "vc1" || s.Codec == "mpeg2video" {
				it.Issues = append(it.Issues, fmt.Sprintf("%s: an older format some TVs and streamers cannot play, so Plex may have to convert the video", strings.ToUpper(map[string]string{"vc1": "VC-1", "mpeg2video": "MPEG-2"}[s.Codec])))
			}
		case "audio":
			anyAudio = true
			audioBps += bps(s)
			t := srcTrack{Codec: s.Codec, Profile: s.Profile, Channels: s.Channels, Layout: s.Layout}
			audios = append(audios, strings.TrimSpace(codecName(t)+" "+layoutName(t)))
			if compatibleCodecs[s.Codec] {
				compatible = true
			}
		case "subtitle":
			if s.Disp["forced"] == 1 && bitmapSubs[s.Codec] {
				msg := "Forced picture subtitles (PGS/VobSub): Plex burns them into the picture, which forces a video conversion"
				if s.Codec == "hdmv_pgs_subtitle" {
					msg += " (converting them to SRT fixes this)"
				}
				it.Issues = appendOnce(it.Issues, msg)
			}
			if (s.Codec == "ass" || s.Codec == "ssa") && (s.Disp["forced"] == 1 || s.Disp["default"] == 1) {
				it.Issues = appendOnce(it.Issues, "Styled subtitles (ASS/SSA) shown by default: many Plex players cannot draw them, so Plex burns them in and converts the video (converting them to SRT fixes this)")
			}
		}
	}
	if vCodec == "" {
		it.Err = "no video"
		return
	}
	if dvProfile > 0 {
		hdr = fmt.Sprintf("Dolby Vision %d", dvProfile)
		switch dvProfile {
		case 7:
			it.Issues = append(it.Issues, "Dolby Vision profile 7 (dual layer): most Plex players cannot show its Dolby Vision; KTL converts it to profile 8.1, which they can")
		case 5:
			it.Issues = append(it.Issues, "Dolby Vision profile 5: Keep the Light cannot re-encode it (it can only copy it)")
		}
	}
	it.Video = strings.TrimSpace(fmt.Sprintf("%s %d×%d %s", codecDisplay(vCodec), w, h, hdr))
	if len(audios) > 4 {
		audios = append(audios[:4], fmt.Sprintf("+%d more", len(audios)-4))
	}
	it.Audio = strings.Join(audios, ", ")
	if anyAudio && !compatible {
		it.Issues = append(it.Issues, "No AC-3, E-AC-3 or AAC audio: many TVs and streamers need Plex to convert the sound (the \"Add a compatible audio track\" rule fixes this)")
	}
	if it.Kbps > 80000 {
		it.Issues = append(it.Issues, fmt.Sprintf("Very high bitrate (%d Mbps): may stutter over Wi-Fi or when watching away from home", it.Kbps/1000))
	}

	// the estimate: the video as HEVC at very good quality; everything else as it is
	if videoBps == 0 { // not recorded: the file's bitrate less the audio (or a share of it)
		videoBps = total - audioBps
		if audioBps == 0 {
			videoBps = total * 0.85
		}
	}
	if it.Duration <= 0 || videoBps <= 0 || it.Size <= 0 || dvProfile == 5 {
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
	s := &g.scan
	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.running, s.dir, s.found, s.done, s.items, s.cancel, s.listing, s.startedAt = true, dir, 0, 0, nil, cancel, true, time.Now()
	s.extras = 0
	s.mu.Unlock()

	go func() {
		files := make(chan string, 64)
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
					strings.Contains(stem, " s original)") || strings.HasPrefix(n, ".keepthelight") || isOwnOutput(stem, tag) {
					return nil
				}
				if skipExtras {
					if rel, err := filepath.Rel(dir, path); err == nil && isPlexExtra(rel) {
						s.mu.Lock()
						s.extras++
						s.mu.Unlock()
						return nil
					}
				}
				s.mu.Lock()
				s.found++
				s.mu.Unlock()
				files <- path
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
				for path := range files {
					if ctx.Err() != nil {
						continue
					}
					it := &scanItem{Path: path, Name: filepath.Base(path)}
					if rel, err := filepath.Rel(dir, filepath.Dir(path)); err == nil && rel != "." {
						it.Folder = rel
					}
					if st, err := os.Stat(path); err == nil {
						it.Size = st.Size()
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
		s.mu.Unlock()
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
	return map[string]any{"ok": true, "running": s.running, "listing": s.listing, "dir": s.dir, "found": s.found, "done": s.done,
		"next": len(s.items), "items": items, "size": formatBytes(size), "worth": worth, "saving": formatBytes(saving), "trouble": trouble, "extras": s.extras}
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
