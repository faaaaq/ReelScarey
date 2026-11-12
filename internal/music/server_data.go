package music

import (
	"context"
	"strings"
)

// ServerSong is one playable song row for the opt-in Subsonic-compatible API.
// FileID is the primary registered file and is also the Subsonic song ID.
type ServerSong struct {
	FileID      int64  `json:"file_id"`
	Title       string `json:"title"`
	Artist      string `json:"artist"`
	Album       string `json:"album"`
	ArtistID    int64  `json:"artist_id"`
	AlbumID     int64  `json:"album_id"`
	Track       int    `json:"track"`
	Disc        int    `json:"disc"`
	Year        *int   `json:"year"`
	DurationMS  int    `json:"duration_ms"`
	Suffix      string `json:"suffix"`
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
	CoverURL    string `json:"cover_url"`
}

const serverSongColumns = `f.id,t.title,ar.name,a.title,ar.id,a.id,t.number,t.disc,a.year,t.duration_ms,f.format,f.size_bytes,a.cover_url,f.relative_path`

func scanServerSong(row interface{ Scan(...any) error }) (ServerSong, error) {
	var song ServerSong
	var format, relativePath string
	if err := row.Scan(&song.FileID, &song.Title, &song.Artist, &song.Album, &song.ArtistID, &song.AlbumID,
		&song.Track, &song.Disc, &song.Year, &song.DurationMS, &format, &song.SizeBytes, &song.CoverURL, &relativePath); err != nil {
		return ServerSong{}, err
	}
	// The Subsonic suffix and content type must describe the real file, not the
	// ffprobe container name stored in music_files.format. Clients such as
	// Amperfy refuse to play a song whose contentType is application/octet-stream
	// or audio/x-ms-wma, so anything the server converts on demand is advertised
	// as the stream it will actually deliver.
	song.Suffix = serverSuffix(format, relativePath)
	song.ContentType = serverContentType(format, relativePath)
	return song, nil
}

// serverPlayableFileOrder ranks a track's registered files so the Subsonic
// layer exposes the most widely playable one. Without this a track that has a
// .webm or .wma copy alongside a .flac/.mp3 copy would surface the copy that
// iOS clients cannot decode.
const serverPlayableFileOrder = `CASE
		WHEN lower(f2.relative_path) LIKE '%.flac' THEN 0
		WHEN lower(f2.relative_path) LIKE '%.mp3' THEN 1
		WHEN lower(f2.relative_path) LIKE '%.m4a' THEN 2
		WHEN lower(f2.relative_path) LIKE '%.mp4' THEN 3
		WHEN lower(f2.relative_path) LIKE '%.aac' THEN 4
		WHEN lower(f2.relative_path) LIKE '%.opus' THEN 5
		WHEN lower(f2.relative_path) LIKE '%.ogg' THEN 5
		WHEN lower(f2.relative_path) LIKE '%.oga' THEN 5
		WHEN lower(f2.relative_path) LIKE '%.wav' THEN 6
		WHEN lower(f2.relative_path) LIKE '%.webm' THEN 8
		WHEN lower(f2.relative_path) LIKE '%.wma' THEN 9
		ELSE 7
	END, f2.id`

const serverSongFrom = ` FROM music_tracks t
	JOIN music_albums a ON a.id=t.album_id
	JOIN music_artists ar ON ar.id=a.artist_id
	JOIN music_files f ON f.id=(SELECT f2.id FROM music_files f2 WHERE f2.track_id=t.id ORDER BY ` + serverPlayableFileOrder + ` LIMIT 1)`

func (s *Store) ServerSong(ctx context.Context, fileID int64) (ServerSong, error) {
	return scanServerSong(s.db.QueryRowContext(ctx, `SELECT `+serverSongColumns+serverSongFrom+` WHERE f.id=?`, fileID))
}

func (s *Store) ServerSongs(ctx context.Context, query string, limit int) ([]ServerSong, error) {
	query = strings.TrimSpace(query)
	rows, err := s.db.QueryContext(ctx, `SELECT `+serverSongColumns+serverSongFrom+`
		WHERE t.title LIKE ? ESCAPE '\' OR a.title LIKE ? ESCAPE '\' OR ar.name LIKE ? ESCAPE '\'
		ORDER BY ar.name COLLATE NOCASE, a.year IS NULL, a.year, a.title COLLATE NOCASE, t.disc, t.number
		LIMIT ?`, likePattern(query), likePattern(query), likePattern(query), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ServerSong{}
	for rows.Next() {
		song, err := scanServerSong(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, song)
	}
	return out, rows.Err()
}

func (s *Store) ServerRandomSongs(ctx context.Context, limit int) ([]ServerSong, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+serverSongColumns+serverSongFrom+` ORDER BY RANDOM() LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ServerSong{}
	for rows.Next() {
		song, err := scanServerSong(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, song)
	}
	return out, rows.Err()
}

func (s *Store) ServerAlbumSongs(ctx context.Context, albumID int64) ([]ServerSong, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+serverSongColumns+serverSongFrom+`
		WHERE a.id=? ORDER BY t.disc, t.number, t.id`, albumID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ServerSong{}
	for rows.Next() {
		song, err := scanServerSong(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, song)
	}
	return out, rows.Err()
}

// ServerArtistSongs returns songs belonging to exactly one artist. The
// Subsonic topSongsByArtistId extension passes an artist id rather than an
// artist name; scoping by ar.id avoids the LIKE false positives (and the
// artist-name collisions) of ServerSongs.
func (s *Store) ServerArtistSongs(ctx context.Context, artistID int64, limit int) ([]ServerSong, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+serverSongColumns+serverSongFrom+`
		WHERE ar.id=? ORDER BY a.year IS NULL, a.year, a.title COLLATE NOCASE, t.disc, t.number, t.id
		LIMIT ?`, artistID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ServerSong{}
	for rows.Next() {
		song, err := scanServerSong(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, song)
	}
	return out, rows.Err()
}

// ServerSongCount returns the number of playable songs (tracks with at least
// one registered file). It backs the Subsonic getScanStatus action.
func (s *Store) ServerSongCount(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM music_tracks t
		WHERE EXISTS (SELECT 1 FROM music_files f WHERE f.track_id=t.id)`).Scan(&count)
	return count, err
}

