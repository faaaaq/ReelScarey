package httpapi

import (
	"context"
	"encoding/json"
	"github.com/faaaaq/ReelScarey/internal/database"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"
)

func TestEventTimelineRecordsChangesAndPaginates(t *testing.T) {
	db, e := database.Open(context.Background(), t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	if _, e = db.Exec(`INSERT INTO media_items(id,kind,tmdb_id,title,status) VALUES(1,'movie',1,'Fixture','missing')`); e != nil {
		t.Fatal(e)
	}
	var count int
	if e = db.QueryRow(`SELECT COUNT(*) FROM activity_events WHERE kind='title' AND title='Fixture' AND state='added'`).Scan(&count); e != nil || count != 1 {
		t.Fatal(count, e)
	}
	for i := 0; i < 55; i++ {
		if _, e = db.Exec(`INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'fixture','1','Owned','queued')`); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = db.Exec(`INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(1,'fixture','old','Older backfill','historical')`); e != nil {
		t.Fatal(e)
	}
	s := &Server{db: db, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	res := httptest.NewRecorder()
	s.activityEvents(res, httptest.NewRequest("GET", "/api/v1/events", nil))
	var page struct {
		Items []activityEvent `json:"items"`
		Next  *string         `json:"next"`
	}
	if e = json.Unmarshal(res.Body.Bytes(), &page); e != nil || res.Code != 200 || len(page.Items) != 50 || page.Next == nil {
		t.Fatal(res.Code, page, e)
	}
	next := httptest.NewRecorder()
	s.activityEvents(next, httptest.NewRequest("GET", "/api/v1/events?before="+*page.Next, nil))
	var older struct {
		Items []activityEvent `json:"items"`
	}
	if e = json.Unmarshal(next.Body.Bytes(), &older); e != nil || len(older.Items) != 7 || older.Items[0].ID >= page.Items[len(page.Items)-1].ID || older.Items[len(older.Items)-1].Title != "Older backfill" {
		t.Fatal(next.Code, older, e)
	}
}
