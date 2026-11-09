package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/faaaaq/ReelScarey/internal/music"
	"github.com/faaaaq/ReelScarey/internal/organization"
)

func (s *Server) musicError(w http.ResponseWriter, err error) {
	var provider *music.Error
	if errors.As(err, &provider) {
		status := http.StatusBadGateway
		switch provider.Code {
		case "music_invalid_request":
			status = http.StatusBadRequest
		case "music_not_found":
			status = http.StatusNotFound
		case "music_rate_limited", "music_acoustid_rate_limited":
			status = http.StatusTooManyRequests
		case "music_acoustid_not_configured", "music_fingerprint_unavailable":
			status = http.StatusConflict
		case "music_acoustid_invalid_key", "music_acoustid_invalid_request", "music_fingerprint_input", "music_fingerprint_failed":
			status = http.StatusBadRequest
		case "music_nothing_to_do", "music_destination_exists", "music_backup_exists", "music_rollback_changed", "music_rollback_unverified",
			"music_replace_in_progress", "music_rollback_state", "music_source_missing", "music_scan_state":
			status = http.StatusConflict
		case "music_no_track", "music_no_tracks", "music_import_source", "music_import_empty",
			"music_import_unmatched", "music_import_duplicate", "music_import_missing", "music_scan_root":
			status = http.StatusBadRequest
		case "music_import_missing_source":
			status = http.StatusConflict
		}
		if provider.RetryAfter > 0 {
			w.Header().Set("Retry-After", strconv.Itoa(provider.RetryAfter))
		}
		fail(w, status, provider.Code, provider.Message)
		return
	}
	if errors.Is(err, music.ErrPlanChanged) {
		fail(w, 409, "plan_changed", err.Error())
		return
	}
	if errors.Is(err, music.ErrInvalidInput) {
		fail(w, 400, "invalid_music", err.Error())
		return
	}
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, 404, "not_found", "This music item is not in your catalog.")
		return
	}
	s.internal(w, "music", err)
}

func (s *Server) searchMusicArtists(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, 503, "music_unavailable", "The music catalog is unavailable.")
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("query"))
	page, valid := pageNumber(r, 500)
	if !valid || !music.ValidQuery(query, page) {
		fail(w, 400, "invalid_search", "Enter 1–200 characters and a page between 1 and 500.")
		return
	}
	results, err := s.music.SearchArtists(r.Context(), query, page)
	if err != nil {
		s.musicError(w, err)
		return
	}
	send(w, 200, results)
}

func (s *Server) musicArtists(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, 503, "music_unavailable", "The music catalog is unavailable.")
		return
	}
	switch r.Method {
	case http.MethodGet:
		query := strings.TrimSpace(r.URL.Query().Get("query"))
		page, valid := pageNumber(r, 100000)
		if !valid || !music.ValidListQuery(query, page) {
			fail(w, 400, "invalid_music", "Enter at most 200 characters and a valid page.")
			return
		}
		items, err := s.music.ListArtists(r.Context(), query, page)
		if err != nil {
			s.musicError(w, err)
			return
		}
		send(w, 200, items)
	case http.MethodPost:
		var body struct {
			MusicBrainzID string `json:"musicbrainz_id"`
			Monitored     *bool  `json:"monitored"`
		}
		if !decodeJSON(w, r, &body) {
			return
		}
		mbid := strings.TrimSpace(body.MusicBrainzID)
		if !music.ValidMBID(mbid) {
			fail(w, 400, "invalid_music", "Choose a valid MusicBrainz artist ID.")
			return
		}
		monitored := true
		if body.Monitored != nil {
			monitored = *body.Monitored
		}
		artist, created, err := s.music.AddArtist(r.Context(), mbid, monitored)
		if err != nil {
			s.musicError(w, err)
			return
		}
		status := http.StatusOK
		if created {
			status = http.StatusCreated
		}
		send(w, status, map[string]any{"item": artist, "created": created})
	default:
		w.Header().Set("Allow", "GET, POST")
		fail(w, 405, "method_not_allowed", "Use GET or POST.")
	}
}

func (s *Server) musicArtist(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, 503, "music_unavailable", "The music catalog is unavailable.")
		return
	}
	id, ok := musicID(w, r)
	if !ok {
		return
	}
	details, err := s.music.Artist(r.Context(), id)
	if err != nil {
		s.musicError(w, err)
		return
	}
	send(w, 200, details)
}

func (s *Server) musicAlbum(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, 503, "music_unavailable", "The music catalog is unavailable.")
		return
	}
	id, ok := musicID(w, r)
	if !ok {
		return
	}
	details, err := s.music.Album(r.Context(), id)
	if err != nil {
		s.musicError(w, err)
		return
	}
	send(w, 200, details)
}

