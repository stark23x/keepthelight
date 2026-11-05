package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// recordSounds replaces the player and returns what it was asked to play.
func recordSounds(t *testing.T) func() []string {
	var mu sync.Mutex
	var played []string
	old := playSoundFn
	playSoundFn = func(id string, wait bool) bool { mu.Lock(); played = append(played, id); mu.Unlock(); return true }
	t.Cleanup(func() { playSoundFn = old })
	return func() []string { mu.Lock(); defer mu.Unlock(); return append([]string(nil), played...) }
}

func runToEnd(t *testing.T, g *guiServer, names ...string) {
	t.Helper()
	dir := t.TempDir()
	var raws []json.RawMessage
	for _, n := range names {
		p := filepath.Join(dir, n+".mkv")
		_ = os.WriteFile(p, []byte("x"), 0o644)
		raws = append(raws, itemSettings(p, filepath.Join(dir, n+"_KTL.mkv")))
	}
	g.queueAdd(raws)
	waitFor(t, "check", func() bool {
		for _, s := range statuses(g) {
			if s[len(s)-len(stChecking):] == stChecking {
				return false
			}
		}
		return true
	})
	if err := g.startQueue(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the queue to finish", func() bool { g.mu.Lock(); defer g.mu.Unlock(); return !g.q.running })
}

// waitSounds: the sound is played just after the queue ends, so give it a moment.
func waitSounds(t *testing.T, played func() []string, n int) {
	t.Helper()
	for end := time.Now().Add(3 * time.Second); len(played()) < n && time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
	}
}

func TestSoundOncePerRun(t *testing.T) {
	fakeTools(t)
	played := recordSounds(t)
	g := newGUIServer()
	g.prefsPath = filepath.Join(t.TempDir(), "settings.json")

	runToEnd(t, g, "one", "two", "three") // a queue: one sound, after the last file
	waitSounds(t, played, 1)
	if p := played(); len(p) != 1 || p[0] != defaultSounds.Done {
		t.Fatalf("all done: %v", p)
	}
	runToEnd(t, g, "good", "bad") // a file failed: the failure sound
	waitSounds(t, played, 2)
	if p := played(); len(p) != 2 || p[1] != defaultSounds.Fail {
		t.Fatalf("with a failure: %v", p)
	}

	// the user's choices; "none" is silent
	raw, _ := json.Marshal(soundPrefs{Done: "", Fail: "file:/x/alarm.wav", Flash: false})
	_ = g.savePref("sounds", raw)
	runToEnd(t, g, "quiet")
	if p := played(); len(p) != 2 {
		t.Fatalf("none must be silent: %v", p)
	}
	runToEnd(t, g, "bad2")
	waitSounds(t, played, 3)
	if p := played(); len(p) != 3 || p[2] != "file:/x/alarm.wav" {
		t.Fatalf("own sound: %v", p)
	}
}

func TestSoundList(t *testing.T) {
	l := soundList()
	if a := l["aliases"].([]map[string]string); len(a) < 5 || a[0]["name"] != "Notification" {
		t.Errorf("%v", l)
	}
	if playSound("nonsense", false) || playSound("", false) {
		t.Error("an unknown sound id played")
	}
}
