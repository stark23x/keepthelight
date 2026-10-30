package main

import (
	"strings"
	"testing"
	"time"
)

func TestBatchProgressAndEstimates(t *testing.T) {
	g := newGUIServer()
	now := time.Date(2026, 10, 29, 20, 0, 0, 0, time.UTC)
	g.now = func() time.Time { return now }
	cfg := &Config{Codec: "x265", Preset: "medium", Output: "/nonexistent/out.mkv", Input: "/nonexistent/in.mkv"}
	mk := func(id int, st string, dur float64, inRun bool) *queueItem {
		return &queueItem{ID: id, Name: itoa(id), Status: st, Duration: dur, cfg: cfg, height: 2160, inRun: inRun}
	}
	g.q.items = []*queueItem{
		mk(1, stDone, 3600, false), // an earlier run: not counted
		mk(2, stDone, 3600, true),
		mk(3, stRunning, 1800, true),
		mk(4, stWaiting, 2400, false),
		mk(5, stWaiting, 7200, false),
	}
	g.q.items[1].Took = 5400
	g.q.items[2].Percent = 50
	g.q.running = true

	// no speed measured yet: estimates come later
	st := g.queueStatusLocked()
	items := st["items"].([]queueItem)
	if items[3].Est != "estimate after the first minutes" {
		t.Errorf("before any speed: %q", items[3].Est)
	}
	b := st["batch"].(map[string]any)
	// done: 3600 + 900 of 3600+1800+2400+7200 = 15000
	if b["files"] != 4 || b["index"] != 2 || int(b["percent"].(float64)) != 30 || !strings.Contains(b["eta"].(string), "estimating") {
		t.Errorf("batch: %v", b)
	}

	// a measured speed for this kind of file (0.5x real time): 2400 s of film take 80 min
	g.q.speeds = map[string]float64{"x265/medium/uhd": 0.5}
	st = g.queueStatusLocked()
	items = st["items"].([]queueItem)
	if items[3].Est != "about 80 min" && items[3].Est != "about 1 h 20 min" {
		t.Errorf("waiting: %q", items[3].Est)
	}
	if items[4].Est != "about 4 h" {
		t.Errorf("waiting, long: %q", items[4].Est)
	}
	if items[1].Took != 5400 || items[1].Est != "" {
		t.Errorf("finished: %+v", items[1])
	}

	// not running: the waiting files and their total
	g.q.running = false
	g.q.items[2].Status = stDone
	b = g.queueStatusLocked()["batch"].(map[string]any)
	if b["waiting"] != 2 || b["eta"] != "about 5 h 20 min in total" || b["percent"] != nil {
		t.Errorf("idle: %v", b)
	}
	// nothing waiting: no batch line
	for _, it := range g.q.items {
		it.Status = stDone
	}
	if b, ok := g.queueStatusLocked()["batch"].(map[string]any); ok && b != nil {
		t.Errorf("nothing waiting: %v", b)
	}
}
