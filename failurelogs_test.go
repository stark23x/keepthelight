package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFailureLogs(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	when := time.Date(2026, 11, 5, 14, 20, 0, 0, time.Local)
	p := saveFailureLog("Heat: (1995).mkv", `W:\Heat.mkv`, `W:\Heat_KTL.mkv`, "-crf 18", []string{"[1/5] Extracting", "ERROR: dovi_tool failed"}, when)
	if p == "" || !strings.HasPrefix(filepath.Base(p), "2026-11-05 14-20-00 Heat_ (1995)") {
		t.Fatalf("path: %q", p)
	}
	b, _ := os.ReadFile(p)
	for _, want := range []string{"Input:  W:\\Heat.mkv", "Options: -crf 18", "ERROR: dovi_tool failed"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("log lacks %q:\n%s", want, b)
		}
	}
	for i := 0; i < keepLogs+5; i++ {
		saveFailureLog("f.mkv", "", "", "", nil, when.Add(time.Duration(i+1)*time.Second))
	}
	m, _ := filepath.Glob(filepath.Join(logsDir(), "*.txt"))
	if len(m) != keepLogs {
		t.Errorf("%d logs kept, want %d", len(m), keepLogs)
	}
}

// A failed file in the queue gets its log saved; a good one does not.
func TestQueueSavesFailureLog(t *testing.T) {
	fakeTools(t)
	t.Setenv("LOCALAPPDATA", t.TempDir())
	g := newGUIServer()
	g.prefsPath = filepath.Join(t.TempDir(), "settings.json")
	runToEnd(t, g, "good", "bad")
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, it := range g.q.items {
		switch it.Status {
		case stFailed:
			if it.LogFile == "" || !fileExists(it.LogFile) {
				t.Errorf("%s: no saved log", it.Name)
			}
		default:
			if it.LogFile != "" {
				t.Errorf("%s (%s): a log was saved", it.Name, it.Status)
			}
		}
	}
}
