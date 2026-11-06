package httpapi

import (
	"errors"
	"net/http"

	"github.com/faaaaq/ReelScarey/internal/announce"
)

func (s *Server) announceSources(w http.ResponseWriter, r *http.Request) {
	if s.announce == nil {
		fail(w, 503, "announce_unavailable", "IRC announce monitoring is not available in this deployment.")
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, e := s.announce.Sources(r.Context())
		if e != nil {
			s.announceError(w, e)
			return
		}
		send(w, 200, map[string]any{"items": items, "definitions": s.announce.Definitions().Items})
	case http.MethodPost:
		var in announce.SourceUpdate
		if !decodeJSON(w, r, &in) {
			return
		}
		out, e := s.announce.SaveSource(r.Context(), "", in)
		if e != nil {
			s.announceError(w, e)
			return
		}
		send(w, 201, out)
	default:
		w.Header().Set("Allow", "GET, POST")
		fail(w, 405, "method_not_allowed", "Use GET or POST.")
	}
}

func (s *Server) announceSource(w http.ResponseWriter, r *http.Request) {
	if s.announce == nil {
		fail(w, 503, "announce_unavailable", "IRC announce monitoring is not available in this deployment.")
		return
	}
	id := r.PathValue("id")
	switch r.Method {
	case http.MethodPut:
		var in announce.SourceUpdate
		if !decodeJSON(w, r, &in) {
			return
		}
		out, e := s.announce.SaveSource(r.Context(), id, in)
		if e != nil {
			s.announceError(w, e)
			return
		}
		send(w, 200, out)
	case http.MethodDelete:
		var in struct {
			Revision int `json:"revision"`
		}
		if !decodeJSON(w, r, &in) {
			return
		}
		if e := s.announce.DeleteSource(r.Context(), id, in.Revision); e != nil {
			s.announceError(w, e)
			return
		}
		send(w, 200, map[string]bool{"removed": true})
	default:
		w.Header().Set("Allow", "PUT, DELETE")
		fail(w, 405, "method_not_allowed", "Use PUT or DELETE.")
	}
}

func (s *Server) announceEvents(w http.ResponseWriter, r *http.Request) {
	if s.announce == nil {
		fail(w, 503, "announce_unavailable", "IRC announce monitoring is not available in this deployment.")
		return
	}
	page, valid := pageNumber(r, 100000)
	if !valid {
		fail(w, 400, "invalid_page", "Choose a valid page.")
		return
	}
	out, e := s.announce.Events(r.Context(), page)
	if e != nil {
		s.announceError(w, e)
		return
	}
	send(w, 200, out)
}

func (s *Server) announceError(w http.ResponseWriter, e error) {
	var problem *announce.Error
	if !errors.As(e, &problem) {
		fail(w, 500, "announce_unavailable", "IRC announce monitoring could not complete the request.")
		return
	}
	status := 400
	if problem.Code == "revision_conflict" {
		status = 409
	}
	fail(w, status, problem.Code, problem.Message)
}
