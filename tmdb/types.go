package tmdb

// Show represents a TV show search result.
type Show struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// Season represents a TV season with its episodes.
type Season struct {
	Episodes []Episode `json:"episodes"`
}

// Episode represents a single TV episode.
type Episode struct {
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
