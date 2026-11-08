package music

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	// MaxDiscoverFiles bounds one reviewed discovery request.
	MaxDiscoverFiles = 500
	// MaxDiscoverAlbums bounds provider work. Files in later album groups stay
	// unresolved and are reported as deferred so the user can continue.
	MaxDiscoverAlbums = 10
	// discoveryArtistSearchPages bounds the artist-name search when an exact
	// name is not on the first provider page. 25 results per page.
	discoveryArtistSearchPages = 5
	// discoveryWorkBudget stops starting new albums before the server's write
	// deadline; discoveryProviderTimeout bounds the provider context itself.
	discoveryWorkBudget      = 22 * time.Second
	discoveryProviderTimeout = 30 * time.Second
)

// DiscoverResult reports what a reviewed catalog-discovery request changed.
// Remaining counts unresolved files in the whole scan, not only the selection.
type DiscoverResult struct {
	Selected     int      `json:"selected"`
	Matched      int      `json:"matched"`
	ArtistsAdded int      `json:"artists_added"`
	AlbumsAdded  int      `json:"albums_added"`
	Unresolved   int      `json:"unresolved"`
	Remaining    int      `json:"remaining"`
	Deferred     int      `json:"deferred"`
	Warnings     []string `json:"warnings"`
}

type discoverGroup struct {
	releaseGroupID   string
	artist           string
	artistCandidates []string
	artistMBIDs      map[string]bool
	album            string
	years            map[int]bool
	files            []ScanFile
}

type discoveryCache struct {
	artists        map[string]Artist
	pendingArtists map[string]Artist
	artistMatches  map[string][]string
	discographies  map[string][]Album
	warnings       []string
	addedArtists   int
	addedAlbums    int
}

