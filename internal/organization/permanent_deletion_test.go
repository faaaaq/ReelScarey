package organization

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/faaaaq/ReelScarey/internal/filecheck"
)

func TestPermanentDeletionUnlinksVideoAndSubtitlesWithoutTrash(t *testing.T) {
	ctx := context.Background()
	f := newReviewFixture(t)
	f.movie(t, 1, "Movies/Film/movie.mkv", []byte("registered media"))
	f.movie(t, 2, "Movies/Other/other.mkv", []byte("unselected media"))
	video := filepath.Join(f.cfg.MediaDir, "Movies/Film/movie.mkv")
	sub := filepath.Join(f.cfg.MediaDir, "Movies/Film/movie.en.srt")
	if err := os.WriteFile(sub, []byte("owned subtitles"), 0600); err != nil {
		t.Fatal(err)
	}
	seed := filepath.Join(f.cfg.DownloadsDir, "seed.mkv")
	if err := os.Link(video, seed); err != nil {
		t.Fatal(err)
	}
	p, err := f.s.PreviewDeletion(ctx, DeletionRequest{Selection: "video_files", IDs: []int64{1}, Mode: "delete"})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Removal.Assets) != 2 {
		t.Fatalf("assets: %+v", p.Removal.Assets)
	}
	for _, a := range p.Removal.Assets {
		if a.Destination != "" {
			t.Fatal("permanent deletion has a retained destination", a)
		}
	}
	if _, err = f.s.Apply(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if err = New(f.db, f.cfg).Tick(ctx); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{video, sub, filepath.Join(f.cfg.MediaDir, ".reelscarey-trash")} {
		if _, err = os.Stat(name); !os.IsNotExist(err) {
			t.Fatalf("retained path %s: %v", name, err)
		}
	}
	if b, err := os.ReadFile(seed); err != nil || string(b) != "registered media" {
		t.Fatalf("seed changed: %q %v", b, err)
	}
	if _, err = os.Stat(filepath.Join(f.cfg.MediaDir, "Movies/Other/other.mkv")); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = f.db.QueryRow(`SELECT COUNT(*) FROM media_files WHERE id=1`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("registration remains: %d %v", count, err)
	}
}

