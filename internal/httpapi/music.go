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
		case "music_invalid_url", "music_invalid_musicbrainz_url":
			status = http.StatusBadRequest
		case "music_not_found", "music_file_missing", "music_artwork_missing":
			status = http.StatusNotFound
		case "music_server_unavailable":
			status = http.StatusConflict
		case "music_acquisition_unavailable":
			status = http.StatusConflict
		case "music_link_invalid", "music_link_unsupported", "music_link_empty":
			status = http.StatusBadRequest
		case "music_monitor_kind":
			status = http.StatusBadRequest
		case "music_rate_limited", "music_acoustid_rate_limited":
			status = http.StatusTooManyRequests
		case "music_acoustid_not_configured", "music_fingerprint_unavailable", "music_musicbrainz_unavailable":
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
		if !music.ValidListQuery(query, 1) {
			fail(w, 400, "invalid_music", "Enter at most 200 characters and a valid page.")
			return
		}
		if r.URL.Query().Get("page") == "all" {
			items, err := s.music.ListAllArtists(r.Context(), query)
			if errors.Is(err, music.ErrListAllLimit) {
				fail(w, 400, "library_too_large", err.Error())
				return
			}
			if err != nil {
				s.musicError(w, err)
				return
			}
			send(w, 200, items)
			return
		}
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

// musicArtistTracks lists tracks that have registered files. The artist page
// asks for a small limit, then reuses the same route to page through all of
// them.
func (s *Server) musicArtistTracks(w http.ResponseWriter, r *http.Request) {
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
		fail(w, 400, "invalid_music", "Choose a valid track page.")
		return
	}
	limit := 15
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 100 {
			fail(w, 400, "invalid_music", "Choose a track limit between 1 and 100.")
			return
		}
		limit = value
	}
	tracks, err := s.music.ArtistTracks(r.Context(), id, page, limit)
	if err != nil {
		s.musicError(w, err)
		return
	}
	send(w, 200, tracks)
}

func (s *Server) musicAlbums(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, 503, "music_unavailable", "The music catalog is unavailable.")
		return
	}
	page, valid := pageNumber(r, 100000)
	if !valid {
		fail(w, 400, "invalid_music", "Choose a valid album page.")
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("query"))
	if !music.ValidListQuery(query, page) {
		fail(w, 400, "invalid_music", "Enter at most 200 characters and a valid page.")
		return
	}
	limit := queryLimit(r, 48, 200)
	albums, err := s.music.ListAlbums(r.Context(), query, page, limit)
	if err != nil {
		s.musicError(w, err)
		return
	}
	send(w, 200, albums)
}

func (s *Server) musicTracks(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, 503, "music_unavailable", "The music catalog is unavailable.")
		return
	}
	page, valid := pageNumber(r, 100000)
	if !valid {
		fail(w, 400, "invalid_music", "Choose a valid track page.")
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("query"))
	if !music.ValidListQuery(query, page) {
		fail(w, 400, "invalid_music", "Enter at most 200 characters and a valid page.")
		return
	}
	limit := queryLimit(r, 100, 200)
	tracks, err := s.music.ListTracks(r.Context(), query, page, limit)
	if err != nil {
		s.musicError(w, err)
		return
	}
	send(w, 200, tracks)
}

func queryLimit(r *http.Request, fallback, maximum int) int {
	value, err := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("limit")))
	if err != nil || value < 1 {
		return fallback
	}
	if value > maximum {
		return maximum
	}
	return value
}

func (s *Server) musicArtistDiscography(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, 503, "music_unavailable", "The music catalog is unavailable.")
		return
	}
	id, ok := musicID(w, r)
	if !ok {
		return
	}
	limit := queryLimit(r, 100, 100)
	albums, err := s.music.ArtistDiscography(r.Context(), id, limit)
	if err != nil {
		s.musicError(w, err)
		return
	}
	send(w, 200, albums)
}

func (s *Server) musicAlbumAcquire(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, 503, "music_unavailable", "The music catalog is unavailable.")
		return
	}
	id, ok := musicID(w, r)
	if !ok {
		return
	}
	var body struct {
		Root     string `json:"root"`
		Format   string `json:"format"`
		Reviewed bool   `json:"reviewed"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if !body.Reviewed {
		fail(w, 400, "review_required", "Review the album before starting the download.")
		return
	}
	job, err := s.music.StartAlbumAcquisition(r.Context(), id, body.Root, body.Format)
	if err != nil {
		s.musicError(w, err)
		return
	}
	send(w, 202, job)
}

func (s *Server) musicProviderAlbumAcquire(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, 503, "music_unavailable", "The music catalog is unavailable.")
		return
	}
	id, ok := musicID(w, r)
	if !ok {
		return
	}
	var body struct {
		Root     string `json:"root"`
		Format   string `json:"format"`
		Reviewed bool   `json:"reviewed"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if !body.Reviewed {
		fail(w, 400, "review_required", "Review the album before starting the download.")
		return
	}
	job, err := s.music.StartProviderAlbumAcquisition(r.Context(), strconv.FormatInt(id, 10), body.Root, body.Format)
	if err != nil {
		s.musicError(w, err)
		return
	}
	send(w, 202, job)
}

