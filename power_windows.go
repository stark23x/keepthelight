//go:build windows

package main

import (
	"errors"
	"os/exec"
	"time"
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
//
// It tells Windows "the system is in use" every 30 seconds rather than holding
// a lasting request: a paused job (its processes frozen) stops telling, so the
// PC can go to sleep as usual while paused.
func preventSleep() (release func()) {
	stop := make(chan struct{})
	_, _, _ = procSetThreadExecutionState.Call(esSystemRequired)
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				_, _, _ = procSetThreadExecutionState.Call(esSystemRequired) // resets the sleep timer
			}
		}
	}()
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

// ---- keeping the PC awake for a whole queue run ----

var (
	procPowerCreateRequest     = kernel32.NewProc("PowerCreateRequest")
	procPowerSetRequest        = kernel32.NewProc("PowerSetRequest")
	procCallNtPowerInformation = powrprof.NewProc("CallNtPowerInformation")
)

// Windows' kinds of power request.
const (
	powerRequestDisplayRequired   = 0
	powerRequestSystemRequired    = 1
	powerRequestExecutionRequired = 3
)

// reasonContext is Windows' REASON_CONTEXT with a plain reason (it shows in
// "powercfg /requests"). The union after Flags is 24 bytes in its larger form.
type reasonContext struct {
	Version uint32
	Flags   uint32
	Reason  *uint16
	_       [2]uintptr
}

// holdAwakeOS asks Windows to keep the PC awake (and the screen on, when
// screen is true) until release is called. Unlike preventSleep it is held by
// a handle, not by a thread, so it lasts for the whole queue run.
func holdAwakeOS(screen bool) (release func(), err error) {
	reason, _ := windows.UTF16PtrFromString("Keep the Light is converting")
	ctx := reasonContext{Version: 0, Flags: 1 /* POWER_REQUEST_CONTEXT_SIMPLE_STRING */, Reason: reason}
	h, _, e := procPowerCreateRequest.Call(uintptr(unsafe.Pointer(&ctx)))
	if windows.Handle(h) == windows.InvalidHandle || h == 0 {
		return nil, e
	}
	kinds := []uintptr{powerRequestSystemRequired, powerRequestExecutionRequired}
	if screen {
		kinds = append(kinds, powerRequestDisplayRequired)
	}
	set := 0
	for _, k := range kinds {
		if r, _, _ := procPowerSetRequest.Call(h, k); r != 0 {
			set++
		}
	}
	if set == 0 {
		_ = windows.CloseHandle(windows.Handle(h))
		return nil, errors.New("Windows did not take the request")
	}
	// Closing the handle ends every request made with it.
	return func() { _ = windows.CloseHandle(windows.Handle(h)) }, nil
}

// modernStandby: the PC uses Modern Standby ("S0 Low Power Idle"), where
// Windows freezes desktop programs when the screen turns off. Only keeping the
// screen on keeps a conversion going there.
func modernStandby() bool {
	var caps [76]byte // SYSTEM_POWER_CAPABILITIES
	r, _, _ := procCallNtPowerInformation.Call(4 /* SystemPowerCapabilities */, 0, 0, uintptr(unsafe.Pointer(&caps[0])), uintptr(len(caps)))
	return r == 0 && caps[20] != 0 // AoAc ("always on, always connected")
}
