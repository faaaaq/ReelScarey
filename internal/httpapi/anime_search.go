package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"github.com/faaaaq/ReelScarey/internal/library"
	"net/http"
	"time"
)

func (s *Server) animeSearchSettings(w http.ResponseWriter, r *http.Request) {
	id, ok := tvID(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	var out library.AnimeSearchSettings
	var e error
	switch r.Method {
	case http.MethodGet:
		out, e = s.library.AnimeSearch(ctx, id)
	case http.MethodPut:
		var in library.AnimeSearchUpdate
		if !decodeJSON(w, r, &in) {
			return
		}
		out, e = s.library.SaveAnimeSearch(ctx, id, in)
	default:
		w.Header().Set("Allow", "GET, PUT")
		fail(w, 405, "method_not_allowed", "Use GET or PUT.")
		return
	}
	if errors.Is(e, library.ErrAnimeSearchInput) {
		fail(w, 400, "invalid_anime_search", e.Error())
		return
	}
	if errors.Is(e, library.ErrAnimeSearchRevision) {
		fail(w, 409, "revision_conflict", e.Error())
		return
	}
	if errors.Is(e, library.ErrAnimeSearchUnavailable) {
		fail(w, 409, "anime_search_unavailable", e.Error())
		return
	}
	if errors.Is(e, sql.ErrNoRows) {
		fail(w, 404, "not_found", "Choose a saved Anime show.")
		return
	}
	if e != nil {
		s.internal(w, "anime_search", e)
		return
	}
	send(w, 200, out)
}
