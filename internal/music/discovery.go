package music

import (
	"context"
	"database/sql"
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
	releaseGroupID string
	artist         string
	album          string
	years          map[int]bool
	files          []ScanFile
}

type discoveryCache struct {
	artists       map[string]Artist
	artistMatches map[string][]string
	addedArtists  int
	addedAlbums   int
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
		artists:       map[string]Artist{},
		artistMatches: map[string][]string{},
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
			if saveErr := s.markDiscoverGroup(dbCtx, group, discoveryErrorMessage(err)); saveErr != nil {
				return result, saveErr
			}
			if discoveryStops(err) {
				result.Warnings = append(result.Warnings, discoveryWarning(err))
				stopped = true
			}
			continue
		}
		for _, file := range group.files {
			trackID, err := s.matchTrack(dbCtx, file, albumID)
			if err != nil {
				if saveErr := s.updateScanFileState(dbCtx, file.ID, "unresolved", 0, 0, 0, err.Error()); saveErr != nil {
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
// album title is the only accepted fallback.
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
		if !artistOK || !albumOK || normalizeMatchName(artist) == "" || normalizeMatchName(album) == "" {
			message := "The file has no album artist and album tags. Add tags or a MusicBrainz release-group ID and scan again."
			if file.Error != "" {
				message = file.Error + " Discovery needs album artist and album tags, or a MusicBrainz release-group ID."
			}
			if err := s.updateScanFileState(ctx, file.ID, "unresolved", 0, 0, 0, message); err != nil {
				return nil, err
			}
			continue
		}
		key := "tags:" + normalizeMatchName(artist) + "\x00" + normalizeMatchName(album)
		if position, ok := index[key]; ok {
			groups[position].files = append(groups[position].files, file)
			if file.Year != nil {
				groups[position].years[*file.Year] = true
			}
			continue
		}
		group := &discoverGroup{artist: artist, album: album, years: map[int]bool{}, files: []ScanFile{file}}
		if file.Year != nil {
			group.years[*file.Year] = true
		}
		index[key] = len(groups)
		groups = append(groups, group)
	}
	return groups, nil
}

func (s *Service) discoverGroupAlbum(ctx context.Context, group *discoverGroup, cache *discoveryCache) (int64, int64, error) {
	if group.releaseGroupID != "" {
		if existing, err := s.store.AlbumByMusicBrainzID(ctx, group.releaseGroupID); err == nil {
			if err := s.ensureAlbumTracks(ctx, existing.ID); err != nil {
				return 0, 0, err
			}
			return existing.ArtistID, existing.ID, nil
		} else if !errors.Is(err, sql.ErrNoRows) {
			return 0, 0, err
		}
		album, artistID, err := s.client.ReleaseGroup(ctx, group.releaseGroupID)
		if err != nil {
			var provider *Error
			if errors.As(err, &provider) && provider.Code == "music_not_found" {
				return 0, 0, &Error{Code: "music_release_group_not_found", Message: "MusicBrainz has no release group with this MusicBrainz ID. Check the tag and scan again."}
			}
			return 0, 0, err
		}
		return s.saveDiscoveredAlbum(ctx, artistID, album, cache)
	}
	artistID, err := s.matchArtist(ctx, group.artist, cache)
	if err != nil {
		return 0, 0, err
	}
	albums, complete, err := s.client.SearchReleaseGroups(ctx, artistID, group.album)
	if err != nil {
		return 0, 0, err
	}
	if !complete {
		return 0, 0, &Error{Code: "music_ambiguous_album", Message: fmt.Sprintf("MusicBrainz returned too many albums to confirm %q by %q. Tag the files with a release-group ID.", group.album, group.artist)}
	}
	title := normalizeMatchName(group.album)
	exact := []Album{}
	for _, album := range albums {
		if normalizeMatchName(album.Title) == title {
			exact = append(exact, album)
		}
	}
	if len(exact) == 0 {
		return 0, 0, &Error{Code: "music_album_not_found", Message: fmt.Sprintf("MusicBrainz has no album exactly titled %q by %q. Tag the files with a release-group ID or add the album manually.", group.album, group.artist)}
	}
	album, ok := chooseExactAlbum(exact, group.years)
	if !ok {
		return 0, 0, &Error{Code: "music_ambiguous_album", Message: fmt.Sprintf("MusicBrainz has more than one exact album match for %q by %q. Tag the files with a release-group ID or year.", group.album, group.artist)}
	}
	return s.saveDiscoveredAlbum(ctx, artistID, album, cache)
}

func (s *Service) matchArtist(ctx context.Context, name string, cache *discoveryCache) (string, error) {
	key := normalizeMatchName(name)
	if ids, ok := cache.artistMatches[key]; ok {
		return uniqueArtist(ids, name)
	}
	results, err := s.client.SearchArtists(ctx, name, 1)
	if err != nil {
		return "", err
	}
	ids := []string{}
	seen := map[string]bool{}
	for _, artist := range results.Items {
		if normalizeMatchName(artist.Name) == key && !seen[artist.MusicBrainzID] {
			seen[artist.MusicBrainzID] = true
			ids = append(ids, artist.MusicBrainzID)
		}
	}
	cache.artistMatches[key] = ids
	return uniqueArtist(ids, name)
}

func uniqueArtist(ids []string, name string) (string, error) {
	switch len(ids) {
	case 0:
		return "", &Error{Code: "music_artist_not_found", Message: fmt.Sprintf("No exact MusicBrainz artist match for %q. Tag the files with MusicBrainz IDs or add the artist first.", name)}
	case 1:
		return ids[0], nil
	default:
		return "", &Error{Code: "music_ambiguous_artist", Message: fmt.Sprintf("MusicBrainz has more than one exact artist match for %q. Tag the files with MusicBrainz IDs.", name)}
	}
}

func (s *Service) saveDiscoveredAlbum(ctx context.Context, artistMBID string, album Album, cache *discoveryCache) (int64, int64, error) {
	if existing, err := s.store.AlbumByMusicBrainzID(ctx, album.MusicBrainzID); err == nil {
		if err := s.ensureAlbumTracks(ctx, existing.ID); err != nil {
			return 0, 0, err
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
		return 0, 0, err
	}
	return artist.ID, albumID, nil
}

func (s *Service) discoverArtist(ctx context.Context, mbid string, cache *discoveryCache) (Artist, bool, error) {
	key := strings.ToLower(strings.TrimSpace(mbid))
	if artist, ok := cache.artists[key]; ok {
		return artist, false, nil
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

func (s *Service) markDiscoverGroup(ctx context.Context, group *discoverGroup, message string) error {
	for _, file := range group.files {
		if err := s.updateScanFileState(ctx, file.ID, "unresolved", 0, 0, 0, message); err != nil {
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

func normalizeMatchName(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

func discoveryStops(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	var provider *Error
	if errors.As(err, &provider) {
		switch provider.Code {
		case "music_not_found", "music_artist_not_found", "music_album_not_found",
			"music_ambiguous_artist", "music_ambiguous_album", "music_release_group_not_found":
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
		provider.Code != "music_release_group_not_found" {
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
