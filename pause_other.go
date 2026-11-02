//go:build !windows

package main

import "syscall"

// suspendTree freezes (or thaws) the job's process group (started with Setpgid).
func suspendTree(pid int, suspend bool) error {
	sig := syscall.SIGCONT
	if suspend {
		sig = syscall.SIGSTOP
	}
	return syscall.Kill(-pid, sig)
}
