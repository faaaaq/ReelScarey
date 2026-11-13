package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/faaaaq/ReelScarey/internal/auth"
	"github.com/faaaaq/ReelScarey/internal/metadata"
	"github.com/faaaaq/ReelScarey/internal/quality"
)

// arrCompat implements a conservative, read-mostly subset of the Sonarr v3,
// Radarr v3 and Prowlarr v1 HTTP APIs. It never fabricates download history or
// files, and unsupported operations fail explicitly instead of pretending.
func (s *Server) arrCompat(w http.ResponseWriter, r *http.Request) {
	flavour, rest, ok := compatRoute(r.URL.Path)
	if !ok {
		fail(w, 404, "not_found", "This compatibility route does not exist.")
		return
	}
	if !s.compatAuthorized(w, r, flavour, rest) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	switch {
	case rest == "/" || rest == "":
		send(w, 200, map[string]any{"appName": "ReelScarey", "appVersion": s.version, "apiVersion": compatAPIVersion(flavour), "serverName": "ReelScarey", "isProduction": true})
	case rest == "/system/status":
		s.compatStatus(w, flavour)
	case rest == "/qualityprofile":
		s.compatQualityProfiles(ctx, w)
	case rest == "/rootfolder":
		s.compatRootFolders(ctx, w)
	case rest == "/tag" || rest == "/languageprofile" || rest == "/downloadclient":
		send(w, 200, []any{})
	case rest == "/indexer/schema":
		send(w, 200, []any{})
	case rest == "/health":
		// ReelScarey has no equivalent persisted health feed yet. An empty list
		// is honest; it does not claim every dependency is healthy.
		send(w, 200, []any{})
	case rest == "/movie" && r.Method == http.MethodGet:
		s.compatMovies(ctx, w)
	case rest == "/movie/lookup" && r.Method == http.MethodGet:
		s.compatMovieLookup(ctx, w, r)
	case rest == "/series" && r.Method == http.MethodGet:
		s.compatSeries(ctx, w)
	case rest == "/series/lookup" && r.Method == http.MethodGet:
		s.compatSeriesLookup(ctx, w, r)
	case rest == "/episode" && r.Method == http.MethodGet:
		s.compatEpisodes(ctx, w, r)
	case rest == "/calendar" && r.Method == http.MethodGet:
		s.compatCalendar(ctx, w, r)
	case rest == "/queue" && r.Method == http.MethodGet:
		s.compatQueue(ctx, w)
	case rest == "/history" && r.Method == http.MethodGet:
		s.compatHistory(ctx, w)
	case rest == "/indexer" && r.Method == http.MethodGet:
		s.compatIndexers(ctx, w)
	case rest == "/command" && r.Method == http.MethodPost:
		s.compatCommand(ctx, w, r)
	case rest == "/search":
		fail(w, 501, "not_implemented", "Prowlarr-style free-text search is not implemented. Use a Sonarr/Radarr release search or the ReelScarey UI.")
	case strings.HasPrefix(rest, "/movie/"), strings.HasPrefix(rest, "/series/"), strings.HasPrefix(rest, "/episode/"):
		fail(w, 501, "not_implemented", "Single-item compatibility reads are not implemented. List endpoints and commands are supported.")
	default:
		fail(w, 404, "not_found", "This compatibility endpoint is not implemented.")
	}
}

// decodeCompatCommand is deliberately more permissive than the first-party API:
// Sonarr/Radarr send additional fields and plural ID arrays. Unknown fields are
// ignored while the body stays bounded and a single JSON object is required.
func decodeCompatCommand(w http.ResponseWriter, r *http.Request, out any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(out); err != nil {
		invalidJSON(w, err)
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		invalidJSON(w, err)
		return false
	}
	return true
}

func compatAPIVersion(flavour string) string {
	if flavour == "prowlarr" {
		return "1.0.0"
	}
	return "3.0.0"
}

