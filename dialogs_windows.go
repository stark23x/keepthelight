//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

// Native Windows common dialogs (comdlg32 / shell32), called directly so no
// helper process is needed.

var (
	comdlg32 = syscall.NewLazyDLL("comdlg32.dll")
	user32   = syscall.NewLazyDLL("user32.dll")
	ole32    = syscall.NewLazyDLL("ole32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")

	procGetOpenFileName = comdlg32.NewProc("GetOpenFileNameW")
	procGetSaveFileName = comdlg32.NewProc("GetSaveFileNameW")
	procCommDlgExtErr   = comdlg32.NewProc("CommDlgExtendedError")
	procCreateWindowEx  = user32.NewProc("CreateWindowExW")
	procDestroyWindow   = user32.NewProc("DestroyWindow")
	procSetForeground   = user32.NewProc("SetForegroundWindow")
	procCoInit          = ole32.NewProc("CoInitializeEx")
	procCoUninit        = ole32.NewProc("CoUninitialize")
	procCoTaskMemFree   = ole32.NewProc("CoTaskMemFree")
	procSHBrowse        = shell32.NewProc("SHBrowseForFolderW")
	procSHGetPath       = shell32.NewProc("SHGetPathFromIDListW")
)

type openFileName struct {
	StructSize    uint32
	Owner         uintptr
	Instance      uintptr
	Filter        *uint16
	CustomFilter  *uint16
	MaxCustFilter uint32
	FilterIndex   uint32
	File          *uint16
	MaxFile       uint32
	FileTitle     *uint16
	MaxFileTitle  uint32
	InitialDir    *uint16
	Title         *uint16
	Flags         uint32
	FileOffset    uint16
	FileExtension uint16
	DefExt        *uint16
	CustData      uintptr
	FnHook        uintptr
	TemplateName  *uint16
	PvReserved    uintptr
	DwReserved    uint32
	FlagsEx       uint32
}

type browseInfo struct {
	Owner       uintptr
	Root        uintptr
	DisplayName *uint16
	Title       *uint16
	Flags       uint32
	Fn          uintptr
	LParam      uintptr
	Image       int32
}

const (
	wsExTopmost    = 0x00000008
	wsExToolwindow = 0x00000080
	wsPopup        = 0x80000000

	ofnOverwritePrompt  = 0x00000002
	ofnAllowMultiselect = 0x00000200
	ofnHideReadOnly     = 0x00000004
	ofnNoChangeDir      = 0x00000008
	ofnPathMustExist    = 0x00000800
	ofnFileMustExist    = 0x00001000
	ofnExplorer         = 0x00080000
	ofnEnableSizing     = 0x00800000
)

func u16(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}

// multiString builds a double-NUL-terminated list of strings (for dialog filters).
func multiString(parts ...string) []uint16 {
	var out []uint16
	for _, p := range parts {
		out = append(out, utf16.Encode([]rune(p))...)
		out = append(out, 0)
	}
	return append(out, 0)
}

// dialogParent is the built-in window, when there is one: dialogs then belong
// to it (modal, centred on it) instead of to a hidden helper window.
var dialogParent atomic.Uintptr

// withOwner runs fn on a locked OS thread with COM initialised. In window mode
// the owner is the Keep the Light window; in browser mode it is a hidden topmost
// window, so the dialog opens in front of the browser window.
func withOwner(fn func(owner uintptr) (string, error)) (string, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if hr, _, _ := procCoInit.Call(0, 2); hr == 0 || hr == 1 {
		defer procCoUninit.Call()
	}
	if p := dialogParent.Load(); p != 0 {
		return fn(p)
	}
	owner, _, _ := procCreateWindowEx.Call(wsExTopmost|wsExToolwindow,
		uintptr(unsafe.Pointer(u16("STATIC"))), uintptr(unsafe.Pointer(u16(""))), wsPopup, 0, 0, 0, 0, 0, 0, 0, 0)
	if owner != 0 {
		procSetForeground.Call(owner)
		defer procDestroyWindow.Call(owner)
	}
	return fn(owner)
}

