package organization

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
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
	if _, e = f.db.Exec(`UPDATE media_files SET probe_json='{"audio_streams":[]}' WHERE id=1`); e != nil {
		t.Fatal(e)
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

func TestReviewedEmbeddedTitleReplacesInPlaceAndRecordsPreviousTitle(t *testing.T) {
	ctx := context.Background()
	f := newReviewFixture(t)
	rel := "Movies/Original/movie.mkv"
	f.movie(t, 1, rel, []byte("original video"))
	if _, e := f.db.Exec(`UPDATE media_files SET probe_json='{"embedded_title":"Wrong title"}' WHERE id=1`); e != nil {
		t.Fatal(e)
	}
	seed := filepath.Join(f.cfg.DownloadsDir, "seeding.mkv")
	if e := os.Link(filepath.Join(f.cfg.MediaDir, filepath.FromSlash(rel)), seed); e != nil {
		t.Fatal(e)
	}
	f.s.SetTagger(func(ctx context.Context, source, stage *os.File, extension, title string) (string, error) {
		if title != "Review movie" || extension != ".mkv" {
			t.Fatal(title, extension)
		}
		if e := stage.Truncate(0); e != nil {
			return "", e
		}
		if _, e := stage.WriteAt([]byte("tagged video"), 0); e != nil {
			return "", e
		}
		return `{"embedded_title":"Review movie"}`, nil
	})
	p, e := f.s.PreviewEmbeddedTitle(ctx, 1)
	if e != nil || p.Replacement.TagTitle != "Review movie" || !p.Replacement.MetadataOnly || p.Replacement.Backup != "" {
		t.Fatal(p, e)
	}
	if p.Replacement.PreviousTitle == nil || *p.Replacement.PreviousTitle != "Wrong title" {
		t.Fatal("previous title not recorded", p.Replacement.PreviousTitle)
	}
	if _, e = f.s.Apply(ctx, p.ID); e != nil {
		t.Fatal(e)
	}
	if e = f.s.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	current := filepath.Join(f.cfg.MediaDir, filepath.FromSlash(rel))
	read := func(name, want string) {
		t.Helper()
		b, e := os.ReadFile(name)
		if e != nil || string(b) != want {
			t.Fatalf("%s: %q %v", name, b, e)
		}
	}
	read(current, "tagged video")
	read(seed, "original video")
	if _, e = os.Stat(filepath.Join(f.cfg.MediaDir, filepath.FromSlash(p.Replacement.TagStage))); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("metadata stage not cleaned", e)
	}
	if backups, _ := filepath.Glob(filepath.Join(f.cfg.MediaDir, ".reelscarey-versions", ".reelscarey-version-*")); len(backups) != 0 {
		t.Fatal("title update retained a video copy", backups)
	}
	saved, e := f.s.Plan(ctx, p.ID)
	if e != nil || saved.State != "completed" || saved.Replacement.PreviousTitle == nil || *saved.Replacement.PreviousTitle != "Wrong title" {
		t.Fatal(saved, e)
	}
	if saved.Replacement.Before.SHA == saved.Replacement.After.SHA {
		t.Fatal("change record does not distinguish the previous and current checksums")
	}
	if _, e = f.s.PreviewRollback(ctx, p.ID); e == nil {
		t.Fatal("metadata-only rollback accepted without a retained video")
	}
}

func TestEmbeddedTitleRejectsStaleTitleAndUnsafeRewrite(t *testing.T) {
	ctx := context.Background()
	f := newReviewFixture(t)
	rel := "Movies/Original/movie.mkv"
	f.movie(t, 1, rel, []byte("original video"))
	p, e := f.s.PreviewEmbeddedTitle(ctx, 1)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.db.Exec(`UPDATE media_items SET title='Changed title' WHERE id=1`); e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.Apply(ctx, p.ID); e == nil {
		t.Fatal("stale title accepted")
	}
	p, e = f.s.PreviewEmbeddedTitle(ctx, 1)
	if e != nil {
		t.Fatal(e)
	}
	f.s.SetTagger(func(context.Context, *os.File, *os.File, string, string) (string, error) {
		return "", errors.New("verification failed")
	})
	if _, e = f.s.Apply(ctx, p.ID); e != nil {
		t.Fatal(e)
	}
	if e = f.s.Tick(ctx); e == nil {
		t.Fatal("failed verifier accepted")
	}
	if e = EnsureMediaAvailable(ctx, f.db); e != nil {
		t.Fatal("safe failure blocked media", e)
	}
	b, e := os.ReadFile(filepath.Join(f.cfg.MediaDir, filepath.FromSlash(rel)))
	if e != nil || string(b) != "original video" {
		t.Fatal(string(b), e)
	}
}

