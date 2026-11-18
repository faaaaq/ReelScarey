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
	// A review over a whole library reads every registered path before it can
	// answer; the server-wide write deadline would otherwise cut the response
	// off mid-flight and the browser would report a page timeout.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(5 * time.Minute))
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	plan, err := s.organization.PreviewTrackCleanup(ctx, in)
	if err != nil {
		s.organizationError(w, err)
		return
	}
	send(w, http.StatusCreated, plan)
}
