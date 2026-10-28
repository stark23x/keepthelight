package main

import (
	"bufio"
	"context"
	_ "embed"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
)

//go:generate go -C tools/mkres run . ../..

// appName is the product name; version comes from the VERSION file (the one
// place to change it for a release, see CHANGELOG.md).
const appName = "Keep the Light"

//go:embed VERSION
var versionFile string

var version = strings.TrimSpace(versionFile)

const usage = `Keep the Light %s - transcode HDR10 / HLG / Dolby Vision video and keep the HDR

Two programs are included:
  Keep the Light.exe     the window (double-click it, or drag a video onto it)
  keepthelight_cli.exe   this command-line version, for terminals and scripts

Usage:
  keepthelight_cli [options] <input> [output]   transcode
  keepthelight_cli -gui [input]      open the window
  keepthelight_cli -server [input]   open the window in Edge/Chrome through a local web
                                     server (fallback if the built-in window does not work)

Options:
  -o <file>            output file (.mkv or .mp4). Default: <input><tag>.mkv
  -tag <text>          what is added to the name for the default output file (default _KTL;
                       "" for none, then the output must go elsewhere or be .mp4)
  -crf <n>             quality, lower = better (default 18)
  -preset <name>       x265 preset: ultrafast..veryslow (default medium)
  -bitrate <rate>      target bitrate instead of CRF, e.g. 20M
  -target-size <size>  aim for this output file size, e.g. 8G or 4500M (1 GB = 1024^3 bytes,
                       as Windows shows it). x265 uses two passes; hardware encoders
                       re-encode with a corrected bitrate if the first try misses.
  -size-retries <n>    extra attempts for hardware encoders to hit the size (default 2)
  -codec <x265|nvenc|qsv>  encoder (default x265; nvenc = NVIDIA GPU, qsv = Intel QuickSync;
                       GPU encoders cannot keep HDR10+ and use a different quality scale)
  -audio <codec>       audio: copy (default), aac, eac3, ac3, flac, opus ...
  -audio-bitrate <r>   e.g. 640k (when not copying; default depends on the format and channels)

Choosing tracks (numbers as the window shows them: audio 1, 2, ...; subtitles 1, 2, ...):
  -audio-tracks <list> keep these audio tracks, each copied or converted:
                       1,2:eac3,3:aac:192k,4:eac3:5.1   (no format = the -audio setting), all, none
  -audio-default <n>   mark audio track n as the default
  -sub-tracks <list>   keep these subtitle tracks: 1,3:forced (":forced" sets the forced flag), all, none;
                       ":srt" converts an ASS/SSA or PGS track to SRT, ":srt+" adds an SRT copy next to it
  -sub-default <n>     mark subtitle track n as the default, or "none"
 Rules, used when tracks are not listed (for example in scripts over many files):
  -audio-lang <list>   keep only audio in these languages, e.g. eng,jpn (tracks with no language
                       are kept; if nothing matches, the default track is kept)
  -drop-commentary     leave out commentary audio tracks
  -add-compatible-audio  when no kept track is AC-3, E-AC-3 or AAC (only TrueHD or DTS, say), add an
                       E-AC-3 copy of the main track for TVs and streamers; the original stays
  -audio-channels <5.1|stereo>  mix converted tracks down to at most 5.1, or to stereo (also per
                       track in -audio-tracks, e.g. 1:eac3:5.1 or 2:aac:192k:stereo; "orig" keeps them)
  -sub-lang <list>     keep only subtitles in these languages
  -subs <all|forced|none>  which subtitles to keep (default all)
  -subs-to-srt <replace|add>  convert ASS/SSA and PGS subtitles to SRT (PGS needs Tesseract OCR),
                       replacing them or added next to them
  -dv <auto|drop>      Dolby Vision: auto keeps it (default), drop = plain HDR10
  -scale <size>        shrink the video to fit 2160p, 1440p, 1080p or 720p, keeping its shape
                       (3840x1600 -> 1920x800 at 1080p) and its HDR10 / HDR10+ / Dolby Vision
                       data. Never enlarges.
  -denoise <level>     noise reduction: light, medium, strong (fast, CPU), or hq (high quality:
                       on the GPU when it can, else slower on the CPU). Keeps 10-bit HDR intact.
  -sample <seconds>    encode only a test of this length (e.g. 30), to check the settings
  -sample-start <time> where the test starts, e.g. 1:02:30 (default: 40%% into the video)
                       The same stretch of the original is kept next to the test, unchanged, for
                       comparing ("... (original clip).mkv"; -sample-original <file> to choose)
  -work-local <folder> files on a network drive are copied to this folder (on this PC) first,
                       converted there, and the result copied back
  -hw-decode           decode the source on the GPU, so the CPU only encodes (falls back to
                       the CPU if the GPU cannot decode the file)
  -copy-video          do not re-encode video at all (keeps any HDR/DV exactly)
  -metadata-only       copy every kept stream unchanged and change only the metadata (a new
                       file is written: FFmpeg cannot change a file in place)
  -replace-original    with -metadata-only: the result takes the original's place; the original
                       goes to the Recycle Bin (renamed "... (original)" on drives without one)

Metadata (with any job):
  -title <text>        the file's title ("" removes it)
  -no-chapters         leave the chapters out
  -cover <image>       use this .jpg/.png as the cover (MKV); -no-cover leaves the cover out
  -meta-json <json>    all metadata changes at once, as the window sends them (or @file.json):
                       {"tags":{"title":"..","date":"2019","description":".."},
                        "tracks":[{"kind":"a","n":1,"title":"Main","lang":"eng","commentary":false}],
                        "chapters":{"names":{"1":"Opening"}}}
  -master-display <s>  override x265 master-display string
  -max-cll <a,b>       override MaxCLL,MaxFALL
  -tmp <dir>           folder for temporary files (Dolby Vision needs space)
  -keep-temp           keep temporary files
  -dry-run             print the commands without running them
  -v                   verbose
  -low-priority        run at low priority: other programs come first, so the PC stays
                       responsive; full speed when the PC is otherwise idle
  -sound               play a sound at the end (the window's choices: Notify when done, Critical
                       stop when it failed)
  -pause               wait for Enter before exiting
  -ffmpeg -ffprobe -dovi-tool -mp4box -hdr10plus-tool -tesseract <path>   tool locations

Required tools (put them next to this exe, in a "tools" folder, or on PATH):
  ffmpeg + ffprobe              always
  dovi_tool + MP4Box            Dolby Vision sources
  hdr10plus_tool                optional, keeps HDR10+ dynamic metadata
  tesseract (Tesseract OCR)     only to convert PGS picture subtitles to SRT
`

