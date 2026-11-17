package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// ---------- the thorough check (optional) ----------
//
// The usual checks of a finished file look at its start and its labels. The
// thorough check reads the whole new file through once: every frame must
// decode, the picture and each sound track must run to the end, the length
// must match the original's, and Dolby Vision or HDR10+ data must be there for
// every frame. It takes a few minutes for a film, so it is a setting, off to
// begin with. A problem is a warning: the file is kept, for you to look at.

// deepResult is what the check found.
type deepResult struct {
	Frames   int
	Problems []string
	Good     []string // what was confirmed, for the "passed" line
}

func (d deepResult) line() string {
	if len(d.Problems) > 0 {
		return "Thorough check: PROBLEMS FOUND: " + strings.Join(d.Problems, "; ")
	}
	return "Thorough check: passed (" + strings.Join(d.Good, "; ") + ")"
}

var reProgressFrame = regexp.MustCompile(`(?m)^frame=\s*(\d+)`)

// deepCheck reads the whole of out. src is the original's probe (nil: no
// length to compare with); fps the new file's frame rate.
func deepCheck(ctx context.Context, cfg *Config, plan *Plan, src *Probe, out string, tmp string) deepResult {
	var res deepResult
	o, err := probeFile(ctx, cfg.Ffprobe, out)
	if err != nil {
		res.Problems = append(res.Problems, "the new file cannot be read ("+err.Error()+")")
		return res
	}
	// 1. every frame of the picture and every sound track decodes
	frames, errs, err := decodeAll(ctx, cfg.Ffmpeg, out)
	if err != nil {
		res.Problems = append(res.Problems, err.Error())
		return res
	}
	res.Frames = frames
	switch {
	case frames == 0:
		res.Problems = append(res.Problems, "no picture could be decoded")
	case len(errs) > 0:
		more := ""
		if len(errs) > 1 {
			more = fmt.Sprintf(" (and %d more)", len(errs)-1)
		}
		res.Problems = append(res.Problems, "errors while decoding: "+errs[0]+more)
	default:
		res.Good = append(res.Good, fmt.Sprintf("%s frames and all sound decode without errors", thousands(frames)))
	}
	// 2. the length matches the original's, and the frames cover it
	if src != nil && src.Duration > 0 {
		if tol := math.Max(2, src.Duration*0.002); math.Abs(src.Duration-o.Duration) > tol {
			res.Problems = append(res.Problems, fmt.Sprintf("the new file is %s long, the original %s", formatClock(o.Duration), formatClock(src.Duration)))
		} else {
			res.Good = append(res.Good, "the length matches the original")
		}
	}
	if fps := outFPS(o, nil); fps > 0 && o.Duration > 0 && frames > 0 {
		if got := float64(frames) / fps; got < o.Duration-math.Max(2, o.Duration*0.01) {
			res.Problems = append(res.Problems, fmt.Sprintf("the picture stops early: %s of frames in a file %s long", formatClock(got), formatClock(o.Duration)))
		}
	}
	// 3. each sound track runs to the end
	for _, a := range audioEnds(ctx, cfg.Ffprobe, out, o.Duration) {
		if a.end < o.Duration-5 {
			res.Problems = append(res.Problems, fmt.Sprintf("sound track %d stops at %s (the file is %s long)", a.n, formatClock(a.end), formatClock(o.Duration)))
		}
	}
	// 4. Dolby Vision / HDR10+ data on every frame
	if o.DV != nil && frames > 0 {
		dovi, derr := findTool("dovi_tool", cfg.DoviTool)
		switch n, err := countRPU(ctx, cfg.Ffmpeg, dovi, out, tmp); {
		case derr != nil:
			res.Good = append(res.Good, "Dolby Vision data not counted (dovi_tool not found)")
		case err != nil:
			res.Problems = append(res.Problems, "the Dolby Vision data could not be read ("+err.Error()+")")
		case n != frames:
			res.Problems = append(res.Problems, fmt.Sprintf("Dolby Vision data for %s of %s frames", thousands(n), thousands(frames)))
		default:
			res.Good = append(res.Good, "Dolby Vision data on every frame")
		}
	} else if plan != nil && plan.UseDV {
		res.Problems = append(res.Problems, "the new file has no Dolby Vision")
	}
	if plan != nil && plan.UseHDR10Plus && !o.HDR10Plus {
		res.Problems = append(res.Problems, "the new file has no HDR10+ data")
	} else if o.HDR10Plus {
		res.Good = append(res.Good, "HDR10+ data present")
	}
	return res
}

