package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/faaaaq/ReelScarey/internal/music"
	"github.com/faaaaq/ReelScarey/internal/organization"
)

func (s *Server) musicError(w http.ResponseWriter, err error) {
	var media *organization.Error
	if errors.As(err, &media) {
		s.organizationError(w, err)
		return
	}
	var provider *music.Error
	if errors.As(err, &provider) {
		status := http.StatusBadGateway
		switch provider.Code {
		case "music_invalid_request", "invalid_music":
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
		case "music_profile_name_taken", "music_correction_conflict", "music_local_track", "music_ambiguous_artist", "music_ambiguous_album",
			"music_upgrade_state", "music_upgrade_empty", "music_upgrade_overlap", "music_upgrade_ambiguous", "music_upgrade_changed":
			status = http.StatusConflict
		}
		if provider.RetryAfter > 0 {
			w.Header().Set("Retry-After", strconv.Itoa(provider.RetryAfter))
		}
		fail(w, status, provider.Code, provider.Message)
		return
	}
	if errors.Is(err, music.ErrMusicServerUserExists) {
		fail(w, 409, "music_server_user_exists", err.Error())
		return
	}
	if errors.Is(err, music.ErrMusicServerUserAbsent) {
		fail(w, 404, "not_found", err.Error())
		return
	}
	if errors.Is(err, music.ErrMusicServerUsername) || errors.Is(err, music.ErrMusicServerEmail) || errors.Is(err, music.ErrMusicServerPassword) {
		fail(w, 400, "invalid_music", err.Error())
		return
	}
	if errors.Is(err, music.ErrPlanChanged) {
		fail(w, 409, "plan_changed", err.Error())
		return
	}
	if errors.Is(err, music.ErrNotFound) {
		fail(w, 404, "not_found", err.Error())
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
		Root        string `json:"root"`
		Format      string `json:"format"`
		AudioSource string `json:"audio_source"`
		Reviewed    bool   `json:"reviewed"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if !body.Reviewed {
		fail(w, 400, "review_required", "Review the album before starting the download.")
		return
	}
	job, err := s.music.StartAlbumAcquisition(r.Context(), id, body.Root, body.Format, body.AudioSource)
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
		Root        string `json:"root"`
		Format      string `json:"format"`
		AudioSource string `json:"audio_source"`
		Reviewed    bool   `json:"reviewed"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if !body.Reviewed {
		fail(w, 400, "review_required", "Review the album before starting the download.")
		return
	}
	job, err := s.music.StartProviderAlbumAcquisition(r.Context(), strconv.FormatInt(id, 10), body.Root, body.Format, body.AudioSource)
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

func (s *Server) musicAcquisitionAcknowledge(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, 503, "music_unavailable", "The music catalog is unavailable.")
		return
	}
	id, ok := musicID(w, r)
	if !ok {
		return
	}
	job, err := s.music.AcknowledgeAcquisition(r.Context(), id)
	if err != nil {
		s.musicError(w, err)
		return
	}
	send(w, 200, job)
}

func (s *Server) musicAcquisitionRetry(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, 503, "music_unavailable", "The music catalog is unavailable.")
		return
	}
	id, ok := musicID(w, r)
	if !ok {
		return
	}
	job, err := s.music.RetryAcquisitionTracks(r.Context(), id)
	if err != nil {
		s.musicError(w, err)
		return
	}
	send(w, 200, job)
}