func parseArgs() (*Config, bool) {
	cfg := &Config{}
	fs := flag.NewFlagSet("keepthelight", flag.ContinueOnError)
	fs.Usage = func() { fmt.Printf(usage, version) }
	fs.StringVar(&cfg.Output, "o", "", "")
	tag := fs.String("tag", "_KTL", "")
	fs.IntVar(&cfg.CRF, "crf", 18, "")
	fs.StringVar(&cfg.Preset, "preset", "medium", "")
	fs.StringVar(&cfg.Bitrate, "bitrate", "", "")
	targetSize := fs.String("target-size", "", "")
	fs.IntVar(&cfg.Retries, "size-retries", 2, "")
	fs.StringVar(&cfg.Codec, "codec", "x265", "")
	fs.StringVar(&cfg.Audio, "audio", "copy", "")
	fs.StringVar(&cfg.AudioBitrate, "audio-bitrate", "", "")
	audioTracks := fs.String("audio-tracks", "", "")
	subTracks := fs.String("sub-tracks", "", "")
	fs.IntVar(&cfg.AudioDefault, "audio-default", 0, "")
	subDefault := fs.String("sub-default", "", "")
	audioLang := fs.String("audio-lang", "", "")
	subLang := fs.String("sub-lang", "", "")
	fs.BoolVar(&cfg.DropCommentary, "drop-commentary", false, "")
	fs.BoolVar(&cfg.AddCompatAudio, "add-compatible-audio", false, "")
	fs.StringVar(&cfg.SubMode, "subs", "", "")
	fs.StringVar(&cfg.SubsToSRT, "subs-to-srt", "", "")
	fs.StringVar(&cfg.Tesseract, "tesseract", "", "")
	fs.StringVar(&cfg.AudioChannels, "audio-channels", "", "")
	fs.BoolVar(&cfg.MetaOnly, "metadata-only", false, "")
	fs.BoolVar(&cfg.ReplaceOriginal, "replace-original", false, "")
	metaJSON := fs.String("meta-json", "", "")
	var title *string
	fs.Func("title", "", func(v string) error { title = &v; return nil })
	noChapters := fs.Bool("no-chapters", false, "")
	coverImg := fs.String("cover", "", "")
	noCover := fs.Bool("no-cover", false, "")
	fs.StringVar(&cfg.DV, "dv", "auto", "")
	fs.BoolVar(&cfg.CopyVideo, "copy-video", false, "")
	fs.StringVar(&cfg.MasterDisplay, "master-display", "", "")
	fs.StringVar(&cfg.MaxCLL, "max-cll", "", "")
	fs.StringVar(&cfg.Tmp, "tmp", "", "")
	fs.BoolVar(&cfg.Keep, "keep-temp", false, "")
	fs.BoolVar(&cfg.DryRun, "dry-run", false, "")
	fs.BoolVar(&cfg.Verbose, "v", false, "")
	showVersion := fs.Bool("version", false, "")
	fs.BoolVar(&cfg.Pause, "pause", false, "")
	fs.BoolVar(&cfg.Sound, "sound", false, "")
	fs.BoolVar(&cfg.LowPriority, "low-priority", false, "")
	fs.BoolVar(&cfg.HWDecode, "hw-decode", false, "")
	fs.StringVar(&cfg.WorkLocal, "work-local", "", "")
	fs.StringVar(&cfg.Scale, "scale", "", "")
	fs.StringVar(&cfg.Denoise, "denoise", "", "")
	fs.IntVar(&cfg.SampleSecs, "sample", 0, "")
	fs.StringVar(&cfg.SampleOriginal, "sample-original", "", "")
	sampleStartArg := fs.String("sample-start", "", "")
	fs.StringVar(&cfg.Ffmpeg, "ffmpeg", "", "")
	fs.StringVar(&cfg.Ffprobe, "ffprobe", "", "")
	fs.StringVar(&cfg.DoviTool, "dovi-tool", "", "")
	fs.StringVar(&cfg.MP4Box, "mp4box", "", "")
	fs.StringVar(&cfg.HDR10Plus, "hdr10plus-tool", "", "")

	// Allow flags and positional arguments in any order.
	var pos []string
	args := os.Args[1:]
	sawFlag := false
	for len(args) > 0 {
		before := len(args)
		if err := fs.Parse(args); err != nil {
			return nil, false
		}
		rest := fs.Args()
		if len(rest) != before {
			sawFlag = true
		}
		if len(rest) == 0 {
			break
		}
		pos = append(pos, rest[0])
		args = rest[1:]
	}
	if *showVersion {
		fmt.Printf("Keep the Light %s\n", version)
		os.Exit(0)
	}
	if len(pos) == 0 || len(pos) > 2 {
		fs.Usage()
		cfg.Pause = len(os.Args) == 1 // double-clicked
		return cfg, false
	}
	cfg.Input = pos[0]
	if len(pos) == 2 {
		cfg.Output = pos[1]
	}
	if len(os.Args) == 2 && !sawFlag {
		cfg.Pause = true // drag-and-drop: keep the window open
	}
	cfg.Codec = strings.ToLower(cfg.Codec)
	cfg.DV = strings.ToLower(cfg.DV)
	if cfg.Codec != "x265" && cfg.Codec != "nvenc" && cfg.Codec != "qsv" {
		fmt.Fprintln(os.Stderr, "error: -codec must be x265, nvenc or qsv")
		return cfg, false
	}
	if cfg.DV != "auto" && cfg.DV != "drop" {
		fmt.Fprintln(os.Stderr, "error: -dv must be auto or drop")
		return cfg, false
	}
	if sc, ok := normScale(cfg.Scale); ok {
		cfg.Scale = sc
	} else {
		fmt.Fprintln(os.Stderr, "error: -scale must be 2160p, 1440p, 1080p or 720p")
		return cfg, false
	}
	if cfg.Audio == "opus" {
		cfg.Audio = "libopus"
	}
	if cfg.Denoise = strings.ToLower(cfg.Denoise); cfg.Denoise == "off" {
		cfg.Denoise = ""
	}
	if !validDenoise(cfg.Denoise) {
		fmt.Fprintln(os.Stderr, "error: -denoise must be light, medium, strong or hq")
		return cfg, false
	}
	if cfg.SampleSecs < 0 || cfg.SampleSecs > 600 {
		fmt.Fprintln(os.Stderr, "error: -sample must be between 1 and 600 seconds")
		return cfg, false
	}
	if st, err := parseTimeArg(*sampleStartArg); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return cfg, false
	} else {
		cfg.SampleStart = st
	}
	var err error
	if cfg.AudioSel, err = parseAudioSel(*audioTracks); err == nil {
		cfg.SubSel, err = parseSubSel(*subTracks)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return cfg, false
	}
	switch d := strings.ToLower(*subDefault); d {
	case "":
	case "none":
		cfg.SubDefault = -1
	default:
		n, err := strconv.Atoi(d)
		if err != nil || n < 1 {
			fmt.Fprintln(os.Stderr, "error: -sub-default must be a track number or none")
			return cfg, false
		}
		cfg.SubDefault = n
	}
	cfg.AudioLangs, cfg.SubLangs = parseLangs(*audioLang), parseLangs(*subLang)
	if cfg.Meta, err = parseMetaJSON(*metaJSON); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return cfg, false
	}
	if title != nil || *noChapters || *coverImg != "" || *noCover {
		if cfg.Meta == nil {
			cfg.Meta = &MetaEdit{}
		}
		if title != nil {
			if cfg.Meta.Tags == nil {
				cfg.Meta.Tags = map[string]string{}
			}
			cfg.Meta.Tags["title"] = *title
		}
		if *noChapters {
			cfg.Meta.Chapters = &ChapterEdit{Remove: true}
		}
		cfg.Meta.Cover, cfg.Meta.RemoveCover = *coverImg, *noCover
		if err := cfg.Meta.validate(); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return cfg, false
		}
	}
	if cfg.MetaOnly {
		cfg.CopyVideo, cfg.Audio = true, "copy"
	}
	if cfg.ReplaceOriginal && !cfg.MetaOnly {
		fmt.Fprintln(os.Stderr, "error: -replace-original works only with -metadata-only")
		return cfg, false
	}
	if cfg.AudioChannels = strings.ToLower(cfg.AudioChannels); !validChannels(cfg.AudioChannels, false) {
		fmt.Fprintln(os.Stderr, "error: -audio-channels must be 5.1 or stereo")
		return cfg, false
	}
	switch cfg.SubMode = strings.ToLower(cfg.SubMode); cfg.SubMode {
	case "", "forced", "none":
	case "all":
		cfg.SubMode = ""
	default:
		fmt.Fprintln(os.Stderr, "error: -subs must be all, forced or none")
		return cfg, false
	}
	switch cfg.SubsToSRT = strings.ToLower(cfg.SubsToSRT); cfg.SubsToSRT {
	case "", "replace", "add":
	default:
		fmt.Fprintln(os.Stderr, "error: -subs-to-srt must be replace or add")
		return cfg, false
	}
	if *targetSize != "" {
		n, err := parseTargetSize(*targetSize)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return cfg, false
		}
		cfg.TargetBytes = n
	}
	if cfg.Retries < 0 || cfg.Retries > 5 {
		fmt.Fprintln(os.Stderr, "error: -size-retries must be between 0 and 5")
		return cfg, false
	}
	if cfg.ReplaceOriginal { // written next to the original first, then put in its place
		ext := strings.ToLower(filepath.Ext(cfg.Output))
		if cfg.Output == "" {
			if ext = strings.ToLower(filepath.Ext(cfg.Input)); ext != ".mkv" && ext != ".mp4" {
				ext = ".mkv"
			}
		}
		cfg.replaceFinal = replacedName(cfg.Input, ext)
		cfg.Output = partialName(cfg.replaceFinal)
	}
	if cfg.Output == "" {
		ext := filepath.Ext(cfg.Input)
		cfg.Output = strings.TrimSuffix(cfg.Input, ext) + cleanTag(*tag) + ".mkv"
	}
	return cfg, true
}

