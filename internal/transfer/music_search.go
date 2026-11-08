package transfer

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// MusicRelease is a redacted music search result. Release.ID remains the
// indexer's release ID; the candidate row is keyed by album and release.
type MusicRelease struct {
	Release
	ParsedArtist string `json:"parsed_artist"`
	ParsedAlbum  string `json:"parsed_album"`
	Year         *int   `json:"year"`
	Format       string `json:"format"`
	Bitrate      int    `json:"bitrate"`
	Lossless     bool   `json:"lossless"`
}

// SearchMusicAlbum searches the configured indexers with the audio category,
// parses release names conservatively and stores sealed candidates for queueing.
func (s *Service) SearchMusicAlbum(ctx context.Context, albumID int64, query string) ([]MusicRelease, error) {
	if albumID < 1 {
		return nil, &Error{Code: "invalid_music_album", Message: "Choose a valid album."}
	}
	var artist, album string
	var year sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT ar.name,a.title,a.year FROM music_albums a JOIN music_artists ar ON ar.id=a.artist_id WHERE a.id=?`, albumID).Scan(&artist, &album, &year); err != nil {
		return nil, err
	}
	query = strings.TrimSpace(query)
	if query == "" {
		query = strings.TrimSpace(artist + " " + album)
	}
	if !utf8.ValidString(query) || utf8.RuneCountInString(query) < 1 || utf8.RuneCountInString(query) > 200 {
		return nil, &Error{Code: "invalid_music_query", Message: "Enter 1–200 characters."}
	}
	candidates, err := s.searchIndexers(ctx, query, "3000")
	if err != nil {
		return nil, err
	}
	byID := map[string]Candidate{}
	for _, candidate := range candidates {
		byID[candidate.ID] = candidate
	}
	type scored struct {
		release MusicRelease
		score   int
	}
	ranked := []scored{}
	for _, candidate := range candidates {
		parsed := ParseMusicRelease(candidate.Title)
		if !musicIdentityMatch(parsed, candidate.Title, artist, album) {
			continue
		}
		score := 0
		if musicSame(parsed.Album, album) || strings.Contains(strings.ToLower(candidate.Title), strings.ToLower(album)) {
			score += 4
		}
		if musicSame(parsed.Artist, artist) || strings.Contains(strings.ToLower(candidate.Title), strings.ToLower(artist)) {
			score += 2
		}
		if parsed.Lossless {
			score += 3
		}
		switch parsed.Format {
		case "FLAC":
			score += 3
		case "ALAC":
			score += 2
		case "MP3":
			score++
		}
		if candidate.Seeders != nil {
			score += min(5, *candidate.Seeders/50)
		}
		ranked = append(ranked, scored{MusicRelease{
			Release:      candidate.Release,
			ParsedArtist: parsed.Artist,
			ParsedAlbum:  parsed.Album,
			Year:         parsed.Year,
			Format:       parsed.Format,
			Bitrate:      parsed.Bitrate,
			Lossless:     parsed.Lossless,
		}, score})
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		return ranked[i].release.Title < ranked[j].release.Title
	})
	now := time.Now().Unix()
	out := []MusicRelease{}
	for _, item := range ranked {
		if len(out) >= 50 {
			break
		}
		candidate, ok := byID[item.release.ID]
		if !ok {
			continue
		}
		payload, err := json.Marshal(candidate)
		if err != nil {
			return nil, err
		}
		sealed, err := s.secrets.Seal("music-release", payload)
		if err != nil {
			return nil, err
		}
		var parsedYear any
		if item.release.Year != nil {
			parsedYear = *item.release.Year
		}
		if _, err := s.db.ExecContext(ctx, `INSERT INTO music_release_candidates(id,album_id,title,size_bytes,seeders,peers,published_at,info_url,parsed_artist,parsed_album,parsed_year,format,bitrate,lossless,payload,expires_at,created_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(id) DO UPDATE SET album_id=excluded.album_id,title=excluded.title,size_bytes=excluded.size_bytes,seeders=excluded.seeders,peers=excluded.peers,published_at=excluded.published_at,info_url=excluded.info_url,parsed_artist=excluded.parsed_artist,parsed_album=excluded.parsed_album,parsed_year=excluded.parsed_year,format=excluded.format,bitrate=excluded.bitrate,lossless=excluded.lossless,payload=excluded.payload,expires_at=excluded.expires_at,created_at=excluded.created_at`,
			fmt.Sprintf("%d:%s", albumID, item.release.ID), albumID, item.release.Title, item.release.Size,
			item.release.Seeders, item.release.Peers, item.release.Published, item.release.InfoURL,
			item.release.ParsedArtist, item.release.ParsedAlbum, parsedYear, item.release.Format, item.release.Bitrate, boolInt(item.release.Lossless),
			sealed, now+30*60, now); err != nil {
			return nil, err
		}
		out = append(out, item.release)
	}
	return out, nil
}

func musicIdentityMatch(parsed ParsedMusicRelease, title, artist, album string) bool {
	if album == "" {
		return false
	}
	if musicSame(parsed.Album, album) {
		return true
	}
	lowerTitle := strings.ToLower(title)
	if !strings.Contains(lowerTitle, strings.ToLower(album)) {
		return false
	}
	if parsed.Artist == "" {
		return strings.Contains(lowerTitle, strings.ToLower(artist))
	}
	return musicSame(parsed.Artist, artist) || strings.Contains(lowerTitle, strings.ToLower(artist))
}

func musicSame(a, b string) bool {
	na, nb := musicNormalize(a), musicNormalize(b)
	return na != "" && na == nb
}

func musicNormalize(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return -1
	}, value)
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
