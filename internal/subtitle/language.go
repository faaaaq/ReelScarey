package subtitle

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"
)

type LanguageProfile struct {
	ID             int64    `json:"id"`
	Name           string   `json:"name"`
	Tracks         []Track  `json:"tracks"`
	AudioLanguages []string `json:"audio_languages"`
	Revision       int      `json:"revision"`
}

type LanguageDefault struct {
	LibraryKey string `json:"library_key"`
	ProfileID  *int64 `json:"profile_id"`
	Revision   int    `json:"revision"`
}

var ErrLanguageProfile = errors.New("Choose a unique profile name, 1–9 subtitle tracks and at most three audio/subtitle languages.")

func (s *Service) LanguageProfiles(ctx context.Context) ([]LanguageProfile, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,subtitle_tracks,audio_languages,revision FROM language_profiles ORDER BY name COLLATE NOCASE,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LanguageProfile{}
	for rows.Next() {
		var profile LanguageProfile
		var tracks, audio string
		if err := rows.Scan(&profile.ID, &profile.Name, &tracks, &audio, &profile.Revision); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(tracks), &profile.Tracks); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(audio), &profile.AudioLanguages); err != nil {
			return nil, err
		}
		out = append(out, profile)
	}
	return out, rows.Err()
}

func validateLanguageProfile(profile LanguageProfile) error {
	profile.Name = strings.TrimSpace(profile.Name)
	if profile.Name == "" || len(profile.Name) > 60 || strings.ContainsFunc(profile.Name, unicode.IsControl) {
		return ErrLanguageProfile
	}
	if len(profile.Tracks) == 0 || len(profile.Tracks) > 9 {
		return ErrLanguageProfile
	}
	seen := map[Track]bool{}
	languages := map[string]bool{}
	for _, track := range profile.Tracks {
		if !validTrack(track) || seen[track] {
			return ErrLanguageProfile
		}
		seen[track] = true
		languages[track.Language] = true
	}
	if len(languages) > 3 || len(profile.AudioLanguages) > 3 {
		return ErrLanguageProfile
	}
	seenAudio := map[string]bool{}
	for _, code := range profile.AudioLanguages {
		if language(code) == "" || seenAudio[code] {
			return ErrLanguageProfile
		}
		seenAudio[code] = true
	}
	return nil
}

