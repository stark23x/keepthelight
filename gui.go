package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed index.html
var indexHTML []byte

//go:embed about.html
var aboutHTML []byte

//go:embed THIRD_PARTY_NOTICES.txt
var noticesText string

//go:embed res/about-icon.png
var aboutIcon []byte

func itoa(i int) string { return strconv.Itoa(i) }

type job struct {
	cmd      *exec.Cmd
	lines    []string
	base     int    // absolute index of lines[0]
	progress string // latest ffmpeg progress line
	prog     *progressTracker
	stage    string
	running  bool
	canceled bool
	exitCode int
	output   string        // the finished file's name
	partial  string        // where it is written until it is complete
	id       int           // tells the page when the current job changes
	done     chan struct{} // closed when the job has ended
}

type guiServer struct {
	token    string
	addr     string
	initial  []string // files given on the command line / dropped on the exe
	mu       sync.Mutex
	lastPing time.Time
	job      *job
	notify   func(msg string)      // shows a message from the page (window mode)
	ask      func(msg string) bool // asks a yes/no question (window mode)
	now      func() time.Time      // clock for the time estimate (nil = time.Now)

	q          batch    // the queue; guarded by mu
	closing    bool     // the window is closing
	lowPrio    bool     // run transcodes at low priority (a remembered setting)
	hwDec      bool     // decode on the GPU (a remembered setting, used from the next file on)
	workLocal  bool     // copy network files to this PC to convert them (a remembered setting)
	savedTotal int64    // space saved by Keep the Light so far, on this PC (bytes)
	inbox      []string // files sent by a later start of Keep the Light, for the page
	opened     int      // how many times that happened (tests)
	raise      func()   // brings the window to the front (window mode)
	jobSeq     int
	scan       scanState // the library scan (its own lock)

	prefsMu   sync.Mutex
	prefsPath string // settings file (empty = the normal place); set by tests
}

// request mirrors the settings form in the page.
type settings struct {
	Input, Output              string
	Mode                       string // encode | copy
	Codec                      string
	Quality                    string // crf | bitrate | size
	CRF                        int
	Bitrate, Preset            string
	Audio, AudioBitrate, DV    string
	MasterDisplay, MaxCLL, Tmp string
	TargetSize                 string     // e.g. 8G, 4500M (when Quality is "size")
	Scale                      string     // "" (keep) or 2160p / 1440p / 1080p / 720p
	AudioTracks, SubTracks     []trackSel // chosen for this file; null = by the rules
	AudioDefault, SubDefault   int
	AudioLang, SubLang         string // rules: languages to keep, e.g. "eng, jpn" (empty = all)
	DropCommentary             bool
	AddCompatAudio             bool      // rule: add an E-AC-3 copy when no track plays everywhere
	AudioChannels              string    // rules: channels for converted tracks: "" (as the source), "5.1", "stereo"
	SubMode                    string    // rules: "" all matching, "forced", "none"
	Meta                       *MetaEdit // metadata changes for this file (null = none)
	Denoise                    string    // noise reduction: "" off, light, medium, strong, hq
	TestSeconds                int       // >0: only a test of this many seconds
	TestStart                  string    // where the test starts ("" = automatic)
	ReplaceOriginal            bool      // metadata only: the result takes the original's place
	Retries                    int       // hardware encoders: extra attempts to hit the target
	Overwrite                  bool      // replace an existing output (the user confirmed it)
}

var (
	reBitrate = regexp.MustCompile(`^\d+(\.\d+)?[kKmM]?$`)
	reMaster  = regexp.MustCompile(`^[GBRWPL0-9(),]+$`)
	reCLL     = regexp.MustCompile(`^\d+,\d+$`)
	reTime    = regexp.MustCompile(`time=(\d+):(\d+):(\d+(?:\.\d+)?)`)
	reStage   = regexp.MustCompile(`^\[(\d)/(\d)\]\s*(.*)`)
	presets   = map[string]bool{"ultrafast": true, "superfast": true, "veryfast": true, "faster": true, "fast": true, "medium": true, "slow": true, "slower": true, "veryslow": true}
	audios    = map[string]string{"copy": "copy", "aac": "aac", "eac3": "eac3", "ac3": "ac3", "flac": "flac", "opus": "libopus"}
)

