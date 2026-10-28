package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ---------- files that another program has open ----------
//
// Backup programs (Backblaze, OneDrive), antivirus scanners and indexers open
// new files for a moment to read them. While one does, Windows refuses to
// delete, rename or move the file ("used by another process"). They let go
// within seconds, so these operations are retried for a while before giving up.

var (
	lockRetryFor = 30 * time.Second
	isLockError  = isLockErrorOS // replaced in tests
)

// retryLocked runs op until it succeeds, fails for another reason, or the file
// stays in use for lockRetryFor.
func retryLocked(op func() error) error {
	wait, deadline := 250*time.Millisecond, time.Now().Add(lockRetryFor)
	for {
		err := op()
		if err == nil || !isLockError(err) || time.Now().Add(wait).After(deadline) {
			return err
		}
		time.Sleep(wait)
		wait = min(wait*2, 2*time.Second)
	}
}

// removeFile deletes a file, waiting for other programs to let go of it. A
// file that is already gone counts as deleted.
func removeFile(path string) error {
	err := retryLocked(func() error { return os.Remove(path) })
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// renameFile renames (or moves on the same drive), waiting for other programs.
func renameFile(from, to string) error {
	return retryLocked(func() error { return os.Rename(from, to) })
}

// removeTemp deletes a temporary file or folder that is no longer needed. It
// never fails the job: what cannot be deleted (still in use by another program)
// is left with a note, and removed by the next job (cleanLeftovers).
func removeTemp(path string) {
	err := retryLocked(func() error { return os.RemoveAll(path) })
	if err == nil {
		return
	}
	note := "could not delete a temporary file (" + shortFSErr(err) + "); it will be removed by the next job: " + path
	if st, e := os.Stat(path); e == nil && st.IsDir() {
		_ = os.WriteFile(filepath.Join(path, leftoverMark), []byte("Keep the Light: safe to delete\n"), 0o644)
	} else if e == nil {
		_ = appendLeftover(path)
	}
	fmt.Println("Note:", note)
}

// leftoverMark marks a temp folder Keep the Light is finished with.
const leftoverMark = ".ktl-delete"

// leftover files (not folders) are listed in a file next to the settings.
func leftoverList() string { return filepath.Join(appDataDir(), "leftovers.txt") }

func appendLeftover(path string) error {
	_ = os.MkdirAll(appDataDir(), 0o700)
	f, err := os.OpenFile(leftoverList(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(path + "\n")
	return err
}

// cleanLeftovers removes what earlier jobs could not: temp folders marked as
// finished in dir, and the listed files. Only Keep the Light's own temp names
// are touched. Nothing waits here: what is still in use stays for next time.
func cleanLeftovers(dir string) {
	if dir != "" {
		ents, _ := os.ReadDir(dir)
		for _, e := range ents {
			if e.IsDir() && strings.HasPrefix(e.Name(), ".keepthelight-") {
				p := filepath.Join(dir, e.Name())
				if _, err := os.Stat(filepath.Join(p, leftoverMark)); err == nil {
					_ = os.RemoveAll(p)
				}
			}
		}
	}
	b, err := os.ReadFile(leftoverList())
	if err != nil {
		return
	}
	var keep []string
	for _, l := range strings.Split(string(b), "\n") {
		l = strings.TrimSpace(l)
		if l == "" || !ownTempName(l) {
			continue
		}
		if err := os.Remove(l); err != nil && !errors.Is(err, os.ErrNotExist) {
			keep = append(keep, l)
		}
	}
	if len(keep) == 0 {
		_ = os.Remove(leftoverList())
	} else {
		_ = os.WriteFile(leftoverList(), []byte(strings.Join(keep, "\n")+"\n"), 0o600)
	}
}

// ownTempName: a path inside one of Keep the Light's temp folders, or one of its
// temp files (never anything else, whatever the list says).
func ownTempName(p string) bool {
	return strings.Contains(p, ".keepthelight-") || strings.HasPrefix(filepath.Base(p), ".keepthelight-")
}

func shortFSErr(err error) string {
	var pe *os.PathError
	if errors.As(err, &pe) {
		err = pe.Err
	}
	var le *os.LinkError
	if errors.As(err, &le) {
		err = le.Err
	}
	if isLockError(err) {
		return "in use by another program"
	}
	return err.Error()
}

// copyThenRemove moves a file to another drive: copy, then delete the source.
// Once the copy is complete the move has succeeded; a source that cannot be
// deleted is left for later instead of failing.
func copyThenRemove(src, dst string) error {
	err := retryLocked(func() error {
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
		return out.Close()
	})
	if err != nil {
		return err
	}
	removeTemp(src)
	return nil
}
