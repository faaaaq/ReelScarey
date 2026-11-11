package httpapi

import (
	"context"
	"net/http"
	"time"
)

type diagnosticDownload struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Media     string `json:"media"`
	State     string `json:"state"`
	ErrorCode string `json:"error_code"`
	Error     string `json:"error"`
	UpdatedAt int64  `json:"updated_at"`
}

type diagnosticBlacklist struct {
	ID        int64  `json:"id"`
	Indexer   string `json:"indexer"`
	Title     string `json:"title"`
	Reason    string `json:"reason"`
	CreatedAt int64  `json:"created_at"`
}

type diagnosticEvent struct {
	ID         int64  `json:"id"`
	OccurredAt int64  `json:"occurred_at"`
	Kind       string `json:"kind"`
	Title      string `json:"title"`
	State      string `json:"state"`
}

type diagnosticIndexer struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	LastError string `json:"last_error"`
	LastTest  int64  `json:"last_test_at"`
}

type diagnosticRun struct {
	ID       string `json:"id"`
	State    string `json:"state"`
	Message  string `json:"message"`
	Started  int64  `json:"started_at"`
	Finished int64  `json:"finished_at"`
}

// diagnostics is a read-only aggregation of already recorded failures. It
// never retries, rescans or changes any catalog or queue state.
func (s *Server) diagnostics(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	downloads, err := s.diagnosticDownloads(ctx)
	if err != nil {
		s.internal(w, "diagnostics_downloads", err)
		return
	}
	blacklist, err := s.diagnosticBlacklist(ctx)
	if err != nil {
		s.internal(w, "diagnostics_blacklist", err)
		return
	}
	events, err := s.diagnosticEvents(ctx)
	if err != nil {
		s.internal(w, "diagnostics_events", err)
		return
	}
	indexers, err := s.diagnosticIndexers(ctx)
	if err != nil {
		s.internal(w, "diagnostics_indexers", err)
		return
	}
	runs, err := s.diagnosticRuns(ctx)
	if err != nil {
		s.internal(w, "diagnostics_runs", err)
		return
	}
	send(w, 200, map[string]any{
		"downloads": downloads, "blacklist": blacklist, "events": events,
		"indexers": indexers, "automation_runs": runs,
		"counts": map[string]int{
			"downloads": len(downloads), "blacklist": len(blacklist),
			"events": len(events), "indexers": len(indexers), "automation_runs": len(runs),
		},
	})
}

func (s *Server) diagnosticDownloads(ctx context.Context) ([]diagnosticDownload, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT d.id,d.title,d.state,d.error_code,d.error_message,d.updated_at,m.title
		FROM downloads d JOIN media_items m ON m.id=d.movie_id
		WHERE d.state IN ('blocked','failed') ORDER BY d.updated_at DESC,d.id LIMIT 50`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []diagnosticDownload{}
	for rows.Next() {
		var item diagnosticDownload
		if err := rows.Scan(&item.ID, &item.Title, &item.State, &item.ErrorCode, &item.Error, &item.UpdatedAt, &item.Media); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Server) diagnosticBlacklist(ctx context.Context) ([]diagnosticBlacklist, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,indexer,title,reason,created_at FROM release_blacklist ORDER BY created_at DESC,id DESC LIMIT 50`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []diagnosticBlacklist{}
	for rows.Next() {
		var item diagnosticBlacklist
		if err := rows.Scan(&item.ID, &item.Indexer, &item.Title, &item.Reason, &item.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Server) diagnosticEvents(ctx context.Context) ([]diagnosticEvent, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,occurred_at,kind,title,state FROM activity_events
		WHERE lower(state) LIKE '%fail%' OR lower(state) LIKE '%blocked%' OR lower(state) LIKE '%error%' OR lower(state) LIKE '%uncertain%'
		ORDER BY occurred_at DESC,id DESC LIMIT 50`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []diagnosticEvent{}
	for rows.Next() {
		var item diagnosticEvent
		if err := rows.Scan(&item.ID, &item.OccurredAt, &item.Kind, &item.Title, &item.State); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Server) diagnosticIndexers(ctx context.Context) ([]diagnosticIndexer, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,last_error,last_test_at FROM indexers WHERE last_error<>'' ORDER BY updated_at DESC,id LIMIT 50`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []diagnosticIndexer{}
	for rows.Next() {
		var item diagnosticIndexer
		if err := rows.Scan(&item.ID, &item.Name, &item.LastError, &item.LastTest); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Server) diagnosticRuns(ctx context.Context) ([]diagnosticRun, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,state,message,started_at,finished_at FROM automation_runs
		WHERE state NOT IN ('completed','skipped','cancelled') ORDER BY started_at DESC,id LIMIT 20`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []diagnosticRun{}
	for rows.Next() {
		var item diagnosticRun
		if err := rows.Scan(&item.ID, &item.State, &item.Message, &item.Started, &item.Finished); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
