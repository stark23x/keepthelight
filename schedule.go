package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// Running unattended: the queue can start at a set time (01:00, say), and
// optionally pause at another (07:00) and carry on at the start time. Keep the
// Light must be open and the PC awake: it does not wake a sleeping PC.

type schedule struct {
	StartOn bool   `json:"startOn"`
	StartAt string `json:"startAt"` // "01:00"
	PauseOn bool   `json:"pauseOn"`
	PauseAt string `json:"pauseAt"`

	pausedBy bool      // the scheduler paused the running file (and resumes it)
	lastTick time.Time // times are acted on when crossed between two ticks
}

func validHM(s string) bool {
	t, err := time.Parse("15:04", s)
	return err == nil && t.Format("15:04") == s
}

func (g *guiServer) loadSchedule() {
	var s schedule
	if json.Unmarshal(g.loadPrefs()["schedule"], &s) == nil {
		g.mu.Lock()
		g.sched.StartOn, g.sched.StartAt, g.sched.PauseOn, g.sched.PauseAt = s.StartOn, s.StartAt, s.PauseOn, s.PauseAt
		g.mu.Unlock()
	}
}

func (g *guiServer) setSchedule(s schedule) error {
	if (s.StartOn && !validHM(s.StartAt)) || (s.PauseOn && !validHM(s.PauseAt)) {
		return fmt.Errorf("times are written as 01:00")
	}
	if s.PauseOn && !s.StartOn {
		return fmt.Errorf("pausing needs a start time to carry on at")
	}
	g.mu.Lock()
	g.sched.StartOn, g.sched.StartAt, g.sched.PauseOn, g.sched.PauseAt = s.StartOn, s.StartAt, s.PauseOn, s.PauseAt
	g.mu.Unlock()
	raw, _ := json.Marshal(s)
	return g.savePref("schedule", raw)
}

// crossed: did the time of day hm come between the last tick and now?
func crossed(hm string, last, now time.Time) bool {
	if !validHM(hm) || last.IsZero() || !now.After(last) || now.Sub(last) > 6*time.Hour {
		return false
	}
	t, _ := time.Parse("15:04", hm)
	for _, day := range []time.Time{last, now} {
		at := time.Date(day.Year(), day.Month(), day.Day(), t.Hour(), t.Minute(), 0, 0, now.Location())
		if at.After(last) && !at.After(now) {
			return true
		}
	}
	return false
}

// scheduleTick acts on the set times (called every 20 seconds).
func (g *guiServer) scheduleTick() {
	now := g.clock()
	g.mu.Lock()
	s := &g.sched
	last := s.lastTick
	s.lastTick = now
	start := s.StartOn && crossed(s.StartAt, last, now)
	pause := s.PauseOn && crossed(s.PauseAt, last, now)
	running, waiting := g.q.running, 0
	for _, it := range g.q.items {
		if it.Status == stWaiting || it.Status == stChecking {
			waiting++
		}
	}
	resume := start && s.pausedBy
	paused := g.job != nil && g.job.running && g.job.paused
	g.mu.Unlock()
	switch {
	case pause && running && !paused:
		if g.pauseJob(true) == nil {
			g.mu.Lock()
			g.sched.pausedBy = true
			g.mu.Unlock()
		}
	case resume:
		_ = g.pauseJob(false)
		g.mu.Lock()
		g.sched.pausedBy = false
		g.mu.Unlock()
	case start && !running && waiting > 0:
		_ = g.startQueue()
	}
}

func (g *guiServer) scheduler(ctx context.Context) {
	g.loadSchedule()
	t := time.NewTicker(20 * time.Second)
	defer t.Stop()
	g.scheduleTick()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			g.scheduleTick()
		}
	}
}

// scheduleText says what is planned, for the Queue tab.
func (s *schedule) text() string {
	var parts []string
	if s.StartOn {
		parts = append(parts, "starts at "+s.StartAt)
	}
	if s.PauseOn {
		parts = append(parts, "pauses at "+s.PauseAt+" and carries on at "+s.StartAt)
	}
	if s.pausedBy {
		parts = append(parts, "paused by the schedule")
	}
	return strings.Join(parts, " · ")
}

// ---------- the watch folder ----------

// Videos placed in the watch folder are offered to the window once they have
// finished copying (the same size and time on two looks, at least 25 seconds
// apart). Each file is offered once (remembered, also after a restart); files
// already in the queue, made by Keep the Light, or carrying its "already
// converted" tag are left alone. The window adds them with the chosen profile.

type watchSeen struct {
	size  int64
	mtime time.Time
	at    time.Time
}

type watchState struct {
	mu      sync.Mutex
	dir     string
	looking map[string]watchSeen
	offered map[string]bool // "path|size": offered before
	loaded  bool
}

func (g *guiServer) watchScan(dir, tag, ffprobe string) ([]string, error) {
	w := &g.watch
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.loaded {
		w.offered = map[string]bool{}
		var list []string
		_ = json.Unmarshal(g.loadPrefs()["watchOffered"], &list)
		for _, k := range list {
			w.offered[k] = true
		}
		w.loaded = true
	}
	if dir != w.dir {
		w.dir, w.looking = dir, map[string]watchSeen{}
	}
	paths, err := videosIn(dir, tag)
	if err != nil {
		return nil, err
	}
	g.mu.Lock()
	inQueue := map[string]bool{}
	for _, it := range g.q.items {
		inQueue[strings.ToLower(it.Input)] = true
		inQueue[strings.ToLower(it.Output)] = true
	}
	g.mu.Unlock()
	now := g.clock()
	var ready []string
	for _, p := range paths {
		st, err := os.Stat(p)
		if err != nil || inQueue[strings.ToLower(p)] {
			continue
		}
		key := fmt.Sprintf("%s|%d", strings.ToLower(p), st.Size())
		if w.offered[key] {
			continue
		}
		prev, ok := w.looking[p]
		if !ok || prev.size != st.Size() || !prev.mtime.Equal(st.ModTime()) {
			w.looking[p] = watchSeen{size: st.Size(), mtime: st.ModTime(), at: now} // still arriving, or new
			continue
		}
		if now.Sub(prev.at) < 25*time.Second {
			continue
		}
		delete(w.looking, p)
		w.offered[key] = true
		if ffprobe != "" { // made by Keep the Light already: not again
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			pr, err := probeFile(ctx, ffprobe, p)
			cancel()
			if err == nil && pr.Converted != "" {
				continue
			}
		}
		ready = append(ready, p)
	}
	if len(ready) > 0 {
		var list []string
		for k := range w.offered {
			list = append(list, k)
		}
		if len(list) > 3000 { // an old, long list: keep it from growing without end
			list = list[len(list)-3000:]
		}
		raw, _ := json.Marshal(list)
		_ = g.savePref("watchOffered", raw)
	}
	return ready, nil
}
