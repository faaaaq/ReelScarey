package httpapi

import (
	"context"
	"github.com/faaaaq/ReelScarey/internal/transfer"
	"net/http"
	"time"
)

func (s *Server) downloadClients(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	if r.Method == "GET" {
		items, e := s.transfer.DownloadClients(r.Context())
		if e != nil {
			s.transferError(w, e)
			return
		}
		defaults, e := s.transfer.ClientDefaults(r.Context())
		if e != nil {
			s.transferError(w, e)
			return
		}
		send(w, 200, map[string]any{"items": items, "defaults": defaults, "downloads_root": s.config.DownloadsDir})
		return
	}
	if r.Method != "POST" {
		w.Header().Set("Allow", "GET, POST")
		fail(w, 405, "method_not_allowed", "Use GET or POST.")
		return
	}
	s.saveDownloadClient(w, r, "")
}
func (s *Server) saveDownloadClient(w http.ResponseWriter, r *http.Request, key string) {
	var in transfer.DownloadClientUpdate
	if !decodeJSON(w, r, &in) {
		return
	}
	if (in.ID != "" && in.ID != key) || (key == "" && (in.ID != "" || in.Revision != 0)) {
		fail(w, 400, "invalid_settings", "Choose the matching download client.")
		return
	}
	in.ID = key
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(35 * time.Second))
	out, e := s.transfer.SaveDownloadClient(ctx, in)
	if e != nil {
		s.transferError(w, e)
		return
	}
	status := 200
	if key == "" {
		status = 201
	}
	send(w, status, out)
}
func (s *Server) downloadClient(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	switch r.Method {
	case "PUT":
		s.saveDownloadClient(w, r, r.PathValue("id"))
	case "DELETE":
		var in struct {
			Revision int  `json:"revision"`
			Reviewed bool `json:"reviewed"`
		}
		if !decodeJSON(w, r, &in) {
			return
		}
		if !in.Reviewed {
			fail(w, 400, "review_required", "Review this client's saved jobs and default assignment before removing it.")
			return
		}
		if e := s.transfer.DeleteDownloadClient(r.Context(), r.PathValue("id"), in.Revision); e != nil {
			s.transferError(w, e)
			return
		}
		send(w, 200, map[string]bool{"removed": true})
	default:
		w.Header().Set("Allow", "PUT, DELETE")
		fail(w, 405, "method_not_allowed", "Use PUT or DELETE.")
	}
}
func (s *Server) downloadClientDefaults(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	var in transfer.ClientDefaults
	if !decodeJSON(w, r, &in) {
		return
	}
	out, e := s.transfer.SaveClientDefaults(r.Context(), in)
	if e != nil {
		s.transferError(w, e)
		return
	}
	send(w, 200, out)
}
func (s *Server) testDownloadClient(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	if !decodeJSON(w, r, &struct{}{}) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(35 * time.Second))
	if e := s.transfer.TestDownloadClient(ctx, r.PathValue("id")); e != nil {
		s.transferError(w, e)
		return
	}
	send(w, 200, map[string]bool{"tested": true})
}
