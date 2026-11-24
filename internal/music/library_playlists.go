package music

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ErrLibraryPlaylistTrackExists distinguishes an already-present track from a
// stale review. Neither error changes the playlist or its revision.
var ErrLibraryPlaylistTrackExists = errors.New("This track is already in the playlist.")

// LibraryPlaylist is the administrator-owned library playlist summary. Its
// duration uses canonical track metadata, so it remains honest when a local
// file later disappears.
type LibraryPlaylist struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	TrackCount int    `json:"track_count"`
	DurationMS int64  `json:"duration_ms"`
	Revision   int    `json:"revision"`
	CreatedAt  int64  `json:"created_at"`
	UpdatedAt  int64  `json:"updated_at"`
}

type LibraryPlaylistList struct {
	Items []LibraryPlaylist `json:"items"`
}

type LibraryPlaylistDetails struct {
	Playlist LibraryPlaylist `json:"playlist"`
	Tracks   []ArtistTrack   `json:"tracks"`
}

type libraryPlaylistQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func validLibraryPlaylistName(name string) (string, bool) {
	name = strings.TrimSpace(name)
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 100 || strings.ContainsFunc(name, unicode.IsControl) {
		return "", false
	}
	return name, true
}

const libraryPlaylistSummaryColumns = `p.id,p.name,p.revision,p.created_at,p.updated_at,
	(SELECT COUNT(*) FROM music_library_playlist_entries e WHERE e.playlist_id=p.id),
	(SELECT COALESCE(SUM(t.duration_ms),0) FROM music_library_playlist_entries e JOIN music_tracks t ON t.id=e.track_id WHERE e.playlist_id=p.id)`

func scanLibraryPlaylist(row interface{ Scan(...any) error }) (LibraryPlaylist, error) {
	var out LibraryPlaylist
	err := row.Scan(&out.ID, &out.Name, &out.Revision, &out.CreatedAt, &out.UpdatedAt, &out.TrackCount, &out.DurationMS)
	return out, err
}

func libraryPlaylistSummary(ctx context.Context, q libraryPlaylistQuerier, id int64) (LibraryPlaylist, error) {
	out, err := scanLibraryPlaylist(q.QueryRowContext(ctx, `SELECT `+libraryPlaylistSummaryColumns+` FROM music_library_playlists p WHERE p.id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return LibraryPlaylist{}, ErrNotFound
	}
	return out, err
}

func (s *Service) ListLibraryPlaylists(ctx context.Context) (LibraryPlaylistList, error) {
	out := LibraryPlaylistList{Items: []LibraryPlaylist{}}
	rows, err := s.store.db.QueryContext(ctx, `SELECT `+libraryPlaylistSummaryColumns+` FROM music_library_playlists p ORDER BY p.name COLLATE NOCASE,p.id`)
	if err != nil {
		return LibraryPlaylistList{}, err
	}
	defer rows.Close()
	for rows.Next() {
		playlist, err := scanLibraryPlaylist(rows)
		if err != nil {
			return LibraryPlaylistList{}, err
		}
		out.Items = append(out.Items, playlist)
	}
	return out, rows.Err()
}

// LibraryPlaylist returns every canonical entry in playlist order, including
// tracks whose registered files have disappeared. The preferred-primary file
// selection and metadata columns deliberately match Store.ArtistTracks.
func (s *Service) LibraryPlaylist(ctx context.Context, id int64) (LibraryPlaylistDetails, error) {
	if id < 1 {
		return LibraryPlaylistDetails{}, ErrInvalidInput
	}
	tx, err := s.store.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return LibraryPlaylistDetails{}, err
	}
	defer tx.Rollback()
	summary, err := libraryPlaylistSummary(ctx, tx, id)
	if err != nil {
		return LibraryPlaylistDetails{}, err
	}
	out := LibraryPlaylistDetails{Playlist: summary, Tracks: []ArtistTrack{}}
	rows, err := tx.QueryContext(ctx, `SELECT t.id,t.album_id,t.musicbrainz_id,t.recording_id,t.disc,t.number,t.title,t.duration_ms,
		f.id,COALESCE(f.bitrate,0),COALESCE(f.size_bytes,0),COALESCE(f.format,''),COALESCE(f.relative_path,''),ar.name,a.title,a.year,a.cover_url
		FROM music_library_playlist_entries e
		JOIN music_tracks t ON t.id=e.track_id
		JOIN music_albums a ON a.id=t.album_id
		JOIN music_artists ar ON ar.id=a.artist_id
		LEFT JOIN music_files f ON f.id=(SELECT MIN(f0.id) FROM music_files f0 WHERE f0.track_id=t.id AND f0.variant_role='primary')
		WHERE e.playlist_id=? ORDER BY e.position,e.track_id`, id)
	if err != nil {
		return LibraryPlaylistDetails{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var track ArtistTrack
		var fileID sql.NullInt64
		if err := rows.Scan(&track.ID, &track.AlbumID, &track.MusicBrainzID, &track.RecordingID, &track.Disc, &track.Number, &track.Title, &track.DurationMS,
			&fileID, &track.Bitrate, &track.SizeBytes, &track.Format, &track.RelativePath, &track.ArtistName, &track.AlbumTitle, &track.AlbumYear, &track.CoverURL); err != nil {
			return LibraryPlaylistDetails{}, err
		}
		if fileID.Valid {
			value := fileID.Int64
			track.FileID = &value
		}
		out.Tracks = append(out.Tracks, track)
	}
	if err := rows.Err(); err != nil {
		return LibraryPlaylistDetails{}, err
	}
	if err := rows.Close(); err != nil {
		return LibraryPlaylistDetails{}, err
	}
	if err := tx.Commit(); err != nil {
		return LibraryPlaylistDetails{}, err
	}
	return out, nil
}

func (s *Service) CreateLibraryPlaylist(ctx context.Context, name string, trackID *int64) (LibraryPlaylistDetails, error) {
	name, valid := validLibraryPlaylistName(name)
	if !valid || trackID != nil && *trackID < 1 {
		return LibraryPlaylistDetails{}, ErrInvalidInput
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return LibraryPlaylistDetails{}, err
	}
	defer tx.Rollback()
	if trackID != nil {
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT 1 FROM music_tracks WHERE id=?`, *trackID).Scan(&exists); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return LibraryPlaylistDetails{}, ErrNotFound
			}
			return LibraryPlaylistDetails{}, err
		}
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO music_library_playlists(name,created_at,updated_at) VALUES(?,unixepoch(),unixepoch())`, name)
	if err != nil {
		return LibraryPlaylistDetails{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return LibraryPlaylistDetails{}, err
	}
	if trackID != nil {
		if _, err = tx.ExecContext(ctx, `INSERT INTO music_library_playlist_entries(playlist_id,track_id,position) VALUES(?,?,1)`, id, *trackID); err != nil {
			return LibraryPlaylistDetails{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return LibraryPlaylistDetails{}, err
	}
	return s.LibraryPlaylist(ctx, id)
}

func checkLibraryPlaylistRevision(ctx context.Context, tx *sql.Tx, id int64, revision int) error {
	if id < 1 || revision < 1 {
		return ErrInvalidInput
	}
	var current int
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM music_library_playlists WHERE id=?`, id).Scan(&current); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if current != revision {
		return ErrRevisionConflict
	}
	return nil
}

