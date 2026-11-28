package music

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Metadata refresh re-resolves the provider-sourced presentation fields of the
// catalog: the artist image, the album cover and the canonical album title
// and year. Refresh never touches MusicBrainz identifiers, monitoring state,
// user-corrected local rows or any media file; a wrong cached picture such as
// a same-named stranger's is replaced or cleared, never kept silently.

const (
	// refreshWorkBudget bounds one reviewed refresh request's provider work.
	refreshWorkBudget = 25 * time.Second
	// refreshArtistPause paces library-wide batches toward the public APIs.
	refreshArtistPause = 150 * time.Millisecond
	// maxRefreshArtists bounds one library-wide refresh batch.
	maxRefreshArtists = 10
)

// ArtistRefreshResult reports what one artist refresh changed.
type ArtistRefreshResult struct {
	ArtistID      int64  `json:"artist_id"`
	Name          string `json:"name"`
	ArtistUpdated bool   `json:"artist_updated"`
	ImageUpdated  bool   `json:"image_updated"`
	ImageCleared  bool   `json:"image_cleared"`
	AlbumsUpdated int    `json:"albums_updated"`
	Message       string `json:"message"`
}

// AlbumRefreshResult reports what one album refresh changed.
type AlbumRefreshResult struct {
	AlbumID      int64  `json:"album_id"`
	Title        string `json:"title"`
	AlbumUpdate  bool   `json:"album_updated"`
	CoverUpdate  bool   `json:"cover_updated"`
	CoverCleared bool   `json:"cover_cleared"`
	Message      string `json:"message"`
}

// LibraryRefreshResult is one bounded batch of a library-wide refresh. The
// cursor carries across requests until Done is true.
type LibraryRefreshResult struct {
	Processed      int    `json:"processed"`
	ArtistsUpdated int    `json:"artists_updated"`
	ImagesCleared  int    `json:"images_cleared"`
	AlbumsUpdated  int    `json:"albums_updated"`
	Failed         int    `json:"failed"`
	Cursor         int64  `json:"cursor"`
	Done           bool   `json:"done"`
	Message        string `json:"message"`
}

// RefreshArtistMetadata re-resolves one artist's canonical details from
// MusicBrainz and its image from the verified Deezer identity, then refreshes
// the covers of the artist's catalog albums.
func (s *Service) RefreshArtistMetadata(ctx context.Context, id int64) (ArtistRefreshResult, error) {
	result := ArtistRefreshResult{ArtistID: id}
	if id < 1 {
		return result, ErrInvalidInput
	}
	var name, mbid, imageURL string
	if err := s.store.db.QueryRowContext(ctx, `SELECT name,musicbrainz_id,image_url FROM music_artists WHERE id=?`, id).
		Scan(&name, &mbid, &imageURL); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return result, ErrNotFound
		}
		return result, err
	}
	result.Name = name
	base := s.topBaseURL
	if base == "" {
		base = defaultTopTracksBaseURL
	}

	// Canonical artist details come from MusicBrainz when the row has a
	// genuine identity. The identifier itself is never rewritten.
	if ValidMBID(mbid) && s.client != nil {
		artist, err := s.client.Artist(ctx, mbid)
		if err == nil {
			if changed := s.updateArtistDetails(ctx, id, artist); changed {
				result.ArtistUpdated = true
				if saved, saveErr := s.store.Artist(ctx, id); saveErr == nil {
					result.Name = saved.Name
				}
			}
		} else if !providerSoftError(err) {
			return result, err
		}
	}

	// The image is resolved from the verified Deezer identity. When the
	// catalog contradicts every same-named candidate, the stored picture —
	// which a wrong name match may have written — is cleared instead of
	// silently kept.
	picture, verified, err := s.refreshArtistPicture(ctx, base, name, imageURL)
	if err != nil {
		return result, err
	}
	switch {
	case picture != "" && picture != imageURL:
		if _, err := s.store.db.ExecContext(ctx, `UPDATE music_artists SET image_url=?,updated_at=unixepoch() WHERE id=?`, picture, id); err != nil {
			return result, err
		}
		result.ImageUpdated = true
	case picture == "" && verified == pictureStateRefused && imageURL != "":
		if _, err := s.store.db.ExecContext(ctx, `UPDATE music_artists SET image_url='',updated_at=unixepoch() WHERE id=?`, id); err != nil {
			return result, err
		}
		result.ImageCleared = true
	}
	s.removeCachedArtwork("artist", id)
	if picture != "" {
		if err := s.downloadArtwork(ctx, "artist", id, picture); err != nil {
			// The catalog row is already honest; a failed download simply
			// leaves the image to the normal lazy fetch path.
			result.Message = "The new artist image could not be downloaded; it will be fetched on the next page view."
		}
	}

	albums, err := s.store.Albums(ctx, id)
	if err != nil {
		return result, err
	}
	verifiedID, _ := s.verifiedDeezerArtistID(ctx, base, name)
	verifiedCovers := map[string]string{}
	if verifiedID > 0 {
		if covers, err := deezerArtistAlbumCovers(ctx, base, verifiedID); err == nil {
			verifiedCovers = covers
		}
	}
	for _, album := range albums {
		updated, _, err := s.refreshAlbumCover(ctx, base, album.ID, name, verifiedID, verifiedCovers)
		if err != nil {
			if providerSoftError(err) {
				result.Message = strings.TrimSpace(result.Message + " Some album covers could not be refreshed because a provider was unavailable.")
				continue
			}
			return result, err
		}
		if updated {
			result.AlbumsUpdated++
		}
	}
	if result.Message == "" && !result.ArtistUpdated && !result.ImageUpdated && !result.ImageCleared && result.AlbumsUpdated == 0 {
		result.Message = "The stored metadata was already current."
	}
	return result, nil
}

