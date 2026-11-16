package music

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"path"
	"strconv"
	"strings"
)

type ScanAlbumCandidate struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Artist     string `json:"artist"`
	Year       *int   `json:"year"`
	TrackCount int    `json:"track_count,omitempty"`
}

type ScanAlbumPreview struct {
	ScanAlbumCandidate
	Tracks []Track `json:"tracks"`
}

const albumTitleSearchLimit = 15

func (s *Service) SearchScanAlbums(ctx context.Context, title string) ([]ScanAlbumCandidate, bool, error) {
	title, ok := cleanText(title, 500)
	if !ok {
		return nil, false, ErrInvalidInput
	}
	var response mbReleaseGroupSearch
	if err := s.client.request(ctx, "/release-group", url.Values{
		"query": {"releasegroup:" + quoteSearchValue(title)},
		"limit": {strconv.Itoa(albumTitleSearchLimit)},
	}, &response, 2<<20); err != nil {
		return nil, false, err
	}
	if response.Count < len(response.ReleaseGroups) || response.Offset != 0 || len(response.ReleaseGroups) > albumTitleSearchLimit {
		return nil, false, invalidResponse("album search")
	}
	results := make([]ScanAlbumCandidate, 0, len(response.ReleaseGroups))
	seen := map[string]bool{}
	for _, raw := range response.ReleaseGroups {
		album, err := normalizeAlbum(raw)
		if err != nil {
			return nil, false, err
		}
		if !seen[album.MusicBrainzID] {
			results = append(results, ScanAlbumCandidate{ID: album.MusicBrainzID, Title: album.Title, Artist: releaseGroupCredit(raw), Year: album.Year})
			seen[album.MusicBrainzID] = true
		}
	}
	return results, response.Count == len(response.ReleaseGroups), nil
}

func releaseGroupCredit(raw mbReleaseGroup) string {
	var name strings.Builder
	for _, credit := range raw.ArtistCredit {
		name.WriteString(firstNonEmpty(credit.Name, credit.Artist.Name))
		name.WriteString(credit.JoinPhrase)
	}
	return strings.TrimSpace(name.String())
}

func (s *Service) PreviewScanAlbum(ctx context.Context, id string) (ScanAlbumPreview, error) {
	id = genuineMBID(id)
	if id == "" {
		return ScanAlbumPreview{}, ErrInvalidInput
	}
	var raw mbReleaseGroup
	if err := s.client.request(ctx, "/release-group/"+id, url.Values{"inc": {"artist-credits"}}, &raw, 2<<20); err != nil {
		return ScanAlbumPreview{}, err
	}
	if !strings.EqualFold(raw.ID, id) {
		return ScanAlbumPreview{}, invalidResponse("release group details")
	}
	album, err := normalizeAlbum(raw)
	if err != nil {
		return ScanAlbumPreview{}, err
	}
	tracks, err := s.scanAlbumTracks(ctx, id)
	if err != nil {
		return ScanAlbumPreview{}, err
	}
	return ScanAlbumPreview{
		ScanAlbumCandidate: ScanAlbumCandidate{ID: id, Title: album.Title, Artist: releaseGroupCredit(raw), Year: album.Year},
		Tracks:             tracks,
	}, nil
}

