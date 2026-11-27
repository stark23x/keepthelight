//go:build !windows

package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// The Start menu shortcut: added on the first start, kept pointing at the copy
// last run, and never put back once removed (in Settings or by hand).
func TestStartMenu(t *testing.T) {
	dir := t.TempDir()
	lnk := filepath.Join(dir, "Programs", startMenuName)
	oldP, oldM, oldT := startMenuPath, makeShortcut, shortcutTarget
	t.Cleanup(func() { startMenuPath, makeShortcut, shortcutTarget = oldP, oldM, oldT })
	startMenuPath = func() string { return lnk }
	makes := 0
	makeShortcut = func(l, exe string) error {
		makes++
		_ = os.MkdirAll(filepath.Dir(l), 0o755)
		return os.WriteFile(l, []byte(exe), 0o644)
	}
	shortcutTarget = func(l string) (string, error) {
		b, err := os.ReadFile(l)
		if err != nil {
			return "", errors.New("none")
		}
		return string(b), nil
	}
	g := newGUIServer()
	g.prefsPath = filepath.Join(dir, "settings.json")

	g.startMenuAtStart() // first start
	if st := g.startMenuInfo(); !st.Present || !st.Here || g.startMenuPref() != "on" || makes != 1 {
		t.Fatalf("first start: %+v pref=%q makes=%d", st, g.startMenuPref(), makes)
	}
	g.startMenuAtStart() // again: nothing to do
	if makes != 1 {
		t.Errorf("a shortcut that is right was made again (%d)", makes)
	}
	_ = os.WriteFile(lnk, []byte(`D:\old copy\Keep the Light.exe`), 0o644) // another copy was run last
	if st := g.startMenuInfo(); !st.Present || st.Here {
		t.Errorf("another copy: %+v", st)
	}
	g.startMenuAtStart()
	if st := g.startMenuInfo(); !st.Here || makes != 2 {
		t.Errorf("not pointed at this copy: %+v", st)
	}
	if st := g.startMenuSet(false); st.Present || g.startMenuPref() != "off" { // removed in Settings
		t.Errorf("remove: %+v", st)
	}
	g.startMenuAtStart()
	if _, err := os.Stat(lnk); err == nil {
		t.Error("put back after being removed")
	}
	if st := g.startMenuSet(true); !st.Present || !st.Here || g.startMenuPref() != "on" { // added in Settings
		t.Errorf("add: %+v", st)
	}
	_ = os.Remove(lnk) // deleted by hand
	g.startMenuAtStart()
	if _, err := os.Stat(lnk); err == nil || g.startMenuPref() != "off" {
		t.Errorf("deleted by hand was put back: pref=%q", g.startMenuPref())
	}
}
