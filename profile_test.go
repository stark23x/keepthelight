package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func itemWith(in, out string, extra map[string]any) json.RawMessage {
	m := map[string]any{"input": in, "output": out, "mode": "encode", "codec": "x265",
		"quality": "crf", "crf": 18, "preset": "medium", "audio": "copy", "dv": "auto"}
	for k, v := range extra {
		m[k] = v
	}
	b, _ := json.Marshal(m)
	return b
}

func rawOf(g *guiServer, i int) map[string]any {
	g.mu.Lock()
	defer g.mu.Unlock()
	var m map[string]any
	_ = json.Unmarshal(g.q.items[i].Raw, &m)
	return m
}

func TestApplyKeepsOwnChangesAndNamesAfterProfile(t *testing.T) {
	fakeTools(t)
	dir := t.TempDir()
	g := newGUIServer()
	g.prefsPath = filepath.Join(t.TempDir(), "settings.json")
	a, b := filepath.Join(dir, "a.mkv"), filepath.Join(dir, "b.mkv")
	for _, p := range []string{a, b} {
		_ = os.WriteFile(p, []byte("x"), 0o644)
	}
	own := map[string]any{"meta": map[string]any{"tags": map[string]any{"title": "Mine"}}}
	g.queueAdd([]json.RawMessage{itemWith(a, filepath.Join(dir, "a_KTL.mkv"), own), itemWith(b, filepath.Join(dir, "b_KTL.mkv"), nil)})

	tmpl, _ := json.Marshal(map[string]any{"output": "x.mkv", "mode": "encode", "codec": "nvenc", "quality": "crf", "crf": 22,
		"preset": "fast", "audio": "copy", "dv": "auto", "profile": "Plex 4K", "nameTag": "_Plex 4K", "keepOwn": true, "meta": nil})
	if n, errs := g.queueApply(tmpl); n != 2 || len(errs) != 0 {
		t.Fatalf("%d %v", n, errs)
	}
	m := rawOf(g, 0)
	if m["codec"] != "nvenc" || m["profile"] != "Plex 4K" || m["output"] != filepath.Join(dir, "a_Plex 4K.mkv") {
		t.Errorf("general settings: %v", m)
	}
	if m["meta"] == nil || m["keepOwn"] != nil || m["nameTag"] != nil {
		t.Errorf("own metadata must be kept, helper keys dropped: %v", m)
	}
	// replacing the own changes too
	tmpl, _ = json.Marshal(map[string]any{"output": "x.mkv", "mode": "encode", "codec": "x265", "quality": "crf", "crf": 18,
		"preset": "medium", "audio": "copy", "dv": "auto", "keepOwn": false, "meta": nil})
	g.queueApply(tmpl)
	if m := rawOf(g, 0); m["meta"] != nil || m["output"] != filepath.Join(dir, "a_Plex 4K.mkv") { // no new ending given: the name stays
		t.Errorf("replaced: %v", m)
	}
}

func TestEditQueuedFile(t *testing.T) {
	fakeTools(t)
	dir := t.TempDir()
	g := newGUIServer()
	g.prefsPath = filepath.Join(t.TempDir(), "settings.json")
	var raws []json.RawMessage
	for _, n := range []string{"first", "second"} {
		p := filepath.Join(dir, n+".mkv")
		_ = os.WriteFile(p, []byte("x"), 0o644)
		raws = append(raws, itemWith(p, filepath.Join(dir, n+"_KTL.mkv"), nil))
	}
	ids, _ := g.queueAdd(raws)
	waitFor(t, "check", func() bool { return !strings.Contains(strings.Join(statuses(g), " "), stChecking) })

	if r := g.queueEditStart(ids[0]); r["ok"] != true || r["settings"] == nil {
		t.Fatalf("start: %v", r)
	}
	// the queue runs the other file and waits for the one being changed
	if err := g.startQueue(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the second file", func() bool { return strings.Contains(strings.Join(statuses(g), " "), "second.mkv="+stDone) })
	time.Sleep(300 * time.Millisecond)
	if s := statuses(g); s[0] != "first.mkv="+stWaiting {
		t.Fatalf("the file being changed must wait: %v", s)
	}
	edited := itemWith(filepath.Join(dir, "first.mkv"), filepath.Join(dir, "first_new.mkv"),
		map[string]any{"meta": map[string]any{"tags": map[string]any{"title": "Edited"}}})
	if r := g.queueEditSave(ids[0], edited); r["ok"] != true {
		t.Fatalf("save: %v", r)
	}
	waitFor(t, "the queue to finish", func() bool { g.mu.Lock(); defer g.mu.Unlock(); return !g.q.running })
	if s := statuses(g); s[0] != "first.mkv="+stDone || !fileExists(filepath.Join(dir, "first_new.mkv")) {
		t.Fatalf("after saving: %v", s)
	}
	if r := g.queueEditStart(ids[0]); r["ok"] != false { // finished files cannot be changed
		t.Errorf("finished file opened: %v", r)
	}
}
