package quality

import (
	"context"
	"github.com/faaaaq/ReelScarey/internal/database"
	"testing"
	"time"
)

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
