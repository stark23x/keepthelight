package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// findTool looks for an external tool: explicit override first, then next to
// the executable (and in ./tools, ./bin), then on PATH.
func findTool(name, override string) (string, error) {
	if override != "" {
		p, err := exec.LookPath(override)
		if err != nil {
			return "", fmt.Errorf("%s not found at %q", name, override)
		}
		return p, nil
	}
	if exe, err := os.Executable(); err == nil {
		d := filepath.Dir(exe)
		for _, dir := range []string{d, filepath.Join(d, "tools"), filepath.Join(d, "bin")} {
			if p, err := exec.LookPath(filepath.Join(dir, name)); err == nil {
				return p, nil
			}
		}
	}
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}
	// MP4Box is not shipped with Keep the Light: use an installed GPAC.
	if strings.EqualFold(name, "MP4Box") {
		for _, env := range []string{"ProgramW6432", "ProgramFiles", "ProgramFiles(x86)"} {
			if base := os.Getenv(env); base != "" {
				if p, err := exec.LookPath(filepath.Join(base, "GPAC", "mp4box")); err == nil {
					return p, nil
				}
			}
		}
	}
	return "", fmt.Errorf("%s not found", name)
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
		return fmt.Errorf("%s failed: %w", filepath.Base(name), err)
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
		return fmt.Errorf("%s failed: %w", filepath.Base(n2), err2)
	}
	if err1 != nil {
		return fmt.Errorf("%s failed: %w", filepath.Base(n1), err1)
	}
	return nil
}
