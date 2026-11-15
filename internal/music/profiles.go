package music

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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
		{Key: "lossless-opus", Name: "Lossless Opus", Codec: "opus", Container: "opus", BitrateKbps: 128, VBR: true, KeepOriginal: true},
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
		if profile.Name == key || profile.Key == key {
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
	// The name column is UNIQUE (case-insensitive). Translate the constraint
	// into a typed conflict before the write so a duplicate save answers with
	// an honest 409-class error instead of a raw storage failure. A concurrent
	// duplicate save can still lose the INSERT race; that only yields the
	// generic storage error, never inconsistent data.
	var taken int64
	if in.ID > 0 {
		err = s.store.db.QueryRowContext(ctx, `SELECT id FROM music_quality_profiles WHERE name=? AND id<>?`, clean.Name, in.ID).Scan(&taken)
	} else {
		err = s.store.db.QueryRowContext(ctx, `SELECT id FROM music_quality_profiles WHERE name=?`, clean.Name).Scan(&taken)
	}
	if err == nil {
		return QualityProfile{}, ErrProfileNameTaken
	}
	if !errors.Is(err, sql.ErrNoRows) {
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
	// Custom profiles get a stable generated key so they are selectable
	// everywhere a profile key is used (download override, optimization,
	// export). The key is derived from the row id and never changes.
	key := fmt.Sprintf("custom-%d", in.ID)
	if _, err := s.store.db.ExecContext(ctx, `UPDATE music_quality_profiles SET key=? WHERE id=? AND custom=1`, key, in.ID); err != nil {
		return QualityProfile{}, err
	}
	return s.customProfile(ctx, in.ID)
}

// DeleteCustomProfile removes a custom profile row and clears references to
// it: the global download-profile preference and any album override are reset
// to inheritance in the same transaction, so a deleted profile can never
// leave a dangling key that silently disables conversion for later downloads.
// Builtin profiles are code and are never stored in the database.
func (s *Service) DeleteCustomProfile(ctx context.Context, id int64) error {
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var profile QualityProfile
	if err := tx.QueryRowContext(ctx, `SELECT id, name, key FROM music_quality_profiles WHERE id=? AND custom=1`, id).
		Scan(&profile.ID, &profile.Name, &profile.Key); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrInvalidInput
		}
		return err
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM music_quality_profiles WHERE id=? AND custom=1`, id)
	if err != nil {
		return err
	}
	if affected, err := result.RowsAffected(); err != nil {
		return err
	} else if affected != 1 {
		return ErrInvalidInput
	}
	if _, err := tx.ExecContext(ctx, `UPDATE music_preferences SET download_profile='', revision=revision+1, updated_at=unixepoch()
		WHERE id=1 AND (download_profile=? OR download_profile=?)`, profile.Key, profile.Name); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE music_albums SET download_profile='', revision=revision+1, updated_at=unixepoch()
		WHERE download_profile IN (?,?)`, profile.Key, profile.Name); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) customProfile(ctx context.Context, id int64) (QualityProfile, error) {
	var profile QualityProfile
	if err := s.store.db.QueryRowContext(ctx, `SELECT id, name, key, codec, container, bitrate_kbps, vbr, compression_level, require_lossless, keep_original, custom FROM music_quality_profiles WHERE id=?`, id).
		Scan(&profile.ID, &profile.Name, &profile.Key, &profile.Codec, &profile.Container, &profile.BitrateKbps, &profile.VBR, &profile.CompressionLevel, &profile.RequireLossless, &profile.KeepOriginal, &profile.Custom); err != nil {
		return QualityProfile{}, err
	}
	return profile, nil
}

// customKeyNamespaceCollision reports whether name sits inside the generated
// "custom-<id>" key namespace (a numeric suffix after the dash). Those names
// are reserved so a hand-written name can never be ambiguous with a
// generated key in ResolveProfileKey.
func customKeyNamespaceCollision(name string) bool {
	suffix, ok := strings.CutPrefix(name, "custom-")
	if !ok || suffix == "" {
		return false
	}
	for _, char := range suffix {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

// ErrProfileNameTaken is returned when a custom profile is saved with a name
// another profile row already uses; the name column is UNIQUE.
var ErrProfileNameTaken = &Error{Code: "music_profile_name_taken", Message: "A custom profile with this name already exists."}

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
	// A custom name must never shadow a builtin key or collide with the
	// generated "custom-<id>" key namespace: ResolveProfileKey checks builtins
	// first, so a custom profile named "original" could never be resolved by
	// name, and a numeric custom-<id> name would be ambiguous with another
	// row's generated key. Rows saved before this check are unaffected; the
	// validation only gates new saves and updates.
	if _, ok := ProfileByKey(clean.Name); ok {
		return QualityProfile{}, ErrInvalidInput
	}
	// The name column compares case-insensitively (COLLATE NOCASE), so a name
	// that differs only in case from a builtin key is rejected for the same
	// reason.
	for _, builtin := range BuiltinProfiles() {
		if strings.EqualFold(clean.Name, builtin.Key) {
			return QualityProfile{}, ErrInvalidInput
		}
	}
	if customKeyNamespaceCollision(clean.Name) {
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

// resolveStoredDownloadProfile normalizes a saved download profile value.
// Empty stays empty (follow the global default), builtin keys are stored
// verbatim, and a custom profile is resolved through the database so the
// stored value is the stable custom-<id> key the UI sends. Unresolvable
// values are ErrInvalidInput.
func (s *Service) resolveStoredDownloadProfile(ctx context.Context, value string) (string, error) {
	key := strings.TrimSpace(value)
	if key == "" {
		return "", nil
	}
	if _, ok := ProfileByKey(key); ok {
		return key, nil
	}
	profile, err := s.ResolveProfileKey(ctx, key)
	if err != nil {
		return "", err
	}
	if profile.Key != "" {
		return profile.Key, nil
	}
	return profile.Name, nil
}
