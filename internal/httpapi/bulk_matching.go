package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"sort"
	"time"

	"github.com/faaaaq/ReelScarey/internal/metadata"
	"github.com/faaaaq/ReelScarey/internal/transfer"
)

func (s *Server) bulkMatchSuggestions(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	var in struct {
		IDs       []string `json:"file_ids"`
		Providers bool     `json:"include_provider"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(95 * time.Second))
	scan, files, e := s.transfer.MatchFiles(ctx, r.PathValue("id"), in.IDs)
	if e != nil {
		s.transferError(w, e)
		return
	}
	out := []transfer.FileSuggestions{}
	cache := map[string][]metadata.Movie{}
	lookups := 0
	for _, file := range files {
		v, e := s.transfer.LocalMatchSuggestions(ctx, scan, file)
		if e != nil {
			s.transferError(w, e)
			return
		}
		if in.Providers && s.metadata != nil {
			remote, known := cache[file.Hint]
			if !known && lookups < 5 {
				lookups++
				if scan.Kind == "movie" {
					page, err := s.metadata.Search(ctx, file.Hint, 1)
					e = err
					remote = page.Items
				} else {
					page, err := s.metadata.SearchSeries(ctx, file.Hint, 1)
					e = err
					for _, item := range page.Items {
						remote = append(remote, item.Movie)
					}
				}
				if e != nil {
					v.Message += " Provider lookup could not finish; saved catalog suggestions remain available."
				}
				cache[file.Hint] = remote
			} else if !known {
				v.Message += " This batch reached its five-query provider limit; use smaller groups for additional remote suggestions."
			}
			seen := map[int64]bool{}
			for _, c := range v.Candidates {
				seen[c.TMDbID] = true
			}
			for _, movie := range remote {
				if seen[movie.TMDbID] || len(v.Candidates) >= 10 {
					continue
				}
				score := transfer.MatchScore(file, movie.Title, movie.Year)
				if score <= 0 {
					continue
				}
				var mediaID int64
				if err := s.db.QueryRowContext(ctx, `SELECT id FROM media_items WHERE kind=? AND tmdb_id=?`, scan.Kind, movie.TMDbID).Scan(&mediaID); err != nil && !errors.Is(err, sql.ErrNoRows) {
					fail(w, 500, "internal_error", "Provider suggestion lookup failed.")
					return
				}
				v.Candidates = append(v.Candidates, transfer.MatchCandidate{MediaID: mediaID, TMDbID: movie.TMDbID, Title: movie.Title, Year: movie.Year, Source: "tmdb", Score: score, Reasons: []string{"Provider search suggestion; verify title, year and canonical identity before adding."}, Episodes: []int64{}})
				seen[movie.TMDbID] = true
			}
		}
		sort.SliceStable(v.Candidates, func(i, j int) bool { return v.Candidates[i].Score > v.Candidates[j].Score })
		out = append(out, v)
	}
	send(w, 200, map[string]any{"items": out, "kind": scan.Kind, "provider_queries": lookups})
}
func (s *Server) bulkMatchReview(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	var in struct {
		Items []transfer.BulkMatchChoice `json:"items"`
	}
	if !decodeBoundedJSON(w, r, &in, 128<<10) {
		return
	}
	v, e := s.transfer.ReviewMatches(r.Context(), r.PathValue("id"), in.Items)
	if e != nil {
		s.transferError(w, e)
		return
	}
	send(w, 200, v)
}
func (s *Server) bulkMatchApply(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	var in struct {
		ID       string `json:"review_id"`
		Reviewed bool   `json:"reviewed"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if e := s.transfer.ApplyMatches(r.Context(), r.PathValue("id"), in.ID, in.Reviewed); e != nil {
		s.transferError(w, e)
		return
	}
	send(w, 202, map[string]bool{"queued": true})
}
