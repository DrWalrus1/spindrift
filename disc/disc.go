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
	IsSeries bool // true when title contains series indicators (arc, set, disc-count)
}

// DetectMovie marks the disc as a movie if no season/disc markers
// were found, only one title is present, and no series indicators were detected.
func (d *DiscInfo) DetectMovie(episodeCount int) {
	if d.IsSeries {
		return
	}
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

	// Disc number: "Disc 2" (space) or "Disc2" (digit immediately following).
	if i := strings.Index(lower, "disc "); i >= 0 {
		fmt.Sscanf(strings.TrimSpace(title[i+5:]), "%d", &info.Disc)
	} else if i := indexDiscDigit(lower); i >= 0 {
		fmt.Sscanf(lower[i+4:], "%d", &info.Disc)
		info.IsSeries = true // "Disc1" style implies a numbered disc in a set
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

	// Series indicators: keywords that signal a TV series rather than a standalone movie.
	for _, kw := range []string{" arc", " set"} {
		if strings.Contains(lower, kw) {
			info.IsSeries = true
			break
		}
	}

	// Show name: everything before the first structural marker.
	// Include "Disc\d" as a marker so it is stripped from the name.
	markers := []string{"Book ", "Season ", "Disc "}
	if di := indexDiscDigit(title); di > 0 {
		markers = append(markers, title[di:di+5]) // e.g. "Disc1"
	}
	for _, marker := range markers {
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

// indexDiscDigit returns the index of the first "disc\d" sequence (disc immediately
// followed by a digit, e.g. "Disc1"), or -1 if not found.
func indexDiscDigit(s string) int {
	lower := strings.ToLower(s)
	for i := 0; i+5 <= len(lower); i++ {
		if lower[i:i+4] == "disc" && lower[i+4] >= '0' && lower[i+4] <= '9' {
			return i
		}
	}
	return -1
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

// EstimateEpisodeCount checks how many episodes a playlist contains.
// It first checks whether the playlist spans multiple substantial clips
// (each clip = one episode), then falls back to comparing the total
// duration against the dominant cluster duration.
func EstimateEpisodeCount(pl *bdmv.Playlist, bdmvRoot string, clusterDur int) int {
	// If the playlist contains N substantial clips, each is one episode.
	if n := pl.SubstantialClipCount(bdmvRoot, DefaultBitrate, MinClusterDuration); n > 1 {
		return n
	}

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

// ChapterEpisodeCount infers the number of episodes from chapter marks
// when all episodes share a single playlist. Returns 0 if the chapter
// structure does not look like TV episodes.
func ChapterEpisodeCount(pl *bdmv.Playlist) int {
	durs := pl.ChapterDurations(MinEpisodeDuration)
	if len(durs) < 2 {
		return 0
	}

	_, _, center := DominantCluster(durs)
	if center < MinEpisodeDuration || center > MaxEpisodeDuration {
		return 0
	}

	// Count chapters that fall within the cluster bounds.
	lo := int(float64(center) * ClusterLowerBound)
	hi := int(float64(center) * ClusterUpperBound)
	count := 0
	for _, d := range durs {
		if d >= lo && d <= hi {
			count++
		}
	}
	if count < 2 {
		return 0
	}
	return count
}

// LoadEpisodePlaylists loads playlists filtered to likely episode content,
// expanding multi-episode streams into individual entries.
func LoadEpisodePlaylists(bdmvRoot string, minDur, maxDur, clusterDur int) ([]*bdmv.Playlist, error) {
	all, err := bdmv.LoadAllPlaylists(bdmvRoot)
	if err != nil {
		return nil, err
	}

	// Sort by episode content duration DESCENDING so that more-complete
	// variants of an episode (e.g. with opening credits) are processed
	// before stripped versions. Use play-item count ASCENDING as a
	// tiebreaker so that simpler playlists win when durations are equal
	// (avoids preferring commentary over main episode when both measure
	// the same duration via file-size estimation).
	sort.SliceStable(all, func(i, j int) bool {
		di := all[i].EstimateDuration(bdmvRoot, DefaultBitrate)
		dj := all[j].EstimateDuration(bdmvRoot, DefaultBitrate)
		if di != dj {
			return di > dj // longer first
		}
		return len(all[i].PlayItems) < len(all[j].PlayItems) // simpler first on tie
	})

	var episodes []*bdmv.Playlist
	seen := map[string]bool{}
	seenCommentary := map[string]bool{} // deduplicate commentary per episode clip

	processPlaylist := func(pl *bdmv.Playlist) {
		clip := pl.PrimaryClip()
		if clip == "" {
			return
		}

		dur := pl.EstimateDuration(bdmvRoot, DefaultBitrate)
		if dur < MinViableDuration {
			return
		}

		episodeCount := EstimateEpisodeCount(pl, bdmvRoot, clusterDur)

		// If the stream-level estimate gives 1, check whether the
		// chapter marks reveal multiple episodes in a single playlist.
		if episodeCount == 1 {
			if n := ChapterEpisodeCount(pl); n > 1 {
				episodeCount = n
			}
		}

		perEpisodeDur := dur / episodeCount

		if perEpisodeDur < minDur || perEpisodeDur > maxDur {
			// For chapter-detected episodes the per-episode duration
			// from file size may be unreliable; trust the chapter
			// analysis if it fired and skip the filter.
			if episodeCount <= 1 {
				return
			}
		}

		if seen[clip] {
			// Primary clip already claimed by a longer/preferred variant.
			// Detect commentary: single-episode playlist where all non-primary
			// items are short overlay clips (Duration < 60s after timestamp fix).
			if episodeCount == 1 && len(pl.PlayItems) > 1 && dur >= minDur && dur <= maxDur {
				allShort := true
				for _, item := range pl.PlayItems {
					if item.ClipName != clip && item.Duration >= 60 {
						allShort = false
						break
					}
				}
				if allShort && !seenCommentary[clip] {
					seenCommentary[clip] = true
					pl.Note = "commentary"
					pl.NoteClip = clip
					episodes = append(episodes, pl)
				}
			}
			return
		}

		seen[clip] = true

		if episodeCount > 1 {
			// Skip play-all if every episode it contains is already covered.
			coveredCount := 0
			for _, item := range pl.PlayItems {
				if item.ClipName != clip && seen[item.ClipName] {
					coveredCount++
				}
			}
			if coveredCount >= episodeCount {
				return
			}
			for i := range episodeCount {
				episodes = append(episodes, &bdmv.Playlist{
					Name:         fmt.Sprintf("%s[%d]", pl.Name, i+1),
					PlayItems:    pl.PlayItems,
					Marks:        pl.Marks,
					EpisodeCount: episodeCount,
					EpisodeIndex: i + 1,
				})
			}
		} else {
			episodes = append(episodes, pl)
		}
	}

	// First pass: single-episode playlists (sorted by duration DESC above).
	// These are processed before multi-episode playlists so that individual
	// episode variants are preferred over play-all compilations.
	for _, pl := range all {
		if EstimateEpisodeCount(pl, bdmvRoot, clusterDur) == 1 {
			processPlaylist(pl)
		}
	}

	// Second pass: multi-episode playlists. These handle discs (e.g. Demon
	// Slayer) where all episodes are packed into a single multi-clip stream
	// with no separate per-episode playlists.
	for _, pl := range all {
		if EstimateEpisodeCount(pl, bdmvRoot, clusterDur) > 1 {
			processPlaylist(pl)
		}
	}

	// Re-sort by playlist name so episodes appear in disc order regardless
	// of which variant was chosen during duration-based deduplication.
	sort.SliceStable(episodes, func(i, j int) bool {
		return episodes[i].Name < episodes[j].Name
	})

	return episodes, nil
}
