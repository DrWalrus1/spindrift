package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/DrWalrus1/spindrift/bdmv"
	"github.com/DrWalrus1/spindrift/disc"
	"github.com/DrWalrus1/spindrift/tmdb"
	"github.com/joho/godotenv"
)

const envTMDBAPIKey = "TMDB_API_KEY"

type trackResult struct {
	Playlist      string `json:"playlist"`
	Clip          string `json:"clip,omitempty"`
	Type          string `json:"type"`
	DiscTitle     string `json:"disc_title"`
	Episode       string `json:"episode,omitempty"`
	Title         string `json:"title,omitempty"`
	Duration      string `json:"duration"`
	TMDBDuration  string `json:"tmdb_duration,omitempty"`
	Chapters      int    `json:"chapters"`
	TMDBID        string `json:"tmdb_id"`
	TMDBEpisodeID string `json:"tmdb_episode_id,omitempty"`
	Note          string `json:"note,omitempty"`
}

func run(bdmvRoot string, startEpisode int, robotMode bool) error {
	d, err := disc.Open(bdmvRoot)
	if err != nil {
		return err
	}

	if !robotMode {
		// --- index.bdmv ---
		fmt.Printf("Version:    %s\n", d.Index.Version)
		fmt.Printf("FirstPlay → %s\n", d.Index.FirstPlay.PlaylistPath(bdmvRoot))
		fmt.Printf("TopMenu   → %s\n", d.Index.TopMenu.PlaylistPath(bdmvRoot))
		for i, t := range d.Index.Titles {
			if t.IsHDMV() {
				fmt.Printf("  Title[%d] → PLAYLIST/%05d.mpls\n", i, t.ObjectIDRef)
			} else {
				fmt.Printf("  Title[%d] → MovieObject[%d] (BD-J)\n", i, t.ObjectIDRef)
			}
		}

		// --- MovieObject.bdmv ---
		fmt.Printf("\nObjects (%d):\n", len(d.MObj.Objects))
		for i, obj := range d.MObj.Objects {
			fmt.Printf("  [%d] resume=%v menuMask=%v titleMask=%v cmds=%d\n",
				i, obj.ResumeIntentionFlag, obj.MenuCallMask, obj.TitleSearchMask,
				len(obj.Commands),
			)
		}

		// --- Disc metadata ---
		fmt.Printf("\nDisc Title: %s\n", d.Info.ShowName)
		if !d.Info.IsMovie {
			fmt.Printf("Season:     %d\n", d.Info.Season)
			fmt.Printf("Disc:       %d\n", d.Info.Disc)
		}
		if startEpisode > 0 {
			fmt.Printf("Start Ep:   %d\n", startEpisode)
		}
		fmt.Println()
	}

	// --- Infer episode duration bounds ---
	minDur, maxDur, clusterDur := disc.InferEpisodeBounds(bdmvRoot)
	if !robotMode {
		fmt.Printf("Inferred episode bounds: %s – %s (cluster center: %s)\n",
			bdmv.FormatDuration(minDur),
			bdmv.FormatDuration(maxDur),
			bdmv.FormatDuration(clusterDur),
		)
	}

	// --- Episode playlists ---
	episodes, err := disc.LoadEpisodePlaylists(bdmvRoot, minDur, maxDur, clusterDur)
	if err != nil {
		return fmt.Errorf("loading playlists: %w", err)
	}

	if len(episodes) == 0 {
		fmt.Println("No episodes found on disc")
		return nil
	}

	d.Info.DetectMovie(len(episodes))

	if !robotMode {
		if d.Info.IsMovie {
			fmt.Printf("Detected: Movie\n\n")
		} else {
			fmt.Printf("Found %d episodes on disc\n\n", len(episodes))
		}
	}

	// --- TMDB lookup ---
	apiKey := os.Getenv(envTMDBAPIKey)
	if apiKey == "" {
		return fmt.Errorf("%s not set — add it to .env or your environment", envTMDBAPIKey)
	}

	client := tmdb.New(apiKey)

	if d.Info.IsMovie {
		return runMovie(client, episodes, d.Info, bdmvRoot, clusterDur, robotMode)
	}
	return runTV(client, episodes, d.Info, bdmvRoot, clusterDur, startEpisode, robotMode)
}

