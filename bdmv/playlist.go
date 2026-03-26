package bdmv

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Playlist represents a parsed .mpls playlist file.
type Playlist struct {
	Name         string
	PlayItems    []PlayItem
	Marks        []PlaylistMark
	EpisodeCount int // >1 when this playlist is shared by multiple episodes
	EpisodeIndex int // 1-based position within EpisodeCount
}

// ChapterCount returns the total number of marks in this playlist assigned
// to this episode. When EpisodeCount > 1, the total is divided as evenly
// as possible across all episodes, with any remainder given to the last ones.
func (p *Playlist) ChapterCount() int {
	total := len(p.Marks)

	n := p.EpisodeCount
	if n <= 1 {
		return total
	}

	base := total / n
	remainder := total % n
	// Last `remainder` episodes get an extra chapter.
	if p.EpisodeIndex > n-remainder {
		return base + 1
	}
	return base
}

// PlayItem represents a single play item within a playlist.
type PlayItem struct {
	ClipName string
	InTime   uint32
	OutTime  uint32
	Duration int // seconds
}

// PlaylistMark represents a chapter mark within a playlist.
type PlaylistMark struct {
	MarkType    uint8
	PlayItemRef uint16
	Timestamp   uint32
	Duration    uint32
}

// TotalDuration returns the sum of all PlayItem durations in seconds.
func (p *Playlist) TotalDuration() int {
	total := 0
	for _, item := range p.PlayItems {
		total += item.Duration
	}
	return total
}

// EpisodeDuration returns the total duration of unique clips,
// skipping duplicates and short bumper/tail clips under 60 seconds.
func (p *Playlist) EpisodeDuration() int {
	seen := map[string]bool{}
	total := 0
	for _, item := range p.PlayItems {
		if seen[item.ClipName] || item.Duration < 60 {
			continue
		}
		seen[item.ClipName] = true
		total += item.Duration
	}
	return total
}

// PrimaryClip returns the name of the first clip with duration over 60 seconds.
func (p *Playlist) PrimaryClip() string {
	for _, item := range p.PlayItems {
		if item.Duration >= 60 {
			return item.ClipName
		}
	}
	if len(p.PlayItems) > 0 {
		return p.PlayItems[0].ClipName
	}
	return ""
}

// StreamPath returns the full path to the primary clip's .m2ts file.
func (p *Playlist) StreamPath(bdmvRoot string) string {
	clip := p.PrimaryClip()
	if clip == "" {
		return ""
	}
	return filepath.Join(bdmvRoot, "STREAM", clip+".m2ts")
}

// clipDuration returns the estimated duration in seconds for a single clip,
// using its CLPI recording rate when available and falling back to the
// provided default bitrate (bytes/sec).
func clipDuration(bdmvRoot, clipName string, defaultBitrate int) int {
	path := filepath.Join(bdmvRoot, "STREAM", clipName+".m2ts")
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	rate := ClipBitrate(bdmvRoot, clipName)
	if rate == 0 {
		rate = defaultBitrate / 8 // convert bits/sec to bytes/sec
	}
	if rate == 0 {
		return 0
	}
	return int(info.Size() / int64(rate))
}

// EstimateDuration estimates total stream duration by summing all unique clips
// in the playlist. Each clip's bitrate is read from its .clpi file when
// available, falling back to the provided default bitrate (bits/sec).
// Summing all clips ensures multi-episode playlists spanning several .m2ts
// files are measured correctly.
func (p *Playlist) EstimateDuration(bdmvRoot string, defaultBitrate int) int {
	seen := map[string]bool{}
	total := 0
	for _, item := range p.PlayItems {
		if seen[item.ClipName] {
			continue
		}
		seen[item.ClipName] = true
		total += clipDuration(bdmvRoot, item.ClipName, defaultBitrate)
	}
	return total
}

// SubstantialClipCount returns the number of unique clips in the playlist
// whose estimated duration meets or exceeds minDur seconds.
func (p *Playlist) SubstantialClipCount(bdmvRoot string, defaultBitrate, minDur int) int {
	seen := map[string]bool{}
	count := 0
	for _, item := range p.PlayItems {
		if seen[item.ClipName] {
			continue
		}
		seen[item.ClipName] = true
		if clipDuration(bdmvRoot, item.ClipName, defaultBitrate) >= minDur {
			count++
		}
	}
	return count
}

// PTSDuration calculates duration in seconds from two PTS timestamps.
// uint32 subtraction handles PTS wraparound at 0xFFFFFFFF naturally.
func PTSDuration(in, out uint32) int {
	return int((out - in) / PTSClock)
}

