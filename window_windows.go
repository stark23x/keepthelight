//go:build windows

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	webview2 "github.com/jchv/go-webview2"
	"github.com/jchv/go-webview2/webviewloader"
)

// The built-in window: the same page as browser mode, shown in a WebView2
// control in our own top-level window. The page talks to Go through a bound
// function instead of HTTP, so no local web server runs in this mode.

var (
	procSetWindowLongPtr  = user32.NewProc("SetWindowLongPtrW")
	procSetWindowLong     = user32.NewProc("SetWindowLongW") // 32-bit Windows
	procCallWindowProc    = user32.NewProc("CallWindowProcW")
	procSetWindowPos      = user32.NewProc("SetWindowPos")
	procSystemParamsInfo  = user32.NewProc("SystemParametersInfoW")
	procGetDpiForSystem   = user32.NewProc("GetDpiForSystem")
	procSetDpiAwarenessCx = user32.NewProc("SetProcessDpiAwarenessContext")
	procSetProcessDPIAwr  = user32.NewProc("SetProcessDPIAware")
	procEnumThreadWindows = user32.NewProc("EnumThreadWindows")
	procGetClassName      = user32.NewProc("GetClassNameW")
	procCurrentThreadID   = kernel32.NewProc("GetCurrentThreadId")
	procMoveMemory        = kernel32.NewProc("RtlMoveMemory")
)

const (
	wmClose      = 0x0010
	wmDPIChanged = 0x02E0
	swpNoZOrder  = 0x0004
	swpNoActive  = 0x0010
	spiWorkArea  = 0x0030
	appIconID    = 1 // icon group resource written by tools/mkres
)

type winRect struct{ Left, Top, Right, Bottom int32 }

func runWindow(g *guiServer) error {
	if v, err := webviewloader.GetInstalledVersion(); err != nil {
		return fmt.Errorf("WebView2 check failed: %w", err)
	} else if v == "" {
		return errors.New("the Microsoft Edge WebView2 Runtime is not installed")
	}

	// The window, WebView2 and the message loop must all stay on this thread.
	runtime.LockOSThread()
	setDPIAware()

	dpi := systemDPI()
	scale := func(v int) int { return v * dpi / 96 }
	wa := workArea()
	waW, waH := int(wa.Right-wa.Left), int(wa.Bottom-wa.Top)
	width, height := min(scale(1000), waW*95/100), min(scale(880), waH*95/100)

	wvlog := &webviewLog{g: g}
	log.SetOutput(wvlog)
	defer log.SetOutput(os.Stderr)

	w := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug:     os.Getenv("KTL_DEBUG") != "", // right-click menu and DevTools
		DataPath:  webviewDataDir(),
		AutoFocus: true,
		WindowOptions: webview2.WindowOptions{
			Title: windowTitle, Width: uint(width), Height: uint(height), IconId: appIconID,
		},
	})
	if w == nil {
		destroyOurWindows()
		return errors.New("WebView2 could not be started" + wvlog.last())
	}
	hwnd := uintptr(w.Window())
	_, _, _ = procSetWindowPos.Call(hwnd, 0,
		uintptr(int(wa.Left)+(waW-width)/2), uintptr(int(wa.Top)+(waH-height)/2),
		uintptr(width), uintptr(height), swpNoZOrder|swpNoActive)
	w.SetSize(scale(600), scale(520), webview2.HintMin)

	dialogParent.Store(hwnd)
	g.notify = func(msg string) { messageBox(hwnd, msg, true) }
	g.ask = func(msg string) bool { return askYesNo(hwnd, msg) }
	hookWindow(hwnd, g)

	// Requests from the page run on their own goroutines (probing a file or
	// showing a file dialog must not freeze the window) and are answered by
	// calling window.__ktlDone(id, result) back on the UI thread.
	_ = w.Bind("ktlPost", func(id int, path string, body json.RawMessage) {
		go func() {
			var res any
			if u, err := url.Parse(path); err != nil {
				res = failResult("bad request")
			} else {
				res = g.call(context.Background(), u.Path, u.Query(), body)
			}
			b, err := json.Marshal(res)
			if err != nil {
				b, _ = json.Marshal(failResult(err.Error()))
			}
			w.Dispatch(func() { w.Eval(fmt.Sprintf("window.__ktlDone(%d,%s)", id, b)) })
		}()
	})

	// Progress in the title, so it shows on the taskbar while minimised.
	go func() {
		last := windowTitle
		for range time.Tick(time.Second) {
			title := g.windowTitleNow()
			if title != last {
				last = title
				w.Dispatch(func() { w.SetTitle(title) })
			}
		}
	}()

	w.SetHtml(string(g.page()))
	w.Run()

	// Window gone (closed after confirming, or Windows is shutting down).
	if g.running() {
		g.cancelAndWait(15 * time.Second)
	}
	return nil
}

// ---- window procedure hook: confirm close while working; follow DPI changes ----

var (
	origWndProc uintptr
	hookedGUI   *guiServer
)

