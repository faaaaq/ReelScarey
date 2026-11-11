package quality

import (
	"context"
	"github.com/faaaaq/ReelScarey/internal/database"
	"strings"
	"testing"
	"time"
)

func TestSoftPenaltiesStayVisibleDeterministicAndNonBlocking(t *testing.T) {
	year := 2020
	seeds := 20
	target := Target{Title: "Example", Year: &year}
	p := Profile{Name: "Fixture", Qualities: []int{1080}, Cutoff: 1080, Language: "any", MinSeeders: 0, MaxSizeGiB: 10, Scoring: Scoring{Sources: []string{"webdl"}, Codecs: []string{"h264"}}}
	base := Release{ID: "base", Title: "Example.2020.1080p.WEB-DL.h264", Size: 1 << 30, Seeders: &seeds, Protocol: "torrent"}
	one, two := 1, 2
	zero := 0
	for _, tc := range []struct {
		name     string
		release  Release
		penalty  []string
		eligible bool
	}{
		{"baseline", base, nil, true},
		{"unknown source", Release{ID: "s", Title: "Example.2020.1080p.h264", Size: 1 << 30, Seeders: &seeds, Protocol: "torrent"}, []string{"Unknown source: -150"}, true},
		{"unknown codec", Release{ID: "c", Title: "Example.2020.1080p.WEB-DL", Size: 1 << 30, Seeders: &seeds, Protocol: "torrent"}, []string{"Unknown codec: -75"}, true},
		{"unknown source and codec", Release{ID: "sc", Title: "Example.2020.1080p", Size: 1 << 30, Seeders: &seeds, Protocol: "torrent"}, []string{"Unknown source: -150", "Unknown codec: -75"}, true},
		{"no seeders", Release{ID: "0", Title: base.Title, Size: 1 << 30, Seeders: &zero, Protocol: "torrent"}, []string{"No seeders: -200"}, true},
		{"one seeder", Release{ID: "1", Title: base.Title, Size: 1 << 30, Seeders: &one, Protocol: "torrent"}, []string{"Low seeders: -100 (1)"}, true},
		{"two seeders", Release{ID: "2", Title: base.Title, Size: 1 << 30, Seeders: &two, Protocol: "torrent"}, []string{"Low seeders: -100 (2)"}, true},
		{"unlabeled seeders", Release{ID: "nil", Title: base.Title, Size: 1 << 30, Protocol: "usenet"}, nil, true},
		{"near size limit", Release{ID: "big", Title: base.Title, Size: 9 << 30, Seeders: &seeds, Protocol: "torrent"}, []string{"Near size limit: -50"}, true},
	} {
		d := Evaluate(p, target, tc.release)
		if d.Eligible != tc.eligible {
			t.Fatalf("%s: penalties changed eligibility: %+v", tc.name, d)
		}
		if got := penaltyReasons(d.ScoringReasons); strings.Join(got, "|") != strings.Join(tc.penalty, "|") {
			t.Fatalf("%s: unexpected visible penalties: %+v (all %+v)", tc.name, got, d.ScoringReasons)
		}
		again := Evaluate(p, target, tc.release)
		if d.Score != again.Score || strings.Join(d.ScoringReasons, "|") != strings.Join(again.ScoringReasons, "|") {
			t.Fatalf("%s: scoring is not deterministic: %+v %+v", tc.name, d, again)
		}
		if len(tc.penalty) > 0 && d.Score >= Evaluate(p, target, base).Score {
			t.Fatalf("%s: penalized release outranked the preferred baseline: %+v", tc.name, d)
		}
	}
	// No penalty for a known-but-unpreferred label, three or more seeders, or a
	// size at exactly 80% of the profile limit.
	for _, release := range []Release{
		{ID: "known", Title: "Example.2020.1080p.HDTV.x265", Size: 8 << 30, Seeders: &seeds, Protocol: "torrent"},
		{ID: "three", Title: base.Title, Size: 1 << 30, Seeders: ptrInt(3), Protocol: "torrent"},
	} {
		if d := Evaluate(p, target, release); len(penaltyReasons(d.ScoringReasons)) != 0 || !d.Eligible {
			t.Fatalf("unexpected penalty: %+v", d)
		}
	}
	// Penalties are soft: a preferred release still outranks a penalized one.
	ranked := Rank(p, target, []Release{base, {ID: "sparse", Title: base.Title, Size: 1 << 30, Seeders: &one, Protocol: "torrent"}})
	if ranked[0].ID != "base" || ranked[1].ID != "sparse" {
		t.Fatal(ranked)
	}
}

