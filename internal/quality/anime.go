package quality

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Anime matching is opt-in and single-episode only. Canonical IDs and explicit
// absolute aliases remain the authority; release names never infer episode order.
var animeSeasonCode = regexp.MustCompile(`(?i)s([0-9]{1,5})e([0-9]{1,6})(?:v([1-9][0-9]?))?`)
var animeAbsoluteCode = regexp.MustCompile(`(?i)([0-9]{1,6})(?:v([1-9][0-9]?))?`)
var animeNumeric = regexp.MustCompile(`^[0-9]+$`)
var animeCRC = regexp.MustCompile(`(?i)^[a-f0-9]{8}$`)
var animeResolution = regexp.MustCompile(`(?i)^(?:480|576|720|1080|2160)[pi]$`)
var animeEpisodeWord = regexp.MustCompile(`(?i)^(?:e[0-9]+|ep[0-9]+|episode[0-9]+|s[0-9]+(?:e[0-9]+)?)$`)
var animeMulti = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(?:complete|batch|pack|season|episodes|movie|film|ova|ona|oad|special|specials|sp|sps|recap|bonus|extra|extras|ncop|nced|pv|op|ed)(?:$|[^a-z0-9])`)
var animeTechnicalNumber = regexp.MustCompile(`(?i)(?:\b(?:8|10|12)[ ._-]*bit\b|\b(?:aac|ac3|eac3|ddp|flac|opus|dts)[ ._-]*[1-9]\.[0-9](?:ch)?\b|\b[1-9]\.[0-9](?:ch|channels)\b)`)

func wordBefore(text string, index int) bool {
	if index == 0 {
		return false
	}
	r, _ := utf8.DecodeLastRuneInString(text[:index])
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}
func wordAfter(text string, index int) bool {
	if index >= len(text) {
		return false
	}
	r, _ := utf8.DecodeRuneInString(text[index:])
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}
func animeTitleMatches(prefix string, t Target) bool {
	prefix = Normalize(prefix)
	titles := append([]string{t.Title}, t.AlternateTitles...)
	for _, title := range titles {
		base := Normalize(title)
		if base == "" {
			continue
		}
		if prefix == base || t.Year != nil && prefix == base+" "+strconv.Itoa(*t.Year) {
			return true
		}
	}
	return false
}
func animeBody(title string) (string, bool) {
	if len(title) == 0 || len(title) > 1000 || !utf8.ValidString(title) || strings.ContainsFunc(title, unicode.IsControl) {
		return "", false
	}
	title = strings.TrimSpace(title)
	if strings.HasPrefix(title, "[") {
		end := strings.IndexByte(title, ']')
		if end < 2 || end > 81 {
			return "", false
		}
		group := title[1:end]
		if strings.ContainsAny(group, "[]") || Normalize(group) == "" || animeNumeric.MatchString(strings.TrimSpace(group)) || animeResolution.MatchString(strings.TrimSpace(group)) || animeSeasonCode.MatchString(group) || animeMulti.MatchString(group) {
			return "", false
		}
		title = strings.TrimSpace(title[end+1:])
		if strings.HasPrefix(title, "[") {
			return "", false
		}
	}
	return title, title != ""
}

// animeTail accepts technical labels after identity, but refuses any additional
// unlabelled number, episode marker, range, batch or special-type qualifier.
func animeTail(tail string) bool {
	if wordAfter(tail, 0) {
		return false
	}
	if animeSeasonCode.MatchString(tail) || animeMulti.MatchString(tail) {
		return false
	}
	for len(tail) > 0 {
		if tail[0] == '[' || tail[0] == '(' {
			close := byte(']')
			if tail[0] == '(' {
				close = ')'
			}
			end := strings.IndexByte(tail, close)
			if end < 1 {
				return false
			}
			label := strings.TrimSpace(tail[1:end])
			if strings.ContainsAny(label, "[]()") || !animeLabel(label, true) {
				return false
			}
			tail = tail[end+1:]
			continue
		}
		next := strings.IndexAny(tail, "[()]")
		if next == 0 {
			return false
		}
		chunk := tail
		if next > 0 {
			chunk = tail[:next]
		}
		if !animeLabel(chunk, false) {
			return false
		}
		if next < 0 {
			break
		}
		tail = tail[next:]
	}
	return true
}
func animeLabel(label string, bracketed bool) bool {
	if animeSeasonCode.MatchString(label) || animeMulti.MatchString(label) {
		return false
	}
	trimmed := strings.TrimSpace(label)
	// Eight-character checksum labels are technical evidence, including numeric
	// CRCs. They are accepted only as a complete label, never as a bare episode.
	if bracketed && animeCRC.MatchString(trimmed) {
		return true
	}
	label = animeTechnicalNumber.ReplaceAllString(label, " technical ")
	fields := strings.FieldsFunc(label, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	for _, field := range fields {
		if animeNumeric.MatchString(field) || animeEpisodeWord.MatchString(field) {
			return false
		}
	}
	return true
}

func identifyAnime(title string, t Target) (string, bool) {
	if !t.TV || !t.Anime || t.Season < 1 || t.Season > 10000 || t.Episode < 1 || t.Episode > 100000 {
		return title, false
	}
	body, ok := animeBody(title)
	if !ok {
		return title, false
	}
	switch t.AnimeMode {
	case "season":
		matches := animeSeasonCode.FindAllStringSubmatchIndex(body, -1)
		if len(matches) != 1 {
			return title, false
		}
		m := matches[0]
		if wordBefore(body, m[0]) || wordAfter(body, m[1]) {
			return title, false
		}
		season, _ := strconv.Atoi(body[m[2]:m[3]])
		episode, _ := strconv.Atoi(body[m[4]:m[5]])
		if season != t.Season || episode != t.Episode || !animeTitleMatches(body[:m[0]], t) {
			return title, false
		}
		tail := body[m[1]:]
		if !animeTail(tail) {
			return title, false
		}
		return tail, true
	case "absolute":
		if t.AbsoluteNumber == nil || *t.AbsoluteNumber < 1 || *t.AbsoluteNumber > 100000 || animeSeasonCode.MatchString(body) {
			return title, false
		}
		var labels string
		found := false
		for _, m := range animeAbsoluteCode.FindAllStringSubmatchIndex(body, -1) {
			if m[0] == 0 || wordBefore(body, m[0]) || wordAfter(body, m[1]) {
				continue
			}
			number, _ := strconv.Atoi(body[m[2]:m[3]])
			if number != *t.AbsoluteNumber || !animeTitleMatches(body[:m[0]], t) {
				continue
			}
			// A bare year or resolution can describe a show rather than an episode.
			// Require the customary explicit dash for these ambiguous numbers.
			if t.Year != nil && number == *t.Year || number == 480 || number == 576 || number == 720 || number == 1080 || number == 2160 {
				prefix := strings.TrimSpace(body[:m[0]])
				if !strings.HasSuffix(prefix, "-") && !strings.HasSuffix(prefix, "–") && !strings.HasSuffix(prefix, "—") {
					continue
				}
			}
			tail := body[m[1]:]
			if !animeTail(tail) {
				continue
			}
			if found {
				return title, false
			}
			labels = tail
			found = true
		}
		return labels, found
	default:
		return title, false
	}
}

// AnimeQueries returns bounded numeric variants for one explicitly chosen title.
// Aliases are identity evidence and are deliberately not a provider fan-out.
func AnimeQueries(t Target, searchTitle string) []string {
	if !t.Anime || !t.TV || t.Season < 1 || t.Season > 10000 || t.Episode < 1 || t.Episode > 100000 {
		return nil
	}
	searchTitle = strings.TrimSpace(searchTitle)
	if searchTitle == "" {
		searchTitle = strings.TrimSpace(t.Title)
	}
	if Normalize(searchTitle) == "" || len(searchTitle) > 200 || !utf8.ValidString(searchTitle) || strings.ContainsFunc(searchTitle, unicode.IsControl) {
		return nil
	}
	if t.AnimeMode == "season" {
		return []string{fmt.Sprintf("%s S%02dE%02d", searchTitle, t.Season, t.Episode)}
	}
	if t.AnimeMode != "absolute" || t.AbsoluteNumber == nil || *t.AbsoluteNumber < 1 || *t.AbsoluteNumber > 100000 {
		return nil
	}
	numbers := []string{strconv.Itoa(*t.AbsoluteNumber), fmt.Sprintf("%02d", *t.AbsoluteNumber), fmt.Sprintf("%03d", *t.AbsoluteNumber)}
	seen := map[string]bool{}
	out := []string{}
	for _, n := range numbers {
		if !seen[n] {
			seen[n] = true
			out = append(out, searchTitle+" "+n)
		}
	}
	return out
}
