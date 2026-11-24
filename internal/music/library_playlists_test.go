package music

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/database"
)

func libraryPlaylistFixture(t *testing.T) (*Service, *sql.DB, string) {
	t.Helper()
	dir := t.TempDir()
	db, err := database.Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`INSERT INTO music_artists(id,musicbrainz_id,name,monitored,status,created_at,updated_at)
		VALUES(1,'artist-playlists','Playlist Artist',1,'available',1,1);
	INSERT INTO music_albums(id,artist_id,musicbrainz_id,title,year,cover_url,monitored,status,created_at,updated_at)
		VALUES(2,1,'album-playlists','Playlist Album',2024,'cover.jpg',1,'available',1,1);
	INSERT INTO music_tracks(id,album_id,musicbrainz_id,recording_id,disc,number,title,duration_ms,created_at,updated_at) VALUES
		(11,2,'track-11','recording-11',1,1,'One',1000,1,1),
		(12,2,'track-12','recording-12',1,2,'Two',2000,1,1),
		(13,2,'track-13','recording-13',1,3,'Three',3000,1,1);
	INSERT INTO music_files(id,artist_id,album_id,track_id,root,relative_path,size_bytes,format,bitrate,duration_ms,imported_at,origin,variant_role) VALUES
		(101,1,2,11,'Music','one-preferred.flac',100,'flac',900,1000,1,'existing','primary'),
		(102,1,2,11,'Music','one-other.mp3',90,'mp3',320,1000,1,'existing','primary'),
		(103,1,2,12,'Music','two.flac',200,'flac',800,2000,1,'existing','primary')`)
	if err != nil {
		t.Fatal(err)
	}
	return NewService(db, t.TempDir()), db, dir
}

func playlistTrackIDs(details LibraryPlaylistDetails) []int64 {
	ids := make([]int64, 0, len(details.Tracks))
	for _, track := range details.Tracks {
		ids = append(ids, track.ID)
	}
	return ids
}

