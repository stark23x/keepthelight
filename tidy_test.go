package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func makeMarkedClip(t *testing.T, ffmpeg, path, secs, mark string) {
	t.Helper()
	args := []string{"-v", "error", "-y", "-f", "lavfi", "-i", "testsrc2=s=160x120:r=25:d=" + secs,
		"-f", "lavfi", "-i", "sine=d=" + secs, "-c:a", "aac", "-c:v", "mpeg4"}
	if mark != "" {
		args = append(args, "-metadata", ktlTagKey+"="+mark)
	}
	if b, err := exec.Command(ffmpeg, append(args, path)...).CombinedOutput(); err != nil {
		t.Fatal(err, string(b))
	}
}

func tidyFindWait(t *testing.T, g *guiServer, dir string, recursive bool) []tidyPair {
	t.Helper()
	if r := g.tidyFind(dir, recursive, "_KTL"); r["ok"] != true {
		t.Fatal(r)
	}
	for i := 0; i < 400; i++ {
		if st := g.tidyStatus(); st["running"] == false {
			return st["pairs"].([]tidyPair)
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("tidy did not finish")
	return nil
}

// Pairs are found by Keep the Light's mark and the length, whatever the names
// (the owner's real case: a copy with a Plex-style name).
func TestTidyPairsByMark(t *testing.T) {
	ffmpeg, e1 := exec.LookPath("ffmpeg")
	_, e2 := exec.LookPath("ffprobe")
	if e1 != nil || e2 != nil {
		t.Skip("ffmpeg/ffprobe not found")
	}
	t.Setenv("LOCALAPPDATA", t.TempDir())
	root := t.TempDir()
	film := filepath.Join(root, "Movies", "The Mandela Effect (2019)")
	season := filepath.Join(root, "TV", "Show", "Season 01")
	other := filepath.Join(root, "Movies", "Two Cuts (2001)")
	for _, d := range []string{film, season, other} {
		_ = os.MkdirAll(d, 0o755)
	}
	orig := filepath.Join(film, "The.Mandela.Effect.2019.1080p.AMZN.WEB-DL.DDP5.1.H.264-NTG.mkv")
	cp := filepath.Join(film, "The Mandela Effect (2019).1080p.HEVC.{tmdb-491037}.mkv")
	makeMarkedClip(t, ffmpeg, orig, "6", "")
	makeMarkedClip(t, ffmpeg, cp, "6", "Keep the Light 1.42.0 · x265 CRF 20 · 160×120")
	// a season: every episode the same length; the episode number decides
	for _, e := range []string{"01", "02"} {
		makeMarkedClip(t, ffmpeg, filepath.Join(season, "show.s01e"+e+".720p.web.mkv"), "4", "")
		makeMarkedClip(t, ffmpeg, filepath.Join(season, "Show - S01E"+e+" - Title.mkv"), "4", "Keep the Light 1.42.0")
	}
	// two unmarked files of the same length as the copy and nothing to tell them apart: left alone
	makeMarkedClip(t, ffmpeg, filepath.Join(other, "Two Cuts A.mkv"), "5", "")
	makeMarkedClip(t, ffmpeg, filepath.Join(other, "Two Cuts B.mkv"), "5", "")
	makeMarkedClip(t, ffmpeg, filepath.Join(other, "Two Cuts (2001).mkv"), "5", "Keep the Light 1.42.0")

	g := &guiServer{}
	if pairs := tidyFindWait(t, g, filepath.Join(root, "Movies"), false); len(pairs) != 0 {
		t.Errorf("without subfolders: %+v", pairs)
	}
	pairs := tidyFindWait(t, g, root, true)
	got := map[string]tidyPair{}
	for _, p := range pairs {
		got[p.CopyName] = p
	}
	if p := got[filepath.Base(cp)]; !p.OK || p.Original != orig {
		t.Errorf("the film: %+v", p)
	}
	for _, e := range []string{"01", "02"} {
		if p := got["Show - S01E"+e+" - Title.mkv"]; !p.OK || p.Name != "show.s01e"+e+".720p.web.mkv" {
			t.Errorf("episode %s: %+v", e, p)
		}
	}
	if p := got["Two Cuts (2001).mkv"]; p.OK || p.Original != "" || p.Why == "" {
		t.Errorf("two possible originals should be left alone: %+v", p)
	}
	if len(pairs) != 4 {
		t.Errorf("%d pairs: %+v", len(pairs), pairs)
	}
	// and it moves the right file (no Recycle Bin here: renamed "(original)")
	if r := g.tidyApply([]string{orig, ""}); r["renamed"] != 1 {
		t.Fatalf("apply: %v", r)
	}
	if fileExists(orig) || !fileExists(cp) || !fileExists(filepath.Join(film, "The.Mandela.Effect.2019.1080p.AMZN.WEB-DL.DDP5.1.H.264-NTG (original).mkv")) {
		t.Error("the original was not set aside correctly")
	}
	// set-aside originals are not offered again
	if pairs := tidyFindWait(t, g, film, false); len(pairs) != 0 {
		t.Errorf("after tidying: %+v", pairs)
	}
}

func TestTidyByMarkPure(t *testing.T) {
	v := []tidyVideo{{"a.mkv", false, 100}, {"b.mkv", true, 101}, {"c (original).mkv", false, 100}, {"d.mkv", false, 300}}
	if f := tidyByMark("x", v); len(f) != 1 || filepath.Base(f[0].Original) != "a.mkv" {
		t.Errorf("%+v", f)
	}
	v = []tidyVideo{{"a.mkv", false, 100}, {"b.mkv", true, 140}}
	if f := tidyByMark("x", v); len(f) != 0 {
		t.Errorf("different lengths paired: %+v", f)
	}
}
