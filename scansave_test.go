package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The scan's results are kept until the next scan: saved when it ends, read
// back when the window opens, with files that are gone or changed marked.
func TestScanResultsKept(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	dir := t.TempDir()
	same, changed, gone := filepath.Join(dir, "a.mkv"), filepath.Join(dir, "b.mkv"), filepath.Join(dir, "c.mkv")
	for _, f := range []string{same, changed, gone} {
		if err := os.WriteFile(f, []byte("12345"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	item := func(p string) *scanItem {
		st, _ := os.Stat(p)
		return &scanItem{Path: p, Name: filepath.Base(p), Size: st.Size(), Mtime: st.ModTime().UnixMilli(), Worth: true, Saving: 100, Gap: 12}
	}
	g := newGUIServer()
	g.scan.dir, g.scan.finished, g.scan.found = dir, time.Now(), 3
	g.scan.opts = scanOpts{Recursive: true, SkipExtras: true}
	g.scan.items = []*scanItem{item(same), item(changed), item(gone)}
	g.scanSave()

	if err := os.WriteFile(changed, []byte("1234567890"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}

	g2 := newGUIServer()
	g2.scanLoad()
	deadline := time.Now().Add(5 * time.Second)
	for {
		r := g2.scanStatus(0)
		if r["checking"] == false {
			if r["saved"] != true || r["dir"] != dir || r["gone"] != 1 || r["changed"] != 1 || r["at"] == nil {
				t.Fatalf("status: %v", r)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the files were never looked at again")
		}
		time.Sleep(20 * time.Millisecond)
	}
	byName := map[string]*scanItem{}
	for _, it := range g2.scan.items {
		byName[it.Name] = it
	}
	if byName["a.mkv"].Gone || byName["a.mkv"].Changed || !byName["b.mkv"].Changed || !byName["c.mkv"].Gone {
		t.Errorf("marks: a %+v b %+v c %+v", byName["a.mkv"], byName["b.mkv"], byName["c.mkv"])
	}
	if !g2.scan.opts.Recursive || byName["a.mkv"].Gap != 12 {
		t.Error("the scan's choices and the rows' details were not kept")
	}

	// cleared: nothing comes back
	g2.scanClear()
	g3 := newGUIServer()
	g3.scanLoad()
	if r := g3.scanStatus(0); r["saved"] == true || len(g3.scan.items) != 0 {
		t.Errorf("after Clear results: %v", r)
	}
}