// toConfig validates the form and produces a Config.
func (s *settings) toConfig() (*Config, error) {
	cfg := &Config{Input: s.Input, Output: s.Output, CRF: s.CRF, Preset: s.Preset, Codec: s.Codec,
		DV: s.DV, CopyVideo: s.Mode == "copy" || s.Mode == "meta", MetaOnly: s.Mode == "meta", MasterDisplay: s.MasterDisplay, MaxCLL: s.MaxCLL, Tmp: s.Tmp}
	if cfg.Codec == "" {
		cfg.Codec = "x265"
	}
	if cfg.Codec != "x265" && cfg.Codec != "nvenc" && cfg.Codec != "qsv" {
		return nil, fmt.Errorf("unknown encoder")
	}
	if cfg.DV == "" {
		cfg.DV = "auto"
	}
	if cfg.DV != "auto" && cfg.DV != "drop" {
		return nil, fmt.Errorf("bad Dolby Vision option")
	}
	if cfg.Preset == "" {
		cfg.Preset = "medium"
	}
	if !presets[cfg.Preset] {
		return nil, fmt.Errorf("unknown preset")
	}
	if !validDenoise(s.Denoise) {
		return nil, fmt.Errorf("unknown noise reduction option")
	}
	if !cfg.CopyVideo {
		cfg.Denoise = s.Denoise
	}
	if s.TestSeconds > 0 {
		if cfg.CopyVideo {
			return nil, fmt.Errorf("a test is only useful when the video is re-encoded")
		}
		st, err := parseTimeArg(s.TestStart)
		if err != nil {
			return nil, err
		}
		cfg.SampleSecs, cfg.SampleStart = min(600, s.TestSeconds), st
		if cfg.Output != "" {
			cfg.Output = testOutputName(cfg.Output, cfg.SampleSecs) // "Film_KTL (30 s test).mkv", next to where the film would go
		}
	}
	sc, ok := normScale(s.Scale)
	if !ok {
		return nil, fmt.Errorf("unknown resolution")
	}
	if !cfg.CopyVideo {
		cfg.Scale = sc
	}
	if cfg.CRF < 0 || cfg.CRF > 51 {
		return nil, fmt.Errorf("quality (CRF) must be between 0 and 51")
	}
	if s.Quality == "size" && !cfg.CopyVideo {
		n, err := parseTargetSize(s.TargetSize)
		if err != nil {
			return nil, err
		}
		cfg.TargetBytes = n
		cfg.Retries = max(0, min(5, s.Retries))
	}
	if s.Quality == "bitrate" {
		if !reBitrate.MatchString(s.Bitrate) {
			return nil, fmt.Errorf("bitrate must look like 20M or 8000k")
		}
		cfg.Bitrate = s.Bitrate
	}
	a, ok := audios[s.Audio]
	if s.Audio == "" {
		a, ok = "copy", true
	}
	if !ok {
		return nil, fmt.Errorf("unknown audio option")
	}
	cfg.Audio = a
	cfg.AudioDefault, cfg.SubDefault = max(0, s.AudioDefault), max(-1, s.SubDefault)
	if err := s.Meta.validate(); err != nil {
		return nil, err
	}
	if !s.Meta.empty() {
		cfg.Meta = s.Meta
	}
	if cfg.MetaOnly {
		cfg.Audio, cfg.AudioBitrate, cfg.Bitrate = "copy", "", ""
	}
	if s.ReplaceOriginal {
		if !cfg.MetaOnly {
			return nil, fmt.Errorf("replacing the original is only possible with \"Metadata only\"")
		}
		cfg.ReplaceOriginal = true
		cfg.Output = replacedName(cfg.Input, strings.ToLower(filepath.Ext(s.Output)))
	}
	cfg.AudioLangs, cfg.SubLangs, cfg.DropCommentary = parseLangs(s.AudioLang), parseLangs(s.SubLang), s.DropCommentary
	cfg.AddCompatAudio = s.AddCompatAudio
	if !validChannels(s.AudioChannels, false) {
		return nil, fmt.Errorf("unknown channels option")
	}
	cfg.AudioChannels = s.AudioChannels
	switch s.SubMode {
	case "", "all":
	case "forced", "none":
		cfg.SubMode = s.SubMode
	default:
		return nil, fmt.Errorf("unknown subtitle option")
	}
	if s.AudioTracks != nil {
		cfg.AudioSel = []trackSel{}
		for _, t := range s.AudioTracks {
			c, ok := audios[t.Codec]
			if t.Codec == "" {
				c, ok = "", true
			}
			if !ok || t.N < 1 || (t.Bitrate != "" && !reBitrate.MatchString(t.Bitrate)) || !validChannels(t.Channels, true) {
				return nil, fmt.Errorf("bad audio track choice")
			}
			cfg.AudioSel = append(cfg.AudioSel, trackSel{N: t.N, Codec: c, Bitrate: t.Bitrate, Channels: t.Channels})
		}
	}
	if s.SubTracks != nil {
		cfg.SubSel = []trackSel{}
		for _, t := range s.SubTracks {
			if t.N < 1 {
				return nil, fmt.Errorf("bad subtitle track choice")
			}
			cfg.SubSel = append(cfg.SubSel, trackSel{N: t.N, Forced: t.Forced})
		}
	}
	if a != "copy" && s.AudioBitrate != "" {
		if !reBitrate.MatchString(s.AudioBitrate) {
			return nil, fmt.Errorf("audio bitrate must look like 640k")
		}
		cfg.AudioBitrate = s.AudioBitrate
	}
	if cfg.MasterDisplay != "" && !reMaster.MatchString(cfg.MasterDisplay) {
		return nil, fmt.Errorf("master-display has invalid characters")
	}
	if cfg.MaxCLL != "" && !reCLL.MatchString(cfg.MaxCLL) {
		return nil, fmt.Errorf("max-cll must look like 1000,400")
	}
	return cfg, nil
}

