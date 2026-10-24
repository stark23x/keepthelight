//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"syscall"
)

// acquireInstanceLock uses an exclusive lock on a file in the settings folder;
// the system drops it when the process ends.
func acquireInstanceLock() (release func(), ok bool) {
	_ = os.MkdirAll(appDataDir(), 0o700)
	f, err := os.OpenFile(filepath.Join(appDataDir(), "instance.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return func() {}, true
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, false
	}
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, true
}

func allowForeground(pid int) {}
