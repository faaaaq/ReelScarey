package music

import (
	"context"
	"errors"
	"strings"
	"time"
)

const (
	// MaxFingerprintFiles bounds one reviewed fingerprint request. Files beyond
	// this stay unresolved and are reported as deferred.
	MaxFingerprintFiles = 20
	// MaxFingerprintAlbums bounds the MusicBrainz work one request can start.
	MaxFingerprintAlbums = 10
	// fingerprintWorkBudget and acoustidProviderTimeout keep the request inside
	// the server write deadline while allowing sequential AcoustID lookups.
	fingerprintWorkBudget   = 20 * time.Second
	acoustidProviderTimeout = 30 * time.Second
)

type fingerprintGroup struct {
	artistID int64
	albumID  int64
}

// FingerprintScan fingerprints selected unresolved scan files, looks them up
// through AcoustID and adds only a strong, unique release group. It never moves
// or edits media; a file it cannot prove stays unresolved with a specific
// message. Cached Chromaprint evidence is reused while size and mtime match.
func (s *Service) FingerprintScan(ctx context.Context, scanID int64, fileIDs []int64, all bool) (DiscoverResult, error) {
	if scanID < 1 || (all && len(fileIDs) > 0) ||
		(!all && (len(fileIDs) < 1 || len(fileIDs) > MaxDiscoverFiles)) {
		return DiscoverResult{}, ErrInvalidInput
	}
	var state string
	if err := s.store.db.QueryRowContext(ctx, `SELECT state FROM music_scans WHERE id=?`, scanID).Scan(&state); err != nil {
		return DiscoverResult{}, err
	}
	if state == "scanning" {
		return DiscoverResult{}, &Error{Code: "music_scan_state", Message: "Wait for the scan to finish before fingerprint matching."}
	}
	key, err := s.acoustidKey(ctx)
	if err != nil {
		return DiscoverResult{}, err
	}
	if s.fingerprinter == nil {
		return DiscoverResult{}, &Error{Code: "music_fingerprint_unavailable", Message: "Chromaprint fingerprinting is not available in this deployment."}
	}
	if s.acoustid == nil {
		return DiscoverResult{}, &Error{Code: "music_acoustid_unavailable", Message: "The AcoustID provider is unavailable. Try again later."}
	}
	files, err := s.unresolvedScanFiles(ctx, scanID, fileIDs, all)
	if err != nil {
		return DiscoverResult{}, err
	}
	result := DiscoverResult{Selected: len(files), Warnings: []string{}}
	dbCtx := context.WithoutCancel(ctx)
	if len(files) == 0 {
		result.Remaining, err = s.refreshScanCounts(dbCtx, scanID)
		return result, err
	}
	run := fingerprintRun{
		maxFiles:        MaxFingerprintFiles,
		maxAlbums:       MaxFingerprintAlbums,
		workBudget:      fingerprintWorkBudget,
		providerTimeout: acoustidProviderTimeout,
	}
	result, err = s.runFingerprint(ctx, scanID, files, key, run)
	return result, err
}

// fingerprintRun bounds one fingerprint pass. A zero maxFiles, maxAlbums or
// workBudget lifts that bound so the durable worker's own lease and context
// govern the pass; onFile reports each examined file.
type fingerprintRun struct {
	maxFiles        int
	maxAlbums       int
	workBudget      time.Duration
	providerTimeout time.Duration
	onFile          func(file ScanFile)
}

