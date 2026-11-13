package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

// ---------- TVmaze: a second opinion on episode numbers ----------
//
// TMDB stays the main source. For a season of a show on the Plex names tab,
// Keep the Light also asks TVmaze (free, no key; its data is CC BY-SA, credited
// in the About box) how it numbers the episodes. TVmaze usually follows the
// order the episodes were first shown in, which is often (not always) TVDB's,
// the other numbering Plex can use. When the two disagree (a double episode,
// a split one, another order), both are shown and the owner picks; a season
// set to "TVmaze's numbering" takes its episode titles from TVmaze, by the
// files' own numbers. Without TVmaze (offline, busy, show not found), TMDB
// alone is used, as before.

var tvmazeAPI = "https://api.tvmaze.com"

var tvmazeClient = &http.Client{Timeout: 20 * time.Second}

type tvmazeEp struct {
	Season  int     `json:"season"`
	Number  int     `json:"number"` // 0 for a special
	Name    string  `json:"name"`
	Airdate string  `json:"airdate"`
	Runtime float64 `json:"runtime"`
	Summary string  `json:"summary"` // HTML
}

// tvmaze keeps what has been asked (shows and their episodes), and paces the
// questions: TVmaze allows at least 20 every 10 seconds.
var tvmaze = struct {
	mu    sync.Mutex
	calls []time.Time
	shows map[string]int       // "tmdb:<id>" or "name:<query>" → TVmaze's show number (0: not found)
	eps   map[int][]tvmazeEp   // show number → its episodes
	at    map[string]time.Time // when a show was looked up, so "not found" is asked again later
}{shows: map[string]int{}, eps: map[int][]tvmazeEp{}, at: map[string]time.Time{}}

