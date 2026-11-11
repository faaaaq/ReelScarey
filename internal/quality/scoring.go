package quality

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
)

var customFormatRegexCache sync.Map

func customFormatRegexp(pattern string) (*regexp.Regexp, error) {
	if cached, ok := customFormatRegexCache.Load(pattern); ok {
		return cached.(*regexp.Regexp), nil
	}
	compiled, err := regexp.Compile("(?i)" + pattern)
	if err == nil {
		customFormatRegexCache.Store(pattern, compiled)
	}
	return compiled, err
}

// CustomFormat is an ordered, profile-local set of case-insensitive regular
// expressions. Required and excluded formats can make a release ineligible;
// other formats only reorder eligible releases.
type CustomFormat struct {
	Name     string   `json:"name"`
	Patterns []string `json:"patterns"`
	Weight   int      `json:"weight"`
	Required bool     `json:"required"`
	Excluded bool     `json:"excluded"`
}

type Scoring struct {
	Sources             []string       `json:"sources"`
	Codecs              []string       `json:"codecs"`
	PreferredTerms      string         `json:"preferred_terms"`
	MaxUsenetAgeDays    int            `json:"max_usenet_age_days"`
	MinUsenetAgeMinutes int            `json:"min_usenet_age_minutes"`
	CustomFormats       []CustomFormat `json:"custom_formats,omitempty"`
}

var Sources = []string{"remux", "bluray", "webdl", "webrip", "hdtv", "dvd"}
var Codecs = []string{"av1", "h265", "h264"}

func validateScoring(s Scoring) error {
	for _, list := range []struct{ values, allowed []string }{{s.Sources, Sources}, {s.Codecs, Codecs}} {
		seen := map[string]bool{}
		for _, v := range list.values {
			if !slices.Contains(list.allowed, v) || seen[v] {
				return fmt.Errorf("Choose distinct supported source and codec preferences, in preferred order.")
			}
			seen[v] = true
		}
	}
	if s.MaxUsenetAgeDays < 0 || s.MaxUsenetAgeDays > 10000 || s.MinUsenetAgeMinutes < 0 || s.MinUsenetAgeMinutes > 10080 || (s.MaxUsenetAgeDays > 0 && s.MinUsenetAgeMinutes > s.MaxUsenetAgeDays*1440) {
		return fmt.Errorf("Use 0–10000 retention days and 0–10080 propagation minutes; minimum age cannot exceed retention.")
	}
	if len(s.PreferredTerms) > 200 || strings.ContainsFunc(s.PreferredTerms, unicode.IsControl) {
		return fmt.Errorf("Preferred terms must contain at most 200 characters without control characters.")
	}
	for _, term := range strings.Split(s.PreferredTerms, ",") {
		if strings.TrimSpace(term) != "" && Normalize(term) == "" {
			return fmt.Errorf("Preferred terms must contain letters or numbers.")
		}
	}
	if err := validateCustomFormats(s.CustomFormats); err != nil {
		return err
	}
	return nil
}

