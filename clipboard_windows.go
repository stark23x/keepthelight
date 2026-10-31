//go:build windows

package main

import (
	"errors"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

// The Windows clipboard, for the window's right-click menu (the page cannot
// always read the clipboard by itself).

var (
	procOpenClipboard    = user32.NewProc("OpenClipboard")
	procCloseClipboard   = user32.NewProc("CloseClipboard")
	procEmptyClipboard   = user32.NewProc("EmptyClipboard")
	procGetClipboardData = user32.NewProc("GetClipboardData")
	procSetClipboardData = user32.NewProc("SetClipboardData")
	procGlobalAlloc      = kernel32.NewProc("GlobalAlloc")
	procGlobalFree       = kernel32.NewProc("GlobalFree")
	procGlobalLock       = kernel32.NewProc("GlobalLock")
	procGlobalUnlock     = kernel32.NewProc("GlobalUnlock")
	procLstrlenW         = kernel32.NewProc("lstrlenW")
	procRtlMoveMemory    = kernel32.NewProc("RtlMoveMemory")
)

const (
	cfUnicodeText = 13
	gmemMoveable  = 0x0002
)

// openClipboard waits a moment if another program has the clipboard open.
func openClipboard() error {
	for i := 0; i < 20; i++ {
		if r, _, _ := procOpenClipboard.Call(0); r != 0 {
			return nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	return errors.New("the clipboard is in use by another program")
}

func readClipboard() (string, error) {
	if err := openClipboard(); err != nil {
		return "", err
	}
	defer procCloseClipboard.Call()
	h, _, _ := procGetClipboardData.Call(cfUnicodeText)
	if h == 0 {
		return "", nil // no text on the clipboard
	}
	p, _, _ := procGlobalLock.Call(h)
	if p == 0 {
		return "", errors.New("could not read the clipboard")
	}
	defer procGlobalUnlock.Call(h)
	n, _, _ := procLstrlenW.Call(p)
	if n == 0 {
		return "", nil
	}
	n = min(n, 1<<22)
	u := make([]uint16, n)
	procRtlMoveMemory.Call(uintptr(unsafe.Pointer(&u[0])), p, n*2) // copied out of the clipboard's memory
	return string(utf16.Decode(u)), nil
}

func writeClipboard(text string) error {
	u, err := syscall.UTF16FromString(text)
	if err != nil {
		return err
	}
	if err := openClipboard(); err != nil {
		return err
	}
	defer procCloseClipboard.Call()
	procEmptyClipboard.Call()
	h, _, _ := procGlobalAlloc.Call(gmemMoveable, uintptr(len(u)*2))
	if h == 0 {
		return errors.New("out of memory")
	}
	p, _, _ := procGlobalLock.Call(h)
	if p == 0 {
		procGlobalFree.Call(h)
		return errors.New("could not write to the clipboard")
	}
	procRtlMoveMemory.Call(p, uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)*2))
	procGlobalUnlock.Call(h)
	if r, _, _ := procSetClipboardData.Call(cfUnicodeText, h); r == 0 {
		procGlobalFree.Call(h)
		return errors.New("could not write to the clipboard")
	}
	return nil // the clipboard owns the memory now
}
