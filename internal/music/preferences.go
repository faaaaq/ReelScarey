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
// searched. PreferredFormats is empty when any format is acceptable. The
// transcoding fields are described in the music quality profiles contract.
type MusicPreferences struct {
	AllowLossless      bool     `json:"allow_lossless"`
	MinBitrate         int      `json:"min_bitrate"`
	PreferredFormats   []string `json:"preferred_formats"`
	DownloadProfile    string   `json:"download_profile"`
	StreamingQuality   string   `json:"streaming_quality"`
	OriginalRetention  string   `json:"original_retention"`
	FallbackPolicy     string   `json:"fallback_policy"`
	TranscodingEnabled bool     `json:"transcoding_enabled"`
	CacheLimitMB       int      `json:"cache_limit_mb"`
	MaxConversions     int      `json:"max_conversions"`
	Revision           int      `json:"revision"`
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
	err := s.store.db.QueryRowContext(ctx, `SELECT allow_lossless, min_bitrate, preferred_formats, download_profile, streaming_quality, original_retention, fallback_policy, transcoding_enabled, cache_limit_mb, max_conversions, revision FROM music_preferences WHERE id=1`).
		Scan(&prefs.AllowLossless, &prefs.MinBitrate, &formats, &prefs.DownloadProfile, &prefs.StreamingQuality, &prefs.OriginalRetention, &prefs.FallbackPolicy, &prefs.TranscodingEnabled, &prefs.CacheLimitMB, &prefs.MaxConversions, &prefs.Revision)
	if err != nil {
		return MusicPreferences{}, err
	}
	prefs.PreferredFormats = splitFormats(formats)
	return prefs, nil
}

// SavePreferences validates a reviewed preference set, compares its revision
// inside a write transaction and increments the stored revision. Empty enum
// fields are normalized to the migration defaults so the stored values are
// always one of the accepted options.
func (s *Service) SavePreferences(ctx context.Context, in MusicPreferences) (MusicPreferences, error) {
	formats, err := normalizeFormats(in.PreferredFormats)
	if err != nil {
		return MusicPreferences{}, err
	}
	if in.MinBitrate < 0 || in.MinBitrate > 320 {
		return MusicPreferences{}, ErrInvalidInput
	}
	if err := validatePreferenceKey(in.DownloadProfile); err != nil {
		return MusicPreferences{}, err
	}
	if in.StreamingQuality == "" {
		in.StreamingQuality = defaultStreamingQuality
	}
	if err := validateStreamingQuality(in.StreamingQuality); err != nil {
		return MusicPreferences{}, err
	}
	if in.OriginalRetention == "" {
		in.OriginalRetention = defaultOriginalRetention
	}
	if err := validateEnum(in.OriginalRetention, originalRetentionValues); err != nil {
		return MusicPreferences{}, err
	}
	if in.FallbackPolicy == "" {
		in.FallbackPolicy = defaultFallbackPolicy
	}
	if err := validateEnum(in.FallbackPolicy, fallbackPolicyValues); err != nil {
		return MusicPreferences{}, err
	}
	if in.CacheLimitMB == 0 {
		in.CacheLimitMB = defaultCacheLimitMB
	}
	if in.CacheLimitMB < 64 || in.CacheLimitMB > 4096 {
		return MusicPreferences{}, ErrInvalidInput
	}
	if in.MaxConversions == 0 {
		in.MaxConversions = defaultMaxConversions
	}
	if in.MaxConversions < 1 || in.MaxConversions > 4 {
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
		SET allow_lossless=?, min_bitrate=?, preferred_formats=?, download_profile=?, streaming_quality=?, original_retention=?, fallback_policy=?, transcoding_enabled=?, cache_limit_mb=?, max_conversions=?, revision=revision+1, updated_at=unixepoch()
		WHERE id=1 AND revision=?`,
		boolInt(in.AllowLossless), in.MinBitrate, strings.Join(formats, ","), in.DownloadProfile, in.StreamingQuality, in.OriginalRetention, in.FallbackPolicy, boolInt(in.TranscodingEnabled), in.CacheLimitMB, in.MaxConversions, in.Revision)
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

// The new preference fields are enum-like; the migration defaults keep them
// non-empty so the stored values are always one of the accepted options.
var streamingQualityValues = []string{"auto", "data-saver", "balanced", "high", "premium", "original"}
var originalRetentionValues = []string{"keep", "delete", "ask"}
var fallbackPolicyValues = []string{"best", "skip", "approve"}

const (
	defaultStreamingQuality  = "balanced"
	defaultOriginalRetention = "keep"
	defaultFallbackPolicy    = "best"
	defaultCacheLimitMB      = 512
	defaultMaxConversions    = 2
)

func validateEnum(value string, allowed []string) error {
	if value == "" {
		return nil
	}
	for _, option := range allowed {
		if value == option {
			return nil
		}
	}
	return ErrInvalidInput
}

func validateStreamingQuality(value string) error {
	return validateEnum(value, streamingQualityValues)
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
