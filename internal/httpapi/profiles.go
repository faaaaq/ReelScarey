package httpapi

import (
	"context"
	"github.com/faaaaq/ReelScarey/internal/automation"
	"net/http"
	"time"
)

func (s *Server) reviewProfiles(w http.ResponseWriter, r *http.Request) {
	if !s.automationReady(w) {
		return
	}
	var in struct {
		Kind   string                   `json:"kind"`
		IDs    []int64                  `json:"ids"`
		Target automation.ProfileChoice `json:"target"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	out, e := s.automation.ReviewProfiles(ctx, in.Kind, in.IDs, in.Target)
	if e != nil {
		s.automationError(w, e)
		return
	}
	send(w, 200, out)
}
func (s *Server) applyProfiles(w http.ResponseWriter, r *http.Request) {
	if !s.automationReady(w) {
		return
	}
	var in automation.ProfileApply
	if !decodeJSON(w, r, &in) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	out, e := s.automation.ApplyProfiles(ctx, in)
	if e != nil {
		s.automationError(w, e)
		return
	}
	send(w, 200, out)
}