func (s *Server) musicScans(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, 503, "music_unavailable", "The music catalog is unavailable.")
		return
	}
	switch r.Method {
	case http.MethodGet:
		page, valid := pageNumber(r, 100000)
		if !valid {
			fail(w, 400, "invalid_music", "Choose a valid page.")
			return
		}
		items, err := s.music.ListScans(r.Context(), page)
		if err != nil {
			s.musicError(w, err)
			return
		}
		send(w, 200, items)
	case http.MethodPost:
		var body struct {
			Root  string   `json:"root"`
			Roots []string `json:"roots"`
			Full  bool     `json:"full"`
		}
		if !decodeJSON(w, r, &body) {
			return
		}
		root := strings.TrimSpace(body.Root)
		selected := body.Roots
		if len(selected) > 0 {
			configured, err := organization.New(s.db, s.config).EffectiveRoots(r.Context(), "music", "music")
			if err != nil {
				s.internal(w, "music_scan_roots", err)
				return
			}
			allowed := map[string]bool{}
			for _, item := range configured {
				allowed[item] = true
			}
			clean := make([]string, 0, len(selected))
			for _, item := range selected {
				item = strings.TrimSpace(strings.ReplaceAll(item, "\\", "/"))
				for len(item) > 1 && strings.HasSuffix(item, "/") {
					item = strings.TrimSuffix(item, "/")
				}
				if !allowed[item] {
					fail(w, 400, "invalid_music_scan", "Choose configured music roots from library roots.")
					return
				}
				clean = append(clean, item)
			}
			selected = clean
		}
		if len(selected) == 0 && root != "" {
			selected = []string{root}
		}
		if len(selected) == 0 {
			fail(w, 400, "invalid_music_scan", "Choose at least one configured music root below the media root.")
			return
		}
		scan, err := s.music.StartScanRootsFull(r.Context(), selected, body.Full)
		if err != nil {
			s.musicError(w, err)
			return
		}
		send(w, 202, scan)
	default:
		w.Header().Set("Allow", "GET, POST")
		fail(w, 405, "method_not_allowed", "Use GET or POST.")
	}
}

func (s *Server) musicScan(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, 503, "music_unavailable", "The music catalog is unavailable.")
		return
	}
	id, ok := musicID(w, r)
	if !ok {
		return
	}
	page, valid := pageNumber(r, 100000)
	if !valid {
		fail(w, 400, "invalid_music", "Choose a valid page.")
		return
	}
	out, err := s.music.ScanPage(r.Context(), id, page)
	if err != nil {
		s.musicError(w, err)
		return
	}
	send(w, 200, out)
}

func (s *Server) musicScanRegister(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, 503, "music_unavailable", "The music catalog is unavailable.")
		return
	}
	id, ok := musicID(w, r)
	if !ok {
		return
	}
	var body struct {
		FileIDs  []int64 `json:"file_ids"`
		Reviewed bool    `json:"reviewed"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if !body.Reviewed {
		fail(w, 400, "review_required", "Review the matched files before registering them.")
		return
	}
	registered, err := s.music.RegisterScan(r.Context(), id, body.FileIDs)
	if err != nil {
		s.musicError(w, err)
		return
	}
	send(w, 200, map[string]any{"registered": registered})
}

func (s *Server) musicScanDiscover(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, 503, "music_unavailable", "The music catalog is unavailable.")
		return
	}
	id, ok := musicID(w, r)
	if !ok {
		return
	}
	var body struct {
		FileIDs  []int64 `json:"file_ids"`
		All      bool    `json:"all"`
		Reviewed bool    `json:"reviewed"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if !body.Reviewed {
		fail(w, 400, "review_required", "Review the unresolved files before discovering catalog entries.")
		return
	}
	if body.All && len(body.FileIDs) > 0 {
		fail(w, 400, "invalid_music", "Choose selected unresolved files or all unresolved files, not both.")
		return
	}
	if !body.All && (len(body.FileIDs) < 1 || len(body.FileIDs) > music.MaxDiscoverFiles) {
		fail(w, 400, "invalid_music", "Choose between 1 and 500 unresolved files, or all unresolved files.")
		return
	}
	for _, fileID := range body.FileIDs {
		if fileID < 1 {
			fail(w, 400, "invalid_music", "Choose valid unresolved files.")
			return
		}
	}
	result, err := s.music.DiscoverScan(r.Context(), id, body.FileIDs, body.All)
	if err != nil {
		s.musicError(w, err)
		return
	}
	send(w, 200, result)
}