func addYouTubeProvenance(t *testing.T, f reviewFixture) {
	t.Helper()
	queries := []string{
		`INSERT INTO youtube_reviews(id,video_id,video_title,channel,target_kind,media_id,target_snapshot,expires_at) VALUES('review','video','Video','Channel','movie',1,'{}',1)`,
		`INSERT INTO youtube_jobs(id,review_id,video_id,video_title,channel,target_kind,media_id,target_snapshot,state,created_at,updated_at) VALUES('youtube','review','video','Video','Channel','movie',1,'{}','completed',1,1)`,
		`INSERT INTO youtube_files(job_id,media_file_id,video_id,title,relative_path,size_bytes,sha256,probe_json,imported_at) SELECT 'youtube',id,'video','Video',relative_path,size_bytes,sha256,probe_json,1 FROM media_files WHERE id=1`,
		`INSERT INTO youtube_subtitle_files(job_id,language,relative_path,sha256,size_bytes) VALUES('youtube','en','Movies/Film/Film.en.vtt','test',8)`,
		`INSERT INTO youtube_subtitle_imports(job_id,language,source_path,destination_path,stage_name,size_bytes,source_mtime,sha256,phase) VALUES('youtube','en','source','Movies/Film/Film.en.vtt','stage',8,1,'test','committed')`,
		`INSERT INTO subtitles(media_file_id,language,variant,source,relative_path,sha256,updated_at) VALUES(1,'en','full','downloaded','Movies/Film/Film.en.vtt','test',1)`,
	}
	for _, q := range queries {
		if _, e := f.db.Exec(q); e != nil {
			t.Fatal(e)
		}
	}
	if e := os.WriteFile(filepath.Join(f.cfg.MediaDir, "Movies", "Film", "Film.en.vtt"), []byte("subtitle"), 0600); e != nil {
		t.Fatal(e)
	}
}
func TestYouTubeProvenanceRestoredAlongWithVideo(t *testing.T) {
	ctx := context.Background()
	f, _ := replacementFixture(t)
	addYouTubeProvenance(t, f)
	p, e := f.s.PreviewReplacement(ctx, 1, "candidate")
	if e != nil || p.Replacement.BeforeYouTube == nil {
		t.Fatal(p, e)
	}
	if _, e = f.s.Apply(ctx, p.ID); e != nil {
		t.Fatal(e)
	}
	if e = f.s.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	var n int
	if e = f.db.QueryRow(`SELECT COUNT(*) FROM youtube_files WHERE media_file_id=1`).Scan(&n); e != nil || n != 0 {
		t.Fatal(n, e)
	}
	move, e := f.s.Preview(ctx, []int64{1}, "anime")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.Apply(ctx, move.ID); e != nil {
		t.Fatal(e)
	}
	if e = f.s.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	var subtitlePath, youtubeSubtitlePath, subtitleJournal string
	if e = f.db.QueryRow(`SELECT s.relative_path,y.relative_path,i.destination_path FROM subtitles s JOIN youtube_subtitle_files y ON y.job_id='youtube' JOIN youtube_subtitle_imports i ON i.job_id=y.job_id AND i.language=y.language WHERE s.media_file_id=1 AND s.language='en'`).Scan(&subtitlePath, &youtubeSubtitlePath, &subtitleJournal); e != nil || subtitlePath != youtubeSubtitlePath || subtitlePath != subtitleJournal {
		t.Fatal("subtitle provenance did not move", subtitlePath, youtubeSubtitlePath, subtitleJournal, e)
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
	if e = f.db.QueryRow(`SELECT COUNT(*) FROM youtube_files WHERE media_file_id=1 AND job_id='youtube' AND video_id='video'`).Scan(&n); e != nil || n != 1 {
		t.Fatal("YouTube source not restored", n, e)
	}
	b, e := os.ReadFile(filepath.Join(f.cfg.MediaDir, filepath.FromSlash(subtitlePath)))
	if e != nil || string(b) != "subtitle" {
		t.Fatal("subtitle changed", string(b), e)
	}
}
func TestYouTubeMetadataUpdateKeepsProvenance(t *testing.T) {
	ctx := context.Background()
	f, _ := replacementFixture(t)
	addYouTubeProvenance(t, f)
	f.s.SetTagger(func(ctx context.Context, source, stage *os.File, extension, title string) (string, error) {
		if _, e := stage.WriteAt([]byte("tagged youtube"), 0); e != nil {
			return "", e
		}
		return `{"embedded_title":"Review movie"}`, nil
	})
	p, e := f.s.PreviewEmbeddedTitle(ctx, 1)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.Apply(ctx, p.ID); e != nil {
		t.Fatal(e)
	}
	if e = f.s.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	var actual, provenance string
	if e = f.db.QueryRow(`SELECT m.sha256,y.sha256 FROM media_files m JOIN youtube_files y ON y.media_file_id=m.id WHERE m.id=1`).Scan(&actual, &provenance); e != nil || actual != provenance {
		t.Fatal(actual, provenance, e)
	}
	saved, e := f.s.Plan(ctx, p.ID)
	if e != nil || saved.Replacement.Before.SHA == saved.Replacement.After.SHA {
		t.Fatal(saved, e)
	}
	if _, e = f.s.PreviewRollback(ctx, p.ID); e == nil {
		t.Fatal("metadata-only rollback accepted without a retained video")
	}
}

func TestMoveRollbackAllowsRecordedMetadataTitleChange(t *testing.T) {
	ctx := context.Background()
	f := newReviewFixture(t)
	rel := "Movies/Original/movie.mkv"
	f.movie(t, 1, rel, []byte("original video"))
	move, e := f.s.Preview(ctx, []int64{1}, "anime")
	if e != nil || len(move.Files) != 1 || move.Files[0].Destination == rel {
		t.Fatal(move, e)
	}
	if _, e = f.s.Apply(ctx, move.ID); e != nil {
		t.Fatal(e)
	}
	if e = f.s.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	f.s.SetTagger(func(ctx context.Context, source, stage *os.File, extension, title string) (string, error) {
		if _, e := stage.WriteAt([]byte("tagged video"), 0); e != nil {
			return "", e
		}
		return `{"embedded_title":"Review movie"}`, nil
	})
	title, e := f.s.PreviewEmbeddedTitle(ctx, 1)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.Apply(ctx, title.ID); e != nil {
		t.Fatal(e)
	}
	if e = f.s.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	rollback, e := f.s.PreviewRollback(ctx, move.ID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.Apply(ctx, rollback.ID); e != nil {
		t.Fatal(e)
	}
	if e = f.s.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	var current string
	if e = f.db.QueryRow(`SELECT relative_path FROM media_files WHERE id=1`).Scan(&current); e != nil || current != rel {
		t.Fatal(current, e)
	}
	content, e := os.ReadFile(filepath.Join(f.cfg.MediaDir, filepath.FromSlash(rel)))
	if e != nil || string(content) != "tagged video" {
		t.Fatal(string(content), e)
	}
}

func TestYouTubeSubtitleHistoryFollowsFolderMoveBeforeRestore(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("whole-folder moves require the Linux Docker deployment")
	}
	ctx := context.Background()
	f, _ := replacementFixture(t)
	addYouTubeProvenance(t, f)
	replacement, e := f.s.PreviewReplacement(ctx, 1, "candidate")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.Apply(ctx, replacement.ID); e != nil {
		t.Fatal(e)
	}
	if e = f.s.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	move, e := f.s.PreviewFolder(ctx, 1, "anime")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.Apply(ctx, move.ID); e != nil {
		t.Fatal(e)
	}
	if e = f.s.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	var a, b, c string
	if e = f.db.QueryRow(`SELECT s.relative_path,y.relative_path,i.destination_path FROM subtitles s JOIN youtube_subtitle_files y ON y.job_id='youtube' JOIN youtube_subtitle_imports i ON i.job_id=y.job_id AND i.language=y.language WHERE s.media_file_id=1 AND s.language='en'`).Scan(&a, &b, &c); e != nil || a != b || a != c {
		t.Fatal(a, b, c, e)
	}
	restore, e := f.s.PreviewRollback(ctx, replacement.ID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.Apply(ctx, restore.ID); e != nil {
		t.Fatal(e)
	}
	if e = f.s.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	var count int
	if e = f.db.QueryRow(`SELECT COUNT(*) FROM youtube_files WHERE media_file_id=1 AND job_id='youtube'`).Scan(&count); e != nil || count != 1 {
		t.Fatal(count, e)
	}
	data, e := os.ReadFile(filepath.Join(f.cfg.MediaDir, filepath.FromSlash(a)))
	if e != nil || string(data) != "subtitle" {
		t.Fatal(string(data), e)
	}
}

func TestEmbeddedTitleResumesVerifiedStageAfterRestart(t *testing.T) {
	ctx := context.Background()
	f := newReviewFixture(t)
	rel := "Movies/Original/movie.mkv"
	f.movie(t, 1, rel, []byte("original video"))
	calls := 0
	tag := func(ctx context.Context, source, stage *os.File, extension, title string) (string, error) {
		calls++
		if _, e := stage.WriteAt([]byte("tagged video"), 0); e != nil {
			return "", e
		}
		return `{"embedded_title":"Review movie"}`, nil
	}
	f.s.SetTagger(tag)
	p, e := f.s.PreviewEmbeddedTitle(ctx, 1)
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
	if e = f.s.prepareEmbeddedTitle(ctx, root, &p); e != nil {
		t.Fatal(e)
	}
	if e = f.s.savePhase(ctx, &p, "prepared"); e != nil {
		t.Fatal(e)
	}
	root.Close()
	restarted := New(f.db, f.cfg)
	restarted.SetTagger(tag)
	if e = restarted.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	if calls != 1 {
		t.Fatal("verified stage was regenerated after restart", calls)
	}
	content, e := os.ReadFile(filepath.Join(f.cfg.MediaDir, filepath.FromSlash(rel)))
	if e != nil || string(content) != "tagged video" {
		t.Fatal(string(content), e)
	}
}

func TestEmbeddedTitleResumesAfterRenameBeforePhaseAdvance(t *testing.T) {
	ctx := context.Background()
	f := newReviewFixture(t)
	rel := "Movies/Original/movie.mkv"
	f.movie(t, 1, rel, []byte("original video"))
	calls := 0
	tag := func(ctx context.Context, source, stage *os.File, extension, title string) (string, error) {
		calls++
		if _, e := stage.WriteAt([]byte("tagged video"), 0); e != nil {
			return "", e
		}
		return `{"embedded_title":"Review movie"}`, nil
	}
	f.s.SetTagger(tag)
	p, e := f.s.PreviewEmbeddedTitle(ctx, 1)
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
	if e = f.s.prepareEmbeddedTitle(ctx, root, &p); e != nil {
		t.Fatal(e)
	}
	if e = f.s.savePhase(ctx, &p, "prepared"); e != nil {
		t.Fatal(e)
	}
	// Simulate interruption after the atomic replacement but before the saved
	// phase advances. The published file already matches the prepared output.
	if e = root.Rename(filepath.FromSlash(p.Replacement.TagStage), filepath.FromSlash(rel)); e != nil {
		t.Fatal(e)
	}
	root.Close()
	restarted := New(f.db, f.cfg)
	restarted.SetTagger(tag)
	if e = restarted.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	if calls != 1 {
		t.Fatal("verified stage was regenerated after rename", calls)
	}
	content, e := os.ReadFile(filepath.Join(f.cfg.MediaDir, filepath.FromSlash(rel)))
	if e != nil || string(content) != "tagged video" {
		t.Fatal(string(content), e)
	}
	saved, e := restarted.Plan(ctx, p.ID)
	if e != nil || saved.State != "completed" || saved.Phase != "committed" {
		t.Fatal(saved, e)
	}
}
