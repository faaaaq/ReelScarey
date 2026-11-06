package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/faaaaq/ReelScarey/internal/youtube"
)

func (s *Server) youtubePlaylistPreview(w http.ResponseWriter, r *http.Request) {
	if !s.youtubeReady(w) {
		return
	}
	var in struct {
		URL    string `json:"source_url"`
		ShowID int64  `json:"show_id"`
		Season int    `json:"season_number"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 70*time.Second)
	defer cancel()
	out, err := s.youtube.PreviewPlaylist(ctx, in.URL, in.ShowID, in.Season, s.youtubeInspector)
	if err != nil {
		s.youtubeError(w, err)
		return
	}
	send(w, 200, out)
}

func (s *Server) youtubePlaylistQueue(w http.ResponseWriter, r *http.Request) {
	if !s.youtubeReady(w) {
		return
	}
	var in youtube.PlaylistQueueInput
	if !decodeJSON(w, r, &in) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()
	out, err := s.youtube.QueuePlaylist(ctx, in, s.youtubeInspector)
	if err != nil {
		s.youtubeError(w, err)
		return
	}
	send(w, 201, map[string]any{"items": out})
}