func (s *Server) musicAcquisitionTrackRetry(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, 503, "music_unavailable", "The music catalog is unavailable.")
		return
	}
	id, ok := musicID(w, r)
	if !ok {
		return
	}
	trackID, err := strconv.ParseInt(r.PathValue("track"), 10, 64)
	if err != nil || trackID < 1 {
		fail(w, 400, "invalid_music", "Choose a failed acquisition track.")
		return
	}
	var body struct {
		SourceURL string `json:"source_url"`
		Reviewed  bool   `json:"reviewed"`
	}
	if !decodeBoundedJSON(w, r, &body, 8<<10) {
		return
	}
	job, err := s.music.RetryAcquisitionTrack(r.Context(), id, trackID, body.SourceURL, body.Reviewed)
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
	// The browser player defaults to the saved streaming quality (the
	// webPlayerStream helper in subsonic.go shares the conversion planner
	// with the Subsonic endpoint); "original" or unreadable preferences
	// stream the pristine file byte-for-byte as before. A client that
	// cannot decode the default container may request a compatible format
	// explicitly (capability negotiation); the params are validated and
	// clamped before they reach the planner.
	started := time.Now()
	playback, err := s.webPlayerStream(r.Context(), id,
		music.NormalizeStreamFormat(r.URL.Query().Get("format")),
		clampStreamBitRate(r.URL.Query().Get("maxBitRate")),
		subsonicQueryInt(r, "timeOffset"), r.URL.Query().Get("compat") == "mp3")
	if err != nil {
		s.musicError(w, err)
		return
	}
	defer playback.Close()
	if key, ok := browserMusicStreamKey(r, id); ok {
		s.musicStreams.put(key, browserMusicStreamInfo{
			Codec:         playback.Codec,
			BitrateKbps:   playback.BitrateKbps,
			Mode:          playback.Mode,
			RangeSeekable: playback.Body == nil,
		})
	}
	playbackDiagnosticHeaders(w, playback, started)
	w.Header().Set("Content-Type", playback.ContentType)
	w.Header().Set("Content-Disposition", "inline")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if playback.Body != nil {
		// On-the-fly conversion output has no known length and is not
		// range-seekable.
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Accept-Ranges", "none")
		w.WriteHeader(http.StatusOK)
		_, _ = io.Copy(w, playback.Body)
		return
	}
	http.ServeContent(w, r, playback.Name, playback.ModTime, playback.File)
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
		// legacy_token_configured (RS-0018) surfaces the legacy shared token —
		// a full-rights god credential — so the administrator can discover it
		// and delete it. TokenHint is populated only when the shared token
		// exists in the secret store, so its presence is exactly that signal;
		// the remaining fields mirror MusicServerStatus unchanged.
		send(w, 200, map[string]any{
			"enabled":                 status.Enabled,
			"configured":              status.Configured,
			"token_hint":              status.TokenHint,
			"users":                   status.Users,
			"revision":                status.Revision,
			"updated_at":              status.UpdatedAt,
			"legacy_token_configured": status.TokenHint != "",
		})
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

// musicServerUserBody carries the editable fields of one music server account.
// Every role is a pointer so an omitted field keeps the stored value.
type musicServerUserBody struct {
	Username          string `json:"username"`
	Email             string `json:"email"`
	Password          string `json:"password"`
	ScrobblingEnabled *bool  `json:"scrobbling_enabled"`
	AdminRole         *bool  `json:"admin_role"`
	SettingsRole      *bool  `json:"settings_role"`
	DownloadRole      *bool  `json:"download_role"`
	UploadRole        *bool  `json:"upload_role"`
	PlaylistRole      *bool  `json:"playlist_role"`
	CoverArtRole      *bool  `json:"cover_art_role"`
	ShareRole         *bool  `json:"share_role"`
	// Optional quality overrides: an omitted field keeps the stored value;
	// an explicit "" clears the override so the account follows global.
	StreamQuality   *string `json:"stream_quality"`
	DownloadProfile *string `json:"download_profile"`
}

func (b musicServerUserBody) input() music.MusicServerUserInput {
	return music.MusicServerUserInput{
		Username:          b.Username,
		Email:             b.Email,
		Password:          b.Password,
		ScrobblingEnabled: b.ScrobblingEnabled,
		AdminRole:         b.AdminRole,
		SettingsRole:      b.SettingsRole,
		DownloadRole:      b.DownloadRole,
		UploadRole:        b.UploadRole,
		PlaylistRole:      b.PlaylistRole,
		CoverArtRole:      b.CoverArtRole,
		ShareRole:         b.ShareRole,
		StreamQuality:     b.StreamQuality,
		DownloadProfile:   b.DownloadProfile,
	}
}