// penaltyReasons filters the visible soft-penalty reasons so the table test
// stays independent of preference and seeder bonus wording.
func TestCustomFormatsCanRankRequireAndExclude(t *testing.T) {
	year := 2020
	seeds := 20
	target := Target{Title: "Example", Year: &year}
	p := Profile{
		Name: "Fixture", Qualities: []int{1080}, Cutoff: 1080, Language: "any",
		MinSeeders: 0, MaxSizeGiB: 10,
		Scoring: Scoring{
			CustomFormats: []CustomFormat{
				{Name: "Proper", Patterns: []string{`\bproper\b`}, Weight: 75},
				{Name: "No cam", Patterns: []string{`\bcam\b`}, Excluded: true},
				{Name: "Required group", Patterns: []string{`-GROUP`}, Required: true},
			},
		},
	}
	if err := Validate(p); err != nil {
		t.Fatal(err)
	}
	base := Release{ID: "base", Title: "Example.2020.1080p.WEB-DL.h264-GROUP", Size: 1 << 30, Seeders: &seeds, Protocol: "torrent"}
	proper := Release{ID: "proper", Title: "Example.2020.1080p.WEB-DL.h264-GROUP.PROPER", Size: 1 << 30, Seeders: &seeds, Protocol: "torrent"}
	d := Evaluate(p, target, base)
	if !d.Eligible || !strings.Contains(strings.Join(d.ScoringReasons, "|"), "Required group") {
		t.Fatalf("required format not applied: %+v", d)
	}
	if Evaluate(p, target, proper).Score <= d.Score {
		t.Fatal("preferred custom format did not raise the score")
	}
	cam := Release{ID: "cam", Title: "Example.2020.1080p.CAM.h264-GROUP", Size: 1 << 30, Seeders: &seeds, Protocol: "torrent"}
	if Evaluate(p, target, cam).Eligible {
		t.Fatal("excluded custom format remained eligible")
	}
	missing := Release{ID: "missing", Title: "Example.2020.1080p.WEB-DL.h264", Size: 1 << 30, Seeders: &seeds, Protocol: "torrent"}
	if Evaluate(p, target, missing).Eligible {
		t.Fatal("required custom format was not enforced")
	}
	for _, bad := range [][]CustomFormat{
		{{Name: "bad", Patterns: []string{"("}}},
		{{Name: "both", Patterns: []string{"x"}, Required: true, Excluded: true}},
		{{Name: "weight", Patterns: []string{"x"}, Weight: 501}},
		{{Name: "bad", Patterns: []string{"x"}}, {Name: "BAD", Patterns: []string{"y"}}},
	} {
		p.Scoring.CustomFormats = bad
		if err := Validate(p); err == nil {
			t.Fatalf("invalid custom format accepted: %#v", bad)
		}
	}
}

func penaltyReasons(reasons []string) []string {
	out := []string{}
	for _, r := range reasons {
		for _, prefix := range []string{"Unknown source:", "Unknown codec:", "Low seeders:", "No seeders:", "Near size limit:"} {
			if strings.HasPrefix(r, prefix) {
				out = append(out, r)
			}
		}
	}
	return out
}

func ptrInt(v int) *int { return &v }

