package main

import (
	"fmt"
	"io"
	"os"
)

func logf(format string, a ...any) { fmt.Printf(format+"\n", a...) }

// fileSizeFor reports the size of a file just written; in dry-run mode nothing
// was written, so it pretends the target was hit.
func fileSizeFor(r *runner, path string, dry int64) (int64, error) {
	if r.dry {
		return dry, nil
	}
	st, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return st.Size(), nil
}

func moveFile(src, dst string) error {
	_ = os.Remove(dst)
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	// different drive: copy instead
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Remove(src)
}

// finishAttempts keeps the best attempt (moving it to dst) and deletes the rest.
func finishAttempts(r *runner, best attempt, all []attempt, dst string) error {
	if r.dry {
		return nil
	}
	for _, a := range all {
		if a.Path != best.Path {
			_ = os.Remove(a.Path)
		}
	}
	if len(all) > 1 {
		fmt.Printf("Keeping attempt %d (video %s kbps).\n", best.N+1, thousands(best.Kbps))
	}
	return moveFile(best.Path, dst)
}

// reportTarget says how close the finished file came to the requested size.
func reportTarget(cfg *Config) {
	if cfg.TargetBytes <= 0 {
		return
	}
	st, err := os.Stat(cfg.Output)
	if err != nil {
		return
	}
	pct := (float64(st.Size())/float64(cfg.TargetBytes) - 1) * 100
	side := "under"
	if pct > 0 {
		side = "over"
	}
	fmt.Printf("\nTarget size: %s   Result: %s   (%.1f%% %s)\n", formatBytes(cfg.TargetBytes), formatBytes(st.Size()), abs(pct), side)
	if st.Size() > cfg.TargetBytes {
		fmt.Printf("WARNING: the file is %s over the target. Run again with a slightly smaller target to compensate.\n", formatBytes(st.Size()-cfg.TargetBytes))
	}
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
