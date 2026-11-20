package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ---------- when the graphics card stops responding ----------
//
// A graphics card can stop taking encodes partway through a queue: the driver
// crashed and recovered, a laptop turned its NVIDIA card off to save power, or
// the card is stuck until Windows restarts. FFmpeg then fails at once with
// "OpenEncodeSessionEx failed: no encode device" (NVENC) or an MFX session
// error (QuickSync), although the same settings worked a minute before.
//
// Keep the Light then waits a little and tries once more. If the card still
// does not answer, the job ends with exitGPUStuck and a plain message, and the
// queue pauses on that file instead of failing every file after it.

const exitGPUStuck = 3 // the command line's exit code when the graphics card stopped responding

var gpuRetryWait = 30 * time.Second

func init() { // tests shorten the wait in the jobs they start (KTL_GPU_RETRY_WAIT_MS)
	if ms, err := strconv.Atoi(os.Getenv("KTL_GPU_RETRY_WAIT_MS")); err == nil && ms >= 0 {
		gpuRetryWait = time.Duration(ms) * time.Millisecond
	}
}

// gpuStuckSigns are FFmpeg's words when the card itself does not answer (not
// when a setting is wrong, and not when the PC has no such card at all).
var gpuStuckSigns = []string{
	"openencodesessionex failed",
	"no encode device",
	"cuda_error_no_device",
	"cuda_error_unknown",
	"error creating a mfx session",
	"error initializing an mfx session",
	"error initializing an internal mfx session",
	"device creation failed",
}

func looksGPUStuck(out string) bool {
	low := strings.ToLower(out)
	for _, s := range gpuStuckSigns {
		if strings.Contains(low, s) {
			return true
		}
	}
	return false
}

// hwEncoderIn names the hardware encoder a command line uses ("" for none).
func hwEncoderIn(args []string) string {
	for _, a := range args {
		switch a {
		case "hevc_nvenc":
			return "nvenc"
		case "hevc_qsv":
			return "qsv"
		}
	}
	return ""
}

// gpuStuckError is the job's error when the card did not answer twice.
type gpuStuckError struct{ codec string }

func (e *gpuStuckError) Error() string {
	if e.codec == "qsv" {
		return "The Intel graphics (QuickSync) stopped responding, so this file could not be encoded (tried twice, " +
			gpuWaitWords() + " apart). Restarting Windows usually fixes this. The queue is paused on this file."
	}
	return "The NVIDIA graphics card stopped responding, so this file could not be encoded (tried twice, " +
		gpuWaitWords() + " apart). Restarting Windows usually fixes this. On a laptop, also check that it is plugged in " +
		"and not in a power-saving mode that turns the NVIDIA card off. The queue is paused on this file."
}

func gpuWaitWords() string {
	if s := int(gpuRetryWait.Seconds()); s >= 1 {
		return fmt.Sprintf("%d seconds", s)
	}
	return "a moment"
}

// gpuWatch passes a tool's error output through and notes whether it shows a
// graphics card that does not answer.
type gpuWatch struct {
	w    io.Writer
	mu   sync.Mutex
	tail string // the end of the last write, in case a sign is split between writes
	seen bool
}

func (g *gpuWatch) Write(p []byte) (int, error) {
	g.mu.Lock()
	s := g.tail + string(p)
	if looksGPUStuck(s) {
		g.seen = true
	}
	if len(s) > 64 {
		s = s[len(s)-64:]
	}
	g.tail = s
	g.mu.Unlock()
	return g.w.Write(p)
}

func (g *gpuWatch) stuck() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.seen
}

// withGPURetry runs one tool step. If it used a hardware encoder and failed
// because the card did not answer, it waits and runs the step once more; if
// the card still does not answer, the error says so plainly.
func withGPURetry(ctx context.Context, codec string, step func(stderr io.Writer) error, stderr io.Writer) error {
	w := &gpuWatch{w: stderr}
	err := step(w)
	if err == nil || codec == "" || !w.stuck() || ctx.Err() != nil {
		return err
	}
	fmt.Printf("\nThe graphics card did not answer. Waiting %s, then trying once more.\n", gpuWaitWords())
	select {
	case <-time.After(gpuRetryWait):
	case <-ctx.Done():
		return err
	}
	w = &gpuWatch{w: stderr}
	if err = step(w); err != nil && w.stuck() && ctx.Err() == nil {
		return &gpuStuckError{codec: codec}
	}
	return err
}

var encoderStuck sync.Map // codec -> true when the start-up check failed because the card did not answer

// checkEncoder is the start-up encoder check for a job. When it failed because
// the card did not answer (rather than because the PC has no such card), it
// waits and checks once more, and if the card still does not answer the error
// is a gpuStuckError, so the queue pauses.
func checkEncoder(ctx context.Context, ffmpeg, codec string) error {
	p := encoderProblem(ctx, ffmpeg, codec)
	if p == "" {
		return nil
	}
	if v, _ := encoderStuck.Load(codec); v != true {
		return fmt.Errorf("%s", p)
	}
	fmt.Printf("The graphics card did not answer. Waiting %s, then checking once more.\n", gpuWaitWords())
	select {
	case <-time.After(gpuRetryWait):
	case <-ctx.Done():
		return ctx.Err()
	}
	encoderChecks.Delete(codec)
	encoderStuck.Delete(codec)
	if p = encoderProblem(ctx, ffmpeg, codec); p == "" {
		return nil
	}
	if v, _ := encoderStuck.Load(codec); v == true {
		return &gpuStuckError{codec: codec}
	}
	return fmt.Errorf("%s", p)
}
