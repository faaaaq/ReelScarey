package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestStorageInsightsReportsBelowCutoffWithoutChangingFiles(t *testing.T) {
	_, db := fixture(t)
	if _, err := db.Exec(`INSERT INTO media_items(kind,tmdb_id,title,year,monitored) VALUES('movie',900001,'Insight fixture',2020,1)`); err != nil {
		t.Fatal(err)
	}
	var mediaID int64
	if err := db.QueryRow(`SELECT id FROM media_items WHERE tmdb_id=900001`).Scan(&mediaID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO media_files(movie_id,relative_path,size_bytes,sha256,probe_json,imported_at,analysis_json,origin) VALUES(?,?,?,?,?,?,?,?)`,
		mediaID, "Movies/Insight/insight.mkv", 1024, "fixture-hash", "{}", 1, `{"width":1280,"height":720}`, "existing"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO item_automation(media_id,profile_id,revision) VALUES(?,1,1)`, mediaID); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: db, logger: slog.New(slog.NewTextHandler(os.Stdout, nil))}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/storage/insights", nil)
	recorder := httptest.NewRecorder()
	server.storageInsightsHandler(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status: %d %s", recorder.Code, recorder.Body)
	}
	var body struct {
		Summary struct {
			RegisteredFiles int `json:"registered_files"`
			BelowCutoff     int `json:"below_cutoff"`
		} `json:"summary"`
		Below []map[string]any `json:"below_cutoff"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Summary.RegisteredFiles != 1 || body.Summary.BelowCutoff != 1 || len(body.Below) != 1 {
		t.Fatalf("unexpected insights: %s", recorder.Body)
	}
}