func (s *Server) musicScanFingerprint(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, 503, "music_unavailable", "The music catalog is unavailable.")
		return
	}
	id, ok := musicID(w, r)
	if !ok {
		return
	}
	var body struct {
		FileIDs  []int64 `json:"file_ids"`
		All      bool    `json:"all"`
		Reviewed bool    `json:"reviewed"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if !body.Reviewed {
		fail(w, 400, "review_required", "Review the unresolved files before fingerprint matching.")
		return
	}
	if body.All && len(body.FileIDs) > 0 {
		fail(w, 400, "invalid_music", "Choose selected unresolved files or all unresolved files, not both.")
		return
	}
	if !body.All && (len(body.FileIDs) < 1 || len(body.FileIDs) > music.MaxDiscoverFiles) {
		fail(w, 400, "invalid_music", "Choose between 1 and 500 unresolved files; at most 20 are fingerprinted per run.")
		return
	}
	for _, fileID := range body.FileIDs {
		if fileID < 1 {
			fail(w, 400, "invalid_music", "Choose valid unresolved files.")
			return
		}
	}
	result, err := s.music.FingerprintScan(r.Context(), id, body.FileIDs, body.All)
	if err != nil {
		s.musicError(w, err)
		return
	}
	send(w, 200, result)
}

func (s *Server) musicScanMatchAll(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, 503, "music_unavailable", "The music catalog is unavailable.")
		return
	}
	id, ok := musicID(w, r)
	if !ok {
		return
	}
	var body struct {
		Reviewed bool `json:"reviewed"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if !body.Reviewed {
		fail(w, 400, "review_required", "Review the unresolved files before matching them all.")
		return
	}
	scan, err := s.music.RequestMatchAll(r.Context(), id, true)
	if err != nil {
		s.musicError(w, err)
		return
	}
	send(w, 202, scan)
}

func (s *Server) musicScanFinalize(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, 503, "music_unavailable", "The music catalog is unavailable.")
		return
	}
	id, ok := musicID(w, r)
	if !ok {
		return
	}
	var body struct {
		Reviewed bool `json:"reviewed"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if !body.Reviewed {
		fail(w, 400, "review_required", "Review the matched files before registering and organizing them.")
		return
	}
	scan, err := s.music.RequestFinalize(r.Context(), id, true)
	if err != nil {
		s.musicError(w, err)
		return
	}
	send(w, 202, scan)
}

func (s *Server) musicAcoustID(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, 503, "music_unavailable", "The music catalog is unavailable.")
		return
	}
	switch r.Method {
	case http.MethodGet:
		status, err := s.music.AcoustIDStatus(r.Context())
		if err != nil {
			s.musicError(w, err)
			return
		}
		send(w, 200, status)
	case http.MethodPut:
		var body struct {
			APIKey string `json:"api_key"`
		}
		if !decodeJSON(w, r, &body) {
			return
		}
		if !music.ValidAcoustIDKey(body.APIKey) {
			fail(w, 400, "invalid_acoustid_key", "Enter an AcoustID application key of 8–128 printable characters.")
			return
		}
		if err := s.music.SaveAcoustIDKey(r.Context(), body.APIKey); err != nil {
			s.musicError(w, err)
			return
		}
		status, err := s.music.AcoustIDStatus(r.Context())
		if err != nil {
			s.musicError(w, err)
			return
		}
		send(w, 200, status)
	case http.MethodDelete:
		if err := s.music.DeleteAcoustIDKey(r.Context()); err != nil {
			s.musicError(w, err)
			return
		}
		status, err := s.music.AcoustIDStatus(r.Context())
		if err != nil {
			s.musicError(w, err)
			return
		}
		send(w, 200, status)
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		fail(w, 405, "method_not_allowed", "Use GET, PUT or DELETE.")
	}
}

func (s *Server) musicScanCancel(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, 503, "music_unavailable", "The music catalog is unavailable.")
		return
	}
	id, ok := musicID(w, r)
	if !ok {
		return
	}
	var body struct{}
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := s.music.CancelScan(r.Context(), id); err != nil {
		s.musicError(w, err)
		return
	}
	send(w, 200, map[string]bool{"cancelled": true})
}

func (s *Server) musicScanSkip(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, 503, "music_unavailable", "The music catalog is unavailable.")
		return
	}
	id, ok := musicID(w, r)
	if !ok {
		return
	}
	var body struct {
		FileID int64 `json:"file_id"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.FileID < 1 {
		fail(w, 400, "invalid_music", "Choose a scanned file to skip.")
		return
	}
	if err := s.music.SkipScanFile(r.Context(), id, body.FileID); err != nil {
		s.musicError(w, err)
		return
	}
	send(w, 200, map[string]bool{"skipped": true})
}

