package bdmv

const (
	// BDMV file type indicators
	TypeIndicatorINDX = "INDX"
	TypeIndicatorMOBJ = "MOBJ"
	TypeIndicatorMPLS = "MPLS"

	// index.bdmv parsing
	indexAppInfoBodyOffset = 4 + 16 // length(4) + reserved(16)

	// Title entry object types
	ObjectTypeHDMVFirstPlay = 0x40
	ObjectTypeHDMV          = 0xA0
	ObjectTypeBDJ           = 0x60
	ObjectTypeMask          = 0xE0

	// MovieObject.bdmv parsing
	movieObjectTableOffset = 0x28

	// MovieObject flags
	mobjFlagResumeIntention = 0x8000
	mobjFlagMenuCallMask    = 0x4000
	mobjFlagTitleSearchMask = 0x2000

	// Navigation command size in bytes
	navCommandSize = 12

	// Playlist parsing
	playlistOffsetAddr     = 0x08
	playlistMarkOffsetAddr = 0x0C
	playlistHeaderSkip     = 4 + 2 // length(4) + reserved(2)
	playItemClipNameLen    = 9
	playItemClipNameUsed   = 5 // just the number part e.g. "01061"
	playItemTimestampSkip  = 4 // reserved(1) + connection(1) + stc_id(1) + reserved(1)

	// PTS clock rate for Blu-ray (45kHz)
	PTSClock = 45000

	// CLPI parsing
	// TS_recording_rate is stored at a fixed offset within the pre-ClipInfo
	// block that begins at 0x28 in all observed CLPI version 0200 files.
	// The block has a 4-byte length field followed by 8 bytes of other data,
	// placing TS_recording_rate at absolute file offset 0x34.
	clpiTSRateOffset = 0x34

	// Sanity bounds for TS_recording_rate (bytes/sec).
	// Standard BD is typically ~4–7 MB/s; UHD BD up to ~15 MB/s.
	clpiMinRate = 1_250_000  // 10 Mbps
	clpiMaxRate = 50_000_000 // 400 Mbps
)
