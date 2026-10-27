//go:build linux

package organization

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFolderMoveIncludesExtrasAndRecoversAfterRename(t *testing.T) {
	ctx := context.Background()
	f := newReviewFixture(t)
	f.movie(t, 1, "Movies/Film/film.mkv", []byte("video"))
	base := filepath.Join(f.cfg.MediaDir, "Movies", "Film")
	if err := os.MkdirAll(filepath.Join(base, "extras", "empty"), 0700); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{"film.en.srt": "subtitle", "poster.jpg": "art", "extras/interview.txt": "extra"} {
		if err := os.WriteFile(filepath.Join(base, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.db.Exec(`INSERT INTO subtitles(media_file_id,language,variant,source,relative_path,sha256,provider_file_id,updated_at) VALUES(1,'en','full','existing','Movies/Film/film.en.srt','x',0,1)`); err != nil {
		t.Fatal(err)
	}
	seeding := filepath.Join(f.cfg.DownloadsDir, "seed.mkv")
	if err := os.Link(filepath.Join(base, "film.mkv"), seeding); err != nil {
		t.Fatal(err)
	}
	p, err := f.s.PreviewFolder(ctx, 1, "anime")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.FolderMove.Inventory.Files) != 4 || len(p.FolderMove.Inventory.Directories) != 3 {
		t.Fatal("missing extras", p.FolderMove.Inventory)
	}
	if _, err = f.s.Apply(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	root, _, err := f.s.openRoot(p.Root)
	if err != nil {
		t.Fatal(err)
	}
	if err = renameFolderNoReplace(root, p.FolderMove.Source, p.FolderMove.Destination); err != nil {
		root.Close()
		t.Fatal(err)
	}
	root.Close()
	if err = New(f.db, f.cfg).Tick(ctx); err != nil {
		t.Fatal("failed to recover after folder rename", err)
	}
	for name, data := range map[string]string{"film.mkv": "video", "film.en.srt": "subtitle", "poster.jpg": "art", "extras/interview.txt": "extra"} {
		b, e := os.ReadFile(filepath.Join(f.cfg.MediaDir, filepath.FromSlash(p.FolderMove.Destination), name))
		if e != nil || string(b) != data {
			t.Fatalf("missing %s: %v", name, e)
		}
	}
	if info, e := os.Stat(filepath.Join(f.cfg.MediaDir, filepath.FromSlash(p.FolderMove.Destination), "extras", "empty")); e != nil || !info.IsDir() {
		t.Fatal("empty folder missing", e)
	}
	if b, e := os.ReadFile(seeding); e != nil || string(b) != "video" {
		t.Fatal("seeding link changed", e)
	}
	var video, sub string
	if err = f.db.QueryRow(`SELECT relative_path FROM media_files WHERE id=1`).Scan(&video); err != nil || video != p.Files[0].Destination {
		t.Fatal("video path not updated", video, err)
	}
	if err = f.db.QueryRow(`SELECT relative_path FROM subtitles WHERE media_file_id=1`).Scan(&sub); err != nil || sub != p.Files[0].Companions[0].Destination {
		t.Fatal("subtitle path not updated", sub, err)
	}
	finished, err := f.s.Plan(ctx, p.ID)
	if err != nil || finished.State != "completed" {
		t.Fatal("not completed", finished, err)
	}
}

func TestFolderMoveRejectsChangesAndDestinationConflict(t *testing.T) {
	ctx := context.Background()
	f := newReviewFixture(t)
	f.movie(t, 1, "Movies/Film/film.mkv", []byte("video"))
	p, err := f.s.PreviewFolder(ctx, 1, "anime")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(f.cfg.MediaDir, "Movies", "Film", "new.nfo"), []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.Apply(ctx, p.ID); err == nil {
		t.Fatal("changed inventory accepted")
	}
	if err = os.MkdirAll(filepath.Join(f.cfg.MediaDir, "Anime", "Movies", "Film"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.PreviewFolder(ctx, 1, "anime"); err == nil {
		t.Fatal("existing destination accepted")
	}
	if _, err = os.Stat(filepath.Join(f.cfg.MediaDir, "Movies", "Film", "film.mkv")); err != nil {
		t.Fatal("original was modified", err)
	}
}

func TestFolderMoveKeepsMediaFencedAfterUncertainRename(t *testing.T) {
	ctx := context.Background()
	f := newReviewFixture(t)
	f.movie(t, 1, "Movies/Film/film.mkv", []byte("video"))
	p, err := f.s.PreviewFolder(ctx, 1, "anime")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.Apply(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	root, _, err := f.s.openRoot(p.Root)
	if err != nil {
		t.Fatal(err)
	}
	if err = renameFolderNoReplace(root, p.FolderMove.Source, p.FolderMove.Destination); err != nil {
		root.Close()
		t.Fatal(err)
	}
	root.Close()
	extra := filepath.Join(f.cfg.MediaDir, filepath.FromSlash(p.FolderMove.Destination), "unexpected.nfo")
	if err = os.WriteFile(extra, []byte("external change"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = f.s.Tick(ctx); err == nil {
		t.Fatal("changed destination accepted")
	}
	if err = EnsureMediaAvailable(ctx, f.db); err == nil {
		t.Fatal("unresolved moved folder released media work")
	}
	if err = os.Remove(extra); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.Apply(ctx, p.ID); err != nil {
		t.Fatal("could not resume verified folder", err)
	}
	if err = f.s.Tick(ctx); err != nil {
		t.Fatal("could not finish recovery", err)
	}
	if err = EnsureMediaAvailable(ctx, f.db); err != nil {
		t.Fatal("finished move still fenced", err)
	}
}

func TestFolderMoveReleasesFenceWhenStoppedBeforeRename(t *testing.T) {
	ctx := context.Background()
	f := newReviewFixture(t)
	f.movie(t, 1, "Movies/Film/film.mkv", []byte("video"))
	p, err := f.s.PreviewFolder(ctx, 1, "anime")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.Apply(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(f.cfg.MediaDir, "Movies", "Film", "new.nfo"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = f.s.Tick(ctx); err == nil {
		t.Fatal("changed source accepted")
	}
	if err = EnsureMediaAvailable(ctx, f.db); err != nil {
		t.Fatal("safe pre-rename failure kept media fenced", err)
	}
}
