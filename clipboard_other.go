//go:build !windows

package main

import "errors"

// No system clipboard here: the page uses the browser's own.
func readClipboard() (string, error)   { return "", errors.New("no clipboard") }
func writeClipboard(text string) error { return errors.New("no clipboard") }
