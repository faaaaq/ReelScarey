package organization

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestMoveRollbackRestoresPathsAndRejectsStaleReview(t *testing.T) {
	for _, folder := range []bool{false, true} {
		t.Run(map[bool]string{false: "files", true: "folder"}[folder], func(t *testing.T) {
			ctx := context.Background()
			f := newReviewFixture(t)
			original := "Movies/Film/original.mkv"
			f.movie(t, 1, original, []byte("original video"))
			base := filepath.Join(f.cfg.MediaDir, "Movies", "Film")
			if err := os.WriteFile(filepath.Join(base, "original.en.srt"), []byte("subtitle"), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := f.db.Exec(`INSERT INTO subtitles(media_file_id,language,variant,source,relative_path,sha256,provider_file_id,updated_at) VALUES(1,'en','full','existing','Movies/Film/original.en.srt','x',0,1)`); err != nil {
				t.Fatal(err)
			}
			seed := filepath.Join(f.cfg.DownloadsDir, "seed.mkv")
			if err := os.Link(filepath.Join(base, "original.mkv"), seed); err != nil {
				t.Fatal(err)
			}
			var p Plan
			var err error
			if folder {
				if err = os.MkdirAll(filepath.Join(base, "extras", "empty"), 0700); err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(filepath.Join(base, "extras", "art.jpg"), []byte("art"), 0600); err != nil {
					t.Fatal(err)
				}
				p, err = f.s.PreviewFolder(ctx, 1, "anime")
			} else {
				p, err = f.s.Preview(ctx, []int64{1}, "anime")
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err = f.s.Apply(ctx, p.ID); err != nil {
				t.Fatal(err)
			}
			if err = f.s.Tick(ctx); err != nil {
				t.Fatal(err)
			}
			restore, err := f.s.PreviewRollback(ctx, p.ID)
			if err != nil {
				t.Fatal(err)
			}
			if restore.RestoresPlan != p.ID || restore.Files[0].Destination != original {
				t.Fatal(restore)
			}
			// Occupying a reviewed path must not authorize replacement.
			if err = os.MkdirAll(base, 0700); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(base, "original.mkv"), []byte("foreign"), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err = f.s.Apply(ctx, restore.ID); err == nil {
				t.Fatal("occupied destination accepted")
			}
			if err = os.Remove(filepath.Join(base, "original.mkv")); err != nil {
				t.Fatal(err)
			}
			if folder {
				if err = os.Remove(base); err != nil {
					t.Fatal(err)
				}
			}
			if _, err = f.s.Apply(ctx, restore.ID); err != nil {
				t.Fatal(err)
			}
			// Recreate the service to exercise durable queue recovery.
			if err = New(f.db, f.cfg).Tick(ctx); err != nil {
				t.Fatal(err)
			}
			for name, want := range map[string]string{filepath.Join(base, "original.mkv"): "original video", filepath.Join(base, "original.en.srt"): "subtitle", seed: "original video"} {
				b, err := os.ReadFile(name)
				if err != nil || string(b) != want {
					t.Fatal(name, string(b), err)
				}
			}
			var path, sub, assignment string
			if err = f.db.QueryRow(`SELECT relative_path FROM media_files WHERE id=1`).Scan(&path); err != nil || path != original {
				t.Fatal(path, err)
			}
			if err = f.db.QueryRow(`SELECT relative_path FROM subtitles WHERE media_file_id=1`).Scan(&sub); err != nil || sub != "Movies/Film/original.en.srt" {
				t.Fatal(sub, err)
			}
			if err = f.db.QueryRow(`SELECT library_key FROM media_organization WHERE media_id=1`).Scan(&assignment); err != nil || assignment != "anime" {
				t.Fatal("future preferences changed", assignment, err)
			}
			if folder {
				if _, err = os.Stat(filepath.Join(base, "extras", "empty")); err != nil {
					t.Fatal(err)
				}
			}
			if _, err = f.s.PreviewRollback(ctx, p.ID); err == nil {
				t.Fatal("stale original move accepted twice")
			}
		})
	}
}