// compatRoute maps the supported prefixes to one canonical path.
func compatRoute(path string) (flavour, rest string, ok bool) {
	if path == "/api/v3" {
		return "root", "/", true
	}
	if strings.HasPrefix(path, "/api/v3/") {
		return "root", path[len("/api/v3"):], true
	}
	for _, name := range []string{"sonarr", "radarr", "prowlarr"} {
		prefix := "/" + name
		if path != prefix && !strings.HasPrefix(path, prefix+"/") {
			continue
		}
		rest = strings.TrimPrefix(path, prefix)
		for _, api := range []string{"/api/v3", "/api/v1"} {
			if strings.HasPrefix(rest, api) {
				rest = strings.TrimPrefix(rest, api)
				break
			}
		}
		if rest == "" {
			rest = "/"
		}
		if !strings.HasPrefix(rest, "/") {
			rest = "/" + rest
		}
		return name, rest, true
	}
	return "", "", false
}

// compatScope derives the required scope from the resource first. The route
// prefix never upgrades a key's scope: a Sonarr key cannot read a movie simply
// because the request was sent to the /sonarr prefix. Shared endpoints keep
// their prefix scope when one exists, or accept any compat scope at /api/v3.
func compatScope(flavour, rest string) string {
	switch {
	case strings.HasPrefix(rest, "/movie"):
		return "compat-radarr"
	case strings.HasPrefix(rest, "/series"), strings.HasPrefix(rest, "/episode"):
		return "compat-sonarr"
	case strings.HasPrefix(rest, "/indexer"):
		return "compat-prowlarr"
	}
	switch flavour {
	case "sonarr":
		return "compat-sonarr"
	case "radarr":
		return "compat-radarr"
	case "prowlarr":
		return "compat-prowlarr"
	}
	return ""
}

func (s *Server) compatAuthorized(w http.ResponseWriter, r *http.Request, flavour, rest string) bool {
	if key, ok := auth.APIKeyFrom(r.Context()); ok {
		scope := compatScope(flavour, rest)
		if scope != "" && !key.HasScope(scope) {
			fail(w, 403, "insufficient_scope", "This API key does not have the "+scope+" scope.")
			return false
		}
		if scope == "" && !key.HasAnyScope(auth.SupportedAPIKeyScopes...) {
			fail(w, 403, "insufficient_scope", "This API key has no compatibility scope.")
			return false
		}
		return true
	}
	// Presence of a session in context is not authorization by itself
	// (RS-0024): only an administrator session may inspect compatibility
	// output. compatSecurity already filters to admin sessions, so this check
	// is defense in depth against future call-site changes.
	if session, ok := r.Context().Value(sessionKey{}).(auth.Session); ok && session.Role == "admin" {
		return true
	}
	fail(w, 401, "authentication_required", "Provide a scoped API key.")
	return false
}

func (s *Server) compatStatus(w http.ResponseWriter, flavour string) {
	send(w, 200, map[string]any{
		"appName":        "ReelScarey",
		"appVersion":     s.version,
		"apiVersion":     compatAPIVersion(flavour),
		"osName":         runtime.GOOS,
		"isProduction":   true,
		"serverName":     "ReelScarey",
		"urlBase":        "",
		"startupPath":    "/",
		"authentication": "apiKey",
		"capabilities": map[string]bool{
			"supportsOnTheFlyRelease": false,
			"supportsSearch":          true,
			"supportsQueue":           true,
			"supportsCalendar":        true,
		},
	})
}

func (s *Server) compatQualityProfiles(ctx context.Context, w http.ResponseWriter) {
	profiles, err := quality.New(s.db).List(ctx)
	if err != nil {
		s.internal(w, "compat_quality_profiles", err)
		return
	}
	out := make([]map[string]any, 0, len(profiles))
	for _, p := range profiles {
		items := make([]map[string]any, 0, len(p.Qualities))
		for _, q := range p.Qualities {
			items = append(items, map[string]any{"quality": map[string]any{"id": q, "name": qualityName(q)}, "allowed": true})
		}
		out = append(out, map[string]any{
			"id": p.ID, "name": p.Name, "upgradeAllowed": true, "cutoff": p.Cutoff, "items": items,
		})
	}
	send(w, 200, out)
}

