package transfer

import (
	"bytes"
	"encoding/json"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// hiddenURL replaces values which look like an address but cannot be reduced
// safely to a scheme and host.
const hiddenURL = "(hidden)"

var (
	trackerPseudoRE = regexp.MustCompile(`^\*\* \[[A-Za-z]{2,8}\] \*\*$`)
	trackerSchemes  = map[string]bool{"http": true, "https": true, "udp": true, "ws": true, "wss": true}
	trackerHostRE   = regexp.MustCompile(`^(\[[0-9A-Fa-f:.]+\]|[A-Za-z0-9.\-_]+)(:[0-9]{1,5})?$`)
)

// RedactTrackerURL reduces a tracker or web seed address to scheme://host[:port].
// Private trackers embed passkeys in the user information, path, query or
// fragment, so none of those parts are ever kept. qBittorrent's pseudo entries
// such as "** [DHT] **" are returned unchanged, and anything that cannot be
// parsed as an absolute tracker address becomes "(hidden)".
func RedactTrackerURL(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if trackerPseudoRE.MatchString(s) {
		return s
	}
	u, err := url.Parse(s)
	if err != nil || u.Opaque != "" || !trackerSchemes[strings.ToLower(u.Scheme)] || u.Hostname() == "" || !trackerHostRE.MatchString(u.Host) {
		return hiddenURL
	}
	return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host)
}

// redactURLsInText replaces every URL-like token in client-supplied text, such
// as a tracker message, with its redacted form.
func redactURLsInText(s string) string {
	if !strings.Contains(s, "://") {
		return s
	}
	fields := strings.FieldsFunc(s, unicode.IsSpace)
	for _, field := range fields {
		if !strings.Contains(field, "://") {
			continue
		}
		token := strings.Trim(field, `"'()<>[]{},;`)
		start := strings.Index(token, "://")
		// Walk back over scheme characters so "url=https://..." keeps its prefix.
		begin := start
		for begin > 0 && isSchemeByte(token[begin-1]) {
			begin--
		}
		replacement := token[:begin] + RedactTrackerURL(token[begin:])
		s = strings.Replace(s, token, replacement, 1)
	}
	return s
}

func isSchemeByte(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '+' || b == '-' || b == '.'
}

// cleanClientText bounds free text reported by the torrent client: it is made
// valid UTF-8, control characters other than newlines are replaced and the
// result is truncated to max runes.
func cleanClientText(s string, max int) string {
	s = strings.ToValidUTF8(s, "\uFFFD")
	s = strings.Map(func(r rune) rune {
		if r == '\n' {
			return r
		}
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	if utf8.RuneCountInString(s) > max {
		s = string([]rune(s)[:max])
	}
	return s
}

// flexInt decodes the integer fields qBittorrent sometimes serializes as a
// double (eta) or, in older releases, as an empty string (tracker tier).
type flexInt int64

func (f *flexInt) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if string(b) == "null" {
		*f = 0
		return nil
	}
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		if strings.TrimSpace(s) == "" {
			*f = -1
			return nil
		}
		n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
		if err != nil {
			return err
		}
		*f = flexInt(n)
		return nil
	}
	var v float64
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	if math.IsNaN(v) || v > math.MaxInt64/2 || v < math.MinInt64/2 {
		return &json.UnsupportedValueError{Str: string(b)}
	}
	*f = flexInt(int64(v))
	return nil
}