// tvmazeGet fetches one API path into out. found=false: TVmaze has no such entry.
func tvmazeGet(ctx context.Context, path string, out any) (found bool, err error) {
	for try := 0; ; try++ {
		tvmazePace(ctx)
		req, err := http.NewRequestWithContext(ctx, "GET", tvmazeAPI+path, nil)
		if err != nil {
			return false, err
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "Keep the Light (Plex file names)")
		resp, err := tvmazeClient.Do(req) // a show lookup answers with a redirect to the show, which is followed
		if err != nil {
			return false, fmt.Errorf("could not reach TVmaze: %v", shortNetErr(err))
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		resp.Body.Close()
		switch {
		case resp.StatusCode == http.StatusNotFound:
			return false, nil
		case resp.StatusCode == http.StatusTooManyRequests && try < 3: // busy: wait and ask again
			select {
			case <-ctx.Done():
				return false, ctx.Err()
			case <-time.After(time.Duration(2<<try) * time.Second):
			}
			continue
		case resp.StatusCode != http.StatusOK:
			return false, fmt.Errorf("TVmaze answered %s", resp.Status)
		}
		return true, json.Unmarshal(body, out)
	}
}

// tvmazePace keeps to 15 questions in any 10 seconds.
func tvmazePace(ctx context.Context) {
	for {
		tvmaze.mu.Lock()
		now := time.Now()
		keep := tvmaze.calls[:0]
		for _, t := range tvmaze.calls {
			if now.Sub(t) < 10*time.Second {
				keep = append(keep, t)
			}
		}
		tvmaze.calls = keep
		if len(keep) < 15 {
			tvmaze.calls = append(tvmaze.calls, now)
			tvmaze.mu.Unlock()
			return
		}
		wait := 10*time.Second - now.Sub(keep[0])
		tvmaze.mu.Unlock()
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

// tvmazeShow finds a show on TVmaze: by its TVDB or IMDb number (from TMDB),
// else by name. 0 = not found.
func tvmazeShow(ctx context.Context, tmdbKey string, tmdbID int, name string) (int, error) {
	k := "name:" + strings.ToLower(name)
	if tmdbID != 0 {
		k = fmt.Sprintf("tmdb:%d", tmdbID)
	}
	tvmaze.mu.Lock()
	id, ok := tvmaze.shows[k]
	fresh := time.Since(tvmaze.at[k]) < 30*time.Minute
	tvmaze.mu.Unlock()
	if ok && (id != 0 || fresh) {
		return id, nil
	}
	var show struct {
		ID int `json:"id"`
	}
	found := false
	if tmdbID != 0 && tmdbKey != "" {
		var ext struct {
			TVDB int    `json:"tvdb_id"`
			IMDb string `json:"imdb_id"`
		}
		if tmdbGet(ctx, tmdbKey, fmt.Sprintf("/tv/%d/external_ids", tmdbID), nil, &ext) == nil {
			var err error
			if ext.TVDB != 0 {
				if found, err = tvmazeGet(ctx, fmt.Sprintf("/lookup/shows?thetvdb=%d", ext.TVDB), &show); err != nil {
					return 0, err
				}
			}
			if !found && ext.IMDb != "" {
				if found, err = tvmazeGet(ctx, "/lookup/shows?imdb="+url.QueryEscape(ext.IMDb), &show); err != nil {
					return 0, err
				}
			}
		}
	}
	if !found && name != "" {
		var err error
		if found, err = tvmazeGet(ctx, "/singlesearch/shows?q="+url.QueryEscape(name), &show); err != nil {
			return 0, err
		}
	}
	if !found {
		show.ID = 0
	}
	tvmaze.mu.Lock()
	tvmaze.shows[k], tvmaze.at[k] = show.ID, time.Now()
	tvmaze.mu.Unlock()
	return show.ID, nil
}

// tvmazeSeason lists a season's episodes on TVmaze (specials left out), in order.
func tvmazeSeason(ctx context.Context, show, season int) ([]tvmazeEp, error) {
	tvmaze.mu.Lock()
	all, ok := tvmaze.eps[show]
	tvmaze.mu.Unlock()
	if !ok {
		if found, err := tvmazeGet(ctx, fmt.Sprintf("/shows/%d/episodes", show), &all); err != nil {
			return nil, err
		} else if !found {
			all = nil
		}
		tvmaze.mu.Lock()
		tvmaze.eps[show] = all
		tvmaze.mu.Unlock()
	}
	var out []tvmazeEp
	for _, e := range all {
		if e.Season == season && e.Number > 0 {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Number < out[b].Number })
	return out, nil
}

var reTags = regexp.MustCompile(`<[^>]+>`)

// plainText turns TVmaze's HTML summary into text.
func plainText(s string) string {
	return strings.Join(strings.Fields(html.UnescapeString(reTags.ReplaceAllString(s, " "))), " ")
}

// ---------- comparing the two ----------

// numberingRow is one episode number as TMDB and TVmaze have it.
type numberingRow struct {
	N      int    `json:"n"`
	TMDB   string `json:"tmdb"`
	TVmaze string `json:"tvmaze"`
	File   bool   `json:"file"` // one of the files has this number
	Differ bool   `json:"differ"`
}

// numberingSuggestion: a season TMDB and TVmaze number differently.
type numberingSuggestion struct {
	Key     string         `json:"key"` // shiftKey
	Season  int            `json:"season"`
	Show    string         `json:"show"`
	TMDB    int            `json:"tmdb"`   // episodes in the season
	TVmaze  int            `json:"tvmaze"` // episodes in the season
	Summary string         `json:"summary"`
	Rows    []numberingRow `json:"rows"`
}

// titleKey is a title reduced for comparing ("Part 1", punctuation and case left out).
func titleKey(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(baseTitle(s)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// sameTitle: the two names are the same episode's (one may be a shortened or
// longer form of the other: "The Army" / "The Army, Part 1").
func sameTitle(a, b string) bool {
	ka, kb := titleKey(a), titleKey(b)
	if ka == "" || kb == "" {
		return true // an unnamed episode ("Episode 9") says nothing
	}
	return ka == kb || (len(ka) >= 6 && strings.Contains(kb, ka)) || (len(kb) >= 6 && strings.Contains(ka, kb))
}

var reGenericTitle = regexp.MustCompile(`(?i)^(episode|chapter|part)\s*#?\s*\d+$`)

// compareNumbering reports whether TMDB and TVmaze number a season differently:
// a different number of episodes, or titles that do not match at two or more
// numbers. files are the episode numbers the files have.
func compareNumbering(tmdb []tmdbEpisode, tvm []tvmazeEp, files map[int]bool) (bool, []numberingRow) {
	if len(tmdb) == 0 || len(tvm) == 0 {
		return false, nil
	}
	a, b := map[int]string{}, map[int]string{}
	hi := 0
	for _, e := range tmdb {
		a[e.Number] = e.Name
		hi = max(hi, e.Number)
	}
	for _, e := range tvm {
		b[e.Number] = e.Name
		hi = max(hi, e.Number)
	}
	var rows []numberingRow
	differ := 0
	for n := 1; n <= hi; n++ {
		x, y := a[n], b[n]
		d := false
		switch {
		case x == "" && y == "":
			continue
		case x == "" || y == "":
			d = true
		case reGenericTitle.MatchString(x) || reGenericTitle.MatchString(y):
		default:
			d = !sameTitle(x, y)
		}
		if d {
			differ++
		}
		rows = append(rows, numberingRow{N: n, TMDB: x, TVmaze: y, File: files[n], Differ: d})
	}
	return len(tmdb) != len(tvm) || differ >= 2, rows
}

// tvmazeTitle is the title of the file's episode(s) by TVmaze's numbering
// (joined for a double: "First / Second", or "Title (Parts 1 & 2)").
func tvmazeTitle(eps []tvmazeEp, first, last int) (title, airdate, summary string, ok bool) {
	var names, sums []string
	for n := first; n <= max(first, last); n++ {
		var e *tvmazeEp
		for i := range eps {
			if eps[i].Number == n {
				e = &eps[i]
			}
		}
		if e == nil {
			return "", "", "", false
		}
		if airdate == "" {
			airdate = e.Airdate
		}
		names = append(names, e.Name)
		if s := plainText(e.Summary); s != "" {
			sums = append(sums, s)
		}
	}
	if len(names) == 1 {
		return names[0], airdate, strings.Join(sums, " "), true
	}
	return joinTitles(names), airdate, strings.Join(sums, " "), true
}

// tvmazeFill fills in a file of a season set to TVmaze's numbering: the show
// (name, year, poster) from TMDB, the episode (title, date, summary) from
// TVmaze by the file's own number. nil (with why) when TVmaze cannot help, and
// TMDB's numbering is used instead.
func tvmazeFill(ctx context.Context, key string, hit *tmdbResult, season int, ep tmdbEp, coverDir string) (*tmdbFill, string) {
	show, err := tvmazeShow(ctx, key, hit.ID, hit.Title)
	if err != nil || show == 0 {
		return nil, "TVmaze not reachable or show not found there: TMDB's numbering used"
	}
	eps, err := tvmazeSeason(ctx, show, season)
	if err != nil {
		return nil, "TVmaze not reachable: TMDB's numbering used"
	}
	title, air, summary, ok := tvmazeTitle(eps, ep.First, ep.Last)
	if !ok {
		return nil, fmt.Sprintf("TVmaze has no %s in season %d: TMDB's numbering used", ep, season)
	}
	f, err := tmdbDetails(ctx, key, "tv", hit.ID, season, 0, coverDir)
	if err != nil {
		return nil, ""
	}
	name, year := f.Title, yearOf(f.Date)
	nums := fmt.Sprintf("S%02dE%02d", season, ep.First)
	if ep.Last > ep.First {
		nums += fmt.Sprintf("-E%02d", ep.Last)
	}
	f.Title = name + " – " + nums
	if title != "" {
		f.Title += " – " + title
	}
	f.Date, f.Description = air, summary
	plexTitle := title
	if ep.Last > ep.First && strings.Contains(title, " (Parts ") {
		plexTitle = baseTitle(title)
	}
	f.PlexName = plexEpisodeRangeName(name, year, season, ep, plexTitle)
	return f, ""
}

// ---------- the choice, kept per show and season ----------

// tvNumbering is the seasons set to a numbering ("tvmaze", or "tmdb" when TMDB's
// was kept after a disagreement), by shiftKey; remembered between sessions.
func (g *guiServer) tvNumbering() map[string]string {
	m := map[string]string{}
	_ = json.Unmarshal(g.loadPrefs()["tvNumbering"], &m)
	return m
}

func (g *guiServer) setTVNumbering(key, source string) {
	m := g.tvNumbering()
	if len(m) >= 1000 {
		m = map[string]string{}
	}
	m[key] = source
	if b, err := json.Marshal(m); err == nil {
		_ = g.savePref("tvNumbering", b)
	}
}

// tvmazeOn: the second opinion is wanted (a setting, on unless turned off).
func (g *guiServer) tvmazeOn() bool {
	var off bool
	_ = json.Unmarshal(g.loadPrefs()["tvmazeOff"], &off)
	return !off
}

// tvmazeLengthFactor: a TVmaze episode about twice as long as the season's
// usual one is a double (2); otherwise 0 (as normal).
func tvmazeLengthFactor(eps []tvmazeEp, n int) float64 {
	var rts []float64
	this := 0.0
	for _, e := range eps {
		if e.Runtime > 0 {
			rts = append(rts, e.Runtime)
		}
		if e.Number == n {
			this = e.Runtime
		}
	}
	if this == 0 || len(rts) < 3 {
		return 0
	}
	sort.Float64s(rts)
	if med := rts[len(rts)/2]; this >= 1.7*med {
		return 2
	}
	return 0
}

// ---------- TVmaze on its own: search, details, and the fallback ----------
//
// When TMDB does not have a show (or has nothing that fits), TVmaze can fill
// a file in by itself: its results are offered beside TMDB's in the searches
// (kind "tvmaze"), and a file whose show TMDB cannot find is looked up there.

type tvmazeShowInfo struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Premiered string `json:"premiered"`
	Summary   string `json:"summary"`
	URL       string `json:"url"`
	Image     *struct {
		Medium   string `json:"medium"`
		Original string `json:"original"`
	} `json:"image"`
}

// tvmazeSearch lists shows TVmaze has by a name (at most 8).
func tvmazeSearch(ctx context.Context, query string) ([]tmdbResult, error) {
	var hits []struct {
		Show tvmazeShowInfo `json:"show"`
	}
	if _, err := tvmazeGet(ctx, "/search/shows?q="+url.QueryEscape(query), &hits); err != nil {
		return nil, err
	}
	var out []tmdbResult
	for _, h := range hits {
		if len(out) == 8 {
			break
		}
		it := tmdbResult{ID: h.Show.ID, Kind: "tvmaze", Title: h.Show.Name, Year: yearOf(h.Show.Premiered), Overview: plainText(h.Show.Summary)}
		if h.Show.Image != nil && h.Show.Image.Medium != "" {
			if b, mime, err := tvmazeImage(ctx, h.Show.Image.Medium, 1<<20); err == nil {
				it.Thumb = "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(b)
			}
		}
		out = append(out, it)
	}
	return out, nil
}

// tvmazeImage downloads a poster from TVmaze.
func tvmazeImage(ctx context.Context, u string, limit int64) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := tvmazeClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("%s", resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	mime := resp.Header.Get("Content-Type")
	if mime == "" {
		mime = "image/jpeg"
	}
	return b, mime, err
}

// tvmazeDetails fills a file in from TVmaze alone: the show, or one episode
// (or two, for a double) of it, with the show's poster saved to coverDir.
func tvmazeDetails(ctx context.Context, show, season int, ep tmdbEp, coverDir string) (*tmdbFill, error) {
	var s tvmazeShowInfo
	found, err := tvmazeGet(ctx, fmt.Sprintf("/shows/%d", show), &s)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("TVmaze has no such show")
	}
	f := &tmdbFill{Title: s.Name, Date: s.Premiered, Description: plainText(s.Summary), Link: s.URL}
	if season > 0 && ep.First > 0 {
		eps, err := tvmazeSeason(ctx, show, season)
		if err != nil {
			return nil, err
		}
		title, air, summary, ok := tvmazeTitle(eps, ep.First, ep.Last)
		if !ok {
			return nil, fmt.Errorf("TVmaze has no %s in season %d of %s", ep, season, s.Name)
		}
		nums := fmt.Sprintf("S%02dE%02d", season, ep.First)
		if ep.Last > ep.First {
			nums += fmt.Sprintf("-E%02d", ep.Last)
		}
		f.Title = s.Name + " – " + nums
		if title != "" {
			f.Title += " – " + title
		}
		f.Date, f.Description = air, summary
		plexTitle := title
		if ep.Last > ep.First && strings.Contains(title, " (Parts ") {
			plexTitle = baseTitle(title)
		}
		f.PlexName = plexEpisodeRangeName(s.Name, yearOf(s.Premiered), season, ep, plexTitle)
	}
	if s.Image != nil && s.Image.Original != "" && coverDir != "" {
		if b, mime, err := tvmazeImage(ctx, s.Image.Original, 15<<20); err == nil {
			ext := ".jpg"
			if strings.Contains(mime, "png") {
				ext = ".png"
			}
			name := filepath.Join(coverDir, fmt.Sprintf("tmdb-tvmaze-%d%s", show, ext)) // "tmdb-": cleaned up with the others
			if err = os.MkdirAll(coverDir, 0o755); err == nil {
				err = os.WriteFile(name, b, 0o644)
			}
			if err == nil {
				f.Cover = name
			}
		}
		if s.Image.Medium != "" {
			if b, mime, err := tvmazeImage(ctx, s.Image.Medium, 1<<20); err == nil {
				f.Thumb = "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(b)
			}
		}
	}
	return f, nil
}
