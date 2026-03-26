package bdmv

import (
	"encoding/binary"
	"os"
	"path/filepath"
)

// ClipBitrate returns the TS recording rate in bytes/sec for a given clip
// by reading its .clpi file. Returns 0 if the file cannot be read or the
// value is outside the expected range.
func ClipBitrate(bdmvRoot, clipName string) int {
	path := filepath.Join(bdmvRoot, "CLIPINF", clipName+".clpi")
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()

	// Verify this is an HDMV CLPI file.
	hdr := make([]byte, 8)
	if _, err := f.Read(hdr); err != nil {
		return 0
	}
	if string(hdr[:4]) != "HDMV" {
		return 0
	}

	if _, err := f.Seek(clpiTSRateOffset, 0); err != nil {
		return 0
	}
	var rate uint32
	if err := binary.Read(f, binary.BigEndian, &rate); err != nil {
		return 0
	}

	r := int(rate)
	if r < clpiMinRate || r > clpiMaxRate {
		return 0
	}
	return r
}
