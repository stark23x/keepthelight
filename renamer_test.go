package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func renWait(t *testing.T, g *guiServer) []renRow {
	t.Helper()
	for i := 0; i < 200; i++ {
		r := g.renList()
		rows := r["rows"].([]renRow)
		busy := false
		for _, x := range rows {
			if x.State == "checking" || x.State == "looking" {
				busy = true
			}
		}
		if !busy {
			return rows
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("the rows were never checked")
	return nil
}

func renSetPlex(g *guiServer, id int, name string) {
	g.ren.mu.Lock()
	defer g.ren.mu.Unlock()
	for _, r := range g.ren.rows {
		if r.ID == id {
			r.plexName = name
		}
	}
}

func TestRenamerRenameAndUndo(t *testing.T) {
	ffmpeg, e1 := exec.LookPath("ffmpeg")
	_, e2 := exec.LookPath("ffprobe")
	if e1 != nil || e2 != nil {
		t.Skip("needs ffmpeg")
	}
	t.Setenv("LOCALAPPDATA", t.TempDir())
	dir := t.TempDir()
	film := filepath.Join(dir, "Heat.1995.REMUX.mkv")
	show := filepath.Join(dir, "the.office.s01e02.mkv")
	for _, p := range []string{film, show} {
		if b, err := exec.Command(ffmpeg, "-v", "error", "-y", "-f", "lavfi", "-i", "testsrc=s=1920x800:r=10:d=0.2", "-c:v", "libx264", p).CombinedOutput(); err != nil {
			t.Fatalf("%v %s", err, b)
		}
	}
	srt := filepath.Join(dir, "Heat.1995.REMUX.en.srt")
	other := filepath.Join(dir, "Heat.1995.REMUX-notes.srt") // not the film's: its name goes on differently
	for _, p := range []string{srt, other} {
		if err := os.WriteFile(p, []byte("1\n00:00:01,000 --> 00:00:02,000\nHi\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	g := newGUIServer()
	g.prefsPath = filepath.Join(t.TempDir(), "settings.json")
	if r := g.renAddPaths([]string{film, show, filepath.Join(dir, "notes.txt")}); r["added"] != 2 {
		t.Fatalf("added: %v", r)
	}
	rows := renWait(t, g)
	if rows[0].Details != "1080p.H264" || rows[0].State != "new" {
		t.Fatalf("film row: %+v", rows[0])
	}
	renSetPlex(g, rows[0].ID, plexFilmName("Heat", "1995", 949))
	renSetPlex(g, rows[1].ID, plexEpisodeName("The Office", "2005", 1, 2, "Diversity Day"))
	rows = renWait(t, g)
	if rows[0].Proposed != "Heat (1995).1080p.H264.{tmdb-949}" || rows[0].State != "ready" {
		t.Fatalf("film: %+v", rows[0])
	}
	if rows[1].Proposed != "The Office (2005).S01E02.Diversity Day.1080p.H264" {
		t.Fatalf("episode: %+v", rows[1])
	}
	// a name typed without SxxEyy is allowed, with a warning
	g.renSetName(rows[1].ID, "The Office pilot")
	if rows = renWait(t, g); rows[1].State != "warn" || !rows[1].Own {
		t.Fatalf("typed: %+v", rows[1])
	}
	g.renSetName(rows[1].ID, "")
	r := g.renApply([]int{rows[0].ID, rows[1].ID})
	if r["renamed"] != 2 {
		t.Fatalf("apply: %v", r)
	}
	newFilm := filepath.Join(dir, "Heat (1995).1080p.H264.{tmdb-949}.mkv")
	for _, p := range []string{newFilm, filepath.Join(dir, "Heat (1995).1080p.H264.{tmdb-949}.en.srt"), other,
		filepath.Join(dir, "The Office (2005).S01E02.Diversity Day.1080p.H264.mkv")} {
		if !fileExists(p) {
			t.Errorf("missing after renaming: %s", filepath.Base(p))
		}
	}
	if rows = renWait(t, g); rows[0].State != "same" || rows[0].Path != newFilm {
		t.Errorf("after renaming: %+v", rows[0])
	}
	if b, _ := os.ReadFile(renLogPath()); len(b) == 0 {
		t.Error("nothing in the rename log")
	}
	u := g.renUndo()
	if u["restored"] != 2 || !fileExists(film) || !fileExists(srt) || !fileExists(show) || fileExists(newFilm) {
		t.Fatalf("undo: %v", u)
	}
	if g.renUndo()["ok"] == true {
		t.Error("a second undo did something")
	}
	// a name that is already taken
	if err := os.WriteFile(newFilm, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if rows = renWait(t, g); rows[0].State != "clash" {
		t.Errorf("clash: %+v", rows[0])
	}
	if r := g.renApply([]int{rows[0].ID}); r["renamed"] != 0 || len(r["skipped"].([]string)) != 1 {
		t.Errorf("clash renamed: %v", r)
	}
}

func TestRenamerFollowsQueue(t *testing.T) {
	ffmpeg, e1 := exec.LookPath("ffmpeg")
	_, e2 := exec.LookPath("ffprobe")
	if e1 != nil || e2 != nil {
		t.Skip("needs ffmpeg")
	}
	t.Setenv("LOCALAPPDATA", t.TempDir())
	dir := t.TempDir()
	in := filepath.Join(dir, "heat 1995.mkv")
	if b, err := exec.Command(ffmpeg, "-v", "error", "-y", "-f", "lavfi", "-i", "testsrc=s=640x480:r=10:d=0.2", "-c:v", "mpeg4", in).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, b)
	}
	g := newGUIServer()
	g.prefsPath = filepath.Join(t.TempDir(), "settings.json")
	ids, errs := g.queueAdd([]json.RawMessage{itemWith(in, filepath.Join(dir, "heat 1995_KTL.mkv"), map[string]any{"plexName": plexFilmName("Heat", "1995", 949)})})
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	rows := renWait(t, g)
	if len(rows) != 1 || rows[0].QueueID != ids[0] || rows[0].Proposed != "Heat (1995).480p.MPEG4.{tmdb-949}" {
		t.Fatalf("queue row: %+v", rows)
	}
	if r := g.renApply([]int{rows[0].ID}); r["renamed"] != 1 {
		t.Fatalf("apply: %v", r)
	}
	g.mu.Lock()
	it := g.findItemLocked(ids[0])
	gotIn, gotOut := it.Input, it.Output
	g.mu.Unlock()
	if filepath.Base(gotIn) != "Heat (1995).480p.MPEG4.{tmdb-949}.mkv" || filepath.Base(gotOut) != "Heat (1995).480p.MPEG4.{tmdb-949}_KTL.mkv" {
		t.Errorf("the queue did not follow: %s, %s", gotIn, gotOut)
	}
	if rows = renWait(t, g); len(rows) != 1 || rows[0].State != "same" {
		t.Errorf("after: %+v", rows)
	}
}
