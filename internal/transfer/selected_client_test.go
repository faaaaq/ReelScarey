package transfer

import (
	"context"
	"testing"
)

// A release download resolves its client in a strict order: an owner-chosen
// default, then the legacy settings connection, then the included built-in
// client. Queue-side validation re-derives the same choice inside the
// reservation transaction, so a changed default fails the job instead of
// silently rerouting it.
func TestSelectedClientPrefersDefaultsThenSettingsThenBuiltIn(t *testing.T) {
	ctx := context.Background()
	f := setupWorkflow(t)

	// The fixture's settings connection stands in for an owner-configured
	// client. Removing it leaves an installation with nothing configured.
	if e := f.s.Remove(ctx, "qbittorrent"); e != nil {
		t.Fatal(e)
	}
	if _, e := f.s.selectedClient(ctx, "torrent"); problemCode(e) != "client_not_configured" {
		t.Fatal("an empty installation should refuse to download honestly", e)
	}
	if _, e := f.s.selectedClient(ctx, "usenet"); problemCode(e) != "client_not_configured" {
		t.Fatal(e)
	}

	// Creating the included connection makes it the default when none is
	// chosen, and the torrent fallback for release downloads.
	if _, e := f.s.createBundled(ctx); e != nil {
		t.Fatal(e)
	}
	d, e := f.s.ClientDefaults(ctx)
	if e != nil || d.Torrent != builtinClientID {
		t.Fatal("the included client did not become the torrent default", d, e)
	}
	c, e := f.s.selectedClient(ctx, "torrent")
	if e != nil || c.profileKey != builtinClientID || c.label != builtinClientName {
		t.Fatal(c, e)
	}
	tx, e := f.s.db.BeginTx(ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	if e = validateSelectedClient(ctx, tx, c); e != nil {
		t.Fatal("queue validation refused the included client", e)
	}
	if e = tx.Commit(); e != nil {
		t.Fatal(e)
	}

	// A paused built-in client is not a fallback.
	if _, e := f.s.db.Exec(`UPDATE download_clients SET enabled=0 WHERE id=?`, builtinClientID); e != nil {
		t.Fatal(e)
	}
	if _, e := f.s.selectedClient(ctx, "torrent"); problemCode(e) != "client_not_configured" {
		t.Fatal(e)
	}
	if _, e := f.s.db.Exec(`UPDATE download_clients SET enabled=1 WHERE id=?`, builtinClientID); e != nil {
		t.Fatal(e)
	}

	// An owner-chosen default wins over the included client, and an in-flight
	// fallback selection fails instead of rerouting to the new default.
	a := namedTorrent(t, f, "Named client", "https://client.invalid", "named")
	d, e = f.s.ClientDefaults(ctx)
	if e != nil {
		t.Fatal(e)
	}
	d.Torrent = a.ID
	if _, e = f.s.SaveClientDefaults(ctx, d); e != nil {
		t.Fatal(e)
	}
	named, e := f.s.selectedClient(ctx, "torrent")
	if e != nil || named.profileKey != a.ID {
		t.Fatal(named, e)
	}
	tx, e = f.s.db.BeginTx(ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	if e = validateSelectedClient(ctx, tx, c); problemCode(e) != "revision_conflict" {
		tx.Rollback()
		t.Fatal("a stale built-in selection must not follow a changed default", e)
	}
	if e = tx.Rollback(); e != nil {
		t.Fatal(e)
	}

	// The legacy settings connection is used when no default is chosen, and
	// takes precedence over the built-in fallback.
	if _, e = f.s.SaveClientDefaults(ctx, ClientDefaults{Torrent: "", Revision: d.Revision + 1}); e != nil {
		t.Fatal(e)
	}
	password := "settings-private-password"
	if e = f.s.ConfigureClient(ctx, ClientUpdate{
		URL:          "https://settings.invalid",
		Username:     "fixture",
		Password:     &password,
		DownloadPath: "/settings-downloads",
	}); e != nil {
		t.Fatal(e)
	}
	settings, e := f.s.selectedClient(ctx, "torrent")
	if e != nil || settings.profileKey != "" || settings.label != "qBittorrent (Settings)" {
		t.Fatal(settings, e)
	}
	tx, e = f.s.db.BeginTx(ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	if e = validateSelectedClient(ctx, tx, settings); e != nil {
		tx.Rollback()
		t.Fatal("queue validation refused the settings connection", e)
	}
	if e = tx.Rollback(); e != nil {
		t.Fatal(e)
	}
}
