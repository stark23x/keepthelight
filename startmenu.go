package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// ---------- the Start menu shortcut ----------
//
// Keep the Light stays portable (no installer, the owner's choice), but makes
// its own Start menu shortcut: on the first start of the window, and with Add /
// Remove in ⚙ Settings. On every start, a shortcut that points at another copy
// (a new version unzipped elsewhere) is pointed at this one, so it always opens
// the copy last run. The setting "startMenu" is "on" or "off" (none yet: the
// first start adds it). A shortcut deleted by hand counts as removed.

const startMenuName = "Keep the Light.lnk"

// startMenuState is what ⚙ Settings shows.
type startMenuState struct {
	Supported bool   `json:"supported"`
	Present   bool   `json:"present"`
	Here      bool   `json:"here"`             // it opens this copy
	Target    string `json:"target,omitempty"` // the exe it opens
	Error     string `json:"error,omitempty"`
}

func (g *guiServer) startMenuPref() string {
	var s string
	_ = json.Unmarshal(g.loadPrefs()["startMenu"], &s)
	return s
}

func (g *guiServer) setStartMenuPref(v string) {
	b, _ := json.Marshal(v)
	_ = g.savePref("startMenu", b)
}

// thisExe is the window's own program (the shortcut's target).
func thisExe() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	// the window opened from the command-line program: the shortcut still opens the window's own program
	if gui := filepath.Join(filepath.Dir(exe), "Keep the Light.exe"); !strings.EqualFold(filepath.Base(exe), "Keep the Light.exe") {
		if _, err := os.Stat(gui); err == nil {
			return gui
		}
	}
	return exe
}

func sameExe(a, b string) bool {
	return a != "" && b != "" && strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

// startMenuAtStart runs when the window opens (in the background).
func (g *guiServer) startMenuAtStart() {
	lnk, exe := startMenuPath(), thisExe()
	if lnk == "" || exe == "" {
		return
	}
	switch g.startMenuPref() {
	case "off":
		return
	case "":
		if _, err := os.Stat(lnk); err != nil { // first start: add it
			if makeShortcut(lnk, exe) == nil {
				g.setStartMenuPref("on")
			}
			return
		}
		g.setStartMenuPref("on")
		fallthrough
	default: // "on"
		if _, err := os.Stat(lnk); err != nil {
			g.setStartMenuPref("off") // deleted by hand: not put back
			return
		}
		if t, err := shortcutTarget(lnk); err == nil && !sameExe(t, exe) {
			_ = makeShortcut(lnk, exe) // a new copy: open this one from now on
		}
	}
}

func (g *guiServer) startMenuInfo() startMenuState {
	lnk, exe := startMenuPath(), thisExe()
	st := startMenuState{Supported: lnk != "" && exe != ""}
	if !st.Supported {
		return st
	}
	if _, err := os.Stat(lnk); err == nil {
		st.Present = true
		if t, err := shortcutTarget(lnk); err == nil {
			st.Target, st.Here = t, sameExe(t, exe)
		}
	}
	return st
}

// startMenuSet adds (pointing at this copy) or removes the shortcut, and
// remembers the choice.
func (g *guiServer) startMenuSet(on bool) startMenuState {
	lnk, exe := startMenuPath(), thisExe()
	var err error
	switch {
	case lnk == "" || exe == "":
		return startMenuState{}
	case on:
		err = makeShortcut(lnk, exe)
		if err == nil {
			g.setStartMenuPref("on")
		}
	default:
		if err = os.Remove(lnk); err == nil || os.IsNotExist(err) {
			err = nil
			g.setStartMenuPref("off")
		}
	}
	st := g.startMenuInfo()
	if err != nil {
		st.Error = err.Error()
	}
	return st
}