// ChapterDurations returns the duration in seconds between consecutive
// entry marks (MarkType == 0) that share the same play item. Short gaps
// under minSecs are excluded (recaps, previews, etc.).
func (p *Playlist) ChapterDurations(minSecs int) []int {
	var entry []PlaylistMark
	for _, m := range p.Marks {
		if m.MarkType == 0 {
			entry = append(entry, m)
		}
	}
	if len(entry) < 2 {
		return nil
	}
	var durs []int
	for i := 0; i < len(entry)-1; i++ {
		if entry[i].PlayItemRef != entry[i+1].PlayItemRef {
			continue
		}
		d := int((entry[i+1].Timestamp - entry[i].Timestamp) / PTSClock)
		if d >= minSecs {
			durs = append(durs, d)
		}
	}
	return durs
}

// FormatDuration formats a duration in seconds as "M:SS".
func FormatDuration(secs int) string {
	return fmt.Sprintf("%d:%02d", secs/60, secs%60)
}

// ParsePlaylist parses the .mpls file at the given path.
func ParsePlaylist(path string) (*Playlist, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	typeInd := make([]byte, 4)
	io.ReadFull(f, typeInd)
	if string(typeInd) != TypeIndicatorMPLS {
		return nil, fmt.Errorf("not an mpls file")
	}

	f.Seek(playlistOffsetAddr, io.SeekStart)
	var playlistOffset uint32
	binary.Read(f, binary.BigEndian, &playlistOffset)

	f.Seek(playlistMarkOffsetAddr, io.SeekStart)
	var markOffset uint32
	binary.Read(f, binary.BigEndian, &markOffset)

	// PlayList() header: length(4) + reserved(2) + num_items(2) + num_subpaths(2)
	f.Seek(int64(playlistOffset)+playlistHeaderSkip, io.SeekStart)
	var numItems uint16
	binary.Read(f, binary.BigEndian, &numItems)
	f.Seek(2, io.SeekCurrent) // skip num_subpaths

	pl := &Playlist{
		Name:      strings.TrimSuffix(filepath.Base(path), ".mpls"),
		PlayItems: make([]PlayItem, 0, numItems),
	}

	for i := 0; i < int(numItems); i++ {
		itemStart, _ := f.Seek(0, io.SeekCurrent)

		var itemLen uint16
		binary.Read(f, binary.BigEndian, &itemLen)

		clipName := make([]byte, playItemClipNameLen)
		io.ReadFull(f, clipName)

		f.Seek(playItemTimestampSkip, io.SeekCurrent)

		var inTime, outTime uint32
		binary.Read(f, binary.BigEndian, &inTime)
		binary.Read(f, binary.BigEndian, &outTime)

		pl.PlayItems = append(pl.PlayItems, PlayItem{
			ClipName: string(clipName[:playItemClipNameUsed]),
			InTime:   inTime,
			OutTime:  outTime,
			Duration: PTSDuration(inTime, outTime),
		})

		// Jump to next PlayItem using itemLen — handles variable-length
		// stream entries regardless of audio/subtitle track count.
		f.Seek(itemStart+2+int64(itemLen), io.SeekStart)
	}

	if markOffset > 0 {
		pl.Marks, _ = parsePlaylistMarks(f, markOffset)
	}

	return pl, nil
}

func parsePlaylistMarks(f *os.File, markOffset uint32) ([]PlaylistMark, error) {
	f.Seek(int64(markOffset), io.SeekStart)

	var length uint32
	binary.Read(f, binary.BigEndian, &length)

	var numMarks uint16
	binary.Read(f, binary.BigEndian, &numMarks)

	// Each mark is 13 bytes: mark_type(1) + play_item_ref(2) + timestamp(4) + es_pid(2) + duration(4)
	marks := make([]PlaylistMark, numMarks)
	for i := range marks {
		var markType uint8
		var playItemRef uint16
		var ts uint32
		var esPid uint16
		var dur uint32

		binary.Read(f, binary.BigEndian, &markType)
		binary.Read(f, binary.BigEndian, &playItemRef)
		binary.Read(f, binary.BigEndian, &ts)
		binary.Read(f, binary.BigEndian, &esPid)
		binary.Read(f, binary.BigEndian, &dur)

		marks[i] = PlaylistMark{
			MarkType:    markType,
			PlayItemRef: playItemRef,
			Timestamp:   ts,
			Duration:    dur,
		}
	}
	return marks, nil
}

// LoadAllPlaylists loads and parses all .mpls files in the PLAYLIST directory.
func LoadAllPlaylists(bdmvRoot string) ([]*Playlist, error) {
	pattern := filepath.Join(bdmvRoot, "PLAYLIST", "*.mpls")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}

	var playlists []*Playlist
	for _, f := range files {
		pl, err := ParsePlaylist(f)
		if err != nil {
			continue
		}
		playlists = append(playlists, pl)
	}

	sort.Slice(playlists, func(i, j int) bool {
		return playlists[i].Name < playlists[j].Name
	})

	return playlists, nil
}