// musicServerUsers lists named accounts. Identity creation belongs to central
// Accounts; this legacy collection remains read-only.
func (s *Server) musicServerUsers(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, 503, "music_unavailable", "The music catalog is unavailable.")
		return
	}
	switch r.Method {
	case http.MethodGet:
		users, err := s.music.ListMusicServerUsers(r.Context())
		if err != nil {
			s.musicError(w, err)
			return
		}
		send(w, 200, map[string]any{"users": users})
	case http.MethodPost:
		var body musicServerUserBody
		if !decodeJSON(w, r, &body) {
			return
		}
		fail(w, http.StatusConflict, "use_central_accounts", "Create the account and grant Subsonic access from Accounts; protocol roles remain editable here.")
	default:
		w.Header().Set("Allow", "GET, POST")
		fail(w, 405, "method_not_allowed", "Use GET or POST.")
	}
}

// musicServerUser reads one account or edits its protocol roles. Identity
// deletion and credential changes belong to central Accounts.
func (s *Server) musicServerUser(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, 503, "music_unavailable", "The music catalog is unavailable.")
		return
	}
	id, ok := musicID(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		user, found, err := s.musicMusicServerUser(r.Context(), id)
		if err != nil {
			s.musicError(w, err)
			return
		}
		if !found {
			fail(w, 404, "not_found", music.ErrMusicServerUserAbsent.Error())
			return
		}
		send(w, 200, user)
	case http.MethodPut:
		var body musicServerUserBody
		if !decodeJSON(w, r, &body) {
			return
		}
		if body.Password != "" {
			fail(w, http.StatusConflict, "use_central_accounts", "Reset the shared password from Accounts; protocol roles remain editable here.")
			return
		}
		user, err := s.music.UpdateMusicServerUser(r.Context(), id, body.input())
		if err != nil {
			s.musicError(w, err)
			return
		}
		send(w, 200, user)
	case http.MethodDelete:
		fail(w, http.StatusConflict, "use_central_accounts", "Remove the account or revoke Subsonic access from Accounts; protocol roles remain editable here.")
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		fail(w, 405, "method_not_allowed", "Use GET, PUT or DELETE.")
	}
}

// musicServerUserPassword issues a new generated password for one account.
func (s *Server) musicServerUserPassword(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, 503, "music_unavailable", "The music catalog is unavailable.")
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		fail(w, 405, "method_not_allowed", "Use POST.")
		return
	}
	if _, ok := musicID(w, r); !ok {
		return
	}
	fail(w, http.StatusConflict, "use_central_accounts", "Reset the shared password from Accounts so browser and Subsonic credentials stay synchronized.")
}

// musicMusicServerUser resolves one account by ID using the list projection.
func (s *Server) musicMusicServerUser(ctx context.Context, id int64) (music.MusicServerUser, bool, error) {
	users, err := s.music.ListMusicServerUsers(ctx)
	if err != nil {
		return music.MusicServerUser{}, false, err
	}
	for _, user := range users {
		if user.ID == id {
			return user, true, nil
		}
	}
	return music.MusicServerUser{}, false, nil
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

// musicArtistRefresh re-resolves one artist's provider-sourced metadata: the
// canonical MusicBrainz details, the verified artist image and the covers of
// the artist's catalog albums.
func (s *Server) musicArtistRefresh(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, 503, "music_unavailable", "The music catalog is unavailable.")
		return
	}
	id, ok := musicID(w, r)
	if !ok {
		return
	}
	result, err := s.music.RefreshArtistMetadata(r.Context(), id)
	if err != nil {
		s.musicError(w, err)
		return
	}
	send(w, 200, result)
}

// musicAlbumRefresh re-resolves one album's cover art and, for a canonical
// release group, its title and year.
func (s *Server) musicAlbumRefresh(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, 503, "music_unavailable", "The music catalog is unavailable.")
		return
	}
	id, ok := musicID(w, r)
	if !ok {
		return
	}
	result, err := s.music.RefreshAlbumMetadata(r.Context(), id)
	if err != nil {
		s.musicError(w, err)
		return
	}
	send(w, 200, result)
}

