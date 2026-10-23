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
	Revision      int    `json:"revision"`
	UpdatedAt     string `json:"updated_at"`
}

type Update struct {
	LibraryName   string `json:"library_name"`
	MovieFolder   string `json:"movie_folder"`
	SeriesFolder  string `json:"series_folder"`
	EpisodeNaming string `json:"episode_naming"`
	Revision      int    `json:"revision"`
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
	return nil
}

type Store struct{ db *sql.DB }

func New(db *sql.DB) *Store { return &Store{db: db} }

func (s *Store) Get(ctx context.Context) (Settings, error) {
	var out Settings
	err := s.db.QueryRowContext(ctx, "SELECT library_name, movie_folder, series_folder, episode_naming,revision, updated_at FROM settings WHERE id = 1").Scan(&out.LibraryName, &out.MovieFolder, &out.SeriesFolder, &out.EpisodeNaming, &out.Revision, &out.UpdatedAt)
	return out, err
}

func (s *Store) Update(ctx context.Context, in Update) (Settings, error) {
	if err := in.Validate(); err != nil {
		return Settings{}, err
	}
	var out Settings
	err := s.db.QueryRowContext(ctx, `UPDATE settings SET library_name=?,movie_folder=?,series_folder=?,episode_naming=CASE WHEN ?='' THEN episode_naming ELSE ? END,revision=revision+1,updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id=1 AND revision=? RETURNING library_name,movie_folder,series_folder,episode_naming,revision,updated_at`, in.LibraryName, in.MovieFolder, in.SeriesFolder, in.EpisodeNaming, in.EpisodeNaming, in.Revision).Scan(&out.LibraryName, &out.MovieFolder, &out.SeriesFolder, &out.EpisodeNaming, &out.Revision, &out.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Settings{}, ErrConflict
	}
	return out, err
}