func validateCustomFormats(formats []CustomFormat) error {
	if len(formats) > 20 {
		return fmt.Errorf("Use at most 20 custom formats per profile.")
	}
	seen := map[string]bool{}
	for _, format := range formats {
		name := strings.TrimSpace(format.Name)
		if name == "" || len(name) > 80 || strings.ContainsFunc(name, unicode.IsControl) {
			return fmt.Errorf("Custom format names must contain 1–80 characters without control characters.")
		}
		if seen[strings.ToLower(name)] {
			return fmt.Errorf("Custom format names must be unique within a profile.")
		}
		seen[strings.ToLower(name)] = true
		if format.Required && format.Excluded {
			return fmt.Errorf("A custom format cannot be both required and excluded.")
		}
		if format.Weight < -500 || format.Weight > 500 {
			return fmt.Errorf("Custom format weights must be between -500 and 500.")
		}
		if len(format.Patterns) == 0 || len(format.Patterns) > 20 {
			return fmt.Errorf("Each custom format needs 1–20 patterns.")
		}
		for _, pattern := range format.Patterns {
			if strings.TrimSpace(pattern) == "" || len(pattern) > 200 || strings.ContainsFunc(pattern, unicode.IsControl) {
				return fmt.Errorf("Custom format patterns must contain 1–200 characters without control characters.")
			}
			if _, err := regexp.Compile("(?i)" + pattern); err != nil {
				return fmt.Errorf("Custom format %q contains an invalid regular expression.", name)
			}
		}
	}
	return nil
}
func scoreEvidence(s Scoring, r Release, tokens string, sizeLimit int64, d *Decision) {
	d.ScoringReasons = []string{}
	has := func(labels ...string) bool {
		for _, v := range labels {
			if strings.Contains(tokens, " "+v+" ") {
				return true
			}
		}
		return false
	}
	switch {
	case has("remux"):
		d.Source = "remux"
	case has("bluray", "blu ray", "bdrip", "brrip"):
		d.Source = "bluray"
	case has("webdl", "web dl"):
		d.Source = "webdl"
	case has("webrip", "web rip"):
		d.Source = "webrip"
	case has("hdtv"):
		d.Source = "hdtv"
	case has("dvdrip", "dvd"):
		d.Source = "dvd"
	default:
		d.Source = "unknown"
	}
	codecs := []string{}
	for _, v := range []struct {
		code   string
		labels []string
	}{{"av1", []string{"av1"}}, {"h265", []string{"h265", "h 265", "x265", "hevc"}}, {"h264", []string{"h264", "h 264", "x264", "avc"}}} {
		if has(v.labels...) {
			codecs = append(codecs, v.code)
		}
	}
	d.Codec = "unknown"
	if len(codecs) == 1 {
		d.Codec = codecs[0]
	}
	for _, v := range []struct {
		value       string
		preferences []string
		weight      int
	}{{d.Source, s.Sources, 200}, {d.Codec, s.Codecs, 100}} {
		if i := slices.Index(v.preferences, v.value); i >= 0 {
			points := (len(v.preferences) - i) * v.weight
			d.Score += points
			d.ScoringReasons = append(d.ScoringReasons, fmt.Sprintf("Preferred %s: +%d", v.value, points))
		}
	}
	seen := map[string]bool{}
	for _, term := range strings.Split(s.PreferredTerms, ",") {
		term = Normalize(term)
		if term != "" && !seen[term] && has(term) {
			d.Score += 25
			d.ScoringReasons = append(d.ScoringReasons, "Preferred term: "+term)
		}
		seen[term] = true
	}
	customHaystack := tokens + "\n" + r.Title
	for _, format := range s.CustomFormats {
		matched := false
		for _, pattern := range format.Patterns {
			re, err := customFormatRegexp(pattern)
			if err == nil && re.MatchString(customHaystack) {
				matched = true
				break
			}
		}
		switch {
		case format.Required && !matched:
			d.Reasons = append(d.Reasons, "Required custom format not matched: "+format.Name)
			continue
		case format.Excluded && matched:
			d.Reasons = append(d.Reasons, "Excluded custom format matched: "+format.Name)
			continue
		}
		if matched {
			if format.Weight != 0 {
				d.Score += format.Weight
				d.ScoringReasons = append(d.ScoringReasons, fmt.Sprintf("Custom format %s: %+d", format.Name, format.Weight))
			}
			if format.Required {
				d.ScoringReasons = append(d.ScoringReasons, "Required custom format: "+format.Name)
			}
		}
	}
	// Torrent seeders dominate the preference bonuses within one resolution:
	// among eligible releases the best-seeded one wins at scan time. The
	// resolution order itself still leads, and usenet releases keep their
	// retention rules instead.
	if r.Protocol != "usenet" && r.Seeders != nil {
		points := min(max(*r.Seeders, 0), 4000) * 2
		d.Score += points
		if points > 0 {
			d.ScoringReasons = append(d.ScoringReasons, fmt.Sprintf("Seeders: +%d", points))
		}
	}
	// Conservative soft penalties. They never make a release ineligible:
	// blocking reasons live in Decision.Reasons, while these only reorder
	// eligible candidates and are always visible in ScoringReasons. Values are
	// static and deterministic, so the same release always ranks identically.
	// The weights stay well below the resolution/cutoff scores above, and a
	// release with preferred source and codec labels keeps its bonus instead.
	if d.Source == "unknown" {
		// Missing source evidence is weaker than a known, merely unpreferred
		// source, but it is common enough that it must not block a grab.
		d.Score -= 150
		d.ScoringReasons = append(d.ScoringReasons, "Unknown source: -150")
	}
	if d.Codec == "unknown" {
		d.Score -= 75
		d.ScoringReasons = append(d.ScoringReasons, "Unknown codec: -75")
	}
	if r.Protocol != "usenet" && r.Seeders != nil {
		switch n := *r.Seeders; {
		case n == 0:
			// An explicitly reported zero is worse than an unlabeled count,
			// which stays untouched here and is handled by the minimum-seeder
			// eligibility rule instead.
			d.Score -= 200
			d.ScoringReasons = append(d.ScoringReasons, "No seeders: -200")
		case n > 0 && n < 3:
			d.Score -= 100
			d.ScoringReasons = append(d.ScoringReasons, fmt.Sprintf("Low seeders: -100 (%d)", n))
		}
	}
	// Releases above 80% of the profile limit are eligible but leave little
	// headroom for size-report variance, so they rank below smaller peers.
	if sizeLimit > 0 && r.Size > sizeLimit*4/5 {
		d.Score -= 50
		d.ScoringReasons = append(d.ScoringReasons, "Near size limit: -50")
	}
	if r.Protocol != "usenet" {
		return
	}
	now := time.Now()
	if r.Now > 0 {
		now = time.Unix(r.Now, 0)
	}
	var published time.Time
	for _, layout := range []string{time.RFC3339Nano, time.RFC1123Z, time.RFC1123} {
		if t, e := time.Parse(layout, r.Published); e == nil {
			published = t
			break
		}
	}
	if !published.IsZero() && !published.After(now) {
		age := now.Sub(published).Hours() / 24
		d.AgeDays = &age
	}
	if s.MaxUsenetAgeDays == 0 && s.MinUsenetAgeMinutes == 0 {
		return
	}
	if d.AgeDays == nil {
		d.Reasons = append(d.Reasons, "Usenet publication date is missing, invalid or in the future.")
		return
	}
	if s.MaxUsenetAgeDays > 0 && *d.AgeDays > float64(s.MaxUsenetAgeDays) {
		d.Reasons = append(d.Reasons, "Usenet article exceeds the configured retention age.")
	}
	if s.MinUsenetAgeMinutes > 0 && *d.AgeDays*1440 < float64(s.MinUsenetAgeMinutes) {
		d.Reasons = append(d.Reasons, "Usenet article is still inside the configured propagation delay.")
	}
}
