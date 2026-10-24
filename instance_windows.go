//go:build windows

package main

import (
	"golang.org/x/sys/windows"
)

var procAllowSetForeground = user32.NewProc("AllowSetForegroundWindow")

// acquireInstanceLock takes a named mutex for this Windows session; it is
// released automatically if the process ends, even by a crash.
func acquireInstanceLock() (release func(), ok bool) {
	name, _ := windows.UTF16PtrFromString(`Local\KeepTheLight.SingleInstance`)
	h, err := windows.CreateMutex(nil, false, name)
	if err == windows.ERROR_ALREADY_EXISTS {
		if h != 0 {
			windows.CloseHandle(h)
		}
		return nil, false
	}
	if err != nil {
		return func() {}, true // cannot tell: behave as before (no single-instance check)
	}
	return func() { windows.CloseHandle(h) }, true
}

// allowForeground lets the running instance bring its window to the front.
func allowForeground(pid int) {
	if pid > 0 {
		_, _, _ = procAllowSetForeground.Call(uintptr(pid))
	}
}