func (cfg *Config) cliArgs() []string {
	a := []string{"-crf", strconv.Itoa(cfg.CRF), "-preset", cfg.Preset, "-codec", cfg.Codec, "-audio", cfg.Audio, "-dv", cfg.DV}
	if cfg.Bitrate != "" {
		a = append(a, "-bitrate", cfg.Bitrate)
	}
	if cfg.LowPriority {
		a = append(a, "-low-priority")
	}
	if cfg.MetaOnly {
		a = append(a, "-metadata-only") // replacing the original is done by the window, after the job
	}
	if !cfg.Meta.empty() {
		b, _ := json.Marshal(cfg.Meta)
		a = append(a, "-meta-json", string(b))
	}
	if cfg.HWDecode {
		a = append(a, "-hw-decode")
	}
	if cfg.WorkLocal != "" {
		a = append(a, "-work-local", cfg.WorkLocal)
	}
	if cfg.Scale != "" {
		a = append(a, "-scale", cfg.Scale)
	}
	if cfg.Denoise != "" {
		a = append(a, "-denoise", cfg.Denoise)
	}
	if cfg.SampleSecs > 0 {
		a = append(a, "-sample", strconv.Itoa(cfg.SampleSecs))
		if cfg.SampleStart >= 0 {
			a = append(a, "-sample-start", strconv.FormatFloat(cfg.SampleStart, 'f', 0, 64))
		}
		a = append(a, "-sample-original", originalClipName(cfg.Output, cfg.SampleSecs))
	}
	if cfg.AudioBitrate != "" {
		a = append(a, "-audio-bitrate", cfg.AudioBitrate)
	}
	if cfg.AudioSel != nil {
		a = append(a, "-audio-tracks", audioSelString(cfg.AudioSel))
	}
	if cfg.SubSel != nil {
		a = append(a, "-sub-tracks", subSelString(cfg.SubSel))
	}
	if cfg.AudioDefault > 0 {
		a = append(a, "-audio-default", strconv.Itoa(cfg.AudioDefault))
	}
	switch {
	case cfg.SubDefault > 0:
		a = append(a, "-sub-default", strconv.Itoa(cfg.SubDefault))
	case cfg.SubDefault < 0:
		a = append(a, "-sub-default", "none")
	}
	if len(cfg.AudioLangs) > 0 {
		a = append(a, "-audio-lang", strings.Join(cfg.AudioLangs, ","))
	}
	if len(cfg.SubLangs) > 0 {
		a = append(a, "-sub-lang", strings.Join(cfg.SubLangs, ","))
	}
	if cfg.DropCommentary {
		a = append(a, "-drop-commentary")
	}
	if cfg.AddCompatAudio {
		a = append(a, "-add-compatible-audio")
	}
	if cfg.AudioChannels != "" {
		a = append(a, "-audio-channels", cfg.AudioChannels)
	}
	if cfg.SubMode != "" {
		a = append(a, "-subs", cfg.SubMode)
	}
	if cfg.CopyVideo {
		a = append(a, "-copy-video")
	}
	if cfg.TargetBytes > 0 {
		a = append(a, "-target-size", strconv.FormatInt(cfg.TargetBytes/1024, 10)+"k", "-size-retries", strconv.Itoa(cfg.Retries))
	}
	if cfg.MasterDisplay != "" {
		a = append(a, "-master-display", cfg.MasterDisplay)
	}
	if cfg.MaxCLL != "" {
		a = append(a, "-max-cll", cfg.MaxCLL)
	}
	if cfg.Tmp != "" {
		a = append(a, "-tmp", cfg.Tmp)
	}
	return append(a, cfg.Input, cfg.Output)
}

func newGUIServer(initialFiles ...string) *guiServer {
	tok := make([]byte, 16)
	_, _ = rand.Read(tok)
	g := &guiServer{token: hex.EncodeToString(tok), lastPing: time.Now()}
	for _, f := range initialFiles {
		if f != "" {
			g.initial = append(g.initial, f)
		}
	}
	return g
}

// runGUI opens the window. It uses the built-in WebView2 window when the
// runtime is available; otherwise (or with -server) it falls back to the 0.8
// behaviour: a local web server shown in an Edge/Chrome app window.
func runGUI(initialFiles []string, forceServer bool) (code int) {
	defer func() {
		if r := recover(); r != nil {
			report(fmt.Sprint("Keep the Light crashed: ", r))
			code = 1
		}
	}()
	g := newGUIServer(initialFiles...)
	if claimInstance(g) {
		return 0 // the window that is already open took the files
	}
	defer releaseInstance()
	g.restoreQueue()
	if !forceServer {
		if hasConsole() {
			fmt.Printf("Keep the Light %s - opening the window (closing it quits)...\n", version)
		}
		err := runWindow(g)
		if err == nil {
			return 0
		}
		if hasConsole() {
			fmt.Println("The built-in window is not available (" + err.Error() + "); using a browser window instead.")
		}
	}
	return runServer(g)
}

// report shows a message the user must see: in the console if there is one,
// otherwise in a message box.
func report(msg string) {
	if hasConsole() {
		fmt.Fprintln(os.Stderr, "\n"+msg)
		waitEnter()
		return
	}
	messageBox(0, msg, true)
}

