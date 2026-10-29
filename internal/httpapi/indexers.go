package httpapi

import (
	"github.com/faaaaq/ReelScarey/internal/transfer"
	"net/http"
)

func (s *Server) indexerStatus(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	out, e := s.transfer.Status(r.Context(), "indexers")
	if e != nil {
		s.transferError(w, e)
		return
	}
	send(w, 200, out)
}

func (s *Server) indexers(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, e := s.transfer.Indexers(r.Context())
		if e != nil {
			s.transferError(w, e)
			return
		}
		send(w, 200, map[string]any{"items": items})
	case http.MethodPost:
		var in transfer.ManagedIndexerUpdate
		if !decodeJSON(w, r, &in) {
			return
		}
		out, e := s.transfer.SaveIndexer(r.Context(), "", in)
		if e != nil {
			s.transferError(w, e)
			return
		}
		send(w, 201, out)
	default:
		w.Header().Set("Allow", "GET, POST")
		fail(w, 405, "method_not_allowed", "Use GET or POST.")
	}
}
func (s *Server) indexerItem(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	id := r.PathValue("id")
	if id != "legacy" && len(id) != 32 {
		fail(w, 400, "invalid_settings", "Choose a saved indexer.")
		return
	}
	switch r.Method {
	case http.MethodPut:
		var in transfer.ManagedIndexerUpdate
		if !decodeJSON(w, r, &in) {
			return
		}
		out, e := s.transfer.SaveIndexer(r.Context(), id, in)
		if e != nil {
			s.transferError(w, e)
			return
		}
		send(w, 200, out)
	case http.MethodDelete:
		var in struct {
			Revision int `json:"revision"`
		}
		if !decodeJSON(w, r, &in) {
			return
		}
		if e := s.transfer.DeleteIndexer(r.Context(), id, in.Revision); e != nil {
			s.transferError(w, e)
			return
		}
		send(w, 200, map[string]bool{"removed": true})
	default:
		w.Header().Set("Allow", "PUT, DELETE")
		fail(w, 405, "method_not_allowed", "Use PUT or DELETE.")
	}
}
func (s *Server) indexerTest(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	if !decodeJSON(w, r, &struct{}{}) {
		return
	}
	if e := s.transfer.TestIndexer(r.Context(), r.PathValue("id")); e != nil {
		s.transferError(w, e)
		return
	}
	send(w, 200, map[string]bool{"connected": true})
}
func (s *Server) indexerEnabled(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	var in struct {
		Revision int  `json:"revision"`
		Enabled  bool `json:"enabled"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	out, e := s.transfer.SetIndexerEnabled(r.Context(), r.PathValue("id"), in.Revision, in.Enabled)
	if e != nil {
		s.transferError(w, e)
		return
	}
	send(w, 200, out)
}
