package music

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/faaaaq/ReelScarey/internal/database"
)

func playbackStateFixture(t *testing.T) (*Service, *sql.DB, string) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	db, err := database.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`INSERT INTO music_artists(id,musicbrainz_id,name,monitored,status,created_at,updated_at) VALUES(1,'artist-local','Artist',1,'available',1,1);
		INSERT INTO music_albums(id,artist_id,musicbrainz_id,title,monitored,status,created_at,updated_at) VALUES(2,1,'album-local','Album',1,'available',1,1);
		INSERT INTO music_tracks(id,album_id,musicbrainz_id,recording_id,disc,number,title,duration_ms,created_at,updated_at) VALUES(3,2,'track-local','recording-local',1,1,'Song',1000,1,1);
		INSERT INTO music_server_users(id,username,password_sealed,created_at,updated_at) VALUES(1,'alice',X'00',1,1),(2,'bob',X'00',1,1)`); err != nil {
		t.Fatal(err)
	}
	insertMusicFile(t, db, 1, 3, "Music/Artist/Album/01 Song.flac", matchingTags())
	return NewService(db, t.TempDir()), db, dir
}

func TestPlaybackStatePersistsAndIsolatesNamedUsers(t *testing.T) {
	ctx := context.Background()
	service, db, dir := playbackStateFixture(t)
	state := service.PlaybackState()
	for _, mark := range []PlaybackMarkTarget{{Kind: "song", ObjectID: 1}, {Kind: "album", ObjectID: 2}, {Kind: "artist", ObjectID: 1}} {
		if err := state.SetStarred(ctx, 1, mark.Kind, mark.ObjectID, true); err != nil {
			t.Fatal(err)
		}
		if err := state.SetRating(ctx, 1, mark.Kind, mark.ObjectID, 5); err != nil {
			t.Fatal(err)
		}
	}
	playlist, err := state.CreatePlaylist(ctx, 1, "Repeated", "Owned order", false, []int64{1, 1})
	if err != nil || len(playlist.EntryIDs) != 2 || playlist.Owner != "alice" || playlist.DurationMS != 2000 {
		t.Fatalf("playlist %#v %v", playlist, err)
	}
	if err = state.SaveQueueByIndex(ctx, 1, 1, 12345, []int64{1, 1}, "fixture-client"); err != nil {
		t.Fatal(err)
	}
	if marks, err := state.Marks(ctx, 2); err != nil || len(marks) != 0 {
		t.Fatalf("bob inherited alice's marks: %#v %v", marks, err)
	}
	if playlists, err := state.Playlists(ctx, 2); err != nil || len(playlists) != 0 {
		t.Fatalf("bob inherited private playlist: %#v %v", playlists, err)
	}
	if _, err = state.Playlist(ctx, 2, playlist.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("bob read alice's private playlist: %v", err)
	}
	if queue, err := state.Queue(ctx, 2); err != nil || len(queue.EntryIDs) != 0 || queue.CurrentIndex != -1 {
		t.Fatalf("bob inherited alice's queue: %#v %v", queue, err)
	}
	if err = state.DeletePlaylist(ctx, 2, playlist.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("bob could delete alice's playlist: %v", err)
	}

	name, public, comment := "Renamed", true, "Public comment"
	removeIndexes := []int{0}
	playlist, err = state.UpdatePlaylist(ctx, 1, playlist.ID, PlaylistChanges{Name: &name, Comment: &comment, Public: &public, RemoveIndexes: removeIndexes, Add: []int64{1, 1}})
	if err != nil || playlist.Name != name || !playlist.Public || playlist.Comment != comment || len(playlist.EntryIDs) != 3 {
		t.Fatalf("playlist update %#v %v", playlist, err)
	}
	if _, err = state.Playlist(ctx, 2, playlist.ID); err != nil {
		t.Fatalf("public playlist was not readable cross-user: %v", err)
	}
	if _, err = state.UpdatePlaylist(ctx, 2, playlist.ID, PlaylistChanges{Name: &name}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("public playlist allowed a non-owner write: %v", err)
	}

	// Close the actual SQLite pool and reopen the same on-disk database. Merely
	// constructing another service over the same pool does not test persistence.
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = database.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	reopened := NewService(db, t.TempDir()).PlaybackState()
	marks, err := reopened.Marks(ctx, 1)
	if err != nil || len(marks) != 3 {
		t.Fatalf("marks did not survive reopen: %#v %v", marks, err)
	}
	for _, mark := range marks {
		if mark.Rating != 5 || mark.StarredAt == 0 {
			t.Fatalf("mark did not survive reopen: %#v", mark)
		}
	}
	queue, err := reopened.Queue(ctx, 1)
	if err != nil || queue.CurrentIndex != 1 || queue.CurrentFileID != 1 || queue.PositionMS != 12345 || len(queue.EntryIDs) != 2 || queue.ChangedBy != "fixture-client" {
		t.Fatalf("index queue did not survive reopen: %#v %v", queue, err)
	}
	playlist, err = reopened.Playlist(ctx, 2, playlist.ID)
	if err != nil || playlist.Owner != "alice" || !playlist.Public || playlist.Comment != comment || playlist.DurationMS != 3000 {
		t.Fatalf("public playlist did not survive reopen: %#v %v", playlist, err)
	}
	if err = reopened.SetStarred(ctx, 0, "song", 1, true); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("legacy/shared identity was allowed to write user state: %v", err)
	}
}

