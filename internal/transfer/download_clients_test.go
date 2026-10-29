package transfer

import (
	"context"
	"encoding/json"
	"github.com/faaaaq/ReelScarey/internal/securestore"
	"strings"
	"testing"
)

func namedTorrent(t *testing.T, f *workflowFixture, name, url, local string) DownloadClient {
	t.Helper()
	password := "named-private-password"
	ctx := context.Background()
	created, e := f.s.SaveDownloadClient(ctx, DownloadClientUpdate{Name: name, Kind: "qbittorrent", URL: url, Username: "fixture", Password: &password, DownloadPath: "/remote", LocalFolder: local})
	if e != nil {
		t.Fatal(e)
	}
	if created.Enabled || created.Revision != 1 {
		t.Fatal("new profile not paused", created)
	}
	saved, e := f.s.SaveDownloadClient(ctx, DownloadClientUpdate{ID: created.ID, Name: name, Kind: created.Kind, Enabled: true, Revision: created.Revision, URL: url, Username: "fixture", DownloadPath: "/remote", LocalFolder: local})
	if e != nil {
		t.Fatal(e)
	}
	return saved
}
func TestNamedClientsKeepExistingJobBindingsAndEncryptedCredentials(t *testing.T) {
	ctx := context.Background()
	f := setupWorkflow(t)
	a := namedTorrent(t, f, "First client", "https://client-a.invalid", "client-a")
	b := namedTorrent(t, f, "Second client", "https://client-b.invalid", "client-b")
	d, e := f.s.ClientDefaults(ctx)
	if e != nil {
		t.Fatal(e)
	}
	d.Torrent = a.ID
	d, e = f.s.SaveClientDefaults(ctx, d)
	if e != nil {
		t.Fatal(e)
	}
	job := f.queue(t)
	if job.ClientKey != a.ID || job.ClientKind != "qbittorrent" || job.ClientLabel != a.Name || job.clientLocal != "client-a" || job.clientRoot != "/remote" {
		t.Fatal(job)
	}
	d.Torrent = b.ID
	if _, e = f.s.SaveClientDefaults(ctx, d); e != nil {
		t.Fatal(e)
	}
	bound, e := f.s.boundClient(ctx, job)
	if e != nil || bound.URL != a.URL || bound.LocalFolder != "client-a" {
		t.Fatal("default rerouted old job", bound, e)
	}
	if p, e := mapJobSource(job, "/remote/ReelScarey/"+job.ID, "owned.mkv", "/remote"); e != nil || p != "client-a/ReelScarey/"+job.ID+"/owned.mkv" || !sourceInJob(job, p) {
		t.Fatal(p, e)
	}
	if sourceInJob(job, "client-b/ReelScarey/"+job.ID+"/owned.mkv") {
		t.Fatal("cross-client source accepted")
	}
	if _, e = f.s.SaveDownloadClient(ctx, DownloadClientUpdate{ID: a.ID, Name: a.Name, Kind: a.Kind, Enabled: true, Revision: a.Revision, URL: a.URL, Username: "fixture", DownloadPath: "/different", LocalFolder: "client-a"}); e == nil {
		t.Fatal("active root changed")
	}
	if e = f.s.DeleteDownloadClient(ctx, a.ID, a.Revision); e == nil {
		t.Fatal("active profile removed")
	}
	profiles, e := f.s.DownloadClients(ctx)
	if e != nil {
		t.Fatal(e)
	}
	public, _ := json.Marshal(profiles)
	if strings.Contains(string(public), "named-private-password") || strings.Contains(string(public), `"password"`) {
		t.Fatal("secret exposed")
	}
	var sealed []byte
	if e = f.db.QueryRow(`SELECT ciphertext FROM integration_configs WHERE name=?`, clientSecretName(a.ID)).Scan(&sealed); e != nil || strings.Contains(string(sealed), "named-private-password") {
		t.Fatal("secret stored in plaintext", e)
	}
	if _, e = securestore.New(ctx, f.db, f.cfg.ConfigDir); e != nil {
		t.Fatal("credentials failed reopen verification", e)
	}
}
func TestClientQueueFenceRejectsChangedDefaultOrCredentials(t *testing.T) {
	ctx := context.Background()
	f := setupWorkflow(t)
	c, e := f.s.selectedClient(ctx, "torrent")
	if e != nil {
		t.Fatal(e)
	}
	d, e := f.s.ClientDefaults(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.SaveClientDefaults(ctx, d); e != nil {
		t.Fatal(e)
	}
	tx, e := f.db.BeginTx(ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	if e = validateSelectedClient(ctx, tx, c); e == nil {
		t.Fatal("changed default revision accepted")
	}
	tx.Rollback()
	c, e = f.s.selectedClient(ctx, "torrent")
	if e != nil {
		t.Fatal(e)
	}
	password := "rotated-private-password"
	if e = f.s.ConfigureClient(ctx, ClientUpdate{URL: c.URL, Username: c.Username, Password: &password, DownloadPath: c.DownloadPath}); e != nil {
		t.Fatal(e)
	}
	tx, e = f.db.BeginTx(ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	if e = validateSelectedClient(ctx, tx, c); e == nil {
		t.Fatal("changed credential snapshot accepted")
	}
	tx.Rollback()
	a := namedTorrent(t, f, "One", "https://client-one.invalid", "")
	d, e = f.s.ClientDefaults(ctx)
	if e != nil {
		t.Fatal(e)
	}
	d.Usenet = a.ID
	if _, e = f.s.SaveClientDefaults(ctx, d); e == nil {
		t.Fatal("torrent used as Usenet default")
	}
	if e = f.s.DeleteDownloadClient(ctx, a.ID, a.Revision-1); e == nil {
		t.Fatal("stale delete accepted")
	}
	if e = f.s.DeleteDownloadClient(ctx, a.ID, a.Revision); e != nil {
		t.Fatal(e)
	}
	if b, e := f.s.secrets.GetNamed(ctx, clientSecretName(a.ID)); e != nil || len(b) != 0 {
		t.Fatal("removed credentials retained", e)
	}
}
