package subtitle

import (
	"context"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/database"
)

func TestLanguageProfilesOverrideLibraryAndItemSubtitleTracks(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	service := &Service{db: db}
	english, err := service.SaveLanguageProfile(ctx, 0, LanguageProfile{
		Name: "English", Tracks: []Track{{Language: "en", Variant: "full"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	japanese, err := service.SaveLanguageProfile(ctx, 0, LanguageProfile{
		Name: "Japanese", Tracks: []Track{{Language: "ja", Variant: "full"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defaults, err := service.LanguageDefaults(ctx)
	if err != nil || len(defaults) != 3 {
		t.Fatalf("defaults: %#v %v", defaults, err)
	}
	for i := range defaults {
		if defaults[i].LibraryKey == "movies" {
			defaults[i].ProfileID = &english.ID
		}
	}
	if _, err := service.SaveLanguageDefaults(ctx, defaults); err != nil {
		t.Fatal(err)
	}
	preferences := service.preferencesFor(ctx, media{Target: Target{MediaID: 1, Kind: "movie"}}, Preferences{Tracks: []Track{{Language: "es", Variant: "full"}}})
	if len(preferences.Tracks) != 1 || preferences.Tracks[0].Language != "en" {
		t.Fatalf("library fallback: %#v", preferences.Tracks)
	}
	if _, err := db.Exec(`INSERT INTO media_items(kind,tmdb_id,title,monitored) VALUES('movie',900002,'Language fixture',1)`); err != nil {
		t.Fatal(err)
	}
	var mediaID int64
	if err := db.QueryRow(`SELECT id FROM media_items WHERE tmdb_id=900002`).Scan(&mediaID); err != nil {
		t.Fatal(err)
	}
	if err := service.SetMediaLanguageProfile(ctx, mediaID, &japanese.ID); err != nil {
		t.Fatal(err)
	}
	preferences = service.preferencesFor(ctx, media{MediaID: mediaID}, Preferences{Tracks: []Track{{Language: "en", Variant: "full"}}})
	if len(preferences.Tracks) != 1 || preferences.Tracks[0].Language != "ja" {
		t.Fatalf("item override: %#v", preferences.Tracks)
	}
	if err := service.SetMediaLanguageProfile(ctx, mediaID, nil); err != nil {
		t.Fatal(err)
	}
	preferences = service.preferencesFor(ctx, media{Target: Target{MediaID: mediaID, Kind: "movie"}}, Preferences{Tracks: []Track{{Language: "en", Variant: "full"}}})
	if len(preferences.Tracks) != 1 || preferences.Tracks[0].Language != "en" {
		// The library default is still English; only the item override was removed.
		t.Fatalf("unexpected fallback: %#v", preferences.Tracks)
	}
}
