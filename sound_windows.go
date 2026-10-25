//go:build windows

package main

import (
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

var (
	procPlaySound     = syscall.NewLazyDLL("winmm.dll").NewProc("PlaySoundW")
	procFlashWindowEx = user32.NewProc("FlashWindowEx")
)

func mediaDir() string {
	w := os.Getenv("WINDIR")
	if w == "" {
		w = `C:\Windows`
	}
	return filepath.Join(w, "Media")
}

// playSoundRaw plays a Windows sound event (alias) or a .wav file. With wait,
// it returns when the sound has finished (the command-line program exits after).
func playSoundRaw(name string, alias, wait bool) bool {
	const (
		sndAsync     = 0x0001
		sndNoDefault = 0x0002
		sndAlias     = 0x00010000
		sndFilename  = 0x00020000
	)
	p, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return false
	}
	flags := uintptr(sndNoDefault)
	if alias {
		flags |= sndAlias
	} else {
		flags |= sndFilename
	}
	if !wait {
		flags |= sndAsync
	}
	r, _, _ := procPlaySound.Call(uintptr(unsafe.Pointer(p)), 0, flags)
	return r != 0
}

type flashInfo struct {
	Size    uint32
	Hwnd    uintptr
	Flags   uint32
	Count   uint32
	Timeout uint32
}

// flashTaskbar makes the window's taskbar button flash until it is used.
func flashTaskbar() {
	hwnd := dialogParent.Load()
	if hwnd == 0 {
		return // browser mode: not our window
	}
	const flashTray, flashTimerNoFG = 0x2, 0xC
	fi := flashInfo{Hwnd: hwnd, Flags: flashTray | flashTimerNoFG}
	fi.Size = uint32(unsafe.Sizeof(fi))
	_, _, _ = procFlashWindowEx.Call(uintptr(unsafe.Pointer(&fi)))
}
