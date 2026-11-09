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

const serverSongColumns = `f.id,t.title,ar.name,a.title,ar.id,a.id,t.number,t.disc,a.year,t.duration_ms,f.format,f.size_bytes,a.cover_url`

func scanServerSong(row interface{ Scan(...any) error }) (ServerSong, error) {
	var song ServerSong
	if err := row.Scan(&song.FileID, &song.Title, &song.Artist, &song.Album, &song.ArtistID, &song.AlbumID,
		&song.Track, &song.Disc, &song.Year, &song.DurationMS, &song.Suffix, &song.SizeBytes, &song.CoverURL); err != nil {
		return ServerSong{}, err
	}
	song.ContentType = audioContentType(song.Suffix, "x."+song.Suffix)
	return song, nil
}

const serverSongFrom = ` FROM music_tracks t
	JOIN music_albums a ON a.id=t.album_id
	JOIN music_artists ar ON ar.id=a.artist_id
	JOIN music_files f ON f.id=(SELECT MIN(f2.id) FROM music_files f2 WHERE f2.track_id=t.id)`

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

func (s *Store) ServerAlbums(ctx context.Context, order string, size, offset int) ([]Album, error) {
	orderBy := `a.title COLLATE NOCASE`
	switch order {
	case "newest":
		orderBy = `a.year IS NULL, a.year DESC, a.title COLLATE NOCASE`
	case "random":
		orderBy = `RANDOM()`
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+albumColumns+`,(SELECT COUNT(*) FROM music_tracks t WHERE t.album_id=a.id)
		FROM music_albums a JOIN music_artists ar ON ar.id=a.artist_id ORDER BY `+orderBy+` LIMIT ? OFFSET ?`, size, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Album{}
	for rows.Next() {
		album, err := scanAlbum(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, album)
	}
	return out, rows.Err()
}

func (s *Store) ServerSearchAlbums(ctx context.Context, query string, limit int) ([]Album, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+albumColumns+`,(SELECT COUNT(*) FROM music_tracks t WHERE t.album_id=a.id)
		FROM music_albums a JOIN music_artists ar ON ar.id=a.artist_id
		WHERE a.title LIKE ? ESCAPE '\' OR ar.name LIKE ? ESCAPE '\'
		ORDER BY a.title COLLATE NOCASE LIMIT ?`, likePattern(strings.TrimSpace(query)), likePattern(strings.TrimSpace(query)), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Album{}
	for rows.Next() {
		album, err := scanAlbum(rows)
		if err != nil {
			return nil, err
		}
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

func (s *Service) ServerAlbums(ctx context.Context, order string, size, offset int) ([]Album, error) {
	return s.store.ServerAlbums(ctx, order, size, offset)
}

func (s *Service) ServerSearchAlbums(ctx context.Context, query string, limit int) ([]Album, error) {
	return s.store.ServerSearchAlbums(ctx, query, limit)
}
