package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// Working locally: a source or result on a network drive is copied to this
// PC first, converted here, and the result copied back. The network is then
// used once in each direction, instead of throughout a long conversion (and
// twice more for Dolby Vision's temporary files), and a network hiccup cannot
// break a conversion halfway.

var isNetworkPath = isNetworkPathOS // tests replace it

type localWork struct {
	dir      string
	finalOut string // where the result goes back to ("" = it is already there)
	keep     bool   // the copy back failed: keep the local result
}

// startLocal copies what is needed to dir and points cfg at the local copies.
// It returns nil when there is nothing to do or not enough space (the job then
// works over the network as before, with a note).
func startLocal(cfg *Config) (*localWork, error) {
	if cfg.WorkLocal == "" || cfg.DryRun || cfg.SampleSecs > 0 || cfg.MetaOnly {
		return nil, nil
	}
	inNet, outNet := isNetworkPath(cfg.Input), isNetworkPath(cfg.Output)
	if !inNet && !outNet {
		return nil, nil
	}
	if isNetworkPath(cfg.WorkLocal) {
		fmt.Println("Note: the folder for working locally is itself on the network, so this file is converted over the network")
		return nil, nil
	}
	var size int64
	if st, err := os.Stat(cfg.Input); err == nil {
		size = st.Size()
	}
	need := size + size/10 // the result is at most about as big as the source
	if inNet {
		need += size
	}
	if free, err := diskFree(cfg.WorkLocal); err == nil && int64(free) < need+(2<<30) {
		fmt.Printf("Note: not enough free space in %s to work locally (about %s needed, %s free), so this file is converted over the network\n",
			cfg.WorkLocal, formatBytes(need), formatBytes(int64(free)))
		return nil, nil
	}
	cleanLeftovers(cfg.WorkLocal) // what earlier jobs could not delete
	dir, err := os.MkdirTemp(cfg.WorkLocal, ".keepthelight-local-")
	if err != nil {
		return nil, fmt.Errorf("cannot create a folder for working locally: %w", err)
	}
	lw := &localWork{dir: dir}
	if inNet {
		local := filepath.Join(dir, filepath.Base(cfg.Input))
		if err := copyWithProgress(cfg.Input, local, "Copying the source to this PC"); err != nil {
			removeTemp(dir)
			return nil, err
		}
		cfg.Input = local
	}
	if outNet {
		lw.finalOut = cfg.Output
		cfg.Output = filepath.Join(dir, "result"+filepath.Ext(cfg.Output))
	}
	if cfg.Tmp == "" || isNetworkPath(cfg.Tmp) {
		cfg.Tmp = dir
	}
	return lw, nil
}

// finish copies the result back to the network. On failure the local result is
// kept and its place reported, so the conversion is not lost.
func (lw *localWork) finish(cfg *Config) error {
	if lw.finalOut != "" {
		if err := copyWithProgress(cfg.Output, lw.finalOut, "Copying the result back"); err != nil {
			lw.keep = true
			return fmt.Errorf("the result could not be copied to %s (%v); it is kept at %s", lw.finalOut, err, cfg.Output)
		}
		cfg.Output = lw.finalOut
	}
	return nil
}

func (lw *localWork) cleanup() {
	if !lw.keep {
		removeTemp(lw.dir)
	}
}

// copyWithProgress copies a file, printing "label: 35% (12.3 GB of 35.0 GB)"
// now and then, so the window shows how far it is.
func copyWithProgress(src, dst, label string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	st, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	total := st.Size()
	fmt.Printf("\n%s (%s)\n", label, formatBytes(total))
	buf := make([]byte, 8<<20)
	var done int64
	last := time.Now()
	start := last
	for {
		n, rerr := in.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				out.Close()
				_ = removeFile(dst)
				return werr
			}
			done += int64(n)
			if time.Since(last) > 3*time.Second && total > 0 {
				last = time.Now()
				fmt.Printf("%s: %d%% (%s of %s, %.0f MB/s)\n", label, done*100/total, formatBytes(done), formatBytes(total),
					float64(done)/1e6/time.Since(start).Seconds())
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			out.Close()
			_ = removeFile(dst)
			return rerr
		}
	}
	if err := out.Close(); err != nil {
		_ = removeFile(dst)
		return err
	}
	return nil
}