// runServer is the browser-based fallback (the only GUI before 0.9).
func runServer(g *guiServer) int {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		report("Cannot start the local GUI server: " + err.Error())
		return 1
	}
	g.addr = ln.Addr().String()

	mux := http.NewServeMux()
	mux.HandleFunc("/", g.handleIndex)
	mux.HandleFunc("/api/", g.auth(g.handleAPI))
	go func() {
		if err := http.Serve(ln, mux); err != nil {
			fmt.Fprintln(os.Stderr, "GUI server stopped:", err)
		}
	}()
	go g.watchdog()

	url := "http://" + g.addr + "/?t=" + g.token
	console := hasConsole()
	if console {
		fmt.Printf("Keep the Light %s\n\n", version)
	}
	if err := openWindow(url); err != nil {
		if console {
			fmt.Println("Could not open a window automatically:", err)
		} else {
			messageBox(0, "Could not open a browser window ("+err.Error()+").\n\n"+
				"Open this address in any web browser (Ctrl+C copies this message):\n\n"+url, false)
		}
	} else if console {
		fmt.Println("Opening the Keep the Light window...")
	}
	if console {
		fmt.Println("\nIf no window appeared, open this address in any web browser:")
		fmt.Println(" ", url)
		fmt.Println("\nKeep this black window open while you work; closing it quits Keep the Light.")
	}
	select {} // exits via watchdog / bye
}

func waitEnter() {
	if !hasConsole() {
		return
	}
	fmt.Print("\nPress Enter to close...")
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
}

func (g *guiServer) touch() {
	g.mu.Lock()
	g.lastPing = time.Now()
	g.mu.Unlock()
}

func (g *guiServer) running() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.job != nil && g.job.running
}

var windowTitle = appName + " " + version

// titleText is the window title: while a job runs it starts with the progress,
// e.g. "42% · about 55 min left - Keep the Light 1.1.0", so it shows on the taskbar.
func titleText(running bool, pct float64, eta string) string {
	if !running {
		return windowTitle
	}
	t := fmt.Sprintf("%.0f%%", math.Floor(pct))
	if eta != "" {
		t += " · " + eta
	}
	return t + " - " + windowTitle
}

// clock is time.Now, replaceable in tests.
func (g *guiServer) clock() time.Time {
	if g.now != nil {
		return g.now()
	}
	return time.Now()
}

func (g *guiServer) watchdog() {
	for range time.Tick(10 * time.Second) {
		g.mu.Lock()
		idle := time.Since(g.lastPing) > 3*time.Minute
		busy := g.job != nil && g.job.running
		g.mu.Unlock()
		if idle && !busy {
			os.Exit(0)
		}
	}
}

func (g *guiServer) bye() {
	at := time.Now()
	go func() {
		time.Sleep(4 * time.Second)
		g.mu.Lock()
		stale := g.lastPing.Before(at)
		busy := g.job != nil && g.job.running
		g.mu.Unlock()
		if stale && !busy {
			os.Exit(0)
		}
	}()
}

func (g *guiServer) auth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Host != g.addr || (r.Header.Get("X-Token") != g.token && r.URL.Query().Get("t") != g.token) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		h(w, r)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}

// page is index.html with the About box, version and dragged-on file filled in.
func (g *guiServer) page() []byte {
	page := bytes.Replace(indexHTML, []byte("__ABOUT__"), aboutHTML, 1)
	page = bytes.Replace(page, []byte("__NOTICES__"), []byte(html.EscapeString(noticesText)), 1)
	page = bytes.ReplaceAll(page, []byte("__ICON__"), []byte("data:image/png;base64,"+base64.StdEncoding.EncodeToString(aboutIcon))) // header and About box
	page = bytes.ReplaceAll(page, []byte("__VERSION__"), []byte(version))
	files := g.initial
	if files == nil {
		files = []string{}
	}
	init, _ := json.Marshal(files)
	return bytes.Replace(page, []byte("const INITIAL = __INITIAL_FILE__;"), []byte("const INITIAL = "+string(init)+";"), 1)
}

func (g *guiServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" || r.Host != g.addr || r.URL.Query().Get("t") != g.token {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(g.page())
	g.touch()
}

func (g *guiServer) handleAPI(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	writeJSON(w, g.call(r.Context(), strings.TrimPrefix(r.URL.Path, "/api/"), r.URL.Query(), body))
}

func okResult() map[string]any { return map[string]any{"ok": true} }

func failResult(msg string) map[string]any { return map[string]any{"ok": false, "error": msg} }

