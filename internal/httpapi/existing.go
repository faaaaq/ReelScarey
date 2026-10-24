package httpapi

import (
	"net/http"
	"path/filepath"

	"github.com/faaaaq/ReelScarey/internal/transfer"
)

func (s *Server) existingCollection(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, e := s.transfer.LibraryScans(r.Context())
		if e != nil {
			s.transferError(w, e)
			return
		}
		prefs, e := s.settings.Get(r.Context())
		if e != nil {
			s.internal(w, "library_scan_settings", e)
			return
		}
		send(w, 200, map[string]any{"items": items, "media_root": s.config.MediaDir, "movie_root": filepath.Join(s.config.MediaDir, prefs.MovieFolder), "series_root": filepath.Join(s.config.MediaDir, prefs.SeriesFolder)})
	case http.MethodPost:
		var in struct {
			Kind string `json:"kind"`
		}
		if !decodeJSON(w, r, &in) {
			return
		}
		out, e := s.transfer.StartLibraryScan(r.Context(), in.Kind)
		if e != nil {
			s.transferError(w, e)
			return
		}
		send(w, 202, out)
	default:
		w.Header().Set("Allow", "GET, POST")
		fail(w, 405, "method_not_allowed", "Use GET or POST.")
	}
}
func (s *Server) existingScan(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	id := r.PathValue("id")
	if len(id) != 32 {
		fail(w, 400, "invalid_scan", "Choose a saved library scan.")
		return
	}
	switch r.Method {
	case http.MethodGet:
		page, ok := pageNumber(r, 100000)
		if !ok {
			fail(w, 400, "invalid_page", "Choose a valid page.")
			return
		}
		out, e := s.transfer.DiscoveredFiles(r.Context(), id, page, r.URL.Query().Get("state"))
		if e != nil {
			s.transferError(w, e)
			return
		}
		send(w, 200, out)
	case http.MethodDelete:
		if !decodeJSON(w, r, &struct{}{}) {
			return
		}
		if e := s.transfer.RemoveLibraryScan(r.Context(), id); e != nil {
			s.transferError(w, e)
			return
		}
		send(w, 200, map[string]bool{"removed": true})
	default:
		w.Header().Set("Allow", "GET, DELETE")
		fail(w, 405, "method_not_allowed", "Use GET or DELETE.")
	}
}
func (s *Server) existingCancel(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) || !decodeJSON(w, r, &struct{}{}) {
		return
	}
	if e := s.transfer.CancelLibraryScan(r.Context(), r.PathValue("id")); e != nil {
		s.transferError(w, e)
		return
	}
	send(w, 200, map[string]bool{"cancelled": true})
}
func (s *Server) existingRegister(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	var in transfer.ExistingRegistration
	if !decodeJSON(w, r, &in) {
		return
	}
	if e := s.transfer.RegisterDiscovered(r.Context(), r.PathValue("id"), in); e != nil {
		s.transferError(w, e)
		return
	}
	send(w, 202, map[string]bool{"queued": true})
}
func (s *Server) existingSkip(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) || !decodeJSON(w, r, &struct{}{}) {
		return
	}
	if e := s.transfer.SkipDiscovered(r.Context(), r.PathValue("id")); e != nil {
		s.transferError(w, e)
		return
	}
	send(w, 200, map[string]bool{"skipped": true})
}
func (s *Server) existingCatalog(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	items, e := s.transfer.ImportCatalog(r.Context(), r.URL.Query().Get("kind"), r.URL.Query().Get("query"))
	if e != nil {
		s.transferError(w, e)
		return
	}
	send(w, 200, map[string]any{"items": items})
}
