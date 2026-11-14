package transfer

import (
	"context"
	"strings"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/library"
	"github.com/faaaaq/ReelScarey/internal/quality"
	"time"
)

// enableAnimeDubOnly marks the fixture show as an anime with the strict
// English-dub requirement and saves the default rule.
func enableAnimeDubOnly(t *testing.T, f *workflowFixture, mediaID int64) {
	t.Helper()
	ctx := context.Background()
	catalog := library.New(f.db)
	anime, err := catalog.Anime(ctx, mediaID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = catalog.SaveAnime(ctx, mediaID, library.AnimeUpdate{Category: "anime", Revision: anime.Revision, NumberingRevision: anime.NumberingRevision, Episodes: nil}); err != nil {
		t.Fatal(err)
	}
	policy, err := catalog.AnimeSearch(ctx, mediaID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = catalog.SaveAnimeSearch(ctx, mediaID, library.AnimeSearchUpdate{Revision: policy.Revision, CategoryRevision: policy.CategoryRevision, NumberingRevision: policy.NumberingRevision, Mode: "season", SearchTitle: "Selected & Alias", AlternateTitles: []string{"Selected & Alias", "Unused Alias"}, EnglishDubOnly: true}); err != nil {
		t.Fatal(err)
	}
	prefs, err := catalog.AnimePreferences(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = catalog.SaveAnimePreferences(ctx, library.AnimePreferences{DubRule: quality.DefaultDubRule, Revision: prefs.Revision}); err != nil {
		t.Fatal(err)
	}
}

// TestAnimeDubRequirementGatesUnsnapshottedQueues proves the queue-time choke:
// a show flagged English-dub-only rejects releases that do not prove English
// audio on EVERY path that carries no AnimeSearch snapshot â€” manual
// interactive queues, RSS/feed candidates and reviewed mapped packs.
func TestAnimeDubRequirementGatesUnsnapshottedQueues(t *testing.T) {
	f, mediaID, _ := setupTV(t)
	ctx := context.Background()
	enableAnimeDubOnly(t, f, mediaID)
	tx, err := f.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	reject := []string{
		"Show - 01 - Dual Audio",
		"Show - 01 - Dual Audio JPN",
		"Show - 01 - Dual Audio - Japanese",
		"Show - 01 [Japanese] [English Subs]",
		"[SubsPlease] Show - 01 [1080p] [Eng Sub]",
		"Show - 01 [1080p]",
		"Show - 01 [RAW]",
	}
	for _, title := range reject {
		if err := validateAnimeDubRequirement(ctx, tx, Candidate{MovieID: mediaID, Title: title}); err == nil {
			t.Fatal("release without English-audio proof was accepted", title)
		} else if !strings.Contains(err.Error(), quality.DubRejectionMessage) {
			t.Fatal("rejection reason not understandable", title, err.Error())
		}
	}
	accept := []string{
		"Show - 01 [English Dub] [1080p]",
		"Show - 01 [Eng Dub]",
		"Show - 01 - Dual Audio ENG/JPN",
		"Show - 01 - Dual Audio JPN/ENG",
		"Show - 01 FuniDub",
	}
	for _, title := range accept {
		if err := validateAnimeDubRequirement(ctx, tx, Candidate{MovieID: mediaID, Title: title}); err != nil {
			t.Fatal("genuine English-dub release rejected", title, err)
		}
	}
	// A show without the flag is unaffected by the gate.
	if err := validateAnimeDubRequirement(ctx, tx, Candidate{MovieID: mediaID + 100000, Title: "Other Show - 01 - RAW"}); err != nil {
		t.Fatal("gate leaked to a non-dub-only show", err)
	}
}

// TestAnimeDubOnlyAutomaticSearchFiltersCandidates proves the pre-selection
// filter for the scheduled automatic path: with the flag set, a Japanese-only
// or subtitle-only candidate is dropped before ranking, and the same search
// without the flag still returns it.
func TestAnimeDubOnlyAutomaticSearchFiltersCandidates(t *testing.T) {
	f, mediaID, capture := animeSearchTorznabReview(t, false, "season")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// Control: without the requirement the fixture's [English] (subtitle)
	// release is returned.
	results, err := f.s.SearchAnimeAutomatic(ctx, mediaID, 1, 1, false)
	if err != nil || len(results) != 1 {
		t.Fatal("control search", len(results), err)
	}
	enableAnimeDubOnly(t, f, mediaID)
	before := capture.requests
	results, err = f.s.SearchAnimeAutomatic(ctx, mediaID, 1, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatal("dub-only show accepted a release without English-audio proof", results)
	}
	if capture.requests <= before {
		t.Fatal("the filtered search did not reach the indexer")
	}
}
