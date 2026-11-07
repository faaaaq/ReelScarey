package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"github.com/faaaaq/ReelScarey/internal/library"
	"net/http"
	"time"
)

// A 5000-title review plus its revision fields can exceed the ordinary 16 KiB
// JSON limit while staying comfortably below this bound.
const monitoringJSONLimit = 1 << 20

func (s *Server) monitoringError(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, library.ErrMonitoringInput):
		fail(w, 400, "invalid_monitoring", e.Error())
	case errors.Is(e, library.ErrMonitoringConflict):
		fail(w, 409, "monitoring_conflict", e.Error())
	case errors.Is(e, sql.ErrNoRows):
		fail(w, 404, "title_not_found", "A selected title no longer exists in this library. Review your selection.")
	default:
		s.internal(w, "bulk_monitoring", e)
	}
}
func (s *Server) reviewMonitoring(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Kind string  `json:"kind"`
		IDs  []int64 `json:"ids"`
	}
	if !decodeJSONLimit(w, r, &body, monitoringJSONLimit) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	items, e := s.library.ReviewMonitoring(ctx, body.Kind, body.IDs)
	if e != nil {
		s.monitoringError(w, e)
		return
	}
	send(w, 200, map[string]any{"kind": body.Kind, "items": items})
}
func (s *Server) applyMonitoring(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Kind      string                          `json:"kind"`
		Monitored *bool                           `json:"monitored"`
		Items     []library.MonitoringExpectation `json:"items"`
	}
	if !decodeJSONLimit(w, r, &body, monitoringJSONLimit) {
		return
	}
	if body.Monitored == nil {
		s.monitoringError(w, library.ErrMonitoringInput)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	out, e := s.library.ApplyMonitoring(ctx, body.Kind, body.Items, *body.Monitored)
	if e != nil {
		s.monitoringError(w, e)
		return
	}
	send(w, 200, out)
}
