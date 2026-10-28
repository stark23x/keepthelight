package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var errBusy = errors.New("in use")

func TestRetryLocked(t *testing.T) {
	old, oldFor := isLockError, lockRetryFor
	defer func() { isLockError, lockRetryFor = old, oldFor }()
	isLockError = func(err error) bool { return errors.Is(err, errBusy) }
	lockRetryFor = 3 * time.Second

	n := 0
	if err := retryLocked(func() error { // let go after a moment
		if n++; n < 3 {
			return errBusy
		}
		return nil
	}); err != nil || n != 3 {
		t.Errorf("released: %v after %d tries", err, n)
	}
	n = 0
	if err := retryLocked(func() error { n++; return os.ErrPermission }); err == nil || n != 1 {
		t.Errorf("another error is not retried: %v %d", err, n)
	}
	lockRetryFor = 600 * time.Millisecond
	start := time.Now()
	if err := retryLocked(func() error { return errBusy }); !errors.Is(err, errBusy) || time.Since(start) > 2*time.Second {
		t.Errorf("held for good: %v after %v", err, time.Since(start))
	}
	if shortFSErr(&os.PathError{Op: "remove", Path: "x", Err: errBusy}) != "in use by another program" {
		t.Error("message")
	}
}

func TestLeftovers(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	dir := t.TempDir()
	done := filepath.Join(dir, ".keepthelight-111")
	kept := filepath.Join(dir, ".keepthelight-222") // kept on purpose after a failure: not marked
	other := filepath.Join(dir, "My Films")
	for _, d := range []string{done, kept, other} {
		_ = os.MkdirAll(d, 0o755)
		_ = os.WriteFile(filepath.Join(d, "attempt1.mkv"), []byte("x"), 0o644)
	}
	_ = os.WriteFile(filepath.Join(done, leftoverMark), nil, 0o644)
	_ = os.WriteFile(filepath.Join(other, leftoverMark), nil, 0o644)
	loose := filepath.Join(dir, ".keepthelight-sample-1.mkv")
	notOurs := filepath.Join(dir, "film.mkv")
	for _, f := range []string{loose, notOurs} {
		_ = os.WriteFile(f, []byte("x"), 0o644)
		_ = appendLeftover(f)
	}
	cleanLeftovers(dir)
	if fileExists(done) || !fileExists(kept) || !fileExists(other) || fileExists(loose) || !fileExists(notOurs) {
		t.Errorf("done %v kept %v other %v loose %v notOurs %v", fileExists(done), fileExists(kept), fileExists(other), fileExists(loose), fileExists(notOurs))
	}
	if fileExists(leftoverList()) {
		t.Error("the list should be gone")
	}

	// a move to another drive: the copy is what matters
	src, dst := filepath.Join(dir, ".keepthelight-333", "a.mkv"), filepath.Join(dir, "out", "film.mkv")
	_ = os.MkdirAll(filepath.Dir(src), 0o755)
	_ = os.MkdirAll(filepath.Dir(dst), 0o755)
	_ = os.WriteFile(src, []byte("video"), 0o644)
	if err := copyThenRemove(src, dst); err != nil || fileExists(src) {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dst); string(b) != "video" {
		t.Error(string(b))
	}
	if !strings.HasSuffix(leftoverList(), "leftovers.txt") {
		t.Error(leftoverList())
	}
}
