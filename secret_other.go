//go:build !windows

package main

import "errors"

// No credential store here: the key stays in the settings file.
func secretStoreOK() bool                    { return false }
func writeSecret(name, value string) error   { return errors.New("no credential store") }
func readSecret(name string) (string, error) { return "", errors.New("no credential store") }
func deleteSecret(name string) error         { return nil }
