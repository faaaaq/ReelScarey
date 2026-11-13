package music

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// AudioSource is the narrow downloader contract used by music acquisitions.
// It is implemented outside this package so tests can provide a fixture.
type AudioSource interface {
	SearchAudio(ctx context.Context, query string, limit int) ([]AudioCandidate, error)
	DownloadAudio(ctx context.Context, id, directory, format string) (AudioFile, error)
	InspectAudio(ctx context.Context, rawURL string) (AudioInspection, error)
}

type AudioInspection struct {
	Kind    string
	Title   string
	Channel string
	Entries []AudioEntry
}

type AudioEntry struct {
	ID              string
	Title           string
	Channel         string
	DurationSeconds int64
}

type AudioCandidate struct {
	ID              string
	Title           string
	Channel         string
	DurationSeconds int64
}

type AudioFile struct {
	Path   string
	Format string
}

type AcquisitionTrackInput struct {
	Title       string `json:"title"`
	Artist      string `json:"artist"`
	Album       string `json:"album"`
	AlbumArtist string `json:"album_artist"`
	Year        *int   `json:"year"`
	TrackNumber int    `json:"track_number"`
	DiscNumber  int    `json:"disc_number"`
	DurationMS  int    `json:"duration_ms"`
	CoverURL    string `json:"cover_url"`
	SourceURL   string `json:"source_url"`
	SearchQuery string `json:"search_query"`
}

type AcquisitionTrack struct {
	ID          int64  `json:"id"`
	JobID       int64  `json:"job_id"`
	Position    int    `json:"position"`
	Title       string `json:"title"`
	Artist      string `json:"artist"`
	Album       string `json:"album"`
	AlbumArtist string `json:"album_artist"`
	Year        *int   `json:"year"`
	TrackNumber int    `json:"track_number"`
	DiscNumber  int    `json:"disc_number"`
	DurationMS  int    `json:"duration_ms"`
	CoverURL    string `json:"cover_url"`
	SourceURL   string `json:"source_url"`
	SearchQuery string `json:"search_query"`
	State       string `json:"state"`
	Error       string `json:"error"`
	MusicFileID *int64 `json:"music_file_id"`
	ImportJobID *int64 `json:"import_job_id"`
}

type AcquisitionJob struct {
	ID             int64              `json:"id"`
	SourceURL      string             `json:"source_url"`
	SourceKind     string             `json:"source_kind"`
	Title          string             `json:"title"`
	Root           string             `json:"root"`
	Format         string             `json:"format"`
	State          string             `json:"state"`
	TrackCount     int                `json:"track_count"`
	CompletedCount int                `json:"completed_count"`
	Error          string             `json:"error"`
	CreatedAt      int64              `json:"created_at"`
	UpdatedAt      int64              `json:"updated_at"`
	Tracks         []AcquisitionTrack `json:"tracks,omitempty"`
}

type AcquisitionJobList struct {
	Items []AcquisitionJob `json:"items"`
	Page  int              `json:"page"`
	Total int              `json:"total"`
	Limit int              `json:"limit"`
}

func (s *Service) SetAudioSource(source AudioSource) { s.audio = source }

// StartAlbumAcquisition queues every catalog track in one album that has no
// registered file. It is used by the unified library and the album page.
func (s *Service) StartAlbumAcquisition(ctx context.Context, albumID int64, root, format string) (AcquisitionJob, error) {
	if albumID < 1 {
		return AcquisitionJob{}, ErrInvalidInput
	}
	album, err := s.store.Album(ctx, albumID)
	if err != nil {
		return AcquisitionJob{}, err
	}
	tracks, err := s.store.Tracks(ctx, albumID)
	if err != nil {
		return AcquisitionJob{}, err
	}
	inputs := []AcquisitionTrackInput{}
	for _, track := range tracks {
		if track.FileID != nil || len(inputs) >= 100 {
			continue
		}
		inputs = append(inputs, AcquisitionTrackInput{
			Title: track.Title, Artist: album.ArtistName, Album: album.Title, AlbumArtist: album.ArtistName,
			Year: album.Year, TrackNumber: track.Number, DiscNumber: track.Disc,
			DurationMS: track.DurationMS, CoverURL: album.CoverURL,
			SearchQuery: strings.TrimSpace(album.ArtistName + " " + track.Title),
		})
	}
	if len(inputs) == 0 {
		return AcquisitionJob{}, &Error{Code: "music_nothing_to_do", Message: "Every catalog track in this album already has a registered file."}
	}
	// The album identity makes a double-clicked or retried submit return the
	// existing queued/running job instead of re-downloading every track.
	return s.startAcquisition(ctx, "album:"+strconv.FormatInt(albumID, 10), "", "album", album.Title, root, format, inputs)
}

