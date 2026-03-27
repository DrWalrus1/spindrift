package tmdb

import "strings"

// Show represents a TV show search result.
type Show struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// SeasonSummary is the season entry returned in show details.
type SeasonSummary struct {
	SeasonNumber int    `json:"season_number"`
	Name         string `json:"name"`
}

// ShowDetails holds full show metadata including the season list.
type ShowDetails struct {
	ID      int             `json:"id"`
	Name    string          `json:"name"`
	Seasons []SeasonSummary `json:"seasons"`
}

// MatchSeason returns the season number whose name best matches discTitle,
// using a case-insensitive substring search. Generic "Season N" names are
// skipped. Returns 0 if no named season matches.
func MatchSeason(discTitle string, seasons []SeasonSummary) int {
	lower := strings.ToLower(discTitle)
	bestLen, bestSeason := 0, 0
	for _, s := range seasons {
		name := strings.TrimSpace(s.Name)
		if name == "" || s.SeasonNumber == 0 {
			continue
		}
		// Skip generic "Season N" names — they won't appear in disc titles.
		lname := strings.ToLower(name)
		if strings.HasPrefix(lname, "season ") {
			continue
		}
		if strings.Contains(lower, lname) && len(name) > bestLen {
			bestLen = len(name)
			bestSeason = s.SeasonNumber
		}
	}
	return bestSeason
}

// Season represents a TV season with its episodes.
type Season struct {
	Episodes []Episode `json:"episodes"`
}

// Episode represents a single TV episode.
type Episode struct {
	ID            int    `json:"id"`
	EpisodeNumber int    `json:"episode_number"`
	Name          string `json:"name"`
	Overview      string `json:"overview"`
	Runtime       int    `json:"runtime"`
}

// Movie represents a movie search result.
type Movie struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
}

// MovieDetails represents full movie metadata.
type MovieDetails struct {
	Title    string `json:"title"`
	Runtime  int    `json:"runtime"`
	Overview string `json:"overview"`
}

// MatchStartEpisode finds the 1-based episode number where this disc begins by
// comparing disc episode durations (in seconds) against TMDB runtimes. It
// slides a window of len(discDurations) across the season and picks the
// position with the lowest total absolute error. Returns 1 if the season has
// no usable runtime data or fewer episodes than the disc.
func MatchStartEpisode(season *Season, discDurations []int) int {
	n := len(discDurations)
	if n == 0 || len(season.Episodes) < n {
		return 1
	}

	bestScore := int(^uint(0) >> 1) // max int
	bestStart := 1

	for i := 0; i <= len(season.Episodes)-n; i++ {
		score := 0
		valid := true
		for j := 0; j < n; j++ {
			tmdbSecs := season.Episodes[i+j].Runtime * 60
			if tmdbSecs == 0 {
				valid = false
				break
			}
			diff := discDurations[j] - tmdbSecs
			if diff < 0 {
				diff = -diff
			}
			score += diff
		}
		if valid && score < bestScore {
			bestScore = score
			bestStart = season.Episodes[i].EpisodeNumber
		}
	}

	return bestStart
}

// EpisodesForDisc returns the slice of episodes starting at
// startEpisode (1-indexed). If startEpisode is 0, starts from episode 1.
func EpisodesForDisc(season *Season, startEpisode, numEpisodes int) []Episode {
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
