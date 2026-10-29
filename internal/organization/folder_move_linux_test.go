//go:build linux

package organization

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/config"
	"github.com/faaaaq/ReelScarey/internal/database"
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

func TestFolderMoveQueuesVerifiedTitleUpdate(t *testing.T) {
	ctx := context.Background()
	f := newReviewFixture(t)
	f.movie(t, 1, "Movies/Film/film.mkv", []byte("video"))
	if err := os.WriteFile(filepath.Join(f.cfg.MediaDir, "Movies", "Film", "poster.jpg"), []byte("art"), 0600); err != nil {
		t.Fatal(err)
	}
	f.s.SetTagger(func(ctx context.Context, source, stage *os.File, extension, title string) (string, error) {
		_, err := stage.WriteAt([]byte("tagged: "+title), 0)
		return `{}`, err
	})
	p, err := f.s.PreviewFolderWithEmbeddedTitle(ctx, 1, "anime")
	if err != nil || p.Files[0].EmbeddedTitle != "Review movie" {
		t.Fatal(p, err)
	}
	if _, err = f.s.Apply(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if err = f.s.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if err = f.s.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(f.cfg.MediaDir, filepath.FromSlash(p.Files[0].Destination)))
	if err != nil || string(b) != "tagged: Review movie" {
		t.Fatal(string(b), err)
	}
	b, err = os.ReadFile(filepath.Join(f.cfg.MediaDir, filepath.FromSlash(p.FolderMove.Destination), "poster.jpg"))
	if err != nil || string(b) != "art" {
		t.Fatal("extra changed", err)
	}
	var child string
	if err = f.db.QueryRow(`SELECT id FROM organization_plans WHERE json_extract(snapshot,'$.replacement.tag_parent')=?`, p.ID).Scan(&child); err != nil {
		t.Fatal(err)
	}
	version, err := f.s.Plan(ctx, child)
	if err != nil || version.State != "completed" {
		t.Fatal(version, err)
	}
	b, err = os.ReadFile(filepath.Join(f.cfg.MediaDir, filepath.FromSlash(version.Replacement.Backup)))
	if err != nil || string(b) != "video" {
		t.Fatal("rollback copy", err)
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

// The disposable test container mounts Anime on tmpfs and Movies on its
// separate container filesystem, so Linux actually returns EXDEV.
func TestCrossFilesystemFolderMove(t *testing.T) {
	root := os.Getenv("REELSCAREY_TEST_CROSS_ROOT")
	if root == "" {
		t.Skip("requires the isolated cross-filesystem container")
	}
	ctx := context.Background()
	cfg := config.Config{MediaDir: filepath.Join(root, "media"), DownloadsDir: filepath.Join(root, "downloads"), ConfigDir: filepath.Join(root, "config")}
	for _, name := range []string{cfg.MediaDir, cfg.DownloadsDir, cfg.ConfigDir, filepath.Join(cfg.MediaDir, "Anime")} {
		if err := os.MkdirAll(name, 0700); err != nil {
			t.Fatal(err)
		}
	}
	db, err := database.Open(ctx, cfg.ConfigDir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	f := reviewFixture{db: db, cfg: cfg, s: New(db, cfg)}
	f.movie(t, 1, "Movies/Film/film.mkv", []byte("video on source filesystem"))
	base := filepath.Join(cfg.MediaDir, "Movies", "Film")
	if err = os.MkdirAll(filepath.Join(base, "extras", "empty"), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(base, "extras", "poster.jpg"), []byte("art"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(filepath.Join(base, "extras"), 0700); err != nil {
		t.Fatal(err)
	}
	seed := filepath.Join(cfg.DownloadsDir, "seed.mkv")
	if err = os.Link(filepath.Join(base, "film.mkv"), seed); err != nil {
		t.Fatal(err)
	}
	p, err := f.s.PreviewFolder(ctx, 1, "anime")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.Apply(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if err = f.s.Tick(ctx); err != nil {
		t.Fatal("cross-filesystem copy failed", err)
	}
	finished, err := f.s.Plan(ctx, p.ID)
	if err != nil || finished.State != "completed" || !finished.FolderMove.Copy {
		t.Fatal("cross-filesystem job did not complete", finished, err)
	}
	dest := filepath.Join(cfg.MediaDir, filepath.FromSlash(finished.FolderMove.Destination))
	if b, e := os.ReadFile(filepath.Join(dest, "extras", "poster.jpg")); e != nil || string(b) != "art" {
		t.Fatal("extra missing", e)
	}
	if info, e := os.Stat(filepath.Join(dest, "extras", "empty")); e != nil || !info.IsDir() {
		t.Fatal("empty directory missing", e)
	}
	if info, e := os.Stat(filepath.Join(dest, "extras")); e != nil || info.Mode().Perm() != 0700 {
		t.Fatal("directory permissions changed", e)
	}
	if info, e := os.Stat(filepath.Join(dest, "extras", "poster.jpg")); e != nil || info.Mode().Perm() != 0600 {
		t.Fatal("file permissions changed", e)
	}
	if _, e := os.Stat(base); !os.IsNotExist(e) {
		t.Fatal("old directory retained unexpectedly", e)
	}
	if b, e := os.ReadFile(seed); e != nil || string(b) != "video on source filesystem" {
		t.Fatal("seeding source changed", e)
	}
	undo, err := f.s.PreviewRollback(ctx, p.ID)
	if err != nil {
		t.Fatal("review cross-filesystem undo", err)
	}
	if _, err = f.s.Apply(ctx, undo.ID); err != nil {
		t.Fatal(err)
	}
	if err = New(db, cfg).Tick(ctx); err != nil {
		t.Fatal("cross-filesystem undo", err)
	}
	undone, err := f.s.Plan(ctx, undo.ID)
	if err != nil || undone.State != "completed" || !undone.FolderMove.Copy {
		t.Fatal(undone, err)
	}
	for name, want := range map[string]string{"film.mkv": "video on source filesystem", "extras/poster.jpg": "art"} {
		b, e := os.ReadFile(filepath.Join(base, name))
		if e != nil || string(b) != want {
			t.Fatal(name, e)
		}
	}
	if _, err = os.Stat(filepath.Join(base, "extras", "empty")); err != nil {
		t.Fatal(err)
	}
	if b, e := os.ReadFile(seed); e != nil || string(b) != "video on source filesystem" {
		t.Fatal("undo changed seeding", e)
	}
	// Interrupt after the fully verified staging directory has been published,
	// before the database is switched; the next worker must identify its own copy.
	f.movie(t, 2, "Movies/Second/second.mkv", []byte("second video"))
	p2, err := f.s.PreviewFolder(ctx, 2, "anime")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.Apply(ctx, p2.ID); err != nil {
		t.Fatal(err)
	}
	p2.FolderMove.Copy = true
	p2.FolderMove.Stage = filepath.ToSlash(filepath.Join(filepath.Dir(p2.FolderMove.Destination), ".reelscarey-folder-"+p2.ID))
	if err = f.s.savePhase(ctx, &p2, "copying"); err != nil {
		t.Fatal(err)
	}
	r, _, err := f.s.openRoot(p2.Root)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.s.copiedStage(ctx, r, &p2); err != nil {
		r.Close()
		t.Fatal(err)
	}
	if err = f.s.verifyCopiedFolder(ctx, r, p2, p2.FolderMove.Stage); err != nil {
		r.Close()
		t.Fatal(err)
	}
	if err = renameFolderNoReplace(r, p2.FolderMove.Stage, p2.FolderMove.Destination); err != nil {
		r.Close()
		t.Fatal(err)
	}
	r.Close()
	if err = New(db, cfg).Tick(ctx); err != nil {
		t.Fatal("published copy did not recover", err)
	}
	finished, err = f.s.Plan(ctx, p2.ID)
	if err != nil || finished.State != "completed" {
		t.Fatal("recovered copy did not complete", finished, err)
	}
	if _, err = os.Stat(filepath.Join(cfg.MediaDir, "Movies", "Second")); !os.IsNotExist(err) {
		t.Fatal("recovered source not cleaned", err)
	}
	// Resume cleanup after the database switched and one source was removed.
	f.movie(t, 3, "Movies/Third/third.mkv", []byte("third video"))
	thirdRoot := filepath.Join(cfg.MediaDir, "Movies", "Third")
	if err = os.WriteFile(filepath.Join(thirdRoot, "poster.jpg"), []byte("poster"), 0600); err != nil {
		t.Fatal(err)
	}
	p3, err := f.s.PreviewFolder(ctx, 3, "anime")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.Apply(ctx, p3.ID); err != nil {
		t.Fatal(err)
	}
	p3.FolderMove.Copy = true
	p3.FolderMove.Stage = filepath.ToSlash(filepath.Join(filepath.Dir(p3.FolderMove.Destination), ".reelscarey-folder-"+p3.ID))
	if err = f.s.savePhase(ctx, &p3, "copying"); err != nil {
		t.Fatal(err)
	}
	r, _, err = f.s.openRoot(p3.Root)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.s.copiedStage(ctx, r, &p3); err != nil {
		r.Close()
		t.Fatal(err)
	}
	if err = renameFolderNoReplace(r, p3.FolderMove.Stage, p3.FolderMove.Destination); err != nil {
		r.Close()
		t.Fatal(err)
	}
	if err = f.s.verifyCopiedFolder(ctx, r, p3, p3.FolderMove.Destination); err != nil {
		r.Close()
		t.Fatal(err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		r.Close()
		t.Fatal(err)
	}
	if err = f.s.commitFolderReferences(ctx, tx, &p3); err != nil {
		tx.Rollback()
		r.Close()
		t.Fatal(err)
	}
	p3.Phase = "committed"
	raw, err := json.Marshal(p3)
	if err != nil {
		tx.Rollback()
		r.Close()
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE organization_plans SET snapshot=? WHERE id=?`, string(raw), p3.ID); err != nil {
		tx.Rollback()
		r.Close()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		r.Close()
		t.Fatal(err)
	}
	if err = r.Remove(filepath.FromSlash(p3.FolderMove.Inventory.Files[0].Source)); err != nil {
		r.Close()
		t.Fatal(err)
	}
	r.Close()
	if err = New(db, cfg).Tick(ctx); err != nil {
		t.Fatal("partial cleanup did not recover", err)
	}
	if _, err = os.Stat(thirdRoot); !os.IsNotExist(err) {
		t.Fatal("partially cleaned folder remains", err)
	}
	// Unexpected contents in a stage are never published or removed.
	f.movie(t, 4, "Movies/Fourth/fourth.mkv", []byte("fourth video"))
	p4, err := f.s.PreviewFolder(ctx, 4, "anime")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.Apply(ctx, p4.ID); err != nil {
		t.Fatal(err)
	}
	p4.FolderMove.Copy = true
	p4.FolderMove.Stage = filepath.ToSlash(filepath.Join(filepath.Dir(p4.FolderMove.Destination), ".reelscarey-folder-"+p4.ID))
	if err = f.s.savePhase(ctx, &p4, "copying"); err != nil {
		t.Fatal(err)
	}
	r, _, err = f.s.openRoot(p4.Root)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.s.copiedStage(ctx, r, &p4); err != nil {
		r.Close()
		t.Fatal(err)
	}
	foreign := filepath.Join(cfg.MediaDir, filepath.FromSlash(p4.FolderMove.Stage), "foreign.txt")
	if err = os.WriteFile(foreign, []byte("foreign"), 0600); err != nil {
		r.Close()
		t.Fatal(err)
	}
	if err = f.s.verifyCopiedFolder(ctx, r, p4, p4.FolderMove.Stage); err == nil {
		r.Close()
		t.Fatal("foreign stage content accepted")
	}
	r.Close()
	if err = New(db, cfg).Tick(ctx); err == nil {
		t.Fatal("unexpected stage content was published")
	}
	if b, e := os.ReadFile(foreign); e != nil || string(b) != "foreign" {
		t.Fatal("foreign stage content not retained", e)
	}
	if _, err = os.Stat(filepath.Join(cfg.MediaDir, "Movies", "Fourth", "fourth.mkv")); err != nil {
		t.Fatal("original was removed", err)
	}
}
