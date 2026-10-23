package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"

	"github.com/faaaaq/ReelScarey/internal/subtitle"
)

func (s *Server) subtitleReady(w http.ResponseWriter) bool {
	if s.subtitle == nil {
		fail(w, 503, "subtitles_unavailable", "The subtitle workflow is unavailable.")
		return false
	}
	return true
}
func (s *Server) subtitleError(w http.ResponseWriter, err error) {
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, 404, "not_found", "This registered media file or subtitle job does not exist.")
		return
	}
	var p *subtitle.Error
	if errors.As(err, &p) {
		status := 502
		switch p.Code {
		case "invalid_preferences", "invalid_credentials", "review_required", "invalid_subtitle":
			status = 400
		case "subtitle_busy", "revision_conflict", "candidate_expired", "provider_not_configured", "provider_changed", "pending_import":
			status = 409
		case "provider_quota", "provider_rate_limit":
			status = 429
		}
		fail(w, status, p.Code, p.Message)
		return
	}
	s.internal(w, "subtitle_workflow", err)
}
func (s *Server) subtitlePreferences(w http.ResponseWriter, r *http.Request) {
	if !s.subtitleReady(w) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		p, e := s.subtitle.Preferences(r.Context())
		if e != nil {
			s.subtitleError(w, e)
			return
		}
		send(w, 200, map[string]any{"preferences": p, "languages": subtitle.Languages})
	case http.MethodPut:
		var p subtitle.Preferences
		if !decodeJSON(w, r, &p) {
			return
		}
		p, e := s.subtitle.SavePreferences(r.Context(), p)
		if e != nil {
			s.subtitleError(w, e)
			return
		}
		send(w, 200, map[string]any{"preferences": p, "languages": subtitle.Languages})
	default:
		w.Header().Set("Allow", "GET, PUT")
		fail(w, 405, "method_not_allowed", "Use GET or PUT.")
	}
}
func (s *Server) subtitleProvider(w http.ResponseWriter, r *http.Request) {
	if !s.subtitleReady(w) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		v, e := s.subtitle.Status(r.Context())
		if e != nil {
			s.subtitleError(w, e)
			return
		}
		send(w, 200, v)
	case http.MethodPut:
		var in subtitle.CredentialUpdate
		if !decodeJSON(w, r, &in) {
			return
		}
		if e := s.subtitle.Configure(r.Context(), in); e != nil {
			s.subtitleError(w, e)
			return
		}
		v, e := s.subtitle.Status(r.Context())
		if e != nil {
			s.subtitleError(w, e)
			return
		}
		send(w, 200, v)
	case http.MethodDelete:
		if e := s.subtitle.Disconnect(r.Context()); e != nil {
			s.subtitleError(w, e)
			return
		}
		send(w, 200, map[string]bool{"configured": false})
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		fail(w, 405, "method_not_allowed", "Use GET, PUT or DELETE.")
	}
}
func (s *Server) subtitleProviderTest(w http.ResponseWriter, r *http.Request) {
	if !s.subtitleReady(w) {
		return
	}
	var in struct{}
	if !decodeJSON(w, r, &in) {
		return
	}
	if e := s.subtitle.Test(r.Context()); e != nil {
		s.subtitleError(w, e)
		return
	}
	send(w, 200, map[string]bool{"connected": true})
}
func (s *Server) subtitleFiles(w http.ResponseWriter, r *http.Request) {
	if !s.subtitleReady(w) {
		return
	}
	page, ok := pageNumber(r, 100000)
	if !ok {
		fail(w, 400, "invalid_page", "Choose a valid page.")
		return
	}
	v, e := s.subtitle.List(r.Context(), page)
	if e != nil {
		s.subtitleError(w, e)
		return
	}
	send(w, 200, v)
}
func (s *Server) subtitleFile(w http.ResponseWriter, r *http.Request) {
	if !s.subtitleReady(w) {
		return
	}
	id, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if e != nil || id < 1 {
		fail(w, 400, "invalid_id", "Choose a registered media file.")
		return
	}
	v, e := s.subtitle.File(r.Context(), id, true)
	if e != nil {
		s.subtitleError(w, e)
		return
	}
	send(w, 200, v)
}
func (s *Server) subtitleFileAction(w http.ResponseWriter, r *http.Request) {
	if !s.subtitleReady(w) {
		return
	}
	id, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if e != nil || id < 1 {
		fail(w, 400, "invalid_id", "Choose a registered media file.")
		return
	}
	var in struct{}
	if !decodeJSON(w, r, &in) {
		return
	}
	switch r.PathValue("action") {
	case "search":
		e = s.subtitle.Search(r.Context(), id)
	case "retry":
		e = s.subtitle.Retry(r.Context(), id)
	default:
		fail(w, 404, "not_found", "Choose search or retry.")
		return
	}
	if e != nil {
		s.subtitleError(w, e)
		return
	}
	send(w, 202, map[string]bool{"requested": true})
}
func (s *Server) subtitleDownload(w http.ResponseWriter, r *http.Request) {
	if !s.subtitleReady(w) {
		return
	}
	var in struct {
		Reviewed bool `json:"reviewed"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if e := s.subtitle.Select(r.Context(), r.PathValue("id"), in.Reviewed); e != nil {
		s.subtitleError(w, e)
		return
	}
	send(w, 202, map[string]bool{"requested": true})
}