func (s *Server) musicArtistTopTracks(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, 503, "music_unavailable", "The music catalog is unavailable.")
		return
	}
	id, ok := musicID(w, r)
	if !ok {
		return
	}
	limit := 20
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 50 {
			fail(w, 400, "invalid_music", "Choose a top-track limit between 1 and 50.")
			return
		}
		limit = value
	}
	tracks, err := s.music.ArtistTopTracks(r.Context(), id, limit)
	if err != nil {
		s.musicError(w, err)
		return
	}
	send(w, 200, tracks)
}

func (s *Server) musicAcquisitions(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, 503, "music_unavailable", "The music catalog is unavailable.")
		return
	}
	switch r.Method {
	case http.MethodGet:
		page, valid := pageNumber(r, 100000)
		if !valid {
			fail(w, 400, "invalid_music", "Choose a valid acquisition page.")
			return
		}
		jobs, err := s.music.ListAcquisitions(r.Context(), page)
		if err != nil {
			s.musicError(w, err)
			return
		}
		send(w, 200, jobs)
	case http.MethodPost:
		var body struct {
			SourceURL  string                        `json:"source_url"`
			SourceKind string                        `json:"source_kind"`
			Title      string                        `json:"title"`
			Root       string                        `json:"root"`
			Format     string                        `json:"format"`
			Tracks     []music.AcquisitionTrackInput `json:"tracks"`
			Reviewed   bool                          `json:"reviewed"`
		}
		if !decodeJSON(w, r, &body) {
			return
		}
		if !body.Reviewed {
			fail(w, 400, "review_required", "Review the selected tracks before starting the download.")
			return
		}
		job, err := s.music.StartAcquisition(r.Context(), body.SourceURL, body.SourceKind, body.Title, body.Root, body.Format, body.Tracks)
		if err != nil {
			s.musicError(w, err)
			return
		}
		send(w, 202, job)
	default:
		w.Header().Set("Allow", "GET, POST")
		fail(w, 405, "method_not_allowed", "Use GET or POST.")
	}
}

func (s *Server) musicAcquisition(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, 503, "music_unavailable", "The music catalog is unavailable.")
		return
	}
	id, ok := musicID(w, r)
	if !ok {
		return
	}
	job, err := s.music.Acquisition(r.Context(), id)
	if err != nil {
		s.musicError(w, err)
		return
	}
	send(w, 200, job)
}

func (s *Server) musicLinkPreview(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, 503, "music_unavailable", "The music catalog is unavailable.")
		return
	}
	var body struct {
		URL string `json:"url"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	preview, err := s.music.ResolveLink(r.Context(), body.URL)
	if err != nil {
		s.musicError(w, err)
		return
	}
	send(w, 200, preview)
}

func (s *Server) musicLinkAcquire(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, 503, "music_unavailable", "The music catalog is unavailable.")
		return
	}
	var body struct {
		URL          string `json:"url"`
		TrackIndices []int  `json:"track_indices"`
		Root         string `json:"root"`
		Format       string `json:"format"`
		Reviewed     bool   `json:"reviewed"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if !body.Reviewed {
		fail(w, 400, "review_required", "Review the selected tracks before starting the download.")
		return
	}
	preview, err := s.music.ResolveLink(r.Context(), body.URL)
	if err != nil {
		s.musicError(w, err)
		return
	}
	if len(body.TrackIndices) == 0 || len(body.TrackIndices) > 100 {
		fail(w, 400, "invalid_music", "Select between 1 and 100 tracks.")
		return
	}
	seen := map[int]bool{}
	tracks := make([]music.AcquisitionTrackInput, 0, len(body.TrackIndices))
	for _, index := range body.TrackIndices {
		if index < 0 || index >= len(preview.Tracks) || seen[index] {
			fail(w, 400, "invalid_music", "A selected track is not part of this link.")
			return
		}
		seen[index] = true
		track := preview.Tracks[index]
		tracks = append(tracks, music.AcquisitionTrackInput{
			Title: track.Title, Artist: track.Artist, Album: track.Album, AlbumArtist: track.AlbumArtist,
			Year: track.Year, TrackNumber: track.TrackNumber, DiscNumber: track.DiscNumber,
			DurationMS: track.DurationMS, CoverURL: track.CoverURL, SourceURL: track.SourceURL, SearchQuery: track.SearchQuery,
		})
	}
	job, err := s.music.StartAcquisition(r.Context(), body.URL, "link", preview.Title, body.Root, body.Format, tracks)
	if err != nil {
		s.musicError(w, err)
		return
	}
	send(w, 202, job)
}