func TestPlaybackStateCatalogDeletionRemovesMarks(t *testing.T) {
	ctx := context.Background()
	service, db, _ := playbackStateFixture(t)
	state := service.PlaybackState()
	for _, mark := range []PlaybackMarkTarget{{Kind: "song", ObjectID: 1}, {Kind: "album", ObjectID: 2}, {Kind: "artist", ObjectID: 1}} {
		if err := state.SetStarred(ctx, 1, mark.Kind, mark.ObjectID, true); err != nil {
			t.Fatal(err)
		}
	}
	countKind := func(kind string) int {
		t.Helper()
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM music_playback_marks WHERE kind=?`, kind).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}
	if _, err := db.Exec(`DELETE FROM music_files WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if countKind("song") != 0 || countKind("album") != 1 || countKind("artist") != 1 {
		t.Fatal("song deletion did not clean only song marks")
	}
	if _, err := db.Exec(`DELETE FROM music_tracks WHERE id=3; DELETE FROM music_albums WHERE id=2`); err != nil {
		t.Fatal(err)
	}
	if countKind("album") != 0 || countKind("artist") != 1 {
		t.Fatal("album deletion did not clean only album marks")
	}
	if _, err := db.Exec(`DELETE FROM music_artists WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if countKind("artist") != 0 {
		t.Fatal("artist deletion left an orphaned mark")
	}
}

func TestPlaybackStateScrobbleBatchOrderingExpiryAndAuthorization(t *testing.T) {
	ctx := context.Background()
	service, db, _ := playbackStateFixture(t)
	state := service.PlaybackState()
	now := time.Now().Unix()
	if err := state.ScrobbleBatch(ctx, 1, []PlaybackScrobble{{FileID: 1, PlayedAt: now - 30}, {FileID: 1, PlayedAt: now - 10}}, false, "alice-client"); err != nil {
		t.Fatal(err)
	}
	if err := state.Scrobble(ctx, 2, 1, now-20, false, "bob-client"); err != nil {
		t.Fatal(err)
	}
	items, err := state.AllNowPlaying(ctx, 100)
	if err != nil || len(items) != 2 {
		t.Fatalf("all-user now playing %#v %v", items, err)
	}
	seen := map[string]bool{}
	for _, item := range items {
		seen[item.Username] = true
		if item.Username == "alice" && item.StartedAt != now-10 {
			t.Fatalf("batch did not retain its newest timestamp: %#v", item)
		}
	}
	if !seen["alice"] || !seen["bob"] {
		t.Fatalf("missing usernames: %#v", items)
	}
	// Older notifications and submissions cannot move or clear newer state.
	if err = state.Scrobble(ctx, 1, 1, now-40, false, "stale-client"); err != nil {
		t.Fatal(err)
	}
	if err = state.Scrobble(ctx, 1, 1, now-40, true, "late-submission"); err != nil {
		t.Fatal(err)
	}
	alice, err := state.NowPlaying(ctx, 1)
	if err != nil || len(alice) != 1 || alice[0].StartedAt != now-10 || alice[0].Client != "alice-client" {
		t.Fatalf("out-of-order scrobble replaced now playing: %#v %v", alice, err)
	}
	// Every item and object is validated before a repeated submission commits.
	err = state.ScrobbleBatch(ctx, 1, []PlaybackScrobble{{FileID: 1, PlayedAt: now}, {FileID: 999, PlayedAt: now}}, true, "batch")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("invalid batch: %v", err)
	}
	if count, err := state.PlayCount(ctx, 1, 1); err != nil || count != 1 {
		t.Fatalf("invalid batch partially committed; count=%d err=%v", count, err)
	}
	if err = state.Scrobble(ctx, 1, 1, now, true, "finished"); err != nil {
		t.Fatal(err)
	}
	if alice, err = state.NowPlaying(ctx, 1); err != nil || len(alice) != 0 {
		t.Fatalf("matching submission did not clear now playing: %#v %v", alice, err)
	}
	if items, err = state.AllNowPlaying(ctx, 100); err != nil || len(items) != 1 || items[0].Username != "bob" {
		t.Fatalf("alice cleared bob's now playing: %#v %v", items, err)
	}
	if _, err = db.Exec(`UPDATE music_now_playing SET started_at=? WHERE user_id=2`, time.Now().Add(-11*time.Minute).Unix()); err != nil {
		t.Fatal(err)
	}
	if items, err = state.AllNowPlaying(ctx, 100); err != nil || len(items) != 0 {
		t.Fatalf("expired now playing remained visible: %#v %v", items, err)
	}
}
