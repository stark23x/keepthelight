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
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
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

// tmdbLanguage is the language TMDB answers in (a setting; English when unset).
var tmdbLang atomic.Value

func tmdbLanguage() string {
	if v, _ := tmdbLang.Load().(string); v != "" {
		return v
	}
	return "en-US"
}

// tmdbLanguages are the choices offered (TMDB has more; these cover most libraries).
var tmdbLanguages = map[string]bool{"en-US": true, "en-GB": true, "es-ES": true, "es-MX": true, "fr-FR": true, "fr-CA": true, "de-DE": true,
	"it-IT": true, "pt-BR": true, "pt-PT": true, "nl-NL": true, "sv-SE": true, "da-DK": true, "nb-NO": true, "fi-FI": true, "pl-PL": true,
	"ru-RU": true, "ja-JP": true, "ko-KR": true, "zh-CN": true, "zh-TW": true}

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
		return fmt.Errorf("add your TMDB API key in Settings first")
	}
	if q == nil {
		q = url.Values{}
	}
	q.Set("language", tmdbLanguage())
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
			return fmt.Errorf("TMDB did not accept the API key (check it in Settings)")
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
	PlexName    string `json:"plexName,omitempty"` // the Plex-style file name, without extension ("" for a show without an episode)
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
		f.PlexName = plexFilmName(m.Title, yearOf(m.ReleaseDate), id)
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
			f.PlexName = plexEpisodeName(show.Name, yearOf(show.FirstAirDate), season, episode, e.Name)
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
	reEpisode  = regexp.MustCompile(`(?i)\bS(\d{1,2})[ ._-]?E(\d{1,3})(?:(?:[ ._]?-[ ._]?E?|[ ._]?E)(\d{1,3}))?\b|\b(\d{1,2})x(\d{2,3})\b`)
	reYear     = regexp.MustCompile(`\b(19[0-9]{2}|20[0-9]{2})\b`)
	reBrackets = regexp.MustCompile(`\[[^\]]*\]|\{[^}]*\}`)
	reJunk     = regexp.MustCompile(`(?i)\b(2160p|1080p|1080i|720p|576p|480p|4k|uhd|bluray|blu ray|bdrip|brrip|remux|web dl|webdl|webrip|web|hdtv|dvdrip|hdr10plus|hdr10|hdr|dovi|dv|sdr|x265|x264|h265|h264|hevc|avc|10bit|aac|ac3|eac3|ddp\d?|dd\d|atmos|truehd|dts( hd)?|ma|flac|proper|repack|extended|unrated|remastered|imax|criterion|directors cut|multi|subs?)\b`)
)

type tmdbGuess struct {
	Kind     string `json:"kind"`
	Query    string `json:"query"`
	Year     string `json:"year"`
	Season   int    `json:"season"`
	Episode  int    `json:"episode"`
	Episode2 int    `json:"episode2,omitempty"` // a double episode in one file: "S05E09-E10"
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
			d.Kind, d.Season, d.Episode, d.Episode2, d.Year = "tv", g.Season, g.Episode, g.Episode2, ""
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
			if e2, _ := strconv.Atoi(sub[3]); e2 > g.Episode && e2 <= g.Episode+4 {
				g.Episode2 = e2 // "S05E09-E10", "S05E09E10", "S05E09-10"
			}
		} else {
			g.Season, _ = strconv.Atoi(sub[4])
			g.Episode, _ = strconv.Atoi(sub[5])
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

// The key is kept in Windows' Credential Manager (an older one in the settings
// file is moved there the first time it is read).
const tmdbCredName = "Keep the Light/TMDB API key"

func (g *guiServer) tmdbKey() string {
	if k, err := readSecret(tmdbCredName); err == nil && k != "" {
		return strings.TrimSpace(k)
	}
	var k string
	_ = json.Unmarshal(g.loadPrefs()["tmdbKey"], &k)
	if k = strings.TrimSpace(k); k != "" && secretStoreOK() {
		if writeSecret(tmdbCredName, k) == nil {
			_ = g.savePref("tmdbKey", json.RawMessage(`""`))
		}
	}
	return k
}

func (g *guiServer) setTMDBKey(k string) error {
	k = strings.TrimSpace(k)
	if secretStoreOK() {
		var err error
		if k == "" {
			err = deleteSecret(tmdbCredName)
		} else {
			err = writeSecret(tmdbCredName, k)
		}
		if err != nil {
			return err
		}
		return g.savePref("tmdbKey", json.RawMessage(`""`)) // never in the settings file
	}
	b, _ := json.Marshal(k)
	return g.savePref("tmdbKey", b)
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
	case "tmdbKeySet":
		if err := g.setTMDBKey(b.Key); err != nil {
			return failResult("could not store the key: " + err.Error())
		}
		return okResult()
	case "tmdbLang":
		var l struct{ Lang string }
		_ = json.Unmarshal(body, &l)
		if !tmdbLanguages[l.Lang] {
			return failResult("unknown language")
		}
		tmdbLang.Store(l.Lang)
		raw, _ := json.Marshal(l.Lang)
		_ = g.savePref("tmdbLang", raw)
		return okResult()
	case "tmdbRemember": // the page matched a file by hand
		var p struct {
			Path, Kind, Title string
			ID                int
		}
		_ = json.Unmarshal(body, &p)
		key := tmdbPickKey(guessSearch(p.Path, b.Tag))
		if key == "" || p.ID <= 0 || (p.Kind != "movie" && p.Kind != "tv") {
			return failResult("nothing to remember")
		}
		g.rememberTMDB(key, tmdbPick{Kind: p.Kind, ID: p.ID, Title: p.Title})
		return okResult()
	case "tmdbForget":
		_ = g.savePref("tmdbPicks", json.RawMessage(`{}`))
		return okResult()
	case "tmdbTestKey":
		var r struct{}
		if err := tmdbGet(ctx, b.Key, "/configuration", nil, &r); err != nil {
			return failResult(err.Error())
		}
		return okResult()
	}
	return failResult("unknown request")
}

