package settings

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

var ErrConflict = errors.New("settings changed; reload and try again")

type Settings struct {
	LibraryName   string `json:"library_name"`
	MovieFolder   string `json:"movie_folder"`
	SeriesFolder  string `json:"series_folder"`
	EpisodeNaming string `json:"episode_naming"`
	// RegisterUnverifiedVideo permits existing-library registration when the
	// video cannot be inspected. Registered files carry no resolution facts
	// and are labeled as unverified.
	RegisterUnverifiedVideo bool `json:"register_unverified_video"`
	// ImportMode selects whether completed downloads are hardlinked into the
	// library when possible or always copied. Publication inside the library
	// still uses a private no-overwrite hardlink.
	ImportMode string `json:"import_mode"`
	// NFOExport writes Jellyfin-compatible .nfo metadata beside imported media.
	NFOExport bool `json:"nfo_export"`
	// ArtworkExport writes a missing movie poster.jpg beside registered movies.
	ArtworkExport bool   `json:"artwork_export"`
	Revision      int    `json:"revision"`
	UpdatedAt     string `json:"updated_at"`
}

type Update struct {
	LibraryName   string `json:"library_name"`
	MovieFolder   string `json:"movie_folder"`
	SeriesFolder  string `json:"series_folder"`
	EpisodeNaming string `json:"episode_naming"`
	// Pointer so a missing field keeps the saved preference unchanged.
	RegisterUnverifiedVideo *bool `json:"register_unverified_video,omitempty"`
	// Pointer so older callers that omit the field keep the saved method.
	ImportMode *string `json:"import_mode,omitempty"`
	// Pointer so older callers keep the saved NFO preference.
	NFOExport *bool `json:"nfo_export,omitempty"`
	// Pointer so older callers keep the saved artwork preference.
	ArtworkExport *bool `json:"artwork_export,omitempty"`
	Revision      int   `json:"revision"`
}

type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

var folderPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 _-]{0,79}$`)
var reservedPattern = regexp.MustCompile(`(?i)^(CON|PRN|AUX|NUL|COM[0-9]|LPT[0-9])$`)

func (u *Update) Validate() error {
	u.LibraryName = strings.TrimSpace(u.LibraryName)
	u.MovieFolder = strings.TrimSpace(u.MovieFolder)
	u.SeriesFolder = strings.TrimSpace(u.SeriesFolder)
	if !utf8.ValidString(u.LibraryName) || utf8.RuneCountInString(u.LibraryName) < 1 || utf8.RuneCountInString(u.LibraryName) > 80 || strings.IndexFunc(u.LibraryName, unicode.IsControl) >= 0 {
		return &ValidationError{"Library name must contain 1–80 characters without control characters."}
	}
	for _, field := range []struct{ name, value string }{{"Movie folder", u.MovieFolder}, {"TV folder", u.SeriesFolder}} {
		if !folderPattern.MatchString(field.value) || reservedPattern.MatchString(field.value) {
			return &ValidationError{fmt.Sprintf("%s must start with a letter or number, use only letters, numbers, spaces, hyphens or underscores (up to 80 characters), and not be a reserved device name.", field.name)}
		}
	}
	if strings.EqualFold(u.MovieFolder, u.SeriesFolder) {
		return &ValidationError{"Movie and TV folders must be different."}
	}
	if u.Revision < 1 {
		return &ValidationError{"A valid settings revision is required. Reload and try again."}
	}
	if u.EpisodeNaming != "" && u.EpisodeNaming != "standard" && u.EpisodeNaming != "with-title" {
		return &ValidationError{"Choose standard episode names or names with episode titles."}
	}
	if u.ImportMode != nil {
		*u.ImportMode = strings.TrimSpace(*u.ImportMode)
		if err := ValidateImportMode(*u.ImportMode); err != nil {
			return err
		}
	}
	return nil
}

// ValidateImportMode accepts the supported download-to-library import methods.
// Empty is not a stored method; callers that omit the existing setting should
// leave the pointer nil instead.
func ValidateImportMode(mode string) error {
	if mode != "hardlink" && mode != "copy" {
		return &ValidationError{"Choose hardlink imports when possible or always copy."}
	}
	return nil
}

// SetImportModeTx persists the first-run import method inside the administrator
// setup transaction. The settings row is created by the foundation migration.
func SetImportModeTx(ctx context.Context, tx *sql.Tx, mode string) error {
	if err := ValidateImportMode(mode); err != nil {
		return err
	}
	var current string
	if err := tx.QueryRowContext(ctx, `SELECT import_mode FROM settings WHERE id=1`).Scan(&current); err != nil {
		return err
	}
	if current == mode {
		return nil
	}
	result, err := tx.ExecContext(ctx, `UPDATE settings SET import_mode=?, revision=revision+1, updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id=1`, mode)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return errors.New("the settings row is missing")
	}
	return nil
}

type Store struct{ db *sql.DB }

func New(db *sql.DB) *Store { return &Store{db: db} }

func (s *Store) Get(ctx context.Context) (Settings, error) {
	var out Settings
	err := s.db.QueryRowContext(ctx, "SELECT library_name, movie_folder, series_folder, episode_naming, register_unverified_video, import_mode, nfo_export, artwork_export, revision, updated_at FROM settings WHERE id = 1").Scan(&out.LibraryName, &out.MovieFolder, &out.SeriesFolder, &out.EpisodeNaming, &out.RegisterUnverifiedVideo, &out.ImportMode, &out.NFOExport, &out.ArtworkExport, &out.Revision, &out.UpdatedAt)
	return out, err
}

func (s *Store) Update(ctx context.Context, in Update) (Settings, error) {
	if err := in.Validate(); err != nil {
		return Settings{}, err
	}
	var out Settings
	err := s.db.QueryRowContext(ctx, `UPDATE settings SET library_name=?,movie_folder=?,series_folder=?,episode_naming=CASE WHEN ?='' THEN episode_naming ELSE ? END,register_unverified_video=COALESCE(?,register_unverified_video),import_mode=COALESCE(?,import_mode),nfo_export=COALESCE(?,nfo_export),artwork_export=COALESCE(?,artwork_export),revision=revision+1,updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id=1 AND revision=? RETURNING library_name,movie_folder,series_folder,episode_naming,register_unverified_video,import_mode,nfo_export,artwork_export,revision,updated_at`, in.LibraryName, in.MovieFolder, in.SeriesFolder, in.EpisodeNaming, in.EpisodeNaming, in.RegisterUnverifiedVideo, in.ImportMode, in.NFOExport, in.ArtworkExport, in.Revision).Scan(&out.LibraryName, &out.MovieFolder, &out.SeriesFolder, &out.EpisodeNaming, &out.RegisterUnverifiedVideo, &out.ImportMode, &out.NFOExport, &out.ArtworkExport, &out.Revision, &out.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Settings{}, ErrConflict
	}
	return out, err
}
