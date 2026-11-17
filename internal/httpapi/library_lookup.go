package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"

	"github.com/faaaaq/ReelScarey/internal/library"
)

// libraryLookup resolves the saved catalog title behind a TMDb id so
// interfaces can deep-link to the title's own page instead of a library
// list. It is a read-only catalog lookup; it never adds anything.
func (s *Server) libraryLookup(w http.ResponseWriter, r *http.Request) {
	tmdbID, err := strconv.ParseInt(r.PathValue("tmdb"), 10, 64)
	if err != nil || tmdbID < 1 {
		fail(w, 400, "invalid_id", "Choose a TMDb title.")
		return
	}
	kind := r.URL.Query().Get("kind")
	if kind != "movie" && kind != "series" {
		fail(w, 400, "invalid_id", "Choose movies or TV shows.")
		return
	}
	if s.library == nil {
		fail(w, 503, "library_unavailable", "The library is unavailable.")
		return
	}
	var (
		item library.Item
		e    error
	)
	if kind == "movie" {
		item, e = s.library.Movie(r.Context(), tmdbID)
	} else {
		item, e = s.library.Series(r.Context(), tmdbID)
	}
	if errors.Is(e, sql.ErrNoRows) {
		fail(w, 404, "not_found", "This title is not in your library.")
		return
	}
	if e != nil {
		s.itemError(w, e)
		return
	}
	send(w, 200, map[string]any{"id": item.ID, "kind": item.Kind})
}