func (s *Server) musicAlbumRestore(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, 503, "music_unavailable", "The music catalog is unavailable.")
		return
	}
	id, ok := musicID(w, r)
	if !ok {
		return
	}
	var body struct {
		Field string `json:"field"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	result, err := s.music.RestoreAlbumMetadataField(r.Context(), id, body.Field)
	if err != nil {
		s.musicError(w, err)
		return
	}
	send(w, 200, result)
}

func (s *Server) musicArtistRestore(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, 503, "music_unavailable", "The music catalog is unavailable.")
		return
	}
	id, ok := musicID(w, r)
	if !ok {
		return
	}
	var body struct {
		Field string `json:"field"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	result, err := s.music.RestoreArtistMetadataField(r.Context(), id, body.Field)
	if err != nil {
		s.musicError(w, err)
		return
	}
	send(w, 200, result)
}

// musicLibraryMetadataRefresh runs one bounded batch of a library-wide
// metadata refresh. The response carries the next cursor until Done.
func (s *Server) musicLibraryMetadataRefresh(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, 503, "music_unavailable", "The music catalog is unavailable.")
		return
	}
	if r.Method != http.MethodPost {
		fail(w, 405, "invalid_music", "Use POST to refresh library metadata.")
		return
	}
	var body struct {
		Cursor int64 `json:"cursor"`
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4<<10))
	if err != nil {
		fail(w, 400, "invalid_music", "The refresh request could not be read.")
		return
	}
	if len(strings.TrimSpace(string(raw))) > 0 {
		if err := json.Unmarshal(raw, &body); err != nil {
			fail(w, 400, "invalid_music", "The refresh request could not be read.")
			return
		}
	}
	result, err := s.music.RefreshLibraryMetadataBatch(r.Context(), body.Cursor)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			result.Message = "The refresh batch stopped at its provider work limit; send it again to continue."
			result.Done = false
			send(w, 200, result)
			return
		}
		s.musicError(w, err)
		return
	}
	send(w, 200, result)
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

func (s *Server) musicScanAlbumLookup(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, 503, "music_unavailable", "The music catalog is unavailable.")
		return
	}
	if _, ok := musicID(w, r); !ok {
		return
	}
	query := r.URL.Query()
	search, preview := s.music.SearchScanAlbums, s.music.PreviewScanAlbum
	id := query.Get("release_group_id")
	switch query.Get("source") {
	case "", "musicbrainz":
	case "itunes":
		search, preview = s.music.SearchITunesScanAlbums, s.music.PreviewITunesScanAlbum
		id = query.Get("collection_id")
	default:
		fail(w, 400, "invalid_music", "Choose MusicBrainz or iTunes.")
		return
	}
	if id != "" {
		album, err := preview(r.Context(), id)
		if err != nil {
			s.musicError(w, err)
			return
		}
		send(w, 200, album)
		return
	}
	items, complete, err := search(r.Context(), query.Get("album"))
	if err != nil {
		s.musicError(w, err)
		return
	}
	send(w, 200, map[string]any{"items": items, "complete": complete})
}

