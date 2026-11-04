package transfer

import (
	"context"
	"crypto/sha1"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type versionsIndexer struct{ extra bool }

func (*versionsIndexer) Test(context.Context, IndexerConfig) error { return nil }
func (v *versionsIndexer) Search(_ context.Context, c IndexerConfig, _ string) ([]Candidate, error) {
	titles := []string{"Test.Movie.2026.1080p.SDR", "Test.Movie.2026.2160p.HDR10"}
	if v.extra {
		titles = append(titles, "Test.Movie.2026.Directors.Cut.1080p.SDR")
	}
	out := []Candidate{}
	for _, title := range titles {
		out = append(out, Candidate{Release: Release{Title: title, Size: 1024}, Generation: c.Generation})
	}
	return out, nil
}
func (*versionsIndexer) Fetch(_ context.Context, _ IndexerConfig, v Candidate) (Payload, string, error) {
	hash := fmt.Sprintf("%x", sha1.Sum([]byte(v.Title)))
	return Payload{Magnet: "magnet:?xt=urn:btih:" + hash}, hash, nil
}

func TestMovieVersionsQueueImportRestartAndAdditionalCopy(t *testing.T) {
	f := setupWorkflow(t)
	ctx := context.Background()
	indexer := &versionsIndexer{extra: true}
	f.s.indexer = indexer
	if _, err := f.db.Exec(`INSERT INTO movie_version_settings(media_id,enabled,hd_profile_id,uhd_profile_id) VALUES(?,1,1,2)`, f.movie.ID); err != nil {
		t.Fatal(err)
	}
	releases, err := f.s.Search(ctx, f.movie.ID, "")
	if err != nil || len(releases) != 3 {
		t.Fatal(releases, err)
	}
	hd, _, err := f.s.QueueMovieVersion(ctx, releases[0].ID, "hd")
	if err != nil {
		t.Fatal(err)
	}
	uhd, _, err := f.s.QueueMovieVersion(ctx, releases[1].ID, "uhd")
	if err != nil {
		t.Fatal(err)
	}
	if hd.MovieVersion != "hd" || uhd.MovieVersion != "uhd" {
		t.Fatal(hd.MovieVersion, uhd.MovieVersion)
	}
	if _, _, err = f.s.QueueMovieVersion(ctx, releases[0].ID, "uhd"); err == nil {
		t.Fatal("one release reused for two targets")
	}
	if _, _, err = f.s.Queue(ctx, releases[2].ID); err == nil {
		t.Fatal("unversioned acquisition bypassed reservations")
	}
	for _, job := range []Job{hd, uhd} {
		data := []byte("generated " + job.MovieVersion + " movie fixture")
		source := f.source(t, job, "movie.mkv", data)
		if _, err = f.db.Exec(`UPDATE downloads SET state='importing',lease_owner='versions-test',lease_until=unixepoch()+60 WHERE id=?`, job.ID); err != nil {
			t.Fatal(err)
		}
		owned, e := f.s.Job(ctx, job.ID)
		if e != nil {
			t.Fatal(e)
		}
		if job.MovieVersion == "uhd" {
			if e = f.s.importer.Execute(ctx, owned, source, int64(len(data))); e == nil {
				t.Fatal("HD video accepted for 4K reservation")
			}
			f.s.importer.Prober = existingProbeFunc(func(context.Context, *os.File) (Probe, error) {
				return Probe{Codec: "hevc", Width: 3840, Height: 1600, Duration: 90}, nil
			})
		}
		if e = f.s.importer.Execute(ctx, owned, source, int64(len(data))); e != nil {
			t.Fatal(e)
		}
		original, e := os.ReadFile(filepath.Join(f.cfg.DownloadsDir, filepath.FromSlash(source)))
		if e != nil || string(original) != string(data) {
			t.Fatal("source changed", e)
		}
	}
	f.reopen(t)
	f.s.indexer = indexer
	details, err := f.s.Details(ctx, f.movie.ID, 1)
	if err != nil || len(details.Files) != 2 {
		t.Fatal(details, err)
	}
	if details.Files[0].Path == details.Files[1].Path {
		t.Fatal("versions share a destination")
	}
	if !strings.Contains(details.Files[0].Path, "[4K]") && !strings.Contains(details.Files[1].Path, "[4K]") {
		t.Fatal(details.Files)
	}
	releases, err = f.s.Search(ctx, f.movie.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	additional, created, err := f.s.QueueMovieVersion(ctx, releases[2].ID, "additional")
	if err != nil || !created {
		t.Fatal(additional, err)
	}
	data := []byte("a separate edition")
	source := f.source(t, additional, "movie.mkv", data)
	if _, err = f.db.Exec(`UPDATE downloads SET state='importing',lease_owner='versions-test',lease_until=unixepoch()+60 WHERE id=?`, additional.ID); err != nil {
		t.Fatal(err)
	}
	additional, err = f.s.Job(ctx, additional.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.s.importer.Execute(ctx, additional, source, int64(len(data))); err != nil {
		t.Fatal(err)
	}
	details, err = f.s.Details(ctx, f.movie.ID, 1)
	if err != nil || details.FileCount != 3 {
		t.Fatal(details, err)
	}
}

func TestMovieVersionsAreOptInAndRequireCorrectReleaseLabels(t *testing.T) {
	f := setupWorkflow(t)
	ctx := context.Background()
	f.s.indexer = &versionsIndexer{}
	releases, err := f.s.Search(ctx, f.movie.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = f.s.QueueMovieVersion(ctx, releases[0].ID, "hd"); err == nil {
		t.Fatal("disabled version policy queued")
	}
	if _, err = f.db.Exec(`INSERT INTO movie_version_settings(media_id,enabled,hd_profile_id,uhd_profile_id) VALUES(?,1,1,2)`, f.movie.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err = f.s.QueueMovieVersion(ctx, releases[0].ID, "uhd"); err == nil {
		t.Fatal("1080p release filled 4K target")
	}
	if _, _, err = f.s.QueueMovieVersion(ctx, releases[1].ID, "hd"); err == nil {
		t.Fatal("4K release filled HD target")
	}
	if _, _, err = f.s.QueueMovieVersion(ctx, releases[0].ID, "bogus"); err == nil {
		t.Fatal("invalid version accepted")
	}
}

func TestExistingMovieVersionCanRegisterDuringIndependentDownload(t *testing.T) {
	f := setupWorkflow(t)
	ctx := context.Background()
	existingVideo(t, f, "Movies/Test Movie (2026)/Test.Movie.2026.1080p.SDR.mkv", []byte("existing version"))
	scan, err := f.s.StartLibraryScan(ctx, "movie")
	if err != nil {
		t.Fatal(err)
	}
	page := finishScan(t, f, scan.ID)
	job := f.queue(t)
	if err = f.s.RegisterDiscovered(ctx, page.Items[0].ID, ExistingRegistration{MediaID: f.movie.ID, Reviewed: true}); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.ProcessLibraryNext(ctx); err != nil {
		t.Fatal(err)
	}
	details, err := f.s.Details(ctx, f.movie.ID, 1)
	if err != nil || details.FileCount != 1 {
		t.Fatal(details, err)
	}
	saved, err := f.s.Job(ctx, job.ID)
	if err != nil || saved.State != "queued" {
		t.Fatal(saved, err)
	}
}

func TestMovieVersionsReplacementRetryCannotSwitchFiles(t *testing.T) {
	f := setupWorkflow(t)
	ctx := context.Background()
	scan := automaticMovieScan(t, f, "Movies/Test Movie (2026)/HD.mkv", "Movies/Test Movie (2026)/4K.mkv")
	finishAutomatic(t, f, scan.ID)
	details, err := f.s.Details(ctx, f.movie.ID, 1)
	if err != nil || details.FileCount != 2 {
		t.Fatal(details, err)
	}
	releases, err := f.s.Search(ctx, f.movie.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	job, _, err := f.s.QueueReplacement(ctx, releases[0].ID, details.Files[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if retry, created, e := f.s.QueueReplacement(ctx, releases[0].ID, details.Files[0].ID); e != nil || created || retry.ID != job.ID {
		t.Fatal(retry, created, e)
	}
	if _, _, err = f.s.QueueReplacement(ctx, releases[0].ID, details.Files[1].ID); err == nil {
		t.Fatal("idempotent release switched replacement target")
	}
	releases, err = f.s.Search(ctx, f.movie.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = f.s.QueueReplacement(ctx, releases[0].ID, details.Files[1].ID); err == nil {
		t.Fatal("same torrent hash switched replacement target")
	}
}
