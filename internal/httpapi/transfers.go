package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"

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
		case "invalid_settings", "invalid_search", "invalid_file", "invalid_release", "unsupported_torrent", "unsafe_path":
			status = 400
		case "indexer_not_configured", "client_not_configured", "active_downloads", "release_expired", "movie_busy", "download_busy", "duplicate_hash":
			status = 409
		case "movie_not_found":
			status = 404
		}
		fail(w, status, e.Code, e.Message)
		return
	}
	s.internal(w, "movie_workflow", err)
}
func integrationName(r *http.Request) (string, bool) {
	name := r.PathValue("name")
	return name, name == "torznab" || name == "qbittorrent"
}
func (s *Server) integrationSettings(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	name, ok := integrationName(r)
	if !ok {
		fail(w, 404, "not_found", "Choose torznab or qbittorrent.")
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
		fail(w, 404, "not_found", "Choose torznab or qbittorrent.")
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
	send(w, 200, map[string]any{"items": out})
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
