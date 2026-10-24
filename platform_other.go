//go:build !windows

package main

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func killTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}

func openWindow(url string) error {
	if os.Getenv("KTL_NO_OPEN") != "" {
		return errors.New("auto-open disabled")
	}
	for _, opener := range []string{"xdg-open", "open"} {
		if p, err := exec.LookPath(opener); err == nil {
			return exec.Command(p, url).Start()
		}
	}
	return errors.New("no browser opener found")
}

func openURL(u string) error {
	for _, opener := range []string{"xdg-open", "open"} {
		if p, err := exec.LookPath(opener); err == nil {
			return exec.Command(p, u).Start()
		}
	}
	return errors.New("no browser opener found")
}

func hasVCRuntime() bool { return true }

func openFolder(dir string) error { return openURL(dir) }

func browseDialog(kind, initial string) (string, error) {
	return "", errors.New("file dialogs are only available on Windows; paste the path instead")
}

func revealFile(path string) {}

func browseFiles(initial string) ([]string, error) {
	return nil, errors.New("file dialogs are only available on Windows; paste the path instead")
}
