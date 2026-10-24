//go:build windows

package main

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// Low priority runs a transcode at Windows' "below normal" priority: it still
// uses the whole CPU when nothing else needs it, but other programs come first,
// so the PC stays responsive. Child processes (ffmpeg, dovi_tool, MP4Box)
// inherit a below-normal priority from the process that starts them.

// setOwnLowPriority lowers this process (and so everything it starts).
func setOwnLowPriority() error {
	return windows.SetPriorityClass(windows.CurrentProcess(), windows.BELOW_NORMAL_PRIORITY_CLASS)
}

// setTreePriority changes the priority of a running job: the process and all
// its descendants, so a change takes effect on the file being encoded now.
func setTreePriority(root int, low bool) error {
	class := uint32(windows.NORMAL_PRIORITY_CLASS)
	if low {
		class = windows.BELOW_NORMAL_PRIORITY_CLASS
	}
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(snap)
	children := map[uint32][]uint32{}
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		children[e.ParentProcessID] = append(children[e.ParentProcessID], e.ProcessID)
	}
	var firstErr error
	todo := []uint32{uint32(root)}
	created := map[uint32]int64{}
	for len(todo) > 0 {
		pid := todo[0]
		todo = todo[1:]
		h, err := windows.OpenProcess(windows.PROCESS_SET_INFORMATION|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
		if err != nil {
			continue // already finished
		}
		start := processStart(h)
		if err := windows.SetPriorityClass(h, class); err != nil && firstErr == nil {
			firstErr = err
		}
		windows.CloseHandle(h)
		created[pid] = start
		for _, c := range children[pid] {
			// A process ID can be reused: only follow real children, which started after their parent.
			if ch, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, c); err == nil {
				if processStart(ch) >= start {
					todo = append(todo, c)
				}
				windows.CloseHandle(ch)
			}
		}
	}
	return firstErr
}

func processStart(h windows.Handle) int64 {
	var c, x, k, u windows.Filetime
	if windows.GetProcessTimes(h, &c, &x, &k, &u) != nil {
		return 0
	}
	return c.Nanoseconds()
}