func requireContemporaryPlaylistOrder(t *testing.T, db *sql.DB, details LibraryPlaylistDetails, revision int, ids ...int64) {
	t.Helper()
	if details.Playlist.Revision != revision || details.Playlist.TrackCount != len(ids) {
		t.Fatalf("playlist summary = %#v, want revision %d and %d tracks", details.Playlist, revision, len(ids))
	}
	got := playlistTrackIDs(details)
	if len(got) != len(ids) {
		t.Fatalf("track order = %v, want %v", got, ids)
	}
	for i := range ids {
		if got[i] != ids[i] {
			t.Fatalf("track order = %v, want %v", got, ids)
		}
	}
	rows, err := db.Query(`SELECT position,track_id FROM music_library_playlist_entries WHERE playlist_id=? ORDER BY position`, details.Playlist.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	index := 0
	for rows.Next() {
		var position int
		var trackID int64
		if err := rows.Scan(&position, &trackID); err != nil {
			t.Fatal(err)
		}
		if index >= len(ids) || position != index+1 || trackID != ids[index] {
			t.Fatalf("non-contiguous entry position=%d track=%d index=%d want=%v", position, trackID, index, ids)
		}
		index++
	}
	if err := rows.Err(); err != nil || index != len(ids) {
		t.Fatalf("stored playlist entries: count=%d err=%v", index, err)
	}
}

func TestLibraryPlaylistLifecycleRevisionChecksAndReopen(t *testing.T) {
	ctx := context.Background()
	service, db, dir := libraryPlaylistFixture(t)

	empty, err := service.ListLibraryPlaylists(ctx)
	if err != nil || empty.Items == nil || len(empty.Items) != 0 {
		t.Fatalf("empty list = %#v, %v", empty, err)
	}
	if _, err := service.LibraryPlaylist(ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown playlist = %v", err)
	}
	emptyDetails, err := service.CreateLibraryPlaylist(ctx, "Empty", nil)
	if err != nil || emptyDetails.Tracks == nil || len(emptyDetails.Tracks) != 0 || emptyDetails.Playlist.TrackCount != 0 {
		t.Fatalf("empty details = %#v, %v", emptyDetails, err)
	}
	if err := service.DeleteLibraryPlaylist(ctx, emptyDetails.Playlist.ID, 1); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"", "  ", strings.Repeat("x", 101), "bad\nname", string([]byte{0xff})} {
		if _, err := service.CreateLibraryPlaylist(ctx, name, nil); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("invalid name %q = %v", name, err)
		}
	}

	initialTrack := int64(11)
	created, err := service.CreateLibraryPlaylist(ctx, "  Focus  ", &initialTrack)
	if err != nil {
		t.Fatal(err)
	}
	if created.Playlist.Name != "Focus" || created.Playlist.CreatedAt < 1 || created.Playlist.UpdatedAt < 1 || created.Playlist.DurationMS != 1000 {
		t.Fatalf("created playlist = %#v", created.Playlist)
	}
	requireContemporaryPlaylistOrder(t, db, created, 1, 11)
	if created.Tracks[0].ArtistName != "Playlist Artist" || created.Tracks[0].AlbumTitle != "Playlist Album" || created.Tracks[0].FileID == nil || *created.Tracks[0].FileID != 101 || created.Tracks[0].Format != "flac" {
		t.Fatalf("preferred-primary metadata = %#v", created.Tracks[0])
	}
	missingTrack := int64(999)
	if _, err := service.CreateLibraryPlaylist(ctx, "Missing", &missingTrack); !errors.Is(err, ErrNotFound) {
		t.Fatalf("create with unknown track = %v", err)
	}

	listed, err := service.ListLibraryPlaylists(ctx)
	if err != nil || len(listed.Items) != 1 || listed.Items[0].Name != "Focus" || listed.Items[0].TrackCount != 1 || listed.Items[0].DurationMS != 1000 {
		t.Fatalf("list = %#v, %v", listed, err)
	}
	if _, err := service.RenameLibraryPlaylist(ctx, created.Playlist.ID, "Stale", 99); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale rename = %v", err)
	}
	renamed, err := service.RenameLibraryPlaylist(ctx, created.Playlist.ID, "  Renamed  ", 1)
	if err != nil {
		t.Fatal(err)
	}
	requireContemporaryPlaylistOrder(t, db, renamed, 2, 11)
	if renamed.Playlist.Name != "Renamed" || renamed.Playlist.CreatedAt != created.Playlist.CreatedAt {
		t.Fatalf("rename changed identity/timestamps: %#v", renamed.Playlist)
	}

	if _, err := service.AddLibraryPlaylistTrack(ctx, renamed.Playlist.ID, 999, 2); !errors.Is(err, ErrNotFound) {
		t.Fatalf("add unknown track = %v", err)
	}
	if _, err := service.AddLibraryPlaylistTrack(ctx, renamed.Playlist.ID, 11, 2); !errors.Is(err, ErrLibraryPlaylistTrackExists) {
		t.Fatalf("duplicate add = %v", err)
	}
	if _, err := service.AddLibraryPlaylistTrack(ctx, renamed.Playlist.ID, 12, 1); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale add = %v", err)
	}
	unchanged, err := service.LibraryPlaylist(ctx, renamed.Playlist.ID)
	if err != nil {
		t.Fatal(err)
	}
	requireContemporaryPlaylistOrder(t, db, unchanged, 2, 11)

	withTwo, err := service.AddLibraryPlaylistTrack(ctx, renamed.Playlist.ID, 12, 2)
	if err != nil {
		t.Fatal(err)
	}
	requireContemporaryPlaylistOrder(t, db, withTwo, 3, 11, 12)
	withThree, err := service.AddLibraryPlaylistTrack(ctx, renamed.Playlist.ID, 13, 3)
	if err != nil {
		t.Fatal(err)
	}
	requireContemporaryPlaylistOrder(t, db, withThree, 4, 11, 12, 13)
	if withThree.Playlist.DurationMS != 6000 {
		t.Fatalf("duration = %d", withThree.Playlist.DurationMS)
	}

	// Losing a file never removes or rewrites a canonical playlist entry.
	if _, err := db.Exec(`DELETE FROM music_files WHERE track_id=12`); err != nil {
		t.Fatal(err)
	}
	withoutFile, err := service.LibraryPlaylist(ctx, renamed.Playlist.ID)
	if err != nil {
		t.Fatal(err)
	}
	requireContemporaryPlaylistOrder(t, db, withoutFile, 4, 11, 12, 13)
	if withoutFile.Tracks[1].FileID != nil || withoutFile.Tracks[1].RelativePath != "" || withoutFile.Playlist.DurationMS != 6000 {
		t.Fatalf("missing file was not represented honestly: %#v", withoutFile)
	}

	for _, ids := range [][]int64{{11, 11, 12}, {11, 12}, {11, 12, 999}, {11, 12, 13, 13}} {
		if _, err := service.ReorderLibraryPlaylist(ctx, renamed.Playlist.ID, ids, 4); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("invalid reorder %v = %v", ids, err)
		}
	}
	reordered, err := service.ReorderLibraryPlaylist(ctx, renamed.Playlist.ID, []int64{13, 11, 12}, 4)
	if err != nil {
		t.Fatal(err)
	}
	requireContemporaryPlaylistOrder(t, db, reordered, 5, 13, 11, 12)
	if _, err := service.ReorderLibraryPlaylist(ctx, renamed.Playlist.ID, []int64{13, 11, 12}, 4); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale reorder = %v", err)
	}
	if _, err := service.RemoveLibraryPlaylistTrack(ctx, renamed.Playlist.ID, 999, 5); !errors.Is(err, ErrNotFound) {
		t.Fatalf("remove unknown track = %v", err)
	}
	removed, err := service.RemoveLibraryPlaylistTrack(ctx, renamed.Playlist.ID, 12, 5)
	if err != nil {
		t.Fatal(err)
	}
	requireContemporaryPlaylistOrder(t, db, removed, 6, 13, 11)
	if _, err := service.RemoveLibraryPlaylistTrack(ctx, renamed.Playlist.ID, 11, 5); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale remove = %v", err)
	}

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopenedDB, err := database.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopenedDB.Close()
	reopened := NewService(reopenedDB, t.TempDir())
	persisted, err := reopened.LibraryPlaylist(ctx, renamed.Playlist.ID)
	if err != nil {
		t.Fatal(err)
	}
	requireContemporaryPlaylistOrder(t, reopenedDB, persisted, 6, 13, 11)
	if err := reopened.DeleteLibraryPlaylist(ctx, persisted.Playlist.ID, 5); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale delete = %v", err)
	}
	if err := reopened.DeleteLibraryPlaylist(ctx, persisted.Playlist.ID, 6); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.LibraryPlaylist(ctx, persisted.Playlist.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted playlist remained: %v", err)
	}
	var entries int
	if err := reopenedDB.QueryRow(`SELECT COUNT(*) FROM music_library_playlist_entries WHERE playlist_id=?`, persisted.Playlist.ID).Scan(&entries); err != nil || entries != 0 {
		t.Fatalf("playlist delete did not cascade: %d %v", entries, err)
	}
}

