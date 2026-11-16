package httpapi

import (
	"net/http"

	"github.com/faaaaq/ReelScarey/internal/transfer"
)

// Direct existing-file import: the owner points at one video inside a
// configured library root for the selected movie/episode. Browsing is
// read-only and bounded; importing keeps the reviewed confirmation and the
// shared verification worker.
func (s *Server) existingDirect(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.existingDirectBrowse(w, r)
	case http.MethodPost:
		s.existingDirectImport(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		fail(w, 405, "method_not_allowed", "Use GET or POST.")
	}
}

func (s *Server) existingDirectBrowse(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	kind := r.URL.Query().Get("kind")
	if kind != "movie" && kind != "series" {
		fail(w, 400, "invalid_scan", "Choose movies or TV shows.")
		return
	}
	collection := r.URL.Query().Get("collection")
	dir := r.URL.Query().Get("dir")
	if len(dir) > 2048 {
		fail(w, 400, "unsafe_path", "Choose a folder below a configured library root.")
		return
	}
	out, e := s.transfer.DirectImportBrowse(r.Context(), kind, collection, dir)
	if e != nil {
		s.transferError(w, e)
		return
	}
	send(w, 200, out)
}

func (s *Server) existingDirectImport(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	var in transfer.DirectImportRequest
	if !decodeBoundedJSON(w, r, &in, 16<<10) {
		return
	}
	if len(in.Episodes) > 20 || len(in.Relative) > 2048 {
		fail(w, 400, "invalid_mapping", "Review the title and episode match before importing this file.")
		return
	}
	out, e := s.transfer.QueueDirectImport(r.Context(), in)
	if e != nil {
		s.transferError(w, e)
		return
	}
	send(w, 202, out)
}
