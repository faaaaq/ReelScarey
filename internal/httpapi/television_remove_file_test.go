package httpapi

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/faaaaq/ReelScarey/internal/auth"
	"github.com/faaaaq/ReelScarey/internal/config"
	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/library"
	"github.com/faaaaq/ReelScarey/internal/metadata"
	"github.com/faaaaq/ReelScarey/internal/securestore"
	"github.com/faaaaq/ReelScarey/internal/transfer"
)

// TestUnlinkEpisodeFileSurfacesTransferErrors guards the episode removal
// endpoint's error mapping. RemoveEpisodeFile reports every user-facing
// failure as a *transfer.Error, and those must surface through transferError
// with their own code, message and mapped status - never as a generic 500
// "internal_error" reply.
func TestUnlinkEpisodeFileSurfacesTransferErrors(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, e := database.Open(ctx, dir)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	accounts, e := auth.New(ctx, db, dir)
	if e != nil {
		t.Fatal(e)
	}
	raw, e := securestore.SecretFile(filepath.Join(dir, "setup-token"), 32, false)
	if e != nil {
		t.Fatal(e)
	}
	session, e := accounts.Setup(ctx, "testadmin", "test-only-password", base64.RawURLEncoding.EncodeToString(raw))
	if e != nil {
		t.Fatal(e)
	}
	secrets, e := securestore.New(ctx, db, dir)
	if e != nil {
		t.Fatal(e)
	}
	cfg := config.Config{ConfigDir: dir, MediaDir: filepath.Join(dir, "media"), DownloadsDir: filepath.Join(dir, "downloads"), PublicURL: "http://example.com"}
	workflow := transfer.New(db, secrets, cfg, transfer.NewQBittorrent(), transfer.NewTorznab())
	handler := New(db, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), fstest.MapFS{"index.html": {Data: []byte("ReelScarey")}}, "test", Dependencies{Auth: accounts, Transfer: workflow})
	show := metadata.Series{Movie: metadata.Movie{TMDbID: 789, Title: "Removal fixture"}, Seasons: []metadata.SeasonSummary{{TMDbID: 790, Number: 1, Title: "Season 1", EpisodeCount: 2}}}
	item, _, e := library.New(db).AddSeries(ctx, show, true)
	if e != nil {
		t.Fatal(e)
	}
	if e = library.New(db).SaveSeason(ctx, item.ID, metadata.Season{SeasonSummary: show.Seasons[0], Episodes: []metadata.Episode{{TMDbID: 791, Number: 1, Title: "One"}, {TMDbID: 792, Number: 2, Title: "Two"}}}, 1); e != nil {
		t.Fatal(e)
	}
	var revision int64
	if e = db.QueryRow(`SELECT revision FROM series_details WHERE media_id=?`, item.ID).Scan(&revision); e != nil {
		t.Fatal(e)
	}
	var first, second int64
	if e = db.QueryRow(`SELECT e.id FROM episodes e JOIN seasons s ON s.id=e.season_id WHERE s.series_id=? AND e.number=1`, item.ID).Scan(&first); e != nil {
		t.Fatal(e)
	}
	if e = db.QueryRow(`SELECT e.id FROM episodes e JOIN seasons s ON s.id=e.season_id WHERE s.series_id=? AND e.number=2`, item.ID).Scan(&second); e != nil {
		t.Fatal(e)
	}
	// One registered file serves both episodes.
	if _, e = db.Exec(`INSERT INTO media_files(id,series_id,relative_path,size_bytes,sha256,probe_json,imported_at,origin) VALUES(9001,?,'TV/Removal fixture/s01e01.mkv',4096,'a1','{}',0,'existing')`, item.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(`INSERT INTO episode_files(episode_id,media_file_id) VALUES(?,9001),(?,9001)`, first, second); e != nil {
		t.Fatal(e)
	}
	remove := fmt.Sprintf("/api/v1/library/series/%d/episodes/%d/remove-file", item.ID, first)
	body := func(rev int64, deleteFile bool) string {
		return fmt.Sprintf(`{"revision":%d,"reviewed":true,"delete_file":%t}`, rev, deleteFile)
	}
	if out := request(handler, "POST", remove, body(revision, false), nil, false); out.Code != 401 {
		t.Fatal("unauthenticated removal accepted", out.Code)
	}
	if out := request(handler, "POST", remove, body(revision, false), &session, false); out.Code != 403 {
		t.Fatal("removal without CSRF accepted", out.Code)
	}
	if out := request(handler, "POST", remove, `{"revision":1}`, &session, true); out.Code != 400 || !strings.Contains(out.Body.String(), "review_required") {
		t.Fatal("unreviewed removal accepted", out.Code, out.Body)
	}
	// Unknown show: the transfer not_found code must map to 404 instead of a
	// generic 500.
	if out := request(handler, "POST", fmt.Sprintf("/api/v1/library/series/999999/episodes/%d/remove-file", first), body(revision, false), &session, true); out.Code != 404 || !strings.Contains(out.Body.String(), `"code":"not_found"`) {
		t.Fatal("unknown-show removal did not surface the mapped transfer error", out.Code, out.Body)
	}
	// Stale revision: revision_conflict maps to 409 with the transfer message,
	// not the generic internal-error copy.
	if out := request(handler, "POST", remove, body(revision+7, false), &session, true); out.Code != 409 || !strings.Contains(out.Body.String(), "revision_conflict") || strings.Contains(out.Body.String(), "Check the application logs") {
		t.Fatal("stale-revision removal did not surface the mapped transfer error", out.Code, out.Body)
	}
	// A file shared with another episode is refused; the shared_file code and
	// its real message must surface rather than a generic 500 (it currently
	// maps to transferError's default bucket, which is not 500).
	if out := request(handler, "POST", remove, body(revision, true), &session, true); out.Code == 500 || !strings.Contains(out.Body.String(), `"code":"shared_file"`) || !strings.Contains(out.Body.String(), "also serves") {
		t.Fatal("shared-file refusal did not surface the transfer code", out.Code, out.Body)
	}
	// Registration-only removal keeps the shared file on disk and succeeds.
	if out := request(handler, "POST", remove, body(revision, false), &session, true); out.Code != 200 {
		t.Fatal("registration-only removal failed", out.Code, out.Body)
	}
	// The episode has no registered file left; that transfer outcome must also
	// surface with its own code instead of a generic 500.
	if out := request(handler, "POST", remove, body(revision+1, false), &session, true); out.Code == 500 || !strings.Contains(out.Body.String(), `"code":"no_episode_file"`) {
		t.Fatal("missing-file removal did not surface the transfer code", out.Code, out.Body)
	}
}
