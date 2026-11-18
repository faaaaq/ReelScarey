package httpapi

import (
	"net/http"
)

// titleQueue exposes the durable automatic embedded-title queue: how much work
// waits, whether it is paused, and an explicit way to stop the batch.
func (s *Server) titleQueue(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		out, err := s.organization.TitleQueue(r.Context())
		if err != nil {
			s.organizationError(w, err)
			return
		}
		send(w, 200, out)
	case http.MethodPut:
		var in struct {
			Paused bool `json:"paused"`
		}
		if !decodeJSON(w, r, &in) {
			return
		}
		out, err := s.organization.SetTitleQueuePaused(r.Context(), in.Paused)
		if err != nil {
			s.organizationError(w, err)
			return
		}
		send(w, 200, out)
	default:
		w.Header().Set("Allow", "GET, PUT")
		fail(w, 405, "method_not_allowed", "Use GET or PUT.")
	}
}

// titleQueueDiscard stops the batch: pending title updates are dropped without
// changing any file. A rewrite already running finishes safely.
func (s *Server) titleQueueDiscard(w http.ResponseWriter, r *http.Request) {
	if !decodeJSON(w, r, &struct{}{}) {
		return
	}
	out, err := s.organization.DiscardPendingTitles(r.Context())
	if err != nil {
		s.organizationError(w, err)
		return
	}
	send(w, 200, out)
}