func qualityName(q int) string {
	switch q {
	case 480:
		return "SDTV"
	case 720:
		return "HDTV-720p"
	case 1080:
		return "HDTV-1080p"
	case 2160:
		return "WEBDL-2160p"
	default:
		return strconv.Itoa(q) + "p"
	}
}

// compatRootFolders deliberately reports the real configured library paths.
// RS-0022 flagged the absolute-path disclosure to scoped compat keys, but real
// Sonarr/Radarr clients validate and display root-folder paths when connecting
// and refuse synthetic paths, so the shape is kept and the disclosure is
// documented in docs/SECURITY.md. Revisit only with evidence that no client
// depends on the path values.
func (s *Server) compatRootFolders(ctx context.Context, w http.ResponseWriter) {
	prefs, err := s.settings.Get(ctx)
	if err != nil {
		s.internal(w, "compat_root_folders", err)
		return
	}
	paths := []string{filepath.Join(s.config.MediaDir, prefs.MovieFolder), filepath.Join(s.config.MediaDir, prefs.SeriesFolder)}
	out := make([]map[string]any, 0, len(paths))
	for i, p := range paths {
		accessible := false
		if info, statErr := os.Stat(p); statErr == nil && info.IsDir() {
			accessible = true
		}
		out = append(out, map[string]any{"id": i + 1, "path": p, "accessible": accessible, "freeSpace": 0})
	}
	send(w, 200, out)
}

func (s *Server) compatMovies(ctx context.Context, w http.ResponseWriter) {
	rows, err := s.db.QueryContext(ctx, `SELECT m.id,m.tmdb_id,m.title,m.year,m.monitored,m.status,m.poster_path,m.release_date,m.runtime_minutes,m.created_at,
		COALESCE((SELECT COUNT(*) FROM media_files f WHERE f.movie_id=m.id),0),
		COALESCE((SELECT SUM(f.size_bytes) FROM media_files f WHERE f.movie_id=m.id),0),
		COALESCE((SELECT profile_id FROM item_automation a WHERE a.media_id=m.id),0)
		FROM media_items m WHERE m.kind='movie' ORDER BY m.id LIMIT 1000`)
	if err != nil {
		s.internal(w, "compat_movies", err)
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, tmdbID, files, size, profile int64
		var title, status, poster, release, created string
		var year, runtime sql.NullInt64
		var monitored bool
		if err = rows.Scan(&id, &tmdbID, &title, &year, &monitored, &status, &poster, &release, &runtime, &created, &files, &size, &profile); err != nil {
			s.internal(w, "compat_movies", err)
			return
		}
		out = append(out, map[string]any{
			"id": id, "tmdbId": tmdbID, "title": title, "year": nullableInt(year), "monitored": monitored,
			"hasFile": files > 0, "sizeOnDisk": size, "qualityProfileId": profile, "path": "",
			"folderName": title, "status": compatMovieStatus(release), "isAvailable": compatReleased(release),
			"added": created, "runtime": nullableInt(runtime), "images": compatImages(poster, title),
		})
	}
	if err = rows.Err(); err != nil {
		s.internal(w, "compat_movies", err)
		return
	}
	send(w, 200, out)
}

func compatMovieStatus(release string) string {
	if compatReleased(release) {
		return "released"
	}
	return "announced"
}

func compatReleased(release string) bool {
	if len(release) != 10 {
		return false
	}
	t, err := time.Parse("2006-01-02", release)
	return err == nil && !t.After(time.Now())
}

func nullableInt(v sql.NullInt64) any {
	if !v.Valid {
		return nil
	}
	return v.Int64
}

func compatImages(poster, title string) []map[string]any {
	url := metadata.PosterURL(poster)
	if url == "" {
		return []map[string]any{}
	}
	return []map[string]any{{"coverType": "poster", "url": url, "remoteUrl": url, "title": title}}
}

