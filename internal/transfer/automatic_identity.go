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
	Title      string `json:"title"`
	Year       int    `json:"year"`
	ProviderID int64  `json:"provider_id"`
	Season     int    `json:"season"`
	Episodes   []int  `json:"episodes"`
}

var automaticProviderID = regexp.MustCompile(`(?i)[\[{]tmdb[-= ]([0-9]+)[\]}]`)
var automaticOtherID = regexp.MustCompile(`(?i)[\[{](?:imdb[-= ]tt[0-9]+|tvdb[-= ][0-9]+)[\]}]`)
var automaticSeasonFolder = regexp.MustCompile(`(?i)^season[ ._-]*([0-9]{1,3})$`)

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
	season, episodes, start, _, ok := quality.EpisodeSpan(base)
	if !ok || len(episodes) == 0 || len(episodes) > 20 {
		return h, automaticReview("Episode numbering is uncertain. Review the season and episode mapping.")
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
		titleFolder := parts[len(parts)-2]
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
					titleFolder = parts[i-1]
				}
			}
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
	return h, nil
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
