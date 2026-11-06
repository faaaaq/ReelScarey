package transfer

import (
	"crypto/sha1"
	"encoding/base32"
	"encoding/hex"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
)

func MagnetHash(raw string) (string, error) {
	bad := func() (string, error) {
		return "", problem("unsupported_torrent", "Use a v1 or hybrid BitTorrent release with a valid info hash.")
	}
	if !textOK(raw, 16384) {
		return bad()
	}
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "magnet" || u.Host != "" {
		return bad()
	}
	hash := ""
	for _, xt := range u.Query()["xt"] {
		if !strings.HasPrefix(strings.ToLower(xt), "urn:btih:") {
			continue
		}
		v := xt[9:]
		var b []byte
		if len(v) == 40 {
			b, e = hex.DecodeString(v)
		} else if len(v) == 32 {
			b, e = base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(v))
		} else {
			return bad()
		}
		if e != nil || len(b) != 20 {
			return bad()
		}
		h := hex.EncodeToString(b)
		if hash != "" && hash != h {
			return bad()
		}
		hash = h
	}
	if hash == "" {
		return bad()
	}
	return hash, nil
}

// manualTorrent is the bounded extraction an owner-supplied .torrent file
// needs: the display name, payload size, file count and the v1 infohash used
// for duplicate prevention and reconcile-by-hash.
type manualTorrent struct {
	Name     string
	Size     int64
	Files    int
	InfoHash string
	Private  bool
}

// TorrentInfo validates uploaded metainfo and derives the identity the
// download pipeline records. The hash covers the exact bencoded info
// dictionary, never a re-encoding. BitTorrent v2-only files carry no v1
// pieces and are refused, because the app could not reconcile their swarm
// identity. Bounds reject malformed, deeply nested and oversized metadata.
func TorrentInfo(body []byte) (manualTorrent, error) {
	var out manualTorrent
	if len(body) < 16 || len(body) > 4<<20 || body[0] != 'd' {
		return out, problem("invalid_torrent", "Upload a .torrent file between 16 bytes and 4 MiB.")
	}
	p := bparser{b: body, i: 1}
	var info []byte
	seen := map[string]bool{}
	for p.i < len(body) && body[p.i] != 'e' {
		key, ok := p.str()
		if !ok || seen[string(key)] {
			return out, problem("invalid_torrent", "The uploaded file is not a readable .torrent file.")
		}
		seen[string(key)] = true
		start := p.i
		if !p.value(0) {
			return out, problem("invalid_torrent", "The uploaded file is not a readable .torrent file.")
		}
		if string(key) == "info" {
			if start >= len(body) || body[start] != 'd' {
				return out, problem("invalid_torrent", "The uploaded file is not a readable .torrent file.")
			}
			info = body[start:p.i]
		}
	}
	if p.i != len(body)-1 || body[p.i] != 'e' || len(info) == 0 {
		return out, problem("invalid_torrent", "The uploaded file is not a readable .torrent file.")
	}
	if !infoHasPieces(info) {
		return out, problem("unsupported_torrent", "This torrent uses BitTorrent v2 without v1 pieces, which this app cannot track reliably. Upload a v1 or hybrid torrent.")
	}
	name, size, files, priv, e := torrentInfoFields(info)
	if e != nil {
		return out, e
	}
	sum := sha1.Sum(info)
	out.Name, out.Size, out.Files, out.Private, out.InfoHash = name, size, files, priv, hex.EncodeToString(sum[:])
	return out, nil
}

// infoHasPieces reports whether the info dictionary carries v1 piece hashes,
// possibly alongside v2 file trees in a hybrid torrent.
func infoHasPieces(info []byte) bool {
	p := bparser{b: info, i: 1}
	for p.i < len(info) && info[p.i] != 'e' {
		key, ok := p.str()
		if !ok {
			return false
		}
		if string(key) == "pieces" {
			v, ok := p.str()
			return ok && len(v) >= 20 && len(v)%20 == 0
		}
		if !p.value(0) {
			return false
		}
	}
	return false
}

// torrentInfoFields walks the info dictionary for the display name, total
// payload size and file count. Multi-file torrents sum their entries; a
// missing or unreadable layout is rejected.
func torrentInfoFields(info []byte) (string, int64, int, bool, error) {
	bad := problem("invalid_torrent", "The torrent has no readable name or file entries.")
	p := bparser{b: info, i: 1}
	name := ""
	size := int64(0)
	files := 0
	priv := false
	for p.i < len(info) && info[p.i] != 'e' {
		key, ok := p.str()
		if !ok {
			return "", 0, 0, false, bad
		}
		switch string(key) {
		case "name":
			v, ok := p.str()
			if !ok || len(v) == 0 || len(v) > 300 || !utf8.Valid(v) {
				return "", 0, 0, false, bad
			}
			name = string(v)
		case "length":
			n, ok := p.int()
			if !ok || n < 0 {
				return "", 0, 0, false, bad
			}
			size, files = n, 1
		case "files":
			if p.i >= len(info) || info[p.i] != 'l' {
				return "", 0, 0, false, bad
			}
			p.i++
			count := 0
			for p.i < len(info) && info[p.i] != 'e' {
				start := p.i
				if !p.value(0) {
					return "", 0, 0, false, bad
				}
				if info[start] != 'd' {
					return "", 0, 0, false, bad
				}
				entry := bparser{b: info, i: start + 1}
				length := int64(-1)
				for entry.i < p.i-1 && info[entry.i] != 'e' {
					k, ok := entry.str()
					if !ok {
						return "", 0, 0, false, bad
					}
					if string(k) == "length" {
						n, ok := entry.int()
						if !ok || n < 0 {
							return "", 0, 0, false, bad
						}
						length = n
					} else if !entry.value(0) {
						return "", 0, 0, false, bad
					}
				}
				if length < 0 {
					return "", 0, 0, false, bad
				}
				size += length
				if size < 0 {
					return "", 0, 0, false, bad
				}
				count++
				if count > 2000 {
					return "", 0, 0, false, problem("invalid_torrent", "The torrent contains more than 2000 files. Split it or choose a smaller selection.")
				}
			}
			p.i++
			if count == 0 {
				return "", 0, 0, false, bad
			}
			files = count
		case "private":
			n, ok := p.int()
			if !ok {
				return "", 0, 0, false, bad
			}
			priv = n == 1
		default:
			if !p.value(0) {
				return "", 0, 0, false, bad
			}
		}
	}
	if name == "" || files == 0 {
		return "", 0, 0, false, bad
	}
	return name, size, files, priv, nil
}

