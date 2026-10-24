package main

import (
	"reflect"
	"testing"
	"time"
)

func TestSingleInstanceHandOff(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // settings folder (non-Windows)
	t.Setenv("LOCALAPPDATA", t.TempDir())    // settings folder (Windows)
	first := newGUIServer()
	if claimInstance(first) {
		t.Fatal("the first instance handed off to nobody")
	}
	defer releaseInstance()
	raised := make(chan struct{}, 1)
	first.raise = func() { raised <- struct{}{} }

	second := newGUIServer(`D:\films\a.mkv`, `D:\films\b.mkv`)
	if !claimInstance(second) {
		t.Fatal("a second start did not hand its files to the first window")
	}
	select {
	case <-raised:
	case <-time.After(2 * time.Second):
		t.Fatal("the first window was not brought to the front")
	}
	first.mu.Lock()
	got := first.takeInboxLocked()
	first.mu.Unlock()
	if !reflect.DeepEqual(got, []string{`D:\films\a.mkv`, `D:\films\b.mkv`}) {
		t.Fatalf("files received: %v", got)
	}
	if st := first.status(0); st["open"] != nil && len(st["open"].([]string)) != 0 {
		t.Fatal("files were handed to the page twice")
	}

	// a start with no files just brings the window forward
	if !claimInstance(newGUIServer()) {
		t.Fatal("plain second start not handed off")
	}
	<-raised

	// once the first instance has gone, the next start becomes the window
	releaseInstance()
	third := newGUIServer()
	if claimInstance(third) {
		t.Fatal("handed off to an instance that has closed")
	}
}
