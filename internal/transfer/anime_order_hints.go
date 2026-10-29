package transfer

import (
	"path"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/faaaaq/ReelScarey/internal/library"
	"github.com/faaaaq/ReelScarey/internal/quality"
)

var orderFileLabel = regexp.MustCompile(`(?i)(?:S[0-9]{1,5}E[0-9]{1,6}|(?:SP|OVA|ONA|OAD)[0-9]{1,6}|[0-9]{1,6})(?:v[1-9][0-9]?)?`)
var orderVersion = regexp.MustCompile(`(?i)v[1-9][0-9]?$`)
var orderExtraLabel = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(?:[0-9]+|(?:SP|OVA|ONA|OAD|E)[0-9]+|S[0-9]+E[0-9]+)(?:$|[^a-z0-9])`)

func orderWordBefore(v string, n int) bool {
	if n == 0 {
		return false
	}
	r, _ := utf8.DecodeLastRuneInString(v[:n])
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}
func orderWordAfter(v string, n int) bool {
	if n >= len(v) {
		return false
	}
	r, _ := utf8.DecodeRuneInString(v[n:])
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}
func orderTitleMatches(prefix string, entry library.AnimeOrderEntry, snapshot *library.AnimeOrderSnapshot) bool {
	prefix = quality.Normalize(prefix)
	title := quality.Normalize(entry.ReleaseTitle)
	if title == "" {
		return false
	}
	if prefix == title {
		return true
	}
	for _, identity := range snapshot.Identities {
		if identity.ID == entry.ProviderID && identity.Year != nil && prefix == title+" "+strconv.Itoa(*identity.Year) {
			return true
		}
	}
	return false
}

// Suggestions are deliberately limited to one exact saved source label. A
// multi-episode file, unfamiliar label or conflicting source order stays blank
// until the owner maps it. Suggestions never authorize publication or imports.
func suggestedOrderEpisodes(name string, targets []TVTarget, snapshot *library.AnimeOrderSnapshot, canonicalTitle string) []int64 {
	if snapshot == nil {
		return suggestedEpisodes(name, targets)
	}
	base := path.Base(strings.ReplaceAll(name, "\\", "/"))
	base = strings.TrimSuffix(base, path.Ext(base))
	if strings.HasPrefix(base, "[") {
		end := strings.IndexByte(base, ']')
		if end < 2 || end > 81 {
			return nil
		}
		group := base[1:end]
		if _, isLabel := library.NormalizeAnimeOrderLabel(group); isLabel || strings.ContainsAny(group, "[]") {
			return nil
		}
		base = strings.TrimSpace(base[end+1:])
		if strings.HasPrefix(base, "[") {
			return nil
		}
	}
	selected := map[int64]bool{}
	for _, target := range targets {
		selected[target.EpisodeID] = true
	}
	matched := map[int64]bool{}
	knownPrefix := false
	for _, where := range orderFileLabel.FindAllStringIndex(base, -1) {
		if orderWordBefore(base, where[0]) || orderWordAfter(base, where[1]) {
			continue
		}
		label, valid := library.NormalizeAnimeOrderLabel(orderVersion.ReplaceAllString(base[where[0]:where[1]], ""))
		if !valid {
			continue
		}
		for _, entry := range snapshot.Entries {
			if !orderTitleMatches(base[:where[0]], entry, snapshot) {
				continue
			}
			knownPrefix = true
			wanted, valid := library.NormalizeAnimeOrderLabel(entry.Label)
			if !valid || wanted != label || !selected[entry.EpisodeID] {
				continue
			}
			tail := base[where[1]:]
			if orderExtraLabel.MatchString(tail) {
				return nil
			}
			matched[entry.EpisodeID] = true
		}
	}
	if len(matched) == 1 {
		for id := range matched {
			return []int64{id}
		}
	}
	if len(matched) > 1 || knownPrefix {
		return nil
	}
	// Canonical SxxExx fallback requires its own exact catalog title and a single
	// unambiguous canonical token. It cannot override a recognized provider order.
	where := episodeToken.FindStringIndex(base)
	if where == nil || quality.Normalize(base[:where[0]]) != quality.Normalize(canonicalTitle) {
		return nil
	}
	return suggestedEpisodes(base, targets)
}