func (s *Service) runFingerprint(ctx context.Context, scanID int64, files []ScanFile, key string, run fingerprintRun) (DiscoverResult, error) {
	result := DiscoverResult{Selected: len(files), Warnings: []string{}}
	dbCtx := context.WithoutCancel(ctx)
	cache := &discoveryCache{
		artists:        map[string]Artist{},
		pendingArtists: map[string]Artist{},
		artistMatches:  map[string][]string{},
		discographies:  map[string][]Album{},
	}
	providerCtx := ctx
	cancel := func() {}
	if run.providerTimeout > 0 {
		providerCtx, cancel = context.WithTimeout(ctx, run.providerTimeout)
	}
	defer cancel()
	var workDeadline time.Time
	if run.workBudget > 0 {
		workDeadline = time.Now().Add(run.workBudget)
	}
	groups := map[string]fingerprintGroup{}
	attempted := 0
	var fatalErr error
	for index, file := range files {
		if (run.maxFiles > 0 && attempted >= run.maxFiles) ||
			(!workDeadline.IsZero() && time.Now().After(workDeadline)) {
			result.Deferred += len(files) - index
			break
		}
		if err := ctx.Err(); err != nil {
			result.Deferred += len(files) - index
			break
		}
		attempted++
		fingerprint, err := s.fingerprintScanFile(providerCtx, file)
		if err != nil {
			if saveErr := s.updateScanFileState(dbCtx, file.ID, "unresolved", 0, 0, 0, acoustidFileMessage(err)); saveErr != nil {
				return result, saveErr
			}
			if run.onFile != nil {
				run.onFile(file)
			}
			if fingerprintStops(err) {
				fatalErr = err
				result.Deferred += len(files) - index - 1
				result.Warnings = append(result.Warnings, acoustidWarning(err))
				break
			}
			continue
		}
		result.Fingerprinted++
		result.ProviderCalls++
		candidates, err := s.acoustid.Lookup(providerCtx, key, fingerprint.DurationSeconds, fingerprint.Value)
		if err != nil {
			if saveErr := s.updateScanFileState(dbCtx, file.ID, "unresolved", 0, 0, 0, acoustidFileMessage(err)); saveErr != nil {
				return result, saveErr
			}
			if run.onFile != nil {
				run.onFile(file)
			}
			if acoustidStops(err) {
				fatalErr = err
				result.Deferred += len(files) - index - 1
				result.Warnings = append(result.Warnings, acoustidWarning(err))
				break
			}
			continue
		}
		match, err := ResolveAcoustIDForAlbum(candidates, file.MusicBrainzReleaseGroupID, file.Album)
		if err != nil {
			if saveErr := s.updateScanFileState(dbCtx, file.ID, "unresolved", 0, 0, 0, acoustidFileMessage(err)); saveErr != nil {
				return result, saveErr
			}
			if run.onFile != nil {
				run.onFile(file)
			}
			continue
		}
		resolved, known := groups[match.ReleaseGroupID]
		if !known {
			if run.maxAlbums > 0 && len(groups) >= run.maxAlbums {
				result.Deferred += len(files) - index
				break
			}
			group := &discoverGroup{releaseGroupID: match.ReleaseGroupID, files: []ScanFile{file}}
			artistID, albumID, err := s.discoverGroupAlbum(providerCtx, group, cache)
			if err != nil {
				message := acoustidFileMessage(err)
				if saveErr := s.updateScanFileState(dbCtx, file.ID, "unresolved", artistID, albumID, 0, message); saveErr != nil {
					return result, saveErr
				}
				if run.onFile != nil {
					run.onFile(file)
				}
				if discoveryStops(err) {
					fatalErr = err
					result.Deferred += len(files) - index - 1
					result.Warnings = append(result.Warnings, acoustidWarning(err))
					break
				}
				continue
			}
			resolved = fingerprintGroup{artistID: artistID, albumID: albumID}
			groups[match.ReleaseGroupID] = resolved
		}
		trackID, err := s.matchTrackByRecording(dbCtx, file, resolved.albumID, match.RecordingID)
		if err != nil {
			message := fingerprintTrackMessage(err)
			if saveErr := s.updateScanFileState(dbCtx, file.ID, "unresolved", resolved.artistID, resolved.albumID, 0, message); saveErr != nil {
				return result, saveErr
			}
			if run.onFile != nil {
				run.onFile(file)
			}
			continue
		}
		if err := s.updateScanFileState(dbCtx, file.ID, "matched", resolved.artistID, resolved.albumID, trackID, ""); err != nil {
			return result, err
		}
		result.Matched++
		if run.onFile != nil {
			run.onFile(file)
		}
	}
	result.ArtistsAdded = cache.addedArtists
	result.AlbumsAdded = cache.addedAlbums
	result.Unresolved = result.Selected - result.Matched
	result.Warnings = append(result.Warnings, cache.warnings...)
	remaining, err := s.refreshScanCounts(dbCtx, scanID)
	if err != nil {
		return result, err
	}
	result.Remaining = remaining
	return result, fatalErr
}

