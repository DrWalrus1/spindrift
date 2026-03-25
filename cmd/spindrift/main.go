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
	Playlist     string `json:"playlist"`
	Clip         string `json:"clip,omitempty"`
	Type         string `json:"type"`
	Episode      string `json:"episode,omitempty"`
	Title        string `json:"title,omitempty"`
	Duration     string `json:"duration"`
	TMDBDuration string `json:"tmdb_duration,omitempty"`
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
	movies, err := client.SearchMovie(info.ShowName)
	if err != nil {
		return fmt.Errorf("TMDB movie search: %w", err)
	}
	if len(movies) == 0 {
		if !robotMode {
			fmt.Printf("No TMDB movie results found for %q\n", info.ShowName)
		}
		printMovieNoTMDB(episodes, bdmvRoot, clusterDur, robotMode)
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

	printMovie(episodes[0], details, bdmvRoot, clusterDur, robotMode)
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
	shows, err := client.SearchTV(info.ShowName)
	if err != nil {
		return fmt.Errorf("TMDB search: %w", err)
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

	season, err := client.GetSeason(show.ID, info.Season)
	if err != nil {
		return fmt.Errorf("TMDB season fetch: %w", err)
	}

	tmdbEps := tmdb.EpisodesForDisc(season, startEpisode, len(episodes))
	printEpisodes(episodes, tmdbEps, info, bdmvRoot, clusterDur, robotMode)
	return nil
}

func printMovie(
	pl *bdmv.Playlist,
	details *tmdb.MovieDetails,
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
			Title:        details.Title,
			Duration:     bdmv.FormatDuration(dur / count),
			TMDBDuration: bdmv.FormatDuration(details.Runtime * 60),
		}}
		json.NewEncoder(os.Stdout).Encode(out)
		return
	}

	fmt.Printf("%-10s %-12s %-12s %s\n", "Type", "Playlist", "Duration", "Title")
	fmt.Println(strings.Repeat("-", 55))
	fmt.Printf("%-10s %-12s %-12s %s\n",
		"Movie",
		pl.Name,
		bdmv.FormatDuration(dur/count),
		details.Title,
	)
}

func printMovieNoTMDB(
	episodes []*bdmv.Playlist,
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
				Playlist: pl.Name,
				Clip:     pl.PrimaryClip(),
				Type:     "movie",
				Duration: bdmv.FormatDuration(dur / count),
			})
		}
		json.NewEncoder(os.Stdout).Encode(out)
		return
	}

	fmt.Printf("%-10s %-12s %s\n", "Type", "Playlist", "Duration")
	fmt.Println(strings.Repeat("-", 35))
	for _, pl := range episodes {
		dur := pl.EstimateDuration(bdmvRoot, disc.DefaultBitrate)
		count := disc.EstimateEpisodeCount(pl, bdmvRoot, clusterDur)
		fmt.Printf("%-10s %-12s %s\n",
			"Movie",
			pl.Name,
			bdmv.FormatDuration(dur/count),
		)
	}
}

func printEpisodes(
	episodes []*bdmv.Playlist,
	tmdbEps []tmdb.Episode,
	info disc.DiscInfo,
	bdmvRoot string,
	clusterDur int,
	robotMode bool,
) {
	if robotMode {
		out := make([]trackResult, 0, len(episodes))
		for i, pl := range episodes {
			dur := pl.EstimateDuration(bdmvRoot, disc.DefaultBitrate)
			count := disc.EstimateEpisodeCount(pl, bdmvRoot, clusterDur)
			r := trackResult{
				Playlist: pl.Name,
				Clip:     pl.PrimaryClip(),
				Type:     "tv",
				Episode:  fmt.Sprintf("S%02dE??", info.Season),
				Duration: bdmv.FormatDuration(dur / count),
			}
			if i < len(tmdbEps) {
				ep := tmdbEps[i]
				r.Episode = fmt.Sprintf("S%02dE%02d", info.Season, ep.EpisodeNumber)
				r.Title = ep.Name
				if ep.Runtime > 0 {
					r.TMDBDuration = bdmv.FormatDuration(ep.Runtime * 60)
				}
			}
			out = append(out, r)
		}
		json.NewEncoder(os.Stdout).Encode(out)
		return
	}

	fmt.Printf("%-6s %-14s %-10s %-12s %s\n",
		"Ep", "Playlist", "Clip", "Duration", "Title")
	fmt.Println(strings.Repeat("-", 72))

	for i, pl := range episodes {
		dur := pl.EstimateDuration(bdmvRoot, disc.DefaultBitrate)
		count := disc.EstimateEpisodeCount(pl, bdmvRoot, clusterDur)

		epLabel := fmt.Sprintf("S%02dE??", info.Season)
		title := "unknown"
		if i < len(tmdbEps) {
			ep := tmdbEps[i]
			epLabel = fmt.Sprintf("S%02dE%02d", info.Season, ep.EpisodeNumber)
			title = ep.Name
		}

		fmt.Printf("%-6s %-14s %-10s %-12s %s\n",
			epLabel,
			pl.Name,
			pl.PrimaryClip(),
			bdmv.FormatDuration(dur/count),
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
	if robotMode {
		out := make([]trackResult, 0, len(episodes))
		for i, pl := range episodes {
			dur := pl.EstimateDuration(bdmvRoot, disc.DefaultBitrate)
			count := disc.EstimateEpisodeCount(pl, bdmvRoot, clusterDur)
			epNum := startEpisode + i
			if startEpisode == 0 {
				epNum = i + 1
			}
			out = append(out, trackResult{
				Playlist: pl.Name,
				Clip:     pl.PrimaryClip(),
				Type:     "tv",
				Episode:  fmt.Sprintf("S%02dE%02d", info.Season, epNum),
				Duration: bdmv.FormatDuration(dur / count),
			})
		}
		json.NewEncoder(os.Stdout).Encode(out)
		return
	}

	fmt.Printf("%-6s %-14s %-10s %s\n", "Ep", "Playlist", "Clip", "Duration")
	fmt.Println(strings.Repeat("-", 48))

	for i, pl := range episodes {
		dur := pl.EstimateDuration(bdmvRoot, disc.DefaultBitrate)
		count := disc.EstimateEpisodeCount(pl, bdmvRoot, clusterDur)

		epNum := startEpisode + i
		if startEpisode == 0 {
			epNum = i + 1
		}

		fmt.Printf("S%02dE%02d %-14s %-10s %s\n",
			info.Season,
			epNum,
			pl.Name,
			pl.PrimaryClip(),
			bdmv.FormatDuration(dur/count),
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