func (s *Server) musicFiles(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, 503, "music_unavailable", "The music catalog is unavailable.")
		return
	}
	page, valid := pageNumber(r, 100000)
	if !valid {
		fail(w, 400, "invalid_music", "Choose a valid page.")
		return
	}
	out, err := s.music.ListFiles(r.Context(), page)
	if err != nil {
		s.musicError(w, err)
		return
	}
	send(w, 200, out)
}

func (s *Server) musicEdits(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, 503, "music_unavailable", "The music catalog is unavailable.")
		return
	}
	page, valid := pageNumber(r, 100000)
	if !valid {
		fail(w, 400, "invalid_music", "Choose a valid page.")
		return
	}
	out, err := s.music.ListEditJobs(r.Context(), page)
	if err != nil {
		s.musicError(w, err)
		return
	}
	send(w, 200, out)
}

func (s *Server) musicEdit(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, 503, "music_unavailable", "The music catalog is unavailable.")
		return
	}
	id, ok := musicID(w, r)
	if !ok {
		return
	}
	out, err := s.music.EditJob(r.Context(), id)
	if err != nil {
		s.musicError(w, err)
		return
	}
	send(w, 200, out)
}

func (s *Server) musicEditPreview(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, 503, "music_unavailable", "The music catalog is unavailable.")
		return
	}
	var body struct {
		FileIDs []int64 `json:"file_ids"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if len(body.FileIDs) == 0 || len(body.FileIDs) > 500 {
		fail(w, 400, "invalid_music", "Choose between 1 and 500 registered files.")
		return
	}
	plan, err := s.music.PreviewEdits(r.Context(), body.FileIDs)
	if err != nil {
		s.musicError(w, err)
		return
	}
	send(w, 200, plan)
}

func (s *Server) musicEditApply(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, 503, "music_unavailable", "The music catalog is unavailable.")
		return
	}
	var body struct {
		FileIDs  []int64 `json:"file_ids"`
		PlanHash string  `json:"plan_hash"`
		Reviewed bool    `json:"reviewed"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if !body.Reviewed {
		fail(w, 400, "review_required", "Review the organization and tag changes before applying them.")
		return
	}
	if len(body.FileIDs) == 0 || len(body.FileIDs) > 500 || strings.TrimSpace(body.PlanHash) == "" {
		fail(w, 400, "invalid_music", "Choose between 1 and 500 registered files and a reviewed plan.")
		return
	}
	job, err := s.music.ApplyEdits(r.Context(), body.FileIDs, body.PlanHash, true)
	if err != nil {
		s.musicError(w, err)
		return
	}
	send(w, 202, job)
}

func (s *Server) musicEditRollback(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, 503, "music_unavailable", "The music catalog is unavailable.")
		return
	}
	id, ok := musicID(w, r)
	if !ok {
		return
	}
	var body struct {
		Reviewed bool `json:"reviewed"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if !body.Reviewed {
		fail(w, 400, "review_required", "Review the rollback before applying it.")
		return
	}
	job, err := s.music.RollbackEdits(r.Context(), id, true)
	if err != nil {
		s.musicError(w, err)
		return
	}
	send(w, 200, job)
}

func (s *Server) musicAlbumReleases(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	id, ok := musicID(w, r)
	if !ok {
		return
	}
	var body struct {
		Query string `json:"query"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	out, err := s.transfer.SearchMusicAlbum(r.Context(), id, strings.TrimSpace(body.Query))
	if err != nil {
		s.transferError(w, err)
		return
	}
	send(w, 200, map[string]any{"items": out, "warnings": s.transfer.IndexerWarnings(r.Context())})
}

