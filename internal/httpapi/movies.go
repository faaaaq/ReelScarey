package httpapi

import (
	"database/sql"
	"errors"
	"github.com/faaaaq/ReelScarey/internal/library"
	"github.com/faaaaq/ReelScarey/internal/metadata"
	"net/http"
	"strconv"
	"strings"
)

func pageNumber(r *http.Request, maximum int) (int, bool) {
	value := r.URL.Query().Get("page")
	if value == "" {
		return 1, true
	}
	page, err := strconv.Atoi(value)
	return page, err == nil && page >= 1 && page <= maximum
}
func (s *Server) providerError(w http.ResponseWriter, err error) {
	var provider *metadata.Error
	if errors.As(err, &provider) {
		status := 502
		switch provider.Code {
		case "tmdb_not_configured":
			status = 409
		case "movie_not_found", "series_not_found", "anime_not_found":
			status = 404
		case "tmdb_invalid_token", "invalid_anime_id", "invalid_anime_query", "invalid_anime_order":
			status = 400
		case "tmdb_rate_limited", "anilist_rate_limited":
			status = 429
		}
		if provider.RetryAfter > 0 {
			w.Header().Set("Retry-After", strconv.Itoa(provider.RetryAfter))
		}
		fail(w, status, provider.Code, provider.Message)
		return
	}
	s.internal(w, "metadata", err)
}
func (s *Server) tmdbSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		configured, updated, err := s.metadata.Status(r.Context())
		if err != nil {
			s.internal(w, "tmdb_status", err)
			return
		}
		send(w, 200, map[string]any{"configured": configured, "updated_at": updated})
	case http.MethodPut:
		var body struct {
			Token string `json:"token"`
		}
		if !decodeJSON(w, r, &body) {
			return
		}
		if !metadata.ValidToken(body.Token) {
			fail(w, 400, "invalid_token", "Enter your TMDb API Read Access Token (not an API key).")
			return
		}
		if err := s.metadata.Configure(r.Context(), body.Token); err != nil {
			s.providerError(w, err)
			return
		}
		send(w, 200, map[string]bool{"configured": true, "tested": true})
	case http.MethodDelete:
		if err := s.metadata.Remove(r.Context()); err != nil {
			s.internal(w, "tmdb_remove", err)
			return
		}
		send(w, 200, map[string]bool{"configured": false})
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		fail(w, 405, "method_not_allowed", "Use GET, PUT or DELETE.")
	}
}
func (s *Server) tmdbTest(w http.ResponseWriter, r *http.Request) {
	if err := s.metadata.Test(r.Context()); err != nil {
		s.providerError(w, err)
		return
	}
	send(w, 200, map[string]bool{"connected": true})
}
func (s *Server) searchMovies(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("query"))
	page, valid := pageNumber(r, 500)
	if !valid || !metadata.ValidQuery(query, page) {
		fail(w, 400, "invalid_search", "Enter 1–200 characters and a page between 1 and 500.")
		return
	}
	results, err := s.metadata.Search(r.Context(), query, page)
	if err != nil {
		s.providerError(w, err)
		return
	}
	type result struct {
		metadata.Movie
		InLibrary bool `json:"in_library"`
	}
	items := make([]result, 0, len(results.Items))
	for _, movie := range results.Items {
		_, err := s.library.Movie(r.Context(), movie.TMDbID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			s.internal(w, "movie_membership", err)
			return
		}
		items = append(items, result{Movie: movie, InLibrary: err == nil})
	}
	send(w, 200, map[string]any{"items": items, "page": results.Page, "total_pages": results.TotalPages, "total_results": results.TotalResults})
}
func (s *Server) movieDetails(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		fail(w, 400, "invalid_movie", "Choose a valid TMDb movie ID.")
		return
	}
	movie, err := s.metadata.Movie(r.Context(), id)
	if err != nil {
		s.providerError(w, err)
		return
	}
	send(w, 200, movie)
}
func (s *Server) addMovie(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TMDbID    int64  `json:"tmdb_id"`
		Monitored *bool  `json:"monitored"`
		Category  string `json:"category"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.TMDbID < 1 || (body.Category != "" && body.Category != "standard" && body.Category != "anime") {
		fail(w, 400, "invalid_movie", "Choose a valid TMDb movie ID.")
		return
	}
	// A retry of a successful add works even if TMDb is temporarily offline.
	if existing, err := s.library.Movie(r.Context(), body.TMDbID); err == nil {
		send(w, 200, map[string]any{"item": existing, "created": false})
		return
	} else if !errors.Is(err, sql.ErrNoRows) {
		s.internal(w, "movie_lookup", err)
		return
	}
	movie, err := s.metadata.Movie(r.Context(), body.TMDbID)
	if err != nil {
		s.providerError(w, err)
		return
	}
	monitored := library.DefaultMovieMonitored(movie)
	if body.Monitored != nil {
		monitored = *body.Monitored
	}
	item, created, err := s.library.AddMovie(r.Context(), movie, monitored, body.Category)
	if err != nil {
		s.internal(w, "movie_add", err)
		return
	}
	if created && item.Monitored && s.automation != nil {
		// Sonarr/Radarr-style search on add: the focused pipeline follows the
		// saved automatic-search mode, while the feed check keeps watching for
		// new releases independently.
		if e := s.automation.RequestItem(r.Context(), item.ID, nil); e != nil {
			s.logger.Warn("search after movie add was not requested", "movie", item.ID, "error", e)
		}
	}
	status := 200
	if created {
		status = 201
	}
	send(w, status, map[string]any{"item": item, "created": created})
}
