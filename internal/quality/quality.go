// Package quality owns conservative release matching and persisted preferences.
// Release labels are evidence for selection, never verification of imported media.
package quality

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type Profile struct {
	ID            int64   `json:"id"`
	Name          string  `json:"name"`
	Qualities     []int   `json:"qualities"`
	Cutoff        int     `json:"cutoff"`
	Language      string  `json:"language"`
	MinSeeders    int     `json:"min_seeders"`
	MaxSizeGiB    int     `json:"max_size_gib"`
	RequiredTerms string  `json:"required_terms"`
	RejectedTerms string  `json:"rejected_terms"`
	Revision      int     `json:"revision"`
	Scoring       Scoring `json:"scoring"`
	// GrabDelayMinutes delays an automatic grab after a release was first seen.
	// Zero means no delay.
	GrabDelayMinutes int `json:"grab_delay_minutes"`
}

type Summary struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Revision int    `json:"revision"`
}

func (p Profile) Summary() Summary { return Summary{ID: p.ID, Name: p.Name, Revision: p.Revision} }

// GrabDelay returns the profile's configured delay before an automatic grab.
// It is a preference only; callers remain responsible for persisting the first
// sighting and fencing their own decisions.
func (p Profile) GrabDelay() time.Duration {
	return time.Duration(p.GrabDelayMinutes) * time.Minute
}

type Target struct {
	MovieVersion        string
	Title               string
	Year                *int
	Season, Episode     int
	TV                  bool
	Anime               bool
	AnimeMode           string
	AlternateTitles     []string
	AbsoluteNumber      *int
	AnimeOrderTitle     string
	AnimeOrderLabels    []string
	SeriesCodec         string
	SeriesGroup         string
	SeriesRulesRevision int
	// Pack marks a season/range/complete-series target. Episode identity is
	// replaced by show title and season/collection evidence.
	Pack bool
}
type Release struct {
	Protocol  string
	ID, Title string
	Size      int64
	Seeders   *int
	Published string
	Now       int64
}
type Decision struct {
	ID             string   `json:"release_id"`
	Title          string   `json:"title"`
	Quality        int      `json:"quality"`
	Language       string   `json:"language"`
	Score          int      `json:"score"`
	Eligible       bool     `json:"eligible"`
	Reasons        []string `json:"reasons"`
	Source         string   `json:"source"`
	Codec          string   `json:"codec"`
	AgeDays        *float64 `json:"age_days"`
	ScoringReasons []string `json:"scoring_reasons"`
}

var Languages = []string{"any", "english", "french", "german", "spanish", "italian", "japanese", "korean", "chinese"}

func Validate(p Profile) error {
	if e := validateScoring(p.Scoring); e != nil {
		return e
	}
	if strings.TrimSpace(p.Name) == "" || len(p.Name) > 60 || strings.ContainsFunc(p.Name, unicode.IsControl) {
		return fmt.Errorf("Use a profile name with 1–60 characters.")
	}
	if len(p.Qualities) == 0 || len(p.Qualities) > 4 || !slices.Contains(p.Qualities, p.Cutoff) {
		return fmt.Errorf("Choose allowed resolutions and a cutoff among them.")
	}
	last := 0
	for _, q := range p.Qualities {
		if !slices.Contains([]int{480, 720, 1080, 2160}, q) || q <= last {
			return fmt.Errorf("Resolutions must be unique and ordered from lowest to highest.")
		}
		last = q
	}
	if !slices.Contains(Languages, p.Language) || p.MinSeeders < 0 || p.MinSeeders > 10000 || p.MaxSizeGiB < 1 || p.MaxSizeGiB > 500 {
		return fmt.Errorf("Choose a supported language, 0–10000 minimum seeders and a 1–500 GiB size limit.")
	}
	if p.GrabDelayMinutes < 0 || p.GrabDelayMinutes > 1440 {
		return fmt.Errorf("Use a grab delay between 0 and 1440 minutes.")
	}
	for _, v := range []string{p.RequiredTerms, p.RejectedTerms} {
		if len(v) > 200 || strings.ContainsFunc(v, unicode.IsControl) {
			return fmt.Errorf("Release terms must contain at most 200 characters without control characters.")
		}
		for _, t := range strings.Split(v, ",") {
			if strings.TrimSpace(t) != "" && Normalize(t) == "" {
				return fmt.Errorf("Release terms must contain letters or numbers.")
			}
		}
	}
	return nil
}
func Normalize(s string) string {
	return strings.Join(strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }), " ")
}

