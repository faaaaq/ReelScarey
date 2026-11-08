package transfer

import (
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/faaaaq/ReelScarey/internal/quality"
)

// Automatic matching uses exact identity evidence, never the fuzzy suggestion score.
type automaticHint struct {
	Title        string `json:"title"`
	Year         int    `json:"year"`
	ProviderID   int64  `json:"provider_id"`
	Season       int    `json:"season"`
	Episodes     []int  `json:"episodes"`
	EpisodeTitle string `json:"episode_title,omitempty"`
}

var automaticProviderID = regexp.MustCompile(`(?i)[\[{]tmdb[-= ]([0-9]+)[\]}]`)
var automaticOtherID = regexp.MustCompile(`(?i)[\[{](?:imdb[-= ]tt[0-9]+|tvdb[-= ][0-9]+)[\]}]`)
var automaticSeasonFolder = regexp.MustCompile(`(?i)^season[ ._-]*([0-9]{1,3})$`)
var automaticEpisodePlaceholder = regexp.MustCompile(`(?i)^(?:episode|ep|e)[ ._-]*[0-9]{1,5}$`)

type automaticReview string

func (e automaticReview) Error() string { return string(e) }

func parseAutomaticTitle(raw string) (automaticHint, error) {
	h := automaticHint{}
	ids := automaticProviderID.FindAllStringSubmatch(raw, -1)
	for _, id := range ids {
		n, err := strconv.ParseInt(id[1], 10, 64)
		if err != nil || n < 1 || (h.ProviderID != 0 && h.ProviderID != n) {
			return h, automaticReview("Conflicting provider IDs need review.")
		}
		h.ProviderID = n
	}
	raw = automaticProviderID.ReplaceAllString(raw, " ")
	raw = automaticOtherID.ReplaceAllString(raw, " ")
	if i := hintQuality.FindStringIndex(raw); i != nil {
		raw = raw[:i[0]]
	}
	// A leading number can be part of a title (1917, 1923, 2001). Prefer the
	// last non-leading year, so '2001 A Space Odyssey (1968)' keeps its title.
	years := hintYear.FindAllStringIndex(raw, -1)
	for i := len(years) - 1; i >= 0; i-- {
		at := years[i]
		prefix := strings.Trim(raw[:at[0]], " ._-()[]{}")
		if prefix == "" {
			continue
		}
		h.Year, _ = strconv.Atoi(raw[at[0]:at[1]])
		raw = prefix
		break
	}
	h.Title = strings.Join(strings.Fields(strings.NewReplacer(".", " ", "_", " ").Replace(strings.Trim(raw, " ._-()[]{}"))), " ")
	if h.Title == "" || len(h.Title) > 200 || quality.Normalize(h.Title) == "" {
		return h, automaticReview("The filename or title folder does not identify a title clearly.")
	}
	return h, nil
}

func automaticIdentity(scan LibraryScan, f DiscoveredFile) (automaticHint, error) {
	h := automaticHint{}
	prefix := strings.TrimSuffix(scan.Folder, "/") + "/"
	if !strings.HasPrefix(f.Path, prefix) {
		return h, automaticReview("The file is outside this scan's folder.")
	}
	parts := strings.Split(strings.TrimPrefix(f.Path, prefix), "/")
	base := strings.TrimSuffix(parts[len(parts)-1], path.Ext(parts[len(parts)-1]))
	if scan.Kind == "movie" {
		file, fileErr := parseAutomaticTitle(base)
		if fileErr != nil && automaticProviderID.MatchString(base) {
			return h, fileErr
		}
		h = file
		if len(parts) > 1 {
			// A managed title directory is authoritative. Release names may be
			// abbreviated, translated or obfuscated; they do not override that folder.
			folder, err := parseAutomaticTitle(parts[len(parts)-2])
			if err != nil {
				return h, err
			}
			if file.ProviderID != 0 && folder.ProviderID != 0 && file.ProviderID != folder.ProviderID {
				return h, automaticReview("The movie filename and folder have different provider IDs.")
			}
			if fileErr == nil && file.Year != 0 && folder.Year != 0 && file.Year != folder.Year {
				return h, automaticReview("The movie filename and folder have different years.")
			}
			h = folder
			if h.ProviderID == 0 {
				h.ProviderID = file.ProviderID
			}
		} else {
			if fileErr != nil {
				return h, fileErr
			}
			if h.Year == 0 && h.ProviderID == 0 {
				return h, automaticReview("A loose movie file needs a year or explicit TMDb ID for automatic matching.")
			}
		}
		return h, nil
	}
	season, episodes, start, end, ok := quality.EpisodeSpan(base)
	if !ok {
		if quality.HasEpisodeLabel(base) {
			return h, automaticReview("Episode numbering is ambiguous. Review the season and episode mapping.")
		}
		// No SxxExx label at all: identify the show from the folder and the
		// episode by its canonical title instead of refusing the file.
		return titleOnlyIdentity(parts, base, h)
	}
	for i, n := range episodes {
		if i > 0 && n != episodes[i-1]+1 {
			return h, automaticReview("Nonconsecutive episode numbers need review.")
		}
	}
	titlePart := strings.Trim(base[:start], " ._-")
	file, fileErr := parseAutomaticTitle(titlePart)
	if fileErr != nil && automaticProviderID.MatchString(titlePart) {
		return h, fileErr
	}
	h = file
	if len(parts) > 1 {
		titleFolder, titleIndex := parts[len(parts)-2], len(parts)-2
		for i, part := range parts[:len(parts)-1] {
			match := automaticSeasonFolder.FindStringSubmatch(part)
			number := -1
			if len(match) > 0 {
				number, _ = strconv.Atoi(match[1])
			} else if strings.EqualFold(part, "specials") {
				number = 0
			}
			if number >= 0 {
				if number != season {
					return h, automaticReview("The episode filename and season folder disagree.")
				}
				if i > 0 {
					titleFolder, titleIndex = parts[i-1], i-1
				}
			}
		}
		// An unnumbered "Season" folder is a container, not a title.
		for titleIndex > 0 && plainSeasonFolder(titleFolder) {
			titleIndex--
			titleFolder = parts[titleIndex]
		}
		folder, err := parseAutomaticTitle(titleFolder)
		if err != nil {
			return h, err
		}
		if fileErr == nil && (file.Year != 0 && folder.Year != 0 && file.Year != folder.Year || file.ProviderID != 0 && folder.ProviderID != 0 && file.ProviderID != folder.ProviderID) {
			return h, automaticReview("The episode filename and show folder disagree; review the show identity.")
		}
		h = folder
		if h.ProviderID == 0 {
			h.ProviderID = file.ProviderID
		}
	} else if fileErr != nil {
		return h, fileErr
	}
	h.Season = season
	h.Episodes = episodes
	h.EpisodeTitle = episodeTitleAfterLabel(base, end)
	return h, nil
}

