package main

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ---------- NFO files for Plex ----------
//
// With "NFO file for Plex after a TMDB match", each new file that has a TMDB
// (or TVmaze) match gets an NFO beside it with the same name, which Plex reads
// with its "Plex NFO Movie" / "Plex NFO Series" agents (Plex Media Server
// 1.43.1 and later; https://support.plex.tv/articles/using-nfo-metadata-files-with-plex/):
//   - a film: <movie> with its title, plot, tagline, date, rating, genres,
//     studios, countries, collection, director, writers and TMDB / IMDb ids;
//   - an episode: <episodedetails> with its title, season, episode, plot,
//     air date, director and writers; a double episode gets one block each.
// The owner's choices (22 November 2026): no cast (Plex fills in its own cast,
// with photos, only when the NFO has none), no tvshow.nfo, new files only, and
// an NFO already there is never overwritten.

// nfoDoc travels with the job in -meta-json ("nfo"), filled in at the lookup.
type nfoDoc struct {
	Movie    *nfoMovie    `json:"movie,omitempty"`
	Episodes []nfoEpisode `json:"episodes,omitempty"`
}

type nfoMovie struct {
	Title         string   `json:"title"`
	OriginalTitle string   `json:"originalTitle,omitempty"`
	Plot          string   `json:"plot,omitempty"`
	Tagline       string   `json:"tagline,omitempty"`
	Premiered     string   `json:"premiered,omitempty"`
	MPAA          string   `json:"mpaa,omitempty"`
	Rating        float64  `json:"rating,omitempty"` // TMDB's, out of 10
	Votes         int      `json:"votes,omitempty"`
	Genres        []string `json:"genres,omitempty"`
	Studios       []string `json:"studios,omitempty"`
	Countries     []string `json:"countries,omitempty"`
	Set           string   `json:"set,omitempty"` // the TMDB collection
	Directors     []string `json:"directors,omitempty"`
	Writers       []string `json:"writers,omitempty"`
	TMDB          int      `json:"tmdb"`
	IMDB          string   `json:"imdb,omitempty"`
}

type nfoEpisode struct {
	Title     string   `json:"title"`
	Season    int      `json:"season"`
	Episode   int      `json:"episode"`
	Plot      string   `json:"plot,omitempty"`
	Aired     string   `json:"aired,omitempty"`
	Directors []string `json:"directors,omitempty"`
	Writers   []string `json:"writers,omitempty"`
}

func (d *nfoDoc) empty() bool { return d == nil || (d.Movie == nil && len(d.Episodes) == 0) }

// the XML Plex reads (tag names from Plex's article)
type xmlRating struct {
	Name    string  `xml:"name,attr"`
	Max     int     `xml:"max,attr"`
	Default bool    `xml:"default,attr"`
	Value   float64 `xml:"value"`
	Votes   int     `xml:"votes,omitempty"`
}

type xmlUniqueID struct {
	Type    string `xml:"type,attr"`
	Default bool   `xml:"default,attr,omitempty"`
	ID      string `xml:",chardata"`
}

type xmlMovie struct {
	XMLName       xml.Name      `xml:"movie"`
	Title         string        `xml:"title"`
	OriginalTitle string        `xml:"originaltitle,omitempty"`
	Plot          string        `xml:"plot,omitempty"`
	Tagline       string        `xml:"tagline,omitempty"`
	Ratings       []xmlRating   `xml:"ratings>rating,omitempty"`
	MPAA          string        `xml:"mpaa,omitempty"`
	Premiered     string        `xml:"premiered,omitempty"`
	Studios       []string      `xml:"studio,omitempty"`
	Genres        []string      `xml:"genre,omitempty"`
	Countries     []string      `xml:"country,omitempty"`
	Set           *xmlSet       `xml:"set,omitempty"`
	UniqueIDs     []xmlUniqueID `xml:"uniqueid"`
	Credits       []string      `xml:"credits,omitempty"`
	Directors     []string      `xml:"director,omitempty"`
}

type xmlSet struct {
	Name string `xml:"name"`
}

type xmlEpisode struct {
	XMLName   xml.Name `xml:"episodedetails"`
	Title     string   `xml:"title"`
	Season    int      `xml:"season"`
	Episode   int      `xml:"episode"`
	Plot      string   `xml:"plot,omitempty"`
	Aired     string   `xml:"aired,omitempty"`
	Directors []string `xml:"director,omitempty"`
	Credits   []string `xml:"credits,omitempty"`
}

