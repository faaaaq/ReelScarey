package transfer

import (
	"net/url"
	"strings"
	"time"
	"unicode"
)

// feedTime converts a provider-supplied publication date to RFC3339 UTC.
// net/http.ParseTime only accepts the three HTTP Date layouts, but Torznab,
// Newznab and RSS feeds commonly publish RFC1123Z, RFC3339 or ISO-like dates.
// An unparseable or missing date stays empty; it is never replaced with now.
func feedTime(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	// Some feeds append a timezone name in parentheses, for example
	// "Thu, 22 Oct 2026 10:00:00 +0000 (UTC)".
	if i := strings.LastIndex(raw, " ("); i > 0 && strings.HasSuffix(raw, ")") {
		raw = strings.TrimSpace(raw[:i])
	}
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		time.RFC1123Z,
		time.RFC1123,
		time.RFC822Z,
		time.RFC822,
		time.RFC850,
		time.ANSIC,
		"Mon, 2 Jan 2006 15:04:05 -0700",
		"Mon, 2 Jan 2006 15:04:05 MST",
		"2 Jan 2006 15:04:05 -0700",
		"2006-01-02 15:04:05 -0700",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
		time.DateOnly,
	} {
		if parsed, err := time.Parse(layout, raw); err == nil && !parsed.IsZero() {
			return parsed.UTC().Format(time.RFC3339)
		}
	}
	return ""
}

// releaseInfoURL accepts only an absolute HTTP(S) details page supplied by the
// provider. It never constructs or fetches a URL and never accepts credentials.
func releaseInfoURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 8192 || strings.ContainsAny(raw, "\r\n\\\x00") || strings.ContainsFunc(raw, unicode.IsControl) {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return ""
	}
	// Provider feeds occasionally carry API keys or private-tracker passkeys
	// in a details URL. Never place those in a browser response.
	for key := range u.Query() {
		lower := strings.ToLower(key)
		for _, sensitive := range []string{"apikey", "api_key", "passkey", "rsskey", "token", "secret", "signature"} {
			if strings.Contains(lower, sensitive) {
				return ""
			}
		}
	}
	return u.String()
}