func (s *Server) compatMovieLookup(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	if s.metadata == nil {
		fail(w, 503, "metadata_unavailable", "TMDb metadata is not configured.")
		return
	}
	term := strings.TrimSpace(r.URL.Query().Get("term"))
	if term == "" || len(term) > 200 {
		fail(w, 400, "invalid_term", "Provide a search term of at most 200 characters.")
		return
	}
	results, err := s.metadata.Search(ctx, term, 1)
	if err != nil {
		s.internal(w, "compat_movie_lookup", err)
		return
	}
	out := []map[string]any{}
	for _, m := range results.Items {
		out = append(out, map[string]any{"tmdbId": m.TMDbID, "title": m.Title, "year": m.Year, "overview": m.Overview, "remotePoster": metadata.PosterURL(m.PosterPath)})
	}
	send(w, 200, out)
}

type compatSeason struct {
	id        int64
	number    int
	monitored bool
	episodes  int
	loaded    int
	files     int
}

func (s *Server) compatSeries(ctx context.Context, w http.ResponseWriter) {
	rows, err := s.db.QueryContext(ctx, `SELECT m.id,m.tmdb_id,m.title,m.year,m.monitored,m.status,m.poster_path,m.created_at,sd.series_status,
		COALESCE((SELECT profile_id FROM item_automation a WHERE a.media_id=m.id),0)
		FROM media_items m JOIN series_details sd ON sd.media_id=m.id WHERE m.kind='series' ORDER BY m.id LIMIT 1000`)
	if err != nil {
		s.internal(w, "compat_series", err)
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, tmdbID, profile int64
		var title, status, poster, created, seriesStatus string
		var year sql.NullInt64
		var monitored bool
		if err = rows.Scan(&id, &tmdbID, &title, &year, &monitored, &status, &poster, &created, &seriesStatus, &profile); err != nil {
			s.internal(w, "compat_series", err)
			return
		}
		seasons, err := s.compatSeasons(ctx, id)
		if err != nil {
			s.internal(w, "compat_series", err)
			return
		}
		episodeCount, episodeFiles, totalEpisodes := 0, 0, 0
		seasonItems := make([]map[string]any, 0, len(seasons))
		for _, season := range seasons {
			episodeCount += season.loaded
			episodeFiles += season.files
			totalEpisodes += season.episodes
			seasonItems = append(seasonItems, map[string]any{
				"seasonNumber": season.number, "monitored": season.monitored,
				"statistics": map[string]any{"episodeCount": season.loaded, "episodeFileCount": season.files, "totalEpisodeCount": season.episodes},
			})
		}
		percent := 0.0
		if totalEpisodes > 0 {
			percent = float64(episodeFiles) / float64(totalEpisodes)
		}
		out = append(out, map[string]any{
			"id": id, "tmdbId": tmdbID, "title": title, "year": nullableInt(year), "monitored": monitored,
			"status": seriesStatus, "qualityProfileId": profile, "path": "", "folderName": title,
			"added": created, "seasons": seasonItems,
			"statistics": map[string]any{"seasonCount": len(seasons), "episodeCount": episodeCount, "episodeFileCount": episodeFiles, "totalEpisodeCount": totalEpisodes, "percentOfEpisodes": percent},
			"images":     compatImages(poster, title),
		})
	}
	if err = rows.Err(); err != nil {
		s.internal(w, "compat_series", err)
		return
	}
	send(w, 200, out)
}

