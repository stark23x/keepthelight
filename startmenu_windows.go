package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// startMenuPath: the shortcut in this user's Start menu (no administrator needed).
func startMenuPath() string {
	ad := os.Getenv("APPDATA")
	if ad == "" {
		return ""
	}
	return filepath.Join(ad, "Microsoft", "Windows", "Start Menu", "Programs", startMenuName)
}

// The shortcut is made and read with Windows' own WScript.Shell, through
// PowerShell. The paths go in as environment variables, never in the script.
func shortcutPS(script string, env ...string) (string, error) {
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.Env = append(os.Environ(), env...)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow}
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			msg := strings.TrimSpace(string(ee.Stderr))
			if len(msg) > 300 {
				msg = msg[:300]
			}
			return "", errors.New(msg)
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func makeShortcut(lnk, exe string) error {
	if err := os.MkdirAll(filepath.Dir(lnk), 0o755); err != nil {
		return err
	}
	_, err := shortcutPS(oneLine(
		"$s = (New-Object -ComObject WScript.Shell).CreateShortcut($env:KTL_LNK)",
		"$s.TargetPath = $env:KTL_EXE",
		"$s.WorkingDirectory = $env:KTL_DIR",
		"$s.IconLocation = $env:KTL_EXE + ',0'",
		"$s.Description = 'Convert HDR and Dolby Vision video to HEVC, keeping the HDR'",
		"$s.Save()",
	), "KTL_LNK="+lnk, "KTL_EXE="+exe, "KTL_DIR="+filepath.Dir(exe))
	return err
}

func shortcutTarget(lnk string) (string, error) {
	return shortcutPS("[Console]::OutputEncoding = [System.Text.Encoding]::UTF8; (New-Object -ComObject WScript.Shell).CreateShortcut($env:KTL_LNK).TargetPath",
		"KTL_LNK="+lnk)
}