// ---------- matches chosen by hand, remembered ----------

// When a file is matched by hand, the choice is remembered by the title its
// name gives ("the office" → The Office (US), TMDB id 2316), so the show's
// other episodes, and later seasons, match without asking.

type tmdbPick struct {
	Kind  string `json:"kind"`
	ID    int    `json:"id"`
	Title string `json:"title"`
}

// tmdbPickKey: the name's title, its kind, and for films the year (two films
// can share a title; a show's episodes rarely carry its year).
func tmdbPickKey(gs tmdbGuess) string {
	q := normTitle(gs.Query)
	if q == "" {
		return ""
	}
	if gs.Kind == "movie" {
		return "movie|" + q + "|" + gs.Year
	}
	return "tv|" + q
}

func (g *guiServer) tmdbPicks() map[string]tmdbPick {
	m := map[string]tmdbPick{}
	_ = json.Unmarshal(g.loadPrefs()["tmdbPicks"], &m)
	return m
}

func (g *guiServer) rememberTMDB(key string, p tmdbPick) {
	m := g.tmdbPicks()
	if len(m) >= 500 { // a long list is never needed; start again rather than grow without end
		m = map[string]tmdbPick{}
	}
	m[key] = p
	if b, err := json.Marshal(m); err == nil {
		_ = g.savePref("tmdbPicks", b)
	}
}

// queueTMDBFollow fills in the waiting files that point to the same title as a
// file just matched by hand (each with its own season and episode).
func (g *guiServer) queueTMDBFollow(id int) map[string]any {
	g.mu.Lock()
	it := g.findItemLocked(id)
	if it == nil {
		g.mu.Unlock()
		return failResult("that file is no longer in the queue")
	}
	key := tmdbPickKey(guessSearch(it.Input, ""))
	var ids []int
	for _, x := range g.q.items {
		if x.ID == id || x.Editing || !(x.pending() || x.Status == stProblem) || (x.s.Meta != nil && x.s.Meta.Tags["title"] != "") {
			continue
		}
		if key != "" && tmdbPickKey(guessSearch(x.Input, "")) == key {
			ids = append(ids, x.ID)
		}
	}
	g.mu.Unlock()
	pick, ok := g.tmdbPicks()[key]
	if !ok || len(ids) == 0 {
		return map[string]any{"ok": true, "files": 0}
	}
	r := g.queueTMDB(ids)
	r["title"] = pick.Title
	return r
}

// ---------- the "Which one?" window ----------

// tmdbAsk is one title the lookup could not settle: several possible matches,
// or none. The window asks once per title, however many files have it.
type tmdbAsk struct {
	Key     string `json:"key"`
	Kind    string `json:"kind"`
	Query   string `json:"query"`
	Year    string `json:"year"`
	Files   int    `json:"files"`
	Example string `json:"example"`
	IDs     []int  `json:"ids"`
	Rows    []int  `json:"rows,omitempty"` // files on the Plex names tab (not in the queue)
	None    bool   `json:"none"`           // nothing was found (rather than several)
}

