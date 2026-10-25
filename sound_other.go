//go:build !windows

package main

func mediaDir() string                                { return "/usr/share/sounds" }
func playSoundRaw(name string, alias, wait bool) bool { return false }
func flashTaskbar()                                   {}
