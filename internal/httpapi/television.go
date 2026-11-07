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

func (s *Server) tvError(w http.ResponseWriter, e error) {
	if errors.Is(e, sql.ErrNoRows) {
		fail(w, 404, "not_found", "This show, season or episode is not in your library.")
		return
	}
	if errors.Is(e, library.ErrRevision) {
		fail(w, 409, "revision_conflict", e.Error())
		return
	}
	s.providerError(w, e)
}
func (s *Server) searchSeries(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("query"))
	page, ok := pageNumber(r, 500)
	if !ok || !metadata.ValidQuery(q, page) {
		fail(w, 400, "invalid_search", "Enter 1–200 characters and a page between 1 and 500.")
		return
	}
	results, e := s.metadata.SearchSeries(r.Context(), q, page)
	if e != nil {
		s.tvError(w, e)
		return
	}
	type result struct {
		metadata.Series
		InLibrary bool `json:"in_library"`
	}
	items := make([]result, 0, len(results.Items))
	for _, v := range results.Items {
		_, e := s.library.Series(r.Context(), v.TMDbID)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			s.tvError(w, e)
			return
		}
		items = append(items, result{Series: v, InLibrary: e == nil})
	}
	send(w, 200, map[string]any{"items": items, "page": results.Page, "total_pages": results.TotalPages, "total_results": results.TotalResults})
}
func tvID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if e != nil || id < 1 {
		fail(w, 400, "invalid_series", "Choose a valid show.")
		return 0, false
	}
	return id, true
}
func tvNumber(w http.ResponseWriter, r *http.Request, key string, min, max int) (int, bool) {
	v, e := strconv.Atoi(r.PathValue(key))
	if e != nil || v < min || v > max {
		fail(w, 400, "invalid_episode", "Choose a valid season or episode number.")
		return 0, false
	}
	return v, true
}
func (s *Server) seriesDetails(w http.ResponseWriter, r *http.Request) {
	id, ok := tvID(w, r)
	if !ok {
		return
	}
	v, e := s.metadata.Series(r.Context(), id)
	if e != nil {
		s.tvError(w, e)
		return
	}
	send(w, 200, v)
}
func (s *Server) addSeries(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TMDbID    int64  `json:"tmdb_id"`
		Monitored *bool  `json:"monitored"`
		Category  string `json:"category"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.TMDbID < 1 || (body.Category != "" && body.Category != "standard" && body.Category != "anime") {
		fail(w, 400, "invalid_series", "Choose a valid TMDb show ID.")
		return
	}
	if existing, e := s.library.Series(r.Context(), body.TMDbID); e == nil {
		send(w, 200, map[string]any{"item": existing, "created": false})
		return
	} else if !errors.Is(e, sql.ErrNoRows) {
		s.tvError(w, e)
		return
	}
	show, e := s.metadata.Series(r.Context(), body.TMDbID)
	if e != nil {
		s.tvError(w, e)
		return
	}
	monitored := true
	if body.Monitored != nil {
		monitored = *body.Monitored
	}
	item, created, e := s.library.AddSeries(r.Context(), show, monitored, body.Category)
	if e != nil {
		s.tvError(w, e)
		return
	}
	status := 200
	if created {
		status = 201
	}
	send(w, status, map[string]any{"item": item, "created": created})
}
func (s *Server) catalogSeries(w http.ResponseWriter, r *http.Request) {
	id, ok := tvID(w, r)
	if !ok {
		return
	}
	s.sendSeries(w, r, id)
}
func (s *Server) sendSeries(w http.ResponseWriter, r *http.Request, id int64) {
	out, e := s.library.TVShow(r.Context(), id)
	if e != nil {
		s.tvError(w, e)
		return
	}
	send(w, 200, out)
}
func (s *Server) catalogSeason(w http.ResponseWriter, r *http.Request) {
	id, ok := tvID(w, r)
	if !ok {
		return
	}
	n, ok := tvNumber(w, r, "number", 0, 10000)
	if !ok {
		return
	}
	out, e := s.library.TVSeason(r.Context(), id, n)
	if e != nil {
		s.tvError(w, e)
		return
	}
	send(w, 200, out)
}
func (s *Server) refreshTV(w http.ResponseWriter, r *http.Request) {
	id, ok := tvID(w, r)
	if !ok {
		return
	}
	var body struct {
		Revision int64 `json:"revision"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.Revision < 1 {
		fail(w, 400, "invalid_revision", "Refresh this show before saving.")
		return
	}
	show, e := s.library.TVShow(r.Context(), id)
	if e != nil {
		s.tvError(w, e)
		return
	}
	if show.Revision != body.Revision {
		s.tvError(w, library.ErrRevision)
		return
	}
	if r.PathValue("number") == "" {
		var fresh metadata.Series
		fresh, e = s.metadata.Series(r.Context(), show.TMDbID)
		if e == nil {
			e = s.library.RefreshSeries(r.Context(), id, fresh, body.Revision)
		}
	} else {
		n, ok := tvNumber(w, r, "number", 0, 10000)
		if !ok {
			return
		}
		found := false
		for _, v := range show.Seasons {
			if v.Number == n {
				found = true
			}
		}
		if !found {
			s.tvError(w, sql.ErrNoRows)
			return
		}
		var season metadata.Season
		season, e = s.metadata.Season(r.Context(), show.TMDbID, n)
		if e == nil {
			e = s.library.SaveSeason(r.Context(), id, season, body.Revision)
		}
	}
	if e != nil {
		s.tvError(w, e)
		return
	}
	s.sendSeries(w, r, id)
}

