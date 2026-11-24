package httpapi

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestDashboardRecentAcrossCatalogs(t *testing.T) {
	h, db := fixture(t)
	load := func() []recentTitle {
		t.Helper()
		res := httptest.NewRecorder()
		h.ServeHTTP(res, httptest.NewRequest("GET", "/api/v1/dashboard", nil))
		var payload struct {
			Recent []recentTitle `json:"recent"`
		}
		if res.Code != 200 || json.Unmarshal(res.Body.Bytes(), &payload) != nil || payload.Recent == nil {
			t.Fatalf("dashboard: %d %s", res.Code, res.Body)
		}
		return payload.Recent
	}
	if len(load()) != 0 {
		t.Fatal("expected empty shelf")
	}
	_, err := db.Exec(`
        INSERT INTO music_artists(id,musicbrainz_id,name,created_at,updated_at) VALUES (1,'artist','Artist',1,1);
        INSERT INTO music_albums(id,artist_id,musicbrainz_id,title,created_at,updated_at) VALUES (1,1,'album','Album',86400,86400);
        INSERT INTO media_items(id,kind,tmdb_id,title,created_at,poster_path) VALUES
        (1,'series',1,'Newest','1970-01-02T00:00:00.500Z','/poster.jpg'),
        (2,'movie',2,'A','1970-01-01T00:00:01.000Z',''),(3,'movie',3,'B','1970-01-01T00:00:01.000Z',''),
        (4,'movie',4,'C','1970-01-01T00:00:01.000Z',''),(5,'movie',5,'D','1970-01-01T00:00:01.000Z','');
    `)
	if err != nil {
		t.Fatal(err)
	}
	got := load()
	if len(got) != 5 || got[0].Title != "Newest" || got[0].Artwork == "" || got[1].Artwork != "/api/v1/library/music/albums/1/cover" || got[4].ID != 3 {
		t.Fatalf("unexpected recent titles: %+v", got)
	}
}
