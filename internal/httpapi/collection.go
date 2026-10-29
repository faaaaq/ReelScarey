package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
)

func (s *Server) movieCollection(w http.ResponseWriter, r *http.Request) {
	id, ok := tvID(w, r)
	if !ok {
		return
	}
	item, e := s.library.Item(r.Context(), id)
	if errors.Is(e, sql.ErrNoRows) {
		fail(w, 404, "not_found", "This title is not in the library.")
		return
	}
	if e != nil {
		s.internal(w, "collection_item", e)
		return
	}
	if item.Kind != "movie" {
		fail(w, 400, "invalid_movie", "Movie collections are available for movies.")
		return
	}
	if s.metadata == nil {
		fail(w, 503, "metadata_unavailable", "TMDb is unavailable.")
		return
	}
	collection, e := s.metadata.MovieCollection(r.Context(), item.TMDbID)
	if e != nil {
		s.providerError(w, e)
		return
	}
	type member struct {
		ID        int64  `json:"id"`
		TMDbID    int64  `json:"tmdb_id"`
		Title     string `json:"title"`
		Year      *int   `json:"year"`
		PosterURL string `json:"poster_url"`
		Status    string `json:"status"`
	}
	members := make([]member, 0, len(collection.Items))
	for _, m := range collection.Items {
		entry := member{TMDbID: m.TMDbID, Title: m.Title, Year: m.Year, PosterURL: m.PosterURL}
		saved, err := s.library.Movie(r.Context(), m.TMDbID)
		if err == nil {
			entry.ID = saved.ID
			entry.Status = saved.Status
		} else if !errors.Is(err, sql.ErrNoRows) {
			s.internal(w, "collection_lookup", err)
			return
		}
		members = append(members, entry)
	}
	send(w, 200, map[string]any{"name": collection.Name, "items": members})
}
