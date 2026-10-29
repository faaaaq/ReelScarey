package httpapi

import (
	"github.com/faaaaq/ReelScarey/internal/subtitle"
	"net/http"
)

func (s *Server) subtitleEdits(w http.ResponseWriter, r *http.Request) {
	if !s.subtitleReady(w) {
		return
	}
	id, ok := tvID(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case "GET":
		v, e := s.subtitle.Edits(r.Context(), id)
		if e != nil {
			s.subtitleError(w, e)
			return
		}
		send(w, 200, map[string]any{"items": v})
	case "POST":
		var in subtitle.EditRequest
		if !decodeBoundedJSON(w, r, &in, 8<<20) {
			return
		}
		v, e := s.subtitle.ReviewEdit(r.Context(), id, in)
		if e != nil {
			s.subtitleError(w, e)
			return
		}
		send(w, 200, v)
	default:
		w.Header().Set("Allow", "GET, POST")
		fail(w, 405, "method_not_allowed", "Use GET or POST.")
	}
}
func (s *Server) subtitleEditAction(w http.ResponseWriter, r *http.Request) {
	if !s.subtitleReady(w) {
		return
	}
	var in struct {
		Reviewed bool `json:"reviewed"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	var e error
	switch r.PathValue("action") {
	case "apply":
		e = s.subtitle.QueueEdit(r.Context(), r.PathValue("id"), in.Reviewed)
	case "retry":
		e = s.subtitle.RetryEdit(r.Context(), r.PathValue("id"))
	default:
		fail(w, 404, "not_found", "Choose apply or retry.")
		return
	}
	if e != nil {
		s.subtitleError(w, e)
		return
	}
	send(w, 202, map[string]bool{"queued": true})
}
