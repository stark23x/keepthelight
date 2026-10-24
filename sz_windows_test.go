//go:build windows

package main

import (
	"testing"
	"unsafe"
)

func TestStructSizes(t *testing.T) {
	if s := unsafe.Sizeof(openFileName{}); s != 152 {
		t.Fatalf("OPENFILENAMEW size %d, want 152", s)
	}
	if s := unsafe.Sizeof(browseInfo{}); s != 64 {
		t.Fatalf("BROWSEINFOW size %d, want 64", s)
	}
}
