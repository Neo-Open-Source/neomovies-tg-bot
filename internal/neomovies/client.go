package neomovies

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	apiBase string
	hc      *http.Client
}

func NewClient(apiBase string) *Client {
	return &Client{
		apiBase: strings.TrimRight(apiBase, "/"),
		hc:      &http.Client{Timeout: 9 * time.Second},
	}
}

type SearchResponse struct {
	Page        int     `json:"page"`
	Results     []Movie `json:"results"`
	TotalPages  int     `json:"total_pages"`
	TotalResult int     `json:"total_results"`
}

type Movie struct {
	ID               any    `json:"id"`
	Title            string `json:"title"`
	Name             string `json:"name"`
	NameRu           string `json:"nameRu"`
	NameOriginal     string `json:"nameOriginal"`
	Overview         string `json:"overview"`
	Description      string `json:"description"`
	ShortDescription string `json:"shortDescription"`
	PosterPath       string `json:"poster_path"`
	PosterURL        string `json:"posterUrl"`
	PosterURLPreview string `json:"posterUrlPreview"`
	ReleaseDate      string `json:"release_date"`
	Year             string `json:"year"`
	Genres           []struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"genres"`
	VoteAverage     float64 `json:"vote_average"`
	Rating          float64 `json:"rating"`
	RatingKinopoisk float64 `json:"ratingKinopoisk"`
	KinopoiskID     int     `json:"kinopoisk_id"`
	ExternalIDs     struct {
		KP   int    `json:"kp"`
		IMDB string `json:"imdb"`
	} `json:"externalIds"`
}

func (c *Client) SearchMovies(ctx context.Context, query string, page int) (*SearchResponse, error) {
	u, _ := url.Parse(c.apiBase + "/api/v1/movies/search")
	q := u.Query()
	q.Set("query", query)
	q.Set("page", strconv.Itoa(page))
	q.Set("lang", "ru")
	u.RawQuery = q.Encode()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("neomovies search status %d: %s", resp.StatusCode, string(body))
	}

	var wrapper struct {
		Success bool           `json:"success"`
		Data    SearchResponse `json:"data"`
	}
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&wrapper); err == nil && (wrapper.Data.Results != nil || wrapper.Data.TotalPages != 0) {
		return &wrapper.Data, nil
	}

	_ = resp.Body.Close()
	req2, _ := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	resp2, err := c.hc.Do(req2)
	if err != nil {
		return nil, err
	}
	defer resp2.Body.Close()
	var direct SearchResponse
	if err := json.NewDecoder(resp2.Body).Decode(&direct); err != nil {
		return nil, err
	}
	return &direct, nil
}

func (c *Client) GetMovieByKPID(ctx context.Context, kpID int) (*Movie, error) {
	if kpID <= 0 {
		return nil, fmt.Errorf("invalid kp_id")
	}
	u := fmt.Sprintf("%s/api/v1/movie/kp_%d", c.apiBase, kpID)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("neomovies getMovie status %d: %s", resp.StatusCode, string(body))
	}

	var wrapper struct {
		Success bool  `json:"success"`
		Data    Movie `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&wrapper); err == nil && (wrapper.Data.KinopoiskID != 0 || wrapper.Data.ExternalIDs.KP != 0) {
		return &wrapper.Data, nil
	}

	_ = resp.Body.Close()
	req2, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	resp2, err := c.hc.Do(req2)
	if err != nil {
		return nil, err
	}
	defer resp2.Body.Close()
	var direct Movie
	if err := json.NewDecoder(resp2.Body).Decode(&direct); err != nil {
		return nil, err
	}
	return &direct, nil
}

type TorrentResult struct {
	Title   string   `json:"title"`
	Quality string   `json:"quality"`
	Voice   []string `json:"voice"`
	Types   []string `json:"types"`
	Seasons []int    `json:"seasons"`
}

func (c *Client) GetTorrentsByIMDB(ctx context.Context, imdbID string, typ string) ([]TorrentResult, error) {
	imdbID = strings.TrimSpace(imdbID)
	if imdbID == "" {
		return nil, fmt.Errorf("imdb_id is empty")
	}
	if typ != "tv" {
		typ = "movie"
	}
	u := fmt.Sprintf("%s/api/v1/torrents/search/%s?type=%s", c.apiBase, url.PathEscape(imdbID), url.QueryEscape(typ))
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("neomovies torrents status %d: %s", resp.StatusCode, string(body))
	}

	var direct []TorrentResult
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&direct); err == nil {
		return direct, nil
	}

	_ = resp.Body.Close()
	req2, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	resp2, err := c.hc.Do(req2)
	if err != nil {
		return nil, err
	}
	defer resp2.Body.Close()
	var wrapper struct {
		Success bool `json:"success"`
		Data    struct {
			Results []TorrentResult `json:"results"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&wrapper); err != nil {
		return nil, err
	}
	return wrapper.Data.Results, nil
}

func UniqueVoices(torrents []TorrentResult) []string {
	set := map[string]struct{}{}
	for _, t := range torrents {
		for _, v := range t.Voice {
			v = strings.TrimSpace(v)
			if v == "" {
				continue
			}
			set[v] = struct{}{}
		}
	}
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for v := range set {
		out = append(out, v)
	}
	sortStrings(out)
	return out
}

func sortStrings(s []string) {
	for i := 0; i < len(s); i++ {
		for j := i + 1; j < len(s); j++ {
			if s[j] < s[i] {
				s[i], s[j] = s[j], s[i]
			}
		}
	}
}

var kpPosterRe = regexp.MustCompile(`kinopoiskapiunofficial\\.tech/images/posters/(kp|kp_small|kp_big)/(\\d+)\\.jpg`)

func (c *Client) ImageURL(path string, kpType string, kpID int) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "http") {
		m := kpPosterRe.FindStringSubmatch(path)
		if len(m) == 3 {
			return fmt.Sprintf("%s/api/v1/images/%s/%s?fallback=true", c.apiBase, m[1], m[2])
		}
		return path
	}
	if kpID > 0 {
		return fmt.Sprintf("%s/api/v1/images/%s/%d?fallback=true", c.apiBase, kpType, kpID)
	}
	return path
}

func (c *Client) PlayerRedirectURL(provider string, idType string, id int) string {
	base := strings.TrimRight(osGetenv("PUBLIC_BASE_URL"), "/")
	if base == "" {
		return fmt.Sprintf("%s/api/v1/players/%s/%s/%d", c.apiBase, provider, idType, id)
	}
	return fmt.Sprintf("%s/api/player?provider=%s&idType=%s&id=%d", base, url.QueryEscape(provider), url.QueryEscape(idType), id)
}

func osGetenv(key string) string {
	return strings.TrimSpace(getenv(key))
}

var getenv = func(k string) string { return "" }