func main() {
	// "-gui [file]" / "-server [file]": open the window (both programs).
	// Keep the Light.exe also opens it with no arguments or a single dragged-on
	// file. Anything else is command-line mode.
	args := os.Args[1:]
	switch {
	case len(args) >= 1 && (args[0] == "-gui" || args[0] == "-server"):
		os.Exit(runGUI(args[1:], args[0] == "-server"))
	case !cliBuild && len(args) == 0:
		os.Exit(runGUI(nil, false))
	case !cliBuild && allExistingFiles(args):
		os.Exit(runGUI(args, false)) // one or more videos dropped on the exe
	}
	if !attachParentConsole() {
		messageBox(0, "Keep the Light.exe is the window version, so it has nowhere to show command-line output.\n\n"+
			"Use keepthelight_cli.exe for command-line use, or start Keep the Light.exe without arguments to open the window.", true)
		os.Exit(2)
	}
	os.Exit(run())
}

// allExistingFiles reports whether every argument is an existing file (so the
// exe was given dropped files, not command-line options).
func allExistingFiles(args []string) bool {
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			return false
		}
		if st, err := os.Stat(a); err != nil || st.IsDir() {
			return false
		}
	}
	return len(args) > 0
}

func run() int {
	cfg, ok := parseArgs()
	code := 0
	if !ok {
		code = 2
		if cfg != nil && len(os.Args) > 1 && cfg.Input != "" {
			code = 2
		}
	} else {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		if err := transcode(ctx, cfg); err != nil {
			fmt.Fprintln(os.Stderr, "\nERROR:", err)
			code = 1
		}
		if cfg.Sound && ctx.Err() == nil { // not after Ctrl+C
			sp := newGUIServer().soundPrefs() // the same choices as the window
			if id := map[bool]string{true: sp.Done, false: sp.Fail}[code == 0]; id != "" {
				playSoundFn(id, true)
			}
		}
	}
	if cfg != nil && cfg.Pause && hasConsole() {
		fmt.Print("\nPress Enter to exit...")
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
	}
	return code
}

