package transfer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func existingVideo(t *testing.T, f *workflowFixture, relative string, body []byte) string {
	t.Helper()
	name := filepath.Join(f.cfg.MediaDir, filepath.FromSlash(relative))
	if e := os.MkdirAll(filepath.Dir(name), 0755); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(name, body, 0644); e != nil {
		t.Fatal(e)
	}
	return name
}
func finishScan(t *testing.T, f *workflowFixture, id string) DiscoveredPage {
	t.Helper()
	ctx := context.Background()
	for range 100 {
		v, e := f.s.LibraryScan(ctx, id)
		if e != nil {
			t.Fatal(e)
		}
		if v.State == "completed" {
			out, e := f.s.DiscoveredFiles(ctx, id, 1, "")
			if e != nil {
				t.Fatal(e)
			}
			return out
		}
		if v.State == "failed" {
			t.Fatal(v.Message)
		}
		if _, e = f.s.ProcessLibraryNext(ctx); e != nil {
			t.Fatal(e)
		}
	}
	t.Fatal("scan did not finish")
	return DiscoveredPage{}
}
func TestDiscoveryHintRemovesShowAndTrailingSeparators(t *testing.T) {
	for _, v := range []struct{ name, kind, want string }{
		{"anime/Hunter x Hunter (2011) [tmdb-46298]/Season 2/Hunter x Hunter - S02E63 - A x Hard x Master.mkv", "series", "Hunter x Hunter"},
		{"anime/Hunter x Hunter (2011) [tmdb-46298]/Season 2/Hunter x Hunter - S02E63 - A x Hard x Master.mp4", "series", "Hunter x Hunter"},
		{"TV/Example Show/Season 02/Example Show - S02E04 - Promise.mkv", "series", "Example Show"},
		{"Movies/Test Movie (2026)/Test.Movie.2026.mp4", "movie", "Test Movie"},
		{"Movies/Test Movie (2026)/Test.Movie.2026.1080p.mkv", "movie", "Test Movie"},
	} {
		if got := discoveryHint(v.name, v.kind); got != v.want {
			t.Fatalf("%s: hint %q, want %q", v.name, got, v.want)
		}
	}
}

func TestUnverifiedVideoRegistrationFollowsOwnerPreference(t *testing.T) {
	f := setupWorkflow(t)
	ctx := context.Background()
	existingVideo(t, f, "Movies/Test Movie (2026)/Test.Movie.2026.mp4", []byte("owned disposable unverified media"))
	f.s.importer.Prober = existingProbeFunc(func(context.Context, *os.File) (Probe, error) {
		return Probe{}, problem("invalid_media", "FFprobe could not verify a playable video. The source was left unchanged.")
	})
	scan, e := f.s.StartLibraryScan(ctx, "movie")
	if e != nil {
		t.Fatal(e)
	}
	page := finishScan(t, f, scan.ID)
	if e = f.s.RegisterDiscovered(ctx, page.Items[0].ID, ExistingRegistration{MediaID: f.movie.ID, Reviewed: true}); e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.ProcessLibraryNext(ctx); e != nil {
		t.Fatal(e)
	}
	updated, e := f.s.LibraryScan(ctx, scan.ID)
	if e != nil || updated.NeedsAttention != 1 || updated.Registered != 0 {
		t.Fatal("registered without the owner preference", updated, e)
	}
	if _, e = f.db.Exec(`UPDATE settings SET register_unverified_video=1; DELETE FROM metadata_refresh_queue`); e != nil {
		t.Fatal(e)
	}
	// Blocked files return through the reviewed retry path, exactly like the
	// interface's "Review match" action on a needs-attention row.
	if e = f.s.RegisterDiscovered(ctx, page.Items[0].ID, ExistingRegistration{MediaID: f.movie.ID, Reviewed: true}); e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.ProcessLibraryNext(ctx); e != nil {
		t.Fatal(e)
	}
	updated, e = f.s.LibraryScan(ctx, scan.ID)
	if e != nil || updated.Registered != 1 || updated.NeedsAttention != 0 {
		t.Fatal("preference did not allow registration", updated, e)
	}
	var queued int
	if e = f.db.QueryRow(`SELECT COUNT(*) FROM metadata_refresh_queue WHERE media_id=?`, f.movie.ID).Scan(&queued); e != nil || queued != 1 {
		t.Fatal("existing-media registration did not queue a metadata refresh", queued, e)
	}
	var raw string
	if e = f.db.QueryRow(`SELECT probe_json FROM media_files WHERE movie_id=?`, f.movie.ID).Scan(&raw); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(raw, "1920") || strings.Contains(raw, `"codec":"h264"`) {
		t.Fatal("fabricated video facts", raw)
	}
	var message string
	if e = f.db.QueryRow(`SELECT message FROM library_scan_files WHERE scan_id=?`, scan.ID).Scan(&message); e != nil || !strings.Contains(message, "without video verification") {
		t.Fatal(message, e)
	}
	// A file ffprobe reads but finds no video stream in is never registered,
	// even with the preference enabled.
	f.s.importer.Prober = existingProbeFunc(func(context.Context, *os.File) (Probe, error) {
		return Probe{}, problem("no_video_stream", "The selected file has no usable video stream.")
	})
	existingVideo(t, f, "Movies/Test Movie (2026)/Test.Movie.2026.extra.mp4", []byte("owned second unverified media"))
	next, e := f.s.StartLibraryScan(ctx, "movie")
	if e != nil {
		t.Fatal(e)
	}
	again := finishScan(t, f, next.ID)
	if e = f.s.RegisterDiscovered(ctx, again.Items[0].ID, ExistingRegistration{MediaID: f.movie.ID, Reviewed: true}); e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.ProcessLibraryNext(ctx); e != nil {
		t.Fatal(e)
	}
	finished, e := f.s.LibraryScan(ctx, next.ID)
	if e != nil || finished.NeedsAttention != 1 || finished.Registered != 0 {
		t.Fatal("a non-video file was registered", finished, e)
	}
}

