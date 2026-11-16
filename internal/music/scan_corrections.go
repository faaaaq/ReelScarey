package music

import (
	"context"
	"database/sql"
	"errors"
)

const MaxScanCorrections = 100

type ScanCorrections struct {
	ReleaseGroupID string           `json:"release_group_id"`
	Artist         string           `json:"artist"`
	Album          string           `json:"album"`
	Files          []ScanCorrection `json:"files"`
}

type ScanCorrection struct {
	FileID  int64  `json:"file_id"`
	TrackID string `json:"track_id"`
	Title   string `json:"title"`
	Disc    int    `json:"disc"`
	Number  int    `json:"number"`
}

// CorrectScanMatches applies a reviewed mapping to unresolved scan rows. Local
// corrections leave the observed tags intact for file verification and future
// scans. The catalog identity supplies the metadata when Finalize is requested.
func (s *Service) CorrectScanMatches(ctx context.Context, scanID int64, correction ScanCorrections) error {
	if scanID < 1 || len(correction.Files) < 1 || len(correction.Files) > MaxScanCorrections {
		return ErrInvalidInput
	}
	artist, artistOK := cleanText(correction.Artist, 500)
	album, albumOK := cleanText(correction.Album, 500)
	groupID := genuineMBID(correction.ReleaseGroupID)
	provider := correction.ReleaseGroupID != ""
	if (provider && groupID == "") || (!provider && (!artistOK || !albumOK)) {
		return ErrInvalidInput
	}
	ids := make([]int64, 0, len(correction.Files))
	seenFiles := map[int64]bool{}
	seenTracks := map[string]bool{}
	seenPositions := map[[2]int]bool{}
	for i := range correction.Files {
		file := &correction.Files[i]
		if file.FileID < 1 || seenFiles[file.FileID] {
			return ErrInvalidInput
		}
		seenFiles[file.FileID] = true
		ids = append(ids, file.FileID)
		if provider {
			file.TrackID = genuineMBID(file.TrackID)
			if file.TrackID == "" || seenTracks[file.TrackID] {
				return ErrInvalidInput
			}
			seenTracks[file.TrackID] = true
		} else {
			title, ok := cleanText(file.Title, 500)
			position := [2]int{file.Disc, file.Number}
			if !ok || file.Disc < 1 || file.Disc > 100000 || file.Number < 1 || file.Number > 100000 || seenPositions[position] {
				return ErrInvalidInput
			}
			file.Title = title
			seenPositions[position] = true
		}
	}
	if err := checkCorrectionScan(ctx, s.store.db, scanID); err != nil {
		return err
	}
	files, err := s.unresolvedScanFiles(ctx, scanID, ids, false)
	if err != nil {
		return err
	}
	if len(files) != len(ids) {
		return ErrPlanChanged
	}
	var target albumTrackMatch
	if provider {
		preview, err := s.PreviewScanAlbum(ctx, groupID)
		if err != nil {
			return err
		}
		for _, track := range preview.Tracks {
			delete(seenTracks, track.MusicBrainzID)
		}
		if len(seenTracks) != 0 {
			return ErrPlanChanged
		}
		group := &discoverGroup{releaseGroupID: groupID}
		cache := &discoveryCache{artists: map[string]Artist{}, pendingArtists: map[string]Artist{}}
		target.artistID, target.albumID, err = s.discoverGroupAlbum(ctx, group, cache)
		if err != nil {
			return err
		}
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := checkCorrectionScan(ctx, tx, scanID); err != nil {
		return err
	}
	catalog := newLocalCatalog(s)
	catalog.db = tx
	for i, item := range correction.Files {
		match := target
		if provider {
			err := tx.QueryRowContext(ctx, `SELECT id FROM music_tracks WHERE album_id=? AND musicbrainz_id=?`, match.albumID, item.TrackID).Scan(&match.trackID)
			if errors.Is(err, sql.ErrNoRows) {
				return ErrPlanChanged
			}
			if err != nil {
				return err
			}
		} else {
			match, err = correctLocalTrack(ctx, catalog, files[i], artist, album, item)
			if err != nil {
				return err
			}
		}
		result, err := tx.ExecContext(ctx, `UPDATE music_scan_files
			SET artist_id=?,album_id=?,track_id=?,state='matched',error='',updated_at=unixepoch()
			WHERE id=? AND scan_id=? AND state='unresolved'`, match.artistID, match.albumID, match.trackID, item.FileID, scanID)
		if err != nil {
			return err
		}
		if changed, err := result.RowsAffected(); err != nil || changed != 1 {
			return ErrPlanChanged
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE music_scans SET
		matched_count=(SELECT COUNT(*) FROM music_scan_files WHERE scan_id=? AND state='matched'),
		unresolved_count=(SELECT COUNT(*) FROM music_scan_files WHERE scan_id=? AND state='unresolved'),
		updated_at=unixepoch() WHERE id=?`, scanID, scanID, scanID)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func checkCorrectionScan(ctx context.Context, db catalogConnection, scanID int64) error {
	scan, err := scanScan(db.QueryRowContext(ctx, `SELECT `+scanColumns+` FROM music_scans WHERE id=?`, scanID))
	if err != nil {
		return err
	}
	return workflowRequestable(scan)
}

func correctLocalTrack(ctx context.Context, catalog *localCatalog, file ScanFile, artist, album string, item ScanCorrection) (albumTrackMatch, error) {
	// Only the reviewed values become identity evidence. Old MusicBrainz IDs
	// belong to the original tags and can point to a different release.
	file.Artist, file.AlbumArtist, file.Album, file.Title = artist, artist, album, item.Title
	file.Track, file.Disc = &item.Number, &item.Disc
	file.MusicBrainzArtistID, file.MusicBrainzReleaseGroupID, file.TagJSON = "", "", ""
	file.MusicBrainzTrackID, file.MusicBrainzRecordingID = "", ""
	identity := tagIdentityFor(file)
	if !identity.canCreate() {
		return albumTrackMatch{}, ErrInvalidInput
	}
	ref, found, err := catalog.resolveAlbum(ctx, identity, true)
	if err != nil {
		return albumTrackMatch{}, err
	}
	if !found {
		return albumTrackMatch{}, ErrInvalidInput
	}
	if !IsLocalMusicID(ref.mbid) {
		return albumTrackMatch{}, &Error{Code: "music_correction_conflict", Message: "A MusicBrainz album with this artist and title is already saved. Use Match to MusicBrainz to choose its tracks."}
	}
	trackID, err := catalog.findOrCreateTrack(ctx, file, identity, ref)
	if err != nil {
		return albumTrackMatch{}, err
	}
	saved := trackByID(catalog.tracksByAlbum[ref.id], trackID)
	if saved == nil || saved.Disc != item.Disc || saved.Number != item.Number || !trackTitleMatches(saved.Title, trackTitleKeys(item.Title)) {
		return albumTrackMatch{}, &Error{Code: "music_correction_conflict", Message: "The saved track has a different title or position. Review the album and track assignment."}
	}
	return albumTrackMatch{ref.artistID, ref.id, trackID}, nil
}
