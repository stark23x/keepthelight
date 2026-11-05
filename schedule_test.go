package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCrossed(t *testing.T) {
	d := func(h, m int) time.Time { return time.Date(2026, 11, 5, h, m, 0, 0, time.Local) }
	if !crossed("01:00", d(0, 59), d(1, 0)) || crossed("01:00", d(1, 0), d(1, 1)) || crossed("01:00", time.Time{}, d(1, 0)) {
		t.Error("01:00")
	}
	// over midnight
	if !crossed("00:00", d(23, 59), d(23, 59).Add(2*time.Minute)) {
		t.Error("midnight")
	}
	if crossed("bad", d(0, 0), d(2, 0)) || !validHM("07:30") || validHM("7:30") {
		t.Error("checks")
	}
}

// The queue starts at its time, pauses at the pause time and carries on at the start time.
func TestScheduleStartsAndPauses(t *testing.T) {
	fakeTools(t)
	g := newGUIServer()
	g.prefsPath = filepath.Join(t.TempDir(), "settings.json")
	now := time.Date(2026, 11, 5, 0, 59, 50, 0, time.Local)
	g.now = func() time.Time { return now }
	if err := g.setSchedule(schedule{PauseOn: true, PauseAt: "07:00"}); err == nil {
		t.Error("a pause time without a start time was accepted")
	}
	if err := g.setSchedule(schedule{StartOn: true, StartAt: "01:00", PauseOn: true, PauseAt: "01:02"}); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	var raws []json.RawMessage
	for _, n := range []string{"slow", "b"} {
		p := filepath.Join(dir, n+".mkv")
		_ = os.WriteFile(p, []byte("video"), 0o644)
		raws = append(raws, itemSettings(p, filepath.Join(dir, n+"_KTL.mkv")))
	}
	g.queueAdd(raws)
	waitFor(t, "files to be checked", func() bool { return !strings.Contains(strings.Join(statuses(g), " "), stChecking) })
	g.scheduleTick() // 00:59:50: nothing yet
	if g.q.running {
		t.Fatal("started early")
	}
	now = now.Add(20 * time.Second) // 01:00:10
	g.scheduleTick()
	waitFor(t, "the slow file to run", func() bool {
		g.mu.Lock()
		defer g.mu.Unlock()
		return g.job != nil && g.job.running && g.job.cmd != nil && g.job.cmd.Process != nil
	})
	now = now.Add(2 * time.Minute) // 01:02:10: pause
	g.scheduleTick()
	g.mu.Lock()
	paused, by, plan := g.job.paused, g.sched.pausedBy, g.sched.text()
	g.mu.Unlock()
	if !paused || !by || !strings.Contains(plan, "paused by the schedule") {
		t.Errorf("pause: %v %v %q", paused, by, plan)
	}
	now = now.Add(24*time.Hour - 2*time.Minute - 20*time.Second) // just before the next 01:00
	g.scheduleTick()
	now = now.Add(20 * time.Second) // 01:00: carry on
	g.scheduleTick()
	g.mu.Lock()
	paused = g.job.paused
	g.mu.Unlock()
	if paused {
		t.Error("not resumed at the start time")
	}
	g.stopQueue()
	waitFor(t, "the queue to stop", func() bool { g.mu.Lock(); defer g.mu.Unlock(); return !g.q.running })
}

// The watch folder offers a file once it has stopped growing, and only once.
func TestWatchFolder(t *testing.T) {
	g := newGUIServer()
	g.prefsPath = filepath.Join(t.TempDir(), "settings.json")
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.Local)
	g.now = func() time.Time { return now }
	dir := t.TempDir()
	f := filepath.Join(dir, "New Film.mkv")
	_ = os.WriteFile(f, []byte("part"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "Old Film_KTL.mkv"), []byte("made by us"), 0o644)
	look := func() []string {
		p, err := g.watchScan(dir, "_KTL", "")
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	if p := look(); len(p) != 0 {
		t.Errorf("offered while arriving: %v", p)
	}
	now = now.Add(30 * time.Second)
	_ = os.WriteFile(f, []byte("partly more"), 0o644) // still being copied
	if p := look(); len(p) != 0 {
		t.Errorf("offered while growing: %v", p)
	}
	now = now.Add(30 * time.Second)
	if p := look(); len(p) != 1 || p[0] != f {
		t.Errorf("finished copying: %v", p)
	}
	now = now.Add(30 * time.Second)
	if p := look(); len(p) != 0 {
		t.Errorf("offered twice: %v", p)
	}
	g2 := newGUIServer() // remembered after a restart
	g2.prefsPath = g.prefsPath
	g2.now = g.now
	g2.watchScan(dir, "_KTL", "")
	now = now.Add(30 * time.Second)
	if p, _ := g2.watchScan(dir, "_KTL", ""); len(p) != 0 {
		t.Errorf("offered again after a restart: %v", p)
	}
}
