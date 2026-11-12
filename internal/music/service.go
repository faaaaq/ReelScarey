package music

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"

	"github.com/faaaaq/ReelScarey/internal/securestore"
)

// Service combines the fixed MusicBrainz provider with the local catalog,
// scanning, reviewed catalog discovery, reviewed AcoustID/Chromaprint matching
// and reviewed file edits. Media writes happen only in ApplyEdits, which
// stages, verifies, publishes without overwrite and retains a backup before a
// tag rewrite.
type Service struct {
	client         *Client
	store          *Store
	mediaDir       string
	downloadsDir   string
	prober         AudioProber
	fingerprinter  Fingerprinter
	acoustid       *AcoustIDClient
	secrets        *securestore.Store
	brainz         *musicbrainzStore
	audio          AudioSource
	topBaseURL     string
	artworkDir     string
	artworkMu      sync.Mutex
	artworkLocks   map[string]*sync.Mutex
	scheduleRunner ScheduleRunner
	fileMu         sync.Mutex
	scheduleMu     sync.Mutex
}

func (s *Service) SetDownloadsDir(dir string) { s.downloadsDir = dir }

func (s *Service) SetArtworkDir(dir string) { s.artworkDir = dir }

func NewService(db *sql.DB, mediaDir string) *Service {
	brainz := &musicbrainzStore{db: db}
	client := New()
	client.SetCache(brainz)
	client.SetCooldownStore(brainz)
	return &Service{client: client, store: NewStore(db), mediaDir: mediaDir, prober: FFprobeTagger{}, fingerprinter: NewChromaprint(), acoustid: NewAcoustID(), brainz: brainz}
}

func NewServiceWithClient(db *sql.DB, client *Client, mediaDir string) *Service {
	brainz := &musicbrainzStore{db: db}
	if client != nil {
		client.SetCache(brainz)
		client.SetCooldownStore(brainz)
	}
	return &Service{client: client, store: NewStore(db), mediaDir: mediaDir, prober: FFprobeTagger{}, fingerprinter: NewChromaprint(), acoustid: NewAcoustID(), brainz: brainz}
}

func (s *Service) SetProber(prober AudioProber) {
	if prober != nil {
		s.prober = prober
	}
}

func (s *Service) SetFingerprinter(fingerprinter Fingerprinter) {
	if fingerprinter != nil {
		s.fingerprinter = fingerprinter
	}
}

// RecoverScans marks scans interrupted by a restart so they never appear to be
// running forever. A queued, matching, fingerprinting or organizing request is
// put back in the queue with an explicit message; partial matched rows are
// preserved and the durable worker resumes the work. Nothing is silently
// reported as completed.
func (s *Service) RecoverScans(ctx context.Context) error {
	_, err := s.store.db.ExecContext(ctx, `UPDATE music_scans SET state='failed', phase='failed', error='The scan was interrupted by a restart.', updated_at=unixepoch() WHERE state='scanning';
		UPDATE music_scans SET phase=CASE WHEN request='finalize' THEN 'organizing' WHEN request='match' THEN 'matching' ELSE phase END,
			progress_total=CASE WHEN request='finalize' THEN MAX((SELECT COUNT(*) FROM music_scan_files f WHERE f.scan_id=music_scans.id AND f.finalize_requested=1),matched_count,1) WHEN request='match' THEN MAX(unresolved_count,1) ELSE MAX(progress_total,1) END,
			progress_done=0,
			message='The workflow was interrupted by a restart and will resume from the saved state.',
			work_owner='', work_until=0, current_path='', updated_at=unixepoch() WHERE request<>'' AND state IN ('review','completed','failed');
		UPDATE music_scans SET work_owner='', work_until=0 WHERE request='';
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

func (s *Service) ListAllArtists(ctx context.Context, query string) (ArtistPage, error) {
	return s.store.ListAllArtists(ctx, query)
}

func (s *Service) ListAlbums(ctx context.Context, query string, page, limit int) (AlbumPage, error) {
	return s.store.ListAlbums(ctx, query, page, limit)
}

func (s *Service) ListTracks(ctx context.Context, query string, page, limit int) (ArtistTrackPage, error) {
	return s.store.ListTracks(ctx, query, page, limit)
}

func (s *Service) ListFiles(ctx context.Context, page int) (MusicFilePage, error) {
	return s.store.ListFiles(ctx, page)
}

// ArtistTracks returns tracks with registered files for one artist.
func (s *Service) ArtistTracks(ctx context.Context, artistID int64, page, limit int) (ArtistTrackPage, error) {
	return s.store.ArtistTracks(ctx, artistID, page, limit)
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
		if IsLocalMusicID(album.MusicBrainzID) {
			// A tag-derived album has no provider identity to load from.
			return AlbumDetails{Album: album, Tracks: []Track{}}, nil
		}
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
