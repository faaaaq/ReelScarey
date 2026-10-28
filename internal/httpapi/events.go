package httpapi

import (
	"net/http"
	"strconv"
	"strings"
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
	rows, e := s.db.QueryContext(r.Context(), `SELECT id,occurred_at,kind,object_id,title,state FROM activity_events WHERE (?=0 OR occurred_at<? OR (occurred_at=? AND id<?)) ORDER BY occurred_at DESC,id DESC LIMIT 51`, beforeTime, beforeTime, beforeTime, beforeID)
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
	send(w, 200, map[string]any{"items": items, "next": next})
}
