// Package nfo renders bounded Jellyfin-compatible movie and episode NFO files.
//
// The package performs no library integration. Callers pass explicit facts;
// nothing is looked up, guessed or copied from a probe. The only filesystem
// helper, WriteFileAtomic, publishes a new file and refuses to replace one.
package nfo

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxTitleRunes = 500
	maxPlotBytes  = 16 << 10
	// MaxFileBytes bounds one rendered document.
	MaxFileBytes = 64 << 10
)

var (
	// ErrInvalid reports missing or out-of-range details.
	ErrInvalid = errors.New("invalid NFO details")
	// ErrExists reports a destination that already exists.
	ErrExists = errors.New("NFO destination already exists")
)

// Movie contains the explicit facts written to a movie NFO. Plot and Premiered
// may be empty; Year and TMDbID may be zero when unknown.
type Movie struct {
	Title     string
	Year      int
	TMDbID    int64
	Plot      string
	Premiered string
}

// Episode contains the explicit facts written to an episode NFO. TMDbID is the
// episode's TMDb ID, not the series ID. Season zero is allowed for specials.
type Episode struct {
	Title     string
	Year      int
	TMDbID    int64
	Plot      string
	Premiered string
	Season    int
	Episode   int
}

type details struct {
	title     string
	year      int
	tmdb      int64
	plot      string
	premiered string
	season    int
	episode   int
}

const xmlDeclaration = "<?xml version=\"1.0\" encoding=\"UTF-8\" standalone=\"yes\"?>\n"

// RenderMovie returns a complete Jellyfin-compatible movie NFO document.
func RenderMovie(movie Movie) ([]byte, error) {
	normalized, err := validate(movie.Title, movie.Year, movie.TMDbID, movie.Plot, movie.Premiered)
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	b.WriteString(xmlDeclaration)
	b.WriteString("<movie>\n")
	writeElement(&b, "title", normalized.title)
	writeYear(&b, normalized.year)
	writePlot(&b, normalized.plot)
	writePremiered(&b, normalized.premiered)
	writeUniqueID(&b, normalized.tmdb)
	b.WriteString("</movie>\n")
	return b.Bytes(), nil
}

// RenderEpisode returns a complete Jellyfin-compatible episode NFO document.
func RenderEpisode(episode Episode) ([]byte, error) {
	if episode.Season < 0 || episode.Season > 999 {
		return nil, fmt.Errorf("%w: season must be between 0 and 999", ErrInvalid)
	}
	if episode.Episode < 1 || episode.Episode > 9999 {
		return nil, fmt.Errorf("%w: episode must be between 1 and 9999", ErrInvalid)
	}
	normalized, err := validate(episode.Title, episode.Year, episode.TMDbID, episode.Plot, episode.Premiered)
	if err != nil {
		return nil, err
	}
	normalized.season, normalized.episode = episode.Season, episode.Episode
	var b bytes.Buffer
	b.WriteString(xmlDeclaration)
	b.WriteString("<episodedetails>\n")
	writeElement(&b, "title", normalized.title)
	writeYear(&b, normalized.year)
	fmt.Fprintf(&b, "<season>%d</season>\n", normalized.season)
	fmt.Fprintf(&b, "<episode>%d</episode>\n", normalized.episode)
	writePlot(&b, normalized.plot)
	writePremiered(&b, normalized.premiered)
	writeUniqueID(&b, normalized.tmdb)
	b.WriteString("</episodedetails>\n")
	return b.Bytes(), nil
}

func validate(title string, year int, tmdb int64, plot, premiered string) (details, error) {
	out := details{
		title:     strings.TrimSpace(title),
		year:      year,
		tmdb:      tmdb,
		plot:      plot,
		premiered: strings.TrimSpace(premiered),
	}
	if out.title == "" || !utf8.ValidString(out.title) || utf8.RuneCountInString(out.title) > maxTitleRunes {
		return details{}, fmt.Errorf("%w: title is required, must be valid UTF-8 and at most %d characters", ErrInvalid, maxTitleRunes)
	}
	if year != 0 && (year < 1870 || year > 2200) {
		return details{}, fmt.Errorf("%w: year must be empty or between 1870 and 2200", ErrInvalid)
	}
	if tmdb < 0 || tmdb > 999_999_999 {
		return details{}, fmt.Errorf("%w: TMDb ID is out of range", ErrInvalid)
	}
	if !utf8.ValidString(out.plot) || len(out.plot) > maxPlotBytes {
		return details{}, fmt.Errorf("%w: plot must be valid UTF-8 and at most %d bytes", ErrInvalid, maxPlotBytes)
	}
	if out.premiered != "" {
		parsed, err := time.Parse(time.DateOnly, out.premiered)
		if err != nil || parsed.Format(time.DateOnly) != out.premiered {
			return details{}, fmt.Errorf("%w: premiered must be YYYY-MM-DD", ErrInvalid)
		}
	}
	return out, nil
}

func writeElement(b *bytes.Buffer, name, value string) {
	b.WriteByte('<')
	b.WriteString(name)
	b.WriteByte('>')
	// EscapeText only fails when its writer fails; bytes.Buffer cannot fail.
	_ = xml.EscapeText(b, []byte(value))
	b.WriteString("</")
	b.WriteString(name)
	b.WriteString(">\n")
}

func writeYear(b *bytes.Buffer, year int) {
	if year > 0 {
		fmt.Fprintf(b, "<year>%d</year>\n", year)
	}
}

func writePlot(b *bytes.Buffer, plot string) {
	if plot != "" {
		writeElement(b, "plot", plot)
	}
}

func writePremiered(b *bytes.Buffer, premiered string) {
	if premiered != "" {
		writeElement(b, "premiered", premiered)
	}
}

func writeUniqueID(b *bytes.Buffer, tmdb int64) {
	if tmdb > 0 {
		fmt.Fprintf(b, "<uniqueid type=\"tmdb\" default=\"true\">%d</uniqueid>\n", tmdb)
	}
}

// WriteFileAtomic writes a new file below an existing directory. The document
// is staged in a temporary file in the same directory and published without
// replacement, so a partial or pre-existing NFO is never visible. Existing
// destinations return ErrExists. A zero perm means 0644.
func WriteFileAtomic(name string, data []byte, perm fs.FileMode) error {
	if name == "" || len(data) > MaxFileBytes {
		return ErrInvalid
	}
	base := filepath.Base(name)
	if base == "" || base == "." || base == string(filepath.Separator) {
		return ErrInvalid
	}
	dir := filepath.Dir(name)
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return ErrInvalid
	}
	if _, err = os.Lstat(name); err == nil {
		return ErrExists
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if perm == 0 {
		perm = 0o644
	}
	temp, err := os.CreateTemp(dir, ".reelscarey-nfo-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err = temp.Chmod(perm.Perm()); err != nil {
		temp.Close()
		return err
	}
	if _, err = temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err = temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err = temp.Close(); err != nil {
		return err
	}
	// A same-directory hard link publishes the complete file atomically and
	// fails when the destination exists, unlike os.Rename which replaces.
	if err = os.Link(tempName, name); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return ErrExists
		}
		return err
	}
	return os.Remove(tempName)
}
