package music

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/securestore"
)

// The music server token is stored in the encrypted integration store. Its name
// must be permitted by the integration_configs CHECK allowlist; when it is not,
// generating a token fails the constraint and surfaces as a generic internal
// error. This exercises the real database, migrations and secure store.
func TestMusicServerTokenLifecycle(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := database.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	service := NewService(db, "")
	secrets, err := securestore.New(ctx, db, dir)
	if err != nil {
		t.Fatal(err)
	}
	service.SetSecrets(secrets)

	status, err := service.MusicServer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if status.Configured || status.Enabled {
		t.Fatalf("music server must start unconfigured and disabled: %+v", status)
	}

	token, status, err := service.GenerateMusicServerToken(ctx)
	if err != nil {
		t.Fatalf("generating a music server token failed: %v", err)
	}
	if len(token) != musicServerTokenLength {
		t.Fatalf("token should be %d characters, got %d: %q", musicServerTokenLength, len(token), token)
	}
	for _, r := range token {
		if !strings.ContainsRune(musicServerTokenAlphabet, r) {
			t.Fatalf("token %q uses a character outside the Crockford Base32 alphabet %q", token, musicServerTokenAlphabet)
		}
	}
	if !status.Configured || !strings.HasSuffix(status.TokenHint, token[len(token)-4:]) {
		t.Fatalf("status did not reflect the new token: %+v", status)
	}
	if status.Enabled {
		t.Fatal("generating a token must not enable the endpoint")
	}

	var stored string
	if err = db.QueryRowContext(ctx, `SELECT name FROM integration_configs WHERE name=?`, musicServerTokenName).Scan(&stored); err != nil {
		t.Fatalf("token row missing from the encrypted store: %v", err)
	}

	if status, err = service.SaveMusicServer(ctx, true, status.Revision); err != nil {
		t.Fatal(err)
	}
	if !status.Enabled {
		t.Fatal("enabling the music server did not persist")
	}

	ok, err := service.VerifyMusicServerToken(ctx, "admin", token, "", "")
	if err != nil || !ok {
		t.Fatalf("plaintext token rejected: ok=%v err=%v", ok, err)
	}
	if ok, err = service.VerifyMusicServerToken(ctx, "admin", token+"x", "", ""); err != nil || ok {
		t.Fatalf("wrong plaintext token accepted: ok=%v err=%v", ok, err)
	}
	// A hand-typed token must still match: lowercase and grouped with dashes.
	if ok, err = service.VerifyMusicServerToken(ctx, "admin", strings.ToLower(token), "", ""); err != nil || !ok {
		t.Fatalf("lowercase token rejected: ok=%v err=%v", ok, err)
	}
	grouped := token[:4] + "-" + token[4:8] + "-" + token[8:12] + "-" + token[12:16] + "-" + token[16:]
	if ok, err = service.VerifyMusicServerToken(ctx, "admin", grouped, "", ""); err != nil || !ok {
		t.Fatalf("grouped token rejected: ok=%v err=%v", ok, err)
	}

	salt := "abc123"
	sum := md5.Sum([]byte(token + salt))
	if ok, err = service.VerifyMusicServerToken(ctx, "admin", "", hex.EncodeToString(sum[:]), salt); err != nil || !ok {
		t.Fatalf("subsonic md5 token rejected: ok=%v err=%v", ok, err)
	}

	// The token must survive a reopen using the same encrypted key material.
	reopened, err := database.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	restored := NewService(reopened, "")
	restoredSecrets, err := securestore.New(ctx, reopened, dir)
	if err != nil {
		t.Fatal(err)
	}
	restored.SetSecrets(restoredSecrets)
	restoredStatus, err := restored.MusicServer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !restoredStatus.Configured || restoredStatus.TokenHint != status.TokenHint {
		t.Fatalf("token did not persist across reopen: %+v", restoredStatus)
	}
	if ok, err = restored.VerifyMusicServerToken(ctx, "admin", token, "", ""); err != nil || !ok {
		t.Fatalf("persisted token rejected after reopen: ok=%v err=%v", ok, err)
	}

	// A pre-existing (legacy, case-sensitive) token must still verify exactly.
	if err = restoredSecrets.SetNamed(ctx, musicServerTokenName, []byte("AbC-def_123")); err != nil {
		t.Fatal(err)
	}
	if ok, err = restored.VerifyMusicServerToken(ctx, "admin", "AbC-def_123", "", ""); err != nil || !ok {
		t.Fatalf("legacy stored token rejected: ok=%v err=%v", ok, err)
	}
	if ok, err = restored.VerifyMusicServerToken(ctx, "admin", "abcDEF123", "", ""); err != nil || !ok {
		t.Fatalf("normalised legacy token rejected: ok=%v err=%v", ok, err)
	}

	removed, err := service.DeleteMusicServerToken(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if removed.Configured {
		t.Fatalf("token still configured after removal: %+v", removed)
	}
}

// ServerAlbumSongs backs the Subsonic album/directory views. Unlike the generic
// Tracks() listing it must join the registered file so each entry carries a
// usable suffix, content type and byte size; tracks without a file are omitted.
// ServerSongCount powers getScanStatus and must count only playable tracks.
func TestServerAlbumSongsAndSongCount(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := database.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	const now = 1700000000
	if _, err := db.ExecContext(ctx, `INSERT INTO music_artists
		(id, musicbrainz_id, name, created_at, updated_at) VALUES (1, 'mb-artist-1', 'ABBA', ?, ?)`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO music_albums
		(id, artist_id, musicbrainz_id, title, year, cover_url, created_at, updated_at)
		VALUES (1, 1, 'mb-album-1', 'Arrival', 1976, 'https://example.invalid/arrival.jpg', ?, ?)`, now, now); err != nil {
		t.Fatal(err)
	}
	// Two tracks with files (out of disc/number order so ordering is exercised)
	// and one bonus track with no registered file.
	tracks := []struct {
		id       int64
		disc     int
		number   int
		title    string
		duration int
	}{
		{id: 1, disc: 1, number: 2, title: "Dancing Queen", duration: 231000},
		{id: 2, disc: 1, number: 1, title: "When I Kissed the Teacher", duration: 183000},
		{id: 3, disc: 2, number: 1, title: "Fernando (bonus)", duration: 100000},
	}
	for _, tr := range tracks {
		if _, err := db.ExecContext(ctx, `INSERT INTO music_tracks
			(id, album_id, disc, number, title, duration_ms, created_at, updated_at)
			VALUES (?, 1, ?, ?, ?, ?, ?, ?)`, tr.id, tr.disc, tr.number, tr.title, tr.duration, now, now); err != nil {
			t.Fatal(err)
		}
	}
	files := []struct {
		id       int64
		trackID  int64
		path     string
		format   string
		sizeByte int64
	}{
		{id: 1, trackID: 2, path: "ABBA/Arrival/01 - When I Kissed the Teacher.mp3", format: "mp3", sizeByte: 4065305},
		{id: 2, trackID: 1, path: "ABBA/Arrival/02 - Dancing Queen.flac", format: "flac", sizeByte: 12345678},
	}
	for _, f := range files {
		if _, err := db.ExecContext(ctx, `INSERT INTO music_files
			(id, artist_id, album_id, track_id, relative_path, size_bytes, format, imported_at)
			VALUES (?, 1, 1, ?, ?, ?, ?, ?)`, f.id, f.trackID, f.path, f.sizeByte, f.format, now); err != nil {
			t.Fatal(err)
		}
	}

	service := NewService(db, "")

	songs, err := service.ServerAlbumSongs(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(songs) != 2 {
		t.Fatalf("expected 2 playable songs, got %d: %+v", len(songs), songs)
	}
	if songs[0].Title != "When I Kissed the Teacher" || songs[1].Title != "Dancing Queen" {
		t.Fatalf("songs not ordered by disc/number: %q then %q", songs[0].Title, songs[1].Title)
	}
	first := songs[0]
	if first.FileID != 1 || first.AlbumID != 1 || first.ArtistID != 1 {
		t.Fatalf("song IDs not populated: %+v", first)
	}
	if first.Suffix != "mp3" || first.ContentType != "audio/mpeg" || first.SizeBytes != 4065305 {
		t.Fatalf("song metadata not populated from the file: %+v", first)
	}
	if first.Track != 1 || first.Disc != 1 || first.DurationMS != 183000 {
		t.Fatalf("song track/disc/duration wrong: %+v", first)
	}
	if first.Album != "Arrival" || first.Artist != "ABBA" {
		t.Fatalf("song album/artist names wrong: %+v", first)
	}
	if first.Year == nil || *first.Year != 1976 {
		t.Fatalf("song year not populated: %+v", first)
	}
	if songs[1].Suffix != "flac" || songs[1].ContentType != "audio/flac" || songs[1].SizeBytes != 12345678 {
		t.Fatalf("second song metadata not populated from the file: %+v", songs[1])
	}

	// The bonus track has no file and must not appear, nor be counted.
	if count, err := service.ServerSongCount(ctx); err != nil || count != 2 {
		t.Fatalf("ServerSongCount = %d (err=%v), want 2", count, err)
	}

	// An album with no registered files yields an empty, non-nil slice.
	empty, err := service.ServerAlbumSongs(ctx, 999)
	if err != nil {
		t.Fatal(err)
	}
	if empty == nil || len(empty) != 0 {
		t.Fatalf("expected empty slice for unknown album, got %+v", empty)
	}
}

// A track can have several registered files (for example a .webm copy beside a
// .flac copy). The Subsonic layer must expose the most widely playable one and
// must report the real file extension as the suffix, never the ffprobe
// container name stored in music_files.format. iOS clients such as Amperfy
// refuse to play a song whose contentType is application/octet-stream.
func TestServerSongPrefersPlayableFileAndRealSuffix(t *testing.T) {
	withoutFFmpeg(t)
	ctx := context.Background()
	dir := t.TempDir()
	db, err := database.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	const now = 1700000000
	if _, err := db.ExecContext(ctx, `INSERT INTO music_artists
		(id, musicbrainz_id, name, created_at, updated_at) VALUES (1, 'mb-artist-1', 'XG', ?, ?)`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO music_albums
		(id, artist_id, musicbrainz_id, title, created_at, updated_at)
		VALUES (1, 1, 'mb-album-1', 'DEPENDS', ?, ?)`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO music_tracks
		(id, album_id, disc, number, title, duration_ms, created_at, updated_at)
		VALUES (1, 1, 1, 1, 'DEPENDS', 185000, ?, ?)`, now, now); err != nil {
		t.Fatal(err)
	}
	// The .webm file has the lowest id, so a naive MIN(id) selection would pick
	// it and report an unplayable container.
	files := []struct {
		id     int64
		path   string
		format string
	}{
		{id: 1, path: "Music/XG/DEPENDS/01 DEPENDS.webm", format: "matroska,webm"},
		{id: 2, path: "Music/XG/DEPENDS/01 DEPENDS.flac", format: "flac"},
		{id: 3, path: "Music/XG/DEPENDS/01 DEPENDS.opus", format: "ogg"},
	}
	for _, f := range files {
		if _, err := db.ExecContext(ctx, `INSERT INTO music_files
			(id, artist_id, album_id, track_id, relative_path, size_bytes, format, imported_at)
			VALUES (?, 1, 1, 1, ?, 1000, ?, ?)`, f.id, f.path, f.format, now); err != nil {
			t.Fatal(err)
		}
	}

	service := NewService(db, "")
	songs, err := service.ServerAlbumSongs(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(songs) != 1 {
		t.Fatalf("expected 1 song, got %d", len(songs))
	}
	song := songs[0]
	if song.FileID != 2 {
		t.Fatalf("expected the .flac file (id 2) to be chosen, got file %d", song.FileID)
	}
	if song.Suffix != "flac" || song.ContentType != "audio/flac" {
		t.Fatalf("suffix/content type must describe the real file: suffix=%q contentType=%q", song.Suffix, song.ContentType)
	}

	// A .webm-only track must still report a real suffix and a playable MIME
	// type rather than the raw container name and application/octet-stream.
	if _, err := db.ExecContext(ctx, `INSERT INTO music_tracks
		(id, album_id, disc, number, title, duration_ms, created_at, updated_at)
		VALUES (2, 1, 1, 2, 'ONLY WEBM', 100000, ?, ?)`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO music_files
		(id, artist_id, album_id, track_id, relative_path, size_bytes, format, imported_at)
		VALUES (4, 1, 1, 2, 'Music/XG/DEPENDS/02 ONLY WEBM.webm', 1000, 'matroska,webm', ?)`, now); err != nil {
		t.Fatal(err)
	}
	songs, err = service.ServerAlbumSongs(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	var webm *ServerSong
	for i := range songs {
		if songs[i].FileID == 4 {
			webm = &songs[i]
		}
	}
	if webm == nil {
		t.Fatal("webm-only track missing from album songs")
	}
	if webm.Suffix != "webm" || webm.ContentType != "audio/webm" {
		t.Fatalf("webm track suffix/content type wrong: suffix=%q contentType=%q", webm.Suffix, webm.ContentType)
	}

	// The album list aggregate must count only tracks that have a file, so it
	// agrees with the album detail view.
	albums, err := service.ServerAlbums(ctx, "alphabeticalByName", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(albums) != 1 || albums[0].TrackCount != 2 {
		t.Fatalf("album list track count must match playable tracks: %+v", albums)
	}
}