func (s *Server) compatSeasons(ctx context.Context, seriesID int64) ([]compatSeason, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT s.id,s.number,s.monitored,s.episode_count,
		(SELECT COUNT(*) FROM episodes e WHERE e.season_id=s.id),
		(SELECT COUNT(*) FROM episodes e JOIN episode_files ef ON ef.episode_id=e.id WHERE e.season_id=s.id)
		FROM seasons s WHERE s.series_id=? ORDER BY s.number`, seriesID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []compatSeason{}
	for rows.Next() {
		var v compatSeason
		if err := rows.Scan(&v.id, &v.number, &v.monitored, &v.episodes, &v.loaded, &v.files); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Server) compatSeriesLookup(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	if s.metadata == nil || !s.metadata.SupportsSeries() {
		fail(w, 503, "metadata_unavailable", "TMDb series metadata is not configured.")
		return
	}
	term := strings.TrimSpace(r.URL.Query().Get("term"))
	if term == "" || len(term) > 200 {
		fail(w, 400, "invalid_term", "Provide a search term of at most 200 characters.")
		return
	}
	results, err := s.metadata.SearchSeries(ctx, term, 1)
	if err != nil {
		s.internal(w, "compat_series_lookup", err)
		return
	}
	out := []map[string]any{}
	for _, m := range results.Items {
		out = append(out, map[string]any{"tmdbId": m.TMDbID, "tvdbId": 0, "title": m.Title, "year": m.Year, "overview": m.Overview, "remotePoster": metadata.PosterURL(m.PosterPath)})
	}
	send(w, 200, out)
}

func (s *Server) compatEpisodes(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	seriesID, err := strconv.ParseInt(r.URL.Query().Get("seriesId"), 10, 64)
	if err != nil || seriesID < 1 {
		fail(w, 400, "invalid_series", "Provide a valid seriesId.")
		return
	}
	rows, err := s.db.QueryContext(ctx, `SELECT e.id,e.tmdb_id,e.number,e.title,e.overview,e.air_date,e.runtime_minutes,e.monitored,s.id,s.number,
		COALESCE((SELECT COUNT(*) FROM episode_files ef WHERE ef.episode_id=e.id),0)
		FROM episodes e JOIN seasons s ON s.id=e.season_id WHERE s.series_id=? ORDER BY s.number,e.number LIMIT 1000`, seriesID)
	if err != nil {
		s.internal(w, "compat_episodes", err)
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, tmdbID, seasonID int64
		var number, seasonNumber, files int
		var title, overview, air string
		var runtime sql.NullInt64
		var monitored bool
		if err = rows.Scan(&id, &tmdbID, &number, &title, &overview, &air, &runtime, &monitored, &seasonID, &seasonNumber, &files); err != nil {
			s.internal(w, "compat_episodes", err)
			return
		}
		out = append(out, map[string]any{
			"id": id, "seriesId": seriesID, "tvdbId": 0, "tmdbId": tmdbID, "seasonNumber": seasonNumber, "episodeNumber": number,
			"title": title, "overview": overview, "airDate": air, "runtime": nullableInt(runtime),
			"monitored": monitored, "hasFile": files > 0, "episodeFileId": 0,
		})
	}
	if err = rows.Err(); err != nil {
		s.internal(w, "compat_episodes", err)
		return
	}
	send(w, 200, out)
}

func (s *Server) compatCalendar(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	start := r.URL.Query().Get("start")
	end := r.URL.Query().Get("end")
	if len(start) != 10 || len(end) != 10 {
		now := time.Now()
		start = now.Format("2006-01-02")
		end = now.AddDate(0, 1, 0).Format("2006-01-02")
	}
	if _, err := time.Parse("2006-01-02", start); err != nil {
		fail(w, 400, "invalid_start", "Use a start date in YYYY-MM-DD form.")
		return
	}
	if _, err := time.Parse("2006-01-02", end); err != nil {
		fail(w, 400, "invalid_end", "Use an end date in YYYY-MM-DD form.")
		return
	}
	out := []map[string]any{}
	movieRows, err := s.db.QueryContext(ctx, `SELECT id,tmdb_id,title,release_date,monitored FROM media_items WHERE kind='movie' AND release_date BETWEEN ? AND ? ORDER BY release_date,id`, start, end)
	if err != nil {
		s.internal(w, "compat_calendar", err)
		return
	}
	for movieRows.Next() {
		var id, tmdbID int64
		var title, release string
		var monitored bool
		if err = movieRows.Scan(&id, &tmdbID, &title, &release, &monitored); err != nil {
			movieRows.Close()
			s.internal(w, "compat_calendar", err)
			return
		}
		out = append(out, map[string]any{"kind": "movie", "id": id, "tmdbId": tmdbID, "title": title, "date": release, "monitored": monitored})
	}
	movieRows.Close()
	episodeRows, err := s.db.QueryContext(ctx, `SELECT m.id,m.title,e.id,e.tmdb_id,s.number,e.number,e.title,e.air_date,e.monitored
		FROM episodes e JOIN seasons s ON s.id=e.season_id JOIN media_items m ON m.id=s.series_id
		WHERE e.air_date BETWEEN ? AND ? ORDER BY e.air_date,m.id,s.number,e.number`, start, end)
	if err != nil {
		s.internal(w, "compat_calendar", err)
		return
	}
	defer episodeRows.Close()
	for episodeRows.Next() {
		var seriesID, episodeID, tmdbID int64
		var seriesTitle, episodeTitle, air string
		var seasonNumber, episodeNumber int
		var monitored bool
		if err = episodeRows.Scan(&seriesID, &seriesTitle, &episodeID, &tmdbID, &seasonNumber, &episodeNumber, &episodeTitle, &air, &monitored); err != nil {
			s.internal(w, "compat_calendar", err)
			return
		}
		out = append(out, map[string]any{"kind": "episode", "id": episodeID, "seriesId": seriesID, "seriesTitle": seriesTitle, "tmdbId": tmdbID, "seasonNumber": seasonNumber, "episodeNumber": episodeNumber, "title": episodeTitle, "date": air, "monitored": monitored})
	}
	if err = episodeRows.Err(); err != nil {
		s.internal(w, "compat_calendar", err)
		return
	}
	send(w, 200, out)
}

func (s *Server) compatQueue(ctx context.Context, w http.ResponseWriter) {
	if s.transfer == nil {
		send(w, 200, []any{})
		return
	}
	page, err := s.transfer.List(ctx, 1)
	if err != nil {
		s.internal(w, "compat_queue", err)
		return
	}
	out := make([]map[string]any, 0, len(page.Items))
	for _, job := range page.Items {
		episodeID, seriesID := int64(0), int64(0)
		if job.Kind == "series" {
			seriesID = job.MovieID
			if len(job.Targets) > 0 {
				episodeID = job.Targets[0].EpisodeID
			}
		}
		out = append(out, map[string]any{
			"id": job.ID, "title": job.Title, "status": compatQueueStatus(job.State), "trackedDownloadStatus": "ok",
			"size": job.Total, "sizeleft": max64(job.Total-job.Downloaded, 0), "progress": job.Progress * 100,
			"protocol": job.Protocol, "downloadId": job.Hash, "movieId": job.MovieID, "seriesId": seriesID, "episodeId": episodeID,
		})
	}
	send(w, 200, out)
}

func compatQueueStatus(state string) string {
	switch state {
	case "queued", "submitting":
		return "queued"
	case "downloading":
		return "downloading"
	case "importing":
		return "importing"
	case "completed":
		return "completed"
	case "failed", "blocked":
		return "failed"
	case "cancelled":
		return "cancelled"
	default:
		return state
	}
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func (s *Server) compatHistory(ctx context.Context, w http.ResponseWriter) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,occurred_at,kind,object_id,title,state FROM activity_events ORDER BY occurred_at DESC,id DESC LIMIT 100`)
	if err != nil {
		s.internal(w, "compat_history", err)
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, occurred int64
		var kind, objectID, title, state string
		if err = rows.Scan(&id, &occurred, &kind, &objectID, &title, &state); err != nil {
			s.internal(w, "compat_history", err)
			return
		}
		out = append(out, map[string]any{
			"id": id, "eventType": kind, "date": time.Unix(occurred, 0).UTC().Format(time.RFC3339),
			"sourceTitle": title, "title": title, "state": state, "objectId": objectID,
		})
	}
	if err = rows.Err(); err != nil {
		s.internal(w, "compat_history", err)
		return
	}
	send(w, 200, out)
}

