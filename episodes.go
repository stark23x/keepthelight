package main

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// ---------- double episodes ----------
//
// Some shows have an hour-long episode that TMDB counts as two (Laverne &
// Shirley S05: "We're in the Army, Now" is episodes 9 and 10), while the file
// is one, named S05E09. Every later file is then one number short of TMDB's,
// so each is looked up as the episode before it. The reverse happens too: one
// TMDB episode in two files.
//
// A file named "S05E09-E10" is read as both episodes. For files named the old
// way, a double (or split) episode is marked on the Plex names tab, by hand or
// on Keep the Light's suggestion; the later files of that season are then
// looked up one number on, and the Plex names tab renames them to match.

// tmdbEp is what a file is on TMDB: episode First (to Last, for a double), or
// part Part of episode First (a split episode).
type tmdbEp struct {
	First, Last, Part int
}

func (e tmdbEp) String() string {
	switch {
	case e.Last > e.First:
		return fmt.Sprintf("episodes %d–%d", e.First, e.Last)
	case e.Part > 0:
		return fmt.Sprintf("part %d of episode %d", e.Part, e.First)
	}
	return fmt.Sprintf("episode %d", e.First)
}

// epShift is one marked episode in a season: the file numbered At holds two
// TMDB episodes ("double"), or the files At and At+1 are the two halves of one
// ("split"). Numbers are the files' own.
type epShift struct {
	At   int    `json:"at"`
	Kind string `json:"kind"` // double | split
}

func shiftKey(pickKey string, season int) string { return fmt.Sprintf("%s|s%d", pickKey, season) }

// mapEpisode: the TMDB episode(s) of the file numbered n (to n2, when the
// name already says it is a double), given the season's marked episodes.
func mapEpisode(n, n2 int, shifts []epShift) tmdbEp {
	s := append([]epShift(nil), shifts...)
	sort.Slice(s, func(a, b int) bool { return s[a].At < s[b].At })
	d := 0
	for _, x := range s {
		switch x.Kind {
		case "double":
			if n == x.At && n2 == 0 {
				return tmdbEp{First: n + d, Last: n + d + 1}
			}
			if n > x.At {
				d++
			}
		case "split":
			if n == x.At && n2 == 0 {
				return tmdbEp{First: n + d, Part: 1}
			}
			if n == x.At+1 && n2 == 0 {
				return tmdbEp{First: x.At + d, Part: 2}
			}
			if n > x.At+1 {
				d--
			}
		}
	}
	e := tmdbEp{First: n + d}
	if n2 > n {
		e.Last = n2 + d
	}
	return e
}

var rePartOf = regexp.MustCompile(`(?i)\s*(?:[,:–-]\s*)?(?:\(?\s*(?:part|pt\.?)\s*(?:\d+|one|two|i{1,3})\s*\)?|\(\s*\d+\s*\))\s*$`)

// baseTitle is an episode's title without its "Part 1" / "(2)".
func baseTitle(s string) string { return strings.TrimSpace(rePartOf.ReplaceAllString(s, "")) }

// joinTitles is the title of a double episode: "We're in the Army, Now
// (Parts 1 & 2)", or "First / Second" when the halves have their own names.
func joinTitles(names []string) string {
	if len(names) == 0 {
		return ""
	}
	same := true
	for _, n := range names[1:] {
		if !strings.EqualFold(baseTitle(n), baseTitle(names[0])) {
			same = false
		}
	}
	if same {
		parts := make([]string, len(names))
		for i := range names {
			parts[i] = fmt.Sprint(i + 1)
		}
		return fmt.Sprintf("%s (Parts %s)", baseTitle(names[0]), strings.Join(parts[:len(parts)-1], ", ")+" & "+parts[len(parts)-1])
	}
	return strings.Join(names, " / ")
}

type tmdbEpisode struct {
	Number   int     `json:"episode_number"`
	Name     string  `json:"name"`
	AirDate  string  `json:"air_date"`
	Runtime  float64 `json:"runtime"`
	Overview string  `json:"overview"`
}