// serverAlbumAggregates selects the track count and total play time that the
// Subsonic album elements report. Clients show "0:00" for a duration-less album
// and some treat that as unplayable, so every server album read fills both.
const serverAlbumAggregates = `,(SELECT COUNT(*) FROM music_tracks t WHERE t.album_id=a.id AND EXISTS (SELECT 1 FROM music_files f WHERE f.track_id=t.id)),(SELECT COALESCE(SUM(t.duration_ms),0) FROM music_tracks t WHERE t.album_id=a.id AND EXISTS (SELECT 1 FROM music_files f WHERE f.track_id=t.id))`

func (s *Store) ServerAlbums(ctx context.Context, order string, size, offset int) ([]Album, error) {
	orderBy := `a.title COLLATE NOCASE`
	switch order {
	case "newest":
		orderBy = `a.created_at DESC, a.id DESC`
	case "recent":
		orderBy = `a.updated_at DESC, a.id DESC`
	case "frequent":
		orderBy = `a.updated_at DESC, a.title COLLATE NOCASE`
	case "starred":
		orderBy = `a.updated_at DESC, a.title COLLATE NOCASE`
	case "byYear":
		orderBy = `a.year IS NULL, a.year DESC, a.title COLLATE NOCASE`
	case "alphabeticalByArtist":
		orderBy = `ar.name COLLATE NOCASE, a.year IS NULL, a.year, a.title COLLATE NOCASE`
	case "random":
		orderBy = `RANDOM()`
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+albumColumns+serverAlbumAggregates+`
		FROM music_albums a JOIN music_artists ar ON ar.id=a.artist_id ORDER BY `+orderBy+` LIMIT ? OFFSET ?`, size, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Album{}
	for rows.Next() {
		var durationMS int
		album, err := scanAlbum(rows, &durationMS)
		if err != nil {
			return nil, err
		}
		album.DurationMS = durationMS
		out = append(out, album)
	}
	return out, rows.Err()
}

func (s *Store) ServerSearchAlbums(ctx context.Context, query string, limit int) ([]Album, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+albumColumns+serverAlbumAggregates+`
		FROM music_albums a JOIN music_artists ar ON ar.id=a.artist_id
		WHERE a.title LIKE ? ESCAPE '\' OR ar.name LIKE ? ESCAPE '\'
		ORDER BY a.title COLLATE NOCASE LIMIT ?`, likePattern(strings.TrimSpace(query)), likePattern(strings.TrimSpace(query)), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Album{}
	for rows.Next() {
		var durationMS int
		album, err := scanAlbum(rows, &durationMS)
		if err != nil {
			return nil, err
		}
		album.DurationMS = durationMS
		out = append(out, album)
	}
	return out, rows.Err()
}

func (s *Service) ServerSong(ctx context.Context, fileID int64) (ServerSong, error) {
	return s.store.ServerSong(ctx, fileID)
}

func (s *Service) ServerAlbum(ctx context.Context, id int64) (AlbumDetails, error) {
	album, err := s.store.Album(ctx, id)
	if err != nil {
		return AlbumDetails{}, err
	}
	tracks, err := s.store.Tracks(ctx, id)
	if err != nil {
		return AlbumDetails{}, err
	}
	return AlbumDetails{Album: album, Tracks: tracks}, nil
}

func (s *Service) ServerSongs(ctx context.Context, query string, limit int) ([]ServerSong, error) {
	return s.store.ServerSongs(ctx, query, limit)
}

func (s *Service) ServerRandomSongs(ctx context.Context, limit int) ([]ServerSong, error) {
	return s.store.ServerRandomSongs(ctx, limit)
}

func (s *Service) ServerAlbumSongs(ctx context.Context, albumID int64) ([]ServerSong, error) {
	return s.store.ServerAlbumSongs(ctx, albumID)
}

func (s *Service) ServerArtistSongs(ctx context.Context, artistID int64, limit int) ([]ServerSong, error) {
	return s.store.ServerArtistSongs(ctx, artistID, limit)
}

func (s *Service) ServerSongCount(ctx context.Context) (int, error) {
	return s.store.ServerSongCount(ctx)
}

func (s *Service) ServerAlbums(ctx context.Context, order string, size, offset int) ([]Album, error) {
	return s.store.ServerAlbums(ctx, order, size, offset)
}

func (s *Service) ServerSearchAlbums(ctx context.Context, query string, limit int) ([]Album, error) {
	return s.store.ServerSearchAlbums(ctx, query, limit)
}
