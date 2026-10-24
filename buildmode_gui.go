//go:build !cli

package main

// cliBuild is false for Keep the Light.exe: started without arguments (or with a
// dragged-on video) it opens the window.
const cliBuild = false