func (s *Server) musicCharts(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, 503, "music_unavailable", "The music catalog is unavailable.")
		return
	}
	limit := 20
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 50 {
			fail(w, 400, "invalid_music", "Choose a chart limit between 1 and 50.")
			return
		}
		limit = value
	}
	tracks, err := s.music.ChartTracks(r.Context(), limit)
	if err != nil {
		s.musicError(w, err)
		return
	}
	send(w, 200, tracks)
}

func (s *Server) musicMonitor(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, 503, "music_unavailable", "The music catalog is unavailable.")
		return
	}
	switch r.Method {
	case http.MethodGet:
		settings, err := s.music.MonitorSettings(r.Context())
		if err != nil {
			s.musicError(w, err)
			return
		}
		send(w, 200, settings)
	case http.MethodPut:
		var body struct {
			Enabled       bool `json:"enabled"`
			IntervalHours int  `json:"interval_hours"`
			Revision      int  `json:"revision"`
		}
		if !decodeJSON(w, r, &body) {
			return
		}
		settings, err := s.music.SaveMonitorSettings(r.Context(), body.Enabled, body.IntervalHours, body.Revision)
		if err != nil {
			s.musicError(w, err)
			return
		}
		send(w, 200, settings)
	default:
		w.Header().Set("Allow", "GET, PUT")
		fail(w, 405, "method_not_allowed", "Use GET or PUT.")
	}
}

func (s *Server) musicMonitorSources(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, 503, "music_unavailable", "The music catalog is unavailable.")
		return
	}
	switch r.Method {
	case http.MethodGet:
		sources, err := s.music.ListMonitorSources(r.Context())
		if err != nil {
			s.musicError(w, err)
			return
		}
		send(w, 200, sources)
	case http.MethodPost:
		var body struct {
			URL string `json:"url"`
		}
		if !decodeJSON(w, r, &body) {
			return
		}
		source, err := s.music.AddMonitorSource(r.Context(), body.URL)
		if err != nil {
			s.musicError(w, err)
			return
		}
		send(w, 201, source)
	default:
		w.Header().Set("Allow", "GET, POST")
		fail(w, 405, "method_not_allowed", "Use GET or POST.")
	}
}

func (s *Server) musicMonitorSource(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, 503, "music_unavailable", "The music catalog is unavailable.")
		return
	}
	id, ok := musicID(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodPut:
		var body struct {
			Enabled bool `json:"enabled"`
		}
		if !decodeJSON(w, r, &body) {
			return
		}
		source, err := s.music.SetMonitorSourceEnabled(r.Context(), id, body.Enabled)
		if err != nil {
			s.musicError(w, err)
			return
		}
		send(w, 200, source)
	case http.MethodDelete:
		if err := s.music.DeleteMonitorSource(r.Context(), id); err != nil {
			s.musicError(w, err)
			return
		}
		send(w, 200, map[string]bool{"deleted": true})
	default:
		w.Header().Set("Allow", "PUT, DELETE")
		fail(w, 405, "method_not_allowed", "Use PUT or DELETE.")
	}
}

func (s *Server) musicMonitorRun(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, 503, "music_unavailable", "The music catalog is unavailable.")
		return
	}
	settings, err := s.music.TriggerMonitor(r.Context())
	if err != nil {
		s.musicError(w, err)
		return
	}
	send(w, 202, settings)
}

// musicFileStream serves one registered audio file with Range support so the
// browser player can seek without downloading the whole track. The URL carries
// only the catalog file ID; the stored relative path is never exposed.
func (s *Server) musicFileStream(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, 503, "music_unavailable", "The music catalog is unavailable.")
		return
	}
	id, ok := musicID(w, r)
	if !ok {
		return
	}
	stream, err := s.music.OpenAudioFile(r.Context(), id)
	if err != nil {
		s.musicError(w, err)
		return
	}
	defer stream.File.Close()
	w.Header().Set("Content-Type", stream.ContentType)
	w.Header().Set("Content-Disposition", "inline")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, stream.Name, stream.ModTime, stream.File)
}

func (s *Server) musicServer(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, 503, "music_unavailable", "The music catalog is unavailable.")
		return
	}
	switch r.Method {
	case http.MethodGet:
		status, err := s.music.MusicServer(r.Context())
		if err != nil {
			s.musicError(w, err)
			return
		}
		send(w, 200, status)
	case http.MethodPut:
		var body struct {
			Enabled  bool `json:"enabled"`
			Revision int  `json:"revision"`
		}
		if !decodeJSON(w, r, &body) {
			return
		}
		status, err := s.music.SaveMusicServer(r.Context(), body.Enabled, body.Revision)
		if err != nil {
			s.musicError(w, err)
			return
		}
		send(w, 200, status)
	default:
		w.Header().Set("Allow", "GET, PUT")
		fail(w, 405, "method_not_allowed", "Use GET or PUT.")
	}
}