func need(name, override string) (string, error) {
	p, err := findTool(name, override)
	if err != nil && !strings.HasSuffix(err.Error(), "not found") {
		return "", err // found, but it cannot start: say why
	}
	if err != nil {
		return "", fmt.Errorf("%s not found. Put %s next to Keep the Light.exe (or in a \"tools\" folder), add it to PATH, or pass its path with a flag", name, name)
	}
	return p, nil
}

func transcode(ctx context.Context, cfg *Config) (retErr error) {
	if _, err := os.Stat(cfg.Input); err != nil {
		return fmt.Errorf("input file: %w", err)
	}
	if abs1, e1 := filepath.Abs(cfg.Input); e1 == nil {
		if abs2, e2 := filepath.Abs(cfg.Output); e2 == nil && strings.EqualFold(abs1, abs2) {
			return fmt.Errorf("output would overwrite the input file")
		}
	}
	ffmpeg, err := need("ffmpeg", cfg.Ffmpeg)
	if err != nil {
		return err
	}
	ffprobe, err := need("ffprobe", cfg.Ffprobe)
	if err != nil {
		return err
	}
	cfg.Ffmpeg, cfg.Ffprobe = ffmpeg, ffprobe
	// Two-pass encodes and the Dolby Vision base layer need every source frame exactly
	// once: a duplicated or dropped frame breaks pass 2 and misaligns the per-frame RPU.
	cfg.vsyncArgs = []string{"-fps_mode", "passthrough"}
	if !supportsFpsMode(ctx, ffmpeg) {
		cfg.vsyncArgs = []string{"-vsync", "passthrough"}
	}
	cfg.cuesFront = supportsCuesToFront(ctx, ffmpeg)
	// child processes may run in the temp folder (x265 two-pass), so paths must be absolute
	for _, f := range []*string{&cfg.Input, &cfg.Output, &cfg.Tmp, &cfg.replaceFinal} {
		if *f != "" {
			if a, err := filepath.Abs(*f); err == nil {
				*f = a
			}
		}
	}

	var origSize int64 // for "space saved"
	if st, err := os.Stat(cfg.Input); err == nil {
		origSize = st.Size()
	}
	defer func() { // a test's sample
		for _, f := range cfg.cleanup {
			removeTemp(f)
		}
	}()
	if cfg.SampleSecs > 0 {
		if err := prepareSample(ctx, cfg); err != nil {
			return err
		}
	}
	lw, err := startLocal(cfg)
	if err != nil {
		return err
	}
	if lw != nil {
		defer func() {
			if retErr != nil && lw.dir != "" { // keep any finished work
				lw.keep = true
				fmt.Fprintln(os.Stderr, "\nThe local working folder was KEPT, so finished work is not lost:\n  "+lw.dir)
			}
			lw.cleanup()
		}()
	}
	if !cfg.CopyVideo {
		if name := map[string]string{"x265": "libx265", "nvenc": "hevc_nvenc", "qsv": "hevc_qsv"}[cfg.Codec]; name != "" && !hasEncoder(ctx, ffmpeg, name) {
			return fmt.Errorf("this ffmpeg build does not include the %s encoder; use a full ffmpeg build or pick another encoder", name)
		}
	}
	p, err := probeFile(ctx, ffprobe, cfg.Input)
	if err != nil {
		return err
	}
	fmt.Println("Source:", p.Summary())
	plan, err := makePlan(cfg, p)
	if err != nil {
		return err
	}
	if err := chooseTracks(ctx, ffprobe, cfg, plan); err != nil {
		return err
	}
	if cfg.TargetBytes > 0 {
		var srcBytes int64
		if st, err := os.Stat(cfg.Input); err == nil {
			srcBytes = st.Size()
		}
		sp, err := planSize(ctx, cfg, p, plan, srcBytes)
		if err != nil {
			return err
		}
		plan.Size = sp
		plan.Warnings = append(plan.Warnings, sp.Warnings...)
	}
	mode := describeMode(cfg, plan)
	fmt.Println("Mode:  ", mode)
	if plan.Size != nil {
		fmt.Println(plan.Size.Describe())
	}
	if plan.Quality != nil && plan.Size == nil {
		fmt.Println("Quality guide:", plan.Quality.Text, "(rough rule of thumb)")
	}
	for _, n := range plan.Notes {
		fmt.Println("Note:", n)
	}
	for _, w := range plan.Warnings {
		fmt.Println("WARNING:", w)
	}

	if cfg.LowPriority {
		if err := setOwnLowPriority(); err != nil {
			fmt.Println("WARNING: could not lower the priority:", err)
		}
	}
	r := &runner{ctx: ctx, dry: cfg.DryRun, verbose: cfg.Verbose}
	if (cfg.Codec == "nvenc" || cfg.Codec == "qsv") && !cfg.CopyVideo && !cfg.DryRun {
		if problem := encoderProblem(ctx, ffmpeg, cfg.Codec); problem != "" {
			return fmt.Errorf("%s", problem)
		}
		g, msg := chooseGPUPath(ctx, cfg, plan)
		cfg.gpu = g
		if msg != "" {
			fmt.Println(msg)
		}
	}
	if cfg.HWDecode && !cfg.CopyVideo && cfg.gpu == nil {
		switch {
		case cfg.DryRun:
			cfg.hwaccel = hwaccelArgs()
		default:
			args, msg := checkHWDecode(ctx, ffmpeg, cfg.Input)
			cfg.hwaccel = args
			fmt.Println(msg)
		}
	}
	if cfg.Denoise == "hq" && !cfg.CopyVideo && !cfg.DryRun {
		c, msg := chooseHQDenoise(ctx, ffmpeg, cfg.Input, cfg.hwaccel)
		cfg.hqDenoise = &c
		fmt.Println(msg)
	} else if cfg.Denoise != "" && !cfg.CopyVideo {
		fmt.Println("Noise reduction:", denoiseNames[cfg.Denoise])
	}
	if !cfg.DryRun {
		defer preventSleep()() // a long encode must not be stopped by the PC going to sleep
	}
	markOK := func() {}

	// Temp dir (only really created when we need it).
	tmpBase := cfg.Tmp
	if tmpBase == "" {
		tmpBase = filepath.Dir(cfg.Output)
	}
	if !cfg.DryRun {
		cleanLeftovers(tmpBase) // what earlier jobs could not delete
	}
	needTmp := plan.UseDV || plan.UseHDR10Plus || plan.Size != nil || metaNeedsFiles(cfg, plan) || plan.needsOCR()
	tmp := filepath.Join(tmpBase, ".keepthelight-tmp")
	if needTmp && !cfg.DryRun {
		d, err := os.MkdirTemp(tmpBase, ".keepthelight-")
		if err != nil {
			return fmt.Errorf("cannot create temp folder: %w", err)
		}
		tmp = d
		succeeded := false
		defer func() {
			switch {
			case !succeeded:
				fmt.Fprintln(os.Stderr, "\nIntermediate files were KEPT so finished work is not lost:\n  "+tmp)
			case !cfg.Keep:
				removeTemp(tmp)
			}
		}()
		markOK = func() { succeeded = true }
	}

	if err := prepareOCR(r, cfg, plan, tmp); err != nil {
		return err
	}
	if plan.UseDV {
		err = runDV(r, cfg, p, plan, tmp)
	} else {
		err = runSimple(r, cfg, p, plan, tmp)
	}
	if err != nil {
		return err
	}
	markOK()
	if cfg.DryRun {
		return nil
	}
	verify(ctx, cfg, plan)
	reportTarget(cfg)
	if lw != nil {
		if err := lw.finish(cfg); err != nil {
			return err
		}
	}
	if cfg.replaceFinal != "" {
		lines, err := replaceOriginal(ctx, ffprobe, cfg.Input, cfg.Output, cfg.replaceFinal)
		if err != nil {
			return err
		}
		for _, l := range lines {
			fmt.Println(l)
		}
		cfg.Output = cfg.replaceFinal
	}
	if cfg.sample != "" { // a test: measure it against the same stretch of the original
		fmt.Println("\nMeasuring the test's quality")
		w, h := p.Width, p.Height
		if plan.ScaleW > 0 {
			w, h = plan.ScaleW, plan.ScaleH
		}
		if line, err := scoreTest(ctx, cfg.Ffmpeg, cfg.Output, cfg.sample, w, h, cfg.Denoise); err == nil {
			fmt.Println(line)
		} else {
			fmt.Println("WARNING: could not measure the test's quality:", err)
		}
	}
	keepOriginalClip(cfg)
	if cfg.sample == "" && origSize > 0 {
		if st, err := os.Stat(cfg.Output); err == nil {
			fmt.Println("Size:", savedText(origSize, st.Size()))
		}
	}
	fmt.Println("\nDone:", cfg.Output)
	return nil
}

