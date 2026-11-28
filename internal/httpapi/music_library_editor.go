package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/faaaaq/ReelScarey/internal/music"
)

func (s *Server) musicLibraryEditor(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, 503, "music_unavailable", "The music catalog is unavailable.")
		return
	}
	switch r.Method {
	case http.MethodGet:
		query := r.URL.Query()
		parts := strings.Split(query.Get("ids"), ",")
		if len(parts) > 200 {
			s.musicError(w, music.ErrInvalidInput)
			return
		}
		ids := make([]int64, 0, len(parts))
		for _, part := range parts {
			id, err := strconv.ParseInt(part, 10, 64)
			if err != nil {
				s.musicError(w, music.ErrInvalidInput)
				return
			}
			ids = append(ids, id)
		}
		result, err := s.music.LibraryEditor(r.Context(), query.Get("kind"), ids)
		if err != nil {
			s.musicError(w, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		send(w, http.StatusOK, result)
	case http.MethodPut:
		var input music.LibraryEditorUpdate
		if !decodeJSONLimit(w, r, &input, 7<<20) {
			return
		}
		result, err := s.music.UpdateLibraryEditor(r.Context(), input)
		if err != nil {
			s.musicError(w, err)
			return
		}
		send(w, http.StatusOK, result)
	default:
		w.Header().Set("Allow", "GET, PUT")
		fail(w, http.StatusMethodNotAllowed, "method_not_allowed", "Use GET or PUT.")
	}
}

func (s *Server) musicLibraryEditorArtwork(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, 503, "music_unavailable", "The music catalog is unavailable.")
		return
	}
	query := r.URL.Query()
	items, err := s.music.SearchLibraryArtwork(r.Context(), query.Get("kind"), query.Get("query"))
	if err != nil {
		s.musicError(w, err)
		return
	}
	send(w, http.StatusOK, map[string]any{"items": items})
}
