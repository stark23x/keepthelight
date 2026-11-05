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

func plexFilmName(title, year string, id int) string {
	t := plexClean(title)
	if t == "" {
		return ""
	}
	if year != "" {
		t += " (" + year + ")"
	}
	if id > 0 {
		t += fmt.Sprintf(" {tmdb-%d}", id)
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
	s += fmt.Sprintf(" - S%02dE%02d", season, episode)
	if t := plexClean(title); t != "" {
		s += " - " + t
	}
	return s
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
		if m.PlexName == "" || it.Output == "" {
			continue
		}
		to := filepath.Join(filepath.Dir(it.Output), m.PlexName+filepath.Ext(it.Output))
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
