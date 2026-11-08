package music

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

const pageSize = 48

var ErrInvalidInput = errors.New("Choose a valid music page or query.")

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

const artistColumns = `id, musicbrainz_id, name, sort_name, disambiguation, country, artist_type, overview, image_url, monitored, status`

func scanArtist(row interface{ Scan(...any) error }) (Artist, error) {
	var artist Artist
	if err := row.Scan(&artist.ID, &artist.MusicBrainzID, &artist.Name, &artist.SortName, &artist.Disambiguation, &artist.Country, &artist.Type, &artist.Overview, &artist.ImageURL, &artist.Monitored, &artist.Status, &artist.AlbumCount); err != nil {
		return Artist{}, err
	}
	return artist, nil
}

func (s *Store) ArtistByMusicBrainzID(ctx context.Context, mbid string) (Artist, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+artistColumns+`,(SELECT COUNT(*) FROM music_albums a WHERE a.artist_id=music_artists.id) FROM music_artists WHERE musicbrainz_id=?`, mbid)
	return scanArtist(row)
}

func (s *Store) Artist(ctx context.Context, id int64) (Artist, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+artistColumns+`,(SELECT COUNT(*) FROM music_albums a WHERE a.artist_id=music_artists.id) FROM music_artists WHERE id=?`, id)
	return scanArtist(row)
}

