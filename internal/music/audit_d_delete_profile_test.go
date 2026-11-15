package music

// Regression tests from the independent production-readiness audit
// (audit_d_*): deleting a custom profile must clear every reference to it in
// the same transaction, so downloads never silently stop converting because
// the global preference or an album override names a profile that no longer
// exists.

import (
	"context"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/database"
)

func TestAuditDDeleteCustomProfileClearsReferences(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO music_artists(id,musicbrainz_id,name,monitored,status,created_at,updated_at) VALUES(1,?, 'Artist',1,'missing',1,1)`, testMBID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO music_albums(id,artist_id,musicbrainz_id,title,monitored,status,created_at,updated_at) VALUES(1,1,?, 'Album',1,'missing',1,1)`, testRGID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO music_albums(id,artist_id,musicbrainz_id,title,monitored,status,created_at,updated_at) VALUES(2,1,? || '-2', 'Second',1,'missing',1,1)`, testRGID); err != nil {
		t.Fatal(err)
	}
	service := NewService(db, "")
	custom, err := service.SaveCustomProfile(ctx, QualityProfile{Name: "Car Opus", Codec: "opus", BitrateKbps: 128})
	if err != nil {
		t.Fatal(err)
	}
	// Reference the profile by key from the global preference and by key and
	// name from two album overrides.
	if _, err := db.Exec(`UPDATE music_preferences SET download_profile=? WHERE id=1`, custom.Key); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE music_albums SET download_profile=? WHERE id=1`, custom.Key); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE music_albums SET download_profile=? WHERE id=2`, custom.Name); err != nil {
		t.Fatal(err)
	}

	if err := service.DeleteCustomProfile(ctx, custom.ID); err != nil {
		t.Fatal(err)
	}

	var global, albumOne, albumTwo string
	if err := db.QueryRow(`SELECT download_profile FROM music_preferences WHERE id=1`).Scan(&global); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT download_profile FROM music_albums WHERE id=1`).Scan(&albumOne); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT download_profile FROM music_albums WHERE id=2`).Scan(&albumTwo); err != nil {
		t.Fatal(err)
	}
	if global != "" || albumOne != "" || albumTwo != "" {
		t.Fatalf("deleted profile references survived: global=%q album1=%q album2=%q", global, albumOne, albumTwo)
	}
	// The row itself is gone.
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM music_quality_profiles WHERE id=?`, custom.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("deleted custom profile row survived")
	}
	// Deleting an unknown id stays an invalid input, not an error leak.
	if err := service.DeleteCustomProfile(ctx, custom.ID); err == nil {
		t.Fatal("double delete accepted")
	}
}

func TestAuditDDeleteCustomProfileKeepsUnrelatedOverrides(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO music_artists(id,musicbrainz_id,name,monitored,status,created_at,updated_at) VALUES(1,?, 'Artist',1,'missing',1,1)`, testMBID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO music_albums(id,artist_id,musicbrainz_id,title,monitored,status,created_at,updated_at) VALUES(1,1,?, 'Album',1,'missing',1,1)`, testRGID); err != nil {
		t.Fatal(err)
	}
	service := NewService(db, "")
	if _, err := service.SaveCustomProfile(ctx, QualityProfile{Name: "Car Opus", Codec: "opus", BitrateKbps: 128}); err != nil {
		t.Fatal(err)
	}
	other, err := service.SaveCustomProfile(ctx, QualityProfile{Name: "Home Opus", Codec: "opus", BitrateKbps: 96})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE music_preferences SET download_profile=? WHERE id=1`, other.Key); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE music_albums SET download_profile=? WHERE id=1`, other.Name); err != nil {
		t.Fatal(err)
	}
	// Delete the first (unreferenced) profile.
	var firstID int64
	if err := db.QueryRow(`SELECT id FROM music_quality_profiles WHERE name='Car Opus'`).Scan(&firstID); err != nil {
		t.Fatal(err)
	}
	if err := service.DeleteCustomProfile(ctx, firstID); err != nil {
		t.Fatal(err)
	}
	var global, album string
	if err := db.QueryRow(`SELECT download_profile FROM music_preferences WHERE id=1`).Scan(&global); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT download_profile FROM music_albums WHERE id=1`).Scan(&album); err != nil {
		t.Fatal(err)
	}
	if global != other.Key || album != other.Name {
		t.Fatalf("unrelated references were cleared: global=%q album=%q", global, album)
	}
}