// pictureState reports how the artist image resolution ended.
type pictureState int

const (
	pictureStateNone pictureState = iota
	pictureStateVerified
	pictureStateRefused
)

// refreshArtistPicture re-resolves the artist picture. The stored URL is
// ignored on refresh: a previously wrong name match must not anchor the
// result.
func (s *Service) refreshArtistPicture(ctx context.Context, base, name, stored string) (string, pictureState, error) {
	providerID, verified, err := s.resolveDeezerArtist(ctx, base, name)
	if err != nil {
		return "", pictureStateNone, err
	}
	_, verifiable, err := s.catalogAlbumTitles(ctx, name)
	if err != nil {
		return "", pictureStateNone, err
	}
	if providerID == 0 {
		if verifiable {
			// The saved catalog's albums refute every same-named candidate;
			// any stored picture came from an unverifiable match.
			return "", pictureStateRefused, nil
		}
		return "", pictureStateNone, nil
	}
	if !verifiable { // Nothing in the catalog contradicts the name match; the best
		// same-named candidate is the only available evidence.
		picture, err := deezerArtistPictureByID(ctx, base, providerID)
		return picture, pictureStateNone, err
	}
	if !verified {
		return "", pictureStateRefused, nil
	}
	picture, err := deezerArtistPictureByID(ctx, base, providerID)
	return picture, pictureStateVerified, err
}

// verifiedDeezerArtistID returns the corroborated Deezer artist for a catalog
// artist name, or zero when the name cannot be verified.
func (s *Service) verifiedDeezerArtistID(ctx context.Context, base, name string) (int64, error) {
	providerID, verified, err := s.resolveDeezerArtist(ctx, base, name)
	if err != nil || !verified {
		return 0, err
	}
	return providerID, nil
}

// updateArtistDetails writes the canonical MusicBrainz fields onto the catalog
// row when they changed. It never rewrites the identifier or monitoring.
func (s *Service) updateArtistDetails(ctx context.Context, id int64, artist Artist) bool {
	var name, sortName, disambiguation, overview string
	var nameOverridden bool
	if err := s.store.db.QueryRowContext(ctx, `SELECT name,name_overridden,sort_name,disambiguation,overview FROM music_artists WHERE id=?`, id).
		Scan(&name, &nameOverridden, &sortName, &disambiguation, &overview); err != nil {
		return false
	}
	if nameOverridden {
		artist.Name = name
	}
	if name == artist.Name && sortName == artist.SortName &&
		disambiguation == artist.Disambiguation && overview == artist.Overview {
		return false
	}
	if _, err := s.store.db.ExecContext(ctx, `UPDATE music_artists SET name=CASE WHEN name_overridden=0 THEN ? ELSE name END,
		sort_name=COALESCE(json_extract(editor_fields,'$.sort_name'),?),
		disambiguation=COALESCE(json_extract(editor_fields,'$.disambiguation'),?),
		overview=COALESCE(json_extract(editor_fields,'$.overview'),?),updated_at=unixepoch() WHERE id=?`,
		artist.Name, artist.SortName, artist.Disambiguation, artist.Overview, id); err != nil {
		return false
	}
	return true
}

