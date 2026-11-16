package music

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Evidence-based recording matching. This is the provider step that resolves
// files by artist and track title even when album, track-number or
// MusicBrainz-tag evidence is missing, ambiguous or wrong. Every candidate is
// scored; only a confident unique winner is auto-accepted, and anything else
// stays unresolved with a specific explanation.

const (
	// recordingAutoAcceptScore is the minimum evidence score for an automatic
	// recording match. Artist agreement plus title agreement reach 0.90, so a
	// match without usable duration evidence still clears it.
	recordingAutoAcceptScore = 0.85
	// recordingReviewFloor is the score above which a candidate is named in
	// the file's diagnostics instead of a generic "not found".
	recordingReviewFloor = 0.55
	// recordingMaxArtists bounds the provider work per file: the album artist
	// and the track artist cover compilations and guest credits.
	recordingMaxArtists = 2
	// recordingDurationToleranceMS and recordingDurationRatio bound the
	// duration agreement evidence.
	recordingDurationToleranceMS = 2500
	recordingDurationRatio       = 0.025
)

type scoredRecording struct {
	hit   RecordingHit
	score float64
}

// ProviderMatchRun bounds one recording-matching pass.
type ProviderMatchRun struct {
	onFile func(file ScanFile)
}

// ProviderMatchStats reports what one recording pass achieved so the workflow
// message can attribute matches to strategies honestly.
type ProviderMatchStats struct {
	Matched    int
	Deferred   int
	ProviderMB bool
	IssuedMB   bool
	IssuedItun bool
}

// ProviderMatchFiles resolves unresolved files through MusicBrainz recording
// searches with iTunes corroboration. It is idempotent: files that already
// match stay untouched, catalog rows are reused, and an interrupted pass
// resumes from the remaining unresolved rows.
func (s *Service) ProviderMatchFiles(ctx context.Context, scanID int64, files []ScanFile, run ProviderMatchRun) (ProviderMatchStats, error) {
	stats := ProviderMatchStats{}
	cache := &discoveryCache{
		artists:        map[string]Artist{},
		pendingArtists: map[string]Artist{},
		artistMatches:  map[string][]string{},
		discographies:  map[string][]Album{},
	}
	rgAlbums := map[string]fingerprintGroup{}
	searchCache := map[string][]scoredRecording{}
	searchFailed := map[string]bool{}
	dbCtx := context.WithoutCancel(ctx)
	for index, file := range files {
		if err := ctx.Err(); err != nil {
			stats.Deferred = len(files) - index
			return stats, err
		}
		if cancelled, err := s.workflowCancelled(dbCtx, scanID); err != nil {
			return stats, err
		} else if cancelled {
			return stats, nil
		}
		matched, message, err := s.matchFileRecordings(ctx, file, cache, rgAlbums, searchCache, searchFailed, &stats)
		if err != nil {
			var provider *Error
			if errors.As(err, &provider) && (provider.Code == "music_rate_limited" || provider.Code == "music_unavailable") {
				// A MusicBrainz outage pauses provider matching for the rest
				// of the pass. The files stay unresolved with their previous
				// state so the next run retries them.
				stats.Deferred = len(files) - index
				return stats, nil
			}
			if !errors.As(err, &provider) {
				return stats, err
			}
			message = provider.Message
		}
		if matched {
			stats.Matched++
		} else {
			if saveErr := s.updateScanFileState(dbCtx, file.ID, "unresolved", 0, 0, 0, message); saveErr != nil {
				return stats, saveErr
			}
		}
		if run.onFile != nil {
			run.onFile(file)
		}
	}
	return stats, nil
}

