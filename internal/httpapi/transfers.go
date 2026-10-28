package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/faaaaq/ReelScarey/internal/organization"
	"github.com/faaaaq/ReelScarey/internal/transfer"
)

func (s *Server) transferReady(w http.ResponseWriter) bool {
	if s.transfer == nil {
		fail(w, 503, "workflow_unavailable", "The download workflow is unavailable.")
		return false
	}
	return true
}
func (s *Server) transferError(w http.ResponseWriter, err error) {
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, 404, "not_found", "This movie or download does not exist.")
		return
	}
	var e *transfer.Error
	if errors.As(err, &e) {
		status := 502
		switch e.Code {
		case "invalid_settings", "invalid_search", "invalid_file", "invalid_release", "unsupported_torrent", "unsupported_nzb", "unsafe_path", "invalid_mapping", "invalid_scan", "too_many_episodes", "invalid_anime_selection":
			status = 400
		case "revision_conflict", "indexer_not_configured", "client_not_configured", "active_downloads", "release_expired", "movie_busy", "media_busy", "download_busy", "duplicate_hash", "episode_busy", "episode_changed", "episodes_not_loaded", "no_episode_targets", "mapping_locked", "scan_busy", "scan_history_full", "duplicate_file", "source_changed", "anime_order_changed":
			status = 409
		case "movie_not_found":
			status = 404
		}
		fail(w, status, e.Code, e.Message)
		return
	}
	s.internal(w, "movie_workflow", err)
}

func (s *Server) inspectMediaFile(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	id, ok := tvID(w, r)
	if !ok || !decodeJSON(w, r, &struct{}{}) {
		return
	}
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(30 * time.Minute))
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Minute)
	defer cancel()
	out, err := s.transfer.RefreshMediaProbe(ctx, id)
	if err != nil {
		var org *organization.Error
		if errors.As(err, &org) {
			s.organizationError(w, err)
		} else {
			s.transferError(w, err)
		}
		return
	}
	send(w, 200, out)
}
func integrationName(r *http.Request) (string, bool) {
	name := r.PathValue("name")
	return name, name == "torznab" || name == "qbittorrent" || name == "sabnzbd"
}
func (s *Server) integrationSettings(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	name, ok := integrationName(r)
	if !ok {
		fail(w, 404, "not_found", "Choose torznab, qbittorrent or sabnzbd.")
		return
	}
	switch r.Method {
	case http.MethodGet:
		out, err := s.transfer.Status(r.Context(), name)
		if err != nil {
			s.transferError(w, err)
			return
		}
		send(w, 200, out)
	case http.MethodPut:
		var err error
		if name == "torznab" {
			var in transfer.IndexerUpdate
			if !decodeJSON(w, r, &in) {
				return
			}
			err = s.transfer.ConfigureIndexer(r.Context(), in)
		} else if name == "sabnzbd" {
			var in transfer.SABUpdate
			if !decodeJSON(w, r, &in) {
				return
			}
			err = s.transfer.ConfigureSAB(r.Context(), in)
		} else {
			var in transfer.ClientUpdate
			if !decodeJSON(w, r, &in) {
				return
			}
			err = s.transfer.ConfigureClient(r.Context(), in)
		}
		if err != nil {
			s.transferError(w, err)
			return
		}
		send(w, 200, map[string]bool{"configured": true, "tested": true})
	case http.MethodDelete:
		if err := s.transfer.Remove(r.Context(), name); err != nil {
			s.transferError(w, err)
			return
		}
		send(w, 200, map[string]bool{"configured": false})
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		fail(w, 405, "method_not_allowed", "Use GET, PUT or DELETE.")
	}
}
func (s *Server) integrationTest(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	name, ok := integrationName(r)
	if !ok {
		fail(w, 404, "not_found", "Choose torznab, qbittorrent or sabnzbd.")
		return
	}
	if err := s.transfer.Test(r.Context(), name); err != nil {
		s.transferError(w, err)
		return
	}
	send(w, 200, map[string]bool{"connected": true})
}
func (s *Server) catalogMovie(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		fail(w, 400, "invalid_movie", "Choose a movie from your library.")
		return
	}
	item, err := s.transfer.MovieByID(r.Context(), id)
	if err != nil {
		s.transferError(w, err)
		return
	}
	send(w, 200, item)
}
func (s *Server) searchReleases(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	id, err := strconv.ParseInt(r.URL.Query().Get("movie_id"), 10, 64)
	if err != nil || id < 1 {
		fail(w, 400, "invalid_movie", "Choose a movie from your library.")
		return
	}
	out, err := s.transfer.Search(r.Context(), id, r.URL.Query().Get("query"))
	if err != nil {
		s.transferError(w, err)
		return
	}
	send(w, 200, map[string]any{"items": out, "warnings": s.transfer.IndexerWarnings(r.Context())})
}
func (s *Server) downloadCollection(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		page, valid := pageNumber(r, 100000)
		if !valid {
			fail(w, 400, "invalid_page", "Choose a valid page.")
			return
		}
		out, err := s.transfer.List(r.Context(), page)
		if err != nil {
			s.transferError(w, err)
			return
		}
		send(w, 200, out)
	case http.MethodPost:
		var body struct {
			ReleaseID string `json:"release_id"`
		}
		if !decodeJSON(w, r, &body) {
			return
		}
		if len(body.ReleaseID) != 32 {
			fail(w, 400, "invalid_release", "Choose a release from a current search.")
			return
		}
		out, created, err := s.transfer.Queue(r.Context(), body.ReleaseID)
		if err != nil {
			s.transferError(w, err)
			return
		}
		status := 200
		if created {
			status = 202
		}
		send(w, status, map[string]any{"item": out, "created": created})
	default:
		w.Header().Set("Allow", "GET, POST")
		fail(w, 405, "method_not_allowed", "Use GET or POST.")
	}
}
func (s *Server) downloadDetail(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	out, err := s.transfer.Job(r.Context(), r.PathValue("id"))
	if err != nil {
		s.transferError(w, err)
		return
	}
	send(w, 200, out)
}
func (s *Server) downloadAction(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	var err error
	switch r.PathValue("action") {
	case "retry":
		var body struct{}
		if !decodeJSON(w, r, &body) {
			return
		}
		err = s.transfer.Retry(r.Context(), r.PathValue("id"))
	case "stop":
		var body struct{}
		if !decodeJSON(w, r, &body) {
			return
		}
		err = s.transfer.Cancel(r.Context(), r.PathValue("id"))
	case "file":
		var body struct {
			Index *int `json:"index"`
		}
		if !decodeJSON(w, r, &body) {
			return
		}
		if body.Index == nil || *body.Index < 0 {
			fail(w, 400, "invalid_file", "Choose a movie file.")
			return
		}
		err = s.transfer.SelectFile(r.Context(), r.PathValue("id"), *body.Index)
	case "mapping":
		var body transfer.TVMappingUpdate
		if !decodeJSON(w, r, &body) {
			return
		}
		err = s.transfer.ConfirmTVMapping(r.Context(), r.PathValue("id"), body)
	default:
		fail(w, 404, "not_found", "This download action does not exist.")
		return
	}
	if err != nil {
		s.transferError(w, err)
		return
	}
	out, err := s.transfer.Job(r.Context(), r.PathValue("id"))
	if err != nil {
		s.transferError(w, err)
		return
	}
	send(w, 200, out)
}
