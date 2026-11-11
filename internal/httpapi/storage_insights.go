package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"time"

	"github.com/faaaaq/ReelScarey/internal/duplicates"
	"github.com/faaaaq/ReelScarey/internal/quality"
)

type storageInsightFile struct {
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	Kind        string `json:"kind"`
	Path        string `json:"relative_path"`
	Size        int64  `json:"size_bytes"`
	Quality     int    `json:"quality"`
	Cutoff      int    `json:"cutoff"`
	HasAnalysis bool   `json:"has_analysis"`
}

type storageInsights struct {
	Summary     map[string]any       `json:"summary"`
	BelowCutoff []storageInsightFile `json:"below_cutoff"`
	Largest     []storageInsightFile `json:"largest"`
	Unresolved  int                  `json:"unresolved"`
}

// storageInsightsHandler is read-only. It never scans disk, runs FFprobe,
// deletes files or changes a profile.
func (s *Server) storageInsightsHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	profileCutoff := map[int64]int{}
	profileRows, err := s.db.QueryContext(ctx, `SELECT id,cutoff FROM quality_profiles`)
	if err != nil {
		s.internal(w, "storage_insights_profiles", err)
		return
	}
	for profileRows.Next() {
		var id int64
		var cutoff int
		if err := profileRows.Scan(&id, &cutoff); err != nil {
			profileRows.Close()
			s.internal(w, "storage_insights_profiles", err)
			return
		}
		profileCutoff[id] = cutoff
	}
	if err := profileRows.Err(); err != nil {
		profileRows.Close()
		s.internal(w, "storage_insights_profiles", err)
		return
	}
	profileRows.Close()
	var scheduleDefault int64
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(default_profile_id,0) FROM search_schedule WHERE id=1`).Scan(&scheduleDefault); err != nil {
		scheduleDefault = 0
	}
	rows, err := s.db.QueryContext(ctx, `SELECT f.id,f.relative_path,f.size_bytes,f.analysis_json,
		COALESCE(f.movie_id,f.series_id),m.title,m.kind,
		COALESCE((SELECT a.profile_id FROM item_automation a WHERE a.media_id=COALESCE(f.movie_id,f.series_id)),0)
		FROM media_files f JOIN media_items m ON m.id=COALESCE(f.movie_id,f.series_id)`)
	if err != nil {
		s.internal(w, "storage_insights", err)
		return
	}
	defer rows.Close()
	out := storageInsights{BelowCutoff: []storageInsightFile{}, Largest: []storageInsightFile{}, Summary: map[string]any{}}
	var totalFiles int
	var totalBytes int64
	for rows.Next() {
		var fileID, mediaID, size int64
		var path, analysis, title, kind string
		var assigned int64
		if err := rows.Scan(&fileID, &path, &size, &analysis, &mediaID, &title, &kind, &assigned); err != nil {
			s.internal(w, "storage_insights", err)
			return
		}
		totalFiles++
		totalBytes += size
		profile := assigned
		if profile == 0 {
			profile = scheduleDefault
		}
		cutoff := profileCutoff[profile]
		item := storageInsightFile{ID: fileID, Title: title, Kind: kind, Path: path, Size: size, Cutoff: cutoff}
		if len(analysis) > 1 {
			var probe struct {
				Width  int `json:"width"`
				Height int `json:"height"`
			}
			if json.Unmarshal([]byte(analysis), &probe) == nil && probe.Width > 0 && probe.Height > 0 {
				item.HasAnalysis = true
				item.Quality = quality.VerifiedQuality(probe.Width, probe.Height)
			}
		}
		if item.HasAnalysis && cutoff > 0 && item.Quality > 0 && item.Quality < cutoff {
			out.BelowCutoff = append(out.BelowCutoff, item)
		}
		out.Largest = append(out.Largest, item)
	}
	if err := rows.Err(); err != nil {
		s.internal(w, "storage_insights", err)
		return
	}
	rows.Close()
	// Keep the response bounded and deterministic.
	sort.SliceStable(out.Largest, func(i, j int) bool {
		if out.Largest[i].Size != out.Largest[j].Size {
			return out.Largest[i].Size > out.Largest[j].Size
		}
		return out.Largest[i].ID < out.Largest[j].ID
	})
	if len(out.Largest) > 10 {
		out.Largest = out.Largest[:10]
	}
	if len(out.BelowCutoff) > 100 {
		out.BelowCutoff = out.BelowCutoff[:100]
	}
	var unresolved int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM library_scan_files WHERE state NOT IN ('registered','skipped')`).Scan(&unresolved); err != nil {
		unresolved = 0
	}
	out.Unresolved = unresolved
	report, _ := duplicates.Review(ctx, s.db, 500)
	out.Summary = map[string]any{
		"registered_files": totalFiles, "registered_bytes": totalBytes,
		"duplicate_groups": report.Total, "below_cutoff": len(out.BelowCutoff),
		"unresolved": unresolved, "largest_returned": len(out.Largest),
	}
	send(w, 200, out)
}