// matchFileRecordings runs the full recording-level decision for one file and
// reports whether it was matched, plus the diagnostic message for an
// unresolved outcome.
func (s *Service) matchFileRecordings(
	ctx context.Context,
	file ScanFile,
	cache *discoveryCache,
	rgAlbums map[string]fingerprintGroup,
	searchCache map[string][]scoredRecording,
	searchFailed map[string]bool,
	stats *ProviderMatchStats,
) (bool, string, error) {
	identity := inferFileIdentity(file)
	title := strings.TrimSpace(identity.Title)
	if title == "" {
		return false, "The file has no track title in its tags or file name, so no recording can be searched.", nil
	}
	candidates := identityArtistCandidates(identity)
	if len(candidates) == 0 {
		// No artist evidence at all. iTunes can still confirm a song exists
		// by title alone and supply the canonical artist for the local
		// fallback, which is exactly the case for files under an "unknown
		// artist" folder with a title in the file name.
		if s.itunester != nil {
			if tracks, err := s.itunester.SearchTracks(ctx, title, 5); err == nil {
				stats.IssuedItun = true
				for _, track := range tracks {
					if !trackTitleMatchesAny(track.TrackName, title) || isPlaceholderName(track.ArtistName) {
						continue
					}
					if recovered, ok := cleanText(track.ArtistName, 500); ok {
						s.rememberRecoveredArtist(file.ID, recovered)
					}
					return false, fmt.Sprintf("iTunes finds a song titled %q by %q, but MusicBrainz has no artist evidence on this file to confirm the recording identity.",
						track.TrackName, track.ArtistName), nil
				}
			}
		}
		return false, "The file has no artist name in its tags, file name or folder, so no recording can be searched.", nil
	}
	if len(candidates) > recordingMaxArtists {
		candidates = candidates[:recordingMaxArtists]
	}

	// MusicBrainz recording search. The results carry the credited artist
	// directly, so an ambiguous artist name never blocks the search.
	var best, second *scoredRecording
	mbTried := false
	for _, artistName := range candidates {
		key := normalizeArtistName(artistName) + "\x00" + normalizeMatchName(title)
		scored, ok := searchCache[key]
		if !ok {
			if searchFailed[key] {
				continue
			}
			mbTried = true
			hits, err := s.client.SearchRecordingByArtistName(ctx, artistName, title)
			if err != nil {
				var provider *Error
				if errors.As(err, &provider) && provider.Code == "music_not_found" {
					searchCache[key] = nil
					continue
				}
				if errors.As(err, &provider) && (provider.Code == "music_rate_limited" || provider.Code == "music_unavailable") {
					searchFailed[key] = true
					return false, "", err
				}
				searchFailed[key] = true
				continue
			}
			scored = scoreRecordingHits(hits, identity)
			searchCache[key] = scored
		}
		mbTried = true
		for i := range scored {
			switch {
			case best == nil || scored[i].score > best.score:
				second = best
				hit := scored[i]
				best = &hit
			case second == nil || scored[i].score > second.score:
				hit := scored[i]
				second = &hit
			}
		}
	}
	if mbTried {
		stats.IssuedMB = true
		stats.ProviderMB = true
	}

	if best != nil && best.score >= recordingAutoAcceptScore {
		// Two different recordings with equal evidence are genuinely
		// ambiguous; only a clear winner is auto-accepted.
		if second != nil && second.score >= recordingAutoAcceptScore &&
			!strings.EqualFold(second.hit.MBID, best.hit.MBID) &&
			!recordingEvidenceSeparates(best.hit, second.hit, identity) {
			return false, recordingAmbiguityMessage(best.hit, identity), nil
		}
		matched, err := s.applyRecordingMatch(ctx, file, best.hit, identity, cache, rgAlbums)
		if err != nil {
			return false, "", err
		}
		if matched {
			return true, "", nil
		}
		return false, "A MusicBrainz recording was found, but its album could not be resolved into the catalog. Try discovery again later.", nil
	}
	if best != nil && best.score >= recordingReviewFloor {
		return false, recordingBelowThresholdMessage(*best), nil
	}

	// iTunes corroboration and name recovery. iTunes has no MusicBrainz
	// identifiers, so it never creates a canonical match by itself; it
	// confirms that the song exists and supplies a canonical artist name when
	// the file's own artist is missing or a placeholder.
	if s.itunester != nil {
		term := iTunesSearchTermFor(candidates[0], title)
		tracks, err := s.itunester.SearchTracks(ctx, term, 5)
		if err != nil {
			var provider *Error
			if errors.As(err, &provider) && (provider.Code == "music_itunes_rate_limited" || provider.Code == "music_itunes_unavailable") {
				return false, "MusicBrainz and iTunes could not search this recording right now. Run matching again later.", nil
			}
			return false, "The recording search could not reach its providers right now. Run matching again later.", nil
		}
		stats.IssuedItun = true
		for _, track := range tracks {
			if !trackTitleMatchesAny(track.TrackName, title) {
				continue
			}
			if !isPlaceholderName(track.ArtistName) &&
				(isPlaceholderName(identity.Artist) || strings.TrimSpace(identity.Artist) == "") {
				if recovered, ok := cleanText(track.ArtistName, 500); ok {
					s.rememberRecoveredArtist(file.ID, recovered)
				}
			}
			return false, fmt.Sprintf("iTunes finds a song titled %q by %q, but MusicBrainz has no matching recording and iTunes does not provide MusicBrainz identities.",
				track.TrackName, track.ArtistName), nil
		}
	}
	if !mbTried {
		return false, "MusicBrainz was unavailable for this recording search. Run matching again later.", nil
	}
	return false, fmt.Sprintf("MusicBrainz and iTunes have no recording for artist %q and title %q. The file is matched from its own names in the local catalog when that evidence exists.",
		candidates[0], title), nil
}

