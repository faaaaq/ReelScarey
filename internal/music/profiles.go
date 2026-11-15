package music

import (
	"context"
	"strings"
	"time"
	"unicode"
)

// QualityProfile is a transcoding target for music files. Builtins are code
// and are never stored in the database; custom profiles are persisted in
// music_quality_profiles with Custom set and Key empty.
type QualityProfile struct {
	ID               int64  `json:"id,omitempty"`
	Name             string `json:"name"`
	Key              string `json:"key,omitempty"`
	Codec            string `json:"codec"`
	Container        string `json:"container,omitempty"`
	BitrateKbps      int    `json:"bitrate_kbps"`
	VBR              bool   `json:"vbr"`
	CompressionLevel int    `json:"compression_level,omitempty"`
	RequireLossless  bool   `json:"require_lossless"`
	KeepOriginal     bool   `json:"keep_original"`
	Custom           bool   `json:"custom,omitempty"`
}

// BuiltinProfiles returns the twelve fixed quality profiles. The list order
// is stable so the UI shows the same choices everywhere.
func BuiltinProfiles() []QualityProfile {
	return []QualityProfile{
		{Key: "original", Name: "Original", Codec: "original", Container: "", BitrateKbps: 0, VBR: true},
		{Key: "lossless-archive", Name: "Lossless Archive", Codec: "flac", Container: "flac", BitrateKbps: 0, VBR: true, CompressionLevel: 8, KeepOriginal: true},
		{Key: "lossless-opus", Name: "Lossless Opus", Codec: "opus", Container: "opus", BitrateKbps: 192, VBR: true, KeepOriginal: true},
		{Key: "opus-economy", Name: "Opus Economy", Codec: "opus", Container: "opus", BitrateKbps: 64, VBR: true},
		{Key: "opus-balanced", Name: "Opus Balanced", Codec: "opus", Container: "opus", BitrateKbps: 96, VBR: true},
		{Key: "opus-high", Name: "Opus High", Codec: "opus", Container: "opus", BitrateKbps: 128, VBR: true},
		{Key: "opus-premium", Name: "Opus Premium", Codec: "opus", Container: "opus", BitrateKbps: 160, VBR: true},
		{Key: "opus-maximum", Name: "Opus Maximum", Codec: "opus", Container: "opus", BitrateKbps: 192, VBR: true},
		{Key: "mp3-compatible", Name: "MP3 Compatible", Codec: "mp3", Container: "mp3", BitrateKbps: 192, VBR: false},
		{Key: "mp3-maximum", Name: "MP3 Maximum", Codec: "mp3", Container: "mp3", BitrateKbps: 320, VBR: false},
		{Key: "aac-compatible", Name: "AAC Compatible", Codec: "aac", Container: "m4a", BitrateKbps: 128, VBR: false},
		{Key: "aac-high", Name: "AAC High", Codec: "aac", Container: "m4a", BitrateKbps: 256, VBR: true},
	}
}

// ProfileByKey returns the builtin profile for a stable key. Custom profiles
// live in the database and are resolved by Service methods.
func ProfileByKey(key string) (QualityProfile, bool) {
	for _, profile := range BuiltinProfiles() {
		if profile.Key == key {
			return profile, true
		}
	}
	return QualityProfile{}, false
}

// ResolveProfileKey resolves a profile key against the database after the
// builtins. Custom rows carry the empty-key column, so they are matched by
// their own name.
func (s *Service) ResolveProfileKey(ctx context.Context, key string) (QualityProfile, error) {
	if profile, ok := ProfileByKey(key); ok {
		return profile, nil
	}
	if strings.TrimSpace(key) == "" {
		return QualityProfile{}, ErrInvalidInput
	}
	profiles, err := s.ListProfiles(ctx)
	if err != nil {
		return QualityProfile{}, err
	}
	for _, profile := range profiles {
		if !profile.Custom {
			continue
		}
		if profile.Name == key {
			return profile, nil
		}
	}
	return QualityProfile{}, ErrInvalidInput
}

// ResolveProfile is the pure resolution order: explicit key/override > per-album
// preference > global preference. Invalid keys are ErrInvalidInput.
func ResolveProfile(global MusicPreferences, albumOverride string, explicit string) (QualityProfile, error) {
	key := strings.TrimSpace(explicit)
	if key == "" {
		key = strings.TrimSpace(albumOverride)
	}
	if key == "" {
		key = strings.TrimSpace(global.DownloadProfile)
	}
	if key == "" {
		return QualityProfile{}, ErrInvalidInput
	}
	profile, ok := ProfileByKey(key)
	if !ok {
		return QualityProfile{}, ErrInvalidInput
	}
	return profile, nil
}

// AlbumProfileOverride reads the per-album download profile override stored
// in the music_albums download_profile column; empty means follow global.
// overrides.go owns the album preferences read and must not be edited.
func (s *Service) AlbumProfileOverride(ctx context.Context, albumID int64) (string, error) {
	var profile string
	if err := s.store.db.QueryRowContext(ctx, `SELECT download_profile FROM music_albums WHERE id=?`, albumID).Scan(&profile); err != nil {
		return "", err
	}
	return profile, nil
}

