package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/faaaaq/ReelScarey/internal/youtube"
)

func (s *Server) youtubeBulkReview(w http.ResponseWriter, r *http.Request) {
	if !s.youtubeReady(w) {
		return
	}
	var in struct {
		URL       string                  `json:"source_url"`
		IDs       []string                `json:"video_ids"`
		Target    youtube.Target          `json:"target"`
		Subtitles youtube.DownloadOptions `json:"subtitles"`
	}
	if !decodeBoundedJSON(w, r, &in, 128<<10) {
		return
	}
	if len(in.IDs) < 1 || len(in.IDs) > 25 {
		s.youtubeError(w, youtube.ErrInput)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
	defer cancel()
	source, e := s.youtubeInspector.Inspect(ctx, in.URL)
	if e != nil {
		s.youtubeError(w, e)
		return
	}
	known := map[string]youtube.Entry{}
	for _, v := range source.Entries {
		if v.Kind == "video" {
			known[v.ID] = v
		}
	}
	seen := map[string]bool{}
	for _, id := range in.IDs {
		if seen[id] || known[id].ID == "" {
			s.youtubeError(w, youtube.ErrChanged)
			return
		}
		seen[id] = true
	}
	out := []youtube.Review{}
	for _, id := range in.IDs {
		v := known[id]
		review, e := s.youtube.PreviewWithSubtitles(ctx, youtube.Video{ID: v.ID, Title: v.Title, Channel: v.Channel}, in.Target, in.Subtitles)
		if e != nil {
			s.youtubeError(w, e)
			return
		}
		out = append(out, review)
	}
	send(w, 200, map[string]any{"items": out})
}
func (s *Server) youtubeBulkQueue(w http.ResponseWriter, r *http.Request) {
	if !s.youtubeReady(w) {
		return
	}
	var in struct {
		IDs      []string `json:"review_ids"`
		Reviewed bool     `json:"reviewed"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if !in.Reviewed {
		fail(w, 400, "review_required", "Review every selected video and destination before queuing.")
		return
	}
	jobs, e := s.youtube.QueueBulk(r.Context(), in.IDs)
	if e != nil {
		s.youtubeError(w, e)
		return
	}
	send(w, 201, map[string]any{"items": jobs})
}
func (s *Server) youtubeSubscriptions(w http.ResponseWriter, r *http.Request) {
	if !s.youtubeReady(w) {
		return
	}
	switch r.Method {
	case "GET":
		v, e := s.youtube.Subscriptions(r.Context())
		if e != nil {
			s.youtubeError(w, e)
			return
		}
		send(w, 200, map[string]any{"items": v})
	case "POST", "PUT":
		var in youtube.SubscriptionUpdate
		if !decodeJSON(w, r, &in) {
			return
		}
		if (r.Method == "POST") != (in.ID == "") {
			s.youtubeError(w, youtube.ErrInput)
			return
		}
		v, e := s.youtube.SaveSubscription(r.Context(), in)
		if e != nil {
			s.youtubeError(w, e)
			return
		}
		send(w, 200, v)
	default:
		w.Header().Set("Allow", "GET, POST, PUT")
		fail(w, 405, "method_not_allowed", "Use GET, POST or PUT.")
	}
}
func (s *Server) youtubeSubscription(w http.ResponseWriter, r *http.Request) {
	if !s.youtubeReady(w) {
		return
	}
	if r.Method == "GET" {
		v, e := s.youtube.SubscriptionEntries(r.Context(), r.PathValue("id"))
		if e != nil {
			s.youtubeError(w, e)
			return
		}
		send(w, 200, map[string]any{"items": v})
		return
	}
	if r.Method == "POST" || r.Method == "DELETE" {
		var in struct {
			Revision int `json:"revision"`
		}
		if !decodeJSON(w, r, &in) {
			return
		}
		var e error
		if r.Method == "DELETE" {
			e = s.youtube.DeleteSubscription(r.Context(), r.PathValue("id"), in.Revision)
		} else {
			e = s.youtube.RequestSubscription(r.Context(), r.PathValue("id"), in.Revision)
		}
		if e != nil {
			s.youtubeError(w, e)
			return
		}
		if r.Method == "DELETE" {
			send(w, 200, map[string]bool{"deleted": true})
			return
		}
		send(w, 202, map[string]bool{"queued": true})
		return
	}
	w.Header().Set("Allow", "GET, POST, DELETE")
	fail(w, 405, "method_not_allowed", "Use GET, POST or DELETE.")
}