// rememberRecoveredArtist stores one iTunes-recovered artist name per file for
// the local fallback phase of the same run.
func (s *Service) rememberRecoveredArtist(fileID int64, name string) {
	if s.recoveredArtists == nil {
		s.recoveredArtists = map[int64]string{}
	}
	s.recoveredArtists[fileID] = name
}

// recoveredArtist returns the iTunes-recovered artist for one file, if any.
func (s *Service) recoveredArtist(fileID int64) string {
	if s.recoveredArtists == nil {
		return ""
	}
	return s.recoveredArtists[fileID]
}

// applyRecordingMatch resolves the winning recording into the catalog: through
// its release group when MusicBrainz names one, otherwise through a
// tag-derived album under the credited artist.
func (s *Service) applyRecordingMatch(
	ctx context.Context,
	file ScanFile,
	hit RecordingHit,
	identity fileIdentity,
	cache *discoveryCache,
	rgAlbums map[string]fingerprintGroup,
) (bool, error) {
	group := chooseRecordingReleaseGroup(hit, identity)
	if group != nil {
		key := strings.ToLower(group.MusicBrainzID)
		resolved, known := rgAlbums[key]
		if !known {
			discoverGroup := &discoverGroup{releaseGroupID: key, files: []ScanFile{file}}
			artistID, albumID, err := s.discoverGroupAlbum(ctx, discoverGroup, cache)
			if err != nil {
				return false, err
			}
			resolved = fingerprintGroup{artistID: artistID, albumID: albumID}
			rgAlbums[key] = resolved
		}
		trackID, err := s.matchTrackByRecording(ctx, file, resolved.albumID, hit.MBID, hit.Title)
		if err == nil {
			if err := s.updateScanFileState(ctx, file.ID, "matched", resolved.artistID, resolved.albumID, trackID, ""); err != nil {
				return false, err
			}
			return true, nil
		}
		// The recording exists but is not on the loaded track list (a bonus
		// track on another release, for example). When the file's own album
		// evidence agrees with the resolved album, extend the catalog album
		// with the genuine recording instead of leaving the file unresolved.
		if albumAgreesWithRecording(identity, group.Title) {
			extended, trackID, extErr := s.extendAlbumWithRecording(ctx, file, resolved.albumID, resolved.artistID, hit)
			if extErr != nil {
				return false, extErr
			}
			if extended {
				if err := s.updateScanFileState(ctx, file.ID, "matched", resolved.artistID, resolved.albumID, trackID, ""); err != nil {
					return false, err
				}
				return true, nil
			}
		}
		return false, nil
	}
	// No release-group evidence: the recording still carries a credited
	// artist, so build the entry through the tag-derived album path with the
	// genuine recording identity attached.
	artist, _, err := s.discoverArtist(ctx, hit.ArtistID, cache)
	if err != nil {
		return false, err
	}
	ev := tagIdentityFor(file).withRecording(hit.ArtistID, hit.MBID, hit.Title)
	ev.artistMBID = strings.ToLower(hit.ArtistID)
	ev.artistName = artist.Name
	ev.artistKey = localArtistKey(artist.Name)
	catalog := newLocalCatalog(s)
	artistID, albumID, trackID, matchErr := catalog.matchIdentity(ctx, file, ev)
	if matchErr != nil {
		var provider *Error
		if errors.As(matchErr, &provider) {
			return false, nil
		}
		return false, matchErr
	}
	if err := s.updateScanFileState(ctx, file.ID, "matched", artistID, albumID, trackID, ""); err != nil {
		return false, err
	}
	return true, nil
}

// extendAlbumWithRecording adds the winning recording to an already resolved
// catalog album when no existing track carries the same title. It never
// overwrites or renumbers a track and returns the new track ID.
func (s *Service) extendAlbumWithRecording(ctx context.Context, file ScanFile, albumID, artistID int64, hit RecordingHit) (bool, int64, error) {
	tracks, err := s.store.Tracks(ctx, albumID)
	if err != nil {
		return false, 0, err
	}
	for _, track := range tracks {
		if trackTitleMatches(track.Title, trackTitleKeys(hit.Title)) {
			return false, 0, nil
		}
	}
	album, err := s.store.Album(ctx, albumID)
	if err != nil {
		return false, 0, err
	}
	ev := tagIdentityFor(file).withRecording(hit.ArtistID, hit.MBID, hit.Title)
	ev.trackTitle = hit.Title
	ev.trackKey = compactMatchName(hit.Title)
	if name := strings.TrimSpace(album.ArtistName); name != "" {
		ev.artistName = name
		ev.artistKey = localArtistKey(name)
	}
	catalog := newLocalCatalog(s)
	trackID, err := catalog.findOrCreateTrack(ctx, file, ev, albumRef{
		id: albumID, artistID: artistID, mbid: strings.ToLower(album.MusicBrainzID), title: album.Title,
	})
	if err != nil {
		return false, 0, err
	}
	return true, trackID, nil
}

