//go:build windows

package main

import (
	"errors"
	"os/exec"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	powrprof                    = windows.NewLazySystemDLL("powrprof.dll")
	procSetSuspendState         = powrprof.NewProc("SetSuspendState")
	procSetThreadExecutionState = kernel32.NewProc("SetThreadExecutionState")
)

const (
	esContinuous     = 0x80000000
	esSystemRequired = 0x00000001
)

// preventSleep keeps Windows from going to sleep while a transcode runs (the
// screen may still turn off). Call the returned function when done.
// The request belongs to a thread, so a dedicated locked thread holds it.
func preventSleep() (release func()) {
	stop, held := make(chan struct{}), make(chan struct{})
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		_, _, _ = procSetThreadExecutionState.Call(esContinuous | esSystemRequired)
		close(held)
		<-stop
		_, _, _ = procSetThreadExecutionState.Call(esContinuous)
	}()
	<-held
	return func() { close(stop) }
}

// powerAction puts the PC to sleep or shuts it down.
func powerAction(action string) error {
	switch action {
	case "shutdown":
		cmd := exec.Command("shutdown", "/s", "/t", "0")
		hideWindow(cmd)
		return cmd.Run()
	case "sleep":
		if err := enableShutdownPrivilege(); err != nil {
			return err
		}
		// SetSuspendState(hibernate=FALSE, force=FALSE, disableWakeEvents=FALSE): sleep, not hibernate.
		if r, _, err := procSetSuspendState.Call(0, 0, 0); r == 0 {
			return err
		}
		return nil
	}
	return errors.New("unknown action")
}

// enableShutdownPrivilege: Windows only lets a program put the PC to sleep with
// this privilege switched on (every account has it, but it starts off).
func enableShutdownPrivilege() error {
	var tok windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_ADJUST_PRIVILEGES|windows.TOKEN_QUERY, &tok); err != nil {
		return err
	}
	defer tok.Close()
	var luid windows.LUID
	if err := windows.LookupPrivilegeValue(nil, windows.StringToUTF16Ptr("SeShutdownPrivilege"), &luid); err != nil {
		return err
	}
	tp := windows.Tokenprivileges{PrivilegeCount: 1}
	tp.Privileges[0] = windows.LUIDAndAttributes{Luid: luid, Attributes: windows.SE_PRIVILEGE_ENABLED}
	return windows.AdjustTokenPrivileges(tok, false, &tp, uint32(unsafe.Sizeof(tp)), nil, nil)
}
