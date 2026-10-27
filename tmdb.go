package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ---------- metadata from TMDB (The Movie Database) ----------
//
// The user's own free API key (a v3 "API key" or a v4 "API read access
// token"), kept in the settings. Search, pick, and the fields are filled in;
// they stay editable, and nothing is written until the job runs.
// "This product uses the TMDB API but is not endorsed or certified by TMDB."

var (
	tmdbAPI    = "https://api.themoviedb.org/3" // variables so tests can use a local server
	tmdbImages = "https://image.tmdb.org/t/p/"
	tmdbClient = &http.Client{Timeout: 20 * time.Second}
)

const tmdbLang = "en-US"

type tmdbResult struct {
	ID       int    `json:"id"`
	Kind     string `json:"kind"` // movie | tv
	Title    string `json:"title"`
	Year     string `json:"year"`
	Overview string `json:"overview"`
	Thumb    string `json:"thumb,omitempty"` // a small poster, as a data: URL
}

// tmdbGet calls the API and decodes the JSON answer into out.
func tmdbGet(ctx context.Context, key, path string, q url.Values, out any) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("add your TMDB API key under Advanced first")
	}
	if q == nil {
		q = url.Values{}
	}
	q.Set("language", tmdbLang)
	bearer := strings.HasPrefix(key, "eyJ") // a v4 read access token (a JWT); otherwise a v3 API key
	if !bearer {
		q.Set("api_key", key)
	}
	req, err := http.NewRequestWithContext(ctx, "GET", tmdbAPI+path+"?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if bearer {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := tmdbClient.Do(req)
	if err != nil {
		return fmt.Errorf("could not reach TMDB (is this PC online?): %v", shortNetErr(err))
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Message string `json:"status_message"`
		}
		_ = json.Unmarshal(body, &e)
		switch resp.StatusCode {
		case http.StatusUnauthorized:
			return fmt.Errorf("TMDB did not accept the API key (check it under Advanced)")
		case http.StatusNotFound:
			return fmt.Errorf("TMDB has no such entry (%s)", strings.TrimSuffix(e.Message, "."))
		case http.StatusTooManyRequests:
			return fmt.Errorf("TMDB asks to slow down; try again in a few seconds")
		}
		if e.Message == "" {
			e.Message = resp.Status
		}
		return fmt.Errorf("TMDB: %s", e.Message)
	}
	return json.Unmarshal(body, out)
}

func shortNetErr(err error) string {
	var ue *url.Error
	if errors.As(err, &ue) && ue.Err != nil {
		return ue.Err.Error()
	}
	return err.Error()
}

// tmdbSearch finds films ("movie") or shows ("tv").
func tmdbSearch(ctx context.Context, key, kind, query, year string) ([]tmdbResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("type what to search for")
	}
	q := url.Values{"query": {query}, "include_adult": {"false"}}
	if y := strings.TrimSpace(year); y != "" {
		if kind == "tv" {
			q.Set("first_air_date_year", y)
		} else {
			q.Set("year", y)
		}
	}
	var r struct {
		Results []struct {
			ID           int
			Title, Name  string
			ReleaseDate  string `json:"release_date"`
			FirstAirDate string `json:"first_air_date"`
			Overview     string
			PosterPath   string `json:"poster_path"`
		}
	}
	path := "/search/movie"
	if kind == "tv" {
		path = "/search/tv"
	}
	if err := tmdbGet(ctx, key, path, q, &r); err != nil {
		return nil, err
	}
	var out []tmdbResult
	var posters []string
	for _, x := range r.Results {
		if len(out) == 12 {
			break
		}
		it := tmdbResult{ID: x.ID, Kind: kind, Title: x.Title, Year: yearOf(x.ReleaseDate), Overview: x.Overview}
		if kind == "tv" {
			it.Title, it.Year = x.Name, yearOf(x.FirstAirDate)
		}
		out = append(out, it)
		posters = append(posters, x.PosterPath)
	}
	// small posters, fetched here (a few at a time) so the page needs no internet access of its own
	var wg sync.WaitGroup
	sem := make(chan struct{}, 6)
	for i := range out {
		if posters[i] == "" {
			continue
		}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if b, mime, err := tmdbImage(ctx, "w92", posters[i], 1<<20); err == nil {
				out[i].Thumb = "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(b)
			}
		}(i)
	}
	wg.Wait()
	return out, nil
}

func yearOf(date string) string {
	if len(date) >= 4 {
		return date[:4]
	}
	return ""
}

// tmdbImage downloads one image at a size ("w92", "w780", "original").
func tmdbImage(ctx context.Context, size, path string, limit int64) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", tmdbImages+size+path, nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := tmdbClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("image: %s", resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, "", err
	}
	if int64(len(b)) > limit {
		return nil, "", fmt.Errorf("image too large")
	}
	mime := http.DetectContentType(b)
	if mime != "image/jpeg" && mime != "image/png" {
		return nil, "", fmt.Errorf("not a JPEG or PNG image")
	}
	return b, mime, nil
}