var extraEpisode = regexp.MustCompile(`(?i)^[^a-z0-9]*(?:e[0-9]{1,3}|[0-9]{1,3})(?:[^a-z0-9]|$)`)

// bracketPrefix matches bracketed group tags at the start of a title segment.
// The automatic matching pipeline strips provider IDs from filenames, but
// Identify is also called independently for search and feed evaluation where
// release titles may contain [Group], [TGx], or similar tracker metadata.
var bracketPrefix = regexp.MustCompile(`\[[^\]]*\]`)

func identifyStandard(title string, t Target) bool {
	n := Normalize(title)
	base := Normalize(t.Title)
	if base == "" {
		return false
	}
	if !t.TV {
		if t.Year == nil {
			return false
		}
		want := base + " " + strconv.Itoa(*t.Year)
		return n == want || strings.HasPrefix(n, want+" ")
	}
	s, e, start, _, ok := EpisodeLabel(title)
	if !ok || s != t.Season || e != t.Episode {
		return false
	}
	// Strip bracketed group/content tags from the title prefix before
	// normalizing. Release names commonly start with "[Group]" or "[TGx]"
	// before the actual show name; removing them lets the base title match
	// without incorrectly accepting broader substrings.
	rawPrefix := title[:start]
	cleanPrefix := bracketPrefix.ReplaceAllString(rawPrefix, " ")
	prefix := Normalize(cleanPrefix)
	if prefix == base || (t.Year != nil && prefix == base+" "+strconv.Itoa(*t.Year)) {
		return true
	}
	// Some releases prepend a presenter or narrator name before the show
	// title (e.g. "Jimmy Carrs Am I The Asshole S02E02"). Accept the match
	// when the base title is a whole-word suffix of the normalized prefix;
	// the episode label supplies the identity evidence, and the leading
	// space keeps run-together prefixes from matching a short title.
	return strings.HasSuffix(prefix, " "+base)
}
func identityLabels(title string, t Target) (string, bool) {
	if t.Pack && t.TV {
		return identifyPack(title, t)
	}
	if t.Anime && t.TV {
		if t.AnimeMode == "mapped" {
			return identifyMappedAnime(title, t)
		}
		return identifyAnime(title, t)
	}
	if !identifyStandard(title, t) {
		return title, false
	}
	if t.TV {
		_, _, _, end, _ := EpisodeLabel(title)
		return title[end:], true
	}
	n := Normalize(title)
	prefix := Normalize(t.Title) + " " + strconv.Itoa(*t.Year)
	return strings.TrimPrefix(n, prefix), true
}
func Identify(title string, t Target) bool { _, ok := identityLabels(title, t); return ok }

// EvaluateOptions controls explicit exceptions to conservative release evaluation.
type EvaluateOptions struct {
	// AllowUnknownSeeders treats a missing seed count as satisfying the
	// profile's minimum. Announce events never carry a seed count, so this lets
	// an announce match a profile that sets MinSeeders without weakening the
	// rule for scheduled searches, which always report a count. A release that
	// reports a count below the minimum is still rejected, and no seed count is
	// invented.
	AllowUnknownSeeders bool
}

func Evaluate(p Profile, t Target, r Release) Decision {
	return EvaluateWithOptions(p, t, r, EvaluateOptions{})
}

