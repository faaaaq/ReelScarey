package music

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
)

// Service combines the fixed MusicBrainz provider with the local catalog,
// scanning, reviewed catalog discovery and reviewed file edits. Media writes
// happen only in ApplyEdits, which stages, verifies, publishes without
// overwrite and retains a backup before a tag rewrite.
type Service struct {
	client         *Client
	store          *Store
	mediaDir       string
	downloadsDir   string
	prober         AudioProber
	scheduleRunner ScheduleRunner
	fileMu         sync.Mutex
	scheduleMu     sync.Mutex
}

func (s *Service) SetDownloadsDir(dir string) { s.downloadsDir = dir }

func NewService(db *sql.DB, mediaDir string) *Service {
	return &Service{client: New(), store: NewStore(db), mediaDir: mediaDir, prober: FFprobeTagger{}}
}

func NewServiceWithClient(db *sql.DB, client *Client, mediaDir string) *Service {
	return &Service{client: client, store: NewStore(db), mediaDir: mediaDir, prober: FFprobeTagger{}}
}

func (s *Service) SetProber(prober AudioProber) {
	if prober != nil {
		s.prober = prober
	}
}

// RecoverScans marks scans interrupted by a restart so they never appear to be
// running forever.
func (s *Service) RecoverScans(ctx context.Context) error {
	_, err := s.store.db.ExecContext(ctx, `UPDATE music_scans SET state='failed', error='The scan was interrupted by a restart.', updated_at=unixepoch() WHERE state='scanning';
		UPDATE music_edit_jobs SET state='failed',error='The edit was interrupted. Inspect the source, output and retained files before retrying.',updated_at=unixepoch() WHERE state='running'`)
	return err
}

func (s *Service) SearchArtists(ctx context.Context, query string, page int) (SearchResults, error) {
	if !ValidQuery(query, page) {
		return SearchResults{}, ErrInvalidInput
	}
	results, err := s.client.SearchArtists(ctx, strings.TrimSpace(query), page)
	if err != nil {
		return SearchResults{}, err
	}
	for i := range results.Items {
		if _, err := s.store.ArtistByMusicBrainzID(ctx, results.Items[i].MusicBrainzID); err == nil {
			results.Items[i].InLibrary = true
		} else if !errors.Is(err, sql.ErrNoRows) {
			return SearchResults{}, err
		}
	}
	return results, nil
}

// AddArtist is idempotent: a repeated add returns the saved artist without
// contacting MusicBrainz, so a retry survives a provider outage.
func (s *Service) AddArtist(ctx context.Context, mbid string, monitored bool) (Artist, bool, error) {
	if !ValidMBID(mbid) {
		return Artist{}, false, ErrInvalidInput
	}
	if existing, err := s.store.ArtistByMusicBrainzID(ctx, mbid); err == nil {
		return existing, false, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Artist{}, false, err
	}
	artist, err := s.client.Artist(ctx, mbid)
	if err != nil {
		return Artist{}, false, err
	}
	albums, err := s.client.ArtistAlbums(ctx, mbid)
	if err != nil {
		return Artist{}, false, err
	}
	artist.Monitored = monitored
	saved, err := s.store.SaveArtist(ctx, artist, albums)
	if err != nil {
		return Artist{}, false, err
	}
	return saved, true, nil
}

func (s *Service) ListArtists(ctx context.Context, query string, page int) (ArtistPage, error) {
	return s.store.ListArtists(ctx, query, page)
}

func (s *Service) ListFiles(ctx context.Context, page int) (MusicFilePage, error) {
	return s.store.ListFiles(ctx, page)
}

func (s *Service) Artist(ctx context.Context, id int64) (ArtistDetails, error) {
	artist, err := s.store.Artist(ctx, id)
	if err != nil {
		return ArtistDetails{}, err
	}
	albums, err := s.store.Albums(ctx, id)
	if err != nil {
		return ArtistDetails{}, err
	}
	return ArtistDetails{Artist: artist, Albums: albums}, nil
}

// Album returns the saved tracks, loading them from MusicBrainz on first open.
func (s *Service) Album(ctx context.Context, id int64) (AlbumDetails, error) {
	album, err := s.store.Album(ctx, id)
	if err != nil {
		return AlbumDetails{}, err
	}
	tracks, err := s.store.Tracks(ctx, id)
	if err != nil {
		return AlbumDetails{}, err
	}
	if len(tracks) == 0 {
		tracks, err = s.loadTracks(ctx, album)
		if err != nil {
			return AlbumDetails{}, err
		}
	}
	return AlbumDetails{Album: album, Tracks: tracks}, nil
}

func (s *Service) loadTracks(ctx context.Context, album Album) ([]Track, error) {
	releases, err := s.client.ReleaseGroupReleases(ctx, album.MusicBrainzID)
	if err != nil {
		return nil, err
	}
	if len(releases) == 0 {
		return nil, &Error{Code: "music_not_found", Message: "MusicBrainz has no releases for this album."}
	}
	chosen := releases[0]
	for _, release := range releases[1:] {
		if betterRelease(release, chosen) {
			chosen = release
		}
	}
	tracks, err := s.client.ReleaseTracks(ctx, chosen.ID)
	if err != nil {
		return nil, err
	}
	if err := s.store.SaveTracks(ctx, album.ID, tracks); err != nil {
		return nil, err
	}
	return tracks, nil
}

// betterRelease prefers an official release, then the earliest known date.
func betterRelease(candidate, current mbRelease) bool {
	candidateOfficial := strings.EqualFold(candidate.Status, "Official")
	currentOfficial := strings.EqualFold(current.Status, "Official")
	if candidateOfficial != currentOfficial {
		return candidateOfficial
	}
	if candidate.Date == "" {
		return false
	}
	if current.Date == "" {
		return true
	}
	return candidate.Date < current.Date
}
