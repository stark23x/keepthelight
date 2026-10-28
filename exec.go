package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// findTool looks for an external tool: explicit override first, then next to
// the executable (and in ./tools, ./bin), then on PATH, then (for MP4Box) in
// GPAC's install folder. A copy that cannot start (typically mp4box.exe copied
// without the DLLs it needs) is skipped in favour of the next one, and if none
// works the error says why.
func findTool(name, override string) (string, error) {
	if override != "" {
		p, err := exec.LookPath(override)
		if err != nil {
			return "", fmt.Errorf("%s not found at %q", name, override)
		}
		return p, nil
	}
	var broken []string
	for _, p := range toolCandidates(name) {
		if problem := toolStartProblem(p); problem != "" {
			broken = append(broken, p+": "+problem)
			continue
		}
		return p, nil
	}
	if len(broken) > 0 {
		hint := ""
		if strings.EqualFold(name, "MP4Box") {
			hint = " Install GPAC with its installer from gpac.io (a copied mp4box.exe on its own does not work) and remove the broken copy."
		}
		return "", fmt.Errorf("%s was found but cannot start (%s).%s The Microsoft Visual C++ Runtime may also be missing",
			name, strings.Join(broken, "; "), hint)
	}
	return "", fmt.Errorf("%s not found", name)
}

// toolCandidates lists where a tool was found, in the order they are tried.
func toolCandidates(name string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(p string, err error) {
		if err == nil && !seen[strings.ToLower(p)] {
			seen[strings.ToLower(p)] = true
			out = append(out, p)
		}
	}
	if exe, err := os.Executable(); err == nil {
		d := filepath.Dir(exe)
		for _, dir := range []string{d, filepath.Join(d, "tools"), filepath.Join(d, "bin")} {
			add(exec.LookPath(filepath.Join(dir, name)))
		}
	}
	add(exec.LookPath(name))
	// Tesseract is not shipped either: its installer puts it in Program Files (or the user's own programs).
	if strings.EqualFold(name, "tesseract") {
		for _, env := range []string{"ProgramW6432", "ProgramFiles", "ProgramFiles(x86)"} {
			if base := os.Getenv(env); base != "" {
				add(exec.LookPath(filepath.Join(base, "Tesseract-OCR", "tesseract")))
			}
		}
		if base := os.Getenv("LOCALAPPDATA"); base != "" {
			add(exec.LookPath(filepath.Join(base, "Programs", "Tesseract-OCR", "tesseract")))
		}
	}
	// MP4Box is not shipped with Keep the Light: use an installed GPAC.
	if strings.EqualFold(name, "MP4Box") {
		for _, env := range []string{"ProgramW6432", "ProgramFiles", "ProgramFiles(x86)"} {
			if base := os.Getenv(env); base != "" {
				add(exec.LookPath(filepath.Join(base, "GPAC", "mp4box")))
			}
		}
	}
	return out
}

var startChecks sync.Map // path -> problem ("" = starts fine); checked once per run

// toolStartProblem tries to start a tool (asking for its version) and reports
// a problem only when Windows could not load it at all: a missing or
// mismatched DLL. Any other result means the program itself runs.
var toolStartProblem = func(path string) string {
	if v, ok := startChecks.Load(path); ok {
		return v.(string)
	}
	arg := "-version"
	if b := strings.ToLower(filepath.Base(path)); strings.HasPrefix(b, "dovi_tool") || strings.HasPrefix(b, "hdr10plus_tool") || strings.HasPrefix(b, "tesseract") {
		arg = "--version"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	problem := ""
	err := toolCommand(ctx, path, arg).Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		problem = loaderProblem(ee.ExitCode())
	}
	startChecks.Store(path, problem)
	return problem
}

// loaderProblem explains the Windows codes for "the program could not be loaded".
func loaderProblem(code int) string {
	switch uint32(code) {
	case 0xC0000135:
		return "a DLL it needs is missing, error 0xc0000135"
	case 0xC000007B:
		return "it or one of its DLLs is for a different kind of Windows, error 0xc000007b"
	case 0xC0000139:
		return "one of its DLLs is the wrong version, error 0xc0000139"
	}
	return ""
}

// toolError describes a failed tool run, in plain words when it could not start at all.
func toolError(name string, err error) error {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if p := loaderProblem(ee.ExitCode()); p != "" {
			return fmt.Errorf("%s could not start: %s (%s)", filepath.Base(name), p, name)
		}
	}
	return fmt.Errorf("%s failed: %w", filepath.Base(name), err)
}

func quote(s string) string {
	if s == "" || strings.ContainsAny(s, " \t\"'()&;|<>^%$") {
		return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
	}
	return s
}

func cmdline(name string, args []string) string {
	parts := []string{quote(name)}
	for _, a := range args {
		parts = append(parts, quote(a))
	}
	return strings.Join(parts, " ")
}

// toolCommand prepares a run of ffmpeg, ffprobe, dovi_tool etc. When this
// process has no console (the window version), the tool gets none either,
// instead of flashing up a black console window.
func toolCommand(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	noConsole(cmd)
	return cmd
}

type runner struct {
	ctx     context.Context
	dry     bool
	verbose bool
	dir     string // working directory for child processes ("" = ours)
}

func (r *runner) show(line string) {
	if r.dry || r.verbose {
		fmt.Println("  >", line)
	}
}

func (r *runner) run(name string, args ...string) error {
	r.show(cmdline(name, args))
	if r.dry {
		return nil
	}
	cmd := toolCommand(r.ctx, name, args...)
	cmd.Dir = r.dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return toolError(name, err)
	}
	return nil
}

// pipe runs `n1 a1... | n2 a2...`.
func (r *runner) pipe(n1 string, a1 []string, n2 string, a2 []string) error {
	r.show(cmdline(n1, a1) + " | " + cmdline(n2, a2))
	if r.dry {
		return nil
	}
	c1 := toolCommand(r.ctx, n1, a1...)
	c2 := toolCommand(r.ctx, n2, a2...)
	c1.Dir, c2.Dir = r.dir, r.dir
	out, err := c1.StdoutPipe()
	if err != nil {
		return err
	}
	c2.Stdin = out
	c1.Stderr = os.Stderr
	c2.Stdout = os.Stdout
	c2.Stderr = os.Stderr
	if err := c1.Start(); err != nil {
		return fmt.Errorf("%s: %w", filepath.Base(n1), err)
	}
	if err := c2.Start(); err != nil {
		_ = c1.Process.Kill()
		_ = c1.Wait()
		return fmt.Errorf("%s: %w", filepath.Base(n2), err)
	}
	err2 := c2.Wait()
	if err2 != nil {
		_ = c1.Process.Kill() // consumer died; don't leave the producer blocked
	}
	err1 := c1.Wait()
	if err2 != nil {
		return toolError(n2, err2)
	}
	if err1 != nil {
		return toolError(n1, err1)
	}
	return nil
}
