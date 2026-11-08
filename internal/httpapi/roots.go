package httpapi

import (
	"net/http"

	"github.com/faaaaq/ReelScarey/internal/organization"
)

// libraryRoots reads and updates read-only discovery roots. Organization
// destinations and naming templates remain in /api/v1/organization.
func (s *Server) libraryRoots(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		out, err := s.organization.GetRoots(r.Context())
		if err != nil {
			s.organizationError(w, err)
			return
		}
		send(w, 200, out)
	case http.MethodPut:
		var in organization.RootsUpdate
		if !decodeJSON(w, r, &in) {
			return
		}
		out, err := s.organization.UpdateRoots(r.Context(), in)
		if err != nil {
			s.organizationError(w, err)
			return
		}
		send(w, 200, out)
	default:
		w.Header().Set("Allow", "GET, PUT")
		fail(w, 405, "method_not_allowed", "Use GET or PUT.")
	}
}
