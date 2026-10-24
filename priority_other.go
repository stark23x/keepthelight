//go:build !windows

package main

import "syscall"

// On other systems low priority means a "nice" value, inherited by children.
func setOwnLowPriority() error { return syscall.Setpriority(syscall.PRIO_PROCESS, 0, 10) }

// Changing a running job is only done on Windows (raising the priority back
// needs extra rights elsewhere); the setting applies from the next file.
func setTreePriority(root int, low bool) error { return nil }