func runSimple(r *runner, cfg *Config, p *Probe, plan *Plan, tmp string) error {
	hdr10plusJSON := ""
	if plan.UseHDR10Plus {
		tool, err := findTool("hdr10plus_tool", cfg.HDR10Plus)
		if err != nil {
			fmt.Println("WARNING: source has HDR10+ dynamic metadata but hdr10plus_tool was not found; output will be plain HDR10")
			plan.UseHDR10Plus = false
		} else {
			hdr10plusJSON = filepath.Join(tmp, "hdr10plus.json")
			fmt.Println("\n[1/2] Extracting HDR10+ metadata")
			if err := r.pipe(cfg.Ffmpeg, []string{"-v", "error", "-i", cfg.Input, "-map", "0:v:0", "-c:v", "copy", "-bsf:v", "hevc_mp4toannexb", "-f", "hevc", "-"},
				tool, []string{"extract", "-i", "-", "-o", hdr10plusJSON}); err != nil {
				return err
			}
		}
	}
	_, warns := buildVideoArgs(cfg, p, plan, hdr10plusJSON)
	for _, w := range warns {
		fmt.Println("WARNING:", w)
	}
	mp4 := isMP4(cfg.Output)
	meta, err := prepareMeta(r, cfg, plan, 0, 1, mp4, tmp)
	if err != nil {
		return err
	}
	// mk builds the ffmpeg command for one pass (0 = ordinary single pass).
	mk := func(pass int, out string) []string {
		cfg.Pass = pass
		vargs, _ := buildVideoArgs(cfg, p, plan, hdr10plusJSON)
		args := append([]string{"-hide_banner", "-y"}, cfg.inputPrefix()...)
		args = append(args, "-i", cfg.Input)
		if pass != 1 {
			args = append(args, meta.Inputs...)
			args = append(args, ocrInputs(plan.Subs, 1+countInputs(meta.Inputs))...)
		}
		args = append(args, "-map", "0:v:0")
		if pass > 0 {
			args = append(args, cfg.vsyncArgs...)
		}
		if pass == 1 { // analysis pass: video only, nothing written
			args = append(args, "-an", "-sn", "-dn")
			args = append(args, vargs...)
			return append(args, "-f", "null", "-")
		}
		args = append(args, audioArgs(plan.Audio, 0)...)
		args = append(args, subtitleArgs(plan.Subs, 0)...)
		args = append(args, meta.Args...)
		args = append(args, vargs...)
		if mp4 {
			args = append(args, "-tag:v", "hvc1", "-movflags", "+faststart")
		} else {
			args = append(args, mkvIndexArgs(cfg, p.Duration)...)
		}
		return append(args, out)
	}

	tp := plan.Size
	switch {
	case tp == nil:
		fmt.Println("\nEncoding")
		return r.run(cfg.Ffmpeg, mk(0, cfg.Output)...)

	case cfg.Codec == "x265":
		r.dir = tmp // x265 keeps its first-pass statistics in the working directory
		defer func() { r.dir = "" }()
		fmt.Println("\nEncoding (pass 1 of 2)")
		if err := r.run(cfg.Ffmpeg, mk(1, "")...); err != nil {
			return err
		}
		fmt.Println("\nEncoding (pass 2 of 2)")
		return r.run(cfg.Ffmpeg, mk(2, cfg.Output)...)

	default: // hardware encoder: measure, and retry with a corrected bitrate if needed
		ext := filepath.Ext(cfg.Output)
		best, all, err := runAttempts(tp, cfg.Retries, func(n, kbps int) (int64, string, error) {
			cfg.Bitrate = fmt.Sprintf("%dk", kbps)
			out := filepath.Join(tmp, fmt.Sprintf("attempt%d%s", n+1, ext))
			fmt.Printf("\nEncoding (attempt %d of up to %d)\n", n+1, cfg.Retries+1)
			if err := r.run(cfg.Ffmpeg, mk(0, out)...); err != nil {
				return 0, "", err
			}
			sz, err := fileSizeFor(r, out, tp.Target)
			return sz, out, err
		}, logf)
		if err != nil {
			return err
		}
		return finishAttempts(r, best, all, cfg.Output)
	}
}