func hookWindow(hwnd uintptr, g *guiServer) {
	hookedGUI = g
	const gwlpWndProc = ^uintptr(3) // -4
	set := procSetWindowLongPtr
	if set.Find() != nil {
		set = procSetWindowLong
	}
	origWndProc, _, _ = set.Call(hwnd, gwlpWndProc, syscall.NewCallback(hookProc))
}

func hookProc(hwnd, msg, wp, lp uintptr) uintptr {
	switch msg {
	case wmClose:
		if hookedGUI.running() {
			if !askYesNo(hwnd, "A transcode is still running.\n\nStop it and close Keep the Light? The unfinished output file will be deleted.") {
				return 0
			}
			hookedGUI.cancelAndWait(15 * time.Second)
		}
	case wmDPIChanged: // moved to a monitor with different scaling: take the suggested size
		var r winRect // lParam points to the suggested RECT; copy it out
		_, _, _ = procMoveMemory.Call(uintptr(unsafe.Pointer(&r)), lp, unsafe.Sizeof(r))
		_, _, _ = procSetWindowPos.Call(hwnd, 0, uintptr(r.Left), uintptr(r.Top),
			uintptr(r.Right-r.Left), uintptr(r.Bottom-r.Top), swpNoZOrder|swpNoActive)
		return 0
	}
	r, _, _ := procCallWindowProc.Call(origWndProc, hwnd, msg, wp, lp)
	return r
}

// ---- helpers ----

// setDPIAware keeps text sharp on scaled displays (per-monitor v2 where
// available). The manifest normally does this already; this covers builds
// without it.
func setDPIAware() {
	if procSetDpiAwarenessCx.Find() == nil {
		const perMonitorAwareV2 = ^uintptr(3) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 (-4)
		if r, _, _ := procSetDpiAwarenessCx.Call(perMonitorAwareV2); r != 0 {
			return
		}
	}
	if procSetProcessDPIAwr.Find() == nil {
		_, _, _ = procSetProcessDPIAwr.Call()
	}
}

func systemDPI() int {
	if procGetDpiForSystem.Find() == nil {
		if d, _, _ := procGetDpiForSystem.Call(); d >= 96 {
			return int(d)
		}
	}
	return 96
}

func workArea() winRect {
	var r winRect
	if ok, _, _ := procSystemParamsInfo.Call(spiWorkArea, 0, uintptr(unsafe.Pointer(&r)), 0); ok == 0 || r.Right <= r.Left {
		return winRect{0, 0, 1280, 800}
	}
	return r
}

// webviewDataDir is where WebView2 keeps its profile (cache etc.). The
// library's default is the roaming AppData folder; local is the right place.
func webviewDataDir() string {
	d := filepath.Join(appDataDir(), "WebView2")
	_ = os.MkdirAll(d, 0o755)
	return d
}

// destroyOurWindows removes a half-built window left behind when WebView2
// failed to start, before falling back to browser mode.
func destroyOurWindows() {
	tid, _, _ := procCurrentThreadID.Call()
	cb := syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
		buf := make([]uint16, 64)
		n, _, _ := procGetClassName.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		if syscall.UTF16ToString(buf[:n]) == "webview" {
			_, _, _ = procDestroyWindow.Call(hwnd)
		}
		return 1
	})
	_, _, _ = procEnumThreadWindows.Call(tid, cb, 0)
}

// webviewLog receives go-webview2's log output. Messages go to a log file next
// to the WebView2 profile. Some of the library's start-up failures end the
// process (log.Fatal) instead of returning an error; when one of those is
// reported, browser mode is started in a new process first, so the user still
// gets a window.
type webviewLog struct {
	g    *guiServer
	mu   sync.Mutex
	msgs []string
	once sync.Once
}

func (l *webviewLog) Write(p []byte) (int, error) {
	msg := strings.TrimSpace(string(p))
	l.mu.Lock()
	l.msgs = append(l.msgs, msg)
	l.mu.Unlock()
	if f, err := os.OpenFile(filepath.Join(filepath.Dir(webviewDataDir()), "webview2.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
		fmt.Fprintf(f, "%s %s\n", time.Now().Format("2006-01-02 15:04:05"), msg)
		f.Close()
	}
	if strings.Contains(msg, "failed with") { // "Creating environment/controller failed with ..." -> log.Fatalf follows
		l.once.Do(func() { startBrowserFallback(l.g.initial, msg) })
	}
	return len(p), nil
}

func (l *webviewLog) last() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.msgs) == 0 {
		return ""
	}
	return ": " + l.msgs[len(l.msgs)-1]
}

func startBrowserFallback(initial []string, reason string) {
	destroyOurWindows()
	exe, err := os.Executable()
	if err == nil {
		cmd := exec.Command(exe, append([]string{"-server"}, initial...)...)
		if err = cmd.Start(); err == nil {
			return
		}
	}
	messageBox(0, "The Keep the Light window could not be opened ("+reason+").\n\nTry starting it with:  \"Keep the Light.exe\" -server", true)
}
