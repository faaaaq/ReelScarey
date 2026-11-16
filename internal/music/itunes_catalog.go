package music

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const itunesSearchLimit = 20

type itunesCatalog struct {
	http         *http.Client
	mu           sync.Mutex
	next         time.Time
	blockedUntil time.Time
	interval     time.Duration
}

func newITunesCatalog() *itunesCatalog {
	return &itunesCatalog{interval: 3 * time.Second, http: &http.Client{
		Timeout:       7 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

type itunesItem struct {
	Wrapper  string `json:"wrapperType"`
	Kind     string `json:"kind"`
	Artist   string `json:"artistName"`
	Album    string `json:"collectionName"`
	AlbumID  int64  `json:"collectionId"`
	Count    int    `json:"trackCount"`
	Date     string `json:"releaseDate"`
	Title    string `json:"trackName"`
	Disc     int    `json:"discNumber"`
	Number   int    `json:"trackNumber"`
	Duration int    `json:"trackTimeMillis"`
}

func (c *itunesCatalog) request(ctx context.Context, endpoint string, query url.Values) ([]itunesItem, error) {
	// Reserve a request slot for the shared scan client. Apple's approximate
	// limit of 20 calls per minute includes album searches and track lookups.
	c.mu.Lock()
	now := time.Now()
	delay := max(c.next.Sub(now), 0)
	if now.Before(c.blockedUntil) {
		c.mu.Unlock()
		return nil, itunesLimited()
	}
	if deadline, ok := ctx.Deadline(); ok && !now.Add(delay).Before(deadline) {
		c.mu.Unlock()
		return nil, context.DeadlineExceeded
	}
	c.next = now.Add(delay + c.interval)
	c.mu.Unlock()
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
	}
	c.mu.Lock()
	blocked := time.Now().Before(c.blockedUntil)
	c.mu.Unlock()
	if blocked {
		return nil, itunesLimited()
	}
	query.Set("country", "US")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://itunes.apple.com"+endpoint+"?"+query.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, itunesUnavailable()
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable {
		c.mu.Lock()
		now := time.Now()
		if until := now.Add(parseRetryAfter(resp.Header.Get("Retry-After"), now)); until.After(c.blockedUntil) {
			c.blockedUntil = until
		}
		c.mu.Unlock()
		return nil, itunesLimited()
	}
	if resp.StatusCode != http.StatusOK {
		return nil, itunesUnavailable()
	}
	var body struct {
		Count   *int         `json:"resultCount"`
		Results []itunesItem `json:"results"`
	}
	const limit = 2 << 20
	raw, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil || len(raw) > limit || json.Unmarshal(raw, &body) != nil || body.Count == nil || *body.Count != len(body.Results) {
		return nil, &Error{Code: "music_itunes_unavailable", Message: "iTunes returned incomplete catalog data."}
	}
	return body.Results, nil
}

func itunesUnavailable() error {
	return &Error{Code: "music_itunes_unavailable", Message: "The iTunes catalog is unavailable. Try again later."}
}

func itunesLimited() error {
	return &Error{Code: "music_itunes_unavailable", Message: "iTunes is limiting requests. Try again later."}
}

func (s *Service) SearchITunesScanAlbums(ctx context.Context, query string) ([]ScanAlbumCandidate, bool, error) {
	query, ok := cleanText(query, 1001)
	if !ok {
		return nil, false, ErrInvalidInput
	}
	if s.itunes == nil {
		return nil, false, itunesUnavailable()
	}
	items, err := s.itunes.request(ctx, "/search", url.Values{
		"term": {query}, "media": {"music"}, "entity": {"album"}, "limit": {strconv.Itoa(itunesSearchLimit)},
	})
	if err != nil {
		return nil, false, err
	}
	result := make([]ScanAlbumCandidate, 0, len(items))
	seen := map[string]bool{}
	for _, item := range items {
		entry, ok := itunesAlbum(item)
		if ok && !seen[entry.ID] {
			seen[entry.ID] = true
			result = append(result, entry)
		}
	}
	return result, len(items) < itunesSearchLimit, nil
}

func itunesAlbum(item itunesItem) (ScanAlbumCandidate, bool) {
	title, titleOK := cleanText(item.Album, 500)
	artist, artistOK := cleanText(item.Artist, 500)
	if item.Wrapper != "collection" || item.AlbumID < 1 || item.Count < 1 || !titleOK || !artistOK {
		return ScanAlbumCandidate{}, false
	}
	return ScanAlbumCandidate{ID: strconv.FormatInt(item.AlbumID, 10), Title: title, Artist: artist, Year: yearFromText(item.Date), TrackCount: item.Count}, true
}

func (s *Service) PreviewITunesScanAlbum(ctx context.Context, id string) (ScanAlbumPreview, error) {
	albumID, err := strconv.ParseInt(id, 10, 64)
	if err != nil || albumID < 1 || strconv.FormatInt(albumID, 10) != id {
		return ScanAlbumPreview{}, ErrInvalidInput
	}
	if s.itunes == nil {
		return ScanAlbumPreview{}, itunesUnavailable()
	}
	items, err := s.itunes.request(ctx, "/lookup", url.Values{"id": {id}, "entity": {"song"}})
	if err != nil {
		return ScanAlbumPreview{}, err
	}
	var out ScanAlbumPreview
	positions := map[[2]int]bool{}
	for _, item := range items {
		if item.AlbumID != albumID {
			continue
		}
		if album, ok := itunesAlbum(item); ok {
			out.ScanAlbumCandidate = album
		}
		if item.Wrapper != "track" || item.Kind != "song" {
			continue
		}
		title, ok := cleanText(item.Title, 500)
		position := [2]int{item.Disc, item.Number}
		if !ok || item.Disc < 1 || item.Number < 1 || positions[position] {
			return ScanAlbumPreview{}, &Error{Code: "music_itunes_unavailable", Message: "iTunes returned an invalid tracklist."}
		}
		positions[position] = true
		out.Tracks = append(out.Tracks, Track{Title: title, Disc: item.Disc, Number: item.Number, DurationMS: max(0, item.Duration)})
	}
	if out.ID == "" || len(out.Tracks) != out.TrackCount {
		return ScanAlbumPreview{}, &Error{Code: "music_not_found", Message: "iTunes has no complete tracklist for this album in the US catalog."}
	}
	return out, nil
}

func (s *Service) discoverByITunes(ctx context.Context, scanID int64, group *discoverGroup, recovered map[int64]albumTrackMatch) (map[int64]albumTrackMatch, error) {
	matched := map[int64]albumTrackMatch{}
	if s.itunes == nil || len(group.artistMBIDs) > 0 || group.releaseGroupID != "" {
		return matched, nil
	}
	folders, order := discoveryFolders(group.files)
	var candidates []ScanAlbumCandidate
	lookedUp := false
	previews := map[string]ScanAlbumPreview{}
	for _, folder := range order {
		var pending []ScanFile
		for _, file := range folders[folder] {
			if _, ok := recovered[file.ID]; !ok {
				pending = append(pending, file)
			}
		}
		if len(pending) == 0 {
			continue
		}
		cohort, err := s.scanAlbumFolder(ctx, scanID, folder, group)
		if err != nil {
			return matched, err
		}
		if len(cohort) < 2 || len(cohort) > MaxScanCorrections || itunesHasMusicBrainzEvidence(cohort) {
			continue
		}
		if !lookedUp {
			var complete bool
			candidates, complete, err = s.SearchITunesScanAlbums(ctx, strings.TrimSpace(group.artist+" "+group.album))
			if err != nil || !complete {
				return matched, err
			}
			lookedUp = true
		}
		var chosen ScanAlbumPreview
		for _, album := range candidates {
			if album.TrackCount != len(cohort) || !matchNameEqual(album.Title, group.album) {
				continue
			}
			preview, ok := previews[album.ID]
			if !ok {
				preview, err = s.PreviewITunesScanAlbum(ctx, album.ID)
				if err != nil {
					return matched, err
				}
				previews[album.ID] = preview
			}
			if !matchNameEqual(preview.Title, group.album) || !sameAlbumTracklist(cohort, preview.Tracks, "") {
				continue
			}
			if chosen.ID != "" {
				chosen = ScanAlbumPreview{}
				break
			}
			chosen = preview
		}
		if chosen.ID == "" {
			continue
		}
		rows, err := s.saveITunesMatches(ctx, scanID, pending, cohort, chosen)
		if err != nil {
			var provider *Error
			if errors.As(err, &provider) && (provider.Code == "music_correction_conflict" || strings.HasPrefix(provider.Code, "music_ambiguous_") || provider.Code == "music_local_track") {
				continue
			}
			return matched, err
		}
		for id, track := range rows {
			matched[id] = track
		}
	}
	return matched, nil
}

func itunesHasMusicBrainzEvidence(files []ScanFile) bool {
	for _, file := range files {
		for _, id := range []string{file.MusicBrainzArtistID, albumArtistMusicBrainzID(file), file.MusicBrainzAlbumID,
			file.MusicBrainzReleaseGroupID, file.MusicBrainzTrackID, file.MusicBrainzRecordingID} {
			if genuineMBID(id) != "" {
				return true
			}
		}
	}
	return false
}

func (s *Service) saveITunesMatches(ctx context.Context, scanID int64, files, cohort []ScanFile, album ScanAlbumPreview) (map[int64]albumTrackMatch, error) {
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	catalog := newLocalCatalog(s)
	catalog.db = tx
	for i := range album.Tracks {
		album.Tracks[i].ID = int64(i + 1)
	}
	matches := make(map[int64]albumTrackMatch, len(files))
	for _, file := range files {
		id, ok := uniqueTrackByTitle(album.Tracks, file.Title)
		if !ok {
			return nil, ErrPlanChanged
		}
		track := album.Tracks[id-1]
		match, err := correctLocalTrack(ctx, catalog, file, album.Artist, album.Title, ScanCorrection{
			Title: track.Title, Disc: track.Disc, Number: track.Number,
		})
		if err != nil {
			return nil, err
		}
		if len(matches) == 0 {
			for _, sibling := range cohort {
				if sibling.AlbumID != nil && (sibling.State == "matched" || sibling.State == "registered") && *sibling.AlbumID != match.albumID {
					return nil, ErrPlanChanged
				}
			}
		}
		result, err := tx.ExecContext(ctx, `UPDATE music_scan_files SET artist_id=?,album_id=?,track_id=?,state='matched',error='',updated_at=unixepoch()
			WHERE id=? AND scan_id=? AND state='unresolved' AND EXISTS (SELECT 1 FROM music_scans WHERE id=? AND state<>'cancelled')`,
			match.artistID, match.albumID, match.trackID, file.ID, scanID, scanID)
		if err != nil {
			return nil, err
		}
		if changed, err := result.RowsAffected(); err != nil || changed != 1 {
			return nil, ErrPlanChanged
		}
		matches[file.ID] = match
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return matches, nil
}