func TestSaveTracksPreservesLibraryPlaylistMembershipAndFencesRevision(t *testing.T) {
	ctx := context.Background()
	service, db, _ := libraryPlaylistFixture(t)
	if _, err := db.Exec(`UPDATE music_tracks SET musicbrainz_id='' WHERE id=12;
		UPDATE music_tracks SET musicbrainz_id='',recording_id='' WHERE id=13;
		INSERT INTO music_tracks(id,album_id,musicbrainz_id,recording_id,disc,number,title,duration_ms,created_at,updated_at)
			VALUES(14,2,'track-14','recording-14',1,4,'Four',4000,1,1);
		INSERT INTO music_library_playlists(id,name,revision,created_at,updated_at) VALUES
			(30,'Rewrite',1,1,30),(31,'Second playlist',7,1,30);
		INSERT INTO music_library_playlist_entries(playlist_id,track_id,position) VALUES
			(30,13,1),(30,11,2),(30,14,3),(30,12,4),
			(31,11,1),(31,13,2)`); err != nil {
		t.Fatal(err)
	}

	store := NewStore(db)
	if err := store.SaveTracks(ctx, 2, []Track{
		// The same MusicBrainz track ID wins despite all other metadata changing.
		{MusicBrainzID: "track-11", RecordingID: "new-recording-11", Disc: 2, Number: 1, Title: "One rewritten", DurationMS: 1100},
		// A recording ID retains membership when no MusicBrainz track ID matches.
		{MusicBrainzID: "new-track-12", RecordingID: "recording-12", Disc: 1, Number: 2, Title: "Two rewritten", DurationMS: 2200},
		// Ordering metadata is evidence only when provider IDs are absent.
		{Disc: 1, Number: 3, Title: "Three", DurationMS: 3300},
		// This is not track 14: equal disc/number/title cannot override its
		// different provider identity, so it must not inherit membership.
		{MusicBrainzID: "replacement-14", RecordingID: "replacement-recording-14", Disc: 1, Number: 4, Title: "Four", DurationMS: 4400},
	}); err != nil {
		t.Fatal(err)
	}
	var one, two, three, replacement int64
	if err := db.QueryRow(`SELECT
		(SELECT id FROM music_tracks WHERE musicbrainz_id='track-11'),
		(SELECT id FROM music_tracks WHERE recording_id='recording-12'),
		(SELECT id FROM music_tracks WHERE title='Three'),
		(SELECT id FROM music_tracks WHERE musicbrainz_id='replacement-14')`).Scan(&one, &two, &three, &replacement); err != nil {
		t.Fatal(err)
	}
	if one == 11 || two == 12 || three == 13 || replacement == 14 {
		t.Fatalf("test requires newly inserted track rows: one=%d two=%d three=%d replacement=%d", one, two, three, replacement)
	}

	rewritten, err := service.LibraryPlaylist(ctx, 30)
	if err != nil {
		t.Fatal(err)
	}
	if rewritten.Playlist.Revision != 5 || rewritten.Playlist.TrackCount != 3 || rewritten.Playlist.DurationMS != 6600 || rewritten.Playlist.UpdatedAt <= 30 {
		t.Fatalf("rewrite summary = %#v", rewritten.Playlist)
	}
	want := []int64{three, one, two}
	got := playlistTrackIDs(rewritten)
	if len(got) != len(want) {
		t.Fatalf("rewrite order = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("rewrite order = %v, want %v", got, want)
		}
	}
	positions := map[int64]int{three: 1, one: 2, two: 4}
	rows, err := db.Query(`SELECT track_id,position FROM music_library_playlist_entries WHERE playlist_id=30 ORDER BY position`)
	if err != nil {
		t.Fatal(err)
	}
	stored := 0
	for rows.Next() {
		var trackID int64
		var position int
		if err := rows.Scan(&trackID, &position); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		if positions[trackID] != position {
			rows.Close()
			t.Fatalf("track %d position = %d, want %d", trackID, position, positions[trackID])
		}
		stored++
	}
	if err := rows.Close(); err != nil || stored != len(positions) {
		t.Fatalf("stored positions count=%d err=%v", stored, err)
	}

	second, err := service.LibraryPlaylist(ctx, 31)
	if err != nil {
		t.Fatal(err)
	}
	requireContemporaryPlaylistOrder(t, db, second, 9, one, three)
	if _, err := service.RemoveLibraryPlaylistTrack(ctx, 30, one, 1); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("catalog rewrite did not fence the old revision: %v", err)
	}

	// An insertion failure rolls back deletion, restoration and trigger bumps.
	if err := store.SaveTracks(ctx, 2, []Track{
		{Disc: 1, Number: 1, Title: "Valid"},
		{Disc: 1, Number: 1, Title: "Duplicate"},
	}); err == nil {
		t.Fatal("invalid catalog rewrite succeeded")
	}
	afterRollback, err := service.LibraryPlaylist(ctx, 30)
	if err != nil {
		t.Fatal(err)
	}
	if afterRollback.Playlist.Revision != 5 {
		t.Fatalf("rollback revision = %d, want 5", afterRollback.Playlist.Revision)
	}
	rolledBackIDs := playlistTrackIDs(afterRollback)
	for index := range want {
		if len(rolledBackIDs) != len(want) || rolledBackIDs[index] != want[index] {
			t.Fatalf("rollback membership = %v, want %v", rolledBackIDs, want)
		}
	}

	// Direct catalog deletion still cascades the entry, but the BEFORE DELETE
	// trigger fences clients in every playlist containing that canonical track.
	if _, err := db.Exec(`DELETE FROM music_tracks WHERE id=?`, three); err != nil {
		t.Fatal(err)
	}
	deleted, err := service.LibraryPlaylist(ctx, 30)
	if err != nil {
		t.Fatal(err)
	}
	if deleted.Playlist.Revision != 6 || deleted.Playlist.TrackCount != 2 {
		t.Fatalf("direct-delete summary = %#v", deleted.Playlist)
	}
	deletedIDs := playlistTrackIDs(deleted)
	if len(deletedIDs) != 2 || deletedIDs[0] != one || deletedIDs[1] != two {
		t.Fatalf("direct-delete order = %v, want [%d %d]", deletedIDs, one, two)
	}
	secondAfterDelete, err := service.LibraryPlaylist(ctx, 31)
	if err != nil {
		t.Fatal(err)
	}
	requireContemporaryPlaylistOrder(t, db, secondAfterDelete, 10, one)
}

