package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/faaaaq/ReelScarey/internal/transfer"
)

// The Usenet routes manage only the application-supervised SABnzbd daemon.
// External SABnzbd/NZBGet connections keep their job-level controls and are
// never reached by these handlers.
func (s *Server) usenetOverview(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		out, e := s.transfer.UsenetOverview(r.Context())
		if e != nil {
			s.transferError(w, e)
			return
		}
		send(w, 200, out)
	case http.MethodPut:
		var in transfer.UsenetBundledUpdate
		if !decodeJSON(w, r, &in) {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
		defer cancel()
		_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(125 * time.Second))
		out, e := s.transfer.UpdateBundledUsenet(ctx, in)
		if e != nil {
			s.transferError(w, e)
			return
		}
		send(w, 200, out)
	default:
		w.Header().Set("Allow", "GET, PUT")
		fail(w, 405, "method_not_allowed", "Use GET or PUT.")
	}
}

func (s *Server) usenetNews(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		out, e := s.transfer.NewsServer(r.Context())
		if e != nil {
			s.transferError(w, e)
			return
		}
		send(w, 200, out)
	case http.MethodPut:
		var in transfer.NewsServerUpdate
		if !decodeJSON(w, r, &in) {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(35 * time.Second))
		out, e := s.transfer.SaveNewsServer(ctx, in)
		if e != nil {
			s.transferError(w, e)
			return
		}
		send(w, 200, out)
	default:
		w.Header().Set("Allow", "GET, PUT")
		fail(w, 405, "method_not_allowed", "Use GET or PUT.")
	}
}

// usenetNewsTest checks saved settings or the submitted host/port. It is a TCP
// reachability check only and never authenticates the provider account.
func (s *Server) usenetNewsTest(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	var in transfer.NewsServerUpdate
	if !decodeJSON(w, r, &in) {
		return
	}
	host, port := in.Host, in.Port
	if host == "" {
		saved, e := s.transfer.NewsServer(r.Context())
		if e != nil {
			s.transferError(w, e)
			return
		}
		if !saved.Configured {
			fail(w, 400, "client_not_configured", "Save a news server before testing it.")
			return
		}
		host, port = saved.Host, saved.Port
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(15 * time.Second))
	if e := transfer.TestNewsServer(ctx, host, port); e != nil {
		s.transferError(w, e)
		return
	}
	send(w, 200, map[string]bool{"reachable": true})
}

func (s *Server) usenetQueue(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		out, e := s.transfer.UsenetQueue(r.Context())
		if e != nil {
			s.transferError(w, e)
			return
		}
		send(w, 200, out)
	case http.MethodPost:
		data, ok := readNZBBody(w, r)
		if !ok {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
		defer cancel()
		_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(50 * time.Second))
		id, e := s.transfer.AddManualNZB(ctx, data)
		if e != nil {
			s.transferError(w, e)
			return
		}
		send(w, 202, map[string]any{"added": true, "id": id})
	default:
		w.Header().Set("Allow", "GET, POST")
		fail(w, 405, "method_not_allowed", "Use GET or POST.")
	}
}

func (s *Server) usenetQueueActions(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	var in struct {
		IDs         []string `json:"ids"`
		Action      string   `json:"action"`
		DeleteFiles bool     `json:"delete_files"`
		Reviewed    bool     `json:"reviewed"`
	}
	if !decodeTorrentJSON(w, r, &in) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(50 * time.Second))
	if e := s.transfer.UsenetQueueAction(ctx, transfer.UsenetQueueAction{IDs: in.IDs, Action: in.Action, DeleteFiles: in.DeleteFiles, Reviewed: in.Reviewed}); e != nil {
		s.transferError(w, e)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) usenetSpeedLimit(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	var in transfer.UsenetSpeedLimitUpdate
	if !decodeJSON(w, r, &in) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if e := s.transfer.SetUsenetSpeedLimit(ctx, in); e != nil {
		s.transferError(w, e)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// readNZBBody reads only an explicit bounded NZB upload. The API never fetches
// a caller-supplied URL.
func readNZBBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	if r.Header.Get("Content-Type") != "application/x-nzb" {
		fail(w, 415, "unsupported_media_type", "Upload an NZB with Content-Type application/x-nzb.")
		return nil, false
	}
	body := http.MaxBytesReader(w, r.Body, 4<<20)
	data, e := io.ReadAll(body)
	if e != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(e, &tooLarge) {
			fail(w, 413, "payload_too_large", "Upload an NZB of at most 4 MiB.")
		} else {
			fail(w, 400, "invalid_nzb", "The uploaded file could not be read.")
		}
		return nil, false
	}
	if len(data) == 0 {
		fail(w, 400, "invalid_nzb", "Choose an .nzb file to upload.")
		return nil, false
	}
	return data, true
}
