package quality

import (
	"regexp"
	"strconv"
	"strings"
)

// packMarker recognizes season and collection wording that identifies a
// multi-episode release. A title that names one exact episode is never a pack,
// even when it also contains a word such as "Complete".
var packMarker = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(?:s[0-9]{1,3}|season[ ._-]*[0-9]{1,3}|pack|batch|collection|complete[ ._-]+(?:series|collection|season))(?:$|[^a-z0-9])`)

// PackTitle reports whether a release title represents a season pack, a range
// of episodes, or a complete-series/collection release rather than one exact
// episode.
func PackTitle(title string) bool {
	if _, _, _, _, ok := EpisodeLabel(title); ok {
		return false
	}
	if _, episodes, _, _, ok := EpisodeSpan(title); ok && len(episodes) > 1 {
		return true
	}
	return packMarker.MatchString(title)
}

var packSeason = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])s(?:eason)?[ ._-]*([0-9]{1,3})`)

// identifyPack verifies the show title and season of a pack release, then
// returns its remaining labels for the normal quality rules. A season-specific
// target rejects a different season; complete-series and collection packs are
// accepted for any season.
func identifyPack(title string, t Target) (string, bool) {
	if !t.TV || t.Title == "" || !PackTitle(title) {
		return title, false
	}
	norm := Normalize(title)
	base := Normalize(t.Title)
	if base == "" {
		return title, false
	}
	idx := -1
	switch {
	case norm == base:
		idx = 0
	case strings.HasPrefix(norm, base+" "):
		idx = 0
	default:
		if pos := strings.Index(norm, " "+base+" "); pos >= 0 {
			idx = pos + 1
		}
	}
	if idx < 0 {
		return title, false
	}
	if t.Season > 0 {
		m := packSeason.FindStringSubmatch(title)
		if len(m) == 2 {
			season, err := strconv.Atoi(m[1])
			if err != nil || season != t.Season {
				return title, false
			}
		} else if !strings.Contains(norm, "complete") && !strings.Contains(norm, "collection") {
			return title, false
		}
	}
	label := strings.TrimSpace(norm[idx+len(base):])
	// The same 180-character bound mapped_anime.go applies to provider-facing
	// titles caps the extracted pack label so oversized garbage never becomes
	// a quality rule input.
	if len(label) > 180 {
		return title, false
	}
	return label, true
}