func TestSourceCodecBonusesAndRetentionRemainBelowResolutionAndIdentity(t *testing.T) {
	year := 2020
	seeds := 20
	target := Target{Title: "Example", Year: &year}
	p := Profile{Name: "Fixture", Qualities: []int{720, 1080}, Cutoff: 1080, Language: "any", MinSeeders: 1, MaxSizeGiB: 40, Scoring: Scoring{Sources: []string{"remux", "webdl"}, Codecs: []string{"h265", "h264"}, PreferredTerms: "proper"}}
	base := Release{ID: "base", Title: "Example.2020.1080p.WEB-DL.h264", Size: 1 << 30, Seeders: &seeds}
	preferred := base
	preferred.ID = "preferred"
	preferred.Title = "Example.2020.1080p.BluRay.REMUX.x265.PROPER"
	a, b := Evaluate(p, target, base), Evaluate(p, target, preferred)
	if !a.Eligible || !b.Eligible || b.Score <= a.Score || b.Codec != "h265" || b.Source != "remux" || len(b.ScoringReasons) != 4 {
		t.Fatal(a, b)
	}
	// The best-seeded release wins within one resolution even when its source
	// and codec preferences rank lower.
	seeded := base
	seeded.ID = "seeded"
	many := 4000
	few := 5
	seeded.Seeders = &many
	sparse := base
	sparse.ID = "sparse"
	sparse.Seeders = &few
	if Evaluate(p, target, seeded).Score <= Evaluate(p, target, sparse).Score {
		t.Fatal("seeders did not dominate the preference bonuses")
	}
	low := preferred
	low.Title = "Example.2020.720p.BluRay.REMUX.x265.PROPER"
	if Evaluate(p, target, low).Score >= a.Score {
		t.Fatal("Bonuses overrode resolution")
	}
	foreign := preferred
	foreign.Title = "Other.2020.1080p.BluRay.REMUX.x265.PROPER"
	if Evaluate(p, target, foreign).Eligible {
		t.Fatal("Bonuses bypassed identity")
	}
	now := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	p.Scoring.MaxUsenetAgeDays = 30
	p.Scoring.MinUsenetAgeMinutes = 60
	for _, tc := range []struct {
		date     string
		eligible bool
	}{{now.Add(-2 * time.Hour).Format(time.RFC3339), true}, {now.Add(-31 * 24 * time.Hour).Format(time.RFC3339), false}, {now.Add(-10 * time.Minute).Format(time.RFC3339), false}, {now.Add(time.Hour).Format(time.RFC3339), false}, {"", false}, {"invalid", false}} {
		r := base
		r.Protocol = "usenet"
		r.Seeders = nil
		r.Published = tc.date
		r.Now = now.Unix()
		if d := Evaluate(p, target, r); d.Eligible != tc.eligible {
			t.Fatal(tc, d)
		}
	}
	r := base
	r.Protocol = "torrent"
	r.Published = "bad"
	if !Evaluate(p, target, r).Eligible {
		t.Fatal("Usenet retention affected torrents")
	}
}
func TestScoringPreferencesPersistAndRejectInvalidRules(t *testing.T) {
	ctx := context.Background()
	db, e := database.Open(ctx, t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	s := New(db)
	p, e := s.Get(ctx, 1)
	if e != nil {
		t.Fatal(e)
	}
	p.Scoring = Scoring{Sources: []string{"webdl", "remux"}, Codecs: []string{"av1"}, MaxUsenetAgeDays: 3000, MinUsenetAgeMinutes: 20}
	saved, e := s.Save(ctx, p)
	if e != nil {
		t.Fatal(e)
	}
	got, e := s.Get(ctx, p.ID)
	if e != nil || got.Scoring.MaxUsenetAgeDays != 3000 || got.Scoring.Sources[0] != "webdl" || got.Revision != saved.Revision {
		t.Fatal(got, e)
	}
	for _, rules := range []Scoring{{Sources: []string{"fake"}}, {Codecs: []string{"av1", "av1"}}, {MaxUsenetAgeDays: -1}, {MinUsenetAgeMinutes: 10081}, {MaxUsenetAgeDays: 1, MinUsenetAgeMinutes: 2000}, {PreferredTerms: "\x00"}} {
		p.Scoring = rules
		if Validate(p) == nil {
			t.Fatal("invalid scoring accepted", rules)
		}
	}
}
