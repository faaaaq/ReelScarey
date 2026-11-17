package httpapi

import (
	"net/http"

	"github.com/faaaaq/ReelScarey/internal/metadata"
)

func (s *Server) tvdbSettings(w http.ResponseWriter, r *http.Request) {
	if s.metadata == nil {
		fail(w, http.StatusServiceUnavailable, "tvdb_unavailable", "TV metadata is unavailable.")
		return
	}
	ctx := r.Context()
	switch r.Method {
	case http.MethodGet:
	case http.MethodPut:
		var in metadata.TVDBCredentials
		if !decodeJSON(w, r, &in) {
			return
		}
		if err := s.metadata.ConfigureTVDB(ctx, in); err != nil {
			s.providerError(w, err)
			return
		}
	case http.MethodDelete:
		if err := s.metadata.RemoveTVDB(ctx); err != nil {
			s.providerError(w, err)
			return
		}
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		fail(w, 405, "method_not_allowed", "Use GET, PUT or DELETE.")
		return
	}
	connected, err := s.metadata.TVDBConnected(ctx)
	if err != nil {
		s.providerError(w, err)
		return
	}
	send(w, 200, map[string]bool{"configured": connected})
}
