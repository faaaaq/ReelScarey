package transfer

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/config"
	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/library"
	"github.com/faaaaq/ReelScarey/internal/metadata"
	"github.com/faaaaq/ReelScarey/internal/securestore"
)

type restoreRemovalFixture struct {
	*Service
	db       *sql.DB
	dir      string
	seriesID int64
	revision int64
	episode1 int64
	fileBody []byte
	relative string
}

func setupRestoreRemovalFixture(t *testing.T) *restoreRemovalFixture {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	db, e := database.Open(ctx, dir)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	secrets, e := securestore.New(ctx, db, dir)
	if e != nil {
		t.Fatal(e)
	}
	cfg := config.Config{ConfigDir: dir, MediaDir: filepath.Join(dir, "media"), DownloadsDir: filepath.Join(dir, "downloads")}
	s := New(db, secrets, cfg, NewQBittorrent(), NewTorznab())
	show := metadata.Series{Movie: metadata.Movie{TMDbID: 789, Title: "Removal fixture"}, Seasons: []metadata.SeasonSummary{{TMDbID: 790, Number: 1, Title: "Season 1", EpisodeCount: 2}}}
	item, _, e := library.New(db).AddSeries(ctx, show, true)
	if e != nil {
		t.Fatal(e)
	}
	if e = library.New(db).SaveSeason(ctx, item.ID, metadata.Season{SeasonSummary: show.Seasons[0], Episodes: []metadata.Episode{{TMDbID: 791, Number: 1, Title: "One"}, {TMDbID: 792, Number: 2, Title: "Two"}}}, 1); e != nil {
		t.Fatal(e)
	}
	var revision, episode2 int64
	if e = db.QueryRow(`SELECT revision FROM series_details WHERE media_id=?`, item.ID).Scan(&revision); e != nil {
		t.Fatal(e)
	}
	var episode1 int64
	if e = db.QueryRow(`SELECT e.id FROM episodes e JOIN seasons s ON s.id=e.season_id WHERE s.series_id=? AND e.number=1`, item.ID).Scan(&episode1); e != nil {
		t.Fatal(e)
	}
	if e = db.QueryRow(`SELECT e.id FROM episodes e JOIN seasons s ON s.id=e.season_id WHERE s.series_id=? AND e.number=2`, item.ID).Scan(&episode2); e != nil {
		t.Fatal(e)
	}
	relative := "TV/Removal fixture/s01e01.mkv"
	body := []byte("episode fixture bytes")
	target := filepath.Join(cfg.MediaDir, filepath.FromSlash(relative))
	if e = os.MkdirAll(filepath.Dir(target), 0o750); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(target, body, 0o600); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(`INSERT INTO media_files(id,series_id,relative_path,size_bytes,sha256,probe_json,imported_at,origin) VALUES(9001,?,'TV/Removal fixture/s01e01.mkv',?,?, '{}',0,'existing')`, item.ID, len(body), "x"); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(`INSERT INTO episode_files(episode_id,media_file_id) VALUES(?,9001),(?,9001)`, episode1, episode2); e != nil {
		t.Fatal(e)
	}
	// The delete path refuses files shared with other episodes, so the second
	// registration is dropped again: exactly one episode owns the file.
	if _, e = db.Exec(`DELETE FROM episode_files WHERE episode_id=?`, episode2); e != nil {
		t.Fatal(e)
	}
	return &restoreRemovalFixture{Service: s, db: db, dir: dir, seriesID: item.ID, revision: revision, episode1: episode1, fileBody: body, relative: relative}
}

