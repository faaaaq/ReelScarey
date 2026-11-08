package httpapi

import (
	"net/http"
	"strconv"

	"github.com/faaaaq/ReelScarey/internal/library"
	"github.com/faaaaq/ReelScarey/internal/metadata"
)

type personCreditItem struct {
	metadata.PersonCredit
	InLibrary bool `json:"in_library"`
}

// personCredits returns a person's latest 24 cast roles for the cast-card
// quick-add list. It excludes the title currently being viewed.
func (s *Server) personCredits(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 || id > 2147483647 {
		fail(w, 400, "invalid_person_id", "Choose a valid TMDb person.")
		return
	}
	excludeKind := r.URL.Query().Get("exclude_kind")
	if excludeKind != "" && excludeKind != "movie" && excludeKind != "series" {
		fail(w, 400, "invalid_person_id", "The excluded title must be a movie or series.")
		return
	}
	var excludeID int64
	if raw := r.URL.Query().Get("exclude_tmdb_id"); raw != "" {
		if excludeID, err = strconv.ParseInt(raw, 10, 64); err != nil || excludeID < 0 || excludeID > 2147483647 {
			fail(w, 400, "invalid_person_id", "The excluded TMDb ID is invalid.")
			return
		}
	}
	credits, err := s.metadata.PersonCredits(r.Context(), id)
	if err != nil {
		s.providerError(w, err)
		return
	}
	items := make([]personCreditItem, 0, len(credits))
	for _, credit := range credits {
		if excludeID > 0 && credit.Kind == excludeKind && credit.TMDbID == excludeID {
			continue
		}
		item := personCreditItem{PersonCredit: credit}
		var existing library.Item
		if credit.Kind == "movie" {
			existing, err = s.library.Movie(r.Context(), credit.TMDbID)
		} else {
			existing, err = s.library.Series(r.Context(), credit.TMDbID)
		}
		item.InLibrary = err == nil && existing.ID > 0
		items = append(items, item)
	}
	send(w, 200, map[string]any{"items": items})
}