// matchTrackByRecording prefers the AcoustID recording ID, then falls back to
// the existing tag/track evidence in matchAlbumTrack. It never guesses.
func (s *Service) matchTrackByRecording(ctx context.Context, file ScanFile, albumID int64, recordingID string) (int64, error) {
	if err := s.ensureAlbumTracks(ctx, albumID); err != nil {
		return 0, &Error{Code: "music_album_tracks_unavailable", Message: "The album matched but its MusicBrainz track list could not be loaded. Try again later."}
	}
	tracks, err := s.store.Tracks(ctx, albumID)
	if err != nil {
		return 0, err
	}
	if len(tracks) == 0 {
		return 0, &Error{Code: "music_unmatched_track", Message: "The album matched but MusicBrainz returned no tracks for it."}
	}
	if id := strings.ToLower(strings.TrimSpace(recordingID)); id != "" {
		for _, track := range tracks {
			if strings.EqualFold(track.RecordingID, id) {
				return track.ID, nil
			}
		}
	}
	return matchAlbumTrack(file, tracks)
}

func fingerprintTrackMessage(err error) string {
	var provider *Error
	if errors.As(err, &provider) && strings.TrimSpace(provider.Message) != "" {
		return provider.Message
	}
	return "The album matched, but no track matches the AcoustID recording or this file's tags. Correct the tags and run fingerprint matching again."
}

func acoustidFileMessage(err error) string {
	var provider *Error
	if errors.As(err, &provider) {
		switch provider.Code {
		case "music_acoustid_rate_limited":
			return "AcoustID is limiting requests. Wait and run fingerprint matching again."
		case "music_acoustid_invalid_key":
			return "AcoustID rejected the application key. Save a valid key in Music import settings and try again."
		case "music_acoustid_unavailable":
			return "AcoustID is unavailable. Try again later."
		case "music_acoustid_invalid_response":
			return "AcoustID returned an invalid response. Try again later."
		case "music_acoustid_no_match":
			return "AcoustID found no match for this fingerprint. Add MusicBrainz tags or the release-group ID and try again."
		case "music_release_group_not_found":
			return "AcoustID named a release group that MusicBrainz no longer has. The file stays unresolved; correct the tags or add the album manually."
		}
		if strings.TrimSpace(provider.Message) != "" {
			return provider.Message
		}
	}
	return "The file could not be matched through AcoustID. Try again or correct the tags."
}

func acoustidWarning(err error) string {
	var provider *Error
	if errors.As(err, &provider) && strings.TrimSpace(provider.Message) != "" {
		return provider.Message
	}
	return "Fingerprint matching stopped early. Try again."
}

func acoustidStops(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	var provider *Error
	if errors.As(err, &provider) {
		switch provider.Code {
		case "music_acoustid_no_match", "music_acoustid_no_confident_match",
			"music_acoustid_no_release_group", "music_acoustid_ambiguous_release_group":
			return false
		}
		return true
	}
	return false
}

func fingerprintStops(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	var provider *Error
	if errors.As(err, &provider) {
		switch provider.Code {
		case "music_fingerprint_failed", "music_fingerprint_input":
			return false
		}
		return true
	}
	return false
}
