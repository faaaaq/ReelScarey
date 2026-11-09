package httpapi

import (
	"io"
	"net/http"
	"strconv"

	"github.com/faaaaq/ReelScarey/internal/duplicates"
	"github.com/faaaaq/ReelScarey/internal/metrics"
)

func (s *Server) duplicateReview(w http.ResponseWriter, r *http.Request) {
	limit := 0
	if raw := r.URL.Query().Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 {
			fail(w, 400, "invalid_limit", "Choose a duplicate review limit between 1 and 500.")
			return
		}
		limit = value
	}
	report, err := duplicates.Review(r.Context(), s.db, limit)
	if err != nil {
		s.internal(w, "duplicate_review", err)
		return
	}
	send(w, 200, report)
}

// prometheusMetrics publishes a bounded snapshot of durable queue state. It is
// intentionally read-only: no counters live in memory across restarts, and the
// authenticated administrator endpoint exposes no paths, titles or secrets.
func (s *Server) prometheusMetrics(w http.ResponseWriter, r *http.Request) {
	registry := s.metricsRegistry
	instruments := s.instruments
	if registry == nil || instruments == nil {
		registry = metrics.NewRegistry()
		var err error
		if instruments, err = metrics.NewInstruments(registry); err != nil {
			s.internal(w, "metrics_registry", err)
			return
		}
	}
	rows, err := s.db.QueryContext(r.Context(), `SELECT state,COUNT(*) FROM downloads GROUP BY state`)
	if err != nil {
		s.internal(w, "metrics_downloads", err)
		return
	}
	queued := 0.0
	for rows.Next() {
		var state string
		var count int
		if err = rows.Scan(&state, &count); err != nil {
			rows.Close()
			s.internal(w, "metrics_downloads", err)
			return
		}
		instruments.SetJobState("download", state, float64(count))
		if state == "queued" {
			queued += float64(count)
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		s.internal(w, "metrics_downloads", err)
		return
	}
	instruments.SetQueueDepth("downloads", queued)

	musicRows, err := s.db.QueryContext(r.Context(), `SELECT state,COUNT(*) FROM music_downloads GROUP BY state`)
	if err != nil {
		s.internal(w, "metrics_music", err)
		return
	}
	musicQueued := 0.0
	for musicRows.Next() {
		var state string
		var count int
		if err = musicRows.Scan(&state, &count); err != nil {
			musicRows.Close()
			s.internal(w, "metrics_music", err)
			return
		}
		instruments.SetJobState("music", state, float64(count))
		if state == "queued" {
			musicQueued += float64(count)
		}
	}
	musicRows.Close()
	if err = musicRows.Err(); err != nil {
		s.internal(w, "metrics_music", err)
		return
	}
	instruments.SetQueueDepth("music", musicQueued)

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, registry.Render())
}
