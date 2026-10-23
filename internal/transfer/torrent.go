package transfer

import (
	"crypto/sha1"
	"encoding/base32"
	"encoding/hex"
	"net/url"
	"strconv"
	"strings"
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
