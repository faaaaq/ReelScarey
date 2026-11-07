package transfer

import "testing"

func TestFeedTimeAcceptsProviderDateLayouts(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{"Thu, 22 Oct 2026 10:00:00 GMT", "2026-10-22T10:00:00Z"},
		{"Thu, 22 Oct 2026 10:00:00 +0000", "2026-10-22T10:00:00Z"},
		{"Thu, 22 Oct 2026 06:00:00 -0400", "2026-10-22T10:00:00Z"},
		{"Thu, 22 Oct 2026 10:00:00 +0000 (UTC)", "2026-10-22T10:00:00Z"},
		{"2026-10-22T10:00:00Z", "2026-10-22T10:00:00Z"},
		{"2026-10-22T10:00:00.125Z", "2026-10-22T10:00:00Z"},
		{"2026-10-22 10:00:00", "2026-10-22T10:00:00Z"},
		{"2026-10-22", "2026-10-22T00:00:00Z"},
	} {
		if got := feedTime(tc.raw); got != tc.want {
			t.Fatalf("feedTime(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
	for _, raw := range []string{"", "not a date", "0001-01-01T00:00:00Z", "2026-02-30T10:00:00Z"} {
		if got := feedTime(raw); got != "" {
			t.Fatalf("feedTime(%q) fabricated %q", raw, got)
		}
	}
}

func TestReleaseInfoURLRejectsUnsafeProviderValues(t *testing.T) {
	for _, raw := range []string{
		"https://tracker.invalid/details/1",
		"http://tracker.invalid/t/1?cat=1",
	} {
		if got := releaseInfoURL(raw); got != raw {
			t.Fatalf("releaseInfoURL(%q) = %q", raw, got)
		}
	}
	for _, raw := range []string{
		"", "javascript:alert(1)", "magnet:?xt=urn:btih:deadbeef",
		"https://user:pass@tracker.invalid/details/1", "/details/1",
		"https://tracker.invalid/details/\n1", "https:///missing-host",
		"https://tracker.invalid/details/1?passkey=private",
		"https://tracker.invalid/details/1?apikey=private",
	} {
		if got := releaseInfoURL(raw); got != "" {
			t.Fatalf("releaseInfoURL(%q) accepted unsafe value %q", raw, got)
		}
	}
}