func (s *Server) musicScanCorrections(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, 503, "music_unavailable", "The music catalog is unavailable.")
		return
	}
	id, ok := musicID(w, r)
	if !ok {
		return
	}
	var body struct {
		music.ScanCorrections
		Reviewed bool `json:"reviewed"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if !body.Reviewed {
		fail(w, 400, "review_required", "Review the track assignments before saving.")
		return
	}
	if err := s.music.CorrectScanMatches(r.Context(), id, body.ScanCorrections); err != nil {
		s.musicError(w, err)
		return
	}
	send(w, 200, map[string]int{"matched": len(body.Files)})
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

// musicProfiles serves GET and POST /api/v1/library/music/profiles. GET lists
// the builtin quality profiles followed by saved custom rows; POST creates a
// custom profile. Builtin profiles are code and can never be created here.
func (s *Server) musicProfiles(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, 503, "music_unavailable", "The music catalog is unavailable.")
		return
	}
	switch r.Method {
	case http.MethodGet:
		profiles, err := s.music.ListProfiles(r.Context())
		if err != nil {
			s.musicError(w, err)
			return
		}
		send(w, 200, map[string]any{"profiles": profiles})
	case http.MethodPost:
		var body music.QualityProfile
		if !decodeJSON(w, r, &body) {
			return
		}
		// Creation only: updates go through PUT /profiles/{id} so a posted ID
		// can never silently rewrite an existing custom row.
		body.ID = 0
		profile, err := s.music.SaveCustomProfile(r.Context(), body)
		if err != nil {
			s.musicError(w, err)
			return
		}
		send(w, 201, profile)
	default:
		w.Header().Set("Allow", "GET, POST")
		fail(w, 405, "method_not_allowed", "Use GET or POST.")
	}
}

// musicProfile serves PUT and DELETE /api/v1/library/music/profiles/{id} for
// custom quality profiles. Both actions fail closed through the music service
// when the ID is missing or names a builtin profile.
func (s *Server) musicProfile(w http.ResponseWriter, r *http.Request) {
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
		var body music.QualityProfile
		if !decodeJSON(w, r, &body) {
			return
		}
		body.ID = id
		profile, err := s.music.SaveCustomProfile(r.Context(), body)
		if err != nil {
			s.musicError(w, err)
			return
		}
		send(w, 200, profile)
	case http.MethodDelete:
		if err := s.music.DeleteCustomProfile(r.Context(), id); err != nil {
			s.musicError(w, err)
			return
		}
		send(w, 200, map[string]bool{"deleted": true})
	default:
		w.Header().Set("Allow", "PUT, DELETE")
		fail(w, 405, "method_not_allowed", "Use PUT or DELETE.")
	}
}

// musicConversions serves GET /api/v1/library/music/conversions?page=.
func (s *Server) musicConversions(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, 503, "music_unavailable", "The music catalog is unavailable.")
		return
	}
	page, valid := pageNumber(r, 100000)
	if !valid {
		fail(w, 400, "invalid_music", "Choose a valid conversion page.")
		return
	}
	jobs, err := s.music.ListConversions(r.Context(), page)
	if err != nil {
		s.musicError(w, err)
		return
	}
	send(w, 200, jobs)
}

// musicConversionAction serves POST /api/v1/library/music/conversions/{id}/
// {cancel|retry}. The action segment is allowlisted so an unknown verb is a
// plain 404 rather than a forwarded mutation.
func (s *Server) musicConversionAction(w http.ResponseWriter, r *http.Request) {
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
	switch r.PathValue("action") {
	case "cancel":
		if err := s.music.CancelConversion(r.Context(), id); err != nil {
			s.musicError(w, err)
			return
		}
		send(w, 200, map[string]bool{"cancelled": true})
	case "retry":
		if err := s.music.RetryConversion(r.Context(), id); err != nil {
			s.musicError(w, err)
			return
		}
		send(w, 200, map[string]bool{"retried": true})
	default:
		fail(w, 404, "not_found", "This conversion action does not exist.")
	}
}

// musicOptimizePreview serves POST /api/v1/library/music/optimize/preview.
// The dry run never writes files or enqueues jobs; file_ids is optional so the
// whole library can be previewed.
func (s *Server) musicOptimizePreview(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, 503, "music_unavailable", "The music catalog is unavailable.")
		return
	}
	var body struct {
		Profile string  `json:"profile"`
		FileIDs []int64 `json:"file_ids"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	profile := strings.TrimSpace(body.Profile)
	if profile == "" {
		fail(w, 400, "invalid_music", "Choose a quality profile to preview.")
		return
	}
	if len(body.FileIDs) > music.MaxDiscoverFiles {
		fail(w, 400, "invalid_music", "Preview at most 500 files at a time.")
		return
	}
	for _, fileID := range body.FileIDs {
		if fileID < 1 {
			fail(w, 400, "invalid_music", "Choose valid registered files.")
			return
		}
	}
	preview, err := s.music.PreviewOptimization(r.Context(), profile, body.FileIDs)
	if err != nil {
		s.musicError(w, err)
		return
	}
	send(w, 200, preview)
}

