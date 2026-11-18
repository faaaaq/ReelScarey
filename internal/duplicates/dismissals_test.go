package duplicates

import (
	"context"
	"testing"
)

func TestDismissalHidesAndRestoresGroups(t *testing.T) {
	db := setupDuplicates(t)
	ctx := context.Background()

	// A movie group and an episode-style key that must be refused for movies.
	if err := Dismiss(ctx, db, Key{Kind: "movie", MediaID: 2}); err != nil {
		t.Fatal(err)
	}
	if err := Dismiss(ctx, db, Key{Kind: "episode", MediaID: 1}); err == nil {
		t.Fatal("movie key accepted an episode dismissal")
	}
	report, err := Review(ctx, db, 10, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, group := range report.Groups {
		if group.MediaID == 2 {
			t.Fatal("dismissed group still listed")
		}
		if group.Dismissed {
			t.Fatal("default report labeled a dismissed group")
		}
	}
	if report.Total != 4 {
		t.Fatalf("total ignored the dismissal: %d", report.Total)
	}

	inclusive, err := Review(ctx, db, 10, true)
	if err != nil {
		t.Fatal(err)
	}
	var flagged int
	for _, group := range inclusive.Groups {
		if group.MediaID == 2 {
			flagged++
			if !group.Dismissed {
				t.Fatal("inclusive report did not label the dismissed group")
			}
		} else if group.Dismissed {
			t.Fatal("inclusive report labeled an active group")
		}
	}
	if flagged != 1 || inclusive.Total != 5 {
		t.Fatalf("unexpected inclusive report: flagged=%d total=%d", flagged, inclusive.Total)
	}

	if err := Restore(ctx, db, Key{Kind: "movie", MediaID: 2}); err != nil {
		t.Fatal(err)
	}
	report, err = Review(ctx, db, 10, false)
	if err != nil {
		t.Fatal(err)
	}
	if report.Total != 5 || len(report.Groups) != 5 {
		t.Fatalf("restored group missing: total=%d groups=%d", report.Total, len(report.Groups))
	}

	// Re-dismissing refreshes the recorded decision instead of failing.
	if err := Dismiss(ctx, db, Key{Kind: "movie", MediaID: 3}); err != nil {
		t.Fatal(err)
	}
	if err := Dismiss(ctx, db, Key{Kind: "movie", MediaID: 3}); err != nil {
		t.Fatal(err)
	}
	report, err = Review(ctx, db, 10, false)
	if err != nil {
		t.Fatal(err)
	}
	if report.Total != 4 {
		t.Fatalf("re-dismissal changed the count: %d", report.Total)
	}
}
