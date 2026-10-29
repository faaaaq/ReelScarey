package httpapi

import (
	"context"
	"encoding/json"
	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/library"
	"github.com/faaaaq/ReelScarey/internal/metadata"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"
)

type franchiseFixture struct{}

func (franchiseFixture) FindFranchise(_ context.Context, kind string, id int64) (metadata.FranchiseResults, error) {
	return metadata.FranchiseResults{Items: []metadata.FranchiseMember{{Kind: "series", TMDbID: 200, Title: "Related show", Franchise: "Fixture", FranchiseID: "Q1", WikidataID: "Q2"}, {Kind: "movie", TMDbID: 300, Title: "New film", Franchise: "Fixture", FranchiseID: "Q1", WikidataID: "Q3"}}}, nil
}
func TestFranchiseResultsBindToLocalMovieAndShow(t *testing.T) {
	db, e := database.Open(context.Background(), t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	if _, e = db.Exec(`INSERT INTO media_items(id,kind,tmdb_id,title,status) VALUES(1,'movie',100,'Seed','available'),(2,'series',200,'Related show','missing')`); e != nil {
		t.Fatal(e)
	}
	s := &Server{db: db, library: library.New(db), franchises: franchiseFixture{}, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	req := httptest.NewRequest("GET", "/api/v1/library/items/1/franchise", nil)
	req.SetPathValue("id", "1")
	out := httptest.NewRecorder()
	s.franchiseTitles(out, req)
	var body struct {
		Items []struct {
			Kind      string `json:"kind"`
			LibraryID int    `json:"library_id"`
			Status    string `json:"status"`
		} `json:"items"`
	}
	if e = json.Unmarshal(out.Body.Bytes(), &body); e != nil || out.Code != 200 || len(body.Items) != 2 || body.Items[0].LibraryID != 2 || body.Items[0].Status != "missing" || body.Items[1].LibraryID != 0 {
		t.Fatal(out.Code, body, e)
	}
}
