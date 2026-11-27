//go:build !windows

package main

import "errors"

// Elsewhere there is no Start menu; tests replace these.
var (
	startMenuPath  = func() string { return "" }
	makeShortcut   = func(lnk, exe string) error { return errors.New("no Start menu here") }
	shortcutTarget = func(lnk string) (string, error) { return "", errors.New("no Start menu here") }
)