// StartProviderAlbumAcquisition resolves a public Deezer album link and queues
// all of its tracks. It is used for albums shown in the full discography that
// are not yet in the local catalog.
func (s *Service) StartProviderAlbumAcquisition(ctx context.Context, providerID, root, format string) (AcquisitionJob, error) {
	providerID = strings.TrimSpace(providerID)
	if providerID == "" || len(providerID) > 32 {
		return AcquisitionJob{}, ErrInvalidInput
	}
	for _, value := range providerID {
		if value < '0' || value > '9' {
			return AcquisitionJob{}, ErrInvalidInput
		}
	}
	preview, err := s.ResolveLink(ctx, "https://www.deezer.com/album/"+providerID)
	if err != nil {
		return AcquisitionJob{}, err
	}
	inputs := make([]AcquisitionTrackInput, 0, len(preview.Tracks))
	for _, track := range preview.Tracks {
		if len(inputs) >= 100 {
			break
		}
		inputs = append(inputs, AcquisitionTrackInput{
			Title: track.Title, Artist: track.Artist, Album: track.Album, AlbumArtist: track.AlbumArtist,
			Year: track.Year, TrackNumber: track.TrackNumber, DiscNumber: track.DiscNumber,
			DurationMS: track.DurationMS, CoverURL: track.CoverURL, SourceURL: track.SourceURL, SearchQuery: track.SearchQuery,
		})
	}
	if len(inputs) == 0 {
		return AcquisitionJob{}, &Error{Code: "music_link_empty", Message: "Deezer returned no tracks for this album."}
	}
	return s.StartAcquisition(ctx, "https://www.deezer.com/album/"+providerID, "deezer_album", preview.Title, root, format, inputs)
}

func validAcquisitionFormat(format string) bool {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "source", "mp3", "flac", "m4a", "opus":
		return true
	default:
		return false
	}
}

func (s *Service) StartAcquisition(ctx context.Context, sourceURL, sourceKind, title, root, format string, tracks []AcquisitionTrackInput) (AcquisitionJob, error) {
	return s.startAcquisition(ctx, "", sourceURL, sourceKind, title, root, format, tracks)
}