// call runs one request from the page. Both front ends use it: the HTTP
// server (server mode) and the WebView2 bridge (window mode).
func (g *guiServer) call(ctx context.Context, name string, q url.Values, body []byte) any {
	g.touch()
	switch name {
	case "tools":
		return g.tools()
	case "probe":
		var s settings
		if err := json.Unmarshal(body, &s); err != nil {
			return failResult("bad request")
		}
		return g.probe(ctx, s)
	case "browse":
		var b struct{ Kind, Initial, Tag string }
		_ = json.Unmarshal(body, &b)
		switch b.Kind {
		case "openMulti": // several files at once
			paths, err := browseFiles(b.Initial)
			if err != nil {
				return failResult(err.Error())
			}
			return map[string]any{"ok": true, "paths": paths}
		case "folderFiles": // every video in a folder
			dir, err := browseDialog("folder", b.Initial)
			if err != nil || dir == "" {
				if err != nil {
					return failResult(err.Error())
				}
				return map[string]any{"ok": true, "paths": []string{}}
			}
			paths, err := videosIn(dir, b.Tag)
			if err != nil {
				return failResult(err.Error())
			}
			return map[string]any{"ok": true, "paths": paths, "folder": dir}
		}
		p, err := browseDialog(b.Kind, b.Initial)
		if err != nil {
			return failResult(err.Error())
		}
		return map[string]any{"ok": true, "path": p}
	case "start":
		return g.start(ctx, body)
	case "test":
		return g.startTest(body)
	case "queueEditStart", "queueEditCancel":
		var b struct{ ID int }
		_ = json.Unmarshal(body, &b)
		if name == "queueEditStart" {
			return g.queueEditStart(b.ID)
		}
		return g.queueEditCancel(b.ID)
	case "queueEditSave":
		var b struct {
			ID       int
			Settings json.RawMessage
		}
		if err := json.Unmarshal(body, &b); err != nil {
			return failResult("bad request")
		}
		return g.queueEditSave(b.ID, b.Settings)
	case "profilesExport": // write profiles to a file the user chooses
		var b struct{ Profiles json.RawMessage }
		_ = json.Unmarshal(body, &b)
		p, err := browseDialog("saveJson", "Keep the Light profiles.json")
		if err != nil || p == "" {
			if err != nil {
				return failResult(err.Error())
			}
			return map[string]any{"ok": true}
		}
		if !strings.EqualFold(filepath.Ext(p), ".json") {
			p += ".json"
		}
		out, _ := json.MarshalIndent(map[string]any{"keepTheLightProfiles": 1, "profiles": b.Profiles}, "", "  ")
		if err := os.WriteFile(p, out, 0o644); err != nil {
			return failResult(err.Error())
		}
		return map[string]any{"ok": true, "path": p}
	case "profilesImport":
		p, err := browseDialog("openJson", "")
		if err != nil || p == "" {
			if err != nil {
				return failResult(err.Error())
			}
			return map[string]any{"ok": true}
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return failResult(err.Error())
		}
		var f struct {
			Profiles []json.RawMessage `json:"profiles"`
		}
		if json.Unmarshal(data, &f) != nil || f.Profiles == nil {
			return failResult(filepath.Base(p) + " is not a Keep the Light profiles file.")
		}
		return map[string]any{"ok": true, "profiles": f.Profiles, "path": p}
	case "sounds": // the sounds that can be chosen
		return soundList()
	case "playSound": // the Test button
		var b struct{ ID string }
		_ = json.Unmarshal(body, &b)
		if !playSoundFn(b.ID, false) {
			return failResult("This sound could not be played.")
		}
		return okResult()
	case "spaceCheck": // before metadata-only jobs: a new file is written, is there room?
		var b struct{ Settings json.RawMessage }
		_ = json.Unmarshal(body, &b)
		return g.spaceCheck(b.Settings)
	case "queueAdd":
		var b struct{ Items []json.RawMessage }
		if err := json.Unmarshal(body, &b); err != nil {
			return failResult("bad request")
		}
		ids, errs := g.queueAdd(b.Items)
		return map[string]any{"ok": len(errs) == 0, "added": len(ids), "errors": errs}
	case "queueApply":
		var b struct{ Settings json.RawMessage }
		if err := json.Unmarshal(body, &b); err != nil {
			return failResult("bad request")
		}
		n, errs := g.queueApply(b.Settings)
		return map[string]any{"ok": len(errs) == 0, "changed": n, "errors": errs}
	case "queueRemove", "queueMove", "queueLog":
		var b struct{ ID, Dir int }
		_ = json.Unmarshal(body, &b)
		var err error
		switch name {
		case "queueRemove":
			err = g.queueRemove(b.ID)
		case "queueMove":
			err = g.queueMove(b.ID, b.Dir)
		case "queueLog":
			return g.queueLog(b.ID)
		}
		if err != nil {
			return failResult(err.Error())
		}
		return okResult()
	case "queueClear":
		g.queueClear()
		return okResult()
	case "queueStart":
		if err := g.startQueue(); err != nil {
			return failResult(capitalize(err.Error()))
		}
		return okResult()
	case "queueStop":
		g.stopQueue()
		return okResult()
	case "after": // what to do when the queue is finished
		var b struct{ Action string }
		_ = json.Unmarshal(body, &b)
		if err := g.setAfter(b.Action); err != nil {
			return failResult(err.Error())
		}
		return okResult()
	case "powerCancel":
		g.mu.Lock()
		g.cancelPowerLocked()
		g.q.after = ""
		g.mu.Unlock()
		return okResult()
	case "ask": // a yes/no question (window mode; browser mode uses confirm())
		var b struct{ Text string }
		_ = json.Unmarshal(body, &b)
		if g.ask == nil {
			return failResult("not available")
		}
		return map[string]any{"ok": true, "yes": g.ask(b.Text)}
	case "status":
		since, _ := strconv.Atoi(q.Get("since"))
		return g.status(since)
	case "cancel":
		g.cancel()
		return okResult()
	case "reveal":
		g.reveal()
		return okResult()
	case "ping":
		return okResult()
	case "bye":
		g.bye()
		return okResult()
	case "open": // a link in the page: open it in the normal browser / mail program
		var o struct{ URL string }
		_ = json.Unmarshal(body, &o)
		u, err := url.Parse(o.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "mailto") {
			return failResult("not a web or mail link")
		}
		if err := openURL(u.String()); err != nil {
			return failResult(err.Error())
		}
		return okResult()
	case "openTools": // the tools folder next to the exe, created if needed
		dir, err := toolsDir()
		if err == nil {
			err = openFolder(dir)
		}
		if err != nil {
			return failResult(err.Error())
		}
		return okResult()
	case "priority": // low priority on/off; applies to the file being encoded now, too
		var b struct{ Low bool }
		_ = json.Unmarshal(body, &b)
		g.mu.Lock()
		g.lowPrio = b.Low
		j := g.job
		g.mu.Unlock()
		if j != nil && j.running && j.cmd != nil && j.cmd.Process != nil {
			_ = setTreePriority(j.cmd.Process.Pid, b.Low)
		}
		raw, _ := json.Marshal(b.Low)
		_ = g.savePref("lowPriority", raw)
		return okResult()
	case "scanStart":
		var b struct {
			Dir, Tag  string
			Recursive bool
		}
		_ = json.Unmarshal(body, &b)
		return g.scanStart(strings.TrimSpace(b.Dir), b.Recursive, b.Tag)
	case "scanStop":
		g.scanStop()
		return okResult()
	case "scanStatus":
		var b struct{ Since int }
		_ = json.Unmarshal(body, &b)
		return g.scanStatus(b.Since)
	case "worklocal": // working locally on/off; used from the next file that starts
		var b struct{ On bool }
		_ = json.Unmarshal(body, &b)
		g.mu.Lock()
		g.workLocal = b.On
		g.mu.Unlock()
		raw, _ := json.Marshal(b.On)
		_ = g.savePref("workLocal", raw)
		return okResult()
	case "hwdecode": // GPU decoding on/off; used from the next file that starts
		var b struct{ On bool }
		_ = json.Unmarshal(body, &b)
		g.mu.Lock()
		g.hwDec = b.On
		g.mu.Unlock()
		raw, _ := json.Marshal(b.On)
		_ = g.savePref("hwDecode", raw)
		return okResult()
	case "prefs":
		res := map[string]any{"ok": true, "prefs": g.loadPrefs()}
		if missing := g.missingTmpDir(); missing != "" {
			res["tmpDirMissing"] = missing
		}
		if missing := g.missingBatchDir(); missing != "" {
			res["batchOutMissing"] = missing
		}
		return res
	case "setPref":
		var pr struct {
			Name  string
			Value json.RawMessage
		}
		if err := json.Unmarshal(body, &pr); err != nil || pr.Name == "" || len(pr.Name) > 64 || len(pr.Value) > 8192 {
			return failResult("bad request")
		}
		if err := g.savePref(pr.Name, pr.Value); err != nil {
			return failResult(err.Error())
		}
		return okResult()
	case "message":
		var m struct{ Text string }
		_ = json.Unmarshal(body, &m)
		if g.notify != nil && m.Text != "" {
			g.notify(m.Text)
		}
		return okResult()
	}
	return failResult("unknown request: " + name)
}

