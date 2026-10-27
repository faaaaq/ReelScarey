package youtube

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/config"
	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/transfer"
)

type testDownloader struct{}

func (testDownloader) Download(ctx context.Context, id, dir string) (string, error) {
	name := id + ".mp4"
	return name, os.WriteFile(filepath.Join(dir, name), []byte("owned generated video"), 0600)
}
func (d testDownloader) DownloadWithSubtitles(ctx context.Context, id, dir string, opts DownloadOptions) (DownloadResult, error) {
	name, err := d.Download(ctx, id, dir)
	if err != nil {
		return DownloadResult{}, err
	}
	sub := id + ".en.vtt"
	if err = os.WriteFile(filepath.Join(dir, sub), []byte("WEBVTT\n\n00:00:00.000 --> 00:00:01.000\nGenerated\n"), 0600); err != nil {
		return DownloadResult{}, err
	}
	return DownloadResult{Video: name, Subtitles: []SubtitleFile{{Language: "en", Name: sub}}, MissingLanguages: []string{"ja"}}, nil
}

func TestReviewedCustomImport(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	db, err := database.Open(ctx, filepath.Join(base, "config"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	media := filepath.Join(base, "media")
	if err = os.Mkdir(media, 0700); err != nil {
		t.Fatal(err)
	}
	s := NewStore(db)
	c, err := s.CreateCollection(ctx, "Generated collection")
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.Preview(ctx, Video{ID: "abcdefghijk", Title: "Generated"}, Target{Kind: "custom", CollectionID: c.ID})
	if err != nil {
		t.Fatal(err)
	}
	j, err := s.Queue(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	w := Worker{Store: s, Downloader: testDownloader{}, Prober: testProber{}, StagingRoot: filepath.Join(base, "staging")}
	if _, err = w.ProcessOne(ctx); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{MediaDir: media}
	review, err := s.ReviewImport(ctx, cfg, j.ID)
	if err != nil {
		t.Fatal(err)
	}
	bad := review
	bad.SHA256 = "changed"
	if err = s.ApproveImport(ctx, cfg, bad); err == nil {
		t.Fatal("changed review accepted")
	}
	if err = s.ApproveImport(ctx, cfg, review); err != nil {
		t.Fatal(err)
	}
	if worked, e := w.ProcessImport(ctx, cfg); !worked || e != nil {
		t.Fatalf("import: %v %v", worked, e)
	}
	got, err := s.Job(ctx, j.ID)
	if err != nil || got.State != "completed" {
		t.Fatalf("job: %+v %v", got, err)
	}
	data, err := os.ReadFile(filepath.Join(media, filepath.FromSlash(review.Destination)))
	if err != nil || string(data) != "owned generated video" {
		t.Fatalf("file: %q %v", data, err)
	}
	var stage string
	if err = db.QueryRow(`SELECT stage_name FROM youtube_imports WHERE job_id=?`, j.ID).Scan(&stage); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(filepath.Dir(filepath.Join(media, filepath.FromSlash(review.Destination))), stage)
	if err = os.WriteFile(foreign, []byte("foreign"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = w.CleanupCommitted(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(foreign)
	if err != nil || string(data) != "foreign" {
		t.Fatalf("cleanup changed foreign stage: %q %v", data, err)
	}
}

func TestReviewedMovieImportRegistersCatalogFile(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	db, err := database.Open(ctx, filepath.Join(base, "config"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	media := filepath.Join(base, "media")
	if err = os.Mkdir(media, 0700); err != nil {
		t.Fatal(err)
	}
	result, err := db.Exec(`INSERT INTO media_items(kind,tmdb_id,title,year) VALUES('movie',123,'Generated movie',2020)`)
	if err != nil {
		t.Fatal(err)
	}
	mediaID, _ := result.LastInsertId()
	s := NewStore(db)
	review, err := s.Preview(ctx, Video{ID: "abcdefghijk", Title: "Generated"}, Target{Kind: "movie", MediaID: mediaID})
	if err != nil {
		t.Fatal(err)
	}
	job, err := s.Queue(ctx, review.ID)
	if err != nil {
		t.Fatal(err)
	}
	w := Worker{Store: s, Downloader: testDownloader{}, Prober: testProber{}, StagingRoot: filepath.Join(base, "staging")}
	if _, err = w.ProcessOne(ctx); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{MediaDir: media}
	ir, err := s.ReviewImport(ctx, cfg, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ApproveImport(ctx, cfg, ir); err != nil {
		t.Fatal(err)
	}
	if worked, e := w.ProcessImport(ctx, cfg); !worked || e != nil {
		t.Fatalf("import: %v %v", worked, e)
	}
	var count int
	if err = db.QueryRow(`SELECT COUNT(*) FROM media_files m JOIN youtube_files y ON y.media_file_id=m.id WHERE m.movie_id=? AND y.job_id=?`, mediaID, job.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("registration: %d %v", count, err)
	}
}

func TestImportNeverReplacesExistingDestination(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	db, err := database.Open(ctx, filepath.Join(base, "config"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	media := filepath.Join(base, "media")
	if err = os.Mkdir(media, 0700); err != nil {
		t.Fatal(err)
	}
	s := NewStore(db)
	c, err := s.CreateCollection(ctx, "Collection")
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.Preview(ctx, Video{ID: "abcdefghijk", Title: "Generated"}, Target{Kind: "custom", CollectionID: c.ID})
	if err != nil {
		t.Fatal(err)
	}
	j, err := s.Queue(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	w := Worker{Store: s, Downloader: testDownloader{}, Prober: testProber{}, StagingRoot: filepath.Join(base, "staging")}
	if _, err = w.ProcessOne(ctx); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{MediaDir: media}
	review, err := s.ReviewImport(ctx, cfg, j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ApproveImport(ctx, cfg, review); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(media, filepath.FromSlash(review.Destination))
	if err = os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(dest, []byte("foreign"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = w.ProcessImport(ctx, cfg); err == nil {
		t.Fatal("expected destination conflict")
	}
	data, err := os.ReadFile(dest)
	if err != nil || string(data) != "foreign" {
		t.Fatalf("destination changed: %q %v", data, err)
	}
}

func TestReviewedEpisodeImportRegistersCanonicalEpisode(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	db, err := database.Open(ctx, filepath.Join(base, "config"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	media := filepath.Join(base, "media")
	if err = os.Mkdir(media, 0700); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{`INSERT INTO media_items(id,kind,tmdb_id,title) VALUES(1,'series',12,'Generated show')`, `INSERT INTO series_details(media_id) VALUES(1)`, `INSERT INTO seasons(id,series_id,tmdb_id,number,title,overview,air_date,episode_count,loaded_at) VALUES(1,1,13,1,'Season 1','','',1,'loaded')`, `INSERT INTO episodes(id,season_id,tmdb_id,number,title,overview,air_date) VALUES(1,1,14,1,'Episode','','')`} {
		if _, err = db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	s := NewStore(db)
	r, err := s.Preview(ctx, Video{ID: "abcdefghijk", Title: "Generated"}, Target{Kind: "episode", MediaID: 1, EpisodeID: 1})
	if err != nil {
		t.Fatal(err)
	}
	j, err := s.Queue(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	w := Worker{Store: s, Downloader: testDownloader{}, Prober: testProber{}, StagingRoot: filepath.Join(base, "staging")}
	if _, err = w.ProcessOne(ctx); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{MediaDir: media}
	ir, err := s.ReviewImport(ctx, cfg, j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ApproveImport(ctx, cfg, ir); err != nil {
		t.Fatal(err)
	}
	if worked, e := w.ProcessImport(ctx, cfg); !worked || e != nil {
		t.Fatalf("import: %v %v", worked, e)
	}
	var count int
	if err = db.QueryRow(`SELECT COUNT(*) FROM episode_files e JOIN youtube_files y ON y.media_file_id=e.media_file_id WHERE e.episode_id=1 AND y.job_id=?`, j.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("registration: %d %v", count, err)
	}
}

func TestReviewedSubtitleImportWithVideo(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	db, err := database.Open(ctx, filepath.Join(base, "config"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	media := filepath.Join(base, "media")
	if err = os.Mkdir(media, 0700); err != nil {
		t.Fatal(err)
	}
	s := NewStore(db)
	c, err := s.CreateCollection(ctx, "Generated")
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.PreviewWithSubtitles(ctx, Video{ID: "abcdefghijk", Title: "Generated"}, Target{Kind: "custom", CollectionID: c.ID}, DownloadOptions{Languages: []string{"en", "ja"}, Automatic: true})
	if err != nil {
		t.Fatal(err)
	}
	j, err := s.Queue(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	w := Worker{Store: s, Downloader: testDownloader{}, Prober: testProber{}, StagingRoot: filepath.Join(base, "staging")}
	if _, err = w.ProcessOne(ctx); err != nil {
		t.Fatal(err)
	}
	job, err := s.Job(ctx, j.ID)
	if err != nil || !strings.Contains(job.Message, "ja") {
		t.Fatalf("missing subtitle notice: %+v %v", job, err)
	}
	cfg := config.Config{MediaDir: media}
	ir, err := s.ReviewImport(ctx, cfg, j.ID)
	if err != nil || len(ir.Subtitles) != 1 || ir.Subtitles[0].Language != "en" {
		t.Fatalf("review: %+v %v", ir, err)
	}
	changed := ir
	changed.Subtitles = append([]SubtitleReview{}, ir.Subtitles...)
	changed.Subtitles[0].SHA256 = "changed"
	if err = s.ApproveImport(ctx, cfg, changed); err == nil {
		t.Fatal("accepted altered subtitle")
	}
	if err = s.ApproveImport(ctx, cfg, ir); err != nil {
		t.Fatal(err)
	}
	if worked, e := w.ProcessImport(ctx, cfg); !worked || e != nil {
		t.Fatalf("import: %v %v", worked, e)
	}
	data, err := os.ReadFile(filepath.Join(media, filepath.FromSlash(ir.Subtitles[0].Destination)))
	if err != nil || !strings.HasPrefix(string(data), "WEBVTT") {
		t.Fatalf("subtitle: %q %v", data, err)
	}
	stages, err := filepath.Glob(filepath.Join(media, filepath.FromSlash(filepath.Dir(ir.Destination)), ".reelscarey-youtube-*.part"))
	if err != nil || len(stages) != 0 {
		t.Fatalf("committed staging links remain: %v %v", stages, err)
	}
	var count int
	if err = db.QueryRow(`SELECT COUNT(*) FROM youtube_subtitle_files WHERE job_id=? AND language='en'`, j.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("provenance: %d %v", count, err)
	}
}

func TestMovieSubtitleRegisteredAlongsideVideo(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	db, err := database.Open(ctx, filepath.Join(base, "config"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	media := filepath.Join(base, "media")
	if err = os.Mkdir(media, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO media_items(id,kind,tmdb_id,title) VALUES(1,'movie',123,'Generated movie')`); err != nil {
		t.Fatal(err)
	}
	s := NewStore(db)
	r, err := s.PreviewWithSubtitles(ctx, Video{ID: "abcdefghijk", Title: "Generated"}, Target{Kind: "movie", MediaID: 1}, DownloadOptions{Languages: []string{"en"}})
	if err != nil {
		t.Fatal(err)
	}
	j, err := s.Queue(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	w := Worker{Store: s, Downloader: testDownloader{}, Prober: testProber{}, StagingRoot: filepath.Join(base, "staging")}
	if _, err = w.ProcessOne(ctx); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{MediaDir: media}
	ir, err := s.ReviewImport(ctx, cfg, j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ApproveImport(ctx, cfg, ir); err != nil {
		t.Fatal(err)
	}
	if _, err = w.ProcessImport(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = db.QueryRow(`SELECT COUNT(*) FROM subtitles s JOIN youtube_files y ON y.media_file_id=s.media_file_id WHERE y.job_id=? AND s.language='en' AND s.source='downloaded'`, j.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("subtitle registration: %d %v", count, err)
	}
}

func TestSubtitleConflictDoesNotOverwriteAndImportResumes(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	db, err := database.Open(ctx, filepath.Join(base, "config"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	media := filepath.Join(base, "media")
	if err = os.Mkdir(media, 0700); err != nil {
		t.Fatal(err)
	}
	s := NewStore(db)
	c, err := s.CreateCollection(ctx, "Generated")
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.PreviewWithSubtitles(ctx, Video{ID: "abcdefghijk", Title: "Generated"}, Target{Kind: "custom", CollectionID: c.ID}, DownloadOptions{Languages: []string{"en"}})
	if err != nil {
		t.Fatal(err)
	}
	j, err := s.Queue(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	w := Worker{Store: s, Downloader: testDownloader{}, Prober: testProber{}, StagingRoot: filepath.Join(base, "staging")}
	if _, err = w.ProcessOne(ctx); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{MediaDir: media}
	ir, err := s.ReviewImport(ctx, cfg, j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ApproveImport(ctx, cfg, ir); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(media, filepath.FromSlash(ir.Subtitles[0].Destination))
	if err = os.MkdirAll(filepath.Dir(sub), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(sub, []byte("foreign"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = w.ProcessImport(ctx, cfg); err == nil {
		t.Fatal("expected subtitle conflict")
	}
	data, err := os.ReadFile(sub)
	if err != nil || string(data) != "foreign" {
		t.Fatalf("subtitle overwritten: %q %v", data, err)
	}
	if err = os.Remove(sub); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = database.Open(ctx, filepath.Join(base, "config"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	w.Store = NewStore(db)
	if _, err = db.Exec(`UPDATE youtube_jobs SET lease_until=0 WHERE id=?`, j.ID); err != nil {
		t.Fatal(err)
	}
	if worked, e := w.ProcessImport(ctx, cfg); !worked || e != nil {
		t.Fatalf("resume: %v %v", worked, e)
	}
	data, err = os.ReadFile(sub)
	if err != nil || !strings.HasPrefix(string(data), "WEBVTT") {
		t.Fatalf("resumed subtitle: %q %v", data, err)
	}
}

func TestChangedSourceIsNotPublished(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	db, err := database.Open(ctx, filepath.Join(base, "config"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	media := filepath.Join(base, "media")
	if err = os.Mkdir(media, 0700); err != nil {
		t.Fatal(err)
	}
	s := NewStore(db)
	c, err := s.CreateCollection(ctx, "Generated")
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.Preview(ctx, Video{ID: "abcdefghijk", Title: "Generated"}, Target{Kind: "custom", CollectionID: c.ID})
	if err != nil {
		t.Fatal(err)
	}
	j, err := s.Queue(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	w := Worker{Store: s, Downloader: testDownloader{}, Prober: testProber{}, StagingRoot: filepath.Join(base, "staging")}
	if _, err = w.ProcessOne(ctx); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{MediaDir: media}
	ir, err := s.ReviewImport(ctx, cfg, j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ApproveImport(ctx, cfg, ir); err != nil {
		t.Fatal(err)
	}
	var source string
	if err = db.QueryRow(`SELECT source_path FROM youtube_jobs WHERE id=?`, j.ID).Scan(&source); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(w.StagingRoot, filepath.FromSlash(source)), []byte("changed after review"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = w.ProcessImport(ctx, cfg); err == nil {
		t.Fatal("changed source accepted")
	}
	if _, err = os.Stat(filepath.Join(media, filepath.FromSlash(ir.Destination))); !os.IsNotExist(err) {
		t.Fatalf("destination unexpectedly exists: %v", err)
	}
}

func TestAcquisitionRefusesSymlinkStagingRoot(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	db, err := database.Open(ctx, filepath.Join(base, "config"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	outside := filepath.Join(base, "outside")
	if err = os.Mkdir(outside, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "staging")
	if err = os.Symlink(outside, link); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	s := NewStore(db)
	c, err := s.CreateCollection(ctx, "Generated")
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.Preview(ctx, Video{ID: "abcdefghijk", Title: "Generated"}, Target{Kind: "custom", CollectionID: c.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Queue(ctx, r.ID); err != nil {
		t.Fatal(err)
	}
	w := Worker{Store: s, Downloader: testDownloader{}, Prober: testProber{}, StagingRoot: link}
	if _, err = w.ProcessOne(ctx); err == nil {
		t.Fatal("accepted symlink root")
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatalf("outside changed: %v %v", entries, err)
	}
}

func TestAcquisitionRefusesOverlappingMediaRoot(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	db, err := database.Open(ctx, filepath.Join(base, "config"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	media := filepath.Join(base, "media")
	if err = os.Mkdir(media, 0700); err != nil {
		t.Fatal(err)
	}
	s := NewStore(db)
	c, err := s.CreateCollection(ctx, "Generated")
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.Preview(ctx, Video{ID: "abcdefghijk", Title: "Generated"}, Target{Kind: "custom", CollectionID: c.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Queue(ctx, r.ID); err != nil {
		t.Fatal(err)
	}
	w := Worker{Store: s, Downloader: testDownloader{}, Prober: testProber{}, StagingRoot: filepath.Join(media, ".reelscarey-youtube"), MediaRoot: media}
	if _, err = w.ProcessOne(ctx); err == nil {
		t.Fatal("accepted overlapping roots")
	}
	if _, err = os.Stat(w.StagingRoot); !os.IsNotExist(err) {
		t.Fatalf("staging directory unexpectedly created: %v", err)
	}
}

type testProber struct{}

func (testProber) Probe(context.Context, *os.File) (transfer.Probe, error) {
	return transfer.Probe{Codec: "h264", Width: 1920, Height: 1080, Duration: 5}, nil
}

func TestWorkerPersistsVerifiedFileForReview(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	db, e := database.Open(ctx, filepath.Join(base, "config"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	s := NewStore(db)
	collection, e := s.CreateCollection(ctx, "XG Days")
	if e != nil {
		t.Fatal(e)
	}
	r, e := s.Preview(ctx, Video{ID: "abcdefghijk", Title: "Generated"}, Target{Kind: "custom", CollectionID: collection.ID})
	if e != nil {
		t.Fatal(e)
	}
	j, e := s.Queue(ctx, r.ID)
	if e != nil {
		t.Fatal(e)
	}
	w := Worker{Store: s, Downloader: testDownloader{}, Prober: testProber{}, StagingRoot: filepath.Join(base, "staging")}
	worked, e := w.ProcessOne(ctx)
	if !worked || e != nil {
		t.Fatalf("process: %v %v", worked, e)
	}
	got, e := s.Job(ctx, j.ID)
	if e != nil || got.State != "awaiting_review" {
		t.Fatalf("state: %+v %v", got, e)
	}
	var file, hash string
	var size int64
	if e = db.QueryRow(`SELECT source_path,source_sha256,source_size FROM youtube_jobs WHERE id=?`, j.ID).Scan(&file, &hash, &size); e != nil || len(hash) != 64 || size <= 0 {
		t.Fatalf("evidence: %q %q %d %v", file, hash, size, e)
	}
	if _, e = os.Stat(filepath.Join(base, "staging", filepath.FromSlash(file))); e != nil {
		t.Fatal(e)
	}
	worked, e = w.ProcessOne(ctx)
	if worked || e != nil {
		t.Fatalf("must not redownload: %v %v", worked, e)
	}
}