func (s *Store) ListArtists(ctx context.Context, query string, page int) (ArtistPage, error) {
	if !ValidListQuery(query, page) {
		return ArtistPage{}, ErrInvalidInput
	}
	query = strings.TrimSpace(query)
	where := ""
	args := []any{}
	if query != "" {
		where = ` WHERE name LIKE ? ESCAPE '\'`
		args = append(args, likePattern(query))
	}
	out := ArtistPage{Items: []Artist{}, Page: page, Limit: pageSize}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM music_artists`+where, args...).Scan(&out.Total); err != nil {
		return ArtistPage{}, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+artistColumns+`,(SELECT COUNT(*) FROM music_albums a WHERE a.artist_id=music_artists.id) FROM music_artists`+where+` ORDER BY name COLLATE NOCASE, id LIMIT ? OFFSET ?`, append(args, pageSize, (page-1)*pageSize)...)
	if err != nil {
		return ArtistPage{}, err
	}
	defer rows.Close()
	for rows.Next() {
		artist, err := scanArtist(rows)
		if err != nil {
			return ArtistPage{}, err
		}
		out.Items = append(out.Items, artist)
	}
	return out, rows.Err()
}

const albumColumns = `a.id, a.artist_id, a.musicbrainz_id, a.title, a.primary_type, a.secondary_types, a.year, a.release_date, a.cover_url, a.monitored, a.status, ar.name`

func scanAlbum(row interface{ Scan(...any) error }) (Album, error) {
	var album Album
	var secondary string
	if err := row.Scan(&album.ID, &album.ArtistID, &album.MusicBrainzID, &album.Title, &album.PrimaryType, &secondary, &album.Year, &album.ReleaseDate, &album.CoverURL, &album.Monitored, &album.Status, &album.ArtistName, &album.TrackCount); err != nil {
		return Album{}, err
	}
	if secondary != "" {
		for _, part := range strings.Split(secondary, ", ") {
			if part = strings.TrimSpace(part); part != "" {
				album.SecondaryTypes = append(album.SecondaryTypes, part)
			}
		}
	}
	return album, nil
}

func (s *Store) Albums(ctx context.Context, artistID int64) ([]Album, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+albumColumns+`,(SELECT COUNT(*) FROM music_tracks t WHERE t.album_id=a.id) FROM music_albums a JOIN music_artists ar ON ar.id=a.artist_id WHERE a.artist_id=? ORDER BY a.year IS NULL, a.year, a.title COLLATE NOCASE, a.id LIMIT 1000`, artistID)
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

func (s *Store) Album(ctx context.Context, id int64) (Album, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+albumColumns+`,(SELECT COUNT(*) FROM music_tracks t WHERE t.album_id=a.id) FROM music_albums a JOIN music_artists ar ON ar.id=a.artist_id WHERE a.id=?`, id)
	return scanAlbum(row)
}

// AlbumByMusicBrainzID finds an existing release group without attaching it to
// a second artist. Discovery checks this before adding anything so a shared
// release group is never duplicated in the catalog.
func (s *Store) AlbumByMusicBrainzID(ctx context.Context, mbid string) (Album, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+albumColumns+`,(SELECT COUNT(*) FROM music_tracks t WHERE t.album_id=a.id) FROM music_albums a JOIN music_artists ar ON ar.id=a.artist_id WHERE a.musicbrainz_id=? ORDER BY a.id LIMIT 1`, mbid)
	return scanAlbum(row)
}

// SaveAlbum inserts one discovered release group idempotently and reports
// whether it created a new row.
func (s *Store) SaveAlbum(ctx context.Context, artistID int64, album Album) (int64, bool, error) {
	if existing, err := s.AlbumByMusicBrainzID(ctx, album.MusicBrainzID); err == nil {
		return existing.ID, false, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return 0, false, err
	}
	now := time.Now().Unix()
	result, err := s.db.ExecContext(ctx, `INSERT INTO music_albums(artist_id,musicbrainz_id,title,primary_type,secondary_types,year,release_date,cover_url,monitored,status,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(artist_id,musicbrainz_id) DO NOTHING`,
		artistID, album.MusicBrainzID, album.Title, album.PrimaryType, strings.Join(album.SecondaryTypes, ", "),
		album.Year, album.ReleaseDate, album.CoverURL, boolInt(album.Monitored), album.Status, now, now)
	if err != nil {
		return 0, false, err
	}
	if affected, err := result.RowsAffected(); err == nil && affected == 1 {
		id, err := result.LastInsertId()
		return id, true, err
	}
	existing, err := s.AlbumByMusicBrainzID(ctx, album.MusicBrainzID)
	if err != nil {
		return 0, false, err
	}
	return existing.ID, false, nil
}

func (s *Store) Tracks(ctx context.Context, albumID int64) ([]Track, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, album_id, musicbrainz_id, recording_id, disc, number, title, duration_ms FROM music_tracks WHERE album_id=? ORDER BY disc, number, id`, albumID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Track{}
	for rows.Next() {
		var track Track
		if err := rows.Scan(&track.ID, &track.AlbumID, &track.MusicBrainzID, &track.RecordingID, &track.Disc, &track.Number, &track.Title, &track.DurationMS); err != nil {
			return nil, err
		}
		out = append(out, track)
	}
	return out, rows.Err()
}

func (s *Store) ListFiles(ctx context.Context, page int) (MusicFilePage, error) {
	if page < 1 || page > 100000 {
		return MusicFilePage{}, ErrInvalidInput
	}
	const limit = 100
	out := MusicFilePage{Items: []MusicFile{}, Page: page, Limit: limit}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM music_files`).Scan(&out.Total); err != nil {
		return MusicFilePage{}, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT f.id,f.artist_id,f.album_id,f.track_id,ar.name,a.title,COALESCE(t.title,''),COALESCE(t.disc,1),COALESCE(t.number,0),f.relative_path,f.format,f.duration_ms,f.size_bytes
		FROM music_files f
		JOIN music_artists ar ON ar.id=f.artist_id
		JOIN music_albums a ON a.id=f.album_id
		LEFT JOIN music_tracks t ON t.id=f.track_id
		ORDER BY ar.name COLLATE NOCASE, a.title COLLATE NOCASE, t.disc, t.number, f.id
		LIMIT ? OFFSET ?`, limit, (page-1)*limit)
	if err != nil {
		return MusicFilePage{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var file MusicFile
		if err := rows.Scan(&file.ID, &file.ArtistID, &file.AlbumID, &file.TrackID, &file.ArtistName, &file.AlbumTitle, &file.TrackTitle, &file.Disc, &file.Number, &file.RelativePath, &file.Format, &file.DurationMS, &file.SizeBytes); err != nil {
			return MusicFilePage{}, err
		}
		out.Items = append(out.Items, file)
	}
	return out, rows.Err()
}

func (s *Store) SaveArtist(ctx context.Context, artist Artist, albums []Album) (Artist, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Artist{}, err
	}
	defer tx.Rollback()
	now := time.Now().Unix()
	if _, err = tx.ExecContext(ctx, `INSERT INTO music_artists(musicbrainz_id,name,sort_name,disambiguation,country,artist_type,overview,image_url,monitored,status,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(musicbrainz_id) DO UPDATE SET name=excluded.name,sort_name=excluded.sort_name,disambiguation=excluded.disambiguation,country=excluded.country,artist_type=excluded.artist_type,overview=excluded.overview,image_url=excluded.image_url,updated_at=excluded.updated_at`,
		artist.MusicBrainzID, artist.Name, artist.SortName, artist.Disambiguation, artist.Country, artist.Type, artist.Overview, artist.ImageURL, boolInt(artist.Monitored), artist.Status, now, now); err != nil {
		return Artist{}, err
	}
	var id int64
	if err = tx.QueryRowContext(ctx, `SELECT id FROM music_artists WHERE musicbrainz_id=?`, artist.MusicBrainzID).Scan(&id); err != nil {
		return Artist{}, err
	}
	for _, album := range albums {
		secondary := strings.Join(album.SecondaryTypes, ", ")
		if _, err = tx.ExecContext(ctx, `INSERT INTO music_albums(artist_id,musicbrainz_id,title,primary_type,secondary_types,year,release_date,cover_url,monitored,status,created_at,updated_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(artist_id,musicbrainz_id) DO UPDATE SET title=excluded.title,primary_type=excluded.primary_type,secondary_types=excluded.secondary_types,year=excluded.year,release_date=excluded.release_date,cover_url=excluded.cover_url,updated_at=excluded.updated_at`,
			id, album.MusicBrainzID, album.Title, album.PrimaryType, secondary, album.Year, album.ReleaseDate, album.CoverURL, boolInt(album.Monitored), album.Status, now, now); err != nil {
			return Artist{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return Artist{}, err
	}
	return s.Artist(ctx, id)
}

func (s *Store) SaveTracks(ctx context.Context, albumID int64, tracks []Track) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `DELETE FROM music_tracks WHERE album_id=?`, albumID); err != nil {
		return err
	}
	now := time.Now().Unix()
	for _, track := range tracks {
		if _, err = tx.ExecContext(ctx, `INSERT INTO music_tracks(album_id,musicbrainz_id,recording_id,disc,number,title,duration_ms,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`,
			albumID, track.MusicBrainzID, track.RecordingID, track.Disc, track.Number, track.Title, track.DurationMS, now, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func likePattern(query string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + replacer.Replace(query) + "%"
}
