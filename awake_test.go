package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// fakeAwake stands in for Windows' power request and records what is held.
type fakeAwake struct {
	mu     sync.Mutex
	held   int // requests held now
	screen bool
	takes  int
}

func (f *fakeAwake) install(t *testing.T) {
	old := holdAwake
	holdAwake = func(screen bool) (func(), error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.held++
		f.takes++
		f.screen = screen
		once := sync.Once{}
		return func() {
			once.Do(func() {
				f.mu.Lock()
				f.held--
				f.mu.Unlock()
			})
		}, nil
	}
	t.Cleanup(func() { holdAwake = old })
}

func (f *fakeAwake) state() (held int, screen bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.held, f.screen
}

// The PC is kept awake for the whole run, not while paused, and let go at the end.
func TestKeepAwakeDuringRun(t *testing.T) {
	fakeTools(t)
	var fa fakeAwake
	fa.install(t)
	dir := t.TempDir()
	var raws []json.RawMessage
	for _, n := range []string{"slow", "b"} {
		p := filepath.Join(dir, n+".mkv")
		_ = os.WriteFile(p, []byte("video"), 0o644)
		raws = append(raws, itemSettings(p, filepath.Join(dir, n+"_KTL.mkv")))
	}
	g := newGUIServer()
	g.prefsPath = filepath.Join(t.TempDir(), "settings.json")
	g.keepAwake, g.keepScreen = true, true
	if _, errs := g.queueAdd(raws); len(errs) > 0 {
		t.Fatal(errs)
	}
	waitFor(t, "files to be checked", func() bool { return !strings.Contains(strings.Join(statuses(g), " "), stChecking) })
	if h, _ := fa.state(); h != 0 {
		t.Fatal("held before the run")
	}
	if err := g.startQueue(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the slow file to run", func() bool {
		g.mu.Lock()
		defer g.mu.Unlock()
		return g.job != nil && g.job.running && g.job.cmd != nil && g.job.cmd.Process != nil
	})
	if h, sc := fa.state(); h != 1 || !sc {
		t.Fatalf("while running: held %d, screen %v", h, sc)
	}
	// the job's own request is left to the window's: the job is told it may sleep only when the setting is off
	g.mu.Lock()
	var args string
	for _, it := range g.q.items {
		if it.Status == stRunning && it.cfg != nil {
			args = strings.Join(it.cfg.cliArgs(), " ")
		}
	}
	g.mu.Unlock()
	if strings.Contains(args, "-allow-sleep") {
		t.Errorf("job told it may sleep: %s", args)
	}
	ctx := context.Background()
	g.call(ctx, "pause", nil, nil)
	if h, _ := fa.state(); h != 0 {
		t.Error("still held while paused")
	}
	g.call(ctx, "resume", nil, nil)
	if h, _ := fa.state(); h != 1 {
		t.Error("not held again after resume")
	}
	// the setting changes during the run: screen off
	g.call(ctx, "keepawake", nil, []byte(`{"on":true,"screen":false}`))
	if h, sc := fa.state(); h != 1 || sc {
		t.Errorf("after turning the screen off: held %d, screen %v", h, sc)
	}
	g.stopQueue()
	waitFor(t, "the run to end", func() bool {
		g.mu.Lock()
		defer g.mu.Unlock()
		return !g.q.running
	})
	waitFor(t, "the request to be given back", func() bool { h, _ := fa.state(); return h == 0 })
}

// Turned off: nothing is held, and each job is told it may let the PC sleep.
func TestKeepAwakeOff(t *testing.T) {
	var fa fakeAwake
	fa.install(t)
	g := newGUIServer()
	g.prefsPath = filepath.Join(t.TempDir(), "settings.json")
	g.call(context.Background(), "keepawake", nil, []byte(`{"on":false,"screen":true}`))
	g.mu.Lock()
	g.q.running = true
	g.mu.Unlock()
	g.syncAwake()
	if h, _ := fa.state(); h != 0 {
		t.Error("held although turned off")
	}
	cfg := &Config{Input: "a.mkv", Output: "b.mkv", Codec: "x265", CRF: 20, Preset: "medium", DV: "auto", AllowSleep: true}
	if a := strings.Join(cfg.cliArgs(), " "); !strings.Contains(a, "-allow-sleep") {
		t.Errorf("command line: %s", a)
	}
	old := os.Args
	defer func() { os.Args = old }()
	os.Args = append([]string{"keepthelight"}, cfg.cliArgs()...)
	if c, ok := parseArgs(); !ok || !c.AllowSleep {
		t.Error("-allow-sleep not read")
	}
	// remembered
	g2 := newGUIServer()
	g2.prefsPath = g.prefsPath
	g2.restoreQueue()
	if g2.keepAwake || !g2.keepScreen {
		t.Errorf("after a restart: awake %v, screen %v", g2.keepAwake, g2.keepScreen)
	}
	// a first start: kept awake; the screen only on PCs with Modern Standby (none here)
	g3 := newGUIServer()
	g3.prefsPath = filepath.Join(t.TempDir(), "settings.json")
	g3.restoreQueue()
	if !g3.keepAwake || g3.keepScreen != modernStandby() {
		t.Errorf("first start: awake %v, screen %v", g3.keepAwake, g3.keepScreen)
	}
}
