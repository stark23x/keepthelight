//go:build windows

package main

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	ntdll                = windows.NewLazySystemDLL("ntdll.dll")
	procNtSuspendProcess = ntdll.NewProc("NtSuspendProcess")
	procNtResumeProcess  = ntdll.NewProc("NtResumeProcess")
)

const processSuspendResume = 0x0800

// suspendTree freezes (or thaws) a process and everything it started: the
// job's keepthelight_cli and its ffmpeg, dovi_tool... Frozen processes use no
// processor time and keep their place, so Resume carries on where it stopped.
func suspendTree(pid int, suspend bool) error {
	proc := procNtResumeProcess
	if suspend {
		proc = procNtSuspendProcess
	}
	if err := proc.Find(); err != nil {
		return err
	}
	for _, p := range treePIDs(pid) {
		h, err := windows.OpenProcess(processSuspendResume, false, uint32(p))
		if err != nil {
			continue // already gone
		}
		_, _, _ = proc.Call(uintptr(h))
		_ = windows.CloseHandle(h)
	}
	return nil
}

// treePIDs is the process and its descendants, from a snapshot of the running processes.
func treePIDs(root int) []int {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return []int{root}
	}
	defer windows.CloseHandle(snap)
	children := map[uint32][]uint32{}
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		if e.ProcessID != e.ParentProcessID {
			children[e.ParentProcessID] = append(children[e.ParentProcessID], e.ProcessID)
		}
	}
	out, seen := []int{}, map[uint32]bool{}
	todo := []uint32{uint32(root)}
	for len(todo) > 0 {
		p := todo[0]
		todo = todo[1:]
		if seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, int(p))
		todo = append(todo, children[p]...)
	}
	return out
}
