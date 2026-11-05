package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Telling Plex when files are ready: after a queue run, Plex is asked to scan
// the library folders that hold the new files, so they show up straight away
// instead of at Plex's next scheduled scan. Set up in Settings with the Plex
// server's address and token (the token is kept in Windows' Credential
// Manager, like the TMDB key). A failed refresh is a note, never an error.

const plexCredName = "Keep the Light/Plex token"

func (g *guiServer) plexServer() (base, token string) {
	var u string
	_ = json.Unmarshal(g.loadPrefs()["plexURL"], &u)
	if t, err := readSecret(plexCredName); err == nil {
		token = strings.TrimSpace(t)
	}
	if token == "" && !secretStoreOK() { // no Credential Manager (not Windows): the settings file
		_ = json.Unmarshal(g.loadPrefs()["plexToken"], &token)
	}
	return normPlexURL(u), token
}

// normPlexURL accepts "192.168.1.10", "192.168.1.10:32400" or a full address.
func normPlexURL(u string) string {
	u = strings.TrimRight(strings.TrimSpace(u), "/")
	if u == "" {
		return ""
	}
	if !strings.Contains(u, "://") {
		u = "http://" + u
	}
	if p, err := url.Parse(u); err == nil && p.Port() == "" && p.Path == "" {
		u += ":32400"
	}
	return u
}

func (g *guiServer) setPlexServer(u, token string) error {
	raw, _ := json.Marshal(strings.TrimSpace(u))
	if err := g.savePref("plexURL", raw); err != nil {
		return err
	}
	token = strings.TrimSpace(token)
	if secretStoreOK() {
		if token == "" {
			return deleteSecret(plexCredName)
		}
		return writeSecret(plexCredName, token)
	}
	t, _ := json.Marshal(token)
	return g.savePref("plexToken", t)
}

var plexHTTP = &http.Client{Timeout: 20 * time.Second}

