package music

import (
	"context"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/database"
)

// Independent audit of the multi-format variant model: preferred-file
// selection must pick a primary row, tolerate two primaries by taking the
// oldest, and never list one track more than once no matter how many
// archive/variant copies exist. ListTrackVariants must order the primary
// first. A track whose rows are all archive/variant (the zero-primary state
// the audit probed for) is reported honestly with no file selected.
func TestAuditDVariantPreferredFileSelection(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewStore(db)

	year := 1997
	if _, err = store.SaveArtist(ctx, Artist{
		MusicBrainzID: testMBID, Name: "Radiohead", SortName: "Radiohead",
		Monitored: true, Status: "missing",
	}, []Album{{
		MusicBrainzID: testRGID, Title: "OK Computer", PrimaryType: "Album",
		Year: &year, Monitored: true, Status: "missing",
	}}); err != nil {
		t.Fatal(err)
	}
	album, err := store.AlbumByMusicBrainzID(ctx, testRGID)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.SaveTracks(ctx, album.ID, []Track{
		{MusicBrainzID: testTrackID, RecordingID: testRecordingID, Disc: 1, Number: 1, Title: "Airbag", DurationMS: 284000},
	}); err != nil {
		t.Fatal(err)
	}
	tracks, err := store.Tracks(ctx, album.ID)
	if err != nil || len(tracks) != 1 {
		t.Fatal("catalog track missing", err)
	}
	trackID := tracks[0].ID

	insertFile := func(role, profile, name, format string) int64 {
		t.Helper()
		result, err := db.Exec(`INSERT INTO music_files(artist_id,album_id,track_id,root,relative_path,size_bytes,sha256,format,bitrate,imported_at,origin,variant_role,variant_profile)
			VALUES(1,1,?,'media',?,10,'sha','`+format+`',128,unixepoch(),'download',?,?)`,
			trackID, "Artist/Album/"+name, role, profile)
		if err != nil {
			t.Fatal(err)
		}
		id, err := result.LastInsertId()
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	primary := insertFile("primary", "", "01 Airbag.flac", "FLAC")
	variant := insertFile("variant", "opus-128", "01 Airbag [opus-128].opus", "opus")
	archive := insertFile("archive", "", "01 Airbag (original).flac", "FLAC")

	// Every library listing selects exactly the primary row, and the extra
	// copies never duplicate the track entry.
	again, err := store.Tracks(ctx, album.ID)
	if err != nil || len(again) != 1 || again[0].FileID == nil || *again[0].FileID != primary {
		t.Fatal("Tracks did not prefer the primary file", again, err)
	}
	listed, err := store.ListTracks(ctx, "", 1, 50)
	if err != nil || listed.Total != 1 || len(listed.Items) != 1 || listed.Items[0].FileID == nil || *listed.Items[0].FileID != primary {
		t.Fatal("ListTracks did not prefer the primary file", listed, err)
	}
	artistTracks, err := store.ArtistTracks(ctx, 1, 1, 50)
	if err != nil || artistTracks.Total != 1 || len(artistTracks.Items) != 1 || artistTracks.Items[0].FileID == nil || *artistTracks.Items[0].FileID != primary {
		t.Fatal("ArtistTracks did not prefer the primary file", artistTracks, err)
	}

	// ListTrackVariants orders the primary first, remaining copies by ID.
	variants, err := store.ListTrackVariants(ctx, trackID)
	if err != nil || len(variants) != 3 {
		t.Fatal("ListTrackVariants", err)
	}
	if variants[0].ID != primary || variants[0].VariantRole != "primary" {
		t.Fatal("primary must come first", variants)
	}
	if variants[1].ID != variant || variants[1].VariantProfile != "opus-128" || variants[2].ID != archive || variants[2].VariantRole != "archive" {
		t.Fatal("variant ordering", variants)
	}

	// Two primaries: the oldest registered primary wins (MIN(id)), which
	// keeps selection stable when an aside rename left a second primary row.
	insertFile("primary", "", "01 Airbag (aside).flac", "FLAC")
	again, err = store.Tracks(ctx, album.ID)
	if err != nil || again[0].FileID == nil || *again[0].FileID != primary {
		t.Fatal("two primaries must select the oldest", again, err)
	}
}

func TestAuditDVariantZeroPrimaryReported(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewStore(db)

	if _, err = store.SaveArtist(ctx, Artist{
		MusicBrainzID: testMBID, Name: "Radiohead", SortName: "Radiohead",
		Monitored: true, Status: "missing",
	}, []Album{{
		MusicBrainzID: testRGID, Title: "OK Computer", PrimaryType: "Album",
		Monitored: true, Status: "missing",
	}}); err != nil {
		t.Fatal(err)
	}
	album, err := store.AlbumByMusicBrainzID(ctx, testRGID)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.SaveTracks(ctx, album.ID, []Track{
		{MusicBrainzID: testTrackID, RecordingID: testRecordingID, Disc: 1, Number: 1, Title: "Airbag", DurationMS: 284000},
	}); err != nil {
		t.Fatal(err)
	}
	tracks, err := store.Tracks(ctx, album.ID)
	if err != nil || len(tracks) != 1 {
		t.Fatal(err)
	}
	trackID := tracks[0].ID
	for _, row := range []struct{ role, name string }{
		{"archive", "01 Airbag (original).flac"},
		{"variant", "01 Airbag [opus-128].opus"},
	} {
		if _, err = db.Exec(`INSERT INTO music_files(artist_id,album_id,track_id,root,relative_path,size_bytes,sha256,format,bitrate,imported_at,origin,variant_role,variant_profile)
			VALUES(1,1,?,'media',?,10,'sha','FLAC',128,unixepoch(),'download',?,'')`,
			trackID, "Artist/Album/"+row.name, row.role); err != nil {
			t.Fatal(err)
		}
	}

	// The zero-primary state (a primary removed outside the current paired
	// flows) must not silently pretend a copy is the primary: the track stays
	// listed but reports no preferred file. The Subsonic layer still serves
	// its most playable copy; the library layer does not fabricate one.
	again, err := store.Tracks(ctx, album.ID)
	if err != nil || len(again) != 1 {
		t.Fatal(err)
	}
	if again[0].FileID != nil {
		t.Fatal("a variant or archive copy must never be presented as the primary", *again[0].FileID)
	}
}
