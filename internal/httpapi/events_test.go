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

func TestMediaActivityIncludesOnlyRelatedEventsAndCandidateDecisions(t *testing.T) {
	db, err := database.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec(`INSERT INTO media_items(id,kind,tmdb_id,title,status) VALUES(1,'movie',1,'Owned fixture','missing'),(2,'movie',2,'Other title','missing');
		INSERT INTO release_candidates(id,movie_id,title,size_bytes,published_at,payload,expires_at) VALUES('cand1',1,'Owned release',100,'2026-01-01T00:00:00Z',X'00',0),('cand2',2,'Other release',100,'2026-01-01T00:00:00Z',X'00',0);
		INSERT INTO downloads(id,movie_id,release_id,title,info_hash,payload,client_url,save_path,state,created_at,updated_at,kind) VALUES('job1',1,'cand1','Owned release','hash',X'01','https://unused.invalid','/unused','failed',1,1,'movie'),('job2',2,'cand2','Other release','hash2',X'02','https://unused.invalid','/unused','failed',1,1,'movie');
		INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(1,'download','job1','Owned release','verification failed'),(2,'download','job2','Other release','verification failed'),(3,'title','1','Owned fixture','metadata refreshed')`); err != nil {
		t.Fatal(err)
	}
	s := &Server{db: db, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	out := httptest.NewRecorder()
	s.activityEvents(out, httptest.NewRequest("GET", "/api/v1/events?media_id=1", nil))
	var page struct {
		Items      []activityEvent `json:"items"`
		Candidates []struct {
			ID         string `json:"id"`
			State      string `json:"state"`
			DownloadID string `json:"download_id"`
		} `json:"candidates"`
	}
	if err = json.Unmarshal(out.Body.Bytes(), &page); err != nil || out.Code != 200 {
		t.Fatal(out.Code, out.Body.String(), err)
	}
	if len(page.Items) < 2 || len(page.Candidates) != 1 || page.Candidates[0].ID != "cand1" || page.Candidates[0].State != "selected for download" || page.Candidates[0].DownloadID != "job1" {
		t.Fatalf("unexpected media activity: %#v", page)
	}
	for _, item := range page.Items {
		if item.Title == "Other release" {
			t.Fatal("unrelated title activity leaked", page.Items)
		}
	}
}

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