func runDV(r *runner, cfg *Config, p *Probe, plan *Plan, tmp string) error {
	dovi, err := need("dovi_tool", cfg.DoviTool)
	if err != nil {
		return err
	}
	mp4box, err := need("MP4Box", cfg.MP4Box)
	if err != nil {
		return err
	}
	fmt.Println("Using", dovi, "and", mp4box)
	rpu := filepath.Join(tmp, "rpu.bin")
	base := filepath.Join(tmp, "video.hevc")
	injected := filepath.Join(tmp, "video_dv.hevc")
	dvmp4 := filepath.Join(tmp, "video_dv.mp4")

	fmt.Println("\n[1/5] Extracting Dolby Vision RPU")
	dargs := []string{}
	if plan.RPUMode != 0 {
		dargs = append(dargs, "-m", fmt.Sprint(plan.RPUMode))
	}
	dargs = append(dargs, "extract-rpu", "-", "-o", rpu)
	if err := r.pipe(cfg.Ffmpeg, []string{"-v", "error", "-i", cfg.Input, "-map", "0:v:0", "-c:v", "copy", "-bsf:v", "hevc_mp4toannexb", "-f", "hevc", "-"},
		dovi, dargs); err != nil {
		return err
	}

	if plan.ScaleW > 0 {
		scaled, err := scaleRPU(r, dovi, rpu, tmp, plan)
		if err != nil {
			return err
		}
		rpu = scaled
	}

	_, warns := buildVideoArgs(cfg, p, plan, "")
	for _, w := range warns {
		fmt.Println("WARNING:", w)
	}
	// mk builds the ffmpeg command that writes the video-only base layer.
	mk := func(pass int, out string) []string {
		cfg.Pass = pass
		vargs, _ := buildVideoArgs(cfg, p, plan, "")
		args := append([]string{"-hide_banner", "-y"}, cfg.inputPrefix()...)
		args = append(args, "-i", cfg.Input, "-map", "0:v:0", "-an", "-sn", "-dn")
		args = append(args, cfg.vsyncArgs...)
		args = append(args, vargs...)
		if pass == 1 {
			return append(args, "-f", "null", "-")
		}
		return append(args, "-f", "hevc", out)
	}
	inject := func(from, to string) error {
		fmt.Println("\n[3/5] Injecting RPU into the new stream")
		return r.run(dovi, "inject-rpu", "-i", from, "--rpu-in", rpu, "-o", to)
	}

	tp := plan.Size
	switch {
	case tp == nil:
		fmt.Println("\n[2/5] Encoding HDR base layer")
		if err := r.run(cfg.Ffmpeg, mk(0, base)...); err != nil {
			return err
		}
		if err := inject(base, injected); err != nil {
			return err
		}

	case cfg.Codec == "x265":
		r.dir = tmp
		fmt.Println("\n[2/5] Encoding HDR base layer (pass 1 of 2)")
		err := r.run(cfg.Ffmpeg, mk(1, "")...)
		if err == nil {
			fmt.Println("\n[2/5] Encoding HDR base layer (pass 2 of 2)")
			err = r.run(cfg.Ffmpeg, mk(2, base)...)
		}
		r.dir = ""
		if err != nil {
			return err
		}
		if err := inject(base, injected); err != nil {
			return err
		}

	default: // hardware encoder: each attempt = encode + inject; the injected size is what we measure
		best, all, err := runAttempts(tp, cfg.Retries, func(n, kbps int) (int64, string, error) {
			cfg.Bitrate = fmt.Sprintf("%dk", kbps)
			b := filepath.Join(tmp, fmt.Sprintf("base%d.hevc", n+1))
			inj := filepath.Join(tmp, fmt.Sprintf("video_dv%d.hevc", n+1))
			fmt.Printf("\n[2/5] Encoding HDR base layer (attempt %d of up to %d)\n", n+1, cfg.Retries+1)
			if err := r.run(cfg.Ffmpeg, mk(0, b)...); err != nil {
				return 0, "", err
			}
			if err := inject(b, inj); err != nil {
				return 0, "", err
			}
			if !r.dry {
				removeTemp(b)
			}
			sz, err := fileSizeFor(r, inj, tp.Target-tp.NonVideo())
			return sz + tp.NonVideo(), inj, err
		}, logf)
		if err != nil {
			return err
		}
		if err := finishAttempts(r, best, all, injected); err != nil {
			return err
		}
	}

	fmt.Println("\n[4/5] Packaging Dolby Vision video track")
	fps := p.AvgFPS
	if fps == "" || fps == "0/0" {
		fps = p.RFPS
	}
	src := injected + ":dv-profile=" + plan.DVProfile + ":hdr=none"
	if fps != "" && fps != "0/0" {
		src += ":fps=" + fps
	}
	if err := r.run(mp4box, "-add", src, "-new", dvmp4); err != nil {
		return err
	}

	fmt.Println("\n[5/5] Muxing with original audio/subtitles/chapters")
	mp4 := isMP4(cfg.Output)
	meta, err := prepareMeta(r, cfg, plan, 1, 2, mp4, tmp)
	if err != nil {
		return err
	}
	margs := []string{"-hide_banner", "-y", "-i", dvmp4, "-i", cfg.Input}
	margs = append(margs, meta.Inputs...)
	margs = append(margs, ocrInputs(plan.Subs, 2+countInputs(meta.Inputs))...)
	margs = append(margs, "-map", "0:v:0")
	margs = append(margs, audioArgs(plan.Audio, 1)...)
	margs = append(margs, subtitleArgs(plan.Subs, 1)...)
	margs = append(margs, meta.Args...)
	margs = append(margs, "-c:v", "copy")
	if mp4 {
		margs = append(margs, "-tag:v", "dvh1", "-movflags", "+faststart")
	} else {
		margs = append(margs, mkvIndexArgs(cfg, p.Duration)...)
	}
	margs = append(margs, cfg.Output)
	return r.run(cfg.Ffmpeg, margs...)
}