func runMovie(
	client *tmdb.Client,
	episodes []*bdmv.Playlist,
	info disc.DiscInfo,
	bdmvRoot string,
	clusterDur int,
	robotMode bool,
) error {
	movies, matchedQuery, err := client.SmartSearchMovie(info.ShowName)
	if err != nil {
		return fmt.Errorf("TMDB movie search: %w", err)
	}
	if !robotMode && matchedQuery != "" && matchedQuery != info.ShowName {
		fmt.Printf("No results for full title; matched using %q\n", matchedQuery)
	}
	if len(movies) == 0 {
		if !robotMode {
			fmt.Printf("No TMDB movie results found for %q\n", info.ShowName)
		}
		printMovieNoTMDB(episodes, info, bdmvRoot, clusterDur, robotMode)
		return nil
	}

	movie := movies[0]
	details, err := client.GetMovie(movie.ID)
	if err != nil {
		return fmt.Errorf("TMDB movie details: %w", err)
	}

	if !robotMode {
		fmt.Printf("TMDB Match: %s (ID: %d, Runtime: %d min)\n\n",
			details.Title, movie.ID, details.Runtime)
	}

	printMovie(episodes[0], details, movie.ID, info, bdmvRoot, clusterDur, robotMode)
	return nil
}

func runTV(
	client *tmdb.Client,
	episodes []*bdmv.Playlist,
	info disc.DiscInfo,
	bdmvRoot string,
	clusterDur int,
	startEpisode int,
	robotMode bool,
) error {
	shows, matchedQuery, err := client.SmartSearchTV(info.ShowName)
	if err != nil {
		return fmt.Errorf("TMDB search: %w", err)
	}
	if !robotMode && matchedQuery != "" && matchedQuery != info.ShowName {
		fmt.Printf("No results for full title; matched using %q\n", matchedQuery)
	}
	if len(shows) == 0 {
		if !robotMode {
			fmt.Printf("No TMDB results found for %q\n", info.ShowName)
		}
		printEpisodesNoTMDB(episodes, info, bdmvRoot, clusterDur, startEpisode, robotMode)
		return nil
	}

	show := shows[0]
	if !robotMode {
		fmt.Printf("TMDB Match: %s (ID: %d)\n\n", show.Name, show.ID)
	}

	season, seasonNum, err := client.SmartGetSeason(show.ID, info.ShowName, info.Season)
	if err != nil {
		return fmt.Errorf("TMDB season fetch: %w", err)
	}
	if !robotMode && seasonNum != info.Season {
		fmt.Printf("Season matched by name: %d\n", seasonNum)
	}

	// Auto-detect which episode this disc starts at when not explicitly given.
	if startEpisode == 0 {
		mainDurations := mainEpisodeDurations(episodes, bdmvRoot, clusterDur)
		startEpisode = tmdb.MatchStartEpisode(season, mainDurations)
		if !robotMode && startEpisode > 1 {
			fmt.Printf("Auto-detected start episode: %d\n\n", startEpisode)
		}
	}

	tmdbEps := tmdb.EpisodesForDisc(season, startEpisode, len(episodes))
	info.Season = seasonNum
	printEpisodes(episodes, tmdbEps, info, show.ID, bdmvRoot, clusterDur, robotMode)
	return nil
}

// mainEpisodeDurations returns PTS-based durations (seconds) for non-commentary
// episodes, used to auto-detect the start episode from TMDB runtimes.
// EpisodeDuration sums only substantial clips (>= 60s) so intro bumpers are
// excluded, giving a duration that closely matches TMDB's runtime figures.
func mainEpisodeDurations(episodes []*bdmv.Playlist, bdmvRoot string, clusterDur int) []int {
	var out []int
	for _, pl := range episodes {
		if pl.Note != "" {
			continue
		}
		dur := pl.EpisodeDuration()
		count := disc.EstimateEpisodeCount(pl, bdmvRoot, clusterDur)
		if count > 1 && count > 0 {
			dur = dur / count
		}
		if dur > 0 {
			out = append(out, dur)
		}
	}
	return out
}

func printMovie(
	pl *bdmv.Playlist,
	details *tmdb.MovieDetails,
	tmdbID int,
	info disc.DiscInfo,
	bdmvRoot string,
	clusterDur int,
	robotMode bool,
) {
	dur := pl.EstimateDuration(bdmvRoot, disc.DefaultBitrate)
	count := disc.EstimateEpisodeCount(pl, bdmvRoot, clusterDur)

	if robotMode {
		out := []trackResult{{
			Playlist:     pl.Name,
			Clip:         pl.PrimaryClip(),
			Type:         "movie",
			DiscTitle:    info.ShowName,
			Title:        details.Title,
			Duration:     bdmv.FormatDuration(dur / count),
			TMDBDuration: bdmv.FormatDuration(details.Runtime * 60),
			Chapters:     pl.ChapterCount(),
			TMDBID:       fmt.Sprintf("%d", tmdbID),
		}}
		json.NewEncoder(os.Stdout).Encode(out)
		return
	}

	fmt.Printf("%-10s %-12s %-12s %-10s %s\n", "Type", "Playlist", "Duration", "Chapters", "Title")
	fmt.Println(strings.Repeat("-", 65))
	fmt.Printf("%-10s %-12s %-12s %-10d %s\n",
		"Movie",
		pl.Name,
		bdmv.FormatDuration(dur/count),
		pl.ChapterCount(),
		details.Title,
	)
}

