package httpapi

import (
	"net/http"
	"strconv"
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
	before := int64(0)
	if value := r.URL.Query().Get("before"); value != "" {
		n, e := strconv.ParseInt(value, 10, 64)
		if e != nil || n < 1 {
			fail(w, 400, "invalid_cursor", "Choose a valid event cursor.")
			return
		}
		before = n
	}
	rows, e := s.db.QueryContext(r.Context(), `SELECT id,occurred_at,kind,object_id,title,state FROM activity_events WHERE (?=0 OR id<?) ORDER BY id DESC LIMIT 51`, before, before)
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
	var next *int64
	if len(items) > 50 {
		items = items[:50]
		id := items[49].ID
		next = &id
	}
	send(w, 200, map[string]any{"items": items, "next": next})
}