func EvaluateWithOptions(p Profile, t Target, r Release, opts EvaluateOptions) Decision {
	d := Decision{ID: r.ID, Title: r.Title, Reasons: []string{}, Language: "unknown"}
	reject := func(s string) { d.Reasons = append(d.Reasons, s) }
	labels, identified := identityLabels(r.Title, t)
	if !identified {
		reject("Title, year or episode identity does not match exactly.")
	}
	tokens := " " + Normalize(labels) + " "
	if t.TV && t.SeriesCodec != "" {
		aliases := []string{"x264", "h264", "h 264", "avc"}
		if t.SeriesCodec == "h265" {
			aliases = []string{"x265", "h265", "h 265", "hevc"}
		}
		matched := false
		for _, alias := range aliases {
			if strings.Contains(tokens, " "+alias+" ") {
				matched = true
			}
		}
		if !matched {
			reject("Required codec not explicitly labeled: " + t.SeriesCodec)
		}
	}
	if t.TV && t.SeriesGroup != "" && !matchesReleaseGroup(r.Title, t.SeriesGroup) {
		reject("Required release group not explicitly labeled: " + t.SeriesGroup)
	}
	if t.TV && !t.Pack {
		for _, bad := range []string{"complete", "season", "pack", "batch"} {
			if strings.Contains(tokens, " "+bad+" ") {
				reject("Season packs require manual episode selection and review.")
				break
			}
		}
	}
	for _, bad := range []string{"cam", "hdcam", "telesync", "telecine", "ts", "tc", "screener", "dvdscr", "sample", "trailer"} {
		if strings.Contains(tokens, " "+bad+" ") {
			reject("Camera recordings, screeners, samples and trailers are excluded.")
			break
		}
	}
	for _, m := range strings.Fields(tokens) {
		q := 0
		switch m {
		case "480p", "576p":
			q = 480
		case "720p":
			q = 720
		case "1080p", "1080i":
			q = 1080
		case "2160p", "4k", "uhd":
			q = 2160
		}
		if q == 0 {
			continue
		}
		if d.Quality != 0 && d.Quality != q {
			reject("Conflicting resolution labels.")
		}
		d.Quality = q
	}
	if !slices.Contains(p.Qualities, d.Quality) {
		reject("Resolution is unknown or excluded by this profile.")
	}
	if !t.TV && !MovieVersionMatches(t.MovieVersion, d.Quality) {
		reject("Resolution does not match the selected movie version.")
	}
	// Language claims must be explicit. MULTI alone does not establish a language.
	for _, lang := range Languages[1:] {
		if strings.Contains(tokens, " "+lang+" ") || (lang == "english" && strings.Contains(tokens, " eng ")) {
			if d.Language == "unknown" {
				d.Language = lang
			}
			if p.Language == lang {
				d.Language = lang
				break
			}
		}
	}
	if p.Language != "any" && d.Language != p.Language {
		reject("The required language is not explicitly labeled.")
	}
	if r.Protocol != "usenet" {
		if r.Seeders == nil {
			// A missing seed count is normal for announce events. When the
			// caller allows it, the absence satisfies the profile's minimum
			// instead of failing it; searches that report a real count still
			// enforce the minimum in the branch below.
			if !opts.AllowUnknownSeeders {
				if p.MinSeeders > 0 {
					reject(fmt.Sprintf("Seed count is unknown; the profile requires at least %d seeders.", p.MinSeeders))
				} else {
					reject("Seed count is unknown or below the minimum.")
				}
			}
		} else if *r.Seeders < p.MinSeeders {
			reject("Seed count is unknown or below the minimum.")
		}
	}
	if r.Size <= 0 || r.Size > int64(p.MaxSizeGiB)*(1<<30) {
		reject("Size is unknown or exceeds the profile limit.")
	}
	for _, rule := range []struct {
		value    string
		required bool
	}{{p.RequiredTerms, true}, {p.RejectedTerms, false}} {
		for _, term := range strings.Split(rule.value, ",") {
			term = Normalize(term)
			if term == "" {
				continue
			}
			found := strings.Contains(tokens, " "+term+" ")
			if rule.required && !found {
				reject("Missing required term: " + term)
			}
			if !rule.required && found {
				reject("Excluded term: " + term)
			}
		}
	}
	// Prefer the cutoff, then lower allowed resolutions, then higher ones.
	q := d.Quality
	if q > p.Cutoff {
		d.Score = 10000 - q
	} else {
		d.Score = 100000 + q*100
	}
	scoreEvidence(p.Scoring, r, tokens, int64(p.MaxSizeGiB)<<30, &d)
	d.Eligible = len(d.Reasons) == 0
	return d
}
func Rank(p Profile, t Target, releases []Release) []Decision {
	out := make([]Decision, 0, len(releases))
	for _, r := range releases {
		out = append(out, Evaluate(p, t, r))
	}
	slices.SortStableFunc(out, func(a, b Decision) int {
		if a.Eligible != b.Eligible {
			if a.Eligible {
				return -1
			}
			return 1
		}
		if a.Score != b.Score {
			return b.Score - a.Score
		}
		return strings.Compare(a.Title, b.Title)
	})
	return out
}

// VerifiedQuality maps inspected dimensions, including common cropped widescreen.
func VerifiedQuality(width, height int) int {
	switch {
	case width >= 3500 || height >= 1900:
		return 2160
	case width >= 1700 || height >= 900:
		return 1080
	case width >= 1100 || height >= 650:
		return 720
	case width > 0 && height > 0:
		return 480
	}
	return 0
}