func (s *Server) compatIndexers(ctx context.Context, w http.ResponseWriter) {
	if s.transfer == nil {
		send(w, 200, []any{})
		return
	}
	items, err := s.transfer.Indexers(ctx)
	if err != nil {
		s.internal(w, "compat_indexers", err)
		return
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		protocol := "torrent"
		if item.Kind == "newznab" {
			protocol = "usenet"
		}
		out = append(out, map[string]any{
			"id": item.ID, "name": item.Name, "implementation": item.Kind, "enable": item.Enabled,
			"protocol": protocol, "sortName": item.Name, "fields": []any{},
		})
	}
	send(w, 200, out)
}

func (s *Server) compatCommand(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name       string  `json:"name"`
		MovieID    int64   `json:"movieId"`
		MovieIDs   []int64 `json:"movieIds"`
		SeriesID   int64   `json:"seriesId"`
		EpisodeID  int64   `json:"episodeId"`
		EpisodeIDs []int64 `json:"episodeIds"`
	}
	if !decodeCompatCommand(w, r, &body) {
		return
	}
	if body.MovieID < 1 && len(body.MovieIDs) > 0 {
		body.MovieID = body.MovieIDs[0]
	}
	if body.EpisodeID < 1 && len(body.EpisodeIDs) > 0 {
		body.EpisodeID = body.EpisodeIDs[0]
	}
	name := strings.ToLower(strings.TrimSpace(body.Name))
	// Commands act on a specific media kind, so an API key must hold the scope
	// for that kind even when it reached the route through a different prefix.
	commandScope := ""
	switch name {
	case "moviesearch", "movierefresh", "refreshmovie":
		commandScope = "compat-radarr"
	case "episodesearch", "refreshseries", "seriesrefresh":
		commandScope = "compat-sonarr"
	}
	if key, ok := auth.APIKeyFrom(r.Context()); ok && commandScope != "" && !key.HasScope(commandScope) {
		fail(w, 403, "insufficient_scope", "This API key does not have the "+commandScope+" scope.")
		return
	}
	if s.automation == nil {
		fail(w, 503, "automation_unavailable", "Automation is not available.")
		return
	}
	switch name {
	case "moviesearch", "movierefresh", "refreshmovie":
		if body.MovieID < 1 {
			fail(w, 400, "invalid_movie", "Provide a valid movieId.")
			return
		}
		if name == "refreshmovie" || name == "movierefresh" {
			if s.metadataRefresh == nil {
				fail(w, 503, "metadata_unavailable", "Metadata refresh is not available.")
				return
			}
			if err := s.metadataRefresh.Enqueue(ctx, body.MovieID, false); err != nil {
				s.internal(w, "compat_refresh_movie", err)
				return
			}
		} else if err := s.automation.RequestItem(ctx, body.MovieID, nil, nil); err != nil {
			fail(w, 409, "command_failed", err.Error())
			return
		}
	case "episodesearch", "refreshseries", "seriesrefresh":
		if body.SeriesID < 1 {
			fail(w, 400, "invalid_series", "Provide a valid seriesId.")
			return
		}
		if name == "refreshseries" || name == "seriesrefresh" {
			if s.metadataRefresh == nil {
				fail(w, 503, "metadata_unavailable", "Metadata refresh is not available.")
				return
			}
			if err := s.metadataRefresh.Enqueue(ctx, body.SeriesID, false); err != nil {
				s.internal(w, "compat_refresh_series", err)
				return
			}
		} else {
			if body.EpisodeID < 1 {
				fail(w, 400, "invalid_episode", "EpisodeSearch requires a valid episodeId.")
				return
			}
			if err := s.automation.RequestItem(ctx, body.SeriesID, &body.EpisodeID, nil); err != nil {
				fail(w, 409, "command_failed", err.Error())
				return
			}
		}
	case "rescanmovie", "rescanseries", "missingepisodesearch":
		fail(w, 501, "not_implemented", "This command is not implemented. Supported commands: MovieSearch, RefreshMovie, EpisodeSearch, RefreshSeries.")
		return
	default:
		fail(w, 501, "not_implemented", "Unsupported command. Supported commands: MovieSearch, RefreshMovie, EpisodeSearch, RefreshSeries.")
		return
	}
	send(w, 202, map[string]any{"name": body.Name, "status": "queued"})
}