// nfoXML is the file's text. A double episode is one <episodedetails> after
// another, as Plex (and Kodi) read them.
func nfoXML(d *nfoDoc) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n")
	enc := xml.NewEncoder(&b)
	enc.Indent("", "  ")
	if m := d.Movie; m != nil {
		x := xmlMovie{Title: m.Title, Plot: m.Plot, Tagline: m.Tagline, MPAA: m.MPAA, Premiered: m.Premiered,
			Studios: m.Studios, Genres: m.Genres, Countries: m.Countries, Credits: m.Writers, Directors: m.Directors}
		if m.OriginalTitle != m.Title {
			x.OriginalTitle = m.OriginalTitle
		}
		if m.Rating > 0 {
			x.Ratings = []xmlRating{{Name: "themoviedb", Max: 10, Default: true, Value: m.Rating, Votes: m.Votes}}
		}
		if m.Set != "" {
			x.Set = &xmlSet{Name: m.Set}
		}
		x.UniqueIDs = append(x.UniqueIDs, xmlUniqueID{Type: "tmdb", Default: true, ID: strconv.Itoa(m.TMDB)})
		if m.IMDB != "" {
			x.UniqueIDs = append(x.UniqueIDs, xmlUniqueID{Type: "imdb", ID: m.IMDB})
		}
		if err := enc.Encode(x); err != nil {
			return nil, err
		}
	} else {
		for i, e := range d.Episodes {
			if i > 0 {
				b.WriteString("\n")
			}
			if err := enc.Encode(xmlEpisode{Title: e.Title, Season: e.Season, Episode: e.Episode, Plot: e.Plot, Aired: e.Aired,
				Directors: e.Directors, Credits: e.Writers}); err != nil {
				return nil, err
			}
		}
	}
	b.WriteString("\n")
	return b.Bytes(), nil
}

// nfoPath is the NFO beside a video: the same name, ending .nfo.
func nfoPath(video string) string {
	return strings.TrimSuffix(video, filepath.Ext(video)) + ".nfo"
}

// writeNFO writes the NFO beside the new file, unless one is there already.
// It says in a line what it did.
func writeNFO(video string, d *nfoDoc) (string, error) {
	if d.empty() {
		return "NFO: not written (no TMDB details for this file)", nil
	}
	path := nfoPath(video)
	if _, err := os.Stat(path); err == nil {
		return "NFO: " + filepath.Base(path) + " is already there, so it was left as it is", nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	data, err := nfoXML(d)
	if err != nil {
		return "", err
	}
	if err := writeFileNew(path, data); err != nil {
		return "", err
	}
	what := "the film"
	if len(d.Episodes) > 1 {
		what = fmt.Sprintf("%d episodes", len(d.Episodes))
	} else if len(d.Episodes) == 1 {
		what = "the episode"
	}
	return "NFO: written for Plex (" + what + "): " + filepath.Base(path), nil
}

// writeFileNew creates a file that must not exist yet (a race with another
// program writing the same NFO leaves theirs alone).
func writeFileNew(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return nil
		}
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		_ = os.Remove(path)
		return err
	}
	return f.Close()
}

// nfoOn: the setting "NFO file for Plex after a TMDB match" (remembered, off at first).
func (g *guiServer) nfoOn() bool {
	var on bool
	_ = json.Unmarshal(g.loadPrefs()["nfoFiles"], &on)
	return on
}

// ---------- from TMDB / TVmaze ----------

type tmdbCrew struct {
	Job        string `json:"job"`
	Department string `json:"department"`
	Name       string `json:"name"`
}

// crewNames: the directors and the writers, each once, in TMDB's order.
func crewNames(crew []tmdbCrew) (directors, writers []string) {
	seen := map[string]bool{}
	for _, c := range crew {
		switch {
		case c.Job == "Director" && !seen["d"+c.Name]:
			seen["d"+c.Name] = true
			directors = append(directors, c.Name)
		case c.Department == "Writing" && !seen["w"+c.Name]:
			seen["w"+c.Name] = true
			writers = append(writers, c.Name)
		}
	}
	return
}

func tmdbEpisodeNFO(x tmdbEpisode, season int) nfoEpisode {
	d, w := crewNames(x.Crew)
	return nfoEpisode{Title: x.Name, Season: season, Episode: x.Number, Plot: x.Overview, Aired: x.AirDate, Directors: d, Writers: w}
}

// tvmazeEpisodesNFO: the episodes first..last of a TVmaze season.
func tvmazeEpisodesNFO(eps []tvmazeEp, season, first, last int) []nfoEpisode {
	var out []nfoEpisode
	for _, e := range eps {
		if e.Season == season && e.Number >= first && e.Number <= max(first, last) {
			out = append(out, nfoEpisode{Title: e.Name, Season: season, Episode: e.Number, Plot: plainText(e.Summary), Aired: e.Airdate})
		}
	}
	return out
}

// usCertification: the film's US rating (PG-13...) from TMDB's release dates.
func usCertification(rd struct {
	Results []struct {
		Country string `json:"iso_3166_1"`
		Dates   []struct {
			Certification string `json:"certification"`
		} `json:"release_dates"`
	} `json:"results"`
}) string {
	for _, r := range rd.Results {
		if r.Country != "US" {
			continue
		}
		for _, d := range r.Dates {
			if c := strings.TrimSpace(d.Certification); c != "" {
				return c
			}
		}
	}
	return ""
}

func names(xs []struct {
	Name string `json:"name"`
}) []string {
	var out []string
	for _, x := range xs {
		if x.Name != "" {
			out = append(out, x.Name)
		}
	}
	return out
}
