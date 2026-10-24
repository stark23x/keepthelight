//go:build cli

package main

// cliBuild is true for keepthelight_cli.exe (built with -tags cli): it is a
// console program, and only opens the window when asked with -gui / -server.
const cliBuild = true