func plexGet(ctx context.Context, base, token, path string, q url.Values, out any) error {
	u := base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Plex-Token", token)
	req.Header.Set("X-Plex-Product", "Keep the Light")
	req.Header.Set("X-Plex-Client-Identifier", "keep-the-light")
	resp, err := plexHTTP.Do(req)
	if err != nil {
		return fmt.Errorf("Plex could not be reached at %s (%v)", base, err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return fmt.Errorf("Plex did not accept the token")
	case resp.StatusCode >= 300:
		return fmt.Errorf("Plex answered %s", resp.Status)
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(out)
}

type plexSection struct {
	Key       string
	Title     string
	Type      string
	Locations []string
}

func plexSections(ctx context.Context, base, token string) ([]plexSection, error) {
	var r struct {
		MediaContainer struct {
			Directory []struct {
				Key, Title, Type string
				Location         []struct{ Path string }
			}
		}
	}
	if err := plexGet(ctx, base, token, "/library/sections", nil, &r); err != nil {
		return nil, err
	}
	var out []plexSection
	for _, d := range r.MediaContainer.Directory {
		s := plexSection{Key: d.Key, Title: d.Title, Type: d.Type}
		for _, l := range d.Location {
			s.Locations = append(s.Locations, l.Path)
		}
		out = append(out, s)
	}
	return out, nil
}

// pathParts splits a Windows, network or Linux path into lower-case folder names.
func pathParts(p string) []string {
	f := strings.FieldsFunc(strings.ToLower(p), func(r rune) bool { return r == '\\' || r == '/' })
	if len(f) > 0 && strings.HasSuffix(f[0], ":") {
		f = f[1:] // a drive letter says nothing about which folder it is
	}
	return f
}

type plexScan struct {
	Key, Title, Path string // Path: the folder to scan, as Plex sees it ("" = the whole library)
}

// plexScansFor works out which libraries hold the new files. This PC and the
// Plex server usually see the same folders by different paths ("W:\Movies\Heat"
// here, "D:\PlexRaid01\Movies\Heat" there), so a library matches when the end
// of one of its folders ("PlexRaid01\Movies") appears in the file's path; the
// longest such match wins. The folder to scan is then given as Plex sees it.
func plexScansFor(sections []plexSection, files []string) []plexScan {
	seen := map[string]bool{}
	var out []plexScan
	for _, f := range files {
		dir := pathParts(anyDir(f))
		best, bestN := []plexScan{}, 0
		for _, s := range sections {
			for _, loc := range s.Locations {
				lp := pathParts(loc)
				for n := len(lp); n >= 1; n-- { // the longest end of the library's folder found in the file's path
					tail := lp[len(lp)-n:]
					at := indexParts(dir, tail)
					if at < 0 {
						continue
					}
					sep := "\\"
					if strings.HasPrefix(loc, "/") {
						sep = "/"
					}
					rest := dir[at+n:]
					p := strings.TrimRight(loc, `\/`)
					if len(rest) > 0 {
						// the original spelling of the rest, from the file's own path
						orig := strings.FieldsFunc(anyDir(f), func(r rune) bool { return r == '\\' || r == '/' })
						p += sep + strings.Join(orig[len(orig)-len(rest):], sep)
					}
					sc := plexScan{Key: s.Key, Title: s.Title, Path: p}
					switch {
					case n > bestN:
						best, bestN = []plexScan{sc}, n
					case n == bestN:
						best = append(best, sc)
					}
					break
				}
			}
		}
		for _, sc := range best {
			k := sc.Key + "|" + sc.Path
			if !seen[k] {
				seen[k] = true
				out = append(out, sc)
			}
		}
	}
	return out
}

// anyDir is a file's folder, whichever slash the path uses (the window runs on
// Windows, but the paths may be network or Linux paths).
func anyDir(p string) string {
	if i := strings.LastIndexAny(p, `\/`); i >= 0 {
		return p[:i]
	}
	return ""
}

func indexParts(hay, needle []string) int {
	for i := 0; i+len(needle) <= len(hay); i++ {
		match := true
		for j := range needle {
			if hay[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// plexRefresh asks Plex to scan the folders of the new files. It returns a
// short note for the window and the log.
func (g *guiServer) plexRefresh(ctx context.Context, files []string) string {
	base, token := g.plexServer()
	if base == "" || token == "" || len(files) == 0 {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	secs, err := plexSections(ctx, base, token)
	if err != nil {
		return "Plex was not refreshed: " + err.Error()
	}
	scans := plexScansFor(secs, files)
	if len(scans) == 0 {
		return "Plex was not refreshed: none of its libraries holds the new files' folders"
	}
	var titles []string
	for _, sc := range scans {
		q := url.Values{}
		if sc.Path != "" {
			q.Set("path", sc.Path)
		}
		if err := plexGet(ctx, base, token, "/library/sections/"+url.PathEscape(sc.Key)+"/refresh", q, nil); err != nil {
			return "Plex was not refreshed: " + err.Error()
		}
		if !containsStr(titles, sc.Title) {
			titles = append(titles, sc.Title)
		}
	}
	return "Plex is scanning the new files (" + strings.Join(titles, ", ") + ")"
}

func containsStr(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// plexCheck: is the server there, and does it take the token?
func plexCheck(ctx context.Context, base, token string) (string, error) {
	var r struct {
		MediaContainer struct {
			FriendlyName string `json:"friendlyName"`
		}
	}
	if err := plexGet(ctx, base, token, "/", nil, &r); err != nil {
		return "", err
	}
	secs, err := plexSections(ctx, base, token)
	if err != nil {
		return "", err
	}
	var names []string
	for _, s := range secs {
		names = append(names, s.Title)
	}
	name := r.MediaContainer.FriendlyName
	if name == "" {
		name = base
	}
	return fmt.Sprintf("Connected to %s: %d libraries (%s).", name, len(secs), strings.Join(names, ", ")), nil
}