func TestPermanentDeletionReviewDoesNotHashAnEntireLargeVideo(t *testing.T) {
	f := newReviewFixture(t)
	f.movie(t, 1, "Movies/large.mkv", []byte("fixture"))
	file, err := os.OpenFile(filepath.Join(f.cfg.MediaDir, "Movies/large.mkv"), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	// Unix truncation creates a sparse fixture; avoid allocating a huge NTFS file.
	size := int64(16 << 30)
	if runtime.GOOS == "windows" {
		size = 1 << 20
	}
	if err = file.Truncate(size); err != nil {
		t.Fatal(err)
	}
	observation, err := filecheck.Capture(context.Background(), file)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(observation)
	if _, err = f.db.Exec(`UPDATE media_files SET size_bytes=?,sha256='',verification_json=? WHERE id=1`, size, string(raw)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	p, err := f.s.PreviewDeletion(ctx, DeletionRequest{Selection: "video_files", IDs: []int64{1}, Mode: "delete"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Removal.Observations["Movies/large.mkv"].Sample == "" {
		t.Fatal("missing bounded file observation")
	}
	var sha string
	if err = f.db.QueryRow(`SELECT sha256 FROM media_files WHERE id=1`).Scan(&sha); err != nil || sha != "" {
		t.Fatalf("review promoted a full checksum: %q %v", sha, err)
	}
}

func TestPermanentDeletionRefusesChangedContentEvenWithRestoredMtime(t *testing.T) {
	f := newReviewFixture(t)
	f.movie(t, 1, "Movies/changed.mkv", []byte("original media"))
	p, err := f.s.PreviewDeletion(context.Background(), DeletionRequest{Selection: "video_files", IDs: []int64{1}, Mode: "delete"})
	if err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(f.cfg.MediaDir, "Movies/changed.mkv")
	file, err := os.OpenFile(name, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = file.WriteAt([]byte("X"), 0)
	file.Close()
	if err != nil {
		t.Fatal(err)
	}
	mtime := time.Unix(0, p.Removal.Assets[0].Mtime)
	if err = os.Chtimes(name, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.Apply(context.Background(), p.ID); err == nil {
		t.Fatal("changed media was accepted")
	}
	if _, err = os.Stat(name); err != nil {
		t.Fatal(err)
	}
}

func TestPermanentDeletionCatalogFailureLeavesFilesAndRowsIntact(t *testing.T) {
	f := newReviewFixture(t)
	f.movie(t, 1, "Movies/retained.mkv", []byte("retained"))
	p, err := f.s.PreviewDeletion(context.Background(), DeletionRequest{Selection: "video_files", IDs: []int64{1}, Mode: "delete"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.Apply(context.Background(), p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(`CREATE TRIGGER refuse_deletion BEFORE DELETE ON media_files BEGIN SELECT RAISE(ABORT,'injected catalog failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err = f.s.Tick(context.Background()); err == nil {
		t.Fatal("catalog failure was ignored")
	}
	var count int
	if err = f.db.QueryRow(`SELECT COUNT(*) FROM media_files WHERE id=1`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("catalog changed: %d %v", count, err)
	}
	if _, err = os.Stat(filepath.Join(f.cfg.MediaDir, "Movies/retained.mkv")); err != nil {
		t.Fatal(err)
	}
}

func TestPermanentDeletionResumesAfterUnlinkBeforeCompletion(t *testing.T) {
	f := newReviewFixture(t)
	f.movie(t, 1, "Movies/resume.mkv", []byte("resume"))
	p, err := f.s.PreviewDeletion(context.Background(), DeletionRequest{Selection: "titles", IDs: []int64{1}, Mode: "delete"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.Apply(context.Background(), p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(`CREATE TRIGGER refuse_completion BEFORE UPDATE OF state ON organization_plans WHEN NEW.state='completed' BEGIN SELECT RAISE(ABORT,'injected completion failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err = f.s.Tick(context.Background()); err == nil {
		t.Fatal("completion failure was ignored")
	}
	p, err = f.s.Plan(context.Background(), p.ID)
	if err != nil || p.Phase != "committed" || p.State != "failed" {
		t.Fatalf("recovery journal: %+v %v", p, err)
	}
	var completedEvents int
	if err = f.db.QueryRow(`SELECT count(*) FROM activity_events WHERE state='permanently deleted'`).Scan(&completedEvents); err != nil || completedEvents != 0 {
		t.Fatal("deletion completion was reported before final commit", completedEvents, err)
	}
	if _, err = os.Stat(filepath.Join(f.cfg.MediaDir, "Movies/resume.mkv")); !os.IsNotExist(err) {
		t.Fatal("file remains", err)
	}
	if _, err = f.db.Exec(`DROP TRIGGER refuse_completion`); err != nil {
		t.Fatal(err)
	}
	if _, err = New(f.db, f.cfg).Apply(context.Background(), p.ID); err != nil {
		t.Fatal(err)
	}
	if err = New(f.db, f.cfg).Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	p, err = f.s.Plan(context.Background(), p.ID)
	if err != nil || p.State != "completed" {
		t.Fatalf("resume failed: %+v %v", p, err)
	}
	if err = f.db.QueryRow(`SELECT count(*) FROM activity_events WHERE state='permanently deleted'`).Scan(&completedEvents); err != nil || completedEvents != 1 {
		t.Fatal("completion event missing or duplicated", completedEvents, err)
	}
}

func TestPermanentDeletionReviewReportsBusyWithoutWaitingForMediaWork(t *testing.T) {
	f := newReviewFixture(t)
	mediaGate.Lock()
	done := make(chan error, 1)
	go func() {
		_, err := f.s.PreviewDeletion(context.Background(), DeletionRequest{Selection: "titles", IDs: []int64{1}, Mode: "delete"})
		done <- err
	}()
	select {
	case err := <-done:
		mediaGate.Unlock()
		if err == nil {
			t.Fatal("busy operation was not reported")
		}
	case <-time.After(time.Second):
		mediaGate.Unlock()
		<-done
		t.Fatal("review waited behind a long media operation")
	}
}

func TestPermanentMusicDeletionUnlinksFilesAtEverySelectionLevel(t *testing.T) {
	for selection, selected := range map[string]int64{"artists": 1, "albums": 2, "tracks": 3, "music_files": 6} {
		t.Run(selection, func(t *testing.T) {
			f := newReviewFixture(t)
			_, err := f.db.Exec(`INSERT INTO music_artists(id,musicbrainz_id,name,status,created_at,updated_at) VALUES(1,'fixture-artist','Artist','available',1,1);
			 INSERT INTO music_albums(id,artist_id,musicbrainz_id,title,status,created_at,updated_at) VALUES(2,1,'fixture-album','Album','available',1,1);
			 INSERT INTO music_tracks(id,album_id,disc,number,title,created_at,updated_at) VALUES(3,2,1,1,'Track',1,1);
			 INSERT INTO music_files(id,artist_id,album_id,track_id,relative_path,size_bytes,imported_at) VALUES(6,1,2,3,'Music/song.flac',5,1)`)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.MkdirAll(filepath.Join(f.cfg.MediaDir, "Music"), 0700); err != nil {
				t.Fatal(err)
			}
			name := filepath.Join(f.cfg.MediaDir, "Music/song.flac")
			if err = os.WriteFile(name, []byte("music"), 0600); err != nil {
				t.Fatal(err)
			}
			guest := filepath.Join(f.cfg.MediaDir, "Music/guest.flac")
			if selection == "artists" {
				if _, err = f.db.Exec(`INSERT INTO music_artists(id,musicbrainz_id,name,status,created_at,updated_at) VALUES(8,'guest','Guest','available',1,1);
				 INSERT INTO music_files(id,artist_id,album_id,track_id,relative_path,size_bytes,imported_at) VALUES(7,8,2,3,'Music/guest.flac',5,1)`); err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(guest, []byte("guest"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			p, err := f.s.PreviewDeletion(context.Background(), DeletionRequest{Selection: selection, IDs: []int64{selected}, Mode: "delete"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = f.s.Apply(context.Background(), p.ID); err != nil {
				t.Fatal(err)
			}
			if err = f.s.Tick(context.Background()); err != nil {
				t.Fatal(err)
			}
			if _, err = os.Stat(name); !os.IsNotExist(err) {
				t.Fatal("music file remains", err)
			}
			if selection == "artists" {
				if _, err = os.Stat(guest); !os.IsNotExist(err) {
					t.Fatal("album guest file was orphaned on disk", err)
				}
			}
			if _, err = os.Stat(filepath.Join(f.cfg.MediaDir, ".reelscarey-trash")); !os.IsNotExist(err) {
				t.Fatal("trash created", err)
			}
		})
	}
}

func TestPermanentDeletionRecoveryRefusesAnUnreviewedReplacement(t *testing.T) {
	f := newReviewFixture(t)
	f.movie(t, 1, "Movies/replaced.mkv", []byte("original"))
	p, err := f.s.PreviewDeletion(context.Background(), DeletionRequest{Selection: "video_files", IDs: []int64{1}, Mode: "delete"})
	if err != nil {
		t.Fatal(err)
	}
	// Recreate the durable state after catalog commit and before file unlink.
	p.Phase = "committed"
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := f.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err = deleteSelectionRows(context.Background(), tx, p.Removal); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`UPDATE organization_plans SET snapshot=?,state='queued' WHERE id=?`, string(raw), p.ID); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(f.cfg.MediaDir, "Movies/replaced.mkv")
	held := filepath.Join(f.cfg.MediaDir, "Movies/original-held.mkv")
	if err = os.Rename(name, held); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(name, []byte("new file"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = New(f.db, f.cfg).Tick(context.Background()); err == nil {
		t.Fatal("replacement was deleted")
	}
	if data, err := os.ReadFile(name); err != nil || string(data) != "new file" {
		t.Fatal("replacement changed", string(data), err)
	}
	if err = EnsureMediaAvailable(context.Background(), f.db); err == nil {
		t.Fatal("unfinished cleanup did not reserve the library")
	}
	if err = os.Remove(name); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(held, name); err != nil {
		t.Fatal(err)
	}
	if _, err = New(f.db, f.cfg).Apply(context.Background(), p.ID); err != nil {
		t.Fatal(err)
	}
	if err = New(f.db, f.cfg).Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(name); !os.IsNotExist(err) {
		t.Fatal("original not deleted after recovery", err)
	}
}
