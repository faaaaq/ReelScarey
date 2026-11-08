package httpapi

import (
	"errors"
	"net/http"

	"github.com/faaaaq/ReelScarey/internal/automation"
)

func (s *Server) metadataRefreshSettings(w http.ResponseWriter, r *http.Request) {
	if s.metadataRefresh == nil {
		fail(w, 503, "metadata_unavailable", "Metadata refresh is unavailable.")
		return
	}
	switch r.Method {
	case "GET":
		v, e := s.metadataRefresh.Get(r.Context())
		if e != nil {
			s.internal(w, "metadata_refresh", e)
			return
		}
		send(w, 200, v)
	case "PUT":
		var p struct {
			Enabled       bool `json:"enabled"`
			Interval      int  `json:"interval_hours"`
			Batch         int  `json:"batch_size"`
			OnlyMonitored bool `json:"only_monitored"`
			Revision      int  `json:"revision"`
		}
		if !decodeJSON(w, r, &p) {
			return
		}
		v, e := s.metadataRefresh.Save(r.Context(), automation.MetadataSchedule{Enabled: p.Enabled, IntervalHours: p.Interval, Batch: p.Batch, OnlyMonitored: p.OnlyMonitored, Revision: p.Revision})
		if errors.Is(e, automation.ErrLease) {
			fail(w, 409, "revision_conflict", "Refresh settings changed. Reload before saving.")
			return
		}
		if e != nil {
			fail(w, 400, "invalid_schedule", "Choose 1–720 hours and 1–25 titles per batch.")
			return
		}
		send(w, 200, v)
	default:
		w.Header().Set("Allow", "GET, PUT")
		fail(w, 405, "method_not_allowed", "Use GET or PUT.")
	}
}
func (s *Server) metadataRefreshRun(w http.ResponseWriter, r *http.Request) {
	if s.metadataRefresh == nil {
		fail(w, 503, "metadata_unavailable", "Metadata refresh is unavailable.")
		return
	}
	if !decodeJSON(w, r, &struct{}{}) {
		return
	}
	if e := s.metadataRefresh.Request(r.Context()); e != nil {
		if errors.Is(e, automation.ErrLease) {
			fail(w, 409, "refresh_busy", "A metadata refresh is already requested or running.")
			return
		}
		s.internal(w, "metadata_refresh", e)
		return
	}
	send(w, 202, map[string]bool{"queued": true})
}

// refreshLibrary queues every title in one library for a full metadata pass.
// The durable worker runs it even while the scheduled refresh is paused.
func (s *Server) refreshLibrary(w http.ResponseWriter, r *http.Request) {
	if s.metadataRefresh == nil {
		fail(w, 503, "metadata_unavailable", "Metadata refresh is unavailable.")
		return
	}
	var body struct {
		Kind string `json:"kind"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.Kind != "movie" && body.Kind != "series" && body.Kind != "anime" {
		fail(w, 400, "invalid_kind", "Choose movie, series or anime.")
		return
	}
	queued, e := s.metadataRefresh.EnqueueLibrary(r.Context(), body.Kind)
	if e != nil {
		s.internal(w, "metadata_refresh_library", e)
		return
	}
	send(w, 202, map[string]any{"queued": queued})
}