// chooseRecordingReleaseGroup picks the release group of a recording that best
// agrees with the file's own album evidence, or the recording's first group
// when the file has none.
func chooseRecordingReleaseGroup(hit RecordingHit, identity fileIdentity) *Album {
	if len(hit.ReleaseGroups) == 0 {
		return nil
	}
	if album := identityAlbumTitle(identity); album != "" {
		for i := range hit.ReleaseGroups {
			if matchNameEqual(hit.ReleaseGroups[i].Title, album) {
				return &hit.ReleaseGroups[i]
			}
		}
	}
	return &hit.ReleaseGroups[0]
}

// albumAgreesWithRecording reports whether the file's album evidence and the
// recording's release group name the same release, which justifies extending
// the resolved catalog album when the track list lacks the recording. Without
// any album evidence the recording itself is the best available home.
func albumAgreesWithRecording(identity fileIdentity, releaseGroupTitle string) bool {
	album := identityAlbumTitle(identity)
	if album == "" {
		return true
	}
	return matchNameEqual(album, releaseGroupTitle)
}

// scoreRecordingHits scores every hit against the file's own evidence.
func scoreRecordingHits(hits []RecordingHit, identity fileIdentity) []scoredRecording {
	out := make([]scoredRecording, 0, len(hits))
	for _, hit := range hits {
		out = append(out, scoredRecording{hit: hit, score: scoreRecordingHit(hit, identity)})
	}
	return out
}

func scoreRecordingHit(hit RecordingHit, identity fileIdentity) float64 {
	score := 0.0
	for _, name := range identityArtistCandidates(identity) {
		if artistNamesEqual(hit.ArtistName, name) {
			score += 0.45
			break
		}
		if primary, ok := primaryArtistName(hit.ArtistName); ok && artistNamesEqual(primary, name) {
			score += 0.40
			break
		}
	}
	if trackTitleMatchesAny(hit.Title, identity.Title) {
		score += 0.45
	}
	score += recordingDurationScore(identity.DurationMS, hit.LengthMS)
	return score
}

// recordingDurationScore rewards agreeing durations and stays neutral when
// either side has none, so a missing duration cannot hide a strong
// artist/title match.
func recordingDurationScore(fileDurationMS, hitLengthMS int) float64 {
	if fileDurationMS <= 0 || hitLengthMS <= 0 {
		return 0
	}
	delta := fileDurationMS - hitLengthMS
	if delta < 0 {
		delta = -delta
	}
	tolerance := recordingDurationToleranceMS
	if limit := int(recordingDurationRatio * float64(fileDurationMS)); limit > tolerance {
		tolerance = limit
	}
	if delta <= tolerance {
		return 0.10
	}
	return 0
}

// recordingEvidenceSeparates reports whether duration evidence prefers the
// first recording over the second. Recordings without durations never
// separate.
func recordingEvidenceSeparates(best, second RecordingHit, identity fileIdentity) bool {
	if identity.DurationMS <= 0 || best.LengthMS <= 0 || second.LengthMS <= 0 {
		return false
	}
	bestDelta, secondDelta := absInt(identity.DurationMS-best.LengthMS), absInt(identity.DurationMS-second.LengthMS)
	if bestDelta == secondDelta {
		return false
	}
	return bestDelta < secondDelta
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func recordingAmbiguityMessage(best RecordingHit, identity fileIdentity) string {
	return fmt.Sprintf("More than one MusicBrainz recording named %q by %q matches this file. ReelScarey will not choose automatically; check the recordings and use Correct match.",
		best.Title, best.ArtistName)
}

func recordingBelowThresholdMessage(best scoredRecording) string {
	return fmt.Sprintf("The closest MusicBrainz recording is %q by %q, but the evidence is too weak to match automatically (confidence %.0f%%). Check the candidates and use Correct match.",
		best.hit.Title, best.hit.ArtistName, best.score*100)
}

// trackTitleMatchesAny reports whether a title matches any of the candidate
// keys derived from the file's title, including the featured-credit base
// form. Both directions are checked so "(ft. X)" suffixes on either side fold.
func trackTitleMatchesAny(candidate, title string) bool {
	return trackTitleMatches(candidate, trackTitleKeys(title)) ||
		trackTitleMatches(title, trackTitleKeys(candidate))
}