// RefreshAlbumMetadata re-resolves one album's cover art and, for a canonical
// MusicBrainz album, its title and year. The identifier is never rewritten.
func (s *Service) RefreshAlbumMetadata(ctx context.Context, id int64) (AlbumRefreshResult, error) {
	result := AlbumRefreshResult{AlbumID: id}
	if id < 1 {
		return result, ErrInvalidInput
	}
	var title, coverURL, mbid, artistName string
	if err := s.store.db.QueryRowContext(ctx, `SELECT a.title,a.cover_url,a.musicbrainz_id,ar.name
		FROM music_albums a JOIN music_artists ar ON ar.id=a.artist_id WHERE a.id=?`, id).
		Scan(&title, &coverURL, &mbid, &artistName); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return result, ErrNotFound
		}
		return result, err
	}
	result.Title = title
	base := s.topBaseURL
	if base == "" {
		base = defaultTopTracksBaseURL
	}

	// Canonical album details come from the MusicBrainz release group when
	// the row carries one. Local tag-derived rows keep their saved titles.
	if ValidMBID(mbid) && s.client != nil {
		album, _, err := s.client.ReleaseGroup(ctx, mbid)
		if err == nil {
			var currentYear any
			var currentTitle, primaryType string
			var titleOverridden, yearOverridden bool
			if err := s.store.db.QueryRowContext(ctx, `SELECT title,year,primary_type,title_overridden,year_overridden FROM music_albums WHERE id=?`, id).
				Scan(&currentTitle, &currentYear, &primaryType, &titleOverridden, &yearOverridden); err == nil {
				year := any(nil)
				if album.Year != nil {
					year = *album.Year
				}
				sameYear := false
				if currentYearInt, ok := currentYear.(int64); ok && album.Year != nil {
					sameYear = int64(*album.Year) == currentYearInt
				} else if !ok {
					sameYear = album.Year == nil
				}
				nextTitle := album.Title
				if titleOverridden {
					nextTitle = currentTitle
				}
				nextYear := year
				if yearOverridden {
					nextYear = currentYear
					sameYear = true
				}
				if currentTitle != nextTitle || !sameYear {
					if _, err := s.store.db.ExecContext(ctx, `UPDATE music_albums SET title=CASE WHEN title_overridden=0 THEN ? ELSE title END,
						year=CASE WHEN year_overridden=0 THEN ? ELSE year END,updated_at=unixepoch() WHERE id=?`, nextTitle, nextYear, id); err == nil {
						result.AlbumUpdate = true
						result.Title = nextTitle
					}
				}
			}
		} else if !providerSoftError(err) {
			return result, err
		}
	}

	updated, cleared, err := s.refreshAlbumCover(ctx, base, id, artistName, 0, nil)
	if err != nil {
		return result, err
	}
	result.CoverUpdate = updated
	result.CoverCleared = cleared
	if result.Message == "" && !result.AlbumUpdate && !result.CoverUpdate && !result.CoverCleared {
		result.Message = "The stored metadata was already current."
	}
	return result, nil
}

