//go:build !windows

package main

import "errors"

// runWindow: the built-in window needs Windows (WebView2); elsewhere the
// browser fallback is used.
func runWindow(g *guiServer) error {
	return errors.New("the built-in window needs Windows")
}