// decodeAll decodes the picture and every sound track of a file to nowhere,
// returning the number of frames and the errors FFmpeg reported.
func decodeAll(ctx context.Context, ffmpeg, path string) (frames int, errs []string, err error) {
	cmd := toolCommand(ctx, ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-nostats", "-progress", "pipe:1",
		"-i", path, "-map", "0:v:0", "-map", "0:a?", "-f", "null", "-")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	runErr := cmd.Run()
	if ctx.Err() != nil {
		return 0, nil, ctx.Err()
	}
	if m := reProgressFrame.FindAllStringSubmatch(stdout.String(), -1); m != nil {
		frames, _ = strconv.Atoi(m[len(m)-1][1])
	}
	seen := map[string]bool{}
	sc := bufio.NewScanner(&stderr)
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if l == "" || seen[l] {
			continue
		}
		seen[l] = true
		errs = append(errs, l)
	}
	if runErr != nil && len(errs) == 0 {
		errs = append(errs, "FFmpeg stopped: "+runErr.Error())
	}
	return frames, errs, nil
}

type audioEnd struct {
	n   int // 1-based among the sound tracks
	end float64
}

// audioEnds reads the last half-minute of the file: where each sound track's
// last piece is.
func audioEnds(ctx context.Context, ffprobe, path string, duration float64) []audioEnd {
	if duration <= 0 {
		return nil
	}
	from := math.Max(0, duration-30)
	out, err := toolCommand(ctx, ffprobe, "-v", "error", "-read_intervals", strconv.FormatFloat(from, 'f', 0, 64)+"%",
		"-select_streams", "a", "-show_entries", "packet=stream_index,pts_time,duration_time", "-of", "csv=p=0", path).Output()
	if err != nil {
		return nil
	}
	order := []string{}
	ends := map[string]float64{}
	for _, l := range strings.Split(string(out), "\n") {
		f := strings.Split(strings.TrimSpace(l), ",")
		if len(f) < 2 {
			continue
		}
		t, err := strconv.ParseFloat(f[1], 64)
		if err != nil {
			continue
		}
		if len(f) > 2 {
			if d, err := strconv.ParseFloat(f[2], 64); err == nil {
				t += d
			}
		}
		if _, ok := ends[f[0]]; !ok {
			order = append(order, f[0])
		}
		ends[f[0]] = math.Max(ends[f[0]], t)
	}
	all, err := toolCommand(ctx, ffprobe, "-v", "error", "-select_streams", "a", "-show_entries", "stream=index", "-of", "csv=p=0", path).Output()
	if err != nil {
		return nil
	}
	var res []audioEnd
	for i, idx := range strings.Fields(string(all)) {
		idx = strings.TrimSuffix(idx, ",")
		e, ok := ends[idx]
		if !ok {
			e = from // nothing of it in the last half-minute
		}
		res = append(res, audioEnd{n: i + 1, end: e})
	}
	return res
}

var reRPUFrames = regexp.MustCompile(`Frames:\s*(\d+)`)

// countRPU counts the frames that carry Dolby Vision data.
func countRPU(ctx context.Context, ffmpeg, dovi, path, tmp string) (int, error) {
	if dovi == "" {
		return 0, fmt.Errorf("dovi_tool not found")
	}
	if tmp == "" {
		tmp = os.TempDir()
	}
	rpu := filepath.Join(tmp, "ktl-check-rpu.bin")
	defer removeTemp(rpu)
	c1 := toolCommand(ctx, ffmpeg, "-v", "error", "-i", path, "-map", "0:v:0", "-c:v", "copy", "-bsf:v", "hevc_mp4toannexb", "-f", "hevc", "-")
	c2 := toolCommand(ctx, dovi, "extract-rpu", "-", "-o", rpu)
	pipe, err := c1.StdoutPipe()
	if err != nil {
		return 0, err
	}
	c2.Stdin = pipe
	var e2 bytes.Buffer
	c2.Stderr = &e2
	if err := c1.Start(); err != nil {
		return 0, err
	}
	if err := c2.Start(); err != nil {
		_ = c1.Process.Kill()
		_ = c1.Wait()
		return 0, err
	}
	err2 := c2.Wait()
	if err2 != nil {
		_ = c1.Process.Kill()
	}
	_ = c1.Wait()
	if err2 != nil {
		return 0, fmt.Errorf("%s", firstNonEmpty(lastLine(e2.String()), err2.Error()))
	}
	out, err := toolCommand(ctx, dovi, "info", "-i", rpu, "-s").Output()
	if err != nil {
		return 0, err
	}
	m := reRPUFrames.FindStringSubmatch(string(out))
	if m == nil {
		return 0, fmt.Errorf("no frame count in dovi_tool's summary")
	}
	return strconv.Atoi(m[1])
}

func firstNonEmpty(a ...string) string {
	for _, s := range a {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}
