package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func parseDiscTitle(bdmvRoot string) (string, error) {
	path := filepath.Join(bdmvRoot, bdmtEnglishXML)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	start := strings.Index(string(data), diNameOpenTag)
	end := strings.Index(string(data), diNameCloseTag)
	if start < 0 || end < 0 {
		return "", fmt.Errorf("could not find disc name in XML")
	}

	name := string(data)[start+len(diNameOpenTag) : end]
	return strings.Join(strings.Fields(name), " "), nil
}

func run(bdmvRoot string, startEpisode int) error {
	// --- index.bdmv ---
	idx, err := ParseIndex(filepath.Join(bdmvRoot, "index.bdmv"))
	if err != nil {
		return fmt.Errorf("parsing index.bdmv: %w", err)
	}

	fmt.Printf("Version:    %s\n", idx.Version)
	fmt.Printf("FirstPlay → %s\n", idx.FirstPlay.PlaylistPath(bdmvRoot))
	fmt.Printf("TopMenu   → %s\n", idx.TopMenu.PlaylistPath(bdmvRoot))
	for i, t := range idx.Titles {
		if t.IsHDMV() {
			fmt.Printf("  Title[%d] → PLAYLIST/%05d.mpls\n", i, t.ObjectIDRef)
		} else {
			fmt.Printf("  Title[%d] → MovieObject[%d] (BD-J)\n", i, t.ObjectIDRef)
		}
	}

	// --- MovieObject.bdmv ---
	mobj, err := ParseMovieObject(filepath.Join(bdmvRoot, "MovieObject.bdmv"))
	if err != nil {
		return fmt.Errorf("parsing MovieObject.bdmv: %w", err)
	}

	fmt.Printf("\nVersion: %s\n", mobj.Version)
	fmt.Printf("Objects (%d):\n", len(mobj.Objects))
	for i, obj := range mobj.Objects {
		fmt.Printf("  [%d] resume=%v menuMask=%v titleMask=%v cmds=%d\n",
			i, obj.ResumeIntentionFlag, obj.MenuCallMask, obj.TitleSearchMask,
			len(obj.Commands),
		)
	}

	// --- Disc metadata ---
	discTitle, err := parseDiscTitle(bdmvRoot)
	if err != nil {
		fmt.Printf("Warning: could not parse disc title: %v\n", err)
		discTitle = filepath.Base(filepath.Dir(bdmvRoot))
		fmt.Printf("Falling back to volume name: %s\n", discTitle)
	}
	fmt.Printf("\nDisc Title: %s\n", discTitle)

	discInfo := ParseDiscTitle(discTitle)
	fmt.Printf("Show:       %s\n", discInfo.ShowName)
	if !discInfo.IsMovie {
		fmt.Printf("Season:     %d\n", discInfo.Season)
		fmt.Printf("Disc:       %d\n", discInfo.Disc)
	}
	if startEpisode > 0 {
		fmt.Printf("Start Ep:   %d\n", startEpisode)
	}
	fmt.Println()

	// --- Infer episode duration bounds from disc ---
	minDur, maxDur, clusterDur := InferEpisodeBounds(bdmvRoot)

	// --- Episode playlists ---
	episodes, err := LoadEpisodePlaylists(bdmvRoot, minDur, maxDur, clusterDur)
	if err != nil {
		return fmt.Errorf("loading playlists: %w", err)
	}

	if len(episodes) == 0 {
		fmt.Println("No episodes found on disc")
		return nil
	}

	// --- Detect movie vs TV ---
	discInfo.DetectMovie(len(episodes))

	if discInfo.IsMovie {
		fmt.Printf("Detected: Movie\n\n")
	} else {
		fmt.Printf("Found %d episodes on disc\n\n", len(episodes))
	}

	// --- TMDB lookup ---
	apiKey := os.Getenv(envTMDBAPIKey)
	if apiKey == "" {
		return fmt.Errorf("%s not set — add it to .env or your environment", envTMDBAPIKey)
	}

	client := NewTMDBClient(apiKey)

	if discInfo.IsMovie {
		return runMovie(client, episodes, discInfo, bdmvRoot, clusterDur)
	}
	return runTV(client, episodes, discInfo, bdmvRoot, clusterDur, startEpisode)
}

func runMovie(
	client *TMDBClient,
	episodes []*Playlist,
	discInfo DiscInfo,
	bdmvRoot string,
	clusterDur int,
) error {
	movies, err := client.SearchMovie(discInfo.ShowName)
	if err != nil {
		return fmt.Errorf("TMDB movie search: %w", err)
	}
	if len(movies) == 0 {
		fmt.Printf("No TMDB movie results found for %q\n", discInfo.ShowName)
		printMovieNoTMDB(episodes, bdmvRoot, clusterDur)
		return nil
	}

	movie := movies[0]
	details, err := client.GetMovie(movie.ID)
	if err != nil {
		return fmt.Errorf("TMDB movie details: %w", err)
	}

	fmt.Printf("TMDB Match: %s (ID: %d, Runtime: %d min)\n\n",
		details.Title, movie.ID, details.Runtime)

	printMovie(episodes[0], details, bdmvRoot, clusterDur)
	return nil
}

