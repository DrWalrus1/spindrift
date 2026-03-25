package disc

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/DrWalrus1/spindrift/bdmv"
)

// DiscInfo contains metadata parsed from the disc's XML manifest.
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

// Disc represents a mounted Blu-ray disc with its BDMV root path.
type Disc struct {
	BDMVRoot string
	Info     DiscInfo
	Index    *bdmv.IndexBDMV
	MObj     *bdmv.MovieObjectBDMV
}

// Open opens a Blu-ray disc at the given BDMV root path and parses
// its index and movie object files.
func Open(bdmvRoot string) (*Disc, error) {
	idx, err := bdmv.ParseIndex(filepath.Join(bdmvRoot, "index.bdmv"))
	if err != nil {
		return nil, fmt.Errorf("parsing index.bdmv: %w", err)
	}

	mobj, err := bdmv.ParseMovieObject(filepath.Join(bdmvRoot, "MovieObject.bdmv"))
	if err != nil {
		return nil, fmt.Errorf("parsing MovieObject.bdmv: %w", err)
	}

	title, err := ParseDiscTitle(bdmvRoot)
	if err != nil {
		// Fall back to volume name
		title = filepath.Base(filepath.Dir(bdmvRoot))
	}

	return &Disc{
		BDMVRoot: bdmvRoot,
		Info:     ParseDiscInfo(title),
		Index:    idx,
		MObj:     mobj,
	}, nil
}

// ParseDiscTitle reads the disc title from bdmt_eng.xml.
func ParseDiscTitle(bdmvRoot string) (string, error) {
	path := filepath.Join(bdmvRoot, BDMTEnglishXML)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	start := strings.Index(string(data), DiNameOpenTag)
	end := strings.Index(string(data), DiNameCloseTag)
	if start < 0 || end < 0 {
		return "", fmt.Errorf("could not find disc name in XML")
	}

	name := string(data)[start+len(DiNameOpenTag) : end]
	return strings.Join(strings.Fields(name), " "), nil
}

// ParseDiscInfo extracts show name, season, and disc number from a disc title string.
func ParseDiscInfo(title string) DiscInfo {
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

// FindBDMVRoots searches mounted volumes for BDMV directories.
func FindBDMVRoots() ([]string, error) {
	roots := discSearchRoots()
	if len(roots) == 0 {
		return nil, fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	var found []string
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			bdmvPath := filepath.Join(root, e.Name(), "BDMV")
			if _, err := os.Stat(bdmvPath); err == nil {
				found = append(found, bdmvPath)
			}
		}
	}

	return found, nil
}

// SelectBDMV returns a BDMV root from an explicit path or auto-detection.
// If multiple discs are found, it prompts the user to choose.
func SelectBDMV(arg string) (string, error) {
	if arg != "" {
		if filepath.Base(arg) == "BDMV" {
			if _, err := os.Stat(arg); err == nil {
				return arg, nil
			}
		}
		bdmvPath := filepath.Join(arg, "BDMV")
		if _, err := os.Stat(bdmvPath); err == nil {
			return bdmvPath, nil
		}
		return "", fmt.Errorf("no BDMV directory found at %s", arg)
	}

	found, err := FindBDMVRoots()
	if err != nil {
		return "", err
	}

	switch len(found) {
	case 0:
		return "", fmt.Errorf("no Blu-ray disc found — is a disc mounted?")
	case 1:
		return found[0], nil
	default:
		fmt.Println("Multiple discs found:")
		for i, f := range found {
			fmt.Printf("  [%d] %s\n", i+1, filepath.Dir(f))
		}
		fmt.Print("Select disc [1]: ")
		choice := 1
		fmt.Scan(&choice)
		if choice < 1 || choice > len(found) {
			return "", fmt.Errorf("invalid selection")
		}
		return found[choice-1], nil
	}
}

func discSearchRoots() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{"/Volumes"}
	case "linux":
		var expanded []string
		for _, root := range []string{"/media", "/run/media", "/mnt"} {
			entries, err := os.ReadDir(root)
			if err != nil {
				continue
			}
			for _, e := range entries {
				if e.IsDir() {
					expanded = append(expanded, filepath.Join(root, e.Name()))
				}
			}
		}
		return expanded
	default:
		return nil
	}
}

