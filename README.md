# Spindrift

Spindrift reads Blu-ray discs and identifies their content — TV episodes or movies — by parsing the disc's BDMV structure and matching against the TMDB metadata API.

It works entirely from the disc's filesystem without needing to decrypt or demux any video streams, making it fast and non-destructive.

## Features

- Auto-detects mounted Blu-ray discs on macOS and Linux
- Parses `index.bdmv`, `MovieObject.bdmv`, and `.mpls` playlist files
- Infers episode duration bounds from stream file sizes using cluster analysis
- Detects combined streams (e.g. a 4-part finale encoded as one file)
- Distinguishes TV series from movies automatically
- Looks up episode and movie titles via the TMDB API
- Usable as both a standalone CLI tool and a Go library

## Installation

### CLI
```bash
go install github.com/lukelynch/spindrift/cmd/spindrift@latest
```

### Library
```bash
go get github.com/lukelynch/spindrift
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
```

### Example output — TV series
```
Found disc: /Volumes/Avatar_Book_2_Disc_1
Disc Title: Avatar: The Last Airbender Book Two: Earth Disc 1
Show:       Avatar: The Last Airbender
Season:     2
Disc:       1

Inferred episode bounds: 16:54 – 27:02 (cluster center: 21:21)
Found 8 episodes on disc

TMDB Match: Avatar: The Last Airbender (ID: 246)

Ep     Playlist       Clip       Duration     Title
------------------------------------------------------------------------
S02E01 01606          01090      22:29        The Avatar State
S02E02 01607          01091      22:18        The Cave of Two Lovers
S02E03 01608          01092      22:32        Return to Omashu
S02E04 01623          01099      21:12        The Swamp
S02E05 01624          01100      21:13        Avatar Day
S02E06 01625          01101      21:13        The Blind Bandit
S02E07 01626          01102      21:11        Zuko Alone
S02E08 01627          01103      21:08        The Chase
```

### Example output — Movie
```
Found disc: /Volumes/PRINCESS MONONOKE
Disc Title: Princess Mononoke
Detected: Movie

TMDB Match: Princess Mononoke (ID: 128, Runtime: 134 min)

Type       Playlist     Duration     Title
-------------------------------------------------------
Movie      00009        133:35       Princess Mononoke
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
    "github.com/lukelynch/spindrift/bdmv"
    "github.com/lukelynch/spindrift/disc"
    "github.com/lukelynch/spindrift/tmdb"
)

// Open a disc
d, err := disc.Open("/Volumes/Avatar_Book_2_Disc_1/BDMV")
if err != nil {
    log.Fatal(err)
}

fmt.Println(d.Info.ShowName) // "Avatar: The Last Airbender"
fmt.Println(d.Info.Season)   // 2
fmt.Println(d.Info.Disc)     // 1

// Infer episode bounds and load episodes
minDur, maxDur, clusterDur := disc.InferEpisodeBounds(d.BDMVRoot)
episodes, err := disc.LoadEpisodePlaylists(d.BDMVRoot, minDur, maxDur, clusterDur)

for _, ep := range episodes {
    fmt.Printf("%s → %s (%s)\n",
        ep.Name,
        ep.PrimaryClip(),
        bdmv.FormatDuration(ep.EstimateDuration(d.BDMVRoot, disc.DefaultBitrate)),
    )
}

// Look up episode titles
client := tmdb.New(os.Getenv("TMDB_API_KEY"))
shows, err := client.SearchTV(d.Info.ShowName)
season, err := client.GetSeason(shows[0].ID, d.Info.Season)
tmdbEps := tmdb.EpisodesForDisc(season, 0, len(episodes))

for i, ep := range episodes {
    fmt.Printf("%s: %s\n", ep.Name, tmdbEps[i].Name)
}
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

Spindrift avoids decrypting or reading video streams directly. Instead it estimates content duration from stream file sizes at a standard Blu-ray bitrate (~35 Mbps).

It then uses a **dominant cluster algorithm** to find the most likely episode duration on the disc:

1. Collect estimated durations for all unique stream files
2. Remove streams that are near-integer multiples of smaller streams (play-all and combined streams)
3. Find the cluster of similar durations with the highest total content weight
4. Expand the cluster bounds by ±20% to account for variable episode lengths

### Multi-episode streams

Some discs encode multiple episodes into a single stream file. Spindrift detects these by checking if a stream's estimated duration is a near-integer multiple of the cluster episode duration, then expands that stream into the appropriate number of episode entries.

### Movie detection

If the disc title contains no season or disc markers and only one episode-length stream is found, the disc is treated as a movie and looked up via TMDB's movie API instead of the TV API.

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
