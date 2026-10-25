package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ---------- GPU decoding ----------

// hwaccelArgs are the ffmpeg input options for decoding on the GPU. On Windows
// that is Direct3D 11 video decoding, which every current Intel, AMD and NVIDIA
// GPU supports, on the GPU that runs the main display (on a PC without a
// graphics card: the processor's built-in graphics). Decoded frames are copied
// back to memory, so the encoders and the resize work exactly as before.
func hwaccelArgs() []string {
	if runtime.GOOS == "windows" {
		return []string{"-hwaccel", "d3d11va"}
	}
	return []string{"-hwaccel", "auto"}
}

// checkHWDecode decodes a few frames on the GPU to find out whether it can
// decode this file. If not (no GPU decoder, or one that does not support the
// format, such as 4:2:2 or 12-bit video) the file is decoded on the CPU as
// usual. It returns the input options to use and a line for the log.
func checkHWDecode(ctx context.Context, ffmpeg, input string) ([]string, string) {
	hw := hwaccelArgs()
	args := append([]string{"-hide_banner", "-nostdin", "-v", "warning"}, hw...)
	args = append(args, "-i", input, "-map", "0:v:0", "-frames:v", "5", "-f", "null", "-")
	out, err := toolCommand(ctx, ffmpeg, args...).CombinedOutput()
	if reason := hwDecodeProblem(string(out), err); reason != "" {
		return nil, "GPU decoding: not available for this file (" + reason + "), so it is decoded on the CPU"
	}
	return hw, "GPU decoding: on (the CPU is left free for encoding)"
}

// hwDecodeProblem reads ffmpeg's output from the test decode. ffmpeg quietly
// falls back to the CPU when the GPU cannot decode a format, so its warnings
// are what tell the two apart.
func hwDecodeProblem(out string, err error) string {
	low := strings.ToLower(out)
	for _, s := range []string{"failed setup for format", "hwaccel initialisation returned error", "device creation failed",
		"failed to create", "no device available", "not supported by decoder", "hardware device setup failed"} {
		if strings.Contains(low, s) {
			return firstLine(out, s)
		}
	}
	if err != nil {
		if l := strings.TrimSpace(out); l != "" {
			return lastLine(l)
		}
		return err.Error()
	}
	return ""
}

func firstLine(out, needle string) string {
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(strings.ToLower(l), needle) {
			return strings.TrimSpace(l)
		}
	}
	return needle
}

func lastLine(s string) string {
	ls := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(ls[len(ls)-1])
}

// ---------- Dolby Vision active area ----------

// Dolby Vision's level 5 metadata ("active area") gives the size of the black
// bars in pixels, for example 276 rows top and bottom for a 2.39:1 film in a
// 3840x2160 frame. Everything else in the RPU (brightness analysis, display
// trims) does not depend on the picture size. When the video is resized, the
// offsets are scaled with it, shot by shot, so a film whose aspect ratio
// changes (IMAX scenes) keeps the right values for every scene.

type l5Preset struct {
	ID     int `json:"id"`
	Left   int `json:"left"`
	Right  int `json:"right"`
	Top    int `json:"top"`
	Bottom int `json:"bottom"`
}

// scaleRPU writes a copy of the RPU with the active area scaled to the new
// size, and returns its path (or the original one if it has no bars to scale).
func scaleRPU(r *runner, dovi, rpu, tmp string, plan *Plan) (string, error) {
	fmt.Println("\n[1/5] Adjusting Dolby Vision active area to the new size")
	exported := filepath.Join(tmp, "l5.json")
	edit := filepath.Join(tmp, "l5_scaled.json")
	out := filepath.Join(tmp, "rpu_scaled.bin")
	if err := r.run(dovi, "export", "-i", rpu, "-d", "level5="+exported); err != nil {
		return "", err
	}
	if r.dry {
		r.show(cmdline(dovi, []string{"editor", "-i", rpu, "-j", edit, "-o", out}))
		return out, nil
	}
	data, err := os.ReadFile(exported)
	if err != nil {
		return "", fmt.Errorf("reading the Dolby Vision active area: %w", err)
	}
	cfgJSON, changed, err := scaleL5(data, plan.SrcW, plan.SrcH, plan.ScaleW, plan.ScaleH)
	if err != nil {
		return "", err
	}
	if !changed {
		fmt.Println("No black bars recorded in the Dolby Vision data; nothing to adjust")
		return rpu, nil
	}
	if err := os.WriteFile(edit, cfgJSON, 0o644); err != nil {
		return "", err
	}
	if err := r.run(dovi, "editor", "-i", rpu, "-j", edit, "-o", out); err != nil {
		return "", err
	}
	return out, nil
}

// scaleL5 turns dovi_tool's level 5 export (an editor config) into an editor
// config with every preset scaled from sw x sh to dw x dh. changed is false
// when all offsets are zero (no bars), so there is nothing to edit.
func scaleL5(data []byte, sw, sh, dw, dh int) (cfg []byte, changed bool, err error) {
	var in struct {
		ActiveArea *struct {
			Presets []l5Preset      `json:"presets"`
			Edits   json.RawMessage `json:"edits"`
		} `json:"active_area"`
	}
	if err := json.Unmarshal(data, &in); err != nil {
		return nil, false, fmt.Errorf("reading the Dolby Vision active area: %w", err)
	}
	aa := in.ActiveArea
	if aa == nil || len(aa.Presets) == 0 || len(aa.Edits) == 0 || sw <= 0 || sh <= 0 {
		return nil, false, nil
	}
	sx := func(v int) int { return int(math.Round(float64(v) * float64(dw) / float64(sw))) }
	sy := func(v int) int { return int(math.Round(float64(v) * float64(dh) / float64(sh))) }
	out := make([]l5Preset, len(aa.Presets))
	for i, p := range aa.Presets {
		if p.Left|p.Right|p.Top|p.Bottom != 0 {
			changed = true
		}
		n := l5Preset{ID: p.ID, Left: sx(p.Left), Right: sx(p.Right), Top: sy(p.Top), Bottom: sy(p.Bottom)}
		// rounding must not leave bars that cover the whole picture
		if n.Left+n.Right >= dw {
			n.Left, n.Right = 0, 0
		}
		if n.Top+n.Bottom >= dh {
			n.Top, n.Bottom = 0, 0
		}
		out[i] = n
	}
	if !changed {
		return nil, false, nil
	}
	cfg, err = json.MarshalIndent(map[string]any{"active_area": map[string]any{
		"crop": false, "presets": out, "edits": aa.Edits,
	}}, "", "  ")
	return cfg, true, err
}
