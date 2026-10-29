//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

// Secrets (the TMDB key) are kept in Windows' Credential Manager, for this
// Windows account only, instead of in the settings file.

var (
	advapi32         = syscall.NewLazyDLL("advapi32.dll")
	procCredWriteW   = advapi32.NewProc("CredWriteW")
	procCredReadW    = advapi32.NewProc("CredReadW")
	procCredDeleteW  = advapi32.NewProc("CredDeleteW")
	procCredFree     = advapi32.NewProc("CredFree")
	errNoSecret      = syscall.Errno(1168) // ERROR_NOT_FOUND
	credTypeGeneric  = uint32(1)
	credPersistLocal = uint32(2) // CRED_PERSIST_LOCAL_MACHINE: this PC, this account
)

type credential struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        syscall.Filetime
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
}

func secretStoreOK() bool { return procCredWriteW.Find() == nil }

func writeSecret(name, value string) error {
	target, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return err
	}
	user, _ := syscall.UTF16PtrFromString("Keep the Light")
	blob := []byte(value)
	c := credential{Type: credTypeGeneric, TargetName: target, Persist: credPersistLocal, UserName: user, CredentialBlobSize: uint32(len(blob))}
	if len(blob) > 0 {
		c.CredentialBlob = &blob[0]
	}
	if r, _, e := procCredWriteW.Call(uintptr(unsafe.Pointer(&c)), 0); r == 0 {
		return e
	}
	return nil
}

func readSecret(name string) (string, error) {
	target, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return "", err
	}
	var p *credential
	if r, _, e := procCredReadW.Call(uintptr(unsafe.Pointer(target)), uintptr(credTypeGeneric), 0, uintptr(unsafe.Pointer(&p))); r == 0 {
		return "", e
	}
	defer procCredFree.Call(uintptr(unsafe.Pointer(p)))
	if p.CredentialBlobSize == 0 || p.CredentialBlob == nil {
		return "", nil
	}
	return string(unsafe.Slice(p.CredentialBlob, p.CredentialBlobSize)), nil
}

func deleteSecret(name string) error {
	target, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return err
	}
	if r, _, e := procCredDeleteW.Call(uintptr(unsafe.Pointer(target)), uintptr(credTypeGeneric), 0); r == 0 && e != errNoSecret {
		return e
	}
	return nil
}
