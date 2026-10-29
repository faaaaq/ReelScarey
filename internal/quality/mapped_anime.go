package quality

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

var mappedLabel = regexp.MustCompile(`(?i)^(?:S([0-9]{1,5})E([0-9]{1,6})|(SP|OVA|ONA|OAD)([0-9]{1,6})|([0-9]{1,6}))$`)
var mappedIdentity = regexp.MustCompile(`(?i)(?:S[0-9]{1,5}E[0-9]{1,6}|(?:SP|OVA|ONA|OAD)[0-9]{1,6}|[0-9]{1,6})(?:v[1-9][0-9]?)?`)
var mappedVersion = regexp.MustCompile(`(?i)v[1-9][0-9]?$`)
var mappedMarker = regexp.MustCompile(`(?i)^\s*(?:\[(?:pack|batch)\]|\((?:pack|batch)\)|(?:pack|batch)\b)`)
var mappedExtraSpecial = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(?:SP|OVA|ONA|OAD)[0-9]+`)

// MappedAnimeLabel identifies a label family, never a canonical episode. Source
// season families deliberately remain distinct from canonical catalog seasons.
func MappedAnimeLabel(label string) (family string, number int, ok bool) {
	m := mappedLabel.FindStringSubmatch(strings.TrimSpace(label))
	if m == nil {
		return "", 0, false
	}
	value := m[5]
	family = "number"
	if m[1] != "" {
		season, _ := strconv.Atoi(m[1])
		if season > 10000 {
			return "", 0, false
		}
		family, value = fmt.Sprintf("S%d", season), m[2]
	} else if m[3] != "" {
		family, value = strings.ToUpper(m[3]), m[4]
	}
	number, _ = strconv.Atoi(value)
	return family, number, number >= 1 && number <= 100000
}

func mappedSet(labels []string) (string, []int, bool) {
	if len(labels) < 1 || len(labels) > 100 {
		return "", nil, false
	}
	family := ""
	numbers := make([]int, 0, len(labels))
	for _, label := range labels {
		f, n, ok := MappedAnimeLabel(label)
		if !ok || family != "" && family != f {
			return "", nil, false
		}
		family = f
		numbers = append(numbers, n)
	}
	sort.Ints(numbers)
	for i := 1; i < len(numbers); i++ {
		if numbers[i] != numbers[i-1]+1 {
			return "", nil, false
		}
	}
	return family, numbers, true
}

func mappedFormat(family string, number, padding int) string {
	if strings.HasPrefix(family, "S") && family != "SP" {
		season, _ := strconv.Atoi(family[1:])
		return fmt.Sprintf("S%02dE%02d", season, number)
	}
	if family == "number" {
		family = ""
	}
	return fmt.Sprintf("%s%0*d", family, padding, number)
}

// MappedAnimeQueries sends one explicitly saved title and a bounded label set.
// A title-only pack query discovers candidates; it never proves their coverage.
func MappedAnimeQueries(title string, labels []string) []string {
	title = strings.TrimSpace(title)
	family, numbers, ok := mappedSet(labels)
	if !ok || Normalize(title) == "" || len(title) > 180 || !utf8.ValidString(title) || strings.ContainsFunc(title, unicode.IsControl) {
		return nil
	}
	queries := []string{}
	if len(numbers) > 1 {
		queries = []string{title + " " + mappedFormat(family, numbers[0], 1) + "-" + mappedFormat(family, numbers[len(numbers)-1], 1), title}
	} else {
		for _, padding := range []int{1, 2, 3} {
			query := title + " " + mappedFormat(family, numbers[0], padding)
			if len(queries) == 0 || queries[len(queries)-1] != query {
				queries = append(queries, query)
			}
		}
	}
	for _, query := range queries {
		if len(query) > 200 {
			return nil
		}
	}
	return queries
}

func identifyMappedAnime(title string, t Target) (string, bool) {
	wantFamily, numbers, ok := mappedSet(t.AnimeOrderLabels)
	if !t.TV || !t.Anime || !ok || Normalize(t.AnimeOrderTitle) == "" || len(t.AnimeOrderTitle) > 180 || !utf8.ValidString(t.AnimeOrderTitle) || strings.ContainsFunc(t.AnimeOrderTitle, unicode.IsControl) {
		return title, false
	}
	if strings.HasPrefix(strings.TrimSpace(title), "[") {
		group := strings.SplitN(strings.TrimSpace(title)[1:], "]", 2)[0]
		if _, _, label := MappedAnimeLabel(mappedVersion.ReplaceAllString(group, "")); label {
			return title, false
		}
	}
	body, ok := animeBody(title)
	if !ok {
		return title, false
	}
	for _, m := range mappedIdentity.FindAllStringIndex(body, -1) {
		if wordBefore(body, m[0]) || wordAfter(body, m[1]) || Normalize(body[:m[0]]) != Normalize(t.AnimeOrderTitle) {
			continue
		}
		token := body[m[0]:m[1]]
		family, first, valid := MappedAnimeLabel(mappedVersion.ReplaceAllString(token, ""))
		if !valid || family != wantFamily || first != numbers[0] {
			continue
		}
		// Bare years/resolutions are ambiguous unless explicitly delimited.
		if family == "number" && (first >= 1900 && first <= 2099 || first == 480 || first == 576 || first == 720 || first == 1080 || first == 2160) {
			prefix := strings.TrimSpace(body[:m[0]])
			if !strings.HasSuffix(prefix, "-") && !strings.HasSuffix(prefix, "–") && !strings.HasSuffix(prefix, "—") {
				continue
			}
		}
		tail := body[m[1]:]
		if len(numbers) > 1 {
			if mappedVersion.MatchString(token) {
				continue
			}
			rest := strings.TrimSpace(tail)
			if len(rest) == 0 || !strings.ContainsRune("-–—", []rune(rest)[0]) {
				continue
			}
			_, width := utf8.DecodeRuneInString(rest)
			rest = strings.TrimSpace(rest[width:])
			end := mappedIdentity.FindStringIndex(rest)
			if end == nil || end[0] != 0 || wordAfter(rest, end[1]) {
				continue
			}
			lastFamily, last, valid := MappedAnimeLabel(rest[:end[1]])
			if !valid || lastFamily != family || last != numbers[len(numbers)-1] {
				continue
			}
			tail = rest[end[1]:]
			// Pack and Batch are accepted only alongside explicit exact endpoints.
			tail = mappedMarker.ReplaceAllString(tail, "")
		}
		if !mappedExtraSpecial.MatchString(tail) && animeTail(tail) {
			return tail, true
		}
	}
	return title, false
}
