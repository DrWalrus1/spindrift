# Spindrift

Spindrift reads Blu-ray discs and identifies their content — TV episodes or movies — by parsing the disc's BDMV structure and matching against the TMDB metadata API.

It works entirely from the disc's filesystem without needing to decrypt or demux any video streams, making it fast and non-destructive.

## Features

- Auto-detects mounted Blu-ray discs on macOS and Linux
- Parses `index.bdmv`, `MovieObject.bdmv`, and `.mpls` playlist files
- Infers episode duration bounds from stream file sizes using cluster analysis
- Detects combined streams (e.g. a 4-part finale encoded as one file)
- Distinguishes TV series from movies automatically
- Detects commentary tracks and links them to their source episodes
- Looks up episode and movie titles via the TMDB API
- Outputs structured JSON for programmatic use (`-r` robot mode)
- Usable as both a standalone CLI tool and a Go library

## Installation

### CLI
```bash
go install github.com/DrWalrus1/spindrift/cmd/spindrift@latest
```

### Library
```bash
go get github.com/DrWalrus1/spindrift
```

## CLI Usage
```bash
# Auto-detect a mounted disc
spindrift

# Explicit disc path
spindrift /Volumes/Avatar_Book_2_Disc_1

# With episode offset for later discs in a set
spindrift /Volumes/Avatar_Book_2_Disc_2 --start-episode 9
spindrift /Volumes/Avatar_Book_2_Disc_3 --start-episode 17

# JSON output for programmatic use
spindrift -r /Volumes/Avatar_Book_2_Disc_1
```

### Flags

| Flag | Description |
|---|---|
| `-r` | Robot mode: output JSON instead of a human-readable table |
| `--start-episode N` | Episode number offset for later discs in a multi-disc set |

### Example output — TV series
```
Found disc: /Volumes/Avatar_Book_2_Disc_1/BDMV
Disc Title: Avatar: The Last Airbender
Season:     2
Disc:       1

Inferred episode bounds: 16:54 – 27:02 (cluster center: 21:21)
Found 8 episodes on disc

TMDB Match: Avatar: The Last Airbender (ID: 246)

Ep     Playlist       Clip       Duration     Chapters   Episode ID   Note                   Title
---------------------------------------------------------------------------------------------------------------------
S02E01 01606          01090      22:29        5          1234567                             The Avatar State
S02E02 01607          01091      22:18        5          1234568                             The Cave of Two Lovers
...
S02E01 01650          01090      22:29        5                       commentary for S02E01
```

### Example output — Movie
```
Found disc: /Volumes/PRINCESS MONONOKE/BDMV
Disc Title: Princess Mononoke
Detected: Movie

TMDB Match: Princess Mononoke (ID: 128, Runtime: 134 min)

Type       Playlist     Duration     Chapters   Title
-----------------------------------------------------------------
Movie      00009        133:35       12         Princess Mononoke
```

### JSON output (`-r`)

Each element in the output array corresponds to one playlist. Fields are omitted when empty.

```json
[
  {
    "playlist": "01606",
    "clip": "01090",
    "type": "tv",
    "disc_title": "Avatar: The Last Airbender",
    "episode": "S02E01",
    "title": "The Avatar State",
    "duration": "22:29",
    "tmdb_duration": "22:00",
    "chapters": 5,
    "tmdb_id": "246",
    "tmdb_episode_id": "1234567"
  },
  {
    "playlist": "01650",
    "clip": "01090",
    "type": "tv",
    "disc_title": "Avatar: The Last Airbender",
    "episode": "S02E01",
    "duration": "22:29",
    "chapters": 5,
    "tmdb_id": "246",
    "note": "commentary for S02E01"
  }
]
```

## Configuration

Spindrift reads a `.env` file from the current working directory. Copy `.env.example` and fill in your TMDB API key:
```bash
cp .env.example .env
```
```
# .env
TMDB_API_KEY=your_read_access_token_here
```

You can also set the key directly in your environment:
```bash
export TMDB_API_KEY=your_read_access_token_here
```