func fileDialog(save bool, initial string) (string, error) { return fileDialogEx(save, false, initial) }

// imageDialog lets the user choose a cover image; wavDialog a sound.
func imageDialog(initial string) (string, error) { return specialDialog("image", initial) }
func wavDialog(initial string) (string, error)   { return specialDialog("wav", initial) }

func specialDialog(kind, initial string) (string, error) {
	dialogKind = kind
	defer func() { dialogKind = "" }()
	return fileDialogEx(false, false, initial)
}

// dialogKind switches fileDialogEx from videos to images or sounds (dialogs run one at a time).
var dialogKind string

// fileDialogEx shows Open or Save As. With multi, several files can be chosen;
// they come back separated by newlines.
func fileDialogEx(save, multi bool, initial string) (string, error) {
	return withOwner(func(owner uintptr) (string, error) {
		buf := make([]uint16, 32768)
		dir := ""
		if initial != "" {
			if d := filepath.Dir(initial); d != "." {
				if st, err := os.Stat(d); err == nil && st.IsDir() {
					dir = d
				}
			}
			if save {
				name := utf16.Encode([]rune(filepath.Base(initial)))
				if len(name) < len(buf)-1 {
					copy(buf, name)
				}
			}
		}
		ofn := &openFileName{Owner: owner, File: &buf[0], MaxFile: uint32(len(buf))}
		ofn.StructSize = uint32(unsafe.Sizeof(*ofn))
		var filter []uint16
		var proc *syscall.LazyProc
		if save {
			filter = multiString("Matroska video (*.mkv)", "*.mkv", "MP4 video (*.mp4)", "*.mp4")
			ofn.FilterIndex = 1
			ofn.DefExt = u16("mkv")
			if strings.EqualFold(filepath.Ext(initial), ".mp4") {
				ofn.FilterIndex = 2
				ofn.DefExt = u16("mp4")
			}
			ofn.Title = u16("Save transcoded video as")
			if dialogKind == "json" {
				filter = multiString("Keep the Light profiles (*.json)", "*.json")
				ofn.FilterIndex, ofn.DefExt, ofn.Title = 1, u16("json"), u16("Export profiles")
			}
			ofn.Flags = ofnExplorer | ofnOverwritePrompt | ofnPathMustExist | ofnHideReadOnly | ofnNoChangeDir | ofnEnableSizing
			proc = procGetSaveFileName
		} else {
			filter = multiString(
				"Video files", "*.mkv;*.mp4;*.m4v;*.mov;*.ts;*.m2ts;*.mts;*.webm;*.avi;*.hevc;*.h265",
				"All files (*.*)", "*.*")
			ofn.FilterIndex = 1
			ofn.Title = u16("Choose a video file")
			switch dialogKind {
			case "image":
				filter = multiString("Images (*.jpg, *.png)", "*.jpg;*.jpeg;*.png")
				ofn.Title = u16("Choose a cover image")
			case "wav":
				filter = multiString("Sounds (*.wav)", "*.wav")
				ofn.Title = u16("Choose a sound")
			case "json":
				filter = multiString("Keep the Light profiles (*.json)", "*.json")
				ofn.Title = u16("Import profiles")
			}
			ofn.Flags = ofnExplorer | ofnFileMustExist | ofnPathMustExist | ofnHideReadOnly | ofnNoChangeDir | ofnEnableSizing
			if multi {
				ofn.Flags |= ofnAllowMultiselect
				ofn.Title = u16("Choose video files")
			}
			proc = procGetOpenFileName
		}
		ofn.Filter = &filter[0]
		if dir != "" {
			ofn.InitialDir = u16(dir)
		}
		r, _, _ := proc.Call(uintptr(unsafe.Pointer(ofn)))
		runtime.KeepAlive(buf)
		runtime.KeepAlive(filter)
		runtime.KeepAlive(ofn)
		if r == 0 {
			if code, _, _ := procCommDlgExtErr.Call(); code == 0x3003 {
				return "", errors.New("too many files chosen at once; add them in smaller groups or use Add folder")
			} else if code != 0 {
				return "", fmt.Errorf("file dialog error 0x%X", code)
			}
			return "", nil // cancelled
		}
		if !multi {
			return syscall.UTF16ToString(buf), nil
		}
		// Several files: "folder\0name1\0name2\0\0". One file: "full path\0\0".
		var parts []string
		for start, i := 0, 0; i < len(buf); i++ {
			if buf[i] == 0 {
				if i == start {
					break
				}
				parts = append(parts, syscall.UTF16ToString(buf[start:i]))
				start = i + 1
			}
		}
		if len(parts) <= 1 {
			return strings.Join(parts, ""), nil
		}
		paths := make([]string, 0, len(parts)-1)
		for _, name := range parts[1:] {
			paths = append(paths, filepath.Join(parts[0], name))
		}
		return strings.Join(paths, "\n"), nil
	})
}

