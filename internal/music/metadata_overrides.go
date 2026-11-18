package music

import (
	"context"
	"database/sql"
	"errors"
)

// RestoreAlbumMetadataField replaces one owner-corrected catalog field with
// its current MusicBrainz value. The override flag is cleared in the same
// transaction as the successful write; provider failure changes nothing.
// This is a catalog operation and never rewrites a media file.
func (s *Service) RestoreAlbumMetadataField(ctx context.Context, id int64, field string) (AlbumRefreshResult, error) {
	result := AlbumRefreshResult{AlbumID: id}
	if id < 1 || field != "title" && field != "year" {
		return result, ErrInvalidInput
	}
	var mbid string
	if err := s.store.db.QueryRowContext(ctx, `SELECT musicbrainz_id FROM music_albums WHERE id=?`, id).Scan(&mbid); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return result, ErrNotFound
		}
		return result, err
	}
	if !ValidMBID(mbid) || s.client == nil {
		return result, &Error{Code: "music_provider_identity", Message: "This album has no MusicBrainz identity to restore from."}
	}
	album, _, err := s.client.ReleaseGroup(ctx, mbid)
	if err != nil {
		return result, err
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	switch field {
	case "title":
		_, err = tx.ExecContext(ctx, `UPDATE music_albums SET title=?,title_overridden=0,updated_at=unixepoch() WHERE id=?`, album.Title, id)
		result.Title = album.Title
	case "year":
		var year any
		if album.Year != nil {
			year = *album.Year
		}
		_, err = tx.ExecContext(ctx, `UPDATE music_albums SET year=?,year_overridden=0,updated_at=unixepoch() WHERE id=?`, year, id)
	}
	if err != nil {
		return result, err
	}
	if err = tx.Commit(); err != nil {
		return result, err
	}
	result.AlbumUpdate = true
	result.Message = "The MusicBrainz " + field + " was restored in the catalog. Existing audio files were not retagged."
	return result, nil
}

// RestoreArtistMetadataField restores the provider artist name without
// clearing any album overrides or modifying audio files.
func (s *Service) RestoreArtistMetadataField(ctx context.Context, id int64, field string) (ArtistRefreshResult, error) {
	result := ArtistRefreshResult{ArtistID: id}
	if id < 1 || field != "name" {
		return result, ErrInvalidInput
	}
	var mbid string
	if err := s.store.db.QueryRowContext(ctx, `SELECT musicbrainz_id FROM music_artists WHERE id=?`, id).Scan(&mbid); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return result, ErrNotFound
		}
		return result, err
	}
	if !ValidMBID(mbid) || s.client == nil {
		return result, &Error{Code: "music_provider_identity", Message: "This artist has no MusicBrainz identity to restore from."}
	}
	artist, err := s.client.Artist(ctx, mbid)
	if err != nil {
		return result, err
	}
	if _, err = s.store.db.ExecContext(ctx, `UPDATE music_artists SET name=?,name_overridden=0,updated_at=unixepoch() WHERE id=?`, artist.Name, id); err != nil {
		return result, err
	}
	result.ArtistUpdated = true
	result.Name = artist.Name
	result.Message = "The MusicBrainz artist name was restored in the catalog. Existing audio files were not retagged."
	return result, nil
}
