package quality

import (
	"context"
	"errors"
	"github.com/faaaaq/ReelScarey/internal/database"
	"testing"
	"time"
)

func TestConservativeIdentityAndProfileRanking(t *testing.T) {
	year := 2020
	seeders := 4
	p := Profile{Name: "Test", Qualities: []int{720, 1080, 2160}, Cutoff: 1080, Language: "english", MinSeeders: 1, MaxSizeGiB: 40, RequiredTerms: "web", RejectedTerms: "av1"}
	target := Target{Title: "A Test: Movie", Year: &year}
	for _, tc := range []struct {
		title string
		ok    bool
	}{
		{"A.Test.Movie.2020.1080p.English.WEB", true},
		{"A.Test.Movie.2019.1080p.English.WEB", false},
		{"Another.A.Test.Movie.2020.1080p.English.WEB", false},
		{"A.Test.Movie.2020.1080p.MULTI.WEB", false},
		{"A.Test.Movie.2020.1080p.English.HDCAM.WEB", false},
		{"A.Test.Movie.2020.1080p.English.WEB.AV1", false},
		{"A.Test.Movie.2020.1080p.720p.English.WEB", false},
		{"A.Test.Movie.2020.English.WEB", false},
	} {
		d := Evaluate(p, target, Release{Title: tc.title, Size: 2 << 30, Seeders: &seeders})
		if d.Eligible != tc.ok {
			t.Errorf("%s: %+v", tc.title, d)
		}
	}
	releases := []Release{{ID: "4k", Title: "A.Test.Movie.2020.2160p.English.WEB", Size: 2 << 30, Seeders: &seeders}, {ID: "720", Title: "A.Test.Movie.2020.720p.English.WEB", Size: 2 << 30, Seeders: &seeders}, {ID: "1080", Title: "A.Test.Movie.2020.1080p.English.WEB", Size: 2 << 30, Seeders: &seeders}}
	ranked := Rank(p, target, releases)
	if ranked[0].ID != "1080" || ranked[1].ID != "720" {
		t.Fatal(ranked)
	}
	if Evaluate(p, target, Release{Title: releases[0].Title, Size: 2 << 30}).Eligible {
		t.Fatal("unknown seed count accepted")
	}
	if Evaluate(p, target, Release{Title: releases[0].Title, Size: 41 << 30, Seeders: &seeders}).Eligible {
		t.Fatal("size limit ignored")
	}
	target = Target{Title: "Test Show", Year: &year, TV: true, Season: 1, Episode: 2}
	for _, title := range []string{"Test.Show.S01E02.E03.1080p.English.WEB", "Test.Show.S01E02+03.1080p.English.WEB", "Test.Show.S01E02.COMPLETE.1080p.English.WEB", "Test.Show.S01E02.SEASON.PACK.1080p.English.WEB"} {
		if Evaluate(p, target, Release{Title: title, Size: 2 << 30, Seeders: &seeders}).Eligible {
			t.Fatal("pack or multiple episode selection accepted", title)
		}
	}
	for _, tc := range []struct {
		title string
		ok    bool
	}{{"Test.Show.S01E02.1080p", true}, {"Test.Show.2020.S01E02.1080p", true}, {"Test.Show.S01E03.1080p", false}, {"Test.Show.S01E02E03.1080p", false}, {"Test.Show.S01E02-E04.1080p", false}, {"Test.Show.S01.1080p", false}, {"Other.Show.S01E02.1080p", false}, {"Test.Show.S01E02.S02E02.1080p", false}, {"Presenter Test Show S01E02.1080p", true}, {"Presenter Other.Show.S01E02.1080p", false}, {"TestShow.S01E02.1080p", false}, {"Jimmy Carrs Test Show S01E02 Mates Rates 1080p AMZN WEB-DL DDP2 0 H 264-RAWR", true}} {
		if Identify(tc.title, target) != tc.ok {
			t.Error(tc)
		}
	}
}
func TestGrabDelayValidationPersistenceAndRevisionConflict(t *testing.T) {
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
	if p.GrabDelayMinutes != 0 || p.GrabDelay() != 0 {
		t.Fatalf("unexpected default grab delay: %+v", p)
	}
	p.GrabDelayMinutes = 90
	saved, e := s.Save(ctx, p)
	if e != nil {
		t.Fatal(e)
	}
	if saved.GrabDelayMinutes != 90 || saved.Revision != p.Revision+1 {
		t.Fatalf("save did not return the stored delay: %+v", saved)
	}
	got, e := s.Get(ctx, p.ID)
	if e != nil || got.GrabDelayMinutes != 90 || got.GrabDelay() != 90*time.Minute || got.Revision != saved.Revision {
		t.Fatal(got, e)
	}
	if _, e = s.Save(ctx, p); !errors.Is(e, ErrConflict) {
		t.Fatal("stale grab-delay update accepted", e)
	}
	for _, delay := range []int{-1, 1441} {
		invalid := got
		invalid.GrabDelayMinutes = delay
		if _, e = s.Save(ctx, invalid); e == nil {
			t.Fatal("invalid grab delay accepted", delay)
		}
	}
	created := got
	created.ID, created.Name, created.Revision = 0, "Delayed profile", 0
	created.GrabDelayMinutes = 1440
	inserted, e := s.Save(ctx, created)
	if e != nil {
		t.Fatal(e)
	}
	if got, e = s.Get(ctx, inserted.ID); e != nil || got.GrabDelayMinutes != 1440 || got.GrabDelay() != 24*time.Hour {
		t.Fatal(got, e)
	}
	for _, delay := range []int{-1, 1441} {
		invalid := got
		invalid.GrabDelayMinutes = delay
		if Validate(invalid) == nil {
			t.Fatal("profile outside the grab-delay range accepted", delay)
		}
	}
}
func TestProfilesPersistAndRejectStaleWrites(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, e := database.Open(ctx, dir)
	if e != nil {
		t.Fatal(e)
	}
	s := New(db)
	p, e := s.Get(ctx, 1)
	if e != nil {
		t.Fatal(e)
	}
	old := p
	p.Name = "My profile"
	p.Language = "japanese"
	p, e = s.Save(ctx, p)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Save(ctx, old); !errors.Is(e, ErrConflict) {
		t.Fatal("stale update accepted", e)
	}
	invalid := p
	invalid.Qualities = []int{1080, 720}
	if _, e = s.Save(ctx, invalid); e == nil {
		t.Fatal("unordered qualities accepted")
	}
	invalid = p
	invalid.ID = 0
	invalid.Name = "MY PROFILE"
	if _, e = s.Save(ctx, invalid); e == nil {
		t.Fatal("duplicate name accepted")
	}
	db.Close()
	db, e = database.Open(ctx, dir)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	got, e := New(db).Get(ctx, p.ID)
	if e != nil || got.Language != "japanese" || got.Revision != 2 {
		t.Fatal(got, e)
	}
	if VerifiedQuality(1920, 800) != 1080 || VerifiedQuality(3840, 1600) != 2160 || VerifiedQuality(0, 0) != 0 {
		t.Fatal("cropped media classification")
	}
}
