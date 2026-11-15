package httpapi

import (
	"net/http"
	"strconv"

	"github.com/faaaaq/ReelScarey/internal/youtube"
)

type seriesSourceItem struct {
	youtube.SeriesSource
	Entries []youtube.SeriesEntry `json:"entries"`
	Matches []youtube.SeriesMatch `json:"matches"`
}

// youtubeSourcesOverview is the shows-overview feed. It is a pure database
// read, so it works even when yt-dlp is not installed on the server.
func (s *Server) youtubeSourcesOverview(w http.ResponseWriter, r *http.Request) {
	out, err := s.youtube.SeriesSourceSummaries(r.Context())
	if err != nil {
		s.youtubeError(w, err)
		return
	}
	send(w, 200, map[string]any{"items": out})
}

// youtubeSourceEpisodes serves the per-episode status table for one source.
// The state is derived from jobs, registered files and subtitle evidence; the
// read never mutates anything and works without yt-dlp.
func (s *Server) youtubeSourceEpisodes(w http.ResponseWriter, r *http.Request) {
	out, err := s.youtube.SeriesEpisodeStatuses(r.Context(), r.PathValue("id"), 500)
	if err != nil {
		s.youtubeError(w, err)
		return
	}
	send(w, 200, map[string]any{"items": out})
}

// youtubeSourceEpisodesQueue applies a bounded, reviewed batch of episode
// selections. The source revision fences the write; a stale revision is a 409.
func (s *Server) youtubeSourceEpisodesQueue(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Revision   int                              `json:"revision"`
		Selections []youtube.SeriesEpisodeSelection `json:"selections"`
		Reviewed   bool                             `json:"reviewed"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if !in.Reviewed {
		fail(w, 400, "review_required", "Review every selected episode before queuing.")
		return
	}
	out, err := s.youtube.QueueSeriesEpisodes(r.Context(), r.PathValue("id"), in.Revision, in.Reviewed, in.Selections)
	if err != nil {
		s.youtubeError(w, err)
		return
	}
	send(w, 200, map[string]any{"items": out})
}

func (s *Server) seriesYouTubeSources(w http.ResponseWriter, r *http.Request) {
	id, ok := tvID(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		// A pure catalog read: registered sources and their entries exist in
		// the database even when yt-dlp is not installed, so this branch never
		// depends on the inspector. Preview/check/queue actions below and in
		// the sibling handlers stay gated on yt-dlp availability.
		sources, err := s.youtube.SeriesSources(r.Context(), id)
		if err != nil {
			s.youtubeError(w, err)
			return
		}
		items := make([]seriesSourceItem, 0, len(sources))
		for _, source := range sources {
			entries, err := s.youtube.SeriesEntries(r.Context(), source.ID)
			if err != nil {
				s.youtubeError(w, err)
				return
			}
			matches, err := s.youtube.SeriesMatches(r.Context(), source.ID)
			if err != nil {
				s.youtubeError(w, err)
				return
			}
			items = append(items, seriesSourceItem{SeriesSource: source, Entries: entries, Matches: matches})
		}
		send(w, 200, map[string]any{"items": items})
	case http.MethodPost:
		if !s.youtubeReady(w) {
			return
		}
		var in youtube.SeriesSourceInput
		if !decodeJSON(w, r, &in) {
			return
		}
		out, err := s.youtube.SaveSeriesSource(r.Context(), id, in)
		if err != nil {
			s.youtubeError(w, err)
			return
		}
		send(w, 200, out)
	default:
		w.Header().Set("Allow", "GET, POST")
		fail(w, 405, "method_not_allowed", "Use GET or POST.")
	}
}

func (s *Server) seriesYouTubePreview(w http.ResponseWriter, r *http.Request) {
	if !s.youtubeReady(w) {
		return
	}
	id, ok := tvID(w, r)
	if !ok {
		return
	}
	var in youtube.SeriesSourceInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.youtube.PreviewSeriesSource(r.Context(), id, in, s.youtubeInspector)
	if err != nil {
		s.youtubeError(w, err)
		return
	}
	send(w, 200, out)
}

func (s *Server) seriesYouTubeSource(w http.ResponseWriter, r *http.Request) {
	if !s.youtubeReady(w) {
		return
	}
	id, ok := tvID(w, r)
	if !ok {
		return
	}
	sourceID := r.PathValue("source")
	if sourceID == "" {
		fail(w, 400, "invalid_selection", "Choose a YouTube source.")
		return
	}
	switch r.Method {
	case http.MethodDelete:
		revision, err := strconv.Atoi(r.URL.Query().Get("revision"))
		if err != nil || revision < 1 {
			fail(w, 400, "invalid_selection", "Reload the source before removing it.")
			return
		}
		if err = s.youtube.RemoveSeriesSource(r.Context(), id, sourceID, revision); err != nil {
			s.youtubeError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case http.MethodPost:
		var in struct {
			Revision int    `json:"revision"`
			Action   string `json:"action"`
		}
		if !decodeJSON(w, r, &in) {
			return
		}
		var err error
		if in.Action == "backfill" {
			err = s.youtube.BackfillSeriesSource(r.Context(), id, sourceID, in.Revision)
		} else {
			err = s.youtube.RequestSeriesCheck(r.Context(), id, sourceID, in.Revision)
		}
		if err != nil {
			s.youtubeError(w, err)
			return
		}
		send(w, 202, map[string]bool{"requested": true})
	default:
		w.Header().Set("Allow", "POST, DELETE")
		fail(w, 405, "method_not_allowed", "Use POST or DELETE.")
	}
}

// seriesYouTubeMatches lets the owner bind the videos of a manual playlist
// source to explicit episodes of the show. GET lists the playlist's videos with
// the show's episodes; PUT replaces the bindings.
func (s *Server) seriesYouTubeMatches(w http.ResponseWriter, r *http.Request) {
	if !s.youtubeReady(w) {
		return
	}
	id, ok := tvID(w, r)
	if !ok {
		return
	}
	sourceID := r.PathValue("source")
	if sourceID == "" {
		fail(w, 400, "invalid_selection", "Choose a YouTube source.")
		return
	}
	switch r.Method {
	case http.MethodGet:
		out, err := s.youtube.PreviewSeriesMatches(r.Context(), id, sourceID, s.youtubeInspector)
		if err != nil {
			s.youtubeError(w, err)
			return
		}
		send(w, 200, out)
	case http.MethodPut:
		var in struct {
			Revision int                        `json:"revision"`
			Queue    bool                       `json:"queue"`
			Matches  []youtube.SeriesMatchInput `json:"matches"`
		}
		if !decodeJSON(w, r, &in) {
			return
		}
		n, err := s.youtube.SaveSeriesMatches(r.Context(), id, sourceID, in.Revision, in.Queue, in.Matches)
		if err != nil {
			s.youtubeError(w, err)
			return
		}
		send(w, 200, map[string]any{"saved": n, "queued": in.Queue})
	default:
		w.Header().Set("Allow", "GET, PUT")
		fail(w, 405, "method_not_allowed", "Use GET or PUT.")
	}
}
