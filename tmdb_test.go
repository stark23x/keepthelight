package main

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGuessSearch(t *testing.T) {
	cases := []struct {
		path string
		want tmdbGuess
	}{
		{`D:\Movies\Dune.Part.Two.2024.2160p.UHD.BluRay.REMUX.DV.HDR.HEVC.TrueHD.Atmos_KTL.mkv`, tmdbGuess{"movie", "Dune Part Two", "2024", 0, 0}},
		{`D:\Movies\Blade Runner 2049 (2017).mkv`, tmdbGuess{"movie", "Blade Runner 2049", "2017", 0, 0}},
		{`D:\Movies\1917 (2019) [2160p].mkv`, tmdbGuess{"movie", "1917", "2019", 0, 0}},
		{`D:\Movies\Heat (1995)\movie.mkv`, tmdbGuess{"movie", "Heat", "1995", 0, 0}},
		{`D:\Movies\Alien.Remastered.mkv`, tmdbGuess{"movie", "Alien", "", 0, 0}},
		{`D:\TV\The Bear\Season 2\The.Bear.S02E03.Sundae.2160p.WEB.mkv`, tmdbGuess{"tv", "The Bear", "", 2, 3}},
		{`D:\TV\Doctor Who (2005)\Doctor Who 2005 1x05 Dalek.mkv`, tmdbGuess{"tv", "Doctor Who", "", 1, 5}},
		{`D:\TV\Severance\S01E01.mkv`, tmdbGuess{"tv", "Severance", "", 1, 1}},
		{`D:\Movies\Inception {tmdb-27205} (2010)_Plex 4K.mkv`, tmdbGuess{"movie", "Inception", "2010", 0, 0}},
	}
	for _, c := range cases {
		p := strings.ReplaceAll(c.path, `\`, string(filepath.Separator))
		if got := guessSearch(p, "_Plex 4K"); got != c.want {
			t.Errorf("%s:\n got %+v\nwant %+v", c.path, got, c.want)
		}
	}
}

func TestTMDB(t *testing.T) {
	jpeg := []byte("\xff\xd8\xff\xe0\x00\x10JFIF\x00poster")
	var auth []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = append(auth, r.URL.Query().Get("api_key")+"|"+r.Header.Get("Authorization"))
		if r.URL.Query().Get("api_key") == "bad" {
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"status_message":"Invalid API key"}`))
			return
		}
		if strings.HasPrefix(r.URL.Path, "/3/") && r.URL.Query().Get("language") != "en-US" {
			t.Errorf("language: %s", r.URL)
		}
		switch r.URL.Path {
		case "/3/search/movie":
			if r.URL.Query().Get("query") != "Heat" || r.URL.Query().Get("year") != "1995" {
				t.Errorf("search: %s", r.URL)
			}
			_, _ = w.Write([]byte(`{"results":[{"id":949,"title":"Heat","release_date":"1995-12-15","overview":"Cops and robbers.","poster_path":"/p.jpg"},{"id":2,"title":"Heat 2","release_date":""}]}`))
		case "/3/movie/949":
			_, _ = w.Write([]byte(`{"title":"Heat","release_date":"1995-12-15","overview":"Cops and robbers.","poster_path":"/p.jpg"}`))
		case "/3/tv/136315":
			_, _ = w.Write([]byte(`{"name":"The Bear","first_air_date":"2022-06-23","overview":"A chef.","poster_path":"/show.jpg"}`))
		case "/3/tv/136315/season/2":
			_, _ = w.Write([]byte(`{"poster_path":"/s2.jpg"}`))
		case "/3/tv/136315/season/2/episode/3":
			_, _ = w.Write([]byte(`{"name":"Sundae","air_date":"2023-06-22","overview":"Sydney eats."}`))
		case "/img/w92/p.jpg", "/img/w780/p.jpg", "/img/w780/s2.jpg", "/img/w92/s2.jpg":
			_, _ = w.Write(jpeg)
		default:
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"status_message":"The resource you requested could not be found."}`))
		}
	}))
	defer srv.Close()
	oldAPI, oldImg := tmdbAPI, tmdbImages
	tmdbAPI, tmdbImages = srv.URL+"/3", srv.URL+"/img/"
	defer func() { tmdbAPI, tmdbImages = oldAPI, oldImg }()
	ctx := context.Background()

	res, err := tmdbSearch(ctx, "k3y", "movie", "Heat", "1995")
	if err != nil || len(res) != 2 || res[0].Year != "1995" || res[0].Thumb != "data:image/jpeg;base64,"+base64.StdEncoding.EncodeToString(jpeg) || res[1].Thumb != "" {
		t.Fatalf("%+v %v", res, err)
	}
	dir := t.TempDir()
	f, err := tmdbDetails(ctx, "eyJtoken", "movie", 949, 0, 0, dir)
	if err != nil || f.Title != "Heat" || f.Date != "1995-12-15" || f.Description != "Cops and robbers." || f.Link != "https://www.themoviedb.org/movie/949" {
		t.Fatalf("%+v %v", f, err)
	}
	if b, _ := os.ReadFile(f.Cover); string(b) != string(jpeg) || filepath.Base(f.Cover) != "tmdb-movie-949.jpg" {
		t.Errorf("cover %s", f.Cover)
	}
	if !strings.Contains(strings.Join(auth, " "), "|Bearer eyJtoken") || !strings.Contains(auth[0], "k3y|") {
		t.Errorf("auth: %v", auth)
	}
	f, err = tmdbDetails(ctx, "k", "tv", 136315, 2, 3, dir)
	if err != nil || f.Title != "The Bear – S02E03 – Sundae" || f.Date != "2023-06-22" || f.Description != "Sydney eats." ||
		filepath.Base(f.Cover) != "tmdb-tv-136315-s2.jpg" || !strings.HasSuffix(f.Link, "/tv/136315/season/2/episode/3") {
		t.Errorf("%+v %v", f, err)
	}
	if _, err := tmdbDetails(ctx, "k", "tv", 136315, 2, 99, dir); err == nil || !strings.Contains(err.Error(), "episode 99") {
		t.Errorf("missing episode: %v", err)
	}
	if _, err := tmdbSearch(ctx, "bad", "movie", "x", ""); err == nil || !strings.Contains(err.Error(), "did not accept the API key") {
		t.Errorf("bad key: %v", err)
	}
	if _, err := tmdbSearch(ctx, "", "movie", "x", ""); err == nil || !strings.Contains(err.Error(), "under Advanced") {
		t.Errorf("no key: %v", err)
	}
}
