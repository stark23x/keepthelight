//go:build windows

package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

const createNoWindow = 0x08000000

func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}

// killTree stops the process and everything it started (ffmpeg, dovi_tool...).
func killTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	k := exec.Command("taskkill", "/T", "/F", "/PID", itoa(cmd.Process.Pid))
	hideWindow(k)
	_ = k.Run()
}

func openWindow(url string) error {
	var cands []string
	for _, env := range []string{"ProgramFiles(x86)", "ProgramFiles", "LocalAppData"} {
		base := os.Getenv(env)
		if base == "" {
			continue
		}
		cands = append(cands,
			filepath.Join(base, "Microsoft", "Edge", "Application", "msedge.exe"),
			filepath.Join(base, "Google", "Chrome", "Application", "chrome.exe"))
	}
	for _, c := range cands {
		if _, err := os.Stat(c); err == nil {
			cmd := exec.Command(c, "--app="+url, "--window-size=1000,860")
			if cmd.Start() == nil {
				return nil
			}
		}
	}
	cmd := exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	return cmd.Start()
}

// openURL opens a web or mail link with the user's default program.
func openURL(u string) error {
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", u).Start()
}

// hasVCRuntime reports whether the Microsoft Visual C++ runtime that the
// bundled dovi_tool and hdr10plus_tool need (vcruntime140.dll) is installed.
func hasVCRuntime() bool {
	for _, p := range []string{
		filepath.Join(os.Getenv("SystemRoot"), "System32", "vcruntime140.dll"),
	} {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	if d, err := toolsDir(); err == nil { // a copy next to the tools also works
		if _, err := os.Stat(filepath.Join(d, "vcruntime140.dll")); err == nil {
			return true
		}
	}
	return false
}

// openFolder shows a folder in Explorer.
func openFolder(dir string) error {
	return exec.Command("explorer.exe", dir).Start()
}

// runPowerShell runs a single-line script (no double quotes) with plain -Command:
// no -EncodedCommand and no execution-policy override.
func runPowerShell(script, initial string) (string, error) {
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-STA", "-Command", script)
	cmd.Env = append(os.Environ(), "KTL_INIT="+initial)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow}
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			msg := strings.TrimSpace(string(ee.Stderr))
			if len(msg) > 300 {
				msg = msg[:300]
			}
			return "", errors.New("PowerShell: " + msg)
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func oneLine(lines ...string) string { return strings.Join(lines, "; ") }

func psPrelude() []string {
	return []string{
		"[Console]::OutputEncoding = [System.Text.Encoding]::UTF8",
		"Add-Type -AssemblyName System.Windows.Forms",
		"$owner = New-Object System.Windows.Forms.Form -Property @{TopMost = $true}",
		"$init = $env:KTL_INIT",
	}
}

func browseDialogPS(kind, initial string) (string, error) {
	var lines []string
	switch kind {
	case "open":
		lines = append(psPrelude(),
			"$d = New-Object System.Windows.Forms.OpenFileDialog",
			"$d.Title = 'Choose a video file'",
			"$d.Filter = 'Video files|*.mkv;*.mp4;*.m4v;*.mov;*.ts;*.m2ts;*.mts;*.webm;*.avi;*.hevc;*.h265|All files|*.*'",
			"if ($init) { $dir = Split-Path $init -ErrorAction SilentlyContinue; if ($dir -and (Test-Path $dir)) { $d.InitialDirectory = $dir } }",
			"if ($d.ShowDialog($owner) -eq 'OK') { Write-Output $d.FileName }")
	case "openJson":
		lines = append(psPrelude(),
			"$d = New-Object System.Windows.Forms.OpenFileDialog",
			"$d.Title = 'Import profiles'",
			"$d.Filter = 'Keep the Light profiles (*.json)|*.json'",
			"if ($d.ShowDialog($owner) -eq 'OK') { Write-Output $d.FileName }")
	case "saveJson":
		lines = append(psPrelude(),
			"$d = New-Object System.Windows.Forms.SaveFileDialog",
			"$d.Title = 'Export profiles'",
			"$d.Filter = 'Keep the Light profiles (*.json)|*.json'",
			"$d.FileName = 'Keep the Light profiles.json'",
			"if ($d.ShowDialog($owner) -eq 'OK') { Write-Output $d.FileName }")
	case "wav":
		lines = append(psPrelude(),
			"$d = New-Object System.Windows.Forms.OpenFileDialog",
			"$d.Title = 'Choose a sound'",
			"$d.Filter = 'Sounds (*.wav)|*.wav'",
			"if ($init) { $dir = Split-Path $init -ErrorAction SilentlyContinue; if ($dir -and (Test-Path $dir)) { $d.InitialDirectory = $dir } }",
			"if ($d.ShowDialog($owner) -eq 'OK') { Write-Output $d.FileName }")
	case "image":
		lines = append(psPrelude(),
			"$d = New-Object System.Windows.Forms.OpenFileDialog",
			"$d.Title = 'Choose a cover image'",
			"$d.Filter = 'Images (*.jpg, *.png)|*.jpg;*.jpeg;*.png'",
			"if ($init) { $dir = Split-Path $init -ErrorAction SilentlyContinue; if ($dir -and (Test-Path $dir)) { $d.InitialDirectory = $dir } }",
			"if ($d.ShowDialog($owner) -eq 'OK') { Write-Output $d.FileName }")
	case "openMulti":
		lines = append(psPrelude(),
			"$d = New-Object System.Windows.Forms.OpenFileDialog",
			"$d.Title = 'Choose video files'",
			"$d.Multiselect = $true",
			"$d.Filter = 'Video files|*.mkv;*.mp4;*.m4v;*.mov;*.ts;*.m2ts;*.mts;*.webm;*.avi;*.hevc;*.h265|All files|*.*'",
			"if ($init) { $dir = Split-Path $init -ErrorAction SilentlyContinue; if ($dir -and (Test-Path $dir)) { $d.InitialDirectory = $dir } }",
			"if ($d.ShowDialog($owner) -eq 'OK') { $d.FileNames -join [char]10 }")
	case "save":
		lines = append(psPrelude(),
			"$d = New-Object System.Windows.Forms.SaveFileDialog",
			"$d.Title = 'Save transcoded video as'",
			"$d.Filter = 'Matroska video (*.mkv)|*.mkv|MP4 video (*.mp4)|*.mp4'",
			"$d.OverwritePrompt = $true",
			"if ($init) { $dir = Split-Path $init -ErrorAction SilentlyContinue; if ($dir -and (Test-Path $dir)) { $d.InitialDirectory = $dir }; $d.FileName = Split-Path $init -Leaf }",
			"if ($d.ShowDialog($owner) -eq 'OK') { Write-Output $d.FileName }")
	case "folder":
		lines = append(psPrelude(),
			"$d = New-Object System.Windows.Forms.FolderBrowserDialog",
			"$d.Description = 'Choose a folder for temporary files'",
			"if ($d.ShowDialog($owner) -eq 'OK') { Write-Output $d.SelectedPath }")
	default:
		return "", errors.New("unknown dialog")
	}
	return runPowerShell(oneLine(lines...), initial)
}

func revealFile(path string) {
	cmd := exec.Command("explorer.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `explorer.exe /select,"` + path + `"`}
	_ = cmd.Start()
}