func (s *Server) musicServerToken(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, 503, "music_unavailable", "The music catalog is unavailable.")
		return
	}
	switch r.Method {
	case http.MethodPost:
		token, status, err := s.music.GenerateMusicServerToken(r.Context())
		if err != nil {
			s.musicError(w, err)
			return
		}
		send(w, 201, map[string]any{"token": token, "server": status})
	case http.MethodDelete:
		status, err := s.music.DeleteMusicServerToken(r.Context())
		if err != nil {
			s.musicError(w, err)
			return
		}
		send(w, 200, status)
	default:
		w.Header().Set("Allow", "POST, DELETE")
		fail(w, 405, "method_not_allowed", "Use POST or DELETE.")
	}
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

func (s *Server) musicArtistImage(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, 503, "music_unavailable", "The music catalog is unavailable.")
		return
	}
	id, ok := musicID(w, r)
	if !ok {
		return
	}
	art, err := s.music.ArtistImage(r.Context(), id)
	if err != nil {
		s.musicError(w, err)
		return
	}
	defer art.File.Close()
	w.Header().Set("Content-Type", art.ContentType)
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, art.Name, art.ModTime, art.File)
}

func (s *Server) musicAlbumCover(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, 503, "music_unavailable", "The music catalog is unavailable.")
		return
	}
	id, ok := musicID(w, r)
	if !ok {
		return
	}
	art, err := s.music.AlbumImage(r.Context(), id)
	if err != nil {
		s.musicError(w, err)
		return
	}
	defer art.File.Close()
	w.Header().Set("Content-Type", art.ContentType)
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, art.Name, art.ModTime, art.File)
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
	out, err := s.music.ScanPageFiltered(r.Context(), id, page, r.URL.Query().Get("state"))
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

func (s *Server) musicMusicBrainz(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, 503, "music_unavailable", "The music catalog is unavailable.")
		return
	}
	switch r.Method {
	case http.MethodGet:
		status, err := s.music.MusicBrainzStatus(r.Context())
		if err != nil {
			s.musicError(w, err)
			return
		}
		send(w, 200, status)
	case http.MethodPut:
		var body struct {
			BaseURL string `json:"base_url"`
		}
		if !decodeJSON(w, r, &body) {
			return
		}
		if strings.TrimSpace(body.BaseURL) == "" {
			fail(w, 400, "invalid_musicbrainz_url", "Enter a MusicBrainz base URL, or reset to the public server.")
			return
		}
		status, err := s.music.SaveMusicBrainzBaseURL(r.Context(), body.BaseURL)
		if err != nil {
			s.musicError(w, err)
			return
		}
		send(w, 200, status)
	case http.MethodDelete:
		status, err := s.music.DeleteMusicBrainzBaseURL(r.Context())
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
		AlbumID int64   `json:"album_id"`
		Year    int     `json:"year"`
		Album   string  `json:"album"`
		Artist  string  `json:"artist"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	fileIDs := body.FileIDs
	if body.AlbumID > 0 {
		ids, err := s.music.AlbumFileIDs(r.Context(), body.AlbumID)
		if err != nil {
			s.musicError(w, err)
			return
		}
		fileIDs = ids
	}
	if len(fileIDs) == 0 || len(fileIDs) > 500 {
		fail(w, 400, "invalid_music", "Choose between 1 and 500 registered files.")
		return
	}
	plan, err := s.music.PreviewEditsWith(r.Context(), fileIDs, music.EditOverrides{Year: body.Year, Album: body.Album, Artist: body.Artist})
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
		AlbumID  int64   `json:"album_id"`
		Year     int     `json:"year"`
		Album    string  `json:"album"`
		Artist   string  `json:"artist"`
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
	fileIDs := body.FileIDs
	if body.AlbumID > 0 {
		ids, err := s.music.AlbumFileIDs(r.Context(), body.AlbumID)
		if err != nil {
			s.musicError(w, err)
			return
		}
		fileIDs = ids
	}
	if len(fileIDs) == 0 || len(fileIDs) > 500 || strings.TrimSpace(body.PlanHash) == "" {
		fail(w, 400, "invalid_music", "Choose between 1 and 500 registered files and a reviewed plan.")
		return
	}
	job, err := s.music.ApplyEditsWith(r.Context(), fileIDs, body.PlanHash, true, music.EditOverrides{Year: body.Year, Album: body.Album, Artist: body.Artist})
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
