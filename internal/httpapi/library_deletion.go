package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/faaaaq/ReelScarey/internal/organization"
)

func (s *Server) libraryDeletion(w http.ResponseWriter, r *http.Request) {
	var in organization.DeletionRequest
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Mode == "trash" {
		fail(w, http.StatusConflict, "reload_required", "Refresh the page before reviewing deletion. Choose permanent deletion or keep files on disk.")
		return
	}
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(5 * time.Minute))
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	plan, err := s.organization.PreviewDeletion(ctx, in)
	if err != nil {
		s.organizationError(w, err)
		return
	}
	send(w, http.StatusCreated, plan)
}

func (s *Server) trackCleanup(w http.ResponseWriter, r *http.Request) {
	var in organization.TrackCleanupRequest
	if !decodeJSON(w, r, &in) {
		return
	}
	plan, err := s.organization.PreviewTrackCleanup(r.Context(), in)
	if err != nil {
		s.organizationError(w, err)
		return
	}
	send(w, http.StatusCreated, plan)
}
