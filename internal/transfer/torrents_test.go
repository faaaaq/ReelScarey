package transfer

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func enableFixtureBuiltin(t *testing.T, f *workflowFixture) {
	t.Helper()
	enableFixtureBuiltinAt(t, f, "http://127.0.0.1:8085")
}

func enableFixtureBuiltinAt(t *testing.T, f *workflowFixture, clientURL string) {
	t.Helper()
	ctx := context.Background()
	if _, e := f.db.Exec(`INSERT INTO download_clients(id,name,kind,enabled,builtin,revision,created_at,updated_at) VALUES(?,?,?,1,1,1,1,1)`, builtinClientID, builtinClientName, "qbittorrent"); e != nil {
		t.Fatal(e)
	}
	c := ClientConfig{URL: clientURL, Username: "reelscarey", Password: "fixture", DownloadPath: filepath.Join(f.cfg.DownloadsDir, "builtin")}
	raw, e := json.Marshal(c)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.s.secrets.SetNamed(ctx, clientSecretName(builtinClientID), raw); e != nil {
		t.Fatal(e)
	}
}

func TestTorrentPageReportsDisabledClient(t *testing.T) {
	f := setupWorkflow(t)
	out, e := f.s.Torrents(context.Background())
	if e != nil || out.Available || out.State != "disabled" || !strings.Contains(out.Message, "not configured") {
		t.Fatal(out, e)
	}
	enableFixtureBuiltin(t, f)
	if _, e = f.db.Exec(`UPDATE download_clients SET enabled=0 WHERE id=?`, builtinClientID); e != nil {
		t.Fatal(e)
	}
	out, e = f.s.Torrents(context.Background())
	if e != nil || out.Available || !strings.Contains(out.Message, "not enabled") {
		t.Fatal(out, e)
	}
}

func TestTorrentPageListsAndReviewsDestructiveActions(t *testing.T) {
	ctx := context.Background()
	f := setupWorkflow(t)
	enableFixtureBuiltin(t, f)
	f.client.list = []Torrent{{Hash: testHash, Name: "Fixture.Release", State: "downloading", Tags: "reelscarey", Size: 100, Completed: 25, Progress: 0.25}}
	f.client.transfer = TransferInfo{DLSpeed: 2048, UPSpeed: 64, Connection: "connected"}
	out, e := f.s.Torrents(ctx)
	if e != nil || !out.Available || out.State != "running" || len(out.Items) != 1 || out.Transfer.DLSpeed != 2048 {
		t.Fatal(out, e)
	}
	if e = f.s.TorrentAction(ctx, testHash, "pause", false, false); e != nil || f.client.action != "pause" {
		t.Fatal(f.client.action, e)
	}
	if e = f.s.TorrentAction(ctx, testHash, "remove", true, false); e == nil {
		t.Fatal("unreviewed removal accepted")
	}
	if e = f.s.TorrentAction(ctx, testHash, "remove", true, true); e != nil || f.client.action != "remove" || !f.client.deleteFiles {
		t.Fatal(f.client.action, f.client.deleteFiles, e)
	}
	if e = f.s.TorrentAction(ctx, "not-a-hash", "pause", false, false); e == nil {
		t.Fatal("invalid hash accepted")
	}
}

func TestManualTorrentInputIsBoundedAndTagged(t *testing.T) {
	ctx := context.Background()
	f := setupWorkflow(t)
	enableFixtureBuiltin(t, f)
	if e := f.s.AddManualTorrent(ctx, "https://example.invalid/file.torrent", nil); e == nil {
		t.Fatal("remote URL accepted")
	}
	if e := f.s.AddManualTorrent(ctx, "magnet:?xt=urn:btih:"+testHash, nil); e != nil {
		t.Fatal(e)
	}
	if f.client.adds != 1 || f.client.lastTag != "reelscarey-manual" || !strings.HasSuffix(f.client.lastSave, filepath.Join("builtin", "ReelScarey", "manual")) || f.client.lastPayload.Magnet == "" {
		t.Fatal(f.client.adds, f.client.lastTag, f.client.lastSave)
	}
	if e := f.s.AddManualTorrent(ctx, "", []byte("not-metainfo")); e == nil {
		t.Fatal("invalid metainfo accepted")
	}
}