// toolsDir is the "tools" folder next to the program, where ffmpeg and the
// bundled helper tools go. It is created if it does not exist yet.
func toolsDir() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	d := filepath.Join(filepath.Dir(exe), "tools")
	if err := os.MkdirAll(d, 0o755); err != nil {
		return "", fmt.Errorf("cannot create %s: %w", d, err)
	}
	return d, nil
}

// appDataDir is where the window keeps its settings and WebView2 profile:
// %LOCALAPPDATA%\Keep the Light on Windows.
func appDataDir() string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		if d, err := os.UserConfigDir(); err == nil {
			base = d
		} else {
			base = os.TempDir()
		}
	}
	return filepath.Join(base, appName)
}

func (g *guiServer) prefsFile() string {
	if g.prefsPath != "" {
		return g.prefsPath
	}
	return filepath.Join(appDataDir(), "settings.json")
}

// loadPrefs returns the page's saved preferences (which sections are open).
func (g *guiServer) loadPrefs() map[string]json.RawMessage {
	g.prefsMu.Lock()
	defer g.prefsMu.Unlock()
	return g.readPrefs()
}

func (g *guiServer) readPrefs() map[string]json.RawMessage { // caller holds prefsMu
	prefs := map[string]json.RawMessage{}
	if b, err := os.ReadFile(g.prefsFile()); err == nil {
		_ = json.Unmarshal(b, &prefs)
	}
	return prefs
}

// missingTmpDir returns the saved temp folder if it no longer exists (deleted,
// or on a drive that is not connected), so the page can fall back to the default.
func (g *guiServer) missingTmpDir() string {
	var dir string
	if raw, ok := g.loadPrefs()["tmpDir"]; !ok || json.Unmarshal(raw, &dir) != nil || dir == "" {
		return ""
	}
	if st, err := os.Stat(dir); err == nil && st.IsDir() {
		return ""
	}
	return dir
}