// The hash covers the original bencoded info dictionary, never a re-encoding.
// Bounds reject malformed, deeply nested and oversized metadata before enqueue.
func TorrentHash(body []byte) (string, error) {
	bad := problem("invalid_torrent", "The indexer did not return valid v1 or hybrid torrent metadata.")
	if len(body) == 0 || len(body) > 4<<20 || body[0] != 'd' {
		return "", bad
	}
	p := bparser{b: body}
	p.i = 1
	var info []byte
	seen := map[string]bool{}
	for p.i < len(body) && body[p.i] != 'e' {
		key, ok := p.str()
		if !ok || seen[string(key)] {
			return "", bad
		}
		seen[string(key)] = true
		start := p.i
		if !p.value(0) {
			return "", bad
		}
		if string(key) == "info" {
			if start >= len(body) || body[start] != 'd' {
				return "", bad
			}
			info = body[start:p.i]
		}
	}
	if p.i != len(body)-1 || body[p.i] != 'e' || len(info) == 0 {
		return "", bad
	}
	// v2-only torrents cannot be reconciled through the v1 info hash in this milestone.
	v := bparser{b: info, i: 1}
	hasPieces := false
	for v.i < len(info) && info[v.i] != 'e' {
		k, ok := v.str()
		if !ok {
			return "", bad
		}
		if string(k) == "pieces" {
			pieces, ok := v.str()
			if !ok || len(pieces)%20 != 0 {
				return "", bad
			}
			hasPieces = true
		} else if !v.value(0) {
			return "", bad
		}
	}
	if !hasPieces {
		return "", bad
	}
	sum := sha1.Sum(info)
	return hex.EncodeToString(sum[:]), nil
}

type bparser struct {
	b    []byte
	i, n int
}

func (p *bparser) str() ([]byte, bool) {
	start := p.i
	for p.i < len(p.b) && p.b[p.i] >= '0' && p.b[p.i] <= '9' {
		p.i++
	}
	if p.i == start || p.i >= len(p.b) || p.b[p.i] != ':' || p.i-start > 8 {
		return nil, false
	}
	s := string(p.b[start:p.i])
	if len(s) > 1 && s[0] == '0' {
		return nil, false
	}
	n, e := strconv.Atoi(s)
	p.i++
	if e != nil || n > len(p.b)-p.i {
		return nil, false
	}
	out := p.b[p.i : p.i+n]
	p.i += n
	return out, true
}
func (p *bparser) int() (int64, bool) {
	if p.i >= len(p.b) || p.b[p.i] != 'i' {
		return 0, false
	}
	p.i++
	start := p.i
	for p.i < len(p.b) && p.b[p.i] != 'e' {
		p.i++
	}
	if p.i >= len(p.b) {
		return 0, false
	}
	s := string(p.b[start:p.i])
	n, e := strconv.ParseInt(s, 10, 64)
	p.i++
	if e != nil || s == "-0" || (len(s) > 1 && s[0] == '0') {
		return 0, false
	}
	return n, true
}
func (p *bparser) value(depth int) bool {
	p.n++
	if depth > 32 || p.n > 100000 || p.i >= len(p.b) {
		return false
	}
	switch p.b[p.i] {
	case 'i':
		p.i++
		start := p.i
		for p.i < len(p.b) && p.b[p.i] != 'e' {
			p.i++
		}
		if p.i == len(p.b) {
			return false
		}
		s := string(p.b[start:p.i])
		if _, e := strconv.ParseInt(s, 10, 64); e != nil || s == "-0" || (len(s) > 1 && s[0] == '0') {
			return false
		}
		p.i++
		return true
	case 'l', 'd':
		dict := p.b[p.i] == 'd'
		p.i++
		seen := map[string]bool{}
		for p.i < len(p.b) && p.b[p.i] != 'e' {
			if dict {
				k, ok := p.str()
				if !ok || seen[string(k)] {
					return false
				}
				seen[string(k)] = true
			}
			if !p.value(depth + 1) {
				return false
			}
		}
		if p.i >= len(p.b) {
			return false
		}
		p.i++
		return true
	default:
		_, ok := p.str()
		return ok
	}
}