// musicOptimizeApply serves POST /api/v1/library/music/optimize/apply. This is
// the explicit administrator action behind the dry run: it enqueues bounded
// library conversion jobs and never replaces files on its own.
func (s *Server) musicOptimizeApply(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, 503, "music_unavailable", "The music catalog is unavailable.")
		return
	}
	var body struct {
		Profile string  `json:"profile"`
		FileIDs []int64 `json:"file_ids"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	profile := strings.TrimSpace(body.Profile)
	if profile == "" {
		fail(w, 400, "invalid_music", "Choose a quality profile to apply.")
		return
	}
	if len(body.FileIDs) < 1 || len(body.FileIDs) > music.MaxDiscoverFiles {
		fail(w, 400, "invalid_music", "Choose between 1 and 500 registered files.")
		return
	}
	for _, fileID := range body.FileIDs {
		if fileID < 1 {
			fail(w, 400, "invalid_music", "Choose valid registered files.")
			return
		}
	}
	enqueued, err := s.music.ApplyOptimization(r.Context(), profile, body.FileIDs)
	if err != nil {
		s.musicError(w, err)
		return
	}
	send(w, 202, map[string]any{"enqueued": enqueued})
}

// musicCache serves GET and DELETE /api/v1/library/music/cache. The stats are
// re-keyed to snake_case for the shared frontend contract; Clear removes cache
// files only and never registered music content.
func (s *Server) musicCache(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, 503, "music_unavailable", "The music catalog is unavailable.")
		return
	}
	switch r.Method {
	case http.MethodGet:
		stats, err := s.music.StreamCacheStats(r.Context())
		if err != nil {
			s.musicError(w, err)
			return
		}
		send(w, 200, map[string]any{
			"entries": stats.Entries, "size_bytes": stats.SizeBytes,
			"limit_bytes": stats.LimitBytes, "hits": stats.Hits, "misses": stats.Misses,
			"direct_streams": stats.DirectStreams, "live_streams": stats.LiveStreams,
			"offset_streams": stats.OffsetStreams, "active_transcodes": stats.ActiveTranscodes,
			"active_cache_fills": stats.ActiveCacheFills, "cache_fill_failures": stats.CacheFillFailures,
		})
	case http.MethodDelete:
		if err := s.music.ClearStreamCache(r.Context()); err != nil {
			s.musicError(w, err)
			return
		}
		send(w, 200, map[string]bool{"cleared": true})
	default:
		w.Header().Set("Allow", "GET, DELETE")
		fail(w, 405, "method_not_allowed", "Use GET or DELETE.")
	}
}

// musicFileExport serves GET /api/v1/library/music/files/{id}/export?profile=KEY.
// Profiles with the "original" codec stream the registered file directly with
// byte-range support. A converting profile serves a finished, self-contained
// audio file (metadata and artwork intact) from a validated library variant
// or a fresh conversion into an isolated temporary directory; the stream
// cache is never used because its encodes strip metadata. The route sits
// behind the same auth/CSRF middleware as every other music API; nothing in
// this handler weakens that.
func (s *Server) musicFileExport(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, 503, "music_unavailable", "The music catalog is unavailable.")
		return
	}
	id, ok := musicID(w, r)
	if !ok {
		return
	}
	profileKey := strings.TrimSpace(r.URL.Query().Get("profile"))
	if profileKey == "" {
		fail(w, 400, "invalid_music", "Choose a quality profile for the export.")
		return
	}
	profile, err := s.music.ResolveProfileKey(r.Context(), profileKey)
	if err != nil {
		s.musicError(w, err)
		return
	}
	if profile.Codec == "original" {
		stream, err := s.music.OpenAudioFile(r.Context(), id)
		if err != nil {
			s.musicError(w, err)
			return
		}
		defer stream.File.Close()
		w.Header().Set("Content-Type", stream.ContentType)
		w.Header().Set("Content-Disposition", "attachment")
		w.Header().Set("Cache-Control", "private, max-age=3600")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		http.ServeContent(w, r, stream.Name, stream.ModTime, stream.File)
		return
	}
	// exportOpener is the optional finished-file export capability of the
	// music service. The concrete music.Service always implements it; test
	// doubles that predate finished-file exports keep the legacy
	// playback-stream path below, so the MusicService interface and the
	// existing handler tests stay unchanged.
	opener, ok := s.music.(exportOpener)
	if !ok {
		playback, err := s.music.OpenPlayback(r.Context(), id, exportFormat(profile.Codec), profile.BitrateKbps, 0)
		if err != nil {
			s.musicError(w, err)
			return
		}
		defer playback.Close()
		w.Header().Set("Content-Type", playback.ContentType)
		w.Header().Set("Content-Disposition", "attachment")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if playback.Body != nil {
			// An on-the-fly conversion has no known length; stream it as it is
			// produced. Cache hits and unconverted files take the ServeContent
			// path below and keep byte-range support.
			w.Header().Set("Cache-Control", "no-store")
			_, _ = io.Copy(w, playback.Body)
			return
		}
		w.Header().Set("Cache-Control", "private, max-age=3600")
		http.ServeContent(w, r, playback.Name, playback.ModTime, playback.File)
		return
	}
	// A converting profile serves a finished, self-contained audio file with
	// its metadata and artwork intact: either a validated library variant or
	// a fresh metadata-preserving conversion into an isolated temporary
	// directory. The stream cache is never used here because its encodes
	// strip metadata with -map_metadata -1.
	export, err := opener.OpenExportStream(r.Context(), id, profile)
	if err != nil {
		s.musicError(w, err)
		return
	}
	// Cleanup contract: Cleanup removes the export's temporary directory and
	// must run only after File is closed (an open file cannot be removed on
	// Windows), so Close is registered as the inner defer. Cleanup is
	// idempotent and best-effort.
	defer export.Cleanup()
	defer export.File.Close()
	w.Header().Set("Content-Type", export.ContentType)
	w.Header().Set("Content-Disposition", exportAttachmentDisposition(export.Name))
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, export.Name, export.ModTime, export.File)
}

// exportOpener is implemented by music.Service for finished-file exports.
type exportOpener interface {
	OpenExportStream(context.Context, int64, music.QualityProfile) (music.ExportStream, error)
}

// exportAttachmentDisposition builds an RFC 6266/RFC 5987 attachment header
// for a download filename. It contains only the base file name, never a
// filesystem path: an ASCII-safe quoted filename always travels first, and
// when the name is not pure ASCII a UTF-8 filename* parameter carries the
// exact name percent-encoded per RFC 5987's attr-char rules.
func exportAttachmentDisposition(name string) string {
	ascii := strings.Map(func(r rune) rune {
		switch {
		case r < 32 || r == 127 || r == '"' || r == '\\':
			return -1
		case r > 126:
			return '_'
		default:
			return r
		}
	}, name)
	if ascii == "" {
		ascii = "audio"
	}
	disposition := `attachment; filename="` + ascii + `"`
	if ascii != name {
		disposition += `; filename*=UTF-8''` + exportRFC5987Value(name)
	}
	return disposition
}

// exportRFC5987Value percent-encodes a filename* parameter value exactly per
// RFC 5987: unreserved and attribute characters stay literal, every other
// character is UTF-8 encoded and percent-escaped byte by byte.
func exportRFC5987Value(name string) string {
	var encoded strings.Builder
	for _, char := range name {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' ||
			strings.ContainsRune("!#$&+-.^_`|~", char) {
			encoded.WriteRune(char)
			continue
		}
		for _, byteValue := range []byte(string(char)) {
			encoded.WriteString(fmt.Sprintf("%%%02X", byteValue))
		}
	}
	return encoded.String()
}

// exportFormat maps a profile codec to the requested-format value OpenPlayback
// understands. Empty means the profile already matched the source and the
// planner passes it through unchanged.
func exportFormat(codec string) string {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "mp3", "flac", "aac", "opus":
		return strings.ToLower(strings.TrimSpace(codec))
	}
	return ""
}