// missingBatchDir returns the saved output folder for batches if it no longer
// exists, so the page can fall back to saving next to each original.
func (g *guiServer) missingBatchDir() string {
	var b struct{ Mode, Dir string }
	if raw, ok := g.loadPrefs()["batchOut"]; !ok || json.Unmarshal(raw, &b) != nil || b.Mode != "dir" || b.Dir == "" {
		return ""
	}
	if st, err := os.Stat(b.Dir); err == nil && st.IsDir() {
		return ""
	}
	return b.Dir
}

func (g *guiServer) savePref(name string, value json.RawMessage) error {
	g.prefsMu.Lock() // read, change and write as one step, so two saves cannot undo each other
	defer g.prefsMu.Unlock()
	prefs := g.readPrefs()
	if len(prefs) >= 50 {
		if _, exists := prefs[name]; !exists {
			return fmt.Errorf("too many settings")
		}
	}
	prefs[name] = value
	b, err := json.MarshalIndent(prefs, "", "  ")
	if err != nil {
		return err
	}
	f := g.prefsFile()
	if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
		return err
	}
	tmp := f + ".tmp" // write then rename, so a crash never leaves half a file
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, f)
}

func (g *guiServer) tools() map[string]any {
	startChecks.Clear() // "Check again" after installing GPAC or the runtime: test the tools afresh
	res, problems := map[string]any{}, map[string]string{}
	for _, t := range []struct{ key, exe string }{
		{"ffmpeg", "ffmpeg"}, {"ffprobe", "ffprobe"}, {"dovi_tool", "dovi_tool"}, {"mp4box", "MP4Box"}, {"hdr10plus_tool", "hdr10plus_tool"},
	} {
		_, err := findTool(t.exe, "")
		res[t.key] = err == nil
		if err != nil && !strings.HasSuffix(err.Error(), "not found") {
			problems[t.key] = err.Error() // found, but it cannot start
		}
	}
	res["problems"] = problems
	res["vcruntime"] = hasVCRuntime()
	return res
}

func (g *guiServer) probe(ctx context.Context, s settings) map[string]any {
	if s.Input == "" {
		return failResult("")
	}
	if _, err := os.Stat(s.Input); err != nil {
		return failResult("File not found: " + s.Input)
	}
	ffprobe, err := findTool("ffprobe", "")
	if err != nil {
		return failResult("ffprobe was not found. Put ffmpeg.exe and ffprobe.exe next to Keep the Light.exe (or in a \"tools\" folder).")
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	p, err := probeFile(ctx, ffprobe, s.Input)
	if err != nil {
		return failResult("Could not read this file as video: " + err.Error())
	}
	resp := map[string]any{
		"ok": true, "summary": p.Summary(), "kind": p.Kind(), "codec": p.Codec, "width": p.Width, "height": p.Height,
		"pixFmt": p.PixFmt, "duration": p.Duration, "hdr10plus": p.HDR10Plus, "hasMastering": p.Mastering != nil,
	}
	if p.CLL != nil {
		resp["cll"] = fmt.Sprintf("%d,%d", p.CLL.MaxCLL, p.CLL.MaxFALL)
	}
	if p.DV != nil {
		resp["dv"] = fmt.Sprintf("%d.%d", p.DV.Profile, p.DV.Compat)
		resp["dvProfile"] = p.DV.Profile
	}
	cfg, err := s.toConfig()
	if err != nil {
		resp["blocker"] = err.Error()
		return resp
	}
	if cfg.Output == "" {
		cfg.Output = "out.mkv"
	}
	plan, err := makePlan(cfg, p)
	if err != nil {
		resp["blocker"] = err.Error()
		return resp
	}
	if (cfg.Codec == "nvenc" || cfg.Codec == "qsv") && !cfg.CopyVideo { // a GPU encoder this PC does not have
		if ffmpeg, err := findTool("ffmpeg", ""); err == nil {
			if problem := encoderProblem(ctx, ffmpeg, cfg.Codec); problem != "" {
				resp["blocker"] = problem
				return resp
			}
		}
	}
	if t, err := probeTracks(ctx, ffprobe, s.Input); err != nil {
		resp["blocker"] = "Could not read the audio and subtitle tracks: " + err.Error()
		return resp
	} else {
		audio, subs, warns, err := selectTracks(cfg, t, isMP4(cfg.Output))
		if err != nil {
			resp["blocker"] = capitalize(err.Error())
			return resp
		}
		plan.Audio, plan.Subs = audio, subs
		w, notes := splitNotes(warns)
		plan.Warnings = append(plan.Warnings, w...)
		resp["notesInfo"] = notes
		resp["audioTracks"], resp["subTracks"] = trackList(t.Audio, true), trackList(t.Subs, false)
		resp["audioChoice"], resp["subChoice"] = audioChoice(audio), subChoice(subs)
		resp["meta"] = metaInfo(t)
		if st, err := os.Stat(s.Input); err == nil {
			resp["fileSize"] = st.Size()
		}
	}
	if plan.UseDV { // a Dolby Vision job needs both tools to actually start; find out now, not halfway through
		for _, tool := range []string{"dovi_tool", "MP4Box"} {
			if _, err := findTool(tool, ""); err != nil {
				msg := "Keeping Dolby Vision needs " + tool + ", which is not installed. "
				if !strings.HasSuffix(err.Error(), "not found") {
					msg = "Keeping Dolby Vision needs " + tool + ", but " + err.Error() + ". "
				}
				resp["blocker"] = msg + `Set Dolby Vision to "Drop (plain HDR10)" to transcode without it.`
				return resp
			}
		}
	}
	resp["mode"] = describeMode(cfg, plan)
	if plan.Quality != nil {
		resp["quality"] = plan.Quality
	}
	if plan.ScaleW > 0 {
		resp["scaledTo"] = fmt.Sprintf("%d×%d", plan.ScaleW, plan.ScaleH)
	}
	warnings := append([]string{}, plan.Warnings...)
	if cfg.TargetBytes > 0 {
		// preview only: no slow audio measuring (DryRun), subtitles from a quick probe
		pcfg := *cfg
		pcfg.DryRun, pcfg.Ffprobe, pcfg.Input = true, ffprobe, s.Input
		var srcBytes int64
		if st, err := os.Stat(s.Input); err == nil {
			srcBytes = st.Size()
		}
		if sp, err := planSize(ctx, &pcfg, p, plan, srcBytes); err != nil {
			resp["blocker"] = err.Error()
		} else {
			resp["sizeText"] = sp.Describe()
			resp["sizeKbps"] = sp.VideoKbps
			resp["sizeExact"] = sp.AudioExact
			warnings = append(warnings, sp.Warnings...)
		}
	}
	resp["warnings"] = warnings
	return resp
}

func splitCRLF(data []byte, atEOF bool) (int, []byte, error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	if i := bytes.IndexAny(data, "\r\n"); i >= 0 {
		return i + 1, data[:i], nil
	}
	if atEOF {
		return len(data), data, nil
	}
	return 0, nil, nil
}

func (g *guiServer) readOutput(j *job, r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	sc.Split(splitCRLF)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), " \t")
		if line == "" {
			continue
		}
		now := g.clock()
		g.mu.Lock()
		if m := reTime.FindStringSubmatch(line); m != nil && strings.Contains(line, "frame=") {
			h, _ := strconv.ParseFloat(m[1], 64)
			mi, _ := strconv.ParseFloat(m[2], 64)
			s, _ := strconv.ParseFloat(m[3], 64)
			j.prog.frameLine(h*3600+mi*60+s, now)
			j.progress = line
		} else {
			if m := reStage.FindStringSubmatch(line); m != nil {
				j.stage = "Step " + m[1] + " of " + m[2] + ": " + m[3]
				j.prog.stageLine(line, now)
			} else if strings.HasPrefix(line, "Encoding") || strings.HasPrefix(line, "Copying") {
				j.stage = line
				j.prog.stageLine(line, now)
			}
			j.lines = append(j.lines, line)
			if len(j.lines) > 4000 {
				drop := len(j.lines) - 3000
				j.lines = j.lines[drop:]
				j.base += drop
			}
		}
		g.mu.Unlock()
	}
}

