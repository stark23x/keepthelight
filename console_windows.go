//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"unsafe"
)

// Keep the Light.exe is a Windows GUI program (no console), keepthelight_cli.exe
// a console program. These helpers make both behave sensibly either way.

var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procGetConsoleWindow = kernel32.NewProc("GetConsoleWindow")
	procAttachConsole    = kernel32.NewProc("AttachConsole")
	procMessageBox       = user32.NewProc("MessageBoxW")
)

const (
	mbOK            = 0x0
	mbYesNo         = 0x4
	mbIconError     = 0x10
	mbIconWarning   = 0x30
	mbIconInfo      = 0x40
	mbDefButton2    = 0x100
	mbSetForeground = 0x10000
	idYes           = 6
)

// hasConsole reports whether this process has a console window to print to.
func hasConsole() bool {
	r, _, _ := procGetConsoleWindow.Call()
	return r != 0
}

// noConsole stops a console tool (ffmpeg, ffprobe, ...) from opening its own
// console window when we have none. With a console, children keep sharing it,
// so Ctrl+C still reaches them as before.
func noConsole(cmd *exec.Cmd) {
	if hasConsole() {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= createNoWindow
}

func stdoutUsable() bool {
	h, err := syscall.GetStdHandle(syscall.STD_OUTPUT_HANDLE)
	return err == nil && h != 0 && h != syscall.InvalidHandle
}

// attachParentConsole is for the GUI exe given command-line arguments. Output
// that is already redirected (the window's own jobs use a pipe) is left alone;
// otherwise it borrows the console of the command prompt that started it.
// It reports false when there is nowhere to show output.
func attachParentConsole() bool {
	if hasConsole() || stdoutUsable() {
		return true
	}
	const attachParentProcess = 0xFFFFFFFF
	if r, _, _ := procAttachConsole.Call(attachParentProcess); r == 0 {
		return false
	}
	if f, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
		os.Stdout, os.Stderr = f, f
	}
	if f, err := os.OpenFile("CONIN$", os.O_RDWR, 0); err == nil {
		os.Stdin = f
	}
	fmt.Println() // the prompt was already printed; start on a fresh line
	return true
}

func msgBox(owner uintptr, text string, flags uintptr) int {
	r, _, _ := procMessageBox.Call(owner, uintptr(unsafe.Pointer(u16(text))), uintptr(unsafe.Pointer(u16(appName))), flags|mbSetForeground)
	return int(r)
}

// messageBox shows an error or an information message.
func messageBox(owner uintptr, text string, isError bool) {
	icon := uintptr(mbIconInfo)
	if isError {
		icon = mbIconWarning
	}
	msgBox(owner, text, mbOK|icon)
}

// askYesNo asks a question; No is the default button.
func askYesNo(owner uintptr, text string) bool {
	return msgBox(owner, text, mbYesNo|mbIconWarning|mbDefButton2) == idYes
}