// tmdbSeason lists a season's episodes (number, title, running time).
func tmdbSeason(ctx context.Context, key string, id, season int) ([]tmdbEpisode, error) {
	var s struct {
		Episodes []tmdbEpisode `json:"episodes"`
	}
	if err := tmdbGet(ctx, key, fmt.Sprintf("/tv/%d/season/%d", id, season), nil, &s); err != nil {
		return nil, fmt.Errorf("season %d: %v", season, err)
	}
	return s.Episodes, nil
}

// tmdbEpisodes fills in a double episode (or a part of a split one): the
// first episode's details, the joined title, and a Plex-style name such as
// "Laverne & Shirley (1976).S05E09-E10.We're in the Army, Now.<details>".
func tmdbEpisodes(ctx context.Context, key string, id, season int, ep tmdbEp, coverDir string) (*tmdbFill, error) {
	f, err := tmdbDetails(ctx, key, "tv", id, season, ep.First, coverDir)
	if err != nil {
		return nil, err
	}
	var show struct {
		Name         string
		FirstAirDate string `json:"first_air_date"`
	}
	if err := tmdbGet(ctx, key, fmt.Sprintf("/tv/%d", id), nil, &show); err != nil {
		return nil, err
	}
	var names, overviews []string
	for e := ep.First; e <= max(ep.First, ep.Last); e++ {
		var x tmdbEpisode
		if err := tmdbGet(ctx, key, fmt.Sprintf("/tv/%d/season/%d/episode/%d", id, season, e), nil, &x); err != nil {
			return nil, fmt.Errorf("season %d, episode %d: %v", season, e, err)
		}
		names = append(names, x.Name)
		if x.Overview != "" && (len(overviews) == 0 || overviews[len(overviews)-1] != x.Overview) {
			overviews = append(overviews, x.Overview)
		}
	}
	title := joinTitles(names)
	if ep.Part > 0 {
		title = fmt.Sprintf("%s (part %d)", names[0], ep.Part)
	}
	nums := fmt.Sprintf("S%02dE%02d", season, ep.First)
	if ep.Last > ep.First {
		nums += fmt.Sprintf("-E%02d", ep.Last)
	}
	f.Title = show.Name + " – " + nums
	if title != "" {
		f.Title += " – " + title
	}
	f.Description = strings.Join(overviews, " ")
	plexTitle := title
	if ep.Last > ep.First && len(names) > 0 && strings.Contains(title, " (Parts ") {
		plexTitle = baseTitle(names[0]) // the name stays short: S05E09-E10 already says it is both
	}
	f.PlexName = plexEpisodeRangeName(show.Name, yearOf(show.FirstAirDate), season, ep, plexTitle)
	return f, nil
}

// ---------- spotting a double episode ----------

// doubleSuggestion: a file that looks like two TMDB episodes.
type doubleSuggestion struct {
	Key    string `json:"key"` // shiftKey
	At     int    `json:"at"`  // the file's own episode number
	Season int    `json:"season"`
	Show   string `json:"show"`
	File   string `json:"file"`
	Title  string `json:"title"` // TMDB's title for the two
	Later  int    `json:"later"` // files after it in that season that move up one
	Why    string `json:"why"`
}