func (s *Server) musicAlbumImport(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, 503, "music_unavailable", "The music catalog is unavailable.")
		return
	}
	id, ok := musicID(w, r)
	if !ok {
		return
	}
	var body struct {
		SourcePath string `json:"source_path"`
		Root       string `json:"root"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if strings.TrimSpace(body.SourcePath) == "" {
		fail(w, 400, "invalid_music", "Choose a completed download folder below the downloads root.")
		return
	}
	root := strings.TrimSpace(body.Root)
	if root == "" {
		root = "Music"
	}
	job, err := s.music.StartImport(r.Context(), id, body.SourcePath, root)
	if err != nil {
		s.musicError(w, err)
		return
	}
	send(w, 202, job)
}

func (s *Server) musicImports(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, 503, "music_unavailable", "The music catalog is unavailable.")
		return
	}
	page, valid := pageNumber(r, 100000)
	if !valid {
		fail(w, 400, "invalid_music", "Choose a valid page.")
		return
	}
	out, err := s.music.ListImports(r.Context(), page)
	if err != nil {
		s.musicError(w, err)
		return
	}
	send(w, 200, out)
}

func (s *Server) musicImport(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, 503, "music_unavailable", "The music catalog is unavailable.")
		return
	}
	id, ok := musicID(w, r)
	if !ok {
		return
	}
	job, err := s.music.ImportJob(r.Context(), id)
	if err != nil {
		s.musicError(w, err)
		return
	}
	send(w, 200, job)
}

func musicDownloadID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" || len(id) > 80 {
		fail(w, 400, "invalid_music", "Choose a valid music download.")
		return "", false
	}
	return id, true
}

func (s *Server) musicAlbumDownload(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	id, ok := musicID(w, r)
	if !ok {
		return
	}
	var body struct {
		ReleaseID string `json:"release_id"`
		Root      string `json:"root"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	releaseID := strings.TrimSpace(body.ReleaseID)
	if releaseID == "" {
		fail(w, 400, "invalid_music", "Choose a release to download.")
		return
	}
	root := strings.TrimSpace(body.Root)
	if root == "" {
		root = "Music"
	}
	job, err := s.transfer.QueueMusic(r.Context(), id, releaseID, root)
	if err != nil {
		s.transferError(w, err)
		return
	}
	send(w, 202, job)
}

func (s *Server) musicDownloads(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	page, valid := pageNumber(r, 100000)
	if !valid {
		fail(w, 400, "invalid_music", "Choose a valid page.")
		return
	}
	out, err := s.transfer.MusicJobs(r.Context(), page)
	if err != nil {
		s.transferError(w, err)
		return
	}
	send(w, 200, out)
}

func (s *Server) musicDownload(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	id, ok := musicDownloadID(w, r)
	if !ok {
		return
	}
	job, err := s.transfer.MusicJob(r.Context(), id)
	if err != nil {
		s.transferError(w, err)
		return
	}
	send(w, 200, job)
}

func (s *Server) musicDownloadFiles(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	id, ok := musicDownloadID(w, r)
	if !ok {
		return
	}
	files, err := s.transfer.MusicDownloadFiles(r.Context(), id)
	if err != nil {
		s.transferError(w, err)
		return
	}
	send(w, 200, map[string]any{"items": files})
}

func (s *Server) musicDownloadMapping(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	id, ok := musicDownloadID(w, r)
	if !ok {
		return
	}
	var body struct {
		Mappings []struct {
			FileIndex int   `json:"file_index"`
			TrackID   int64 `json:"track_id"`
		} `json:"mappings"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	mapping := map[int]int64{}
	for _, entry := range body.Mappings {
		if entry.FileIndex < 0 || entry.TrackID < 1 {
			fail(w, 400, "invalid_music_mapping", "Map every file to a valid catalog track.")
			return
		}
		if _, exists := mapping[entry.FileIndex]; exists {
			fail(w, 400, "invalid_music_mapping", "A file was mapped more than once.")
			return
		}
		mapping[entry.FileIndex] = entry.TrackID
	}
	if len(mapping) == 0 {
		fail(w, 400, "invalid_music_mapping", "Map every file to a valid catalog track.")
		return
	}
	job, err := s.transfer.MapMusicDownload(r.Context(), id, mapping)
	if err != nil {
		var provider *music.Error
		if errors.As(err, &provider) {
			s.musicError(w, err)
			return
		}
		s.transferError(w, err)
		return
	}
	send(w, 200, job)
}

func (s *Server) musicDownloadCancel(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	id, ok := musicDownloadID(w, r)
	if !ok {
		return
	}
	var body struct{}
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := s.transfer.CancelMusic(r.Context(), id); err != nil {
		s.transferError(w, err)
		return
	}
	send(w, 200, map[string]bool{"cancelled": true})
}

func (s *Server) musicDownloadRetry(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	id, ok := musicDownloadID(w, r)
	if !ok {
		return
	}
	var body struct{}
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := s.transfer.RetryMusic(r.Context(), id); err != nil {
		s.transferError(w, err)
		return
	}
	send(w, 200, map[string]bool{"retried": true})
}

func musicID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		fail(w, 400, "invalid_music", "Choose a valid music catalog ID.")
		return 0, false
	}
	return id, true
}
