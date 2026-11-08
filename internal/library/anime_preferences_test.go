package library

import (
	"context"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/quality"
)

func TestAnimePreferencesAndEnglishDubToggle(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec(`INSERT INTO media_items(id,kind,tmdb_id,title,monitored,status,category) VALUES(1,'series',1,'Anime Show',1,'missing','anime')`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO series_details(media_id,revision) VALUES(1,1)`); err != nil {
		t.Fatal(err)
	}
	store := New(db)
	prefs, err := store.AnimePreferences(ctx)
	if err != nil || prefs.DubRule != quality.DefaultDubRule {
		t.Fatal("default rule", prefs, err)
	}
	saved, err := store.SaveAnimePreferences(ctx, AnimePreferences{DubRule: "Custom Dub", Revision: prefs.Revision})
	if err != nil || saved.DubRule != "Custom Dub" || saved.Revision != prefs.Revision+1 {
		t.Fatal("save rule", saved, err)
	}
	if _, err = store.SaveAnimePreferences(ctx, AnimePreferences{DubRule: "Custom Dub", Revision: prefs.Revision}); err == nil {
		t.Fatal("stale rule revision accepted")
	}
	count, err := store.SetAllAnimeEnglishDub(ctx, true)
	if err != nil || count != 1 {
		t.Fatal("bulk enable", count, err)
	}
	settings, err := store.AnimeSearch(ctx, 1)
	if err != nil || !settings.EnglishDubOnly {
		t.Fatal("bulk toggle not saved", settings, err)
	}
	if _, err = store.SetAllAnimeEnglishDub(ctx, false); err != nil {
		t.Fatal(err)
	}
	settings, err = store.AnimeSearch(ctx, 1)
	if err != nil || settings.EnglishDubOnly {
		t.Fatal("bulk disable not saved", settings, err)
	}
}