// unlinkEpisodeFile removes one episode's library association after review.
// The file itself stays on disk; watching, seeding sources and other episodes
// that share the file are untouched.
func (s *Server) unlinkEpisodeFile(w http.ResponseWriter, r *http.Request) {
	id, ok := tvID(w, r)
	if !ok {
		return
	}
	episode, e := strconv.ParseInt(r.PathValue("episode"), 10, 64)
	if e != nil || episode < 1 {
		fail(w, 400, "invalid_selection", "Choose a saved episode.")
		return
	}
	var body struct {
		Revision int64 `json:"revision"`
		Reviewed bool  `json:"reviewed"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if !body.Reviewed || body.Revision < 1 {
		fail(w, 400, "review_required", "Review removing this episode from the library first.")
		return
	}
	if e = s.library.UnlinkEpisodeFile(r.Context(), id, body.Revision, episode); e != nil {
		s.tvError(w, e)
		return
	}
	s.sendSeries(w, r, id)
}

func (s *Server) monitorAllEpisodes(w http.ResponseWriter, r *http.Request) {
	id, ok := tvID(w, r)
	if !ok {
		return
	}
	var body struct {
		Revision int64 `json:"revision"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.Revision < 1 {
		fail(w, 400, "invalid_monitoring", "Reload this show and try again.")
		return
	}
	if err := s.library.MonitorAllEpisodes(r.Context(), id, body.Revision); err != nil {
		if errors.Is(err, library.ErrMonitoringInput) {
			fail(w, 400, "invalid_monitoring", "Reload this show and try again.")
			return
		}
		s.tvError(w, err)
		return
	}
	s.sendSeries(w, r, id)
}

// monitorAllShows is the explicit one-time bulk action behind "Monitor all
// future episodes" on the TV Shows and Anime libraries.
func (s *Server) monitorAllShows(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Category string `json:"category"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	count, err := s.library.MonitorAllShows(r.Context(), body.Category)
	if err != nil {
		if errors.Is(err, library.ErrMonitoringInput) {
			fail(w, 400, "invalid_monitoring", "Choose the standard or anime library.")
			return
		}
		s.tvError(w, err)
		return
	}
	send(w, 200, map[string]int{"shows": count})
}

func (s *Server) monitorTV(w http.ResponseWriter, r *http.Request) {
	id, ok := tvID(w, r)
	if !ok {
		return
	}
	var body struct {
		Monitored *bool `json:"monitored"`
		Revision  int64 `json:"revision"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.Monitored == nil || body.Revision < 1 {
		fail(w, 400, "invalid_monitoring", "Choose a monitoring preference and current revision.")
		return
	}
	var season, episode *int
	if r.PathValue("number") != "" {
		n, ok := tvNumber(w, r, "number", 0, 10000)
		if !ok {
			return
		}
		season = &n
	}
	if r.PathValue("episode") != "" {
		n, ok := tvNumber(w, r, "episode", 1, 100000)
		if !ok {
			return
		}
		episode = &n
	}
	if e := s.library.MonitorTV(r.Context(), id, body.Revision, season, episode, *body.Monitored); e != nil {
		s.tvError(w, e)
		return
	}
	s.sendSeries(w, r, id)
}

func (s *Server) monitorEpisodes(w http.ResponseWriter, r *http.Request) {
	id, ok := tvID(w, r)
	if !ok {
		return
	}
	season, ok := tvNumber(w, r, "number", 0, 10000)
	if !ok {
		return
	}
	var body struct {
		Monitored  *bool   `json:"monitored"`
		Revision   int64   `json:"revision"`
		EpisodeIDs []int64 `json:"episode_ids"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.Monitored == nil || body.Revision < 1 {
		fail(w, 400, "invalid_monitoring", "Choose episode IDs, a preference and the current revision.")
		return
	}
	if err := s.library.MonitorEpisodes(r.Context(), id, body.Revision, season, body.EpisodeIDs, *body.Monitored); err != nil {
		if errors.Is(err, library.ErrMonitoringInput) {
			fail(w, 400, "invalid_monitoring", "Choose 1–100 distinct episode IDs from this season.")
			return
		}
		s.tvError(w, err)
		return
	}
	s.sendSeries(w, r, id)
}

func (s *Server) monitorSeasons(w http.ResponseWriter, r *http.Request) {
	id, ok := tvID(w, r)
	if !ok {
		return
	}
	var body struct {
		Monitored *bool   `json:"monitored"`
		Revision  int64   `json:"revision"`
		SeasonIDs []int64 `json:"season_ids"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.Monitored == nil || body.Revision < 1 {
		fail(w, 400, "invalid_monitoring", "Choose season IDs, a preference and the current revision.")
		return
	}
	if err := s.library.MonitorSeasons(r.Context(), id, body.Revision, body.SeasonIDs, *body.Monitored); err != nil {
		if errors.Is(err, library.ErrMonitoringInput) {
			fail(w, 400, "invalid_monitoring", "Choose 1–100 distinct season IDs from this show.")
			return
		}
		s.tvError(w, err)
		return
	}
	s.sendSeries(w, r, id)
}