// refreshAlbumCover re-resolves one album cover. The verified Deezer artist's
// pre-fetched cover list is preferred, then the artist-and-title search, then
// the Cover Art Archive for a canonical release group. The stored cover is
// ignored so a wrong cached cover can be replaced.
func (s *Service) refreshAlbumCover(ctx context.Context, base string, albumID int64, artistName string, verifiedDeezerID int64, verifiedCovers map[string]string) (bool, bool, error) {
	var title, coverURL, mbid string
	if err := s.store.db.QueryRowContext(ctx, `SELECT title,cover_url,musicbrainz_id FROM music_albums WHERE id=?`, albumID).
		Scan(&title, &coverURL, &mbid); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, false, nil
		}
		return false, false, err
	}
	picture := ""
	if verifiedDeezerID > 0 {
		if verifiedCovers != nil {
			picture = verifiedCovers[compactMatchName(title)]
		} else if cover, err := deezerArtistAlbumCovers(ctx, base, verifiedDeezerID); err == nil {
			picture = cover[compactMatchName(title)]
		} else if !providerSoftError(err) {
			return false, false, err
		}
	}
	if picture == "" {
		if cover, err := deezerAlbumCover(ctx, base, artistName, title); err != nil {
			if !providerSoftError(err) {
				return false, false, err
			}
		} else {
			picture = cover
		}
	}
	if picture == "" && ValidMBID(mbid) {
		picture = coverURLFor(mbid)
	}
	switch {
	case picture != "" && picture != coverURL:
		if _, err := s.store.db.ExecContext(ctx, `UPDATE music_albums SET cover_url=?,updated_at=unixepoch() WHERE id=?`, picture, albumID); err != nil {
			return false, false, err
		}
		s.removeCachedArtwork("album", albumID)
		if err := s.downloadArtwork(ctx, "album", albumID, picture); err != nil {
			// The row is honest; the image lazy-loads on the next view.
			return true, false, nil
		}
		return true, false, nil
	case picture == "" && coverURL != "":
		// No provider can supply a cover for this album; a stored cover that
		// no provider supports is dropped so the placeholder shows instead.
		if _, err := s.store.db.ExecContext(ctx, `UPDATE music_albums SET cover_url='',updated_at=unixepoch() WHERE id=?`, albumID); err != nil {
			return false, false, err
		}
		s.removeCachedArtwork("album", albumID)
		return false, true, nil
	}
	return false, false, nil
}

// RefreshLibraryMetadataBatch refreshes one bounded batch of artists (with
// their album covers) for a library-wide reviewed refresh. The cursor is the
// last processed artist ID; Done reports that the whole catalog is done.
// Progress is resumable: each batch is independent and idempotent.
func (s *Service) RefreshLibraryMetadataBatch(ctx context.Context, cursor int64) (LibraryRefreshResult, error) {
	result := LibraryRefreshResult{Cursor: cursor}
	if cursor < 0 {
		return result, ErrInvalidInput
	}
	deadline := time.Now().Add(refreshWorkBudget)
	rows, err := s.store.db.QueryContext(ctx, `SELECT id FROM music_artists WHERE id>? ORDER BY id LIMIT ?`, cursor, maxRefreshArtists)
	if err != nil {
		return result, err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return result, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return result, err
	}
	rows.Close()
	if len(ids) == 0 {
		result.Done = true
		result.Message = "Every artist in the catalog has been refreshed."
		return result, nil
	}
	stopped := false
	for _, id := range ids {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		if time.Now().After(deadline) {
			stopped = true
			break
		}
		refreshCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		refresh, err := s.RefreshArtistMetadata(refreshCtx, id)
		cancel()
		if err != nil {
			if providerSoftError(err) {
				result.Failed++
			} else {
				return result, err
			}
		}
		result.Processed++
		result.Cursor = id
		result.ArtistsUpdated += boolToInt(refresh.ArtistUpdated || refresh.ImageUpdated)
		result.ImagesCleared += boolToInt(refresh.ImageCleared)
		result.AlbumsUpdated += refresh.AlbumsUpdated
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		case <-time.After(refreshArtistPause):
		}
	}
	if !stopped {
		// Every fetched artist was processed; a follow-up batch discovers
		// whether more artists exist.
		result.Done = len(ids) < maxRefreshArtists
		if result.Done {
			result.Message = "Every artist in the catalog has been refreshed."
		}
	}
	return result, nil
}

// removeCachedArtwork deletes the cached artwork files for one item so the
// next view fetches the refreshed image instead of serving the old cache.
func (s *Service) removeCachedArtwork(kind string, id int64) {
	if s.artworkDir == "" {
		return
	}
	for _, ext := range []string{".jpg", ".png", ".webp"} {
		_ = os.Remove(filepath.Join(s.artworkDir, kind+"-"+strconv.FormatInt(id, 10)+ext))
	}
}

// providerSoftError reports provider failures a refresh can continue past:
// an unreachable provider must not abort a library-wide refresh.
func providerSoftError(err error) bool {
	if err == nil {
		return false
	}
	var provider *Error
	if errors.As(err, &provider) {
		switch provider.Code {
		case "music_unavailable", "music_rate_limited", "music_not_found",
			"music_invalid_request", "music_top_tracks_unavailable",
			"music_itunes_unavailable", "music_itunes_rate_limited":
			return true
		}
	}
	return errors.Is(err, context.DeadlineExceeded)
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