func TestExistingMovieDiscoveryReviewedRegistrationAndRestart(t *testing.T) {
	f := setupWorkflow(t)
	ctx := context.Background()
	body := []byte("owned disposable unit media")
	name := existingVideo(t, f, "Movies/Test Movie (2026)/Test.Movie.2026.mp4", body)
	before, _ := os.Stat(name)
	existingVideo(t, f, "Movies/Test Movie (2026)/sample.mp4", body)
	existingVideo(t, f, "Movies/Test Movie (2026)/notes.txt", body)
	if e := f.s.Remove(ctx, "qbittorrent"); e != nil {
		t.Fatal(e)
	}
	if e := f.s.Remove(ctx, "torznab"); e != nil {
		t.Fatal(e)
	}
	scan, e := f.s.StartLibraryScan(ctx, "movie")
	if e != nil {
		t.Fatal(e)
	}
	// A queued scan over the same roots fences a duplicate scan until it is
	// cancelled or finishes.
	if other, e := f.s.StartLibraryScan(ctx, "movie"); e == nil {
		t.Fatal("overlapping scan was not refused", other)
	}
	// Persist discovery work before reopening, without using a client/provider.
	if _, e = f.s.ProcessLibraryNext(ctx); e != nil {
		t.Fatal(e)
	}
	f.reopen(t)
	page := finishScan(t, f, scan.ID)
	if page.Total != 1 || page.Items[0].State != "review" || page.Items[0].Hint != "Test Movie" {
		t.Fatal(page)
	}
	if page.Scan.NeedsMatch != 1 || page.Scan.NeedsAttention != 0 {
		t.Fatal("incorrect unmatched counts", page.Scan)
	}
	file := page.Items[0]
	if e = f.s.RegisterDiscovered(ctx, file.ID, ExistingRegistration{MediaID: f.movie.ID}); e == nil {
		t.Fatal("review bypassed")
	}
	if e = f.s.RegisterDiscovered(ctx, file.ID, ExistingRegistration{MediaID: f.movie.ID, Reviewed: true}); e != nil {
		t.Fatal(e)
	}
	f.reopen(t)
	if _, e = f.s.ProcessLibraryNext(ctx); e != nil {
		t.Fatal(e)
	}
	updated, e := f.s.LibraryScan(ctx, scan.ID)
	if e != nil || updated.NeedsMatch != 0 || updated.Registered != 1 {
		t.Fatal("counts did not update", updated, e)
	}
	details, e := f.s.Details(ctx, f.movie.ID, 1)
	if e != nil || details.FileCount != 1 || len(details.Files) != 1 || details.HistoryCount != 0 || details.Files[0].Origin != "existing" || details.Files[0].DownloadID != "" || details.Files[0].Path != name {
		t.Fatal(details, e)
	}
	var hash string
	if e = f.db.QueryRow(`SELECT sha256 FROM media_files WHERE movie_id=?`, f.movie.ID).Scan(&hash); e != nil {
		t.Fatal(e)
	}
	want := sha256.Sum256(body)
	if hash != hex.EncodeToString(want[:]) {
		t.Fatal("incorrect file fingerprint")
	}
	if e = f.s.RegisterDiscovered(ctx, file.ID, ExistingRegistration{MediaID: f.movie.ID, Reviewed: true}); e != nil {
		t.Fatal("retry not idempotent", e)
	}
	after, _ := os.Stat(name)
	actual, _ := os.ReadFile(name)
	if !os.SameFile(before, after) || before.ModTime() != after.ModTime() || string(actual) != string(body) {
		t.Fatal("existing video changed")
	}
	var jobs int
	_ = f.db.QueryRow(`SELECT COUNT(*) FROM downloads`).Scan(&jobs)
	if jobs != 0 {
		t.Fatal("synthetic download created")
	}
	next, e := f.s.StartLibraryScan(ctx, "movie")
	if e != nil {
		t.Fatal(e)
	}
	again := finishScan(t, f, next.ID)
	if again.Total != 0 {
		t.Fatal("registered file rediscovered")
	}
	if e = f.s.RemoveLibraryScan(ctx, scan.ID); e != nil {
		t.Fatal(e)
	}
	details, e = f.s.Details(ctx, f.movie.ID, 1)
	if e != nil || details.FileCount != 1 {
		t.Fatal("forgetting scan removed media", e)
	}
}
func TestExistingTVSharedFileAndTargetConflicts(t *testing.T) {
	f, id, targets := setupTV(t)
	ctx := context.Background()
	oldJob := queueSeason(t, f, id)
	if _, e := f.db.Exec(`UPDATE downloads SET state='blocked',error_code='fixture_failure' WHERE id=?`, oldJob.ID); e != nil {
		t.Fatal(e)
	}
	if e := f.s.Cancel(ctx, oldJob.ID); e != nil {
		t.Fatal(e)
	}
	existingVideo(t, f, "TV Shows/Test Show/Season 1/Test.Show.S01E01-E02.mp4", []byte("owned fixture"))
	scan, e := f.s.StartLibraryScan(ctx, "series")
	if e != nil {
		t.Fatal(e)
	}
	page := finishScan(t, f, scan.ID)
	file := page.Items[0]
	for _, episodes := range [][]int64{nil, {targets[0].EpisodeID, targets[2].EpisodeID}, {targets[0].EpisodeID, targets[0].EpisodeID}} {
		if e = f.s.RegisterDiscovered(ctx, file.ID, ExistingRegistration{MediaID: id, Episodes: episodes, Reviewed: true}); e == nil {
			t.Fatal("invalid episode mapping accepted")
		}
	}
	if e = f.s.RegisterDiscovered(ctx, file.ID, ExistingRegistration{MediaID: id, Episodes: []int64{targets[1].EpisodeID, targets[0].EpisodeID}, Reviewed: true}); e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.ProcessLibraryNext(ctx); e != nil {
		t.Fatal(e)
	}
	details, e := f.s.Details(ctx, id, 1)
	if e != nil || details.FileCount != 1 || details.Files[0].Episodes != "S01E01, S01E02" || details.TotalSize != int64(len("owned fixture")) {
		t.Fatal(details, e)
	}
	if e = f.s.RegisterDiscovered(ctx, file.ID, ExistingRegistration{MediaID: id, Episodes: []int64{targets[2].EpisodeID}, Reviewed: true}); e == nil {
		t.Fatal("registered match silently changed")
	}
	if e = f.s.Retry(ctx, oldJob.ID); e == nil {
		t.Fatal("cancelled download reserved an episode now registered from existing media")
	}
}