// A failed removal transaction must put the staged file back where the
// catalog still points: a restore failure must never be silent (RS-0003).
func TestFailedRemovalRestoresStagedFile(t *testing.T) {
	f := setupRestoreRemovalFixture(t)
	ctx := context.Background()
	if _, e := f.db.Exec(`CREATE TRIGGER fail_removal_event BEFORE INSERT ON activity_events WHEN NEW.state='episode and its file deleted' BEGIN SELECT RAISE(ABORT, 'injected event write failure'); END;`); e != nil {
		t.Fatal(e)
	}
	e := f.RemoveEpisodeFile(ctx, f.seriesID, f.revision, f.episode1, true)
	if e == nil {
		t.Fatal("the injected activity failure must be returned")
	}
	target := filepath.Join(f.cfg.MediaDir, filepath.FromSlash(f.relative))
	body, e2 := os.ReadFile(target)
	if e2 != nil || string(body) != string(f.fileBody) {
		t.Fatalf("staged file was not restored to the catalog path: %v", e2)
	}
	entries, _ := os.ReadDir(filepath.Dir(target))
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".reelscarey-removing-") {
			t.Fatalf("staged copy was left behind: %s", entry.Name())
		}
	}
	var rows int
	if e = f.db.QueryRow(`SELECT COUNT(*) FROM media_files WHERE id=9001`).Scan(&rows); e != nil || rows != 1 {
		t.Fatal("registration must survive the rolled-back transaction", rows, e)
	}
}

// When the restore itself fails, the divergence is journaled as an actionable
// event and the error names both causes (RS-0003). The failure is injected at
// the exact restore unit: the catalog path is occupied by a directory, which
// makes the rename fail on every platform.
func TestRestoreFailureIsJournaledAndSurfaced(t *testing.T) {
	f := setupRestoreRemovalFixture(t)
	ctx := context.Background()
	target := filepath.Join(f.cfg.MediaDir, filepath.FromSlash(f.relative))
	// The live file is moved aside into the staged position, exactly like a
	// real removal staging step, then the target is occupied so the restore
	// rename fails.
	staged := target + ".reelscarey-removing-test"
	if e := os.Rename(target, staged); e != nil {
		t.Fatal(e)
	}
	if e := os.Mkdir(target, 0o750); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(target, "blocked.bin"), []byte("x"), 0o600); e != nil {
		t.Fatal(e)
	}
	err := f.restoreStagedMedia(ctx, staged, f.relative)
	if err == nil {
		t.Fatal("the blocked restore must fail")
	}
	var objectID string
	if e := f.db.QueryRow(`SELECT object_id FROM activity_events WHERE state='restore failed; manual repair needed'`).Scan(&objectID); e != nil {
		t.Fatal("restore divergence was not journaled:", e)
	}
	if objectID != f.relative {
		t.Fatalf("divergence event lost the path: %q", objectID)
	}
	// The staged copy is still there for manual repair, and the divergence
	// survives a reopen (restart behavior) because the event is durable.
	if _, e := os.Stat(staged); e != nil {
		t.Fatal("the staged copy must remain for manual repair:", e)
	}
	if e := f.db.Close(); e != nil {
		t.Fatal(e)
	}
	reopened, e := database.Open(ctx, f.dir)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	var state string
	if e := reopened.QueryRow(`SELECT state FROM activity_events WHERE state='restore failed; manual repair needed'`).Scan(&state); e != nil {
		t.Fatal("divergence event did not survive a reopen:", e)
	}
}

// A staged copy that cannot be removed after a successful commit is journaled
// instead of silently accumulating (RS-0003).
func TestStagedCopyCleanupFailureIsJournaled(t *testing.T) {
	f := setupRestoreRemovalFixture(t)
	ctx := context.Background()
	staged := filepath.Join(f.cfg.MediaDir, filepath.FromSlash("TV/Removal fixture/.reelscarey-removing-leftover.mkv"))
	if e := os.Mkdir(staged, 0o750); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(staged, "inner.bin"), []byte("x"), 0o600); e != nil {
		t.Fatal(e)
	}
	f.removeStagedMedia(ctx, staged, f.relative)
	var state string
	if e := f.db.QueryRow(`SELECT state FROM activity_events WHERE state='staged copy removal failed; delete it manually'`).Scan(&state); e != nil {
		t.Fatal("cleanup failure was not journaled:", e)
	}
	if _, e := os.Stat(staged); e != nil {
		t.Fatal("the leftover copy must remain for manual removal:", e)
	}
}
