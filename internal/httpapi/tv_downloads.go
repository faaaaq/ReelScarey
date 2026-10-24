package httpapi

import (
	"net/http"
	"strconv"
)

func (s *Server) searchTVReleases(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	id, ok := tvID(w, r)
	if !ok {
		return
	}
	season, ok := tvNumber(w, r, "number", 0, 10000)
	if !ok {
		return
	}
	episode := 0
	if raw := r.URL.Query().Get("episode"); raw != "" {
		n, e := strconv.Atoi(raw)
		if e != nil || n < 1 || n > 100000 {
			fail(w, 400, "invalid_episode", "Choose a valid episode number.")
			return
		}
		episode = n
	}
	out, e := s.transfer.SearchTV(r.Context(), id, season, episode, r.URL.Query().Get("query"))
	if e != nil {
		s.transferError(w, e)
		return
	}
	send(w, 200, map[string]any{"items": out, "warnings": s.transfer.IndexerWarnings(r.Context())})
}