func printMovieNoTMDB(
	episodes []*bdmv.Playlist,
	info disc.DiscInfo,
	bdmvRoot string,
	clusterDur int,
	robotMode bool,
) {
	if robotMode {
		out := make([]trackResult, 0, len(episodes))
		for _, pl := range episodes {
			dur := pl.EstimateDuration(bdmvRoot, disc.DefaultBitrate)
			count := disc.EstimateEpisodeCount(pl, bdmvRoot, clusterDur)
			out = append(out, trackResult{
				Playlist:  pl.Name,
				Clip:      pl.PrimaryClip(),
				Type:      "movie",
				DiscTitle: info.ShowName,
				Duration:  bdmv.FormatDuration(dur / count),
				Chapters:  pl.ChapterCount(),
			})
		}
		json.NewEncoder(os.Stdout).Encode(out)
		return
	}

	fmt.Printf("%-10s %-12s %-12s %s\n", "Type", "Playlist", "Duration", "Chapters")
	fmt.Println(strings.Repeat("-", 46))
	for _, pl := range episodes {
		dur := pl.EstimateDuration(bdmvRoot, disc.DefaultBitrate)
		count := disc.EstimateEpisodeCount(pl, bdmvRoot, clusterDur)
		fmt.Printf("%-10s %-12s %-12s %d\n",
			"Movie",
			pl.Name,
			bdmv.FormatDuration(dur/count),
			pl.ChapterCount(),
		)
	}
}

func printEpisodes(
	episodes []*bdmv.Playlist,
	tmdbEps []tmdb.Episode,
	info disc.DiscInfo,
	showID int,
	bdmvRoot string,
	clusterDur int,
	robotMode bool,
) {
	// Build clip → episode label map from main (non-commentary) episodes.
	clipEpisode := map[string]string{}
	{
		idx := 0
		for _, pl := range episodes {
			if pl.Note != "" {
				continue
			}
			label := fmt.Sprintf("S%02dE??", info.Season)
			if idx < len(tmdbEps) {
				label = fmt.Sprintf("S%02dE%02d", info.Season, tmdbEps[idx].EpisodeNumber)
			}
			clipEpisode[pl.PrimaryClip()] = label
			idx++
		}
	}

	if robotMode {
		out := make([]trackResult, 0, len(episodes))
		tmdbIdx := 0
		for _, pl := range episodes {
			dur := pl.EstimateDuration(bdmvRoot, disc.DefaultBitrate)
			count := disc.EstimateEpisodeCount(pl, bdmvRoot, clusterDur)
			note := pl.Note
			if note == "commentary" && pl.NoteClip != "" {
				if epLabel, ok := clipEpisode[pl.NoteClip]; ok {
					note = fmt.Sprintf("commentary for %s", epLabel)
				}
			}
			r := trackResult{
				Playlist:  pl.Name,
				Clip:      pl.PrimaryClip(),
				Type:      "tv",
				DiscTitle: info.ShowName,
				Episode:   fmt.Sprintf("S%02dE??", info.Season),
				Duration:  bdmv.FormatDuration(dur / count),
				Chapters:  pl.ChapterCount(),
				TMDBID:    fmt.Sprintf("%d", showID),
				Note:      note,
			}
			if pl.Note == "" && tmdbIdx < len(tmdbEps) {
				ep := tmdbEps[tmdbIdx]
				tmdbIdx++
				r.Episode = fmt.Sprintf("S%02dE%02d", info.Season, ep.EpisodeNumber)
				r.Title = ep.Name
				if ep.Runtime > 0 {
					r.TMDBDuration = bdmv.FormatDuration(ep.Runtime * 60)
				}
				if ep.ID > 0 {
					r.TMDBEpisodeID = fmt.Sprintf("%d", ep.ID)
				}
			}
			out = append(out, r)
		}
		json.NewEncoder(os.Stdout).Encode(out)
		return
	}

	fmt.Printf("%-6s %-14s %-10s %-12s %-10s %-12s %-22s %s\n",
		"Ep", "Playlist", "Clip", "Duration", "Chapters", "Episode ID", "Note", "Title")
	fmt.Println(strings.Repeat("-", 117))

	tmdbIdx := 0
	for _, pl := range episodes {
		dur := pl.EstimateDuration(bdmvRoot, disc.DefaultBitrate)
		count := disc.EstimateEpisodeCount(pl, bdmvRoot, clusterDur)

		epLabel := fmt.Sprintf("S%02dE??", info.Season)
		title := "unknown"
		episodeID := ""
		note := pl.Note
		if note == "commentary" && pl.NoteClip != "" {
			if linked, ok := clipEpisode[pl.NoteClip]; ok {
				note = fmt.Sprintf("commentary for %s", linked)
			}
		}
		if pl.Note == "" && tmdbIdx < len(tmdbEps) {
			ep := tmdbEps[tmdbIdx]
			tmdbIdx++
			epLabel = fmt.Sprintf("S%02dE%02d", info.Season, ep.EpisodeNumber)
			title = ep.Name
			if ep.ID > 0 {
				episodeID = fmt.Sprintf("%d", ep.ID)
			}
		}

		fmt.Printf("%-6s %-14s %-10s %-12s %-10d %-12s %-22s %s\n",
			epLabel,
			pl.Name,
			pl.PrimaryClip(),
			bdmv.FormatDuration(dur/count),
			pl.ChapterCount(),
			episodeID,
			note,
			title,
		)
	}
}

