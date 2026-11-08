package music

import (
	"context"
	"errors"
	"strings"
)

// ErrRevisionConflict reports that a reviewed music setting changed after it
// was loaded. Callers must reload the current value before retrying.
var ErrRevisionConflict = errors.New("These music settings changed after they were loaded. Reload and try again.")

// MusicPreferences are the release-selection rules used when a wanted album is
// searched. PreferredFormats is empty when any format is acceptable.
type MusicPreferences struct {
	AllowLossless    bool     `json:"allow_lossless"`
	MinBitrate       int      `json:"min_bitrate"`
	PreferredFormats []string `json:"preferred_formats"`
	Revision         int      `json:"revision"`
}

var musicFormats = []string{"FLAC", "ALAC", "MP3", "AAC", "OGG", "OPUS", "WAV"}

func knownFormat(value string) bool {
	for _, format := range musicFormats {
		if format == value {
			return true
		}
	}
	return false
}

// GetPreferences reads the single persisted preference row. A missing row is
// treated as an error because migration 063 seeds it.
func (s *Service) GetPreferences(ctx context.Context) (MusicPreferences, error) {
	var prefs MusicPreferences
	var formats string
	err := s.store.db.QueryRowContext(ctx, `SELECT allow_lossless, min_bitrate, preferred_formats, revision FROM music_preferences WHERE id=1`).
		Scan(&prefs.AllowLossless, &prefs.MinBitrate, &formats, &prefs.Revision)
	if err != nil {
		return MusicPreferences{}, err
	}
	prefs.PreferredFormats = splitFormats(formats)
	return prefs, nil
}

// SavePreferences validates a reviewed preference set, compares its revision
// inside a write transaction and increments the stored revision.
func (s *Service) SavePreferences(ctx context.Context, in MusicPreferences) (MusicPreferences, error) {
	formats, err := normalizeFormats(in.PreferredFormats)
	if err != nil {
		return MusicPreferences{}, err
	}
	if in.MinBitrate < 0 || in.MinBitrate > 320 {
		return MusicPreferences{}, ErrInvalidInput
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return MusicPreferences{}, err
	}
	defer tx.Rollback()
	var current int
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM music_preferences WHERE id=1`).Scan(&current); err != nil {
		return MusicPreferences{}, err
	}
	if current != in.Revision {
		return MusicPreferences{}, ErrRevisionConflict
	}
	result, err := tx.ExecContext(ctx, `UPDATE music_preferences
		SET allow_lossless=?, min_bitrate=?, preferred_formats=?, revision=revision+1, updated_at=unixepoch()
		WHERE id=1 AND revision=?`,
		boolInt(in.AllowLossless), in.MinBitrate, strings.Join(formats, ","), in.Revision)
	if err != nil {
		return MusicPreferences{}, err
	}
	if affected, err := result.RowsAffected(); err != nil {
		return MusicPreferences{}, err
	} else if affected != 1 {
		return MusicPreferences{}, ErrRevisionConflict
	}
	if err := tx.Commit(); err != nil {
		return MusicPreferences{}, err
	}
	return s.GetPreferences(ctx)
}

// CandidateAllowed applies the saved preferences to one release candidate.
// A lossless candidate is rejected when lossless is not allowed, a candidate
// below the minimum bitrate is rejected, and a non-empty preferred-format list
// restricts candidates to the listed formats (case-insensitive).
func CandidateAllowed(prefs MusicPreferences, format string, bitrate int, lossless bool) bool {
	if lossless && !prefs.AllowLossless {
		return false
	}
	if bitrate < prefs.MinBitrate {
		return false
	}
	if len(prefs.PreferredFormats) == 0 {
		return true
	}
	format = strings.ToUpper(strings.TrimSpace(format))
	for _, preferred := range prefs.PreferredFormats {
		if strings.EqualFold(preferred, format) {
			return true
		}
	}
	return false
}

func splitFormats(value string) []string {
	out := []string{}
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, strings.ToUpper(part))
	}
	return out
}

func normalizeFormats(formats []string) ([]string, error) {
	if len(formats) > len(musicFormats) {
		return nil, ErrInvalidInput
	}
	out := make([]string, 0, len(formats))
	seen := map[string]bool{}
	for _, format := range formats {
		format = strings.ToUpper(strings.TrimSpace(format))
		if !knownFormat(format) {
			return nil, ErrInvalidInput
		}
		if seen[format] {
			continue
		}
		seen[format] = true
		out = append(out, format)
	}
	return out, nil
}
