package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

type TMDBClient struct {
	APIKey string
}

type TMDBSearchResult struct {
	Results []TMDBShow `json:"results"`
}

type TMDBShow struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type TMDBSeason struct {
	Episodes []TMDBEpisode `json:"episodes"`
}

type TMDBEpisode struct {
	EpisodeNumber int    `json:"episode_number"`
	Name          string `json:"name"`
	Overview      string `json:"overview"`
}

type TMDBMovie struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
}

type TMDBMovieSearchResult struct {
	Results []TMDBMovie `json:"results"`
}

type TMDBMovieDetails struct {
	Title    string `json:"title"`
	Runtime  int    `json:"runtime"`
	Overview string `json:"overview"`
}

type DiscInfo struct {
	ShowName string
	Season   int
	Disc     int
	IsMovie  bool
}

// DetectMovie marks the disc as a movie if no season/disc markers
// were found and only one title is present.
func (d *DiscInfo) DetectMovie(episodeCount int) {
	if d.Season == 1 && d.Disc == 1 && episodeCount == 1 {
		d.IsMovie = true
	}
}

func NewTMDBClient(apiKey string) *TMDBClient {
	return &TMDBClient{APIKey: apiKey}
}

func (c *TMDBClient) get(endpoint string) (*http.Response, error) {
	req, err := http.NewRequest("GET", tmdbBaseURL+endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Accept", "application/json")
	return http.DefaultClient.Do(req)
}

func (c *TMDBClient) SearchTV(query string) ([]TMDBShow, error) {
	resp, err := c.get("/search/tv?query=" + url.QueryEscape(query))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result TMDBSearchResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Results, nil
}

func (c *TMDBClient) GetSeason(showID, season int) (*TMDBSeason, error) {
	resp, err := c.get(fmt.Sprintf("/tv/%d/season/%d", showID, season))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var s TMDBSeason
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return nil, err
	}
	return &s, nil
}

func (c *TMDBClient) SearchMovie(query string) ([]TMDBMovie, error) {
	resp, err := c.get("/search/movie?query=" + url.QueryEscape(query))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result TMDBMovieSearchResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Results, nil
}

func (c *TMDBClient) GetMovie(movieID int) (*TMDBMovieDetails, error) {
	resp, err := c.get(fmt.Sprintf("/movie/%d", movieID))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var m TMDBMovieDetails
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return nil, err
	}
	return &m, nil
}

func ParseDiscTitle(title string) DiscInfo {
	info := DiscInfo{Season: 1, Disc: 1}

	lower := strings.ToLower(title)

	if i := strings.Index(lower, "disc "); i >= 0 {
		fmt.Sscanf(strings.TrimSpace(title[i+5:]), "%d", &info.Disc)
	}

	bookWords := map[string]int{
		"one": 1, "two": 2, "three": 3, "four": 4,
		"five": 5, "six": 6, "seven": 7, "eight": 8,
	}

	if i := strings.Index(lower, "book "); i >= 0 {
		word := strings.ToLower(strings.Trim(strings.Fields(title[i+5:])[0], ",:"))
		if n, ok := bookWords[word]; ok {
			info.Season = n
		} else {
			fmt.Sscanf(word, "%d", &info.Season)
		}
	} else if i := strings.Index(lower, "season "); i >= 0 {
		fmt.Sscanf(strings.TrimSpace(title[i+7:]), "%d", &info.Season)
	}

	for _, marker := range []string{"Book ", "Season ", "Disc "} {
		if i := strings.Index(title, marker); i > 0 {
			info.ShowName = strings.TrimSpace(title[:i])
			break
		}
	}
	if info.ShowName == "" {
		info.ShowName = title
	}

	return info
}

// EpisodesForDisc returns the slice of TMDB episodes starting at
// startEpisode (1-indexed). If startEpisode is 0, starts from episode 1.
func EpisodesForDisc(season *TMDBSeason, startEpisode, numEpisodes int) []TMDBEpisode {
	startIdx := 0
	if startEpisode > 1 {
		startIdx = startEpisode - 1
	}

	if startIdx >= len(season.Episodes) {
		return nil
	}

	end := startIdx + numEpisodes
	if end > len(season.Episodes) {
		end = len(season.Episodes)
	}

	return season.Episodes[startIdx:end]
}
