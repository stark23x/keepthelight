//go:build !windows

package main

import (
	"errors"
	"syscall"
)

func diskFree(dir string) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, err
	}
	return st.Bavail * uint64(st.Bsize), nil
}

// Only Windows has a Recycle Bin here: elsewhere the original is renamed.
func hasRecycleBin(string) bool { return false }

func recycle(string) error { return errors.New("no Recycle Bin") }

func isNetworkPathOS(string) bool { return false }

// isLockErrorOS: files are not locked against deleting or renaming here.
func isLockErrorOS(err error) bool { return false }