// verify re-probes the output and reports whether the HDR signalling survived.
func verify(ctx context.Context, cfg *Config, plan *Plan) {
	if cfg.CopyVideo {
		return
	}
	o, err := probeFile(ctx, cfg.Ffprobe, cfg.Output)
	if err != nil {
		fmt.Println("WARNING: could not verify output:", err)
		return
	}
	fmt.Println("\nOutput:", o.Summary())
	switch plan.Kind {
	case "hdr10":
		if o.Transfer != "smpte2084" {
			fmt.Println("WARNING: output is not tagged PQ (smpte2084) - HDR may not be recognised")
		}
		if o.Mastering == nil {
			fmt.Println("NOTE: no mastering-display metadata detected in the output")
		}
	case "hlg":
		if o.Transfer != "arib-std-b67" {
			fmt.Println("WARNING: output is not tagged HLG (arib-std-b67)")
		}
	}
	if plan.UseDV && o.DV == nil {
		fmt.Println("WARNING: ffprobe did not report a Dolby Vision configuration in the output. " +
			"Check with MediaInfo; if it is missing, try the .mp4 output, which is the most reliable container for DV.")
	}
}

func describeMode(cfg *Config, plan *Plan) string {
	m := describeHDR(cfg, plan)
	if plan.ScaleW > 0 {
		m += fmt.Sprintf(", resized %dx%d -> %dx%d", plan.SrcW, plan.SrcH, plan.ScaleW, plan.ScaleH)
	}
	return m
}

