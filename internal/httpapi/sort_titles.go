package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/faaaaq/ReelScarey/internal/nfoexport"
	"github.com/faaaaq/ReelScarey/internal/organization"
)

// clearEpisodeSortTitles processes one bounded page. Clients repeat with the
// returned cursor so libraries of any size do not monopolize one HTTP request.
func (s *Server) clearEpisodeSortTitles(w http.ResponseWriter, r *http.Request) {
	var input struct {
		AfterID int64 `json:"after_id"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if input.AfterID < 0 {
		fail(w, http.StatusBadRequest, "invalid_sort_title_cursor", "The cleanup position must not be negative. Restart the batch from the beginning.")
		return
	}
	if s.nfoExport == nil {
		fail(w, http.StatusServiceUnavailable, "sort_title_cleanup_unavailable", "Episode sort-title cleanup is not configured.")
		return
	}
	// Keep the response alive while a bounded set of sidecars is verified and
	// atomically replaced. The deadline also stops a stalled filesystem.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(2 * time.Minute))
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	report, err := s.nfoExport.ClearSortTitles(ctx, input.AfterID)
	if err != nil {
		switch {
		case errors.Is(err, nfoexport.ErrSortTitleCursor):
			fail(w, http.StatusBadRequest, "invalid_sort_title_cursor", "The cleanup position is invalid. Restart the batch from the beginning.")
		case errors.Is(err, nfoexport.ErrConfig), errors.Is(err, nfoexport.ErrMediaRoot):
			fail(w, http.StatusServiceUnavailable, "sort_title_cleanup_unavailable", "The media folder is unavailable. Check the mount and try again.")
		case errors.Is(err, context.DeadlineExceeded):
			fail(w, http.StatusGatewayTimeout, "sort_title_cleanup_timeout", "This batch took too long. Resume from the last returned cleanup position.")
		default:
			var typed *organization.Error
			if errors.As(err, &typed) {
				s.organizationError(w, err)
			} else {
				s.internal(w, "sort_title_cleanup", err)
			}
		}
		return
	}
	send(w, http.StatusOK, report)
}
