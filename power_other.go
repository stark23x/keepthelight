//go:build !windows

package main

import "errors"

func preventSleep() (release func()) { return func() {} }

func powerAction(action string) error {
	return errors.New("sleep and shut down are only available on Windows")
}
