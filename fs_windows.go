//go:build windows

package main

import (
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

var (
	procGetDiskFreeSpaceEx = kernel32.NewProc("GetDiskFreeSpaceExW")
	procGetDriveType       = kernel32.NewProc("GetDriveTypeW")
	procSHFileOperation    = shell32.NewProc("SHFileOperationW")
)

// diskFree returns the free bytes on the drive holding dir.
func diskFree(dir string) (uint64, error) {
	var free uint64
	p, err := syscall.UTF16PtrFromString(dir)
	if err != nil {
		return 0, err
	}
	if r, _, e := procGetDiskFreeSpaceEx.Call(uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&free)), 0, 0); r == 0 {
		return 0, e
	}
	return free, nil
}

// hasRecycleBin: only local fixed drives have one (network drives and USB
// sticks delete files for good, so the original is renamed there instead).
func hasRecycleBin(path string) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	vol := filepath.VolumeName(abs)
	if len(vol) != 2 || strings.HasPrefix(vol, `\\`) {
		return false
	}
	root, _ := syscall.UTF16PtrFromString(vol + `\`)
	const driveFixed = 3
	t, _, _ := procGetDriveType.Call(uintptr(unsafe.Pointer(root)))
	return t == driveFixed
}

type shFileOp struct {
	Hwnd         uintptr
	Func         uint32
	From         *uint16
	To           *uint16
	Flags        uint16
	Aborted      int32
	NameMappings uintptr
	Title        *uint16
}

// recycle moves a file to the Recycle Bin, without asking.
func recycle(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	from := append(syscall.StringToUTF16(abs), 0) // the list ends with two NULs
	const (
		foDelete          = 3
		fofSilent         = 0x0004
		fofNoConfirmation = 0x0010
		fofAllowUndo      = 0x0040
		fofNoErrorUI      = 0x0400
	)
	op := shFileOp{Func: foDelete, From: &from[0], Flags: fofAllowUndo | fofNoConfirmation | fofSilent | fofNoErrorUI}
	if r, _, _ := procSHFileOperation.Call(uintptr(unsafe.Pointer(&op))); r != 0 {
		return syscall.Errno(r)
	}
	if op.Aborted != 0 {
		return syscall.Errno(1223) // ERROR_CANCELLED
	}
	if fileExists(abs) {
		return syscall.ERROR_ACCESS_DENIED
	}
	return nil
}
