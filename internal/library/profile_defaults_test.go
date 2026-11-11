package library

import (
	"context"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/database"
)

func TestProfileDefaultsAreRevisionChecked(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := New(db)
	items, err := store.ProfileDefaults(ctx)
	if err != nil || len(items) != 3 {
		t.Fatalf("defaults: %#v %v", items, err)
	}
	profile := int64(1)
	for i := range items {
		if items[i].LibraryKey == "movies" {
			items[i].ProfileID = &profile
		}
	}
	saved, err := store.SaveProfileDefaults(ctx, items)
	if err != nil {
		t.Fatal(err)
	}
	if saved[0].ProfileID == nil || *saved[0].ProfileID != 1 {
		t.Fatalf("saved defaults: %#v", saved)
	}
	if got, err := store.DefaultProfileID(ctx, "movies"); err != nil || got != 1 {
		t.Fatalf("effective default: %d %v", got, err)
	}
	if _, err := store.SaveProfileDefaults(ctx, items); err == nil {
		t.Fatal("stale revisions were accepted")
	}
}