func printEpisodesNoTMDB(
	episodes []*bdmv.Playlist,
	info disc.DiscInfo,
	bdmvRoot string,
	clusterDur int,
	startEpisode int,
	robotMode bool,
) {
	// Build clip → episode label map from main (non-commentary) episodes.
	clipEpisode := map[string]string{}
	{
		n := startEpisode
		if n == 0 {
			n = 1
		}
		for _, pl := range episodes {
			if pl.Note != "" {
				continue
			}
			clipEpisode[pl.PrimaryClip()] = fmt.Sprintf("S%02dE%02d", info.Season, n)
			n++
		}
	}

	resolveNote := func(pl *bdmv.Playlist) string {
		if pl.Note == "commentary" && pl.NoteClip != "" {
			if linked, ok := clipEpisode[pl.NoteClip]; ok {
				return fmt.Sprintf("commentary for %s", linked)
			}
		}
		return pl.Note
	}

	if robotMode {
		out := make([]trackResult, 0, len(episodes))
		epNum := startEpisode
		if epNum == 0 {
			epNum = 1
		}
		for _, pl := range episodes {
			dur := pl.EstimateDuration(bdmvRoot, disc.DefaultBitrate)
			count := disc.EstimateEpisodeCount(pl, bdmvRoot, clusterDur)
			ep := fmt.Sprintf("S%02dE%02d", info.Season, epNum)
			if pl.Note == "" {
				epNum++
			}
			out = append(out, trackResult{
				Playlist:  pl.Name,
				Clip:      pl.PrimaryClip(),
				Type:      "tv",
				DiscTitle: info.ShowName,
				Episode:   ep,
				Duration:  bdmv.FormatDuration(dur / count),
				Chapters:  pl.ChapterCount(),
				Note:      resolveNote(pl),
			})
		}
		json.NewEncoder(os.Stdout).Encode(out)
		return
	}

	fmt.Printf("%-6s %-14s %-10s %-12s %-10s %-22s\n", "Ep", "Playlist", "Clip", "Duration", "Chapters", "Note")
	fmt.Println(strings.Repeat("-", 80))

	epNum := startEpisode
	if epNum == 0 {
		epNum = 1
	}
	for _, pl := range episodes {
		dur := pl.EstimateDuration(bdmvRoot, disc.DefaultBitrate)
		count := disc.EstimateEpisodeCount(pl, bdmvRoot, clusterDur)

		ep := fmt.Sprintf("S%02dE%02d", info.Season, epNum)
		if pl.Note == "" {
			epNum++
		}

		fmt.Printf("%-6s %-14s %-10s %-12s %-10d %-22s\n",
			ep,
			pl.Name,
			pl.PrimaryClip(),
			bdmv.FormatDuration(dur/count),
			pl.ChapterCount(),
			resolveNote(pl),
		)
	}
}

func parseArgs() (discArg string, startEpisode int, robotMode bool) {
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-r":
			robotMode = true
		case "--start-episode":
			if i+1 < len(args) {
				fmt.Sscanf(args[i+1], "%d", &startEpisode)
				i++
			}
		default:
			if !strings.HasPrefix(args[i], "--") && !strings.HasPrefix(args[i], "-") && discArg == "" {
				discArg = args[i]
			}
		}
	}
	return discArg, startEpisode, robotMode
}

func main() {
	err := godotenv.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
	}

	discArg, startEpisode, robotMode := parseArgs()

	bdmvRoot, err := disc.SelectBDMV(discArg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		fmt.Fprintf(os.Stderr, "Usage: spindrift [disc-path] [-r] [--start-episode N]\n")
		os.Exit(1)
	}

	if !robotMode {
		fmt.Printf("Found disc: %s\n", bdmvRoot)
	}

	if err := run(bdmvRoot, startEpisode, robotMode); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
