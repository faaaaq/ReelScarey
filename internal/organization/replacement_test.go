package organization

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func replacementFixture(t *testing.T) (reviewFixture, string) {
	t.Helper()
	f := newReviewFixture(t)
	f.movie(t, 1, "Movies/Film/Film.mkv", []byte("old verified video"))
	source := "Movies/Incoming/new.mkv"
	name := filepath.Join(f.cfg.MediaDir, filepath.FromSlash(source))
	if e := os.MkdirAll(filepath.Dir(name), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(name, []byte("new verified video"), 0600); e != nil {
		t.Fatal(e)
	}
	root, resolved, e := f.s.openRoot("")
	if e != nil {
		t.Fatal(e)
	}
	inf, e := inspect(root, source)
	root.Close()
	if e != nil {
		t.Fatal(e)
	}
	_, e = f.db.Exec(`INSERT INTO library_scans(id,kind,media_root,resolved_root,folder,state,created_at,updated_at) VALUES('scan','movie',?,?, 'Movies','completed',1,1)`, f.cfg.MediaDir, resolved)
	if e != nil {
		t.Fatal(e)
	}
	_, e = f.db.Exec(`INSERT INTO library_scan_files(id,scan_id,relative_path,size_bytes,mtime,file_identity,updated_at) VALUES('candidate','scan',?,?,?,?,1)`, source, inf.Size, inf.Mtime, inf.Identity)
	if e != nil {
		t.Fatal(e)
	}
	f.s.SetProbe(func(context.Context, *os.File) (string, error) { return `{"codec":"h264"}`, nil })
	return f, name
}
func TestReplacementAndRollbackKeepBothVersionsAndSource(t *testing.T) {
	ctx := context.Background()
	f, source := replacementFixture(t)
	seedingSource := filepath.Join(f.cfg.DownloadsDir, "seeding.mkv")
	if e := os.Link(filepath.Join(f.cfg.MediaDir, "Movies", "Film", "Film.mkv"), seedingSource); e != nil {
		t.Fatal(e)
	}
	p, e := f.s.PreviewReplacement(ctx, 1, "candidate")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.Apply(ctx, p.ID); e != nil {
		t.Fatal(e)
	}
	if e = f.s.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	current := filepath.Join(f.cfg.MediaDir, "Movies", "Film", "Film.mkv")
	read := func(name string, want string) {
		t.Helper()
		b, e := os.ReadFile(name)
		if e != nil || string(b) != want {
			t.Fatalf("%s: %q %v", name, b, e)
		}
	}
	read(current, "new verified video")
	read(source, "new verified video")
	read(filepath.Join(f.cfg.MediaDir, filepath.FromSlash(p.Replacement.Backup)), "old verified video")
	if e = os.WriteFile(seedingSource, []byte("seeding source changed"), 0600); e != nil {
		t.Fatal(e)
	}
	read(filepath.Join(f.cfg.MediaDir, filepath.FromSlash(p.Replacement.Backup)), "old verified video")
	if e = os.WriteFile(source, []byte("source changed later"), 0600); e != nil {
		t.Fatal(e)
	}
	read(current, "new verified video") // copies must not share the source inode
	p, e = f.s.Plan(ctx, p.ID)
	if e != nil || p.State != "completed" {
		t.Fatal(p, e)
	}
	back, e := f.s.PreviewRollback(ctx, p.ID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.Apply(ctx, back.ID); e != nil {
		t.Fatal(e)
	}
	if e = f.s.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	read(current, "old verified video")
	read(source, "source changed later")
	read(filepath.Join(f.cfg.MediaDir, filepath.FromSlash(back.Replacement.Backup)), "new verified video")
	versions, e := f.s.ReplacementVersions(ctx, 1)
	if e != nil || len(versions) != 2 || versions[0].ID != back.ID {
		t.Fatal("saved versions not ordered", versions, e)
	}
	var events int
	if e = f.db.QueryRow(`SELECT COUNT(*) FROM activity_events WHERE kind='title' AND object_id='1' AND state IN ('file replaced','previous file restored')`).Scan(&events); e != nil || events != 2 {
		t.Fatal("replacement history missing", events, e)
	}
	if _, e = f.s.PreviewRollback(ctx, p.ID); e == nil {
		t.Fatal("stale version allowed")
	}
}

func TestReplacementResumesAfterRemovalAndNeverOverwritesForeignFile(t *testing.T) {
	ctx := context.Background()
	f, _ := replacementFixture(t)
	p, e := f.s.PreviewReplacement(ctx, 1, "candidate")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.Apply(ctx, p.ID); e != nil {
		t.Fatal(e)
	}
	root, _, e := f.s.openRoot(p.Root)
	if e != nil {
		t.Fatal(e)
	}
	incoming, e := root.Open(filepath.FromSlash(p.Replacement.Incoming.Source))
	if e != nil {
		t.Fatal(e)
	}
	digest, e := hash(ctx, incoming)
	incoming.Close()
	if e != nil {
		t.Fatal(e)
	}
	r := p.Replacement
	r.Incoming.SHA = digest
	r.After = ReplacementMetadata{Path: r.Before.Path, Size: r.Incoming.Size, SHA: digest, Probe: `{"codec":"h264"}`, FileIndex: -1, ImportedAt: 10, Origin: "existing", SourceName: "new.mkv"}
	if e = f.s.savePhase(ctx, &p, "prepared"); e != nil {
		t.Fatal(e)
	}
	backup := r.Original
	backup.Destination = r.Backup
	if e = f.s.publishReplacementCopy(ctx, root, p, backup, 0); e != nil {
		t.Fatal(e)
	}
	if e = f.s.savePhase(ctx, &p, "archived"); e != nil {
		t.Fatal(e)
	}
	if e = root.Remove(filepath.FromSlash(r.Original.Source)); e != nil {
		t.Fatal(e)
	}
	if e = f.s.savePhase(ctx, &p, "removed"); e != nil {
		t.Fatal(e)
	}
	root.Close()
	current := filepath.Join(f.cfg.MediaDir, filepath.FromSlash(r.Original.Source))
	if e = os.WriteFile(current, []byte("foreign file"), 0600); e != nil {
		t.Fatal(e)
	}
	worker := New(f.db, f.cfg)
	if e = worker.Tick(ctx); e == nil {
		t.Fatal("foreign destination was accepted")
	}
	b, e := os.ReadFile(current)
	if e != nil || string(b) != "foreign file" {
		t.Fatal(string(b), e)
	}
	if e = EnsureMediaAvailable(ctx, f.db); e == nil {
		t.Fatal("uncertain operation must reserve media")
	}
	if e = os.Remove(current); e != nil {
		t.Fatal(e)
	}
	if _, e = worker.Apply(ctx, p.ID); e != nil {
		t.Fatal(e)
	}
	if e = worker.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	b, e = os.ReadFile(current)
	if e != nil || string(b) != "new verified video" {
		t.Fatal(string(b), e)
	}
	b, e = os.ReadFile(filepath.Join(f.cfg.MediaDir, filepath.FromSlash(r.Backup)))
	if e != nil || string(b) != "old verified video" {
		t.Fatal(string(b), e)
	}
}
func TestReplacementRejectsChangedSourceBeforeWriting(t *testing.T) {
	ctx := context.Background()
	f, source := replacementFixture(t)
	p, e := f.s.PreviewReplacement(ctx, 1, "candidate")
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(source, []byte("tampered candidate with another size"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.Apply(ctx, p.ID); e == nil {
		t.Fatal("changed source accepted")
	}
	b, e := os.ReadFile(filepath.Join(f.cfg.MediaDir, "Movies", "Film", "Film.mkv"))
	if e != nil || string(b) != "old verified video" {
		t.Fatal(string(b), e)
	}
}

func TestReplacementRejectsDiscoveredSymlink(t *testing.T) {
	f, source := replacementFixture(t)
	if e := os.Remove(source); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink(filepath.Join(f.cfg.MediaDir, "Movies", "Film", "Film.mkv"), source); e != nil {
		t.Skip("symlinks unavailable: ", e)
	}
	if _, e := f.s.PreviewReplacement(context.Background(), 1, "candidate"); e == nil {
		t.Fatal("symlink source accepted")
	}
	b, e := os.ReadFile(filepath.Join(f.cfg.MediaDir, "Movies", "Film", "Film.mkv"))
	if e != nil || string(b) != "old verified video" {
		t.Fatal(string(b), e)
	}
}

func TestReplacementRestoresOriginalIfIncomingDisappearsAfterInterruption(t *testing.T) {
	ctx := context.Background()
	f, source := replacementFixture(t)
	p, e := f.s.PreviewReplacement(ctx, 1, "candidate")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.Apply(ctx, p.ID); e != nil {
		t.Fatal(e)
	}
	root, _, e := f.s.openRoot(p.Root)
	if e != nil {
		t.Fatal(e)
	}
	incoming, e := root.Open(filepath.FromSlash(p.Replacement.Incoming.Source))
	if e != nil {
		t.Fatal(e)
	}
	digest, e := hash(ctx, incoming)
	incoming.Close()
	if e != nil {
		t.Fatal(e)
	}
	r := p.Replacement
	r.Incoming.SHA = digest
	r.After = ReplacementMetadata{Path: r.Before.Path, Size: r.Incoming.Size, SHA: digest, Probe: `{}`, FileIndex: -1, ImportedAt: 10, Origin: "existing", SourceName: "new.mkv"}
	if e = f.s.savePhase(ctx, &p, "prepared"); e != nil {
		t.Fatal(e)
	}
	backup := r.Original
	backup.Destination = r.Backup
	if e = f.s.publishReplacementCopy(ctx, root, p, backup, 0); e != nil {
		t.Fatal(e)
	}
	if e = f.s.savePhase(ctx, &p, "archived"); e != nil {
		t.Fatal(e)
	}
	if e = root.Remove(filepath.FromSlash(r.Original.Source)); e != nil {
		t.Fatal(e)
	}
	root.Close()
	if e = os.Remove(source); e != nil {
		t.Fatal(e)
	}
	worker := New(f.db, f.cfg)
	if e = worker.Tick(ctx); e == nil {
		t.Fatal("missing source accepted")
	}
	if _, e = worker.RecoverReplacement(ctx, p.ID); e != nil {
		t.Fatal(e)
	}
	if e = worker.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(filepath.Join(f.cfg.MediaDir, filepath.FromSlash(r.Original.Source)))
	if e != nil || string(b) != "old verified video" {
		t.Fatal(string(b), e)
	}
	if e = EnsureMediaAvailable(ctx, f.db); e != nil {
		t.Fatal(e)
	}
}

func TestReplacementPreservesSharedEpisodeMappingAndExternalSubtitle(t *testing.T) {
	ctx := context.Background()
	f, _ := replacementFixture(t)
	statements := []string{
		`UPDATE media_items SET kind='series' WHERE id=1`,
		`INSERT INTO series_details(media_id) VALUES(1)`,
		`INSERT INTO seasons(id,series_id,tmdb_id,number,title,overview,air_date,episode_count) VALUES(1,1,1,1,'Season','','',2)`,
		`INSERT INTO episodes(id,season_id,tmdb_id,number,title,overview,air_date) VALUES(1,1,11,1,'One','',''),(2,1,12,2,'Two','','')`,
		`UPDATE media_files SET movie_id=NULL,series_id=1 WHERE id=1`,
		`INSERT INTO episode_files(episode_id,media_file_id) VALUES(1,1),(2,1)`,
		`UPDATE library_scans SET kind='series' WHERE id='scan'`,
		`INSERT INTO subtitles(media_file_id,language,variant,source,relative_path,updated_at) VALUES(1,'en','full','existing','Movies/Film/Film.en.srt',1)`,
	}
	for _, q := range statements {
		if _, e := f.db.Exec(q); e != nil {
			t.Fatal(e)
		}
	}
	subtitle := filepath.Join(f.cfg.MediaDir, "Movies", "Film", "Film.en.srt")
	if e := os.WriteFile(subtitle, []byte("subtitle"), 0600); e != nil {
		t.Fatal(e)
	}
	p, e := f.s.PreviewReplacement(ctx, 1, "candidate")
	if e != nil || len(p.Replacement.Episodes) != 2 {
		t.Fatal(p, e)
	}
	if _, e = f.s.Apply(ctx, p.ID); e != nil {
		t.Fatal(e)
	}
	if e = f.s.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	var count int
	if e = f.db.QueryRow(`SELECT COUNT(*) FROM episode_files WHERE media_file_id=1`).Scan(&count); e != nil || count != 2 {
		t.Fatal(count, e)
	}
	b, e := os.ReadFile(subtitle)
	if e != nil || string(b) != "subtitle" {
		t.Fatal(string(b), e)
	}
	restore, e := f.s.PreviewRollback(ctx, p.ID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.Apply(ctx, restore.ID); e != nil {
		t.Fatal(e)
	}
	if e = f.s.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	if e = f.db.QueryRow(`SELECT COUNT(*) FROM episode_files WHERE media_file_id=1`).Scan(&count); e != nil || count != 2 {
		t.Fatal(count, e)
	}
	b, e = os.ReadFile(subtitle)
	if e != nil || string(b) != "subtitle" {
		t.Fatal(string(b), e)
	}
}
