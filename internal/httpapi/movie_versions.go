package httpapi

import (
	"github.com/faaaaq/ReelScarey/internal/quality"
	"net/http"
	"strconv"
)

func (s *Server) movieVersions(w http.ResponseWriter, r *http.Request) {
	if !s.automationReady(w) {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		fail(w, 400, "invalid_id", "Choose a movie in your library.")
		return
	}
	if r.Method == http.MethodPut {
		var in quality.MovieVersionPolicy
		if !decodeJSON(w, r, &in) {
			return
		}
		if err = s.automation.SetMovieVersions(r.Context(), id, in); err != nil {
			s.automationError(w, err)
			return
		}
	} else if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET, PUT")
		fail(w, 405, "method_not_allowed", "Use GET or PUT.")
		return
	}
	out, err := s.automation.MovieVersions(r.Context(), id)
	if err != nil {
		s.automationError(w, err)
		return
	}
	send(w, 200, out)
}
