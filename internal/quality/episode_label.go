package quality

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Every span has one explicit season and a bounded consecutive episode list.
// The single-episode projection alone is eligible for ordinary automation.
var standardLabel = regexp.MustCompile(`(?i)(?:s(?:eason)?[ ._-]*([0-9]{1,4})[ ._-]*e(?:p(?:isode)?s?)?[ ._-]*([0-9]{1,5})|([0-9]{1,4})x([0-9]{1,5}))`)
var chainLabel = regexp.MustCompile(`(?i)^[ ._]*e(?:p(?:isode)?s?)?[ ._-]*([0-9]{1,5})`)
var rangeLabel = regexp.MustCompile(`(?i)^[ ._]*-[ ._]*(?:(e(?:p(?:isode)?s?)?)[ ._-]*)?([0-9]{1,5})`)
var extraStandardLabel = regexp.MustCompile(`(?i)^[ ._,+&/\-]*(?:s(?:eason)?[ ._-]*[0-9]|e(?:p(?:isode)?s?)?[ ._-]*[0-9]|x[0-9]|(?:and|to)[ ._-]+[0-9])`)
var remainingEpisodeLabel = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(?:s(?:eason)?[ ._-]*[0-9]{1,4}|e(?:p(?:isode)?s?)?[ ._-]*[0-9]{1,5}|x[0-9]{1,2})(?:$|[^a-z0-9])`)

func HasEpisodeLabel(title string) bool { return standardLabel.MatchString(title) }

func EpisodeSpan(title string) (season int, episodes []int, start, end int, ok bool) {
	if len(title) > 1000 || !utf8.ValidString(title) || strings.ContainsFunc(title, unicode.IsControl) {
		return
	}
	matches := standardLabel.FindAllStringSubmatchIndex(title, -1)
	if len(matches) != 1 {
		return
	}
	m := matches[0]
	start, end = m[0], m[1]
	if wordBefore(title, start) {
		return
	}
	first := 0
	for i := 2; i < len(m); i += 4 {
		if m[i] >= 0 {
			season, _ = strconv.Atoi(title[m[i]:m[i+1]])
			first, _ = strconv.Atoi(title[m[i+2]:m[i+3]])
			break
		}
	}
	if first < 1 {
		return
	}
	episodes = []int{first}
	tail := title[end:]
	if r := rangeLabel.FindStringSubmatchIndex(tail); r != nil {
		// A resolution following a dash is not the endpoint of an episode range.
		resolution := r[2] < 0 && r[5] < len(tail) && animeResolution.MatchString(tail[r[4]:r[5]+1]) && !wordAfter(tail, r[5]+1)
		if !resolution {
			if wordAfter(tail, r[1]) {
				return season, nil, start, end, false
			}
			last, _ := strconv.Atoi(tail[r[4]:r[5]])
			if last <= first || last-first >= 20 {
				return season, nil, start, end, false
			}
			for n := first + 1; n <= last; n++ {
				episodes = append(episodes, n)
			}
			end += r[1]
		}
	} else {
		for {
			c := chainLabel.FindStringSubmatchIndex(title[end:])
			if c == nil {
				break
			}
			tail = title[end:]
			next, _ := strconv.Atoi(tail[c[2]:c[3]])
			if next != episodes[len(episodes)-1]+1 || len(episodes) >= 20 {
				return season, nil, start, end, false
			}
			episodes = append(episodes, next)
			end += c[1]
		}
	}
	tail = title[end:]
	if wordAfter(title, end) || extraEpisode.MatchString(tail) || extraStandardLabel.MatchString(tail) || remainingEpisodeLabel.MatchString(tail) {
		return season, nil, start, end, false
	}
	ok = true
	return
}

func EpisodeLabel(title string) (season, episode, start, end int, ok bool) {
	s, numbers, a, b, found := EpisodeSpan(title)
	if !found || len(numbers) != 1 || s > 99 || numbers[0] > 999 {
		return
	}
	return s, numbers[0], a, b, true
}