// tmdbUnsettled lists the waiting files still needing a match, by title.
func (g *guiServer) tmdbUnsettled() []tmdbAsk {
	g.mu.Lock()
	defer g.mu.Unlock()
	var list []tmdbAsk
	at := map[string]int{}
	for _, it := range g.q.items {
		if it.Editing || !(it.pending() || it.Status == stProblem) || (it.s.Meta != nil && it.s.Meta.Tags["title"] != "") {
			continue
		}
		var m struct {
			Note string `json:"tmdbNote"`
		}
		_ = json.Unmarshal(it.Raw, &m)
		none := strings.HasPrefix(m.Note, "TMDB: nothing found")
		if !none && !strings.HasPrefix(m.Note, "TMDB: needs a match") {
			continue
		}
		gs := guessSearch(it.Input, "")
		key := tmdbPickKey(gs)
		if key == "" {
			key = "file|" + it.Input // no title in the name: asked about on its own
		}
		i, ok := at[key]
		if !ok {
			i, at[key] = len(list), len(list)
			list = append(list, tmdbAsk{Key: key, Kind: gs.Kind, Query: gs.Query, Year: gs.Year, Example: it.Name, None: none})
		}
		list[i].Files++
		list[i].IDs = append(list[i].IDs, it.ID)
	}
	list = g.renAsksInto(list)
	sort.SliceStable(list, func(a, b int) bool { return list[a].Files > list[b].Files })
	return list
}

// tmdbChoose settles a title: the choice is remembered, and every waiting file
// with that title is filled in from it (each episode with its own season and episode).
func (g *guiServer) tmdbChoose(key string, p tmdbPick) map[string]any {
	if p.ID <= 0 || (p.Kind != "movie" && p.Kind != "tv") {
		return failResult("nothing chosen")
	}
	var ids, rows []int
	for _, a := range g.tmdbUnsettled() {
		if a.Key == key {
			ids, rows = a.IDs, a.Rows
		}
	}
	if len(ids) == 0 && len(rows) == 0 {
		return failResult("those files are no longer waiting for a match")
	}
	if !strings.HasPrefix(key, "file|") {
		g.rememberTMDB(key, p)
	} else {
		g.mu.Lock() // a single file without a title in its name: matched directly
		g.oneOff = map[string]tmdbPick{key: p}
		g.mu.Unlock()
	}
	r := map[string]any{"ok": true, "files": 0}
	if len(ids) > 0 {
		r = g.queueTMDB(ids)
	}
	if len(rows) > 0 { // files on the Plex names tab
		if x := g.renLookup(rows, map[string]tmdbPick{key: p}); x["ok"] == true {
			n, _ := r["files"].(int)
			r["ok"], r["files"] = true, n+x["files"].(int)
		}
	}
	r["title"] = p.Title
	return r
}

// ---------- the queue: matching without asking ----------

// normTitle compares titles loosely: letters and digits only, lower case
// ("Spider-Man: No Way Home" = "spider man no way home").
func normTitle(s string) string {
	var b strings.Builder
	space := false
	for _, r := range strings.ToLower(s) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			b.WriteRune(r)
			space = false
		case r == '&':
			if b.Len() > 0 {
				b.WriteString(" and")
			}
			space = true
		default:
			space = true
		}
	}
	return strings.TrimPrefix(b.String(), "the ")
}

// tmdbAutoMatch picks the one clear result for a search, or none: a single
// result, or a single one whose title equals the search (and year, if known).
func tmdbAutoMatch(res []tmdbResult, g tmdbGuess) *tmdbResult {
	if len(res) == 1 && (g.Year == "" || res[0].Year == g.Year || res[0].Year == "") {
		return &res[0]
	}
	var hit *tmdbResult
	n := 0
	for i := range res {
		r := &res[i]
		if normTitle(r.Title) == normTitle(g.Query) && (g.Year == "" || r.Year == g.Year) {
			if n++; n == 1 {
				hit = r
			}
		}
	}
	if n == 1 {
		return hit
	}
	return nil
}

