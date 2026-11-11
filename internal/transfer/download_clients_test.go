package transfer

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/faaaaq/ReelScarey/internal/securestore"
	"net/http"
	"net/http/httptest"
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

// Both external Usenet clients are ordinary named connections: they are
// tested before saving, start paused, are scoped to the usenet protocol, can be
// chosen as its default, and release downloads resolve them through that
// default. No owner account or real daemon is involved here.
func TestNamedUsenetClientsAreTestableScopedAndDefaulted(t *testing.T) {
	ctx := context.Background()
	f := setupWorkflow(t)
	sab := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sab/api" || r.URL.Query().Get("apikey") != "sab-private-key" {
			w.WriteHeader(403)
			return
		}
		if r.URL.Query().Get("mode") == "version" {
			fmt.Fprint(w, `{"version":"5.1.3"}`)
			return
		}
		fmt.Fprint(w, `{"categories":["reelscarey"]}`)
	}))
	defer sab.Close()
	nzbget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/jsonrpc" {
			w.WriteHeader(404)
			return
		}
		var body struct {
			Method string `json:"method"`
			ID     int    `json:"id"`
		}
		if e := json.NewDecoder(r.Body).Decode(&body); e != nil || body.ID != 1 {
			w.WriteHeader(400)
			return
		}
		switch body.Method {
		case "version":
			fmt.Fprint(w, `{"id":1,"result":"26.3","error":null}`)
		case "config":
			fmt.Fprint(w, `{"id":1,"result":[{"Name":"Category1.Name","Value":"reelscarey"},{"Name":"Category1.DestDir","Value":"/completed/ReelScarey"},{"Name":"Category1.Extensions","Value":""},{"Name":"Extensions","Value":""},{"Name":"ScriptOrder","Value":""}],"error":null}`)
		default:
			w.WriteHeader(400)
		}
	}))
	defer nzbget.Close()

	sabKey := "sab-private-key"
	created, e := f.s.SaveDownloadClient(ctx, DownloadClientUpdate{Name: "External SAB", Kind: "sabnzbd", URL: sab.URL + "/sab", APIKey: &sabKey, DownloadPath: "/completed"})
	if e != nil || created.Enabled || created.Revision != 1 {
		t.Fatal("SAB was not testable and paused", created, e)
	}
	created, e = f.s.SaveDownloadClient(ctx, DownloadClientUpdate{ID: created.ID, Name: created.Name, Kind: created.Kind, URL: created.URL, DownloadPath: "/completed", Revision: created.Revision, Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	d, e := f.s.ClientDefaults(ctx)
	if e != nil {
		t.Fatal(e)
	}
	d.Usenet = created.ID
	if d, e = f.s.SaveClientDefaults(ctx, d); e != nil {
		t.Fatal("SAB could not become the usenet default", e)
	}
	selected, e := f.s.selectedClient(ctx, "usenet")
	if e != nil || selected.profileKey != created.ID || selected.kind != "sabnzbd" || selected.URL != created.URL {
		t.Fatal("usenet release did not route to the named SAB client", selected, e)
	}
	if e = f.s.TestDownloadClient(ctx, created.ID); e != nil {
		t.Fatal("saved SAB connection did not test", e)
	}

	user, pass := "nzbget-user", "nzbget-private-password"
	createdNZB, e := f.s.SaveDownloadClient(ctx, DownloadClientUpdate{Name: "External NZBGet", Kind: "nzbget", URL: nzbget.URL, Username: user, Password: &pass, DownloadPath: "/completed"})
	if e != nil || createdNZB.Enabled || createdNZB.Revision != 1 {
		t.Fatal("NZBGet was not testable and paused", createdNZB, e)
	}
	createdNZB, e = f.s.SaveDownloadClient(ctx, DownloadClientUpdate{ID: createdNZB.ID, Name: createdNZB.Name, Kind: createdNZB.Kind, URL: createdNZB.URL, Username: user, DownloadPath: "/completed", Revision: createdNZB.Revision, Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	d, e = f.s.ClientDefaults(ctx)
	if e != nil {
		t.Fatal(e)
	}
	d.Usenet = createdNZB.ID
	if d, e = f.s.SaveClientDefaults(ctx, d); e != nil {
		t.Fatal("NZBGet could not become the usenet default", e)
	}
	selected, e = f.s.selectedClient(ctx, "usenet")
	if e != nil || selected.profileKey != createdNZB.ID || selected.kind != "nzbget" || selected.URL != createdNZB.URL {
		t.Fatal("usenet release did not route to the named NZBGet client", selected, e)
	}
	if e = f.s.TestDownloadClient(ctx, createdNZB.ID); e != nil {
		t.Fatal("saved NZBGet connection did not test", e)
	}

	d.Usenet = ""
	d.Torrent = createdNZB.ID
	if _, e = f.s.SaveClientDefaults(ctx, d); e == nil {
		t.Fatal("a usenet client was accepted as the torrent default")
	}
	d.Torrent = ""
	if _, e = f.s.SaveClientDefaults(ctx, d); e != nil {
		t.Fatal(e)
	}
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

// A download_clients row without a matching integration config (legacy or an
// interrupted save) must still list: the empty sealed blob yields empty
// connection fields instead of failing the whole settings page, and a
// configured row keeps decrypting its stored connection.
func TestDownloadClientsListToleratesRowsWithoutCredentialConfig(t *testing.T) {
	ctx := context.Background()
	f := setupWorkflow(t)
	a := namedTorrent(t, f, "Configured client", "https://client-a.invalid", "client-a")
	orphanID := strings.Repeat("f", 32)
	if _, e := f.db.Exec(`INSERT INTO download_clients(id,name,kind,enabled,created_at,updated_at) VALUES(?,?,?,?,?,?)`, orphanID, "Legacy row", "sabnzbd", 0, 1, 1); e != nil {
		t.Fatal(e)
	}
	profiles, e := f.s.DownloadClients(ctx)
	if e != nil {
		t.Fatal("list failed on a client without a config", e)
	}
	seen := map[string]DownloadClient{}
	for _, c := range profiles {
		seen[c.ID] = c
	}
	legacy, ok := seen[orphanID]
	if !ok {
		t.Fatal("legacy row missing from the list", profiles)
	}
	if legacy.URL != "" || legacy.Username != "" || legacy.DownloadPath != "" || legacy.LocalFolder != "" {
		t.Fatal("legacy row fabricated connection fields", legacy)
	}
	configured, ok := seen[a.ID]
	if !ok {
		t.Fatal("configured row missing from the list", profiles)
	}
	if configured.URL != a.URL || configured.Username != "fixture" || configured.DownloadPath != "/remote" || configured.LocalFolder != "client-a" {
		t.Fatal("configured row did not decrypt its stored connection", configured)
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