// tmdbFill is what goes into the metadata fields.
type tmdbFill struct {
	Title       string `json:"title"`
	Date        string `json:"date"`
	Description string `json:"description"`
	Cover       string `json:"cover,omitempty"` // the poster, saved on this PC
	Thumb       string `json:"thumb,omitempty"`
	Link        string `json:"link"` // the TMDB page
	Note        string `json:"note,omitempty"`
}

// tmdbDetails gets a film, or a show's episode (season, episode > 0) or the
// show itself, with its poster saved to coverDir.
func tmdbDetails(ctx context.Context, key, kind string, id, season, episode int, coverDir string) (*tmdbFill, error) {
	f := &tmdbFill{}
	var poster string
	switch kind {
	case "movie":
		var m struct {
			Title, Overview string
			ReleaseDate     string `json:"release_date"`
			PosterPath      string `json:"poster_path"`
		}
		if err := tmdbGet(ctx, key, fmt.Sprintf("/movie/%d", id), nil, &m); err != nil {
			return nil, err
		}
		f.Title, f.Date, f.Description, poster = m.Title, m.ReleaseDate, m.Overview, m.PosterPath
		f.Link = fmt.Sprintf("https://www.themoviedb.org/movie/%d", id)
	case "tv":
		var show struct {
			Name, Overview string
			FirstAirDate   string `json:"first_air_date"`
			PosterPath     string `json:"poster_path"`
		}
		if err := tmdbGet(ctx, key, fmt.Sprintf("/tv/%d", id), nil, &show); err != nil {
			return nil, err
		}
		f.Title, f.Date, f.Description, poster = show.Name, show.FirstAirDate, show.Overview, show.PosterPath
		f.Link = fmt.Sprintf("https://www.themoviedb.org/tv/%d", id)
		if season > 0 {
			var s struct {
				PosterPath string `json:"poster_path"`
			}
			if err := tmdbGet(ctx, key, fmt.Sprintf("/tv/%d/season/%d", id, season), nil, &s); err != nil {
				return nil, fmt.Errorf("season %d: %v", season, err)
			}
			if s.PosterPath != "" {
				poster = s.PosterPath // the season's poster suits an episode better
			}
			f.Link += fmt.Sprintf("/season/%d", season)
		}
		if season > 0 && episode > 0 {
			var e struct {
				Name, Overview string
				AirDate        string `json:"air_date"`
			}
			if err := tmdbGet(ctx, key, fmt.Sprintf("/tv/%d/season/%d/episode/%d", id, season, episode), nil, &e); err != nil {
				return nil, fmt.Errorf("season %d, episode %d: %v", season, episode, err)
			}
			f.Title = fmt.Sprintf("%s – S%02dE%02d", show.Name, season, episode)
			if e.Name != "" {
				f.Title += " – " + e.Name
			}
			f.Date, f.Description = e.AirDate, e.Overview
			f.Link += fmt.Sprintf("/episode/%d", episode)
		}
	default:
		return nil, fmt.Errorf("unknown kind %q", kind)
	}
	if poster != "" {
		b, mime, err := tmdbImage(ctx, "w780", poster, 8<<20)
		if err == nil {
			ext := map[string]string{"image/jpeg": ".jpg", "image/png": ".png"}[mime]
			name := filepath.Join(coverDir, fmt.Sprintf("tmdb-%s-%d", kind, id))
			if season > 0 {
				name += fmt.Sprintf("-s%d", season)
			}
			name += ext
			if err = os.MkdirAll(coverDir, 0o755); err == nil {
				err = os.WriteFile(name, b, 0o644)
			}
			if err == nil {
				f.Cover = name
				if small, smime, e2 := tmdbImage(ctx, "w92", poster, 1<<20); e2 == nil {
					f.Thumb = "data:" + smime + ";base64," + base64.StdEncoding.EncodeToString(small)
				}
			}
		}
		if err != nil {
			f.Note = "The poster could not be downloaded (" + err.Error() + "); the cover is unchanged."
		}
	}
	return f, nil
}