func (g *guiServer) status(since int) map[string]any {
	g.mu.Lock()
	defer g.mu.Unlock()
	j := g.job
	if j == nil {
		return map[string]any{"active": false, "queue": g.queueStatusLocked(), "open": g.takeInboxLocked()}
	}
	if since < j.base {
		since = j.base
	}
	var lines []string
	if since-j.base < len(j.lines) {
		lines = append(lines, j.lines[since-j.base:]...)
	}
	return map[string]any{
		"active": true, "running": j.running, "lines": lines, "next": j.base + len(j.lines),
		"progress": j.progress, "percent": j.prog.percent(), "eta": g.remainingLocked(g.clock()), "stage": j.stage, "exitCode": j.exitCode,
		"jobId": j.id, "queue": g.queueStatusLocked(), "title": g.titleLocked(g.clock()), "open": g.takeInboxLocked(),
		"canceled": j.canceled, "output": j.output,
	}
}

func (g *guiServer) cancel() {
	g.mu.Lock()
	j := g.job
	if j != nil && j.running {
		j.canceled = true
		cmd := j.cmd
		g.mu.Unlock()
		killTree(cmd)
		return
	}
	g.mu.Unlock()
}

// cancelAndWait stops a running job and waits (up to timeout) until it has
// exited and its partial output is removed.
//
// It is used when the window closes: the queue is stopped too (otherwise the
// next file would start as the program exits), and the interrupted file goes
// back to waiting so it is remembered for next time.
func (g *guiServer) cancelAndWait(timeout time.Duration) {
	g.mu.Lock()
	g.q.stop, g.closing = true, true
	g.mu.Unlock()
	g.cancel()
	busy := func() bool { g.mu.Lock(); defer g.mu.Unlock(); return (g.job != nil && g.job.running) || g.q.running }
	for end := time.Now().Add(timeout); time.Now().Before(end) && busy(); {
		time.Sleep(100 * time.Millisecond)
	}
}

func (g *guiServer) reveal() {
	g.mu.Lock()
	var out string
	if g.job != nil {
		out = g.job.output
	}
	g.mu.Unlock()
	if out != "" {
		if _, err := os.Stat(out); err == nil {
			revealFile(out)
		}
	}
}
