//go:build windows

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"sync"
	"syscall"
)

// ---------- Browse windows in a helper process ----------
//
// Windows' Open / Save / folder windows load File Explorer's add-ons (shell
// extensions) into the program that opens them. A faulty one crashes that
// program: on the owner's PC, ExplorerPatcher's "Register as shell extension"
// crashed Keep the Light now and then on Browse (28 November 2026). So each
// Browse window opens in a small helper: Keep the Light's own program started
// with -dialog-helper, owned by the window. If an add-on crashes it, only the
// Browse window closes, with a message, and the queue keeps running.
// If the helper cannot be started, the window opens here as before, on one
// dedicated thread set up as Windows' dialogs need.

var dialogBusy sync.Mutex // one Browse window at a time

var procEnableWindow = user32.NewProc("EnableWindow")

func browseDialog(kind, initial string) (string, error) {
	if !dialogBusy.TryLock() {
		return "", nil // a Browse window is already open: this click does nothing
	}
	defer dialogBusy.Unlock()
	r, err := runDialogHelper(kind, initial)
	if errors.Is(err, errHelperUnavailable) {
		var p string
		onDialogThread(func() { p, err = browseDialogHere(kind, initial) })
		return p, err
	}
	if err != nil {
		return "", err
	}
	return r.Path, nil
}

func browseFiles(initial string) ([]string, error) {
	if !dialogBusy.TryLock() {
		return nil, nil
	}
	defer dialogBusy.Unlock()
	r, err := runDialogHelper("openMulti", initial)
	if errors.Is(err, errHelperUnavailable) {
		var ps []string
		onDialogThread(func() { ps, err = browseFilesHere(initial) })
		return ps, err
	}
	if err != nil {
		return nil, err
	}
	return r.Paths, nil
}

var errHelperUnavailable = errors.New("the dialog helper could not be started")

func runDialogHelper(kind, initial string) (dialogResult, error) {
	var r dialogResult
	exe, err := os.Executable()
	if err != nil {
		return r, errHelperUnavailable
	}
	cmd := exec.Command(exe, "-dialog-helper", kind, initial, folderTitle, strconv.FormatUint(uint64(dialogParent.Load()), 10))
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow}
	procAllowSetForeground.Call(^uintptr(0)) // ASFW_ANY: the helper's window may come to the front
	if err := cmd.Start(); err != nil {
		return r, errHelperUnavailable
	}
	err = cmd.Wait()
	if owner := dialogParent.Load(); owner != 0 {
		// The Browse window disables its owner while it is open; a helper that
		// crashed never turns it back on, so the window would stay frozen.
		procEnableWindow.Call(owner, 1)
	}
	if err != nil && crashed(err) {
		return r, errors.New(dialogCrashMessage)
	}
	if json.Unmarshal(bytes.TrimSpace(out.Bytes()), &r) != nil {
		if err != nil {
			return r, errHelperUnavailable // it did not run as a helper (an old copy?): open it here
		}
		return r, errors.New("the Browse window gave no answer")
	}
	if r.Error != "" {
		return r, errors.New(r.Error)
	}
	return r, nil
}

// dialogHelperMain is the helper's whole run: one Browse window, then the answer as JSON.
func dialogHelperMain(args []string) int {
	runtime.LockOSThread()
	get := func(i int) string {
		if i < len(args) {
			return args[i]
		}
		return ""
	}
	kind, initial := get(0), get(1)
	folderTitle = get(2)
	if owner, err := strconv.ParseUint(get(3), 10, 64); err == nil && owner != 0 {
		dialogParent.Store(uintptr(owner))
	}
	var r dialogResult
	var err error
	if kind == "openMulti" {
		r.Paths, err = browseFilesHere(initial)
	} else {
		r.Path, err = browseDialogHere(kind, initial)
	}
	if err != nil {
		r.Error = err.Error()
	}
	b, _ := json.Marshal(r)
	os.Stdout.Write(append(b, '\n'))
	return 0
}

// onDialogThread runs fn on the one thread kept for dialogs in this process.
var dialogThread = make(chan func())
var dialogThreadOnce sync.Once

func onDialogThread(fn func()) {
	dialogThreadOnce.Do(func() {
		go func() {
			runtime.LockOSThread() // never unlocked: this thread stays the dialogs' own
			for f := range dialogThread {
				f()
			}
		}()
	})
	done := make(chan struct{})
	dialogThread <- func() { defer close(done); fn() }
	<-done
}