func folderDialog() (string, error) {
	return withOwner(func(owner uintptr) (string, error) {
		display := make([]uint16, 260)
		bi := &browseInfo{Owner: owner, DisplayName: &display[0], Title: u16("Choose a folder for temporary files"),
			Flags: 0x40 | 0x01} // BIF_NEWDIALOGSTYLE | BIF_RETURNONLYFSDIRS
		pidl, _, _ := procSHBrowse.Call(uintptr(unsafe.Pointer(bi)))
		runtime.KeepAlive(display)
		runtime.KeepAlive(bi)
		if pidl == 0 {
			return "", nil
		}
		defer procCoTaskMemFree.Call(pidl)
		path := make([]uint16, 32768)
		if ok, _, _ := procSHGetPath.Call(pidl, uintptr(unsafe.Pointer(&path[0]))); ok == 0 {
			return "", errors.New("could not read the chosen folder")
		}
		return syscall.UTF16ToString(path), nil
	})
}

func browseDialog(kind, initial string) (string, error) {
	var p string
	var err error
	switch kind {
	case "open":
		p, err = fileDialog(false, initial)
	case "save":
		p, err = fileDialog(true, initial)
	case "folder":
		p, err = folderDialog()
	case "image":
		p, err = imageDialog(initial)
	case "wav":
		p, err = wavDialog(initial)
	case "openJson":
		p, err = specialDialog("json", initial)
	case "saveJson":
		dialogKind = "json"
		p, err = fileDialog(true, initial)
		dialogKind = ""
	default:
		return "", errors.New("unknown dialog")
	}
	if err != nil {
		// Fall back to a PowerShell dialog if the native one failed to open.
		if p2, err2 := browseDialogPS(kind, initial); err2 == nil {
			return p2, nil
		} else {
			return "", fmt.Errorf("%v; PowerShell fallback also failed: %v", err, err2)
		}
	}
	return p, nil
}

// browseFiles lets the user pick several video files.
func browseFiles(initial string) ([]string, error) {
	p, err := withRetryPS(func() (string, error) { return fileDialogEx(false, true, initial) }, "openMulti", initial)
	if err != nil || p == "" {
		return nil, err
	}
	return strings.Split(p, "\n"), nil
}

// withRetryPS runs a native dialog and falls back to PowerShell if it fails to open.
func withRetryPS(native func() (string, error), kind, initial string) (string, error) {
	p, err := native()
	if err == nil {
		return p, nil
	}
	if strings.Contains(err.Error(), "too many files") {
		return "", err
	}
	p2, err2 := browseDialogPS(kind, initial)
	if err2 != nil {
		return "", fmt.Errorf("%v; PowerShell fallback also failed: %v", err, err2)
	}
	return p2, nil
}
