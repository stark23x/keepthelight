package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// extractRPU reads the Dolby Vision scene data (the RPU) out of the source.
//
// The usual way streams the video from FFmpeg straight into dovi_tool. A file
// with a glitch near the start (two frames with the same timestamp, which
// FFmpeg refuses to pass on) can make that fail, with dovi_tool reporting
// "Invalid PPS index". The second try gives every frame a plain running number
// as its timestamp (a raw stream needs none, so nothing is lost or reordered),
// writes the video to a temp file, and reads that. If that fails too, the
// message says what to do instead.
func extractRPU(r *runner, cfg *Config, dovi string, mode int, rpu, tmp string) error {
	dargs := func(in string) []string {
		a := []string{}
		if mode != 0 {
			a = append(a, "-m", fmt.Sprint(mode))
		}
		return append(a, "extract-rpu", in, "-o", rpu)
	}
	err := r.pipe(cfg.Ffmpeg, []string{"-v", "error", "-i", cfg.Input, "-map", "0:v:0", "-c:v", "copy", "-bsf:v", "hevc_mp4toannexb", "-f", "hevc", "-"},
		dovi, dargs("-"))
	if err == nil || r.ctx.Err() != nil {
		return err
	}
	fmt.Println("\nReading the Dolby Vision data failed (" + err.Error() + "); trying again more carefully")
	raw := filepath.Join(tmp, "source.hevc")
	defer func() { _ = os.Remove(raw) }()
	err2 := r.run(cfg.Ffmpeg, "-hide_banner", "-v", "error", "-y", "-fflags", "+genpts", "-i", cfg.Input, "-map", "0:v:0", "-c:v", "copy",
		"-bsf:v", "hevc_mp4toannexb,setts=pts=N:dts=N", "-f", "hevc", raw)
	if err2 == nil {
		_ = os.Remove(rpu)
		if err2 = r.run(dovi, dargs(raw)...); err2 == nil {
			fmt.Println("The second try worked.")
			return nil
		}
	}
	if r.ctx.Err() != nil {
		return err2
	}
	return fmt.Errorf("this file's video has a glitch (%v), so its Dolby Vision data cannot be read. "+
		"To convert it, set Dolby Vision to \"Drop (plain HDR10)\", or re-mux the file with MKVToolNix first and try again", err)
}
