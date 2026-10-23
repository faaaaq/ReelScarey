// Package quality owns conservative release matching and persisted preferences.
// Release labels are evidence for selection, never verification of imported media.
package quality

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

type Profile struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	Qualities     []int  `json:"qualities"`
	Cutoff        int    `json:"cutoff"`
	Language      string `json:"language"`
	MinSeeders    int    `json:"min_seeders"`
	MaxSizeGiB    int    `json:"max_size_gib"`
	RequiredTerms string `json:"required_terms"`
	RejectedTerms string `json:"rejected_terms"`
	Revision      int    `json:"revision"`
}
type Target struct {
	Title           string
	Year            *int
	Season, Episode int
	TV              bool
}
type Release struct {
	ID, Title string
	Size      int64
	Seeders   *int
}
type Decision struct {
	ID       string   `json:"release_id"`
	Title    string   `json:"title"`
	Quality  int      `json:"quality"`
	Language string   `json:"language"`
	Score    int      `json:"score"`
	Eligible bool     `json:"eligible"`
	Reasons  []string `json:"reasons"`
}

var Languages = []string{"any", "english", "french", "german", "spanish", "italian", "japanese", "korean", "chinese"}

func Validate(p Profile) error {
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

var episodeCode = regexp.MustCompile(`(?i)s([0-9]{1,2})e([0-9]{1,3})(?:e[0-9]+|[- ]+e?[0-9]+)?`)
var extraEpisode = regexp.MustCompile(`(?i)^[^a-z0-9]*(?:e[0-9]{1,3}|[0-9]{1,3})(?:[^a-z0-9]|$)`)

func Identify(title string, t Target) bool {
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
	matches := episodeCode.FindAllStringSubmatchIndex(title, -1)
	if len(matches) != 1 {
		return false
	}
	m := matches[0]
	s, _ := strconv.Atoi(title[m[2]:m[3]])
	e, _ := strconv.Atoi(title[m[4]:m[5]])
	// Automatic searches deliberately accept one exact episode, never a pack/range.
	if s != t.Season || e != t.Episode || m[1] != m[5] {
		return false
	}
	if extraEpisode.MatchString(title[m[1]:]) {
		return false
	}
	prefix := Normalize(title[:m[0]])
	return prefix == base || (t.Year != nil && prefix == base+" "+strconv.Itoa(*t.Year))
}
func Evaluate(p Profile, t Target, r Release) Decision {
	d := Decision{ID: r.ID, Title: r.Title, Reasons: []string{}, Language: "unknown"}
	reject := func(s string) { d.Reasons = append(d.Reasons, s) }
	if !Identify(r.Title, t) {
		reject("Title, year or episode identity does not match exactly.")
	}
	labels := r.Title
	if Identify(r.Title, t) {
		if t.TV {
			m := episodeCode.FindStringIndex(r.Title)
			labels = r.Title[m[1]:]
		} else {
			n := Normalize(r.Title)
			prefix := Normalize(t.Title) + " " + strconv.Itoa(*t.Year)
			labels = strings.TrimPrefix(n, prefix)
		}
	}
	tokens := " " + Normalize(labels) + " "
	if t.TV {
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
	if r.Seeders == nil || *r.Seeders < p.MinSeeders {
		reject("Seed count is unknown or below the minimum.")
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
	if r.Seeders != nil {
		d.Score += min(*r.Seeders, 99)
	}
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