func describeHDR(cfg *Config, plan *Plan) string {
	switch {
	case cfg.MetaOnly:
		return "metadata only (every kept stream copied unchanged)"
	case cfg.CopyVideo:
		return "video stream copy"
	case plan.UseDV:
		return fmt.Sprintf("Dolby Vision %s (RPU extract -> encode -> inject -> mux)", plan.DVProfile)
	case plan.UseHDR10Plus:
		return "HDR10+ (dynamic metadata preserved)"
	}
	return plan.Kind
}

func hasEncoder(ctx context.Context, ffmpeg, name string) bool {
	out, err := toolCommand(ctx, ffmpeg, "-hide_banner", "-encoders").Output()
	if err != nil {
		return true // cannot tell; let the real run report any problem
	}
	return strings.Contains(string(out), " "+name+" ")
}

// cleanTag removes characters a Windows file name cannot contain.
func cleanTag(s string) string {
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune(`\/:*?"<>|`, r) || r < 32 {
			return -1
		}
		return r
	}, s)
}

// supportsCuesToFront reports whether this ffmpeg can write an MKV's index
// (cues) at the start of the file (FFmpeg 5.1 and newer).
func supportsCuesToFront(ctx context.Context, ffmpeg string) bool {
	out, err := toolCommand(ctx, ffmpeg, "-hide_banner", "-h", "muxer=matroska").Output()
	return err == nil && strings.Contains(string(out), "cues_to_front")
}

// mkvIndexArgs puts the seek index at the front of an MKV, like MP4's
// "faststart", so a player or a Plex server can seek and resume without first
// reading the end of the file. Space for it is reserved up front (generously:
// about 64 bytes per second of video, a few hundred KB for a film); if that is
// ever not enough, ffmpeg moves the data to make room instead, so the index is
// never lost.
func mkvIndexArgs(cfg *Config, duration float64) []string {
	if !cfg.cuesFront {
		return nil
	}
	reserve := max(64<<10, int(duration*64))
	return []string{"-reserve_index_space", strconv.Itoa(reserve), "-cues_to_front", "1"}
}

// supportsFpsMode reports whether this ffmpeg knows -fps_mode (FFmpeg 5.1 and newer).
func supportsFpsMode(ctx context.Context, ffmpeg string) bool {
	out, err := toolCommand(ctx, ffmpeg, "-hide_banner", "-h", "full").Output()
	if err != nil {
		return true
	}
	return strings.Contains(string(out), "-fps_mode")
}
