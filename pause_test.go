package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// procState is a process's state letter from /proc (T = stopped), "" if unknown.
func procState(pid int) string {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return ""
	}
	s := string(b)
	if i := strings.LastIndex(s, ")"); i >= 0 && i+2 < len(s) {
		return s[i+2 : i+3]
	}
	return ""
}

// Pause freezes the running job and Resume lets it carry on; Stop after this
// file lets the queue end once the running file is done, leaving the rest waiting.
func TestPauseAndStopAfter(t *testing.T) {
	fakeTools(t)
	if _, err := os.Stat("/proc/self/stat"); err != nil {
		t.Skip("no /proc")
	}
	dir := t.TempDir()
	var raws []json.RawMessage
	for _, n := range []string{"slow", "b", "c"} {
		p := filepath.Join(dir, n+".mkv")
		_ = os.WriteFile(p, []byte("video"), 0o644)
		raws = append(raws, itemSettings(p, filepath.Join(dir, n+"_KTL.mkv")))
	}
	g := newGUIServer()
	g.prefsPath = filepath.Join(t.TempDir(), "settings.json")
	if _, errs := g.queueAdd(raws); len(errs) > 0 {
		t.Fatal(errs)
	}
	waitFor(t, "files to be checked", func() bool { return !strings.Contains(strings.Join(statuses(g), " "), stChecking) })
	if err := g.pauseJob(true); err == nil {
		t.Error("paused with nothing running")
	}
	if err := g.startQueue(); err != nil {
		t.Fatal(err)
	}
	var pid int
	waitFor(t, "the slow file to run", func() bool {
		g.mu.Lock()
		defer g.mu.Unlock()
		if g.job != nil && g.job.running && g.job.cmd != nil && g.job.cmd.Process != nil {
			pid = g.job.cmd.Process.Pid
			return true
		}
		return false
	})
	if err := g.pauseJob(true); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the job to be frozen", func() bool { return procState(pid) == "T" })
	g.mu.Lock()
	title, items := g.titleLocked(g.clock()), g.queueStatusLocked()["items"].([]queueItem)
	g.mu.Unlock()
	if !strings.Contains(title, "Paused") || items[0].ETA != "paused" {
		t.Errorf("while paused: title %q, row %q", title, items[0].ETA)
	}
	if err := g.pauseJob(false); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the job to run again", func() bool { s := procState(pid); return s != "T" && s != "" })

	// stop after this file: the slow one is canceled here to save time; b and c stay waiting
	g.call(context.Background(), "queueStopAfter", nil, []byte(`{"on":true}`))
	g.mu.Lock()
	on := g.q.stopAfter
	g.mu.Unlock()
	if !on {
		t.Fatal("stop after this file was not set")
	}
	if err := g.pauseJob(true); err != nil { // canceling a paused job works too
		t.Fatal(err)
	}
	g.cancel()
	waitFor(t, "the queue to stop", func() bool { g.mu.Lock(); defer g.mu.Unlock(); return !g.q.running })
	if got := strings.Join(statuses(g), " "); got != "slow.mkv=canceled b.mkv=waiting c.mkv=waiting" {
		t.Errorf("statuses: %s", got)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.q.stopAfter {
		t.Error("stop after this file still set for the next run")
	}
}