type existingProbeFunc func(context.Context, *os.File) (Probe, error)

func (f existingProbeFunc) Probe(ctx context.Context, file *os.File) (Probe, error) {
	return f(ctx, file)
}
func TestExistingChangedFilesAndLeaseFences(t *testing.T) {
	for _, mode := range []string{"before", "replace", "during", "lease"} {
		t.Run(mode, func(t *testing.T) {
			f := setupWorkflow(t)
			ctx := context.Background()
			name := existingVideo(t, f, "Movies/Test.Movie.2026.mp4", []byte("original generated bytes"))
			scan, e := f.s.StartLibraryScan(ctx, "movie")
			if e != nil {
				t.Fatal(e)
			}
			page := finishScan(t, f, scan.ID)
			id := page.Items[0].ID
			if e = f.s.RegisterDiscovered(ctx, id, ExistingRegistration{MediaID: f.movie.ID, Reviewed: true}); e != nil {
				t.Fatal(e)
			}
			switch mode {
			case "replace":
				stat, _ := os.Stat(name)
				if e = os.Rename(name, name+".old"); e != nil {
					t.Fatal(e)
				}
				if e = os.WriteFile(name, []byte(strings.Repeat("x", int(stat.Size()))), 0644); e != nil {
					t.Fatal(e)
				}
				if e = os.Chtimes(name, stat.ModTime(), stat.ModTime()); e != nil {
					t.Fatal(e)
				}
			case "before":
				if e = os.WriteFile(name, []byte("changed bytes"), 0644); e != nil {
					t.Fatal(e)
				}
			case "during":
				f.s.importer.Prober = existingProbeFunc(func(context.Context, *os.File) (Probe, error) {
					e := os.WriteFile(name, []byte("mutated generated bytes"), 0644)
					return Probe{Codec: "h264", Width: 1920, Height: 1080, Duration: 90}, e
				})
			case "lease":
				f.s.importer.Prober = existingProbeFunc(func(context.Context, *os.File) (Probe, error) {
					_, e := f.db.Exec(`UPDATE library_scan_files SET lease_owner='other',lease_until=unixepoch()+60 WHERE id=?`, id)
					return Probe{Codec: "h264", Width: 1920, Height: 1080, Duration: 90}, e
				})
			}
			if _, e = f.s.ProcessLibraryNext(ctx); e != nil {
				t.Fatal(e)
			}
			var count int
			_ = f.db.QueryRow(`SELECT COUNT(*) FROM media_files`).Scan(&count)
			if count != 0 {
				t.Fatal("stale source/target/lease registered")
			}
			if mode == "lease" {
				_, e = f.db.Exec(`UPDATE library_scan_files SET lease_until=0 WHERE id=?`, id)
				if e != nil {
					t.Fatal(e)
				}
				f.s.importer.Prober = f.probe
				f.reopen(t)
				if _, e = f.s.ProcessLibraryNext(ctx); e != nil {
					t.Fatal(e)
				}
				_ = f.db.QueryRow(`SELECT COUNT(*) FROM media_files`).Scan(&count)
				if count != 1 {
					t.Fatal("expired verification did not recover")
				}
			}
		})
	}
}
func TestExistingUnsafeDiscoveryAndCancellation(t *testing.T) {
	f := setupWorkflow(t)
	ctx := context.Background()
	body := []byte("owned data")
	existingVideo(t, f, "Movies/valid.mp4", body)
	outside := filepath.Join(t.TempDir(), "outside.mp4")
	if e := os.WriteFile(outside, body, 0644); e != nil {
		t.Fatal(e)
	}
	// Windows may require developer-mode privileges; Linux CI exercises this.
	linked := os.Symlink(outside, filepath.Join(f.cfg.MediaDir, "Movies", "linked.mp4")) == nil
	scan, e := f.s.StartLibraryScan(ctx, "movie")
	if e != nil {
		t.Fatal(e)
	}
	page := finishScan(t, f, scan.ID)
	if page.Total != 1 {
		t.Fatal("unexpected discovered paths", linked, page)
	}
	if e = f.s.SkipDiscovered(ctx, page.Items[0].ID); e != nil {
		t.Fatal(e)
	}
	if e = f.s.RegisterDiscovered(ctx, page.Items[0].ID, ExistingRegistration{MediaID: f.movie.ID, Reviewed: true}); e == nil {
		t.Fatal("skipped file queued")
	}
	scan, e = f.s.StartLibraryScan(ctx, "movie")
	if e != nil {
		t.Fatal(e)
	}
	if e = f.s.CancelLibraryScan(ctx, scan.ID); e != nil {
		t.Fatal(e)
	}
	f.reopen(t)
	if worked, e := f.s.ProcessLibraryNext(ctx); worked || e != nil {
		t.Fatal("cancelled scan resumed", e)
	}
	if _, e = f.s.StartLibraryScan(ctx, "../../outside"); e == nil {
		t.Fatal("arbitrary scan root accepted")
	}
	files, _ := os.ReadFile(outside)
	if string(files) != string(body) {
		t.Fatal("outside source modified")
	}
}