func bumpLibraryPlaylistRevision(ctx context.Context, tx *sql.Tx, id int64, revision int) error {
	result, err := tx.ExecContext(ctx, `UPDATE music_library_playlists SET revision=revision+1,updated_at=unixepoch() WHERE id=? AND revision=?`, id, revision)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return ErrRevisionConflict
	}
	return nil
}

func libraryPlaylistTrackIDs(ctx context.Context, q libraryPlaylistQuerier, id int64) ([]int64, error) {
	rows, err := q.QueryContext(ctx, `SELECT track_id FROM music_library_playlist_entries WHERE playlist_id=? ORDER BY position,track_id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var trackID int64
		if err := rows.Scan(&trackID); err != nil {
			return nil, err
		}
		ids = append(ids, trackID)
	}
	return ids, rows.Err()
}

// replaceLibraryPlaylistTracks is called only after complete membership has
// been validated. Delete-and-reinsert avoids transient position uniqueness
// conflicts and makes every successful mutation contiguous and deterministic.
func replaceLibraryPlaylistTracks(ctx context.Context, tx *sql.Tx, id int64, trackIDs []int64) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM music_library_playlist_entries WHERE playlist_id=?`, id); err != nil {
		return err
	}
	for index, trackID := range trackIDs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO music_library_playlist_entries(playlist_id,track_id,position) VALUES(?,?,?)`, id, trackID, index+1); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) RenameLibraryPlaylist(ctx context.Context, id int64, name string, revision int) (LibraryPlaylistDetails, error) {
	name, valid := validLibraryPlaylistName(name)
	if !valid {
		return LibraryPlaylistDetails{}, ErrInvalidInput
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return LibraryPlaylistDetails{}, err
	}
	defer tx.Rollback()
	if err = checkLibraryPlaylistRevision(ctx, tx, id, revision); err != nil {
		return LibraryPlaylistDetails{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE music_library_playlists SET name=? WHERE id=? AND revision=?`, name, id, revision); err != nil {
		return LibraryPlaylistDetails{}, err
	}
	if err = bumpLibraryPlaylistRevision(ctx, tx, id, revision); err != nil {
		return LibraryPlaylistDetails{}, err
	}
	if err = tx.Commit(); err != nil {
		return LibraryPlaylistDetails{}, err
	}
	return s.LibraryPlaylist(ctx, id)
}

