// Package music owns MusicBrainz normalization, the local music catalog,
// existing-file scanning, reviewed catalog discovery, reviewed organization and
// tag writing.
package music

import (
	"errors"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Error struct {
	Code       string
	Message    string
	RetryAfter int
}

func (e *Error) Error() string { return e.Message }

var ErrNotFound = errors.New("The requested music item is not in the catalog.")

var mbidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func ValidMBID(value string) bool { return mbidPattern.MatchString(value) }

func ValidQuery(query string, page int) bool {
	return utf8.ValidString(query) && utf8.RuneCountInString(query) >= 1 &&
		utf8.RuneCountInString(query) <= 200 && !strings.ContainsFunc(query, unicode.IsControl) &&
		page >= 1 && page <= 500
}

func ValidListQuery(query string, page int) bool {
	return utf8.ValidString(query) && utf8.RuneCountInString(query) <= 200 &&
		!strings.ContainsFunc(query, unicode.IsControl) && page >= 1 && page <= 100000
}

// Artist is a MusicBrainz artist as stored in the local catalog. ID is zero
// before the artist is added.
type Artist struct {
	ID             int64  `json:"id,omitempty"`
	MusicBrainzID  string `json:"musicbrainz_id"`
	Name           string `json:"name"`
	SortName       string `json:"sort_name"`
	Disambiguation string `json:"disambiguation"`
	Country        string `json:"country"`
	Type           string `json:"type"`
	Overview       string `json:"overview"`
	ImageURL       string `json:"image_url"`
	Monitored      bool   `json:"monitored"`
	Status         string `json:"status"`
	AlbumCount     int    `json:"album_count"`
	InLibrary      bool   `json:"in_library,omitempty"`
}

type Album struct {
	ID             int64    `json:"id,omitempty"`
	ArtistID       int64    `json:"artist_id,omitempty"`
	ArtistName     string   `json:"artist_name,omitempty"`
	MusicBrainzID  string   `json:"musicbrainz_id"`
	Title          string   `json:"title"`
	PrimaryType    string   `json:"primary_type"`
	SecondaryTypes []string `json:"secondary_types"`
	Year           *int     `json:"year"`
	ReleaseDate    string   `json:"release_date"`
	CoverURL       string   `json:"cover_url"`
	Monitored      bool     `json:"monitored"`
	Status         string   `json:"status"`
	TrackCount     int      `json:"track_count"`
}

type Track struct {
	ID            int64  `json:"id,omitempty"`
	AlbumID       int64  `json:"album_id,omitempty"`
	MusicBrainzID string `json:"musicbrainz_id"`
	RecordingID   string `json:"recording_id"`
	Disc          int    `json:"disc"`
	Number        int    `json:"number"`
	Title         string `json:"title"`
	DurationMS    int    `json:"duration_ms"`
}

type SearchResults struct {
	Page         int      `json:"page"`
	TotalPages   int      `json:"total_pages"`
	TotalResults int      `json:"total_results"`
	Items        []Artist `json:"items"`
}

type ArtistPage struct {
	Items []Artist `json:"items"`
	Page  int      `json:"page"`
	Total int      `json:"total"`
	Limit int      `json:"limit"`
}

type ArtistDetails struct {
	Artist Artist  `json:"artist"`
	Albums []Album `json:"albums"`
}

type MusicFile struct {
	ID           int64  `json:"id"`
	ArtistID     int64  `json:"artist_id"`
	AlbumID      int64  `json:"album_id"`
	TrackID      *int64 `json:"track_id"`
	ArtistName   string `json:"artist_name"`
	AlbumTitle   string `json:"album_title"`
	TrackTitle   string `json:"track_title"`
	Disc         int    `json:"disc"`
	Number       int    `json:"number"`
	RelativePath string `json:"relative_path"`
	Format       string `json:"format"`
	DurationMS   int    `json:"duration_ms"`
	SizeBytes    int64  `json:"size_bytes"`
}

type MusicFilePage struct {
	Items []MusicFile `json:"items"`
	Page  int         `json:"page"`
	Total int         `json:"total"`
	Limit int         `json:"limit"`
}

type AlbumDetails struct {
	Album  Album   `json:"album"`
	Tracks []Track `json:"tracks"`
}

func coverURL(releaseGroupMBID string) string {
	if !ValidMBID(releaseGroupMBID) {
		return ""
	}
	return "https://coverartarchive.org/release-group/" + releaseGroupMBID + "/front-500"
}

func cleanText(value string, maximum int) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" || utf8.RuneCountInString(value) > maximum || strings.ContainsFunc(value, unicode.IsControl) {
		return "", false
	}
	return value, true
}