func TestRealExistingLibraryMedia(t *testing.T) {
	if os.Getenv("REELSCAREY_LIVE_EXISTING") != "1" {
		t.Skip("opt-in generated media and actual FFprobe")
	}
	f := setupWorkflow(t)
	ctx := context.Background()
	f.s.importer.Prober = FFprobe{}
	dir := filepath.Join(f.cfg.MediaDir, "Movies", "Owned generated video")
	if e := os.MkdirAll(dir, 0755); e != nil {
		t.Fatal(e)
	}
	name := filepath.Join(dir, "Owned.Generated.2026.mp4")
	cmd := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "color=c=navy:s=320x180:d=2", "-c:v", "libx264", "-pix_fmt", "yuv420p", name)
	hideProbeWindow(cmd)
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatal(string(out), e)
	}
	before, e := os.ReadFile(name)
	if e != nil {
		t.Fatal(e)
	}
	stat, _ := os.Stat(name)
	scan, e := f.s.StartLibraryScan(ctx, "movie")
	if e != nil {
		t.Fatal(e)
	}
	page := finishScan(t, f, scan.ID)
	if e = f.s.RegisterDiscovered(ctx, page.Items[0].ID, ExistingRegistration{MediaID: f.movie.ID, Reviewed: true}); e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.ProcessLibraryNext(ctx); e != nil {
		t.Fatal(e)
	}
	details, e := f.s.Details(ctx, f.movie.ID, 1)
	if e != nil || len(details.Files) != 1 || details.Files[0].Probe.Codec != "h264" || details.Files[0].Probe.Width != 320 || details.Files[0].Probe.Height != 180 || details.Files[0].Probe.Duration < 1 {
		t.Fatal(details, e)
	}
	after, _ := os.ReadFile(name)
	latest, _ := os.Stat(name)
	if string(before) != string(after) || !os.SameFile(stat, latest) || stat.ModTime() != latest.ModTime() {
		t.Fatal("real video modified")
	}
	if !strings.Contains(details.Files[0].Release, "Owned.Generated") {
		t.Fatal("original filename missing")
	}
	t.Log("PASS: generated video discovered, reviewed, hashed and FFprobe verified in place; bytes, identity and timestamp preserved; no download job created")
}