func (s *Service) DeleteLibraryPlaylist(ctx context.Context, id int64, revision int) error {
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = checkLibraryPlaylistRevision(ctx, tx, id, revision); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM music_library_playlists WHERE id=? AND revision=?`, id, revision)
	if err != nil {
		return err
	}
	if affected, err := result.RowsAffected(); err != nil {
		return err
	} else if affected != 1 {
		return ErrRevisionConflict
	}
	return tx.Commit()
}

func (s *Service) AddLibraryPlaylistTrack(ctx context.Context, id, trackID int64, revision int) (LibraryPlaylistDetails, error) {
	if trackID < 1 {
		return LibraryPlaylistDetails{}, ErrInvalidInput
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return LibraryPlaylistDetails{}, err
	}
	defer tx.Rollback()
	if err = checkLibraryPlaylistRevision(ctx, tx, id, revision); err != nil {
		return LibraryPlaylistDetails{}, err
	}
	var exists int
	if err = tx.QueryRowContext(ctx, `SELECT 1 FROM music_tracks WHERE id=?`, trackID).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return LibraryPlaylistDetails{}, ErrNotFound
		}
		return LibraryPlaylistDetails{}, err
	}
	ids, err := libraryPlaylistTrackIDs(ctx, tx, id)
	if err != nil {
		return LibraryPlaylistDetails{}, err
	}
	for _, existing := range ids {
		if existing == trackID {
			return LibraryPlaylistDetails{}, ErrLibraryPlaylistTrackExists
		}
	}
	if err = replaceLibraryPlaylistTracks(ctx, tx, id, append(ids, trackID)); err != nil {
		return LibraryPlaylistDetails{}, err
	}
	if err = bumpLibraryPlaylistRevision(ctx, tx, id, revision); err != nil {
		return LibraryPlaylistDetails{}, err
	}
	if err = tx.Commit(); err != nil {
		return LibraryPlaylistDetails{}, err
	}
	return s.LibraryPlaylist(ctx, id)
}

func (s *Service) RemoveLibraryPlaylistTrack(ctx context.Context, id, trackID int64, revision int) (LibraryPlaylistDetails, error) {
	if trackID < 1 {
		return LibraryPlaylistDetails{}, ErrInvalidInput
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return LibraryPlaylistDetails{}, err
	}
	defer tx.Rollback()
	if err = checkLibraryPlaylistRevision(ctx, tx, id, revision); err != nil {
		return LibraryPlaylistDetails{}, err
	}
	ids, err := libraryPlaylistTrackIDs(ctx, tx, id)
	if err != nil {
		return LibraryPlaylistDetails{}, err
	}
	kept := make([]int64, 0, len(ids))
	found := false
	for _, existing := range ids {
		if existing == trackID {
			found = true
			continue
		}
		kept = append(kept, existing)
	}
	if !found {
		return LibraryPlaylistDetails{}, ErrNotFound
	}
	if err = replaceLibraryPlaylistTracks(ctx, tx, id, kept); err != nil {
		return LibraryPlaylistDetails{}, err
	}
	if err = bumpLibraryPlaylistRevision(ctx, tx, id, revision); err != nil {
		return LibraryPlaylistDetails{}, err
	}
	if err = tx.Commit(); err != nil {
		return LibraryPlaylistDetails{}, err
	}
	return s.LibraryPlaylist(ctx, id)
}

func (s *Service) ReorderLibraryPlaylist(ctx context.Context, id int64, trackIDs []int64, revision int) (LibraryPlaylistDetails, error) {
	seen := make(map[int64]bool, len(trackIDs))
	for _, trackID := range trackIDs {
		if trackID < 1 || seen[trackID] {
			return LibraryPlaylistDetails{}, ErrInvalidInput
		}
		seen[trackID] = true
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return LibraryPlaylistDetails{}, err
	}
	defer tx.Rollback()
	if err = checkLibraryPlaylistRevision(ctx, tx, id, revision); err != nil {
		return LibraryPlaylistDetails{}, err
	}
	current, err := libraryPlaylistTrackIDs(ctx, tx, id)
	if err != nil {
		return LibraryPlaylistDetails{}, err
	}
	if len(current) != len(trackIDs) {
		return LibraryPlaylistDetails{}, ErrInvalidInput
	}
	for _, trackID := range current {
		if !seen[trackID] {
			return LibraryPlaylistDetails{}, ErrInvalidInput
		}
	}
	if err = replaceLibraryPlaylistTracks(ctx, tx, id, trackIDs); err != nil {
		return LibraryPlaylistDetails{}, err
	}
	if err = bumpLibraryPlaylistRevision(ctx, tx, id, revision); err != nil {
		return LibraryPlaylistDetails{}, err
	}
	if err = tx.Commit(); err != nil {
		return LibraryPlaylistDetails{}, err
	}
	return s.LibraryPlaylist(ctx, id)
}
