package requests

import (
	"context"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/database"
)

func TestIMDbCSVAndAtomicResolvedImport(t *testing.T) {
	ids, err := IMDbIDs("\ufeffConst,Title\ntt0133093,The Matrix\ntt0133093,Duplicate\ntt0903747,Breaking Bad\n")
	if err != nil || len(ids) != 2 || ids[0] != "tt0133093" {
		t.Fatal(ids, err)
	}
	for _, body := range []string{"Title\nFoo\n", "Const\nnm1234567\n", "Const\ntt123456\nmalformed\n"} {
		if _, err = IMDbIDs(body); err == nil {
			t.Fatal("invalid CSV accepted", body)
		}
	}
	ctx := context.Background()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec(`INSERT INTO request_accounts(id,username,password_hash,created_at) VALUES(1,'viewer','hash',1)`); err != nil {
		t.Fatal(err)
	}
	s := New(db)
	items := []Request{{Kind: "movie", TMDbID: 603, Title: "The Matrix"}, {Kind: "series", TMDbID: 1396, Title: "Breaking Bad"}}
	if n, err := s.ImportResolved(ctx, 1, items); err != nil || n != 2 {
		t.Fatal(n, err)
	}
	if n, err := s.ImportResolved(ctx, 1, items); err != nil || n != 0 {
		t.Fatal("duplicate import not idempotent", n, err)
	}
	items = append(items, Request{Kind: "movie", TMDbID: 0, Title: "Bad"})
	if _, err = s.ImportResolved(ctx, 1, items); err == nil {
		t.Fatal("invalid batch accepted")
	}
	rows, err := s.List(ctx, 1)
	if err != nil || len(rows) != 2 {
		t.Fatal("invalid batch changed requests", rows, err)
	}
}