Get a free API key at [themoviedb.org/settings/api](https://www.themoviedb.org/settings/api). Use the **API Read Access Token** (the long JWT), not the shorter API key.

## Library Usage
```go
import (
    "github.com/DrWalrus1/spindrift/bdmv"
    "github.com/DrWalrus1/spindrift/disc"
    "github.com/DrWalrus1/spindrift/tmdb"
)

// Open a disc
d, err := disc.Open("/Volumes/Avatar_Book_2_Disc_1/BDMV")
if err != nil {
    log.Fatal(err)
}

fmt.Println(d.Info.ShowName) // "Avatar: The Last Airbender"
fmt.Println(d.Info.Season)   // 2
fmt.Println(d.Info.Disc)     // 1
fmt.Println(d.Info.IsMovie)  // false
fmt.Println(d.Info.IsSeries) // true

// Infer episode bounds and load episodes
minDur, maxDur, clusterDur := disc.InferEpisodeBounds(d.BDMVRoot)
episodes, err := disc.LoadEpisodePlaylists(d.BDMVRoot, minDur, maxDur, clusterDur)

for _, ep := range episodes {
    fmt.Printf("%s → %s (%s)",
        ep.Name,
        ep.PrimaryClip(),
        bdmv.FormatDuration(ep.EstimateDuration(d.BDMVRoot, disc.DefaultBitrate)),
    )
    if ep.Note != "" {
        fmt.Printf(" [%s, source clip: %s]", ep.Note, ep.NoteClip)
    }
    fmt.Println()
}

// Detect whether the disc is a movie
d.Info.DetectMovie(len(episodes))

// Look up episode titles via TMDB
client := tmdb.New(os.Getenv("TMDB_API_KEY"))

// Smart search degrades the query word-by-word until results are found
shows, matchedQuery, err := client.SmartSearchTV(d.Info.ShowName)

// Fetch season, matching by name if the disc title contains one (e.g. "Book Two")
season, seasonNum, err := client.SmartGetSeason(shows[0].ID, d.Info.ShowName, d.Info.Season)

tmdbEps := tmdb.EpisodesForDisc(season, 0, len(episodes))
for i, ep := range episodes {
    if ep.Note != "" {
        continue // skip commentary tracks
    }
    fmt.Printf("%s: %s\n", ep.Name, tmdbEps[i].Name)
}

// Movie workflow
movies, _, err := client.SmartSearchMovie(d.Info.ShowName)
details, err := client.GetMovie(movies[0].ID)
fmt.Printf("%s (%d min)\n", details.Title, details.Runtime)
```

### Disc auto-detection

```go
// Find all mounted Blu-ray discs
roots, err := disc.FindBDMVRoots()

// Or let the user pick interactively (prompts if multiple discs found)
bdmvRoot, err := disc.SelectBDMV("") // "" = auto-detect
bdmvRoot, err := disc.SelectBDMV("/Volumes/MyDisc") // explicit path
```

### Low-level access

```go
// Load all playlists from a disc
playlists, err := bdmv.LoadAllPlaylists(bdmvRoot)

// Parse a single playlist file
pl, err := bdmv.ParsePlaylist("/path/to/PLAYLIST/01606.mpls")

// Raw stream duration analysis
durations := disc.StreamDurations(bdmvRoot)
minDur, maxDur, center := disc.DominantCluster(durations)

// Clip bitrate from CLPI file (bytes/sec); 0 if unavailable
rate := bdmv.ClipBitrate(bdmvRoot, "01090")

// Parse disc metadata from a title string directly
info := disc.ParseDiscInfo("Avatar: The Last Airbender Book Two: Earth Disc 1")
// info.ShowName == "Avatar: The Last Airbender"
// info.Season   == 2
// info.Disc     == 1
```

## Packages

| Package | Description |
|---|---|
| `bdmv` | Parses Blu-ray BDMV files: `index.bdmv`, `MovieObject.bdmv`, `.mpls` playlists |
| `disc` | Disc detection, episode clustering, duration inference, playlist loading |
| `tmdb` | TMDB API client for TV and movie metadata lookup |
| `env` | `.env` file loader |

## How It Works

### Episode detection

Spindrift avoids decrypting or reading video streams directly. Instead it estimates content duration from stream file sizes at a standard Blu-ray bitrate (~35 Mbps), using per-clip bitrates from `.clpi` files when available.

It then uses a **dominant cluster algorithm** to find the most likely episode duration on the disc:

1. Collect estimated durations for all unique stream files
2. Remove streams that are near-integer multiples of smaller streams (play-all and combined streams)
3. Find the cluster of similar durations with the highest total content weight
4. Expand the cluster bounds by ±20% to account for variable episode lengths

### Multi-episode streams

Some discs encode multiple episodes into a single stream file. Spindrift detects these by checking if a stream's estimated duration is a near-integer multiple of the cluster episode duration, then expands that stream into the appropriate number of episode entries. As a fallback, chapter mark spacing is used to infer episode boundaries when duration estimation is insufficient.

### Commentary track detection

When loading playlists, Spindrift tracks which video clips have already been assigned to an episode. If a playlist's primary clip is new but one of its other clips is a previously seen episode clip, the playlist is marked as a commentary track. Its `Note` field is set to `"commentary"` and `NoteClip` records the clip ID of the underlying episode.

### Movie detection

If the disc title contains no season or disc markers and only one episode-length stream is found, the disc is treated as a movie and looked up via TMDB's movie API instead of the TV API.

## Test Volumes

The `test_volumes/` directory holds lightweight disc snapshots used for development and testing. Each volume contains the real BDMV metadata (playlists, clip info, disc title XML) but no actual video — stream files are replaced with sparse stubs that match the originals in size.

### Creating a test volume

Insert a disc and run:
```bash
./mktestvol.sh                          # auto-detect all mounted discs
./mktestvol.sh /Volumes/MyDisc          # explicit path
```

The script copies all metadata, creates sparse `.m2ts` stubs, bundles everything except the stubs into `metadata.tar`, and ejects the disc when done.

### What gets committed to git

Git does not preserve sparse files — committing a 50 GB stub would store 50 GB of zeros. To avoid this, `BDMV/` is gitignored entirely. Only one file per volume is committed:

- `metadata.tar` — contains `BDMV/PLAYLIST/`, `BDMV/CLIPINF/`, `BDMV/*.bdmv`, `BDMV/META/` (if present), and `BDMV/STREAM/sizes.txt` (a `filename size` manifest used to regenerate stubs)

### Restoring a volume after a fresh clone

After cloning, recreate `BDMV/` and the stream stubs from the committed tar:
```bash
./mktestvol.sh --restore                # restore all volumes in test_volumes/
./mktestvol.sh --restore MyDisc         # restore a specific volume by name
```

## Known Limitations

- **Duration estimates** are based on file size and assume a fixed bitrate. Actual durations may differ by a few minutes. Encrypted streams prevent direct PTS timestamp parsing.
- **Episode offset** for later discs in a multi-disc set must be provided manually via `--start-episode`, since discs don't encode their position in a set.
- **Movie detection** uses a simple heuristic and may misidentify single-episode TV discs as movies.
- **AACS-encrypted discs** are supported for metadata reading but video streams cannot be read directly.

## Platform Support

| Platform | Auto-detection paths |
|---|---|
| macOS | `/Volumes/*` |
| Linux | `/media/*`, `/run/media/*/*`, `/mnt/*` |

## License

MIT
