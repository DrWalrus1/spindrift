package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
)

// discSearchRoots returns the platform-specific directories to search
// for mounted optical disc volumes.
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

// FindBDMVRoots searches mounted volumes for BDMV directories and
// returns all found paths.
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
			bdmv := filepath.Join(root, e.Name(), "BDMV")
			if _, err := os.Stat(bdmv); err == nil {
				found = append(found, bdmv)
			}
		}
	}

	return found, nil
}

// SelectBDMV returns a single BDMV root, either from the command line
// argument, auto-detected, or interactively chosen if multiple are found.
func SelectBDMV(arg string) (string, error) {
	if arg != "" {
		if filepath.Base(arg) == "BDMV" {
			if _, err := os.Stat(arg); err == nil {
				return arg, nil
			}
		}
		bdmv := filepath.Join(arg, "BDMV")
		if _, err := os.Stat(bdmv); err == nil {
			return bdmv, nil
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
		fmt.Printf("Found disc: %s\n", filepath.Dir(found[0]))
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

// absFloat returns the absolute value of a float64.
func absFloat(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

// removeMultiples filters out durations that are near-integer multiples
// of smaller durations within the plausible episode count range —
// these are likely "play all" or combined streams.
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

			// Only consider multiples within plausible episode count range
			if rounded < minMultiple || rounded > float64(maxEpisodesPerStream) {
				continue
			}

			// Relative tolerance: error as fraction of the multiple
			if absFloat(ratio-rounded)/rounded < multipleDetectionTolerance {
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

// streamDurations returns estimated durations for all unique clips
// above the minimum cluster duration threshold.
func streamDurations(bdmvRoot string) []int {
	pattern := filepath.Join(bdmvRoot, "PLAYLIST", "*.mpls")
	files, _ := filepath.Glob(pattern)

	seen := map[string]bool{}
	var durations []int

	for _, f := range files {
		pl, err := ParsePlaylist(f)
		if err != nil {
			continue
		}
		clip := pl.PrimaryClip()
		if clip == "" || seen[clip] {
			continue
		}
		seen[clip] = true

		dur := pl.EstimateDuration(bdmvRoot)
		if dur >= minClusterDuration {
			durations = append(durations, dur)
		}
	}

	sort.Ints(durations)
	durations = removeMultiples(durations)
	return durations
}

// dominantCluster finds the cluster of similar durations that represents
// the most total content (sum of durations), not just the most streams.
// Returns (min, max, center) bounds in seconds.
func dominantCluster(durations []int) (min, max, center int) {
	if len(durations) == 0 {
		return minEpisodeDuration, maxEpisodeDuration, 0
	}

	if len(durations) == 1 {
		d := durations[0]
		return int(float64(d) * clusterLowerBound),
			int(float64(d) * clusterUpperBound),
			d
	}

	bestStart, bestScore := 0, 0

	for i := 0; i < len(durations); i++ {
		totalDuration := 0
		for j := i; j < len(durations); j++ {
			ratio := float64(durations[j]) / float64(durations[i])
			if ratio > clusterTolerance {
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
		if ratio > clusterTolerance {
			break
		}
		bestEnd = j
	}

	clusterMin := durations[bestStart]
	clusterMax := durations[bestEnd]
	clusterCenter := (clusterMin + clusterMax) / 2

	return int(float64(clusterMin) * clusterLowerBound),
		int(float64(clusterMax) * clusterUpperBound),
		clusterCenter
}

// InferEpisodeBounds analyses stream durations on the disc and returns
// likely min/max episode duration bounds and cluster center in seconds.
func InferEpisodeBounds(bdmvRoot string) (minDur, maxDur, clusterDur int) {
	durations := streamDurations(bdmvRoot)

	if len(durations) == 0 {
		return minEpisodeDuration, maxEpisodeDuration, 0
	}

	minDur, maxDur, clusterDur = dominantCluster(durations)

	fmt.Printf("Inferred episode bounds: %s – %s (cluster center: %s)",
		FormatDuration(minDur), FormatDuration(maxDur), FormatDuration(clusterDur))
	fmt.Printf(" (from %d unique stream durations)\n", len(durations))

	return minDur, maxDur, clusterDur
}
