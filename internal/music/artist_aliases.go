package music

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// ArtistAliasMerge records one consolidated reviewed alias group. MergedIDs
// are the removed duplicate rows; SurvivorID remains stable for existing links.
type ArtistAliasMerge struct {
	SurvivorID int64
	MergedIDs  []int64
	Name       string
}

// ArtistAliasSkip explains why a reviewed alias group was left unchanged.
type ArtistAliasSkip struct {
	ArtistIDs []int64
	Name      string
	Reason    string
}

// ArtistAliasReport summarizes startup consolidation without hiding skipped
// duplicates. No report entry implies a media-file write; only SQLite rows change.
type ArtistAliasReport struct {
	Merged  []ArtistAliasMerge
	Skipped []ArtistAliasSkip
}

// ConsolidateArtistAliases combines duplicates only within explicitly reviewed
// alias groups. Production calls this before workers resume, so active-work
// checks are defensive rather than a replacement for startup serialization.
func (s *Service) ConsolidateArtistAliases(ctx context.Context) (ArtistAliasReport, error) {
	return s.store.consolidateArtistAliases(ctx)
}

type artistAliasRow struct {
	Artist
	createdAt int64
}

func (s *Store) consolidateArtistAliases(ctx context.Context) (ArtistAliasReport, error) {
	var report ArtistAliasReport
	rows, err := s.db.QueryContext(ctx, `SELECT id,musicbrainz_id,name,sort_name,disambiguation,country,artist_type,overview,image_url,monitored,status,created_at,name_overridden FROM music_artists ORDER BY id`)
	if err != nil {
		return report, err
	}
	groups := map[string][]artistAliasRow{}
	for rows.Next() {
		var row artistAliasRow
		var monitored int
		var overridden int
		if err := rows.Scan(&row.ID, &row.MusicBrainzID, &row.Name, &row.SortName, &row.Disambiguation,
			&row.Country, &row.Type, &row.Overview, &row.ImageURL, &monitored, &row.Status, &row.createdAt, &overridden); err != nil {
			rows.Close()
			return report, err
		}
		row.Monitored = monitored != 0
		row.NameOverridden = overridden != 0
		if group, ok := reviewedArtistAlias(row.Name); ok {
			groups[group.key] = append(groups[group.key], row)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return report, err
	}
	if err := rows.Close(); err != nil {
		return report, err
	}
	for _, group := range reviewedArtistAliasGroups {
		artists := groups[group.key]
		if len(artists) < 2 {
			continue
		}
		merged, skip, err := s.mergeArtistAliasGroup(ctx, artists)
		if err != nil {
			return report, err
		}
		if skip != "" {
			ids := make([]int64, 0, len(artists))
			for _, artist := range artists {
				ids = append(ids, artist.ID)
			}
			report.Skipped = append(report.Skipped, ArtistAliasSkip{ArtistIDs: ids, Name: group.displayName, Reason: skip})
			continue
		}
		report.Merged = append(report.Merged, merged)
	}
	return report, nil
}

func (s *Store) mergeArtistAliasGroup(ctx context.Context, artists []artistAliasRow) (ArtistAliasMerge, string, error) {
	// One explicitly owner-renamed row takes precedence over the default
	// lowest-ID survivor. Multiple competing owner choices stay untouched.
	overridden := -1
	for i, artist := range artists {
		if !artist.NameOverridden {
			continue
		}
		if overridden >= 0 {
			return ArtistAliasMerge{}, "multiple owner-renamed rows require manual review", nil
		}
		overridden = i
	}
	if overridden > 0 {
		artists[0], artists[overridden] = artists[overridden], artists[0]
	}
	ids := make([]int64, 0, len(artists))
	genuineMBIDs := map[string]bool{}
	for _, artist := range artists {
		ids = append(ids, artist.ID)
		if strings.TrimSpace(artist.MusicBrainzID) != "" && !IsLocalMusicID(artist.MusicBrainzID) {
			genuineMBIDs[strings.ToLower(strings.TrimSpace(artist.MusicBrainzID))] = true
		}
	}
	if len(genuineMBIDs) > 1 {
		return ArtistAliasMerge{}, "multiple different MusicBrainz artist identities require manual review", nil
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return ArtistAliasMerge{}, "", err
	}
	defer tx.Rollback()
	placeholders := artistAliasPlaceholders(len(ids))
	args := artistAliasArgs(ids)

	var active int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM music_scan_files f JOIN music_scans s ON s.id=f.scan_id
		WHERE s.state IN ('scanning','review') AND f.artist_id IN (`+placeholders+`)`, args...).Scan(&active); err != nil {
		return ArtistAliasMerge{}, "", err
	}
	if active > 0 {
		return ArtistAliasMerge{}, "an active or awaiting-review scan references these artists", nil
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM music_edit_jobs j JOIN music_edit_files e ON e.job_id=j.id
		JOIN music_files f ON f.id=e.music_file_id WHERE j.state='running' AND f.artist_id IN (`+placeholders+`)`, args...).Scan(&active); err != nil {
		return ArtistAliasMerge{}, "", err
	}
	if active > 0 {
		return ArtistAliasMerge{}, "a running tag-edit job references these artists", nil
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM music_upgrade_jobs j WHERE j.state IN ('queued','running') AND
		((j.scope='artist' AND j.scope_id IN (`+placeholders+`)) OR EXISTS(SELECT 1 FROM music_upgrade_items i
		WHERE i.job_id=j.id AND i.artist_id IN (`+placeholders+`)))`, append(append([]any{}, args...), args...)...).Scan(&active); err != nil {
		return ArtistAliasMerge{}, "", err
	}
	if active > 0 {
		return ArtistAliasMerge{}, "a queued or running source-upgrade job references these artists", nil
	}

	albumRows, err := tx.QueryContext(ctx, `SELECT artist_id,musicbrainz_id,title FROM music_albums WHERE artist_id IN (`+placeholders+`) ORDER BY id`, args...)
	if err != nil {
		return ArtistAliasMerge{}, "", err
	}
	albumTitles := map[string]int64{}
	albumMBIDs := map[string]int64{}
	for albumRows.Next() {
		var artistID int64
		var mbid, title string
		if err := albumRows.Scan(&artistID, &mbid, &title); err != nil {
			albumRows.Close()
			return ArtistAliasMerge{}, "", err
		}
		key := compactMatchName(title)
		if key != "" {
			if previousArtist, exists := albumTitles[key]; exists && previousArtist != artistID {
				albumRows.Close()
				return ArtistAliasMerge{}, fmt.Sprintf("overlapping album title %q requires manual album review", title), nil
			}
			albumTitles[key] = artistID
		}
		if strings.TrimSpace(mbid) != "" {
			mbid = strings.ToLower(strings.TrimSpace(mbid))
			if previousArtist, exists := albumMBIDs[mbid]; exists && previousArtist != artistID {
				albumRows.Close()
				return ArtistAliasMerge{}, "overlapping album identities require manual album review", nil
			}
			albumMBIDs[mbid] = artistID
		}
	}
	if err := albumRows.Err(); err != nil {
		albumRows.Close()
		return ArtistAliasMerge{}, "", err
	}
	if err := albumRows.Close(); err != nil {
		return ArtistAliasMerge{}, "", err
	}

	survivor := artists[0]
	originalMBID := survivor.MusicBrainzID
	adoptedMBID := originalMBID
	if IsLocalMusicID(adoptedMBID) {
		for _, artist := range artists[1:] {
			if !IsLocalMusicID(artist.MusicBrainzID) {
				adoptedMBID = artist.MusicBrainzID
				break
			}
		}
	}
	for _, loser := range artists[1:] {
		survivor.Monitored = survivor.Monitored || loser.Monitored
		if loser.createdAt < survivor.createdAt {
			survivor.createdAt = loser.createdAt
		}
		mergeArtistAliasMetadata(&survivor, loser)
		if _, err := tx.ExecContext(ctx, `UPDATE music_albums SET artist_id=?,updated_at=unixepoch() WHERE artist_id=?`, survivor.ID, loser.ID); err != nil {
			return ArtistAliasMerge{}, "", err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE music_files SET artist_id=? WHERE artist_id=?`, survivor.ID, loser.ID); err != nil {
			return ArtistAliasMerge{}, "", err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE music_scan_files SET artist_id=? WHERE artist_id=?`, survivor.ID, loser.ID); err != nil {
			return ArtistAliasMerge{}, "", err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE music_upgrade_items SET artist_id=? WHERE artist_id=?`, survivor.ID, loser.ID); err != nil {
			return ArtistAliasMerge{}, "", err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE music_upgrade_jobs SET scope_id=?,updated_at=unixepoch() WHERE scope='artist' AND scope_id=?`, survivor.ID, loser.ID); err != nil {
			return ArtistAliasMerge{}, "", err
		}
		if err := mergeArtistPlaybackMarks(ctx, tx, survivor.ID, loser.ID); err != nil {
			return ArtistAliasMerge{}, "", err
		}
	}

	// Keep the survivor's display and sort names unchanged. Apart from
	// respecting owner choice, this avoids enqueueing automatic tag rewrites
	// through the music_artist_metadata_refreshed trigger.
	if _, err := tx.ExecContext(ctx, `UPDATE music_artists SET name=?,sort_name=?,disambiguation=?,country=?,artist_type=?,overview=?,image_url=?,monitored=?,
		status=CASE WHEN EXISTS(SELECT 1 FROM music_files WHERE artist_id=?) THEN 'available' ELSE 'missing' END,created_at=?,updated_at=unixepoch() WHERE id=?`,
		survivor.Name, survivor.SortName, survivor.Disambiguation, survivor.Country, survivor.Type, survivor.Overview,
		survivor.ImageURL, boolInt(survivor.Monitored), survivor.ID, survivor.createdAt, survivor.ID); err != nil {
		return ArtistAliasMerge{}, "", err
	}
	for _, loser := range artists[1:] {
		if _, err := tx.ExecContext(ctx, `DELETE FROM music_artists WHERE id=?`, loser.ID); err != nil {
			return ArtistAliasMerge{}, "", err
		}
	}
	if adoptedMBID != originalMBID {
		if _, err := tx.ExecContext(ctx, `UPDATE music_artists SET musicbrainz_id=?,updated_at=unixepoch() WHERE id=?`, adoptedMBID, survivor.ID); err != nil {
			return ArtistAliasMerge{}, "", err
		}
	}
	check, err := tx.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return ArtistAliasMerge{}, "", err
	}
	if check.Next() {
		check.Close()
		return ArtistAliasMerge{}, "", errors.New("music artist alias consolidation failed its foreign-key check")
	}
	if err := check.Err(); err != nil {
		check.Close()
		return ArtistAliasMerge{}, "", err
	}
	if err := check.Close(); err != nil {
		return ArtistAliasMerge{}, "", err
	}
	if err := tx.Commit(); err != nil {
		return ArtistAliasMerge{}, "", err
	}
	return ArtistAliasMerge{SurvivorID: survivor.ID, MergedIDs: ids[1:], Name: survivor.Name}, "", nil
}

func mergeArtistAliasMetadata(survivor *artistAliasRow, loser artistAliasRow) {
	if survivor.Disambiguation == "" {
		survivor.Disambiguation = loser.Disambiguation
	}
	if survivor.Country == "" {
		survivor.Country = loser.Country
	}
	if survivor.Type == "" {
		survivor.Type = loser.Type
	}
	if survivor.Overview == "" {
		survivor.Overview = loser.Overview
	}
	if survivor.ImageURL == "" {
		survivor.ImageURL = loser.ImageURL
	}
}

func mergeArtistPlaybackMarks(ctx context.Context, tx *sql.Tx, survivorID, loserID int64) error {
	if _, err := tx.ExecContext(ctx, `INSERT INTO music_playback_marks(user_id,kind,object_id,starred_at,rating,updated_at)
		SELECT user_id,'artist',?,starred_at,rating,updated_at FROM music_playback_marks WHERE kind='artist' AND object_id=?
		ON CONFLICT(user_id,kind,object_id) DO UPDATE SET
		starred_at=CASE WHEN excluded.starred_at>0 AND (music_playback_marks.starred_at=0 OR excluded.starred_at<music_playback_marks.starred_at)
			THEN excluded.starred_at ELSE music_playback_marks.starred_at END,
		rating=MAX(music_playback_marks.rating,excluded.rating),updated_at=MAX(music_playback_marks.updated_at,excluded.updated_at)`, survivorID, loserID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM music_playback_marks WHERE kind='artist' AND object_id=?`, loserID)
	return err
}

func artistAliasPlaceholders(count int) string {
	return strings.TrimSuffix(strings.Repeat("?,", count), ",")
}

func artistAliasArgs(ids []int64) []any {
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}
	return args
}
