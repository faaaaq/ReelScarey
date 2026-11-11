package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/faaaaq/ReelScarey/internal/automation"
	"github.com/faaaaq/ReelScarey/internal/library"
	"github.com/faaaaq/ReelScarey/internal/quality"
)

// applyLibraryDefaultProfile assigns the library's configured profile to a
// newly added title. It never overrides an explicit per-title assignment and
// never fails the add if the optional assignment cannot be written.
func (s *Server) applyLibraryDefaultProfile(ctx context.Context, mediaID int64, libraryKey string) {
	if s.automation == nil || mediaID < 1 {
		return
	}
	profileID, err := s.library.DefaultProfileID(ctx, libraryKey)
	if err != nil || profileID < 1 {
		return
	}
	if err := s.automation.Assign(ctx, mediaID, automation.AssignmentUpdate{ProfileID: &profileID, Revision: 0}); err != nil {
		s.logger.Warn("library default profile was not assigned", "media", mediaID, "library", libraryKey, "error", err)
	}
}

func (s *Server) libraryProfileDefaults(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		items, err := s.library.ProfileDefaults(r.Context())
		if err != nil {
			s.internal(w, "library_profile_defaults", err)
			return
		}
		profiles, err := quality.New(s.db).List(r.Context())
		if err != nil {
			s.internal(w, "library_profile_defaults_profiles", err)
			return
		}
		send(w, 200, map[string]any{"items": items, "profiles": profiles})
	case http.MethodPut:
		var body struct {
			Items []library.ProfileDefault `json:"items"`
		}
		if !decodeJSON(w, r, &body) {
			return
		}
		items, err := s.library.SaveProfileDefaults(r.Context(), body.Items)
		if err != nil {
			if errors.Is(err, library.ErrProfileDefaultInput) {
				fail(w, 400, "validation_error", err.Error())
				return
			}
			s.internal(w, "library_profile_defaults_save", err)
			return
		}
		send(w, 200, map[string]any{"items": items})
	default:
		w.Header().Set("Allow", "GET, PUT")
		fail(w, 405, "method_not_allowed", "Use GET or PUT.")
	}
}
