package music

import (
	"context"
	"database/sql"
	"sort"
	"strings"
)

type AlbumPreferences struct {
	Preferences MusicPreferences `json:"preferences"`
	Inherited   bool             `json:"inherited"`
	Revision    int              `json:"revision"`
}

type UpgradeSuggestion struct {
	AlbumID    int64    `json:"album_id"`
	ArtistID   int64    `json:"artist_id"`
	ArtistName string   `json:"artist_name"`
	Title      string   `json:"title"`
	Year       *int     `json:"year"`
	Formats    []string `json:"formats"`
	Bitrate    int      `json:"bitrate"`
	Reason     string   `json:"reason"`
}

func validateMusicPreferences(in MusicPreferences) (MusicPreferences, error) {
	if in.MinBitrate < 0 || in.MinBitrate > 320 {
		return MusicPreferences{}, ErrInvalidInput
	}
	formats := []string{}
	seen := map[string]bool{}
	for _, value := range in.PreferredFormats {
		value = strings.ToUpper(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if !knownFormat(value) {
			return MusicPreferences{}, ErrInvalidInput
		}
		if !seen[value] {
			seen[value] = true
			formats = append(formats, value)
		}
	}
	in.PreferredFormats = formats
	return in, nil
}

// AlbumPreferences returns the effective preferences for one album: the saved
// override when present, otherwise the global preference.
func (s *Service) AlbumPreferences(ctx context.Context, albumID int64) (AlbumPreferences, error) {
	global, err := s.GetPreferences(ctx)
	if err != nil {
		return AlbumPreferences{}, err
	}
	var allow, minimum sql.NullInt64
	var formats string
	var revision int
	if err := s.store.db.QueryRowContext(ctx, `SELECT allow_lossless,min_bitrate,preferred_formats,revision FROM music_albums WHERE id=?`, albumID).Scan(&allow, &minimum, &formats, &revision); err != nil {
		return AlbumPreferences{}, err
	}
	if !allow.Valid && !minimum.Valid && strings.TrimSpace(formats) == "" {
		return AlbumPreferences{Preferences: global, Inherited: true, Revision: revision}, nil
	}
	out := global
	if allow.Valid {
		out.AllowLossless = allow.Int64 == 1
	}
	if minimum.Valid {
		out.MinBitrate = int(minimum.Int64)
	}
	if strings.TrimSpace(formats) != "" {
		out.PreferredFormats = splitFormats(formats)
	}
	return AlbumPreferences{Preferences: out, Inherited: false, Revision: revision}, nil
}

func (s *Service) SaveAlbumPreferences(ctx context.Context, albumID int64, in MusicPreferences) (AlbumPreferences, error) {
	clean, err := validateMusicPreferences(in)
	if err != nil {
		return AlbumPreferences{}, err
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return AlbumPreferences{}, err
	}
	defer tx.Rollback()
	var current int
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM music_albums WHERE id=?`, albumID).Scan(&current); err != nil {
		return AlbumPreferences{}, err
	}
	if current != in.Revision {
		return AlbumPreferences{}, ErrRevisionConflict
	}
	if _, err := tx.ExecContext(ctx, `UPDATE music_albums SET allow_lossless=?,min_bitrate=?,preferred_formats=?,revision=revision+1,updated_at=unixepoch() WHERE id=? AND revision=?`,
		boolInt(clean.AllowLossless), clean.MinBitrate, strings.Join(clean.PreferredFormats, ","), albumID, in.Revision); err != nil {
		return AlbumPreferences{}, err
	}
	if err := tx.Commit(); err != nil {
		return AlbumPreferences{}, err
	}
	return s.AlbumPreferences(ctx, albumID)
}

// ResetAlbumPreferences removes the album override and returns to inheritance.
func (s *Service) ResetAlbumPreferences(ctx context.Context, albumID int64) (AlbumPreferences, error) {
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return AlbumPreferences{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE music_albums SET allow_lossless=NULL,min_bitrate=NULL,preferred_formats='',revision=revision+1,updated_at=unixepoch() WHERE id=?`, albumID); err != nil {
		return AlbumPreferences{}, err
	}
	if err := tx.Commit(); err != nil {
		return AlbumPreferences{}, err
	}
	return s.AlbumPreferences(ctx, albumID)
}

// UpgradeSuggestions lists albums whose registered files fall below their
// effective preference. It is a read-only suggestion; it never replaces files.
func (s *Service) UpgradeSuggestions(ctx context.Context, limit int) ([]UpgradeSuggestion, error) {
	if limit < 1 {
		limit = 200
	}
	if limit > 500 {
		limit = 500
	}
	rows, err := s.store.db.QueryContext(ctx, `SELECT a.id,ar.id,ar.name,a.title,a.year,f.format,f.bitrate
		FROM music_albums a
		JOIN music_artists ar ON ar.id=a.artist_id
		JOIN music_files f ON f.album_id=a.id
		ORDER BY a.id, f.id`)
	if err != nil {
		return nil, err
	}
	type albumFiles struct {
		suggestion UpgradeSuggestion
		formats    map[string]bool
		lossless   bool
		bitrate    int
	}
	order := []int64{}
	byID := map[int64]*albumFiles{}
	for rows.Next() {
		var albumID, artistID int64
		var artistName, title, format string
		var year sql.NullInt64
		var bitrate int
		if err := rows.Scan(&albumID, &artistID, &artistName, &title, &year, &format, &bitrate); err != nil {
			rows.Close()
			return nil, err
		}
		entry := byID[albumID]
		if entry == nil {
			entry = &albumFiles{suggestion: UpgradeSuggestion{AlbumID: albumID, ArtistID: artistID, ArtistName: artistName, Title: title}, formats: map[string]bool{}}
			if year.Valid {
				y := int(year.Int64)
				entry.suggestion.Year = &y
			}
			byID[albumID] = entry
			order = append(order, albumID)
		}
		format = strings.ToUpper(strings.TrimSpace(format))
		if format != "" {
			entry.formats[format] = true
		}
		if format == "FLAC" || format == "ALAC" || format == "WAV" {
			entry.lossless = true
		}
		if bitrate > entry.bitrate {
			entry.bitrate = bitrate
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := []UpgradeSuggestion{}
	for _, albumID := range order {
		entry := byID[albumID]
		effective, err := s.AlbumPreferences(ctx, albumID)
		if err != nil {
			return nil, err
		}
		reasons := []string{}
		if effective.Preferences.AllowLossless && !entry.lossless {
			reasons = append(reasons, "Lossless is preferred but all registered files are lossy.")
		}
		if effective.Preferences.MinBitrate > 0 && !entry.lossless && entry.bitrate < effective.Preferences.MinBitrate {
			reasons = append(reasons, "Registered files are below the minimum bitrate.")
		}
		if len(effective.Preferences.PreferredFormats) > 0 {
			matched := false
			for _, format := range effective.Preferences.PreferredFormats {
				if entry.formats[format] {
					matched = true
					break
				}
			}
			if !matched {
				reasons = append(reasons, "Registered files are not in a preferred format.")
			}
		}
		if len(reasons) == 0 {
			continue
		}
		suggestion := entry.suggestion
		for format := range entry.formats {
			suggestion.Formats = append(suggestion.Formats, format)
		}
		sort.Strings(suggestion.Formats)
		suggestion.Bitrate = entry.bitrate
		suggestion.Reason = strings.Join(reasons, " ")
		out = append(out, suggestion)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}
