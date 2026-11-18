package httpapi

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type activityEvent struct {
	ID       int64  `json:"id"`
	Time     int64  `json:"occurred_at"`
	Kind     string `json:"kind"`
	ObjectID string `json:"object_id"`
	Title    string `json:"title"`
	State    string `json:"state"`
}

func (s *Server) activityEvents(w http.ResponseWriter, r *http.Request) {
	beforeTime, beforeID := int64(0), int64(0)
	if value := r.URL.Query().Get("before"); value != "" {
		parts := strings.Split(value, ":")
		if len(parts) != 2 {
			fail(w, 400, "invalid_cursor", "Choose a valid event cursor.")
			return
		}
		var e1, e2 error
		beforeTime, e1 = strconv.ParseInt(parts[0], 10, 64)
		beforeID, e2 = strconv.ParseInt(parts[1], 10, 64)
		if e1 != nil || e2 != nil || beforeTime < 1 || beforeID < 1 {
			fail(w, 400, "invalid_cursor", "Choose a valid event cursor.")
			return
		}
	}
	mediaID := int64(0)
	if raw := strings.TrimSpace(r.URL.Query().Get("media_id")); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value < 1 {
			fail(w, 400, "invalid_media", "Choose a valid library item.")
			return
		}
		mediaID = value
	}
	condition := `(?=0 OR occurred_at<? OR (occurred_at=? AND id<?))`
	args := []any{beforeTime, beforeTime, beforeTime, beforeID}
	if mediaID > 0 {
		condition += ` AND (
			(kind IN ('title','playback') AND object_id=?) OR
			(kind='download' AND EXISTS(SELECT 1 FROM downloads d WHERE d.id=activity_events.object_id AND d.movie_id=?)) OR
			(kind='youtube' AND EXISTS(SELECT 1 FROM youtube_jobs y WHERE y.id=activity_events.object_id AND y.media_id=?)) OR
			(kind='organization' AND EXISTS(SELECT 1 FROM organization_plans p WHERE p.id=activity_events.object_id AND (json_extract(p.snapshot,'$.replacement.media_id')=? OR json_extract(p.snapshot,'$.media_id')=?))) OR
			(kind='subtitle' AND (EXISTS(SELECT 1 FROM media_files f WHERE f.id=CAST(activity_events.object_id AS INTEGER) AND COALESCE(f.movie_id,f.series_id)=?) OR EXISTS(SELECT 1 FROM subtitle_jobs j JOIN media_files mf ON mf.id=j.media_file_id WHERE j.id=CAST(activity_events.object_id AS INTEGER) AND COALESCE(mf.movie_id,mf.series_id)=?)))
		)`
		args = append(args, strconv.FormatInt(mediaID, 10), mediaID, mediaID, mediaID, mediaID, mediaID, mediaID)
	}
	rows, e := s.db.QueryContext(r.Context(), `SELECT id,occurred_at,kind,object_id,title,state FROM activity_events WHERE `+condition+` ORDER BY occurred_at DESC,id DESC LIMIT 51`, args...)
	if e != nil {
		s.internal(w, "events", e)
		return
	}
	defer rows.Close()
	items := []activityEvent{}
	for rows.Next() {
		var v activityEvent
		if e = rows.Scan(&v.ID, &v.Time, &v.Kind, &v.ObjectID, &v.Title, &v.State); e != nil {
			s.internal(w, "events", e)
			return
		}
		items = append(items, v)
	}
	if e = rows.Err(); e != nil {
		s.internal(w, "events", e)
		return
	}
	var next *string
	if len(items) > 50 {
		items = items[:50]
		cursor := strconv.FormatInt(items[49].Time, 10) + ":" + strconv.FormatInt(items[49].ID, 10)
		next = &cursor
	}
	response := map[string]any{"items": items, "next": next}
	if mediaID > 0 {
		candidates := []map[string]any{}
		rows, err := s.db.QueryContext(r.Context(), `SELECT id,title,scope,published_at,expires_at,(SELECT d.id FROM downloads d WHERE d.release_id=release_candidates.id ORDER BY d.created_at DESC LIMIT 1) FROM release_candidates WHERE movie_id=? ORDER BY published_at DESC,id LIMIT 50`, mediaID)
		if err != nil {
			s.internal(w, "release_candidates", err)
			return
		}
		defer rows.Close()
		for rows.Next() {
			var id, title, scope, published string
			var expires int64
			var downloadID sql.NullString
			if err := rows.Scan(&id, &title, &scope, &published, &expires, &downloadID); err != nil {
				s.internal(w, "release_candidates", err)
				return
			}
			state := "available candidate"
			if downloadID.Valid {
				state = "selected for download"
			} else if expires > 0 && expires < time.Now().Unix() {
				state = "expired candidate"
			}
			candidate := map[string]any{"id": id, "title": title, "scope": scope, "published_at": published, "state": state}
			if downloadID.Valid {
				candidate["download_id"] = downloadID.String
			}
			candidates = append(candidates, candidate)
		}
		if err := rows.Err(); err != nil {
			s.internal(w, "release_candidates", err)
			return
		}
		response["candidates"] = candidates
	}
	send(w, 200, response)
}
