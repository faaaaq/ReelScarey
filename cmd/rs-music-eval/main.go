// Controlled music-matching evaluation harness.
//
// Runs the current (post-fix) matching pipeline — MusicBrainz recording
// search with iTunes corroboration, then the local name fallback — against a
// disposable copy of the live music database. It never touches the live
// instance, the live library files or the original DB copy.
//
// Usage: go run eval/main.go <workdir-db> <report-out.json>
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	music "github.com/faaaaq/ReelScarey/internal/music"
)

type report struct {
	Baseline          int      `json:"baseline_unresolved"`
	RecordingMatched  int      `json:"recording_matched"`
	NamesMatched      int      `json:"names_matched"`
	MBRounds          int      `json:"mb_rounds"`
	ItunesRounds      int      `json:"itunes_rounds"`
	Remaining         int      `json:"remaining_unresolved"`
	RemainingMessages []bucket `json:"remaining_by_message"`
	RemainingByArtist []bucket `json:"remaining_by_artist"`
}

type bucket struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: eval <db> <report.json>")
		os.Exit(2)
	}
	dbPath := os.Args[1]
	reportPath := "eval-report.json"
	if len(os.Args) > 2 {
		reportPath = os.Args[2]
	}
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Minute)
	defer cancel()

	uriPath := filepath.ToSlash(dbPath)
	if !strings.HasPrefix(uriPath, "/") && filepath.VolumeName(uriPath) != "" {
		uriPath = "/" + uriPath
	}
	u := url.URL{Scheme: "file", Path: strings.ReplaceAll(uriPath, "\\", "/")}
	u.RawQuery = "immutable=0"
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		panic(err)
	}
	defer db.Close()

	service := music.NewServiceWithClient(db, music.New(), ".")
	service.EnableiTunes()
	if err := service.LoadMusicBrainzBase(ctx); err != nil {
		panic(err)
	}

	const scanID = 2
	loadUnresolved := func() []music.ScanFile {
		files, err := scanUnresolved(db, scanID)
		if err != nil {
			panic(err)
		}
		return files
	}

	out := report{Baseline: len(loadUnresolved())}
	fmt.Printf("baseline unresolved: %d\n", out.Baseline)

	// Recording pass with rate-limit-aware resumption.
	for round := 0; round < 40; round++ {
		if ctx.Err() != nil {
			break
		}
		files := loadUnresolved()
		if len(files) == 0 {
			break
		}
		stats, err := service.ProviderMatchFiles(ctx, scanID, files, music.ProviderMatchRun{})
		if err != nil {
			fmt.Printf("recording pass stopped: %v\n", err)
			break
		}
		if stats.IssuedMB {
			out.MBRounds++
		}
		if stats.IssuedItun {
			out.ItunesRounds++
		}
		out.RecordingMatched += stats.Matched
		fmt.Printf("recording round %d: matched %d deferred %d\n", round+1, stats.Matched, stats.Deferred)
		if stats.Deferred == 0 {
			break
		}
		waitForMusicBrainz(ctx, service)
	}

	// Local name fallback, also resumable while MusicBrainz is cooling down.
	for round := 0; round < 5; round++ {
		files := loadUnresolved()
		if len(files) == 0 {
			break
		}
		stats, err := service.LocalNamesFallbackFiles(ctx, scanID, files, music.NamesFallbackRun{})
		if err != nil {
			fmt.Printf("names pass stopped: %v\n", err)
			break
		}
		out.NamesMatched += stats.Matched
		fmt.Printf("names round %d: matched %d skipped %d\n", round+1, stats.Matched, stats.Skipped)
		if stats.Matched == 0 {
			break
		}
		waitForMusicBrainz(ctx, service)
	}

	remaining := loadUnresolved()
	out.Remaining = len(remaining)
	out.RemainingMessages = bucketsQuery(db, `SELECT error, COUNT(*) FROM music_scan_files WHERE scan_id=? AND state='unresolved' GROUP BY error ORDER BY COUNT(*) DESC LIMIT 20`)
	out.RemainingByArtist = bucketsQuery(db, `SELECT coalesce(nullif(trim(tag_album_artist),''),tag_artist), COUNT(*) FROM music_scan_files WHERE scan_id=? AND state='unresolved' GROUP BY 1 ORDER BY 2 DESC LIMIT 25`)

	payload, _ := json.MarshalIndent(out, "", "  ")
	if err := os.WriteFile(reportPath, payload, 0o644); err != nil {
		panic(err)
	}
	fmt.Printf("report written to %s\n", reportPath)
}

func scanUnresolved(db *sql.DB, scanID int64) ([]music.ScanFile, error) {
	rows, err := db.Query(`SELECT id, scan_id, relative_path, size_bytes, modified_at, sha256, format, duration_ms, bitrate, sample_rate, channels,
		tag_artist, tag_album_artist, tag_album, tag_title, tag_track, tag_disc, tag_year,
		tag_musicbrainz_artist, tag_musicbrainz_album, tag_musicbrainz_release_group, tag_musicbrainz_track, tag_musicbrainz_recording,
		tag_json, artist_id, album_id, track_id, state, error
		FROM music_scan_files WHERE scan_id=? AND state='unresolved' ORDER BY id`, scanID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	files := []music.ScanFile{}
	for rows.Next() {
		var f music.ScanFile
		if err := rows.Scan(&f.ID, &f.ScanID, &f.RelativePath, &f.SizeBytes, &f.ModifiedAt, &f.SHA256, &f.Format, &f.DurationMS, &f.Bitrate, &f.SampleRate, &f.Channels,
			&f.Artist, &f.AlbumArtist, &f.Album, &f.Title, &f.Track, &f.Disc, &f.Year,
			&f.MusicBrainzArtistID, &f.MusicBrainzAlbumID, &f.MusicBrainzReleaseGroupID, &f.MusicBrainzTrackID, &f.MusicBrainzRecordingID,
			&f.TagJSON, &f.ArtistID, &f.AlbumID, &f.TrackID, &f.State, &f.Error); err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

func bucketsQuery(db *sql.DB, query string) []bucket {
	rows, err := db.Query(query, 2)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []bucket{}
	for rows.Next() {
		var key sql.NullString
		var count int
		if err := rows.Scan(&key, &count); err != nil {
			return out
		}
		out = append(out, bucket{Key: key.String, Count: count})
	}
	return out
}

// waitForMusicBrainz sleeps until the provider cooldown ends, bounded.
func waitForMusicBrainz(ctx context.Context, service *music.Service) {
	status, err := service.MusicBrainzStatus(ctx)
	if err != nil || status.CooldownUntil == 0 {
		time.Sleep(30 * time.Second)
		return
	}
	until := time.Unix(status.CooldownUntil, 0)
	wait := time.Until(until)
	if wait <= 0 {
		return
	}
	if wait > 20*time.Minute {
		wait = 20 * time.Minute
	}
	fmt.Printf("MusicBrainz cooling down until %s; waiting %s\n", until.Format(time.RFC3339), wait.Truncate(time.Second))
	select {
	case <-ctx.Done():
	case <-time.After(wait + time.Second):
	}
}
