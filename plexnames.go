package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

// Plex-style names, optional: after a TMDB match a file can be named the way
// Plex expects ("Heat (1995) {tmdb-949}.mkv", "The Office (2005) - S01E02 -
// Diversity Day.mkv"). Only the new file is named so; nothing is renamed until
// the names have been shown and confirmed.

// plexClean leaves out what Windows does not allow in a file name, and tidies spaces.
func plexClean(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r == ':':
			return '-' // "Mission: Impossible" → "Mission- Impossible" reads better than nothing
		case strings.ContainsRune(`\/*?"<>|`, r) || r < 32:
			return -1
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	return strings.TrimRight(s, ". ")
}

// The owner's format, parts joined by dots, with the new file's details where
// plexDetailsMark stands (filled in once the file has been checked):
//
//	The Office (2005).S01E02.Diversity Day.1080p.HEVC.mkv
//	Heat (1995).2160p.HEVC.DV.{tmdb-949}.mkv
//
// ("<" cannot be in a Windows file name, so the mark can never clash with a title.)
const plexDetailsMark = "<details>"

func plexFilmName(title, year string, id int) string {
	t := plexClean(title)
	if t == "" {
		return ""
	}
	if year != "" {
		t += " (" + year + ")"
	}
	t += "." + plexDetailsMark
	if id > 0 {
		t += fmt.Sprintf(".{tmdb-%d}", id)
	}
	return t
}

func plexEpisodeName(show, year string, season, episode int, title string) string {
	s := plexClean(show)
	if s == "" || season <= 0 || episode <= 0 {
		return ""
	}
	if year != "" {
		s += " (" + year + ")"
	}
	s += fmt.Sprintf(".S%02dE%02d", season, episode)
	if t := plexClean(title); t != "" {
		s += "." + t
	}
	return s + "." + plexDetailsMark
}

// plexResolution names a picture's size the usual way, by whichever side is
// larger for its class (a 1920×800 scope film is 1080p).
func plexResolution(w, h int) string {
	switch {
	case w <= 0 || h <= 0:
		return ""
	case w >= 3200 || h >= 1800:
		return "2160p"
	case w >= 2200 || h >= 1300:
		return "1440p"
	case w >= 1700 || h >= 900:
		return "1080p"
	case w >= 1100 || h >= 650:
		return "720p"
	case h > 500 || w > 900:
		return "576p"
	}
	return "480p"
}

// plexHDR is the kind of HDR the new file has ("" for SDR).
func plexHDR(p *Probe, plan *Plan) string {
	switch {
	case plan.UseDV:
		return "DV"
	case plan.UseHDR10Plus:
		return "HDR10+"
	case plan.Kind == "hlg":
		return "HLG"
	case plan.Kind == "hdr10":
		return "HDR10"
	}
	return ""
}

// plexDetails: "1080p.HEVC", "2160p.HEVC.DV" for the new file.
func plexDetails(w, h int, hdr string) string { return plexDetailsFor(w, h, "HEVC", hdr) }

// plexDetailsFor: the same for any video format ("1080p.H264" for a file renamed as it is).
func plexDetailsFor(w, h int, codec, hdr string) string {
	parts := []string{}
	if r := plexResolution(w, h); r != "" {
		parts = append(parts, r)
	}
	parts = append(parts, codec)
	if hdr != "" {
		parts = append(parts, hdr)
	}
	return strings.Join(parts, ".")
}

// plexFinal puts the details into a proposed name (or leaves the mark out
// when they are not known yet).
func plexFinal(name, details string) string {
	if details == "" {
		name = strings.Replace(name, "."+plexDetailsMark, "", 1)
	}
	return strings.Replace(name, plexDetailsMark, details, 1)
}

// plexRename is one proposed change, for the Review names window.
type plexRename struct {
	ID   int    `json:"id"`
	From string `json:"from"` // the output's current name
	To   string `json:"to"`   // the proposed name
	Why  string `json:"why,omitempty"`
}

// plexProposals lists the waiting files with a Plex-style name to offer (from a
// TMDB match), and which of them cannot take it (the name exists, or two files
// would get the same one).
func (g *guiServer) plexProposals() []plexRename {
	g.mu.Lock()
	defer g.mu.Unlock()
	var list []plexRename
	taken := map[string]int{}
	for _, it := range g.q.items {
		taken[strings.ToLower(it.Output)]++
	}
	for _, it := range g.q.items {
		if it.Editing || !(it.pending() || it.Status == stProblem) {
			continue
		}
		var m struct {
			PlexName string `json:"plexName"`
		}
		_ = json.Unmarshal(it.Raw, &m)
		if m.PlexName == "" || it.Output == "" || (strings.Contains(m.PlexName, plexDetailsMark) && it.PlexDetails == "") {
			continue // not checked yet: its size and HDR are not known
		}
		to := filepath.Join(filepath.Dir(it.Output), plexFinal(m.PlexName, it.PlexDetails)+filepath.Ext(it.Output))
		if strings.EqualFold(to, it.Output) {
			continue // already named so
		}
		p := plexRename{ID: it.ID, From: filepath.Base(it.Output), To: filepath.Base(to)}
		switch {
		case fileExists(to):
			p.Why = "a file of that name already exists"
		case taken[strings.ToLower(to)] > 0:
			p.Why = "another file in the queue has that name"
		case strings.EqualFold(to, it.Input):
			p.Why = "that is the original's own name"
		}
		taken[strings.ToLower(to)]++
		list = append(list, p)
	}
	return list
}

// plexApply gives the chosen waiting files their Plex-style names (each is
// checked again, as when it was added). Files that cannot take theirs keep the
// name they have.
func (g *guiServer) plexApply(ids []int) map[string]any {
	want := map[int]bool{}
	for _, id := range ids {
		want[id] = true
	}
	props := g.plexProposals()
	g.mu.Lock()
	n, skipped := 0, []string{}
	for _, p := range props {
		if !want[p.ID] {
			continue
		}
		if p.Why != "" {
			skipped = append(skipped, p.From+": "+p.Why)
			continue
		}
		it := g.findItemLocked(p.ID)
		if it == nil {
			continue
		}
		out := filepath.Join(filepath.Dir(it.Output), p.To)
		setRawField(it, "output", out)
		var s settings
		if json.Unmarshal(it.Raw, &s) == nil {
			if cfg, err := validateSettings(s); err == nil {
				it.s, it.cfg, it.Output = s, cfg, cfg.Output
				it.Status, it.Note, it.checked = stChecking, "", false
				n++
			}
		}
	}
	g.mu.Unlock()
	if n > 0 {
		g.kickChecker()
		g.saveQueue()
	}
	return map[string]any{"ok": true, "renamed": n, "skipped": skipped}
}
