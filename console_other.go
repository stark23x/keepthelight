//go:build !windows

package main

import (
	"fmt"
	"os"
	"os/exec"
)

func hasConsole() bool              { return true }
func noConsole(cmd *exec.Cmd)       {}
func attachParentConsole() bool     { return true }
func askYesNo(uintptr, string) bool { return false }

func messageBox(owner uintptr, text string, isError bool) {
	fmt.Fprintln(os.Stderr, text)
}
