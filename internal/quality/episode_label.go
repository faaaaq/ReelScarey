package quality

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Additional explicit single-episode grammars. No absolute numbering, ranges or
// pack interpretation is introduced by these labels.
var standardLabel = regexp.MustCompile(`(?i)(?:s([0-9]{1,2})[ ._-]*e([0-9]{1,3})|([0-9]{1,2})x([0-9]{1,3})|season[ ._-]+([0-9]{1,2})[ ._-]+episode[ ._-]+([0-9]{1,3}))`)
var extraStandardLabel = regexp.MustCompile(`(?i)^[ ._-]*(?:x[0-9]|ep(?:isode)?[ ._-]*[0-9])`)

func EpisodeLabel(title string) (season, episode, start, end int, ok bool) {
	if len(title) > 1000 || !utf8.ValidString(title) {
		return
	}
	matches := standardLabel.FindAllStringSubmatchIndex(title, -1)
	if len(matches) != 1 {
		return
	}
	m := matches[0]
	start, end = m[0], m[1]
	if wordBefore(title, start) || wordAfter(title, end) || extraEpisode.MatchString(title[end:]) {
		return
	}
	tail := strings.ToLower(title[end:])
	if extraStandardLabel.MatchString(tail) {
		return
	}
	for i := 2; i < len(m); i += 4 {
		if m[i] >= 0 {
			season, _ = strconv.Atoi(title[m[i]:m[i+1]])
			episode, _ = strconv.Atoi(title[m[i+2]:m[i+3]])
			break
		}
	}
	ok = episode > 0
	return
}
