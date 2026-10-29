package quality

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
)

type Scoring struct {
	Sources             []string `json:"sources"`
	Codecs              []string `json:"codecs"`
	PreferredTerms      string   `json:"preferred_terms"`
	MaxUsenetAgeDays    int      `json:"max_usenet_age_days"`
	MinUsenetAgeMinutes int      `json:"min_usenet_age_minutes"`
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
	return nil
}
func scoreEvidence(s Scoring, r Release, tokens string, d *Decision) {
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