// cleanCovers removes downloaded posters older than 60 days (queued files may
// still need recent ones).
func cleanCovers(dir string) {
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if info, err := e.Info(); err == nil && strings.HasPrefix(e.Name(), "tmdb-") && time.Since(info.ModTime()) > 60*24*time.Hour {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

// ---------- a search from the file's name ----------

var (
	reEpisode  = regexp.MustCompile(`(?i)\bS(\d{1,2})[ ._-]?E(\d{1,3})\b|\b(\d{1,2})x(\d{2,3})\b`)
	reYear     = regexp.MustCompile(`\b(19[0-9]{2}|20[0-9]{2})\b`)
	reBrackets = regexp.MustCompile(`\[[^\]]*\]|\{[^}]*\}`)
	reJunk     = regexp.MustCompile(`(?i)\b(2160p|1080p|1080i|720p|576p|480p|4k|uhd|bluray|blu ray|bdrip|brrip|remux|web dl|webdl|webrip|web|hdtv|dvdrip|hdr10plus|hdr10|hdr|dovi|dv|sdr|x265|x264|h265|h264|hevc|avc|10bit|aac|ac3|eac3|ddp\d?|dd\d|atmos|truehd|dts( hd)?|ma|flac|proper|repack|extended|unrated|remastered|imax|criterion|directors cut|multi|subs?)\b`)
)

type tmdbGuess struct {
	Kind    string `json:"kind"`
	Query   string `json:"query"`
	Year    string `json:"year"`
	Season  int    `json:"season"`
	Episode int    `json:"episode"`
}

// guessSearch turns "Dune.Part.Two.2024.2160p.UHD.REMUX_KTL.mkv" into a
// search for the film "Dune Part Two" from 2024, and "The Bear S02E03 ..."
// into season 2, episode 3 of "The Bear". The folder's name is used when the
// file's own name says nothing ("movie.mkv" in "Heat (1995)").
func guessSearch(path, tag string) tmdbGuess {
	g := guessName(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)), tag)
	d := guessName(filepath.Base(filepath.Dir(path)), "")
	switch {
	case d.Query == "":
	case g.Query == "": // "S01E03.mkv" in "The Bear"
		if g.Kind == "tv" {
			d.Kind, d.Season, d.Episode, d.Year = "tv", g.Season, g.Episode, ""
		}
		return d
	case g.Kind == "movie" && g.Year == "" && d.Kind == "movie" && d.Year != "": // "movie.mkv" in "Heat (1995)"
		return d
	}
	return g
}

func guessName(stem, tag string) tmdbGuess {
	g := tmdbGuess{Kind: "movie"}
	for _, t := range []string{tag, "_KTL", "_hdr"} {
		if t != "" && strings.HasSuffix(strings.ToLower(stem), strings.ToLower(t)) {
			stem = stem[:len(stem)-len(t)]
		}
	}
	s := reBrackets.ReplaceAllString(stem, " ")
	s = strings.NewReplacer(".", " ", "_", " ", "(", " ", ")", " ").Replace(s)
	s = strings.Join(strings.Fields(s), " ")
	cut := len(s)
	if m := reEpisode.FindStringSubmatchIndex(s); m != nil {
		g.Kind = "tv"
		sub := reEpisode.FindStringSubmatch(s)
		if sub[1] != "" {
			g.Season, _ = strconv.Atoi(sub[1])
			g.Episode, _ = strconv.Atoi(sub[2])
		} else {
			g.Season, _ = strconv.Atoi(sub[3])
			g.Episode, _ = strconv.Atoi(sub[4])
		}
		cut = m[0]
	}
	// the year: the last one before the episode / release details, but not the title itself ("1917")
	for _, m := range reYear.FindAllStringIndex(s[:cut], -1) {
		if m[0] > 0 {
			g.Year, cut = s[m[0]:m[1]], m[0]
		}
	}
	if m := reJunk.FindStringIndex(s[:cut]); m != nil && m[0] > 0 {
		cut = m[0]
	}
	g.Query = strings.Trim(strings.TrimSpace(s[:cut]), "-– ")
	if g.Kind == "tv" {
		g.Year = "" // a year next to an episode is usually the show's, not worth narrowing by
	}
	return g
}

// ---------- the window's requests ----------

func (g *guiServer) tmdbKey() string {
	var k string
	_ = json.Unmarshal(g.loadPrefs()["tmdbKey"], &k)
	return strings.TrimSpace(k)
}

func (g *guiServer) tmdbCall(ctx context.Context, name string, body []byte) map[string]any {
	var b struct {
		Kind, Query, Year, Path, Tag string
		ID, Season, Episode          int
		Key                          string
	}
	_ = json.Unmarshal(body, &b)
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	switch name {
	case "tmdbGuess":
		return map[string]any{"ok": true, "guess": guessSearch(b.Path, b.Tag), "hasKey": g.tmdbKey() != ""}
	case "tmdbSearch":
		res, err := tmdbSearch(ctx, g.tmdbKey(), b.Kind, b.Query, b.Year)
		if err != nil {
			return failResult(err.Error())
		}
		return map[string]any{"ok": true, "results": res}
	case "tmdbDetails":
		dir := filepath.Join(appDataDir(), "covers")
		cleanCovers(dir)
		f, err := tmdbDetails(ctx, g.tmdbKey(), b.Kind, b.ID, b.Season, b.Episode, dir)
		if err != nil {
			return failResult(err.Error())
		}
		return map[string]any{"ok": true, "fill": f}
	case "tmdbTestKey":
		var r struct{}
		if err := tmdbGet(ctx, b.Key, "/configuration", nil, &r); err != nil {
			return failResult(err.Error())
		}
		return okResult()
	}
	return failResult("unknown request")
}