// queueTMDB looks up waiting files on TMDB, in the background. A clear match
// fills in the title, date, description and cover (as an Edit would); anything
// else is marked "needs a match", to be chosen with Edit. Files whose title was
// already changed are left alone.
func (g *guiServer) queueTMDB(ids []int) map[string]any { return g.queueTMDBAgain(ids, false) }

// queueTMDBAgain: again=true also looks up files whose title came from TMDB
// (after a double episode was marked, their numbers changed).
func (g *guiServer) queueTMDBAgain(ids []int, again bool) map[string]any {
	key := g.tmdbKey()
	if key == "" {
		return failResult("Add your free TMDB API key in Settings (⚙) first.")
	}
	want := map[int]bool{}
	for _, id := range ids {
		want[id] = true
	}
	g.mu.Lock()
	var todo []int
	for _, it := range g.q.items {
		if (len(ids) == 0 || want[it.ID]) && (it.pending() || it.Status == stProblem) && !it.Editing {
			if it.s.Meta != nil && it.s.Meta.Tags["title"] != "" {
				var m struct {
					Note string `json:"tmdbNote"`
				}
				_ = json.Unmarshal(it.Raw, &m)
				if !again || !strings.HasPrefix(m.Note, "TMDB: ") {
					continue
				}
			}
			todo = append(todo, it.ID)
			setRawField(it, "tmdbNote", "TMDB: looking up…")
		}
	}
	g.mu.Unlock()
	if len(todo) == 0 {
		return failResult("No waiting files to look up (files with a title of their own are skipped).")
	}
	go func() {
		dir := filepath.Join(appDataDir(), "covers")
		cleanCovers(dir)
		picks := g.tmdbPicks()
		g.mu.Lock()
		for k, v := range g.oneOff { // a file chosen in the window that has no title in its name
			picks[k] = v
		}
		g.oneOff = nil
		g.mu.Unlock()
		lk := &tmdbLookup{key: key, coverDir: dir, picks: picks, found: map[string]*tmdbFound{}, shifts: g.renShifts()}
		for _, id := range todo {
			g.mu.Lock()
			it := g.findItemLocked(id)
			var input string
			if it != nil && !it.Editing && (it.pending() || it.Status == stProblem) {
				input = it.Input
			}
			g.mu.Unlock()
			if input == "" {
				continue
			}
			note, meta := lk.file(input)
			g.mu.Lock()
			if it := g.findItemLocked(id); it != nil && it.Input == input && !it.Editing && (it.pending() || it.Status == stProblem) {
				if meta != nil {
					m := it.s.Meta
					if m == nil {
						m = &MetaEdit{}
					}
					if m.Tags == nil {
						m.Tags = map[string]string{}
					}
					for k, v := range meta.Tags {
						m.Tags[k] = v
					}
					if meta.Cover != "" {
						m.Cover, m.RemoveCover = meta.Cover, false
					}
					setRawField(it, "meta", m)
					if meta.plexName != "" { // a proposal: only used once confirmed in Review names
						setRawField(it, "plexName", meta.plexName)
					}
				}
				setRawField(it, "tmdbNote", note)
				var s settings
				if json.Unmarshal(it.Raw, &s) == nil {
					if cfg, err := validateSettings(s); err == nil {
						it.s, it.cfg = s, cfg
						it.Status, it.Note, it.checked = stChecking, "", false
					}
				}
			}
			g.mu.Unlock()
			g.kickChecker()
		}
		g.saveQueue()
	}()
	return map[string]any{"ok": true, "files": len(todo)}
}

// tmdbLookup is one run of the queue's lookup: the remembered matches, and
// what each title gave, so a show's episodes are searched once, not once each.
type tmdbLookup struct {
	key, coverDir string
	picks         map[string]tmdbPick
	found         map[string]*tmdbFound
	shifts        map[string][]epShift // double / split episodes marked on the Plex names tab (episodes.go)
}

type tmdbFound struct {
	hit        *tmdbResult
	n          int  // results, when there was no clear match
	remembered bool // the match was chosen by hand before
	err        error
}

// tmdbLookupFile finds one file on TMDB without asking (see tmdbLookup.file).
func tmdbLookupFile(key, input, coverDir string) (string, *MetaEdit) {
	return (&tmdbLookup{key: key, coverDir: coverDir, found: map[string]*tmdbFound{}}).file(input)
}