// spotDoubles looks at one season's files (their own episode numbers and
// lengths in minutes) against TMDB's episodes: a file about twice as long as
// the others whose TMDB episode and the next one share a title, or together
// match its length, is suggested as a double episode.
func spotDoubles(files map[int]float64, tmdb []tmdbEpisode, shifts []epShift) []epShift {
	if len(files) < 3 || len(tmdb) == 0 {
		return nil
	}
	var lens []float64
	for _, m := range files {
		if m > 0 {
			lens = append(lens, m)
		}
	}
	if len(lens) < 3 {
		return nil
	}
	sort.Float64s(lens)
	median := lens[len(lens)/2]
	byNum := map[int]tmdbEpisode{}
	for _, e := range tmdb {
		byNum[e.Number] = e
	}
	marked := map[int]bool{}
	for _, s := range shifts {
		marked[s.At] = true
	}
	var nums []int
	for n := range files {
		nums = append(nums, n)
	}
	sort.Ints(nums)
	var out []epShift
	for _, n := range nums {
		m := files[n]
		if marked[n] || m < 1.6*median {
			continue
		}
		ep := mapEpisode(n, 0, append(shifts, out...))
		a, okA := byNum[ep.First]
		b, okB := byNum[ep.First+1]
		if !okA || !okB {
			continue
		}
		sameTitle := a.Name != "" && strings.EqualFold(baseTitle(a.Name), baseTitle(b.Name))
		fits := a.Runtime > 0 && b.Runtime > 0 && math.Abs(m-(a.Runtime+b.Runtime)) <= 0.25*(a.Runtime+b.Runtime) &&
			math.Abs(m-a.Runtime) > 0.4*a.Runtime
		if sameTitle || fits {
			out = append(out, epShift{At: n, Kind: "double"})
		}
	}
	return out
}

// ---------- the target size of a double episode ----------

var rePart = regexp.MustCompile(`(?i)[ ._-]pt\d$`)

// episodeFactor is how many episodes a file holds, for the target size: 2 for
// a double episode ("S05E09-E10", or marked on the Plex names tab: marked),
// ½ for one half of a split episode ("…-pt1"); 1 otherwise. why: for the log.
func episodeFactor(input string, marked float64) (float64, string) {
	if marked > 0 && marked != 1 {
		if marked > 1 {
			return marked, fmt.Sprintf("it is marked as a double episode (%g episodes)", marked)
		}
		return marked, "it is marked as half of a split episode"
	}
	gs := guessSearch(input, "")
	if gs.Kind == "tv" && gs.Episode2 > gs.Episode {
		n := float64(gs.Episode2 - gs.Episode + 1)
		return n, fmt.Sprintf("its name says it is %g episodes (E%02d-E%02d)", n, gs.Episode, gs.Episode2)
	}
	if gs.Kind == "tv" && rePart.MatchString(strings.TrimSuffix(filepath.Base(input), filepath.Ext(input))) {
		return 0.5, "its name says it is half of a split episode"
	}
	return 1, ""
}

// scaleTarget applies episodeFactor to a target file size.
func scaleTarget(cfg *Config) string {
	if cfg.TargetBytes <= 0 {
		return ""
	}
	f, why := episodeFactor(cfg.Input, cfg.SizeFactor)
	if f == 1 {
		return ""
	}
	before := cfg.TargetBytes
	cfg.TargetBytes = int64(float64(cfg.TargetBytes) * f)
	verb := "doubled"
	switch {
	case f == 3:
		verb = "tripled"
	case f < 1:
		verb = "halved"
	case f != 2:
		verb = fmt.Sprintf("multiplied by %g", f)
	}
	return fmt.Sprintf("The target size is %s (%s → %s), as %s", verb, fmtSize(before), fmtSize(cfg.TargetBytes), why)
}

// renSizeFactor: what the Plex names tab's marks say a file holds (0: nothing marked).
func (g *guiServer) renSizeFactor(input string) float64 {
	g.ren.mu.Lock()
	defer g.ren.mu.Unlock()
	if g.ren.fixed[strings.ToLower(input)] {
		return 0
	}
	gs := guessSearch(input, "")
	if gs.Kind != "tv" || gs.Episode2 > 0 {
		return 0
	}
	pk := tmdbPickKey(gs)
	if pk == "" {
		return 0
	}
	if k := shiftKey(pk, gs.Season); g.tvNumbering()[k] == "tvmaze" { // TVmaze's way: a double is one long episode there
		return tvmazeLengthFactor(g.ren.tvmEps[k], gs.Episode)
	}
	e := mapEpisode(gs.Episode, 0, g.ren.shifts[shiftKey(pk, gs.Season)])
	switch {
	case e.Last > e.First:
		return float64(e.Last - e.First + 1)
	case e.Part > 0:
		return 0.5
	}
	return 0
}
