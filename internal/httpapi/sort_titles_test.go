package httpapi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClearEpisodeSortTitlesAPI(t *testing.T) {
	f := newOrganizationReviewFixture(t)
	if _, err := f.db.Exec(`INSERT INTO media_items(id,kind,tmdb_id,title,year) VALUES(10,'series',1010,'API Show',2024)`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`INSERT INTO series_details(media_id) VALUES(10)`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`INSERT INTO seasons(id,series_id,tmdb_id,number,title,overview,air_date,episode_count,loaded_at) VALUES(20,10,2020,1,'Season 1','','',1,'loaded')`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`INSERT INTO episodes(id,season_id,tmdb_id,number,title,overview,air_date) VALUES(30,20,3030,1,'Pilot','','')`); err != nil {
		t.Fatal(err)
	}
	relative := "TV/API Show/Season 1/API Show.S01E01.mkv"
	media := filepath.Join(f.cfg.MediaDir, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(media), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(media, []byte("owned episode"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`INSERT INTO media_files(id,series_id,relative_path,size_bytes,sha256,probe_json,imported_at,origin) VALUES(40,10,?,13,'episode-sha','{}',1,'existing')`, relative); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`INSERT INTO episode_files(episode_id,media_file_id) VALUES(30,40)`); err != nil {
		t.Fatal(err)
	}
	target := strings.TrimSuffix(media, filepath.Ext(media)) + ".nfo"
	input := `<episodedetails><title>Pilot</title><sorttitle>provider order</sorttitle><plot>Keep me</plot></episodedetails>`
	if err := os.WriteFile(target, []byte(input), 0o600); err != nil {
		t.Fatal(err)
	}
	endpoint := "/api/v1/library/episodes/sort-titles/clear"
	if out := request(f.handler, "POST", endpoint, `{}`, nil, false); out.Code != 401 {
		t.Fatalf("anonymous response = %d %s", out.Code, out.Body.String())
	}
	if out := request(f.handler, "POST", endpoint, `{}`, &f.session, false); out.Code != 403 {
		t.Fatalf("missing CSRF response = %d %s", out.Code, out.Body.String())
	}
	if out := request(f.handler, "POST", endpoint, `{"after_id":-1}`, &f.session, true); out.Code != 400 {
		t.Fatalf("negative cursor response = %d %s", out.Code, out.Body.String())
	}
	out := request(f.handler, "POST", endpoint, `{}`, &f.session, true)
	if out.Code != 200 {
		t.Fatalf("cleanup response = %d %s", out.Code, out.Body.String())
	}
	var report struct {
		Scanned     int   `json:"scanned"`
		Changed     int   `json:"changed"`
		Failed      int   `json:"failed"`
		Done        bool  `json:"done"`
		NextAfterID int64 `json:"next_after_id"`
	}
	if err := json.Unmarshal(out.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Scanned != 1 || report.Changed != 1 || report.Failed != 0 || !report.Done || report.NextAfterID != 40 {
		t.Fatalf("unexpected report: %+v", report)
	}
	raw, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	want := `<episodedetails><title>Pilot</title><sorttitle></sorttitle><plot>Keep me</plot></episodedetails>`
	if string(raw) != want {
		t.Fatalf("cleaned NFO = %s, want %s", raw, want)
	}
}