func (s *Service) SaveLanguageProfile(ctx context.Context, id int64, profile LanguageProfile) (LanguageProfile, error) {
	profile.Name = strings.TrimSpace(profile.Name)
	if err := validateLanguageProfile(profile); err != nil {
		return LanguageProfile{}, err
	}
	tracks, _ := json.Marshal(profile.Tracks)
	audio, _ := json.Marshal(profile.AudioLanguages)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return LanguageProfile{}, err
	}
	defer tx.Rollback()
	var duplicate int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM language_profiles WHERE name=? COLLATE NOCASE AND id<>?`, profile.Name, id).Scan(&duplicate); err != nil {
		return LanguageProfile{}, err
	}
	if duplicate > 0 {
		return LanguageProfile{}, ErrLanguageProfile
	}
	if id == 0 {
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM language_profiles`).Scan(&count); err != nil {
			return LanguageProfile{}, err
		}
		if count >= 20 {
			return LanguageProfile{}, fmt.Errorf("%w: at most 20 profiles are supported", ErrLanguageProfile)
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO language_profiles(name,subtitle_tracks,audio_languages) VALUES(?,?,?)`, profile.Name, string(tracks), string(audio))
		if err != nil {
			return LanguageProfile{}, err
		}
		profile.ID, err = result.LastInsertId()
		if err != nil {
			return LanguageProfile{}, err
		}
		profile.Revision = 1
	} else {
		result, err := tx.ExecContext(ctx, `UPDATE language_profiles SET name=?,subtitle_tracks=?,audio_languages=?,revision=revision+1,updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id=? AND revision=?`, profile.Name, string(tracks), string(audio), id, profile.Revision)
		if err != nil {
			return LanguageProfile{}, err
		}
		if changed, _ := result.RowsAffected(); changed != 1 {
			return LanguageProfile{}, ErrLanguageProfile
		}
		profile.Revision++
	}
	if err := tx.Commit(); err != nil {
		return LanguageProfile{}, err
	}
	return profile, nil
}

func (s *Service) DeleteLanguageProfile(ctx context.Context, id int64) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM language_profiles WHERE id=?`, id)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Service) LanguageDefaults(ctx context.Context) ([]LanguageDefault, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT library_key,profile_id,revision FROM library_language_defaults ORDER BY CASE library_key WHEN 'movies' THEN 0 WHEN 'tv' THEN 1 ELSE 2 END`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LanguageDefault{}
	for rows.Next() {
		var item LanguageDefault
		if err := rows.Scan(&item.LibraryKey, &item.ProfileID, &item.Revision); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Service) SaveLanguageDefaults(ctx context.Context, items []LanguageDefault) ([]LanguageDefault, error) {
	if len(items) != 3 {
		return nil, ErrLanguageProfile
	}
	seen := map[string]bool{}
	for _, item := range items {
		if !slices.Contains([]string{"movies", "tv", "anime"}, item.LibraryKey) || seen[item.LibraryKey] {
			return nil, ErrLanguageProfile
		}
		seen[item.LibraryKey] = true
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	for _, item := range items {
		var revision int
		if err := tx.QueryRowContext(ctx, `SELECT revision FROM library_language_defaults WHERE library_key=?`, item.LibraryKey).Scan(&revision); err != nil {
			return nil, err
		}
		if revision != item.Revision {
			return nil, fmt.Errorf("%w: a library default changed; reload and try again", ErrLanguageProfile)
		}
		if item.ProfileID != nil {
			var count int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM language_profiles WHERE id=?`, *item.ProfileID).Scan(&count); err != nil {
				return nil, err
			}
			if count != 1 {
				return nil, ErrLanguageProfile
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE library_language_defaults SET profile_id=?,revision=revision+1,updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE library_key=?`, item.ProfileID, item.LibraryKey); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.LanguageDefaults(ctx)
}

// SetMediaLanguageProfile assigns an explicit profile to one title. A nil
// profile removes the override and falls back to the library default.
func (s *Service) SetMediaLanguageProfile(ctx context.Context, mediaID int64, profileID *int64) error {
	if mediaID < 1 {
		return ErrLanguageProfile
	}
	if profileID == nil {
		_, err := s.db.ExecContext(ctx, `DELETE FROM item_language_profiles WHERE media_id=?`, mediaID)
		return err
	}
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM media_items WHERE id=?`, mediaID).Scan(&count); err != nil {
		return err
	}
	if count != 1 {
		return sql.ErrNoRows
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM language_profiles WHERE id=?`, *profileID).Scan(&count); err != nil {
		return err
	}
	if count != 1 {
		return ErrLanguageProfile
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO item_language_profiles(media_id,profile_id) VALUES(?,?) ON CONFLICT(media_id) DO UPDATE SET profile_id=excluded.profile_id,revision=item_language_profiles.revision+1,updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now')`, mediaID, *profileID)
	return err
}

// preferencesFor overlays a library or item language profile on the global
// subtitle preferences. It falls back silently when no profile is assigned.
func (s *Service) preferencesFor(ctx context.Context, m media, p Preferences) Preferences {
	var profileID sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT profile_id FROM item_language_profiles WHERE media_id=?`, m.MediaID).Scan(&profileID); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return p
	}
	if !profileID.Valid {
		key := "tv"
		if m.Kind == "movie" {
			key = "movies"
		} else if m.Category == "anime" {
			key = "anime"
		}
		if err := s.db.QueryRowContext(ctx, `SELECT profile_id FROM library_language_defaults WHERE library_key=?`, key).Scan(&profileID); err != nil {
			return p
		}
	}
	if !profileID.Valid {
		return p
	}
	var tracks string
	if err := s.db.QueryRowContext(ctx, `SELECT subtitle_tracks FROM language_profiles WHERE id=?`, profileID.Int64).Scan(&tracks); err != nil {
		return p
	}
	var parsed []Track
	if json.Unmarshal([]byte(tracks), &parsed) == nil && len(parsed) > 0 {
		p.Tracks = parsed
	}
	return p
}
