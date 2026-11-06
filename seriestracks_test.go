package main

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestSeriesTrackNames(t *testing.T) {
	ffmpeg, e1 := exec.LookPath("ffmpeg")
	_, e2 := exec.LookPath("ffprobe")
	if e1 != nil || e2 != nil {
		t.Skip("needs ffmpeg")
	}
	dir := t.TempDir()
	mk := func(name string, audios int) string {
		p := filepath.Join(dir, name)
		args := []string{"-v", "error", "-y", "-f", "lavfi", "-i", "testsrc=s=64x64:r=10:d=1"}
		for i := 0; i < audios; i++ {
			args = append(args, "-f", "lavfi", "-i", "sine=d=1")
		}
		args = append(args, "-map", "0")
		for i := 0; i < audios; i++ {
			args = append(args, "-map", itoa(i+1))
		}
		args = append(args, "-c:v", "mpeg4", "-c:a", "ac3", "-metadata:s:a", "language=jpn", p)
		if b, err := exec.Command(ffmpeg, args...).CombinedOutput(); err != nil {
			t.Fatalf("%v %s", err, b)
		}
		return p
	}
	ep1, ep2, ep3 := mk("The Office S01E01.mkv", 2), mk("The Office S01E02.mkv", 2), mk("The Office S01E03.mkv", 1)
	other := mk("Other Show S01E01.mkv", 2)
	g := newGUIServer()
	g.prefsPath = filepath.Join(t.TempDir(), "settings.json")
	ids, errs := g.queueAdd([]json.RawMessage{
		itemSettings(ep1, filepath.Join(dir, "o1.mkv")),
		itemWith(ep2, filepath.Join(dir, "o2.mkv"), map[string]any{"meta": map[string]any{"tags": map[string]string{"title": "Diversity Day"}}}),
		itemSettings(ep3, filepath.Join(dir, "o3.mkv")), itemSettings(other, filepath.Join(dir, "o4.mkv"))})
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	// an Edit of episode 1 that names its tracks
	edited := itemWith(ep1, filepath.Join(dir, "o1.mkv"), map[string]any{"meta": map[string]any{
		"tags":   map[string]string{"title": "Pilot"},
		"tracks": []map[string]any{{"kind": "a", "n": 1, "title": "Japanese", "lang": "jpn"}, {"kind": "a", "n": 2, "title": "Japanese Commentary", "commentary": true}}}})
	r := g.queueEditSave(ids[0], edited)
	offer, _ := r["seriesOffer"].(map[string]any)
	if r["ok"] != true || offer == nil || offer["files"] != 2 || offer["show"] != "The Office" {
		t.Fatalf("offer: %v", r)
	}
	// saving again with the same names offers nothing
	if r := g.queueEditSave(ids[0], edited); r["seriesOffer"] != nil {
		t.Errorf("offered again: %v", r)
	}
	c := g.queueSeriesTracks(ids[0])
	if c["applied"] != 1 || len(c["different"].([]string)) != 1 || c["different"].([]string)[0] != "The Office S01E03.mkv" {
		t.Fatalf("copy: %v", c)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	e2m := g.findItemLocked(ids[1]).s.Meta
	if e2m == nil || len(e2m.Tracks) != 2 || *e2m.Tracks[1].Title != "Japanese Commentary" || !*e2m.Tracks[1].Commentary {
		t.Fatalf("episode 2 tracks: %+v", e2m)
	}
	if e2m.Tags["title"] != "Diversity Day" {
		t.Errorf("episode 2 lost its own title: %v", e2m.Tags)
	}
	if m := g.findItemLocked(ids[3]).s.Meta; m != nil && len(m.Tracks) > 0 {
		t.Errorf("another show was changed: %+v", m)
	}
	if m := g.findItemLocked(ids[2]).s.Meta; m != nil && len(m.Tracks) > 0 {
		t.Errorf("a differently laid out episode was changed: %+v", m)
	}
}

func TestSeriesKey(t *testing.T) {
	if seriesKey(`D:\TV\The Office (2005) S01E02.mkv`) != seriesKey(`D:\TV\The.Office.S01E05.1080p.mkv`) {
		t.Error("same show, different spellings")
	}
	if seriesKey(`D:\Films\Heat (1995).mkv`) != "" {
		t.Error("a film is no series")
	}
}
