package organization

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMovieVersionsOrganizationKeepsSeparateFilesAndSidecars(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	f.movie(t, 1, "Movies/Film/film-hd.mkv", []byte("HD video"))
	f.movie(t, 2, "Movies/Film/film-4k.mkv", []byte("4K video"))
	if _, err := f.db.Exec(`UPDATE media_files SET movie_id=1,probe_json=CASE id WHEN 1 THEN '{"width":1920,"height":800}' ELSE '{"width":3840,"height":1600}' END`); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"film-hd", "film-4k"} {
		if err := os.WriteFile(filepath.Join(f.cfg.MediaDir, "Movies", "Film", name+".en.srt"), []byte(name+" subtitle"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	p, err := f.s.Preview(ctx, []int64{1, 2}, "")
	if err != nil {
		t.Fatal(err)
	}
	if p.Files[0].Destination == p.Files[1].Destination || !strings.Contains(p.Files[0].Destination, "1080p") || !strings.Contains(p.Files[1].Destination, "2160p") {
		t.Fatal(p.Files)
	}
	appliedID := p.ID
	if _, err = f.s.Apply(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if err = f.s.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	for _, v := range p.Files {
		want := "HD video"
		sidecar := "film-hd subtitle"
		if v.FileID == 2 {
			want = "4K video"
			sidecar = "film-4k subtitle"
		}
		name := filepath.Join(f.cfg.MediaDir, filepath.FromSlash(v.Destination))
		if body, e := os.ReadFile(name); e != nil || string(body) != want {
			t.Fatal(name, string(body), e)
		}
		if body, e := os.ReadFile(strings.TrimSuffix(name, filepath.Ext(name)) + ".en.srt"); e != nil || string(body) != sidecar {
			t.Fatal("sidecar crossed versions", string(body), e)
		}
	}
	p, err = f.s.Preview(ctx, []int64{1, 2}, "")
	if err != nil || !p.Files[0].Unchanged || !p.Files[1].Unchanged {
		t.Fatal("unstable version names", p, err)
	}
	rollback, err := f.s.PreviewRollback(ctx, appliedID)
	if err != nil {
		t.Fatal("multi-file rollback preview failed", err)
	}
	if len(rollback.Files) != 2 {
		t.Fatal("rollback did not retain both versions", rollback.Files)
	}
	if _, err = f.s.Apply(ctx, rollback.ID); err != nil {
		t.Fatal(err)
	}
	if err = f.s.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	for _, v := range rollback.Files {
		want := "HD video"
		if v.FileID == 2 {
			want = "4K video"
		}
		body, e := os.ReadFile(filepath.Join(f.cfg.MediaDir, filepath.FromSlash(v.Destination)))
		if e != nil || string(body) != want {
			t.Fatal("rollback did not restore version", v.Destination, string(body), e)
		}
	}
}

func TestMovieVersionsReplacingAndRollingBackHDLeaves4KUntouched(t *testing.T) {
	f, _ := replacementFixture(t)
	ctx := context.Background()
	f.movie(t, 2, "Movies/Film/4K.mkv", []byte("retained 4K version"))
	if _, err := f.db.Exec(`UPDATE media_files SET movie_id=1 WHERE id=2`); err != nil {
		t.Fatal(err)
	}
	p, err := f.s.PreviewReplacement(ctx, 1, "candidate")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.Apply(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if err = f.s.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	check := func() {
		t.Helper()
		body, e := os.ReadFile(filepath.Join(f.cfg.MediaDir, "Movies", "Film", "4K.mkv"))
		if e != nil || string(body) != "retained 4K version" {
			t.Fatal("other version changed", e)
		}
		var n int
		if e = f.db.QueryRow(`SELECT COUNT(*) FROM media_files WHERE movie_id=1`).Scan(&n); e != nil || n != 2 {
			t.Fatal(n, e)
		}
	}
	check()
	undo, err := f.s.PreviewRollback(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.Apply(ctx, undo.ID); err != nil {
		t.Fatal(err)
	}
	if err = f.s.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	check()
}