// file finds one file on TMDB without asking: the note for the queue list, and
// the metadata when there was one clear (or remembered) match.
// showFor finds the film or show a file is of, as the lookup would (remembered
// match first, else a search that matches clearly). nil: none settled.
func (lk *tmdbLookup) showFor(ctx context.Context, input string) (*tmdbResult, error) {
	gs := guessSearch(input, "")
	pk := tmdbPickKey(gs)
	if p, ok := lk.picks[pk]; ok && pk != "" {
		return &tmdbResult{ID: p.ID, Kind: p.Kind, Title: p.Title}, nil
	}
	if gs.Query == "" {
		return nil, nil
	}
	res, err := tmdbSearch(ctx, lk.key, gs.Kind, gs.Query, gs.Year)
	if err != nil {
		return nil, err
	}
	return tmdbAutoMatch(res, gs), nil
}

func (lk *tmdbLookup) file(input string) (string, *MetaEdit) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	key, coverDir := lk.key, lk.coverDir
	gs := guessSearch(input, "")
	pk := tmdbPickKey(gs)
	if pk == "" {
		pk = "file|" + input
	}
	if _, ok := lk.picks[pk]; gs.Query == "" && !ok {
		return "TMDB: needs a match (no title in the file name; choose it, or use Edit)", nil
	}
	fd := lk.found[pk]
	if fd == nil {
		fd = &tmdbFound{}
		if p, ok := lk.picks[pk]; ok {
			fd.hit, fd.remembered = &tmdbResult{ID: p.ID, Kind: p.Kind, Title: p.Title}, true
		} else {
			res, err := tmdbSearch(ctx, key, gs.Kind, gs.Query, gs.Year)
			if err == nil && len(res) == 0 && gs.Year != "" { // the year in a name is sometimes off by one
				res, err = tmdbSearch(ctx, key, gs.Kind, gs.Query, "")
			}
			fd.err, fd.n = err, len(res)
			if err == nil {
				fd.hit = tmdbAutoMatch(res, gs)
			}
		}
		if fd.err == nil || ctx.Err() == nil { // a timeout is not remembered: the next file tries again
			lk.found[pk] = fd
		}
	}
	if fd.err != nil {
		return "TMDB: " + fd.err.Error(), nil
	}
	hit := fd.hit
	if hit == nil {
		if fd.n == 0 {
			return "TMDB: nothing found for \"" + gs.Query + "\" (use Edit to search)", nil
		}
		return fmt.Sprintf("TMDB: needs a match (%d possible results for \"%s\"; choose it, and the other files of that title follow)", fd.n, gs.Query), nil
	}
	var f *tmdbFill
	var err error
	ep := tmdbEp{First: gs.Episode, Last: gs.Episode2}
	if _, renumbered := lk.shifts["fixed|"+strings.ToLower(input)]; gs.Kind == "tv" && !renumbered {
		ep = mapEpisode(gs.Episode, gs.Episode2, lk.shifts[shiftKey(pk, gs.Season)])
	}
	if ep.Last > ep.First || ep.Part > 0 {
		f, err = tmdbEpisodes(ctx, key, hit.ID, gs.Season, ep, coverDir)
	} else {
		f, err = tmdbDetails(ctx, key, hit.Kind, hit.ID, gs.Season, ep.First, coverDir)
	}
	if err != nil {
		return "TMDB: " + err.Error(), nil
	}
	m := &MetaEdit{Tags: map[string]string{}, Cover: f.Cover}
	for k, v := range map[string]string{"title": f.Title, "date": f.Date, "description": f.Description} {
		if v != "" { // nothing from TMDB: the file's own stays
			m.Tags[k] = v
		}
	}
	what := f.Title
	if hit.Year != "" && gs.Kind == "movie" {
		what += " (" + hit.Year + ")"
	}
	m.plexName = f.PlexName
	m.tvID, m.tmdbEp = 0, ep
	if hit.Kind == "tv" {
		m.tvID = hit.ID
	}
	if gs.Kind == "tv" && (ep.First != gs.Episode || ep.Last != gs.Episode2 || ep.Part > 0) {
		what += " (" + ep.String() + " on TMDB)"
	}
	if fd.remembered {
		return "TMDB: " + what + " (as matched before; check it with Edit)", m
	}
	return "TMDB: " + what + " (check it with Edit)", m
}

// setRawField changes one value in a queued file's settings as the page sees them.
func setRawField(it *queueItem, k string, v any) {
	var m map[string]any
	if json.Unmarshal(it.Raw, &m) != nil || m == nil {
		m = map[string]any{}
	}
	m[k] = v
	it.Raw, _ = json.Marshal(m)
}
