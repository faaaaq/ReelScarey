package organization

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestReviewedRemovalKeepsSourcesAndRefusesChangedHistory(t *testing.T) {
	for _, mode := range []string{"keep", "trash"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			f := newReviewFixture(t)
			f.movie(t, 1, "Movies/Film/film.mkv", []byte("video"))
			f.movie(t, 2, "Movies/Other/other.mkv", []byte("unrelated"))
			seed := filepath.Join(f.cfg.DownloadsDir, "seed.mkv")
			source := filepath.Join(f.cfg.MediaDir, "Movies", "Film", "film.mkv")
			if err := os.Link(source, seed); err != nil {
				t.Fatal(err)
			}
			if _, err := f.db.Exec(`INSERT INTO subtitles(media_file_id,language,variant,source,relative_path,sha256,provider_file_id,updated_at) VALUES(1,'en','full','embedded','','x',0,1)`); err != nil {
				t.Fatal(err)
			}
			p, err := f.s.PreviewRemoval(ctx, 1, mode)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = f.db.Exec(`UPDATE media_items SET monitored=0 WHERE id=1`); err != nil {
				t.Fatal(err)
			}
			if _, err = f.s.Apply(ctx, p.ID); err == nil {
				t.Fatal("stale catalog accepted")
			}
			p, err = f.s.PreviewRemoval(ctx, 1, mode)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = f.s.Apply(ctx, p.ID); err != nil {
				t.Fatal(err)
			}
			if err = New(f.db, f.cfg).Tick(ctx); err != nil {
				t.Fatal(err)
			}
			var count int
			if err = f.db.QueryRow(`SELECT COUNT(*) FROM media_items WHERE id=1`).Scan(&count); err != nil || count != 0 {
				t.Fatal(count, err)
			}
			if err = f.db.QueryRow(`SELECT COUNT(*) FROM media_items WHERE id=2`).Scan(&count); err != nil || count != 1 {
				t.Fatal("unrelated catalog removed", count, err)
			}
			for _, name := range []string{seed, filepath.Join(f.cfg.MediaDir, "Movies", "Other", "other.mkv")} {
				if _, err = os.Stat(name); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "keep" {
				if _, err = os.Stat(source); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err = os.Stat(source); !os.IsNotExist(err) {
					t.Fatal("source remained", err)
				}
				if b, e := os.ReadFile(filepath.Join(f.cfg.MediaDir, filepath.FromSlash(p.Files[0].Destination))); e != nil || string(b) != "video" {
					t.Fatal("trash not retained", e)
				}
			}
		})
	}
}