func TestLibraryPlaylistsAreIndependentOfSubsonicPlaylists(t *testing.T) {
	ctx := context.Background()
	service, db, _ := libraryPlaylistFixture(t)
	if _, err := db.Exec(`INSERT INTO music_server_users(id,username,password_sealed,created_at,updated_at) VALUES(1,'alice',X'00',1,1);
		INSERT INTO music_playlists(id,user_id,name,created_at,updated_at) VALUES(50,1,'Shared name',1,1);
		INSERT INTO music_playlist_entries(id,playlist_id,position,music_file_id) VALUES(60,50,1,101)`); err != nil {
		t.Fatal(err)
	}
	trackID := int64(11)
	first, err := service.CreateLibraryPlaylist(ctx, "Shared name", &trackID)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.DeleteLibraryPlaylist(ctx, first.Playlist.ID, 1); err != nil {
		t.Fatal(err)
	}
	var subsonicPlaylists, subsonicEntries int
	if err := db.QueryRow(`SELECT (SELECT COUNT(*) FROM music_playlists WHERE id=50),(SELECT COUNT(*) FROM music_playlist_entries WHERE id=60)`).Scan(&subsonicPlaylists, &subsonicEntries); err != nil || subsonicPlaylists != 1 || subsonicEntries != 1 {
		t.Fatalf("library delete touched Subsonic state: %d %d %v", subsonicPlaylists, subsonicEntries, err)
	}
	second, err := service.CreateLibraryPlaylist(ctx, "Still independent", &trackID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM music_playlists WHERE id=50`); err != nil {
		t.Fatal(err)
	}
	detail, err := service.LibraryPlaylist(ctx, second.Playlist.ID)
	if err != nil || len(detail.Tracks) != 1 || detail.Tracks[0].ID != 11 {
		t.Fatalf("Subsonic delete touched library playlist: %#v %v", detail, err)
	}
}