// Use the same track list that will be used when saving the match.
func (s *Service) scanAlbumTracks(ctx context.Context, mbid string) ([]Track, error) {
	album, err := s.store.AlbumByMusicBrainzID(ctx, mbid)
	if err == nil && album.TrackCount > 0 {
		return s.store.Tracks(ctx, album.ID)
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	return s.providerAlbumTracks(ctx, mbid)
}

// Album title alone is insufficient. This fallback requires one complete,
// unambiguous track list in a single scanned folder, including files already
// matched on the scan. Number tags and the provider's primary artist name are
// not required. A conflicting provider ID or a duplicate title fails closed.
func (s *Service) recoverAlbumByTracklist(ctx context.Context, scanID int64, group *discoverGroup, cache *discoveryCache) (map[int64]albumTrackMatch, error) {
	matches := map[int64]albumTrackMatch{}
	folders, order := discoveryFolders(group.files)
	for _, folder := range order {
		if err := ctx.Err(); err != nil {
			return matches, err
		}
		cohort, err := s.scanAlbumFolder(ctx, scanID, folder, group)
		if err != nil {
			return matches, err
		}
		if len(cohort) < 2 {
			continue
		}
		candidates, complete, err := s.SearchScanAlbums(ctx, group.album)
		if err != nil {
			return matches, err
		}
		if !complete {
			return matches, nil
		}
		var chosen string
		for _, candidate := range candidates {
			if !matchNameEqual(candidate.Title, group.album) {
				continue
			}
			tracks, err := s.scanAlbumTracks(ctx, candidate.ID)
			if err != nil {
				return matches, err
			}
			if !sameAlbumTracklist(cohort, tracks, candidate.ID) {
				continue
			}
			if chosen != "" {
				return matches, &Error{Code: "music_ambiguous_tracklist", Message: "More than one MusicBrainz album has this exact tracklist. Use Correct match to choose the edition."}
			}
			chosen = candidate.ID
		}
		if chosen == "" {
			continue
		}
		album, artistMBID, err := s.client.ReleaseGroup(ctx, chosen)
		if err != nil {
			return matches, err
		}
		if len(group.artistMBIDs) > 0 && !group.artistMBIDs[strings.ToLower(artistMBID)] {
			continue
		}
		artistID, albumID, err := s.saveDiscoveredAlbum(ctx, artistMBID, album, cache)
		if err != nil {
			return matches, err
		}
		tracks, err := s.store.Tracks(ctx, albumID)
		if err != nil {
			return matches, err
		}
		if !sameAlbumTracklist(cohort, tracks, chosen) {
			continue
		}
		for _, file := range folders[folder] {
			id, _ := uniqueTrackByTitle(tracks, file.Title)
			matches[file.ID] = albumTrackMatch{artistID, albumID, id}
		}
	}
	return matches, nil
}

func discoveryFolders(files []ScanFile) (map[string][]ScanFile, []string) {
	folders := map[string][]ScanFile{}
	var order []string
	for _, file := range files {
		folder := path.Dir(file.RelativePath)
		if folders[folder] == nil {
			order = append(order, folder)
		}
		folders[folder] = append(folders[folder], file)
	}
	return folders, order
}

type albumTrackMatch struct {
	artistID, albumID, trackID int64
}

func (s *Service) scanAlbumFolder(ctx context.Context, scanID int64, folder string, group *discoverGroup) ([]ScanFile, error) {
	query := `SELECT ` + scanFileColumns + ` FROM music_scan_files WHERE scan_id=?`
	args := []any{scanID}
	if folder != "." {
		query += ` AND relative_path>=? AND relative_path<?`
		args = append(args, folder+"/", folder+"0")
	}
	rows, err := s.store.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var files []ScanFile
	for rows.Next() {
		file, err := scanScanFile(rows)
		if err != nil {
			return nil, err
		}
		if path.Dir(file.RelativePath) != folder {
			continue
		}
		if !matchNameEqual(group.album, file.Album) || !artistNamesEqual(group.artist, firstNonEmpty(file.AlbumArtist, file.Artist)) {
			return nil, nil
		}
		files = append(files, file)
	}
	return files, rows.Err()
}

func sameAlbumTracklist(files []ScanFile, tracks []Track, releaseGroupID string) bool {
	if len(files) < 2 || len(files) != len(tracks) {
		return false
	}
	// Provider tracks are not in the local catalog yet and have no local IDs.
	comparable := append([]Track(nil), tracks...)
	for i := range comparable {
		comparable[i].ID = int64(i + 1)
	}
	used := map[int64]bool{}
	for _, file := range files {
		id, unique := uniqueTrackByTitle(comparable, file.Title)
		if groupID := genuineMBID(file.MusicBrainzReleaseGroupID); groupID != "" && groupID != releaseGroupID {
			return false
		}
		if !unique || used[id] {
			return false
		}
		track := comparable[id-1]
		if trackID := genuineMBID(file.MusicBrainzTrackID); trackID != "" && trackID != track.MusicBrainzID {
			return false
		}
		if recording := genuineMBID(file.MusicBrainzRecordingID); recording != "" && recording != track.RecordingID {
			return false
		}
		used[id] = true
	}
	return true
}