// ListProfiles returns the builtin profiles followed by custom rows.
func (s *Service) ListProfiles(ctx context.Context) ([]QualityProfile, error) {
	profiles := BuiltinProfiles()
	rows, err := s.store.db.QueryContext(ctx, `SELECT id, name, key, codec, container, bitrate_kbps, vbr, compression_level, require_lossless, keep_original, custom FROM music_quality_profiles ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var profile QualityProfile
		if err := rows.Scan(&profile.ID, &profile.Name, &profile.Key, &profile.Codec, &profile.Container, &profile.BitrateKbps, &profile.VBR, &profile.CompressionLevel, &profile.RequireLossless, &profile.KeepOriginal, &profile.Custom); err != nil {
			return nil, err
		}
		profiles = append(profiles, profile)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return profiles, nil
}

// SaveCustomProfile creates or updates a custom profile row. Builtin profiles
// are code and are never stored in the database.
func (s *Service) SaveCustomProfile(ctx context.Context, in QualityProfile) (QualityProfile, error) {
	clean, err := validateCustomProfile(in)
	if err != nil {
		return QualityProfile{}, err
	}
	now := time.Now().Unix()
	clean.Key = ""
	clean.Custom = true
	if in.ID > 0 {
		result, err := s.store.db.ExecContext(ctx, `UPDATE music_quality_profiles
			SET name=?, codec=?, container=?, bitrate_kbps=?, vbr=?, compression_level=?, require_lossless=?, keep_original=?, custom=1, updated_at=?
			WHERE id=? AND custom=1`,
			clean.Name, clean.Codec, clean.Container, clean.BitrateKbps, boolInt(clean.VBR), clean.CompressionLevel, boolInt(clean.RequireLossless), boolInt(clean.KeepOriginal), now, in.ID)
		if err != nil {
			return QualityProfile{}, err
		}
		if affected, err := result.RowsAffected(); err != nil {
			return QualityProfile{}, err
		} else if affected != 1 {
			return QualityProfile{}, ErrInvalidInput
		}
		return s.customProfile(ctx, in.ID)
	}
	result, err := s.store.db.ExecContext(ctx, `INSERT INTO music_quality_profiles
		(name, key, codec, container, bitrate_kbps, vbr, compression_level, require_lossless, keep_original, custom, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		clean.Name, "", clean.Codec, clean.Container, clean.BitrateKbps, boolInt(clean.VBR), clean.CompressionLevel, boolInt(clean.RequireLossless), boolInt(clean.KeepOriginal), 1, now, now)
	if err != nil {
		return QualityProfile{}, err
	}
	if in.ID, err = result.LastInsertId(); err != nil {
		return QualityProfile{}, err
	}
	return s.customProfile(ctx, in.ID)
}

// DeleteCustomProfile removes a custom profile row. Builtin profiles are code
// and are never stored in the database.
func (s *Service) DeleteCustomProfile(ctx context.Context, id int64) error {
	result, err := s.store.db.ExecContext(ctx, `DELETE FROM music_quality_profiles WHERE id=? AND custom=1`, id)
	if err != nil {
		return err
	}
	if affected, err := result.RowsAffected(); err != nil {
		return err
	} else if affected != 1 {
		return ErrInvalidInput
	}
	return nil
}

func (s *Service) customProfile(ctx context.Context, id int64) (QualityProfile, error) {
	var profile QualityProfile
	if err := s.store.db.QueryRowContext(ctx, `SELECT id, name, key, codec, container, bitrate_kbps, vbr, compression_level, require_lossless, keep_original, custom FROM music_quality_profiles WHERE id=?`, id).
		Scan(&profile.ID, &profile.Name, &profile.Key, &profile.Codec, &profile.Container, &profile.BitrateKbps, &profile.VBR, &profile.CompressionLevel, &profile.RequireLossless, &profile.KeepOriginal, &profile.Custom); err != nil {
		return QualityProfile{}, err
	}
	return profile, nil
}

// validateCustomProfile trims the name and validates codec, bitrate, container
// and name length so a custom profile row cannot be saved with an unusable
// encoder target. Control characters are rejected on the raw name so a
// trailing newline or tab is never silently accepted.
func validateCustomProfile(in QualityProfile) (QualityProfile, error) {
	clean := QualityProfile{
		Name:             strings.TrimSpace(in.Name),
		Codec:            strings.TrimSpace(strings.ToLower(in.Codec)),
		Container:        strings.TrimSpace(strings.ToLower(in.Container)),
		BitrateKbps:      in.BitrateKbps,
		VBR:              in.VBR,
		CompressionLevel: in.CompressionLevel,
		RequireLossless:  in.RequireLossless,
		KeepOriginal:     in.KeepOriginal,
	}
	if clean.Name == "" || len(clean.Name) > 100 {
		return QualityProfile{}, ErrInvalidInput
	}
	for _, char := range in.Name {
		if unicode.IsControl(char) {
			return QualityProfile{}, ErrInvalidInput
		}
	}
	switch clean.Codec {
	case "original":
		if clean.BitrateKbps != 0 {
			return QualityProfile{}, ErrInvalidInput
		}
		clean.Container = ""
	case "flac":
		if clean.BitrateKbps != 0 {
			return QualityProfile{}, ErrInvalidInput
		}
		if clean.CompressionLevel < 0 || clean.CompressionLevel > 8 {
			return QualityProfile{}, ErrInvalidInput
		}
		clean.Container = "flac"
	case "mp3", "aac", "opus":
		if clean.BitrateKbps < 1 || clean.BitrateKbps > 320 {
			return QualityProfile{}, ErrInvalidInput
		}
		clean.CompressionLevel = 0
	default:
		return QualityProfile{}, ErrInvalidInput
	}
	if clean.Container != "" {
		for _, char := range clean.Container {
			if !(char >= 'a' && char <= 'z') {
				return QualityProfile{}, ErrInvalidInput
			}
		}
		if len(clean.Container) > 8 {
			return QualityProfile{}, ErrInvalidInput
		}
	}
	return clean, nil
}

// validatePreferenceKey accepts an empty download profile (follow global) or
// a builtin key. Custom profiles are resolved by the Service before use.
func validatePreferenceKey(key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil
	}
	if _, ok := ProfileByKey(key); ok {
		return nil
	}
	return ErrInvalidInput
}