func TestExistingDiscoveryContinuesPastFormerAggregateCaps(t *testing.T) {
	f := setupWorkflow(t)
	ctx := context.Background()
	existingVideo(t, f, "Movies/Remaining.mp4", []byte("owned fixture"))
	scan, err := f.s.StartLibraryScanWithMatching(ctx, "movie", "", true)
	if err != nil {
		t.Fatal(err)
	}
	// Represent completed discovery batches without creating thousands of files.
	if _, err = f.db.ExecContext(ctx, `WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i<2000)
		INSERT INTO library_scan_dirs(scan_id,relative_path,state,examined)
		SELECT ?, 'Movies/previous-'||i, 'done', 100 FROM n`, scan.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.ExecContext(ctx, `WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i<10000)
		INSERT INTO library_scan_files(id,scan_id,relative_path,size_bytes,mtime,file_identity,updated_at)
		SELECT printf('%032x',i), ?, 'Movies/previous-'||i||'.mp4', 1, 1, 'fixture', 1 FROM n`, scan.ID); err != nil {
		t.Fatal(err)
	}
	page := finishScan(t, f, scan.ID)
	if page.Scan.Discovered != 10001 || page.Scan.Examined != 200001 || page.Scan.AutomaticMatching == nil || page.Scan.AutomaticMatching.State != "pending" {
		t.Fatal(page.Scan)
	}
}
