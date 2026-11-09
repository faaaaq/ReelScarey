package releaseguard

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

const (
	maxIdentityBytes   = 512
	maxReleaseKeyBytes = 512
)

// TorrentKey normalizes and validates a BitTorrent v1 (40 hex characters) or
// v2 (64 hex characters) infohash. The returned key is lowercase so callers
// compare one stable release identity.
func TorrentKey(infoHash string) (string, error) {
	key := strings.ToLower(strings.TrimSpace(infoHash))
	if len(key) != 40 && len(key) != 64 {
		return "", invalid("torrent infohash must contain 40 or 64 hexadecimal characters")
	}
	if _, err := hex.DecodeString(key); err != nil {
		return "", invalid("torrent infohash contains non-hexadecimal characters")
	}
	return key, nil
}

// UsenetKey hashes a bounded NZB GUID/job identity with SHA-256. GUIDs are
// different lengths and may contain characters that do not belong in a stable
// release key, so the digest is the key. The GUID is not normalized beyond
// trimming surrounding whitespace because provider job IDs are case-sensitive.
func UsenetKey(guid string) (string, error) {
	guid = strings.TrimSpace(guid)
	if guid == "" {
		return "", invalid("NZB GUID is required")
	}
	if len(guid) > maxIdentityBytes {
		return "", invalid("NZB GUID is too long")
	}
	sum := sha256.Sum256([]byte(guid))
	return hex.EncodeToString(sum[:]), nil
}

// UsenetTitleKey is the fallback identity for providers that do not supply a
// usable GUID. It normalizes case and whitespace, then hashes a domain-separated
// title so a title can never collide with a GUID supplied to UsenetKey.
func UsenetTitleKey(title string) (string, error) {
	normalized := strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(title)), " "))
	if normalized == "" {
		return "", invalid("release title is required")
	}
	if len(normalized) > maxIdentityBytes {
		return "", invalid("release title is too long")
	}
	sum := sha256.Sum256([]byte("title\x00" + normalized))
	return hex.EncodeToString(sum[:]), nil
}
