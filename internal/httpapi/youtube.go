package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/faaaaq/ReelScarey/internal/youtube"
)

func (s *Server) youtubeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, youtube.ErrInput), errors.Is(err, youtube.ErrInvalidURL):
		fail(w, 400, "invalid_youtube_source", err.Error())
	case errors.Is(err, youtube.ErrChanged), errors.Is(err, youtube.ErrBusy):
		fail(w, 409, "youtube_changed", err.Error())
	case errors.Is(err, sql.ErrNoRows):
		fail(w, 404, "not_found", "YouTube item was not found.")
	case errors.Is(err, youtube.ErrInspection):
		fail(w, 502, "youtube_unavailable", "The public YouTube source could not be inspected.")
	default:
		s.internal(w, "youtube", err)
	}
}
func (s *Server) youtubeReady(w http.ResponseWriter) bool {
	if s.youtubeInspector == nil {
		fail(w, 503, "youtube_unavailable", "Install yt-dlp on the ReelScarey server and restart the app to use YouTube sourcing.")
		return false
	}
	return true
}
func (s *Server) youtubeInspect(w http.ResponseWriter, r *http.Request) {
	if !s.youtubeReady(w) {
		return
	}
	var in struct {
		URL string `json:"url"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
	defer cancel()
	out, err := s.youtubeInspector.Inspect(ctx, in.URL)
	if err != nil {
		s.youtubeError(w, err)
		return
	}
	send(w, 200, out)
}
func (s *Server) youtubeCollections(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		out, err := s.youtube.Collections(r.Context())
		if err != nil {
			s.youtubeError(w, err)
			return
		}
		send(w, 200, map[string]any{"items": out})
	case http.MethodPost:
		var in struct {
			Title string `json:"title"`
		}
		if !decodeJSON(w, r, &in) {
			return
		}
		out, err := s.youtube.CreateCollection(r.Context(), in.Title)
		if err != nil {
			s.youtubeError(w, err)
			return
		}
		send(w, 201, out)
	default:
		w.Header().Set("Allow", "GET, POST")
		fail(w, 405, "method_not_allowed", "Use GET or POST.")
	}
}
func (s *Server) youtubeReview(w http.ResponseWriter, r *http.Request) {
	if !s.youtubeReady(w) {
		return
	}
	var in struct {
		VideoID   string                  `json:"video_id"`
		Target    youtube.Target          `json:"target"`
		Subtitles youtube.DownloadOptions `json:"subtitles"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	source, err := youtube.NormalizeURL("https://www.youtube.com/watch?v=" + in.VideoID)
	if err != nil || source.ID != in.VideoID {
		s.youtubeError(w, youtube.ErrInvalidURL)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
	defer cancel()
	found, err := s.youtubeInspector.Inspect(ctx, source.URL)
	if err != nil {
		s.youtubeError(w, err)
		return
	}
	if len(found.Entries) != 1 || found.Entries[0].ID != in.VideoID {
		s.youtubeError(w, youtube.ErrInspection)
		return
	}
	v := found.Entries[0]
	out, err := s.youtube.PreviewWithSubtitles(ctx, youtube.Video{ID: v.ID, Title: v.Title, Channel: v.Channel}, in.Target, in.Subtitles)
	if err != nil {
		s.youtubeError(w, err)
		return
	}
	send(w, 200, out)
}
func (s *Server) youtubeQueue(w http.ResponseWriter, r *http.Request) {
	if !s.youtubeReady(w) {
		return
	}
	var in struct{}
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.youtube.Queue(r.Context(), r.PathValue("id"))
	if err != nil {
		s.youtubeError(w, err)
		return
	}
	send(w, 201, out)
}
func (s *Server) youtubeJobs(w http.ResponseWriter, r *http.Request) {
	out, err := s.youtube.Jobs(r.Context())
	if err != nil {
		s.youtubeError(w, err)
		return
	}
	send(w, 200, map[string]any{"items": out})
}
func (s *Server) youtubeFiles(w http.ResponseWriter, r *http.Request) {
	out, err := s.youtube.Files(r.Context())
	if err != nil {
		s.youtubeError(w, err)
		return
	}
	send(w, 200, map[string]any{"items": out})
}
func (s *Server) youtubeCancel(w http.ResponseWriter, r *http.Request) {
	var in struct{}
	if !decodeJSON(w, r, &in) {
		return
	}
	if err := s.youtube.Cancel(r.Context(), r.PathValue("id")); err != nil {
		s.youtubeError(w, err)
		return
	}
	send(w, 200, map[string]string{"status": "cancelled"})
}
func (s *Server) youtubeImportReview(w http.ResponseWriter, r *http.Request) {
	out, err := s.youtube.ReviewImport(r.Context(), s.config, r.PathValue("id"))
	if err != nil {
		s.youtubeError(w, err)
		return
	}
	send(w, 200, out)
}
func (s *Server) youtubeImport(w http.ResponseWriter, r *http.Request) {
	var in youtube.ImportReview
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.JobID != r.PathValue("id") {
		s.youtubeError(w, youtube.ErrInput)
		return
	}
	err := s.youtube.ApproveImport(r.Context(), s.config, in)
	if err != nil {
		s.youtubeError(w, err)
		return
	}
	send(w, 202, map[string]string{"status": "queued"})
}