// DiscoverScan resolves strong album evidence for selected unresolved scan
// files, creates only the exact MusicBrainz catalog entries it can prove, loads
// their tracks and re-matches those files. It never moves or edits media.
func (s *Service) DiscoverScan(ctx context.Context, scanID int64, fileIDs []int64, all bool) (DiscoverResult, error) {
	if scanID < 1 || (all && len(fileIDs) > 0) ||
		(!all && (len(fileIDs) < 1 || len(fileIDs) > MaxDiscoverFiles)) {
		return DiscoverResult{}, ErrInvalidInput
	}
	var state string
	if err := s.store.db.QueryRowContext(ctx, `SELECT state FROM music_scans WHERE id=?`, scanID).Scan(&state); err != nil {
		return DiscoverResult{}, err
	}
	if state == "scanning" {
		return DiscoverResult{}, &Error{Code: "music_scan_state", Message: "Wait for the scan to finish before discovering catalog entries."}
	}
	files, err := s.unresolvedScanFiles(ctx, scanID, fileIDs, all)
	if err != nil {
		return DiscoverResult{}, err
	}
	result := DiscoverResult{Selected: len(files), Warnings: []string{}}
	dbCtx := context.WithoutCancel(ctx)
	groups, err := s.buildDiscoveryGroups(dbCtx, files)
	if err != nil {
		return result, err
	}
	cache := &discoveryCache{
		artists:        map[string]Artist{},
		pendingArtists: map[string]Artist{},
		artistMatches:  map[string][]string{},
		discographies:  map[string][]Album{},
	}
	providerCtx, cancel := context.WithTimeout(ctx, discoveryProviderTimeout)
	defer cancel()
	workDeadline := time.Now().Add(discoveryWorkBudget)
	stopped := false
	processed := 0
	for i, group := range groups {
		if stopped || processed >= MaxDiscoverAlbums || time.Now().After(workDeadline) {
			for _, remaining := range groups[i:] {
				result.Deferred += len(remaining.files)
			}
			break
		}
		processed++
		artistID, albumID, err := s.discoverGroupAlbum(providerCtx, group, cache)
		if err != nil {
			if saveErr := s.markDiscoverGroup(dbCtx, group, artistID, albumID, discoveryErrorMessage(err)); saveErr != nil {
				return result, saveErr
			}
			if discoveryStops(err) {
				cache.addWarning(discoveryWarning(err))
				stopped = true
			}
			continue
		}
		for _, file := range group.files {
			trackID, err := s.matchTrack(dbCtx, file, albumID)
			if err != nil {
				message := discoveryTrackMessage(err)
				if saveErr := s.updateScanFileState(dbCtx, file.ID, "unresolved", artistID, albumID, 0, message); saveErr != nil {
					return result, saveErr
				}
				continue
			}
			if err := s.updateScanFileState(dbCtx, file.ID, "matched", artistID, albumID, trackID, ""); err != nil {
				return result, err
			}
			result.Matched++
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
	return result, nil
}

func (s *Service) unresolvedScanFiles(ctx context.Context, scanID int64, fileIDs []int64, all bool) ([]ScanFile, error) {
	if all {
		rows, err := s.store.db.QueryContext(ctx, `SELECT `+scanFileColumns+` FROM music_scan_files
			WHERE scan_id=? AND state='unresolved' ORDER BY id LIMIT ?`, scanID, MaxDiscoverFiles)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		files := []ScanFile{}
		for rows.Next() {
			file, err := scanScanFile(rows)
			if err != nil {
				return nil, err
			}
			files = append(files, file)
		}
		return files, rows.Err()
	}
	seen := map[int64]bool{}
	ids := make([]int64, 0, len(fileIDs))
	for _, id := range fileIDs {
		if id > 0 && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return []ScanFile{}, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, 0, len(ids)+1)
	args = append(args, scanID)
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := s.store.db.QueryContext(ctx, `SELECT `+scanFileColumns+` FROM music_scan_files
		WHERE scan_id=? AND state='unresolved' AND id IN (`+placeholders+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byID := map[int64]ScanFile{}
	for rows.Next() {
		file, err := scanScanFile(rows)
		if err != nil {
			return nil, err
		}
		byID[file.ID] = file
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	files := make([]ScanFile, 0, len(ids))
	for _, id := range ids {
		if file, ok := byID[id]; ok {
			files = append(files, file)
		}
	}
	return files, nil
}

// buildDiscoveryGroups groups strong evidence without inferring identity. A
// release-group ID is its own group; otherwise normalized album artist plus
// album title is the only accepted fallback. A trusted album-artist MBID is
// kept as direct evidence instead of searching the name again.
func (s *Service) buildDiscoveryGroups(ctx context.Context, files []ScanFile) ([]*discoverGroup, error) {
	groups := []*discoverGroup{}
	index := map[string]int{}
	for _, file := range files {
		releaseGroupID := strings.ToLower(strings.TrimSpace(file.MusicBrainzReleaseGroupID))
		if ValidMBID(releaseGroupID) {
			key := "rg:" + releaseGroupID
			if position, ok := index[key]; ok {
				groups[position].files = append(groups[position].files, file)
				continue
			}
			index[key] = len(groups)
			groups = append(groups, &discoverGroup{releaseGroupID: releaseGroupID, files: []ScanFile{file}})
			continue
		}
		artist, artistOK := cleanText(firstNonEmpty(file.AlbumArtist, file.Artist), 500)
		album, albumOK := cleanText(file.Album, 500)
		artistName, albumName := normalizeMatchName(artist), normalizeMatchName(album)
		if !artistOK || !albumOK || artistName == "" || albumName == "" {
			message := discoveryMissingTagMessage(file, artistName == "", albumName == "")
			if err := s.updateScanFileState(ctx, file.ID, "unresolved", 0, 0, 0, message); err != nil {
				return nil, err
			}
			continue
		}
		key := "tags:" + artistGroupKey(artist) + "\x00" + albumName
		if position, ok := index[key]; ok {
			addFileToDiscoveryGroup(groups[position], file)
			continue
		}
		group := &discoverGroup{
			artist:           artist,
			artistCandidates: discoveryArtistCandidates(artist, file.Artist),
			artistMBIDs:      map[string]bool{},
			album:            album,
			years:            map[int]bool{},
		}
		addFileToDiscoveryGroup(group, file)
		index[key] = len(groups)
		groups = append(groups, group)
	}
	return groups, nil
}

func addFileToDiscoveryGroup(group *discoverGroup, file ScanFile) {
	group.files = append(group.files, file)
	if file.Year != nil {
		group.years[*file.Year] = true
	}
	if mbid := trustedArtistMusicBrainzID(file); mbid != "" {
		group.artistMBIDs[mbid] = true
	}
}

func discoveryMissingTagMessage(file ScanFile, missingArtist, missingAlbum bool) string {
	var message string
	switch {
	case missingArtist && missingAlbum:
		message = "The file has no artist or album tags. Add artist, album and MusicBrainz tags and scan again."
	case missingArtist:
		message = "The file has no artist or album-artist tag. Add one or a MusicBrainz artist ID and run discovery again."
	default:
		message = "The file has no album tag. Add it or a MusicBrainz release-group ID and run discovery again."
	}
	if file.Error != "" {
		message = file.Error + " " + message
	}
	return message
}

// trustedArtistMusicBrainzID returns the album artist's MBID when the tags
// prove it. A track-artist MBID is only used when the album artist and track
// artist are the same name, so a compilation track is never attached to one
// featured artist.
func trustedArtistMusicBrainzID(file ScanFile) string {
	if mbid := albumArtistMusicBrainzID(file); ValidMBID(mbid) {
		return strings.ToLower(mbid)
	}
	if strings.TrimSpace(file.AlbumArtist) != "" && !artistNamesEqual(file.AlbumArtist, file.Artist) {
		return ""
	}
	if mbid := strings.ToLower(strings.TrimSpace(file.MusicBrainzArtistID)); ValidMBID(mbid) {
		return mbid
	}
	return ""
}

// albumArtistMusicBrainzID reads the album-artist MBID from the saved tag
// document. The scan table has no dedicated column for it; older scans simply
// do not carry the value and fall back to the name search.
func albumArtistMusicBrainzID(file ScanFile) string {
	if strings.TrimSpace(file.TagJSON) == "" {
		return ""
	}
	var tags AudioTags
	if json.Unmarshal([]byte(file.TagJSON), &tags) != nil {
		return ""
	}
	return tags.MusicBrainzAlbumArtistID
}

func discoveryArtistCandidates(albumArtist, trackArtist string) []string {
	out := []string{}
	seen := map[string]bool{}
	add := func(value string) {
		value = strings.TrimSpace(value)
		key := normalizeArtistName(value)
		if value == "" || key == "" || seen[key] {
			return
		}
		seen[key] = true
		out = append(out, value)
	}
	add(albumArtist)
	if artistNamesEqual(albumArtist, trackArtist) {
		if primary, ok := primaryArtistName(albumArtist); ok {
			add(primary)
		}
	}
	return out
}

func (s *Service) discoverGroupAlbum(ctx context.Context, group *discoverGroup, cache *discoveryCache) (int64, int64, error) {
	if group.releaseGroupID != "" {
		if existing, err := s.store.AlbumByMusicBrainzID(ctx, group.releaseGroupID); err == nil {
			if err := s.ensureAlbumTracks(ctx, existing.ID); err != nil {
				return existing.ArtistID, existing.ID, albumTracksUnavailableError(existing.Title, err)
			}
			return existing.ArtistID, existing.ID, nil
		} else if !errors.Is(err, sql.ErrNoRows) {
			return 0, 0, err
		}
		album, artistID, err := s.client.ReleaseGroup(ctx, group.releaseGroupID)
		if err != nil {
			var provider *Error
			if errors.As(err, &provider) && provider.Code == "music_not_found" {
				return 0, 0, &Error{Code: "music_release_group_not_found", Message: fmt.Sprintf("MusicBrainz has no release group with the ID %q from this file's tag. Correct the tag and scan again.", group.releaseGroupID)}
			}
			return 0, 0, err
		}
		return s.saveDiscoveredAlbum(ctx, artistID, album, cache)
	}
	artistID, err := s.resolveDiscoveryArtist(ctx, group, cache)
	if err != nil {
		return 0, 0, err
	}
	album, err := s.findDiscoveryAlbum(ctx, artistID, group, cache)
	if err != nil {
		return 0, 0, err
	}
	return s.saveDiscoveredAlbum(ctx, artistID, album, cache)
}

// resolveDiscoveryArtist prefers an MBID the file already carries, then exact
// normalized names across a bounded number of provider pages.
func (s *Service) resolveDiscoveryArtist(ctx context.Context, group *discoverGroup, cache *discoveryCache) (string, error) {
	if len(group.artistMBIDs) > 1 {
		return "", &Error{Code: "music_ambiguous_artist", Message: fmt.Sprintf("The files for %q carry more than one MusicBrainz artist ID. Correct the tags or add the artist first.", group.album)}
	}
	for mbid := range group.artistMBIDs {
		if _, err := s.store.ArtistByMusicBrainzID(ctx, mbid); err == nil {
			return mbid, nil
		} else if !errors.Is(err, sql.ErrNoRows) {
			return "", err
		}
		artist, err := s.client.Artist(ctx, mbid)
		if err == nil {
			cache.pendingArtists[strings.ToLower(mbid)] = artist
			return mbid, nil
		}
		var provider *Error
		if !errors.As(err, &provider) || provider.Code != "music_not_found" {
			return "", err
		}
		cache.addWarning(fmt.Sprintf("MusicBrainz has no artist with the ID %q tagged on %q; discovery searched by name instead.", mbid, group.album))
		break
	}
	var notFound error
	for _, candidate := range group.artistCandidates {
		mbid, err := s.matchArtistName(ctx, candidate, cache)
		if err == nil {
			return mbid, nil
		}
		var provider *Error
		if errors.As(err, &provider) && provider.Code == "music_artist_not_found" {
			if notFound == nil {
				notFound = err
			}
			continue
		}
		return "", err
	}
	if notFound != nil {
		return "", notFound
	}
	return "", &Error{Code: "music_artist_not_found", Message: fmt.Sprintf("The file tags do not identify the artist for %q. Add an artist, album-artist or MusicBrainz artist tag and run discovery again.", group.album)}
}

func (s *Service) matchArtistName(ctx context.Context, name string, cache *discoveryCache) (string, error) {
	key := normalizeArtistName(name)
	if ids, ok := cache.artistMatches[key]; ok {
		return uniqueArtist(ids, name)
	}
	queryName := name
	if isVariousArtistsKey(normalizeArtistName(name)) {
		queryName = "Various Artists"
	}
	ids := []string{}
	seen := map[string]bool{}
	for page := 1; page <= discoveryArtistSearchPages; page++ {
		results, err := s.client.SearchArtistName(ctx, queryName, page)
		if err != nil {
			return "", err
		}
		for _, artist := range results.Items {
			if artistNamesEqual(name, artist.Name) && !seen[artist.MusicBrainzID] {
				seen[artist.MusicBrainzID] = true
				ids = append(ids, artist.MusicBrainzID)
			}
		}
		if len(ids) > 1 || len(results.Items) == 0 || page >= results.TotalPages {
			break
		}
	}
	cache.artistMatches[key] = ids
	return uniqueArtist(ids, name)
}

func uniqueArtist(ids []string, name string) (string, error) {
	switch len(ids) {
	case 0:
		return "", &Error{Code: "music_artist_not_found", Message: fmt.Sprintf("No exact MusicBrainz artist match for %q. Tag the files with the MusicBrainz artist ID or add the artist first.", name)}
	case 1:
		return ids[0], nil
	default:
		return "", &Error{Code: "music_ambiguous_artist", Message: fmt.Sprintf("MusicBrainz has more than one exact artist match for %q. Tag the files with the MusicBrainz artist ID.", name)}
	}
}

func (s *Service) findDiscoveryAlbum(ctx context.Context, artistMBID string, group *discoverGroup, cache *discoveryCache) (Album, error) {
	albums, complete, err := s.client.SearchReleaseGroups(ctx, artistMBID, group.album)
	if err != nil {
		return Album{}, err
	}
	searchExact := exactTitleAlbums(albums, group.album)
	if complete {
		if album, ok, ambiguous := chooseGroupAlbum(searchExact, group); ok {
			return album, nil
		} else if ambiguous {
			return Album{}, ambiguousDiscoveryAlbumError(group)
		}
	}
	// The one-page search is either incomplete or did not contain an exact
	// title. Compare the bounded artist discography instead; this is still an
	// exact normalized title comparison, not a fuzzy match.
	discography, err := s.artistDiscography(ctx, artistMBID, cache)
	if err != nil {
		return Album{}, err
	}
	if album, ok, ambiguous := chooseGroupAlbum(exactTitleAlbums(discography, group.album), group); ok {
		return album, nil
	} else if ambiguous {
		return Album{}, ambiguousDiscoveryAlbumError(group)
	}
	if len(searchExact) == 1 {
		return searchExact[0], nil
	}
	if len(searchExact) > 1 {
		return Album{}, ambiguousDiscoveryAlbumError(group)
	}
	return Album{}, &Error{Code: "music_album_not_found", Message: fmt.Sprintf("MusicBrainz has no release group exactly titled %q by %q, including the artist discography. Tag the files with the MusicBrainz release-group ID or add the album manually.", group.album, group.artist)}
}

func (s *Service) artistDiscography(ctx context.Context, mbid string, cache *discoveryCache) ([]Album, error) {
	key := strings.ToLower(strings.TrimSpace(mbid))
	if albums, ok := cache.discographies[key]; ok {
		return albums, nil
	}
	albums, err := s.client.ArtistAlbums(ctx, mbid)
	if err != nil {
		return nil, err
	}
	cache.discographies[key] = albums
	return albums, nil
}

func exactTitleAlbums(albums []Album, title string) []Album {
	out := []Album{}
	seen := map[string]bool{}
	for _, album := range albums {
		if !matchNameEqual(album.Title, title) {
			continue
		}
		key := strings.ToLower(album.MusicBrainzID)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, album)
	}
	return out
}

func chooseGroupAlbum(exact []Album, group *discoverGroup) (Album, bool, bool) {
	if len(exact) == 0 {
		return Album{}, false, false
	}
	album, ok := chooseExactAlbum(exact, group.years)
	return album, ok, !ok
}

func ambiguousDiscoveryAlbumError(group *discoverGroup) error {
	return &Error{Code: "music_ambiguous_album", Message: fmt.Sprintf("MusicBrainz has more than one release group exactly titled %q by %q. Tag the files with the MusicBrainz release-group ID or a year.", group.album, group.artist)}
}

func (s *Service) saveDiscoveredAlbum(ctx context.Context, artistMBID string, album Album, cache *discoveryCache) (int64, int64, error) {
	if existing, err := s.store.AlbumByMusicBrainzID(ctx, album.MusicBrainzID); err == nil {
		if err := s.ensureAlbumTracks(ctx, existing.ID); err != nil {
			return existing.ArtistID, existing.ID, albumTracksUnavailableError(existing.Title, err)
		}
		return existing.ArtistID, existing.ID, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return 0, 0, err
	}
	artist, created, err := s.discoverArtist(ctx, artistMBID, cache)
	if err != nil {
		return 0, 0, err
	}
	if created {
		cache.addedArtists++
	}
	albumID, added, err := s.store.SaveAlbum(ctx, artist.ID, album)
	if err != nil {
		return 0, 0, err
	}
	if added {
		cache.addedAlbums++
	}
	if err := s.ensureAlbumTracks(ctx, albumID); err != nil {
		return artist.ID, albumID, albumTracksUnavailableError(album.Title, err)
	}
	return artist.ID, albumID, nil
}

func albumTracksUnavailableError(title string, err error) error {
	var provider *Error
	if errors.As(err, &provider) && strings.TrimSpace(provider.Message) != "" {
		return &Error{Code: "music_album_tracks_unavailable", Message: fmt.Sprintf("The album %q was added to the catalog, but its MusicBrainz tracks could not be loaded: %s", title, provider.Message)}
	}
	return &Error{Code: "music_album_tracks_unavailable", Message: fmt.Sprintf("The album %q was added to the catalog, but its track list could not be loaded. Open the album and retry.", title)}
}

func (s *Service) discoverArtist(ctx context.Context, mbid string, cache *discoveryCache) (Artist, bool, error) {
	key := strings.ToLower(strings.TrimSpace(mbid))
	if artist, ok := cache.artists[key]; ok {
		return artist, false, nil
	}
	if artist, ok := cache.pendingArtists[key]; ok {
		saved, err := s.store.SaveArtist(ctx, artist, nil)
		if err != nil {
			return Artist{}, false, err
		}
		delete(cache.pendingArtists, key)
		cache.artists[key] = saved
		return saved, true, nil
	}
	existing, err := s.store.ArtistByMusicBrainzID(ctx, mbid)
	if err == nil {
		cache.artists[key] = existing
		return existing, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Artist{}, false, err
	}
	artist, err := s.client.Artist(ctx, mbid)
	if err != nil {
		return Artist{}, false, err
	}
	saved, err := s.store.SaveArtist(ctx, artist, nil)
	if err != nil {
		return Artist{}, false, err
	}
	cache.artists[key] = saved
	return saved, true, nil
}

func (c *discoveryCache) addWarning(message string) {
	message = strings.TrimSpace(message)
	if message == "" {
		return
	}
	for _, existing := range c.warnings {
		if existing == message {
			return
		}
	}
	c.warnings = append(c.warnings, message)
}

func (s *Service) ensureAlbumTracks(ctx context.Context, albumID int64) error {
	var tracks int
	if err := s.store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM music_tracks WHERE album_id=?`, albumID).Scan(&tracks); err != nil {
		return err
	}
	if tracks > 0 {
		return nil
	}
	_, err := s.Album(ctx, albumID)
	return err
}

func chooseExactAlbum(albums []Album, years map[int]bool) (Album, bool) {
	if len(albums) == 1 {
		return albums[0], true
	}
	if len(years) == 0 {
		return Album{}, false
	}
	matched := []Album{}
	for _, album := range albums {
		if album.Year != nil && years[*album.Year] {
			matched = append(matched, album)
		}
	}
	if len(matched) == 1 {
		return matched[0], true
	}
	return Album{}, false
}

func (s *Service) markDiscoverGroup(ctx context.Context, group *discoverGroup, artistID, albumID int64, message string) error {
	for _, file := range group.files {
		if err := s.updateScanFileState(ctx, file.ID, "unresolved", artistID, albumID, 0, message); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) updateScanFileState(ctx context.Context, fileID int64, state string, artistID, albumID, trackID int64, message string) error {
	_, err := s.store.db.ExecContext(ctx, `UPDATE music_scan_files SET artist_id=?,album_id=?,track_id=?,state=?,error=?,updated_at=unixepoch() WHERE id=?`,
		nullableID(artistID), nullableID(albumID), nullableID(trackID), state, message, fileID)
	return err
}

func (s *Service) refreshScanCounts(ctx context.Context, scanID int64) (int, error) {
	var files, matched, unresolved int
	if err := s.store.db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(state='matched'),0), COALESCE(SUM(state='unresolved'),0) FROM music_scan_files WHERE scan_id=?`, scanID).Scan(&files, &matched, &unresolved); err != nil {
		return 0, err
	}
	if _, err := s.store.db.ExecContext(ctx, `UPDATE music_scans SET file_count=?,matched_count=?,unresolved_count=?,updated_at=unixepoch() WHERE id=?`, files, matched, unresolved, scanID); err != nil {
		return 0, err
	}
	return unresolved, nil
}

func discoveryTrackMessage(err error) string {
	var provider *Error
	if errors.As(err, &provider) {
		if provider.Code == "music_unmatched_track" {
			return "The album is in the catalog, but no track matches this file's MusicBrainz track ID, disc/track number or title. Correct the tags and run discovery again."
		}
		if strings.TrimSpace(provider.Message) != "" {
			return provider.Message
		}
	}
	return "The album matched but this file's track could not be matched. Correct the title, disc and track tags and run discovery again."
}

func discoveryStops(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	var provider *Error
	if errors.As(err, &provider) {
		switch provider.Code {
		case "music_not_found", "music_artist_not_found", "music_album_not_found",
			"music_ambiguous_artist", "music_ambiguous_album", "music_release_group_not_found",
			"music_album_tracks_unavailable", "music_unmatched_track":
			return false
		}
		return true
	}
	return false
}

func discoveryErrorMessage(err error) string {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "Discovery stopped before this album was checked. Run it again to continue."
	}
	var provider *Error
	if errors.As(err, &provider) && provider.Code != "music_not_found" &&
		provider.Code != "music_artist_not_found" && provider.Code != "music_album_not_found" &&
		provider.Code != "music_ambiguous_artist" && provider.Code != "music_ambiguous_album" &&
		provider.Code != "music_release_group_not_found" && provider.Code != "music_album_tracks_unavailable" {
		return provider.Message + " Discovery stopped for this album."
	}
	return err.Error()
}

func discoveryWarning(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "Discovery stopped early to stay within the MusicBrainz request limit. Run it again to continue."
	}
	if errors.Is(err, context.Canceled) {
		return "Discovery was cancelled."
	}
	return err.Error()
}