// absFloat returns the absolute value of a float64.
func absFloat(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

// removeMultiples filters out durations that are near-integer multiples
// of smaller durations within the plausible episode count range.
func removeMultiples(durations []int) []int {
	if len(durations) <= 1 {
		return durations
	}

	var filtered []int
	for i, d := range durations {
		isMultiple := false
		for j, smaller := range durations {
			if j >= i {
				break
			}
			ratio := float64(d) / float64(smaller)
			rounded := float64(int(ratio + 0.5))
			if rounded < MinMultiple || rounded > float64(MaxEpisodesPerStream) {
				continue
			}
			if absFloat(ratio-rounded)/rounded < MultipleDetectionTolerance {
				isMultiple = true
				break
			}
		}
		if !isMultiple {
			filtered = append(filtered, d)
		}
	}
	return filtered
}

// StreamDurations returns estimated durations for all unique clips
// above the minimum cluster duration threshold.
func StreamDurations(bdmvRoot string) []int {
	pattern := filepath.Join(bdmvRoot, "PLAYLIST", "*.mpls")
	files, _ := filepath.Glob(pattern)

	seen := map[string]bool{}
	var durations []int

	for _, f := range files {
		pl, err := bdmv.ParsePlaylist(f)
		if err != nil {
			continue
		}
		clip := pl.PrimaryClip()
		if clip == "" || seen[clip] {
			continue
		}
		seen[clip] = true

		dur := pl.EstimateDuration(bdmvRoot, DefaultBitrate)
		if dur >= MinClusterDuration {
			durations = append(durations, dur)
		}
	}

	sort.Ints(durations)
	durations = removeMultiples(durations)
	return durations
}

// DominantCluster finds the cluster of similar durations representing
// the most total content. Returns (min, max, center) in seconds.
func DominantCluster(durations []int) (min, max, center int) {
	if len(durations) == 0 {
		return MinEpisodeDuration, MaxEpisodeDuration, 0
	}

	if len(durations) == 1 {
		d := durations[0]
		return int(float64(d) * ClusterLowerBound),
			int(float64(d) * ClusterUpperBound),
			d
	}

	bestStart, bestScore := 0, 0

	for i := range durations {
		totalDuration := 0
		for j := i; j < len(durations); j++ {
			ratio := float64(durations[j]) / float64(durations[i])
			if ratio > ClusterTolerance {
				break
			}
			totalDuration += durations[j]
		}
		if totalDuration > bestScore {
			bestScore = totalDuration
			bestStart = i
		}
	}

	bestEnd := bestStart
	for j := bestStart + 1; j < len(durations); j++ {
		ratio := float64(durations[j]) / float64(durations[bestStart])
		if ratio > ClusterTolerance {
			break
		}
		bestEnd = j
	}

	clusterMin := durations[bestStart]
	clusterMax := durations[bestEnd]

	return int(float64(clusterMin) * ClusterLowerBound),
		int(float64(clusterMax) * ClusterUpperBound),
		(clusterMin + clusterMax) / 2
}

// InferEpisodeBounds analyses stream durations and returns likely
// min/max bounds and cluster center in seconds.
func InferEpisodeBounds(bdmvRoot string) (minDur, maxDur, clusterDur int) {
	durations := StreamDurations(bdmvRoot)
	if len(durations) == 0 {
		return MinEpisodeDuration, MaxEpisodeDuration, 0
	}
	return DominantCluster(durations)
}

// EstimateEpisodeCount checks if the stream duration is a near-integer
// multiple of the cluster episode duration.
func EstimateEpisodeCount(pl *bdmv.Playlist, bdmvRoot string, clusterDur int) int {
	if clusterDur <= 0 {
		return 1
	}

	totalDur := pl.EstimateDuration(bdmvRoot, DefaultBitrate)
	if totalDur < MinViableDuration {
		return 1
	}

	ratio := float64(totalDur) / float64(clusterDur)
	rounded := int(ratio + 0.5)

	if rounded < 1 || rounded > MaxEpisodesPerStream {
		return 1
	}

	if absFloat(ratio-float64(rounded))/float64(rounded) > EpisodeRatioTolerance {
		return 1
	}

	return rounded
}

// LoadEpisodePlaylists loads playlists filtered to likely episode content,
// expanding multi-episode streams into individual entries.
func LoadEpisodePlaylists(bdmvRoot string, minDur, maxDur, clusterDur int) ([]*bdmv.Playlist, error) {
	all, err := bdmv.LoadAllPlaylists(bdmvRoot)
	if err != nil {
		return nil, err
	}

	var episodes []*bdmv.Playlist
	seen := map[string]bool{}

	for _, pl := range all {
		clip := pl.PrimaryClip()
		if clip == "" || seen[clip] {
			continue
		}

		dur := pl.EstimateDuration(bdmvRoot, DefaultBitrate)
		if dur < MinViableDuration {
			continue
		}

		episodeCount := EstimateEpisodeCount(pl, bdmvRoot, clusterDur)
		perEpisodeDur := dur / episodeCount

		if perEpisodeDur < minDur || perEpisodeDur > maxDur {
			continue
		}

		seen[clip] = true

		if episodeCount > 1 {
			for i := 0; i < episodeCount; i++ {
				episodes = append(episodes, &bdmv.Playlist{
					Name:      fmt.Sprintf("%s[%d]", pl.Name, i+1),
					PlayItems: pl.PlayItems,
					Marks:     pl.Marks,
				})
			}
		} else {
			episodes = append(episodes, pl)
		}
	}

	return episodes, nil
}