// episodeTitleAfterLabel keeps the text following an explicit SxxExx label as
// weak secondary evidence. A missing title is fine: the numbered mapping
// remains authoritative. Pure placeholders such as "Episode 2" are not titles.
func episodeTitleAfterLabel(base string, end int) string {
	tail := base[end:]
	if i := hintQuality.FindStringIndex(tail); i != nil {
		tail = tail[:i[0]]
	}
	tail = strings.Trim(tail, " ._-–—()[]{}")
	normalized := quality.Normalize(tail)
	if normalized == "" {
		return ""
	}
	if automaticEpisodePlaceholder.MatchString(normalized) {
		return ""
	}
	digits := true
	for _, r := range normalized {
		if r < '0' || r > '9' {
			digits = false
			break
		}
	}
	if digits {
		return ""
	}
	return strings.Join(strings.Fields(tail), " ")
}

func plainSeasonFolder(part string) bool {
	trimmed := strings.TrimRight(part, " ._-")
	return strings.EqualFold(trimmed, "season") || strings.EqualFold(trimmed, "seasons")
}

// titleOnlyIdentity handles episode files with no SxxExx label. The show still
// comes from exact folder evidence; the episode is resolved later by matching
// its canonical title, which must be unique across the whole show.
func titleOnlyIdentity(parts []string, base string, h automaticHint) (automaticHint, error) {
	folderSeason := -1
	titleFolder := ""
	for i := len(parts) - 2; i >= 0; i-- {
		part := parts[i]
		if match := automaticSeasonFolder.FindStringSubmatch(part); len(match) > 0 {
			folderSeason, _ = strconv.Atoi(match[1])
			continue
		}
		trimmed := strings.TrimRight(part, " ._-")
		if strings.EqualFold(trimmed, "specials") {
			folderSeason = 0
			continue
		}
		if plainSeasonFolder(part) {
			continue
		}
		titleFolder = part
		break
	}
	if titleFolder == "" {
		file, fileErr := parseAutomaticTitle(base)
		if fileErr != nil {
			return h, fileErr
		}
		h = file
		if h.Year == 0 && h.ProviderID == 0 {
			return h, automaticReview("A loose episode file needs a year or explicit TMDb ID for automatic matching.")
		}
	} else {
		folder, err := parseAutomaticTitle(titleFolder)
		if err != nil {
			return h, err
		}
		h = folder
	}
	h.Season = folderSeason
	h.Episodes = nil
	h.EpisodeTitle = episodeTitleTail(base, h.Title)
	if h.EpisodeTitle == "" {
		return h, automaticReview("The filename does not include an episode title after the show name. Review the mapping.")
	}
	return h, nil
}

// episodeTitleTail removes the show name, container markers such as "SE" and
// bare episode numbers, leaving the episode title text.
func episodeTitleTail(base, show string) string {
	raw := strings.NewReplacer(".", " ", "_", " ").Replace(base)
	if i := hintQuality.FindStringIndex(raw); i != nil {
		raw = raw[:i[0]]
	}
	if show != "" {
		lower, prefix := strings.ToLower(raw), strings.ToLower(strings.TrimSpace(show))
		if at := strings.Index(lower, prefix); at >= 0 {
			raw = raw[at+len(prefix):]
		}
	}
	tokens := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ' ' || r == '-' || r == '.' || r == '_' || r == '–' || r == '—'
	})
	markers := map[string]bool{
		"se": true, "sp": true, "ova": true, "oad": true,
		"ncop": true, "nced": true, "pv": true,
	}
	for i := 0; i < len(tokens); i++ {
		token := strings.ToLower(tokens[i])
		if markers[token] {
			continue
		}
		if token == "special" || token == "specials" || token == "episode" || token == "ep" {
			continue
		}
		isNumber := true
		for _, r := range tokens[i] {
			if r < '0' || r > '9' {
				isNumber = false
				break
			}
		}
		if isNumber {
			continue
		}
		return strings.TrimSpace(strings.Join(tokens[i:], " "))
	}
	return ""
}

func (h automaticHint) key() string {
	return fmt.Sprintf("%s|%d|%d", quality.Normalize(h.Title), h.Year, h.ProviderID)
}
func (h automaticHint) matches(title string, year *int, providerID int64) bool {
	if h.ProviderID > 0 {
		return h.ProviderID == providerID
	}
	return quality.Normalize(h.Title) == quality.Normalize(title) && (h.Year == 0 || year != nil && h.Year == *year) && (h.ProviderID == 0 || h.ProviderID == providerID)
}