func runTV(
	client *TMDBClient,
	episodes []*Playlist,
	discInfo DiscInfo,
	bdmvRoot string,
	clusterDur int,
	startEpisode int,
) error {
	shows, err := client.SearchTV(discInfo.ShowName)
	if err != nil {
		return fmt.Errorf("TMDB search: %w", err)
	}
	if len(shows) == 0 {
		fmt.Printf("No TMDB results found for %q\n", discInfo.ShowName)
		printEpisodesNoTMDB(episodes, discInfo, bdmvRoot, clusterDur, startEpisode)
		return nil
	}

	show := shows[0]
	fmt.Printf("TMDB Match: %s (ID: %d)\n\n", show.Name, show.ID)

	season, err := client.GetSeason(show.ID, discInfo.Season)
	if err != nil {
		return fmt.Errorf("TMDB season fetch: %w", err)
	}

	tmdbEps := EpisodesForDisc(season, startEpisode, len(episodes))
	printEpisodes(episodes, tmdbEps, discInfo, bdmvRoot, clusterDur)
	return nil
}

func printMovie(
	pl *Playlist,
	details *TMDBMovieDetails,
	bdmvRoot string,
	clusterDur int,
) {
	dur := pl.EstimateDuration(bdmvRoot)
	count := pl.EstimateEpisodeCount(bdmvRoot, clusterDur)

	fmt.Printf("%-10s %-12s %-12s %s\n", "Type", "Playlist", "Duration", "Title")
	fmt.Println(strings.Repeat("-", 55))
	fmt.Printf("%-10s %-12s %-12s %s\n",
		"Movie",
		pl.Name,
		FormatDuration(dur/count),
		details.Title,
	)
}

func printMovieNoTMDB(
	episodes []*Playlist,
	bdmvRoot string,
	clusterDur int,
) {
	fmt.Printf("%-10s %-12s %s\n", "Type", "Playlist", "Duration")
	fmt.Println(strings.Repeat("-", 35))
	for _, pl := range episodes {
		dur := pl.EstimateDuration(bdmvRoot)
		count := pl.EstimateEpisodeCount(bdmvRoot, clusterDur)
		fmt.Printf("%-10s %-12s %s\n",
			"Movie",
			pl.Name,
			FormatDuration(dur/count),
		)
	}
}

func printEpisodes(
	episodes []*Playlist,
	tmdbEps []TMDBEpisode,
	discInfo DiscInfo,
	bdmvRoot string,
	clusterDur int,
) {
	fmt.Printf("%-6s %-14s %-10s %-12s %s\n",
		"Ep", "Playlist", "Clip", "Duration", "Title")
	fmt.Println(strings.Repeat("-", 72))

	for i, pl := range episodes {
		dur := pl.EstimateDuration(bdmvRoot)
		count := pl.EstimateEpisodeCount(bdmvRoot, clusterDur)

		epLabel := fmt.Sprintf("S%02dE??", discInfo.Season)
		title := "unknown"
		if i < len(tmdbEps) {
			ep := tmdbEps[i]
			epLabel = fmt.Sprintf("S%02dE%02d", discInfo.Season, ep.EpisodeNumber)
			title = ep.Name
		}

		fmt.Printf("%-6s %-14s %-10s %-12s %s\n",
			epLabel,
			pl.Name,
			pl.PrimaryClip(),
			FormatDuration(dur/count),
			title,
		)
	}
}

func printEpisodesNoTMDB(
	episodes []*Playlist,
	discInfo DiscInfo,
	bdmvRoot string,
	clusterDur int,
	startEpisode int,
) {
	fmt.Printf("%-6s %-14s %-10s %s\n", "Ep", "Playlist", "Clip", "Duration")
	fmt.Println(strings.Repeat("-", 48))

	for i, pl := range episodes {
		dur := pl.EstimateDuration(bdmvRoot)
		count := pl.EstimateEpisodeCount(bdmvRoot, clusterDur)

		epNum := startEpisode + i
		if startEpisode == 0 {
			epNum = i + 1
		}

		fmt.Printf("S%02dE%02d %-14s %-10s %s\n",
			discInfo.Season,
			epNum,
			pl.Name,
			pl.PrimaryClip(),
			FormatDuration(dur/count),
		)
	}
}

func parseArgs() (discArg string, startEpisode int) {
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--start-episode":
			if i+1 < len(args) {
				fmt.Sscanf(args[i+1], "%d", &startEpisode)
				i++
			}
		default:
			if !strings.HasPrefix(args[i], "--") && discArg == "" {
				discArg = args[i]
			}
		}
	}
	return discArg, startEpisode
}

func main() {
	if err := loadEnv(".env"); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
	}

	discArg, startEpisode := parseArgs()

	bdmvRoot, err := SelectBDMV(discArg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		fmt.Fprintf(os.Stderr, "Usage: %s [disc-path] [--start-episode N]\n", os.Args[0])
		os.Exit(1)
	}

	if err := run(bdmvRoot, startEpisode); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