func (s *Service) startAcquisition(ctx context.Context, dedupKey, sourceURL, sourceKind, title, root, format string, tracks []AcquisitionTrackInput) (AcquisitionJob, error) {
	if s.audio == nil {
		return AcquisitionJob{}, &Error{Code: "music_acquisition_unavailable", Message: "Audio downloading is unavailable because yt-dlp is not configured."}
	}
	if len(tracks) == 0 || len(tracks) > 100 || !validAcquisitionFormat(format) {
		return AcquisitionJob{}, ErrInvalidInput
	}
	root, err := s.cleanImportRoot(root)
	if err != nil {
		return AcquisitionJob{}, err
	}
	format = strings.ToLower(strings.TrimSpace(format))
	if format == "" {
		format = "source"
	}
	// Provider links deduplicate on their URL; album jobs on the album id
	// (CS-7). The write transaction is BEGIN IMMEDIATE, so a concurrent
	// duplicate submit serializes behind this check and the partial unique
	// index backs the check up even across processes.
	dedupKey = strings.TrimSpace(dedupKey)
	if dedupKey == "" {
		if trimmed := strings.TrimSpace(sourceURL); trimmed != "" {
			dedupKey = "url:" + trimmed
		}
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return AcquisitionJob{}, err
	}
	defer tx.Rollback()
	if dedupKey != "" {
		var existing int64
		err := tx.QueryRowContext(ctx, `SELECT id FROM music_acquisition_jobs WHERE dedup_key=? AND state IN ('queued','running') LIMIT 1`, dedupKey).Scan(&existing)
		if err == nil {
			tx.Rollback()
			return s.Acquisition(ctx, existing)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return AcquisitionJob{}, err
		}
	}
	now := time.Now().Unix()
	result, err := tx.ExecContext(ctx, `INSERT INTO music_acquisition_jobs(dedup_key,source_url,source_kind,title,root,format,state,track_count,created_at,updated_at) VALUES(?,?,?,?,?,?,'queued',?,?,?)`,
		nullableText(dedupKey), strings.TrimSpace(sourceURL), strings.TrimSpace(sourceKind), strings.TrimSpace(title), root, format, len(tracks), now, now)
	if err != nil {
		// The partial unique index is the cross-process backstop for the
		// in-transaction check above (CS-7).
		if dedupKey != "" && strings.Contains(err.Error(), "UNIQUE constraint failed") {
			var existing int64
			if e := tx.QueryRowContext(ctx, `SELECT id FROM music_acquisition_jobs WHERE dedup_key=? AND state IN ('queued','running') LIMIT 1`, dedupKey).Scan(&existing); e == nil {
				tx.Rollback()
				return s.Acquisition(ctx, existing)
			}
		}
		return AcquisitionJob{}, err
	}
	if err != nil {
		return AcquisitionJob{}, err
	}
	jobID, err := result.LastInsertId()
	if err != nil {
		return AcquisitionJob{}, err
	}
	for i, track := range tracks {
		track.Title = strings.TrimSpace(track.Title)
		track.Artist = strings.TrimSpace(track.Artist)
		track.Album = strings.TrimSpace(track.Album)
		track.AlbumArtist = strings.TrimSpace(track.AlbumArtist)
		track.SearchQuery = strings.TrimSpace(track.SearchQuery)
		track.SourceURL = strings.TrimSpace(track.SourceURL)
		if track.Title == "" || track.Artist == "" || track.Album == "" || track.SearchQuery == "" && track.SourceURL == "" {
			return AcquisitionJob{}, ErrInvalidInput
		}
		if track.AlbumArtist == "" {
			track.AlbumArtist = track.Artist
		}
		if track.TrackNumber < 1 {
			track.TrackNumber = 1
		}
		if track.DiscNumber < 1 {
			track.DiscNumber = 1
		}
		if track.DurationMS < 0 {
			track.DurationMS = 0
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO music_acquisition_tracks(job_id,position,title,artist,album,album_artist,year,track_number,disc_number,duration_ms,cover_url,source_url,search_query,state,created_at,updated_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,'queued',?,?)`,
			jobID, i+1, track.Title, track.Artist, track.Album, track.AlbumArtist, track.Year, track.TrackNumber, track.DiscNumber, track.DurationMS, strings.TrimSpace(track.CoverURL), track.SourceURL, track.SearchQuery, now, now); err != nil {
			return AcquisitionJob{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return AcquisitionJob{}, err
	}
	return s.Acquisition(ctx, jobID)
}

// nullableText stores an optional dedup identity as NULL so pre-migration
// rows and identity-less jobs never collide in the partial unique index.
func nullableText(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func (s *Service) Acquisition(ctx context.Context, id int64) (AcquisitionJob, error) {
	var job AcquisitionJob
	err := s.store.db.QueryRowContext(ctx, `SELECT id,source_url,source_kind,title,root,format,state,track_count,completed_count,error,created_at,updated_at FROM music_acquisition_jobs WHERE id=?`, id).
		Scan(&job.ID, &job.SourceURL, &job.SourceKind, &job.Title, &job.Root, &job.Format, &job.State, &job.TrackCount, &job.CompletedCount, &job.Error, &job.CreatedAt, &job.UpdatedAt)
	if err != nil {
		return AcquisitionJob{}, err
	}
	rows, err := s.store.db.QueryContext(ctx, `SELECT id,job_id,position,title,artist,album,album_artist,year,track_number,disc_number,duration_ms,cover_url,source_url,search_query,state,error,music_file_id,import_job_id FROM music_acquisition_tracks WHERE job_id=? ORDER BY position,id`, id)
	if err != nil {
		return AcquisitionJob{}, err
	}
	defer rows.Close()
	job.Tracks = []AcquisitionTrack{}
	for rows.Next() {
		var track AcquisitionTrack
		var year sql.NullInt64
		var musicFileID, importJobID sql.NullInt64
		if err := rows.Scan(&track.ID, &track.JobID, &track.Position, &track.Title, &track.Artist, &track.Album, &track.AlbumArtist, &year, &track.TrackNumber, &track.DiscNumber, &track.DurationMS, &track.CoverURL, &track.SourceURL, &track.SearchQuery, &track.State, &track.Error, &musicFileID, &importJobID); err != nil {
			return AcquisitionJob{}, err
		}
		if year.Valid {
			value := int(year.Int64)
			track.Year = &value
		}
		if musicFileID.Valid {
			value := musicFileID.Int64
			track.MusicFileID = &value
		}
		if importJobID.Valid {
			value := importJobID.Int64
			track.ImportJobID = &value
		}
		job.Tracks = append(job.Tracks, track)
	}
	return job, rows.Err()
}

func (s *Service) ListAcquisitions(ctx context.Context, page int) (AcquisitionJobList, error) {
	if page < 1 || page > 100000 {
		return AcquisitionJobList{}, ErrInvalidInput
	}
	const limit = 20
	out := AcquisitionJobList{Items: []AcquisitionJob{}, Page: page, Limit: limit}
	if err := s.store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM music_acquisition_jobs`).Scan(&out.Total); err != nil {
		return AcquisitionJobList{}, err
	}
	rows, err := s.store.db.QueryContext(ctx, `SELECT id FROM music_acquisition_jobs ORDER BY id DESC LIMIT ? OFFSET ?`, limit, (page-1)*limit)
	if err != nil {
		return AcquisitionJobList{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return AcquisitionJobList{}, err
		}
		job, err := s.Acquisition(ctx, id)
		if err != nil {
			return AcquisitionJobList{}, err
		}
		out.Items = append(out.Items, job)
	}
	return out, rows.Err()
}

// RunAcquisitions processes durable acquisition work. Downloads are never
// started from an HTTP request and never depend on the request that queued
// them remaining open.
func (s *Service) RunAcquisitions(ctx context.Context, logger *slog.Logger) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for ctx.Err() == nil {
		worked, err := s.ProcessAcquisition(ctx)
		if err != nil && ctx.Err() == nil {
			logger.Error("music acquisition worker failed", "error", err)
		}
		if worked && err == nil {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) ProcessAcquisition(ctx context.Context) (bool, error) {
	s.reconcileAcquisitions(ctx)
	var trackID int64
	err := s.store.db.QueryRowContext(ctx, `SELECT id FROM music_acquisition_tracks WHERE state IN ('queued','downloading') ORDER BY id LIMIT 1`).Scan(&trackID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if _, err := s.store.db.ExecContext(ctx, `UPDATE music_acquisition_tracks SET state='downloading',error='',updated_at=unixepoch() WHERE id=? AND state IN ('queued','downloading')`, trackID); err != nil {
		return false, err
	}
	track, job, err := s.acquisitionTrack(ctx, trackID)
	if err != nil {
		return false, err
	}
	if err := s.acquireTrack(ctx, track, job); err != nil {
		if stateErr := s.setAcquisitionTrackState(ctx, trackID, "failed", err.Error()); stateErr != nil {
			slog.Default().Error("music acquisition track state write failed", "track_id", trackID, "error", stateErr)
		}
		if refreshErr := s.refreshAcquisitionJob(ctx, job.ID); refreshErr != nil {
			slog.Default().Error("music acquisition job refresh failed", "job_id", job.ID, "error", refreshErr)
		}
		return true, nil
	}
	if err := s.refreshAcquisitionJob(ctx, job.ID); err != nil {
		slog.Default().Error("music acquisition job refresh failed", "job_id", job.ID, "error", err)
	}
	return true, nil
}
func (s *Service) acquisitionTrack(ctx context.Context, trackID int64) (AcquisitionTrack, AcquisitionJob, error) {
	job, err := s.acquisitionJobForTrack(ctx, trackID)
	if err != nil {
		return AcquisitionTrack{}, AcquisitionJob{}, err
	}
	acq, err := s.Acquisition(ctx, job.ID)
	if err != nil {
		return AcquisitionTrack{}, AcquisitionJob{}, err
	}
	for _, track := range acq.Tracks {
		if track.ID == trackID {
			return track, job, nil
		}
	}
	return AcquisitionTrack{}, AcquisitionJob{}, sql.ErrNoRows
}

func (s *Service) acquisitionJobForTrack(ctx context.Context, trackID int64) (AcquisitionJob, error) {
	var job AcquisitionJob
	err := s.store.db.QueryRowContext(ctx, `SELECT j.id,j.source_url,j.source_kind,j.title,j.root,j.format,j.state,j.track_count,j.completed_count,j.error,j.created_at,j.updated_at
		FROM music_acquisition_jobs j JOIN music_acquisition_tracks t ON t.job_id=j.id WHERE t.id=?`, trackID).
		Scan(&job.ID, &job.SourceURL, &job.SourceKind, &job.Title, &job.Root, &job.Format, &job.State, &job.TrackCount, &job.CompletedCount, &job.Error, &job.CreatedAt, &job.UpdatedAt)
	return job, err
}

func (s *Service) acquireTrack(ctx context.Context, track AcquisitionTrack, job AcquisitionJob) error {
	if s.audio == nil {
		return &Error{Code: "music_acquisition_unavailable", Message: "Audio downloading is unavailable because yt-dlp is not configured."}
	}
	stagingRoot := filepath.Join(s.downloadsDir, "music-acquisitions", fmt.Sprintf("%d", job.ID), fmt.Sprintf("%d", track.ID))
	if !contained(s.downloadsDir, stagingRoot) {
		return ErrInvalidInput
	}
	if err := os.RemoveAll(stagingRoot); err != nil {
		return err
	}
	if err := os.MkdirAll(stagingRoot, 0o700); err != nil {
		return err
	}
	videoID := strings.TrimSpace(track.SourceURL)
	if videoID == "" {
		query := strings.TrimSpace(track.SearchQuery)
		if query == "" {
			query = strings.TrimSpace(track.Artist + " " + track.Title)
		}
		// The query may carry album context, but the track title is always the
		// authoritative name to match: audio belonging to a different track or
		// album is never accepted on duration or album-name evidence alone.
		candidates, err := s.audio.SearchAudio(ctx, query, 10)
		if err != nil {
			return &Error{Code: "music_acquisition_search", Message: "YouTube search failed for this track."}
		}
		chosen := chooseAudioCandidate(candidates, track.DurationMS, track.Title)
		if chosen == nil {
			return &Error{Code: "music_acquisition_match", Message: "No close enough YouTube audio match was found for this track."}
		}
		videoID = chosen.ID
	}
	audio, err := s.audio.DownloadAudio(ctx, videoID, stagingRoot, job.Format)
	if err != nil {
		return &Error{Code: "music_acquisition_download", Message: "yt-dlp could not download or convert this track."}
	}
	if !contained(stagingRoot, audio.Path) {
		return ErrInvalidInput
	}
	relative, err := filepath.Rel(s.downloadsDir, audio.Path)
	if err != nil || !contained(s.downloadsDir, audio.Path) {
		return ErrInvalidInput
	}
	relative = filepath.ToSlash(relative)
	albumID, acquiredTrackID, err := s.ensureAcquiredTrack(ctx, track)
	if err != nil {
		return err
	}
	importID, err := s.startAcquiredImport(ctx, albumID, acquiredTrackID, relative, job.Root)
	if err != nil {
		return err
	}
	if _, err := s.store.db.ExecContext(ctx, `UPDATE music_acquisition_tracks SET state='importing',import_job_id=?,error='',updated_at=unixepoch() WHERE id=?`, importID, track.ID); err != nil {
		return err
	}
	return nil
}

// audioCandidateMatchesTitle reports whether a YouTube result title belongs to
// the requested track. Parenthetical and bracketed decorations such as
// "(Official Audio)" or "(from DEPENDS)" are stripped from both titles first,
// so the requested track must be named by the candidate's main title. The
// comparison is punctuation-insensitive ("S.A.D." and "SAD" agree) but aligned
// on token boundaries, so a substring such as "sad" inside "SADDEST" or the
// unrelated song "DEPENDS" is rejected even when the duration matches. A blank
// requested title cannot be checked and is accepted so callers without a title
// keep the previous duration-only behavior.
func audioCandidateMatchesTitle(candidateTitle, title string) bool {
	want := compactMatchName(stripTitleDecorations(title))
	if want == "" {
		want = compactMatchName(title)
	}
	if want == "" {
		return true
	}
	tokens := strings.Fields(normalizeMatchName(stripTitleDecorations(candidateTitle)))
	if len(tokens) == 0 {
		tokens = strings.Fields(normalizeMatchName(candidateTitle))
	}
	if len(tokens) == 0 {
		return false
	}
	// The requested title may be embedded in a longer YouTube title; accept it
	// only when its normalized letters line up with complete candidate tokens
	// ("XG - S.A.D. (Official Audio)"), never inside a larger word ("SADDEST",
	// "DEPENDS").
	for i := range tokens {
		var joined strings.Builder
		for j := i; j < len(tokens); j++ {
			joined.WriteString(tokens[j])
			if joined.Len() > len(want) {
				break
			}
			if joined.String() == want {
				return true
			}
		}
	}
	return false
}

// stripTitleDecorations removes parenthetical and bracketed suffixes that
// video titles use for video/audio/album context. The requested track must be
// named by the remaining main title, so an album name inside a decoration
// ("S.A.D. (from DEPENDS)") can never satisfy a different track request.
func stripTitleDecorations(title string) string {
	var out strings.Builder
	out.Grow(len(title))
	round, square := 0, 0
	for _, r := range title {
		switch r {
		case '(':
			round++
		case ')':
			if round > 0 {
				round--
			}
		case '[':
			square++
		case ']':
			if square > 0 {
				square--
			}
		default:
			if round == 0 && square == 0 {
				out.WriteRune(r)
			}
		}
	}
	return out.String()
}

// chooseAudioCandidate picks the closest-duration YouTube result that also
// names the requested track. Timing alone is not evidence of identity: without
// a title match the caller gets no candidate and reports an honest "no match"
// instead of importing a different song.
func chooseAudioCandidate(candidates []AudioCandidate, durationMS int, title string) *AudioCandidate {
	if len(candidates) == 0 {
		return nil
	}
	expected := int64(durationMS / 1000)
	var best *AudioCandidate
	bestDiff := int64(11)
	for i := range candidates {
		candidate := &candidates[i]
		if !audioCandidateMatchesTitle(candidate.Title, title) {
			continue
		}
		if expected <= 0 {
			return candidate
		}
		if candidate.DurationSeconds <= 0 {
			continue
		}
		diff := candidate.DurationSeconds - expected
		if diff < 0 {
			diff = -diff
		}
		if diff < bestDiff {
			best, bestDiff = candidate, diff
		}
	}
	return best
}

func (s *Service) ensureAcquiredTrack(ctx context.Context, track AcquisitionTrack) (int64, int64, error) {
	catalog := newLocalCatalog(s)
	number := track.TrackNumber
	if number < 1 {
		number = 1
	}
	disc := track.DiscNumber
	if disc < 1 {
		disc = 1
	}
	year := track.Year
	var lastErr error
	for attempt := 0; attempt < 50; attempt++ {
		n := number + attempt
		file := ScanFile{
			Artist: track.Artist, AlbumArtist: track.AlbumArtist, Album: track.Album, Title: track.Title,
			Track: &n, Disc: &disc, Year: year, DurationMS: track.DurationMS,
		}
		_, albumID, trackID, err := catalog.matchTagCreate(ctx, file)
		if err == nil {
			if cover := strings.TrimSpace(track.CoverURL); cover != "" {
				_, _ = s.store.db.ExecContext(ctx, `UPDATE music_albums SET cover_url=?,updated_at=unixepoch() WHERE id=? AND cover_url=''`, cover, albumID)
			}
			return albumID, trackID, nil
		}
		lastErr = err
		var provider *Error
		if !errors.As(err, &provider) || provider.Code != "music_local_track" {
			return 0, 0, err
		}
	}
	if lastErr == nil {
		lastErr = ErrInvalidInput
	}
	return 0, 0, lastErr
}

func (s *Service) startAcquiredImport(ctx context.Context, albumID, trackID int64, source, root string) (int64, error) {
	root, err := s.cleanImportRoot(root)
	if err != nil {
		return 0, err
	}
	source = path.Clean(strings.ReplaceAll(strings.TrimSpace(source), "\\", "/"))
	if source == "." || strings.HasPrefix(source, "../") || strings.HasPrefix(source, "/") {
		return 0, ErrInvalidInput
	}
	abs := filepath.Join(s.downloadsDir, filepath.FromSlash(source))
	if !contained(s.downloadsDir, abs) {
		return 0, ErrInvalidInput
	}
	info, err := os.Stat(abs)
	if err != nil || !info.Mode().IsRegular() {
		return 0, &Error{Code: "music_acquisition_source", Message: "The downloaded audio file is missing."}
	}
	album, err := s.loadImportAlbum(ctx, albumID)
	if err != nil {
		return 0, err
	}
	if trackByID(album.Tracks, trackID) == nil {
		return 0, ErrInvalidInput
	}
	now := time.Now().Unix()
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `INSERT INTO music_import_jobs(album_id,root,source_path,state,file_count,created_at,updated_at) VALUES(?,?,?,'running',1,?,?)`, albumID, root, source, now, now)
	if err != nil {
		return 0, err
	}
	jobID, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO music_import_files(job_id,source_path,track_id,state,created_at,updated_at) VALUES(?,?,?,'pending',?,?)`, jobID, source, trackID, now, now); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return jobID, nil
}

func (s *Service) setAcquisitionTrackState(ctx context.Context, id int64, state, message string) error {
	_, err := s.store.db.ExecContext(ctx, `UPDATE music_acquisition_tracks SET state=?,error=?,updated_at=unixepoch() WHERE id=?`, state, message, id)
	return err
}

func (s *Service) reconcileAcquisitions(ctx context.Context) {
	// Rescue tracks whose download vanished: a 'downloading' row untouched for
	// two hours is never going to finish, so it fails honestly instead of
	// blocking the worker queue forever.
	stale, err := s.store.db.QueryContext(ctx, `SELECT id,job_id FROM music_acquisition_tracks WHERE state='downloading' AND updated_at < unixepoch()-7200`)
	if err == nil {
		type stuckTrack struct {
			id, jobID int64
		}
		stuck := []stuckTrack{}
		for stale.Next() {
			var item stuckTrack
			if err := stale.Scan(&item.id, &item.jobID); err != nil {
				break
			}
			stuck = append(stuck, item)
		}
		staleErr := stale.Err()
		stale.Close()
		if staleErr != nil {
			return
		}
		for _, item := range stuck {
			if err := s.setAcquisitionTrackState(ctx, item.id, "failed", "The download did not finish. Try this track again."); err != nil {
				s.journalAcquisitionWriteFailure(ctx, item.jobID, "a stuck download could not be marked failed", err)
				continue
			}
			if err := s.refreshAcquisitionJob(ctx, item.jobID); err != nil {
				s.journalAcquisitionWriteFailure(ctx, item.jobID, "the acquisition job could not be refreshed", err)
			}
		}
	}
	rows, err := s.store.db.QueryContext(ctx, `SELECT t.id,t.job_id,t.import_job_id,j.state,j.error FROM music_acquisition_tracks t JOIN music_import_jobs j ON j.id=t.import_job_id WHERE t.state='importing'`)
	if err != nil {
		return
	}
	type pending struct {
		id, jobID, importID int64
		state, message      string
	}
	items := []pending{}
	for rows.Next() {
		var item pending
		if err := rows.Scan(&item.id, &item.jobID, &item.importID, &item.state, &item.message); err != nil {
			continue
		}
		items = append(items, item)
	}
	_ = rows.Close()
	for _, item := range items {
		switch item.state {
		case "completed":
			var fileID int64
			err := s.store.db.QueryRowContext(ctx, `SELECT f.id FROM music_files f JOIN music_import_files i ON i.destination_path=f.relative_path WHERE i.job_id=? AND i.track_id=(SELECT track_id FROM music_import_files WHERE job_id=? LIMIT 1) LIMIT 1`, item.importID, item.importID).Scan(&fileID)
			if err != nil {
				if err := s.setAcquisitionTrackState(ctx, item.id, "failed", "The imported file could not be attached to the acquisition."); err != nil {
					s.journalAcquisitionWriteFailure(ctx, item.jobID, "an imported track could not be marked failed", err)
				}
				continue
			}
			if _, err := s.store.db.ExecContext(ctx, `UPDATE music_acquisition_tracks SET state='completed',music_file_id=?,error='',updated_at=unixepoch() WHERE id=?`, fileID, item.id); err != nil {
				s.journalAcquisitionWriteFailure(ctx, item.jobID, "an imported track could not be marked completed", err)
			}
		case "failed":
			if err := s.setAcquisitionTrackState(ctx, item.id, "failed", item.message); err != nil {
				s.journalAcquisitionWriteFailure(ctx, item.jobID, "a failed import could not be recorded", err)
			}
		}
		if err := s.refreshAcquisitionJob(ctx, item.jobID); err != nil {
			s.journalAcquisitionWriteFailure(ctx, item.jobID, "the acquisition job could not be refreshed", err)
		}
	}
}

// journalAcquisitionWriteFailure records a failed acquisition state write as
// an activity event so a job cannot sit in a running state with no record of
// the failure (RS-0014). The journal write itself is best effort: when the
// database is failing there is nowhere durable left to record anything.
func (s *Service) journalAcquisitionWriteFailure(ctx context.Context, jobID int64, detail string, writeErr error) {
	_, _ = s.store.db.ExecContext(context.WithoutCancel(ctx), `INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'music',?,?,'acquisition state update failed')`,
		strconv.FormatInt(jobID, 10), fmt.Sprintf("Music acquisition #%d: %s (%v)", jobID, detail, writeErr))
}

func (s *Service) refreshAcquisitionJob(ctx context.Context, jobID int64) error {
	var total, completed, active int
	if err := s.store.db.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(CASE WHEN state='completed' THEN 1 ELSE 0 END),0),COALESCE(SUM(CASE WHEN state IN ('queued','downloading','importing') THEN 1 ELSE 0 END),0) FROM music_acquisition_tracks WHERE job_id=?`, jobID).Scan(&total, &completed, &active); err != nil {
		return err
	}
	state := "queued"
	message := ""
	if active > 0 {
		state = "running"
	} else if completed == total {
		state = "completed"
	} else if completed > 0 {
		state = "completed"
		message = fmt.Sprintf("%d of %d tracks completed. Review failed tracks and retry if needed.", completed, total)
	} else {
		state = "failed"
		message = "No tracks completed."
	}
	_, err := s.store.db.ExecContext(ctx, `UPDATE music_acquisition_jobs SET state=?,completed_count=?,error=?,updated_at=unixepoch() WHERE id=?`, state, completed, message, jobID)
	return err
}
