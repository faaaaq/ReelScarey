package transfer

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"strings"
	"time"
)

type DownloadClient struct {
	LocalFolder  string `json:"local_folder"`
	ID           string `json:"id"`
	Name         string `json:"name"`
	Kind         string `json:"kind"`
	Enabled      bool   `json:"enabled"`
	Builtin      bool   `json:"builtin,omitempty"`
	Revision     int    `json:"revision"`
	URL          string `json:"url"`
	Username     string `json:"username,omitempty"`
	DownloadPath string `json:"download_path"`
	ActiveJobs   int    `json:"active_jobs"`
}
type ClientDefaults struct {
	Torrent  string `json:"torrent"`
	Usenet   string `json:"usenet"`
	Revision int    `json:"revision"`
}
type DownloadClientUpdate struct {
	LocalFolder  string  `json:"local_folder"`
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Kind         string  `json:"kind"`
	Enabled      bool    `json:"enabled"`
	Revision     int     `json:"revision"`
	URL          string  `json:"url"`
	Username     string  `json:"username"`
	DownloadPath string  `json:"download_path"`
	Password     *string `json:"password"`
	APIKey       *string `json:"api_key"`
}

func clientProtocol(kind string) string {
	if kind == "qbittorrent" || kind == "transmission" {
		return "torrent"
	}
	if kind == "sabnzbd" || kind == "nzbget" {
		return "usenet"
	}
	return ""
}
func clientSecretName(key string) string { return "download-client:" + key }
func clientChanged() error {
	return problem("revision_conflict", "Download client settings changed. Reload and review them again.")
}
func defaultsTx(ctx context.Context, tx interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}) (ClientDefaults, error) {
	var d ClientDefaults
	e := tx.QueryRowContext(ctx, `SELECT torrent,usenet,revision FROM download_client_preferences WHERE id=1`).Scan(&d.Torrent, &d.Usenet, &d.Revision)
	return d, e
}
func (s *Service) ClientDefaults(ctx context.Context) (ClientDefaults, error) {
	return defaultsTx(ctx, s.db)
}
func (s *Service) DownloadClients(ctx context.Context) ([]DownloadClient, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT c.id,c.name,c.kind,c.enabled,c.builtin,c.revision,(SELECT COUNT(*) FROM downloads d WHERE d.client_key=c.id AND d.state<>'completed') FROM download_clients c ORDER BY c.name COLLATE NOCASE,c.id LIMIT 32`)
	if e != nil {
		return nil, e
	}
	out := []DownloadClient{}
	for rows.Next() {
		var c DownloadClient
		if e = rows.Scan(&c.ID, &c.Name, &c.Kind, &c.Enabled, &c.Builtin, &c.Revision, &c.ActiveJobs); e != nil {
			rows.Close()
			return nil, e
		}
		out = append(out, c)
	}
	e = errors.Join(rows.Err(), rows.Close())
	if e != nil {
		return nil, e
	}
	for n := range out {
		c, e := s.managedClientConfig(ctx, out[n].ID)
		if e != nil {
			return nil, e
		}
		out[n].URL = c.URL
		out[n].Username = c.Username
		out[n].DownloadPath = c.DownloadPath
		out[n].LocalFolder = c.LocalFolder
	}
	return out, nil
}
func (s *Service) managedClientConfig(ctx context.Context, key string) (ClientConfig, error) {
	var c ClientConfig
	var kind, label string
	var revision int
	if e := s.db.QueryRowContext(ctx, `SELECT kind,name,revision FROM download_clients WHERE id=?`, key).Scan(&kind, &label, &revision); e != nil {
		return c, e
	}
	b, e := s.secrets.GetNamed(ctx, clientSecretName(key))
	if e != nil {
		return c, e
	}
	if len(b) == 0 {
		return c, problem("client_not_configured", "This saved client has no matching credentials. Restore its configuration backup.")
	}
	if e = json.Unmarshal(b, &c); e != nil {
		return c, e
	}
	c.profileKey = key
	c.kind = kind
	c.label = label
	c.profileRevision = revision
	return c, nil
}
func (s *Service) testDownloadClient(ctx context.Context, kind string, c ClientConfig) error {
	switch kind {
	case "qbittorrent":
		_, e := s.client.Test(ctx, c)
		return e
	case "transmission":
		_, e := s.transmission.Test(ctx, c)
		return e
	case "sabnzbd":
		return s.sab.Test(ctx, c)
	case "nzbget":
		return s.nzbget.Test(ctx, c)
	default:
		return problem("invalid_settings", "Choose qBittorrent, Transmission, SABnzbd or NZBGet.")
	}
}
func (s *Service) SaveDownloadClient(ctx context.Context, in DownloadClientUpdate) (DownloadClient, error) {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	var result DownloadClient
	_, keyError := hex.DecodeString(in.ID)
	if !textOK(strings.TrimSpace(in.Name), 60) || clientProtocol(in.Kind) == "" || (in.ID != "" && (len(in.ID) != 32 || keyError != nil || strings.ToLower(in.ID) != in.ID)) || (in.ID == "" && in.Revision != 0) {
		return result, problem("invalid_settings", "Use a unique name and a supported download client.")
	}
	if in.ID == builtinClientID {
		return result, problem("invalid_selection", "The built-in client is supervised by the application. Use its dedicated controls instead.")
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.LocalFolder != "" && (!fs.ValidPath(in.LocalFolder) || in.LocalFolder == "." || strings.ContainsAny(in.LocalFolder, "\\:\x00") || len(in.LocalFolder) > 250) {
		return result, problem("invalid_settings", "The local folder must be a relative folder inside the app's download mount.")
	}
	u, e := endpoint(in.URL)
	if e != nil {
		return result, e
	}
	if in.Kind == "nzbget" {
		u.Path = strings.TrimSuffix(u.Path, "/jsonrpc")
	}
	remote, e := remoteRoot(in.DownloadPath)
	if e != nil {
		return result, e
	}
	var prior ClientConfig
	if in.ID == "" {
		if in.Enabled {
			return result, problem("invalid_settings", "New clients start paused. Test and save, then enable explicitly.")
		}
		in.ID = ID()
	} else {
		prior, e = s.managedClientConfig(ctx, in.ID)
		if e != nil {
			return result, e
		}
		if prior.profileRevision != in.Revision || prior.kind != in.Kind {
			return result, clientChanged()
		}
	}
	c := ClientConfig{URL: u.String(), Username: strings.TrimSpace(in.Username), DownloadPath: remote, Password: prior.Password, APIKey: prior.APIKey, LocalFolder: in.LocalFolder}
	if in.Password != nil {
		c.Password = *in.Password
	}
	if in.APIKey != nil {
		c.APIKey = *in.APIKey
	}
	if in.Kind == "sabnzbd" {
		c.Username = ""
		c.Password = ""
		if !textOK(c.APIKey, 2048) || (prior.URL != "" && prior.URL != c.URL && in.APIKey == nil) {
			return result, problem("invalid_settings", "Enter the full SABnzbd API key; re-enter it when changing the address.")
		}
	} else {
		c.APIKey = ""
		if !textOK(c.Username, 200) || !textOK(c.Password, 2048) || ((prior.URL != "" && (prior.URL != c.URL || prior.Username != c.Username)) && in.Password == nil) {
			return result, problem("invalid_settings", "Enter a username and password; re-enter the password when changing the address or username.")
		}
	}
	if e = s.testDownloadClient(ctx, in.Kind, c); e != nil {
		return result, e
	}
	plain, e := json.Marshal(c)
	if e != nil {
		return result, e
	}
	sealed, e := s.secrets.Seal("config:"+clientSecretName(in.ID), plain)
	if e != nil {
		return result, e
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return result, e
	}
	defer tx.Rollback()
	var duplicate int
	if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM download_clients WHERE name=? COLLATE NOCASE AND id<>?`, in.Name, in.ID).Scan(&duplicate); e != nil {
		return result, e
	}
	if duplicate > 0 {
		return result, problem("invalid_settings", "Another download client already has that name.")
	}
	if in.Revision == 0 {
		var count int
		if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM download_clients`).Scan(&count); e != nil {
			return result, e
		}
		if count >= 32 {
			return result, problem("invalid_settings", "At most 32 named download clients are supported.")
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO download_clients(id,name,kind,enabled,created_at,updated_at) VALUES(?,?,?,?,?,?)`, in.ID, in.Name, in.Kind, false, time.Now().Unix(), time.Now().Unix()); e != nil {
			return result, e
		}
	} else {
		var active int
		if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM downloads WHERE client_key=? AND state<>'completed'`, in.ID).Scan(&active); e != nil {
			return result, e
		}
		if active > 0 && (prior.URL != c.URL || prior.DownloadPath != c.DownloadPath || prior.LocalFolder != c.LocalFolder) {
			return result, problem("active_downloads", "Finish this client's jobs before changing its address or download folder. Stopped jobs retain their mapping for retry.")
		}
		d, e := defaultsTx(ctx, tx)
		if e != nil {
			return result, e
		}
		if !in.Enabled && (d.Torrent == in.ID || d.Usenet == in.ID) {
			return result, problem("active_downloads", "Choose another default before pausing this client.")
		}
		r, e := tx.ExecContext(ctx, `UPDATE download_clients SET name=?,enabled=?,revision=revision+1,updated_at=? WHERE id=? AND revision=?`, in.Name, in.Enabled, time.Now().Unix(), in.ID, in.Revision)
		if e != nil {
			return result, e
		}
		n, _ := r.RowsAffected()
		if n != 1 {
			return result, clientChanged()
		}
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO integration_configs(name,ciphertext) VALUES(?,?) ON CONFLICT(name) DO UPDATE SET ciphertext=excluded.ciphertext,updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now')`, clientSecretName(in.ID), sealed); e != nil {
		return result, e
	}
	if e = tx.Commit(); e != nil {
		return result, e
	}
	result = DownloadClient{ID: in.ID, Name: in.Name, Kind: in.Kind, Enabled: in.Enabled, Revision: in.Revision + 1, URL: c.URL, Username: c.Username, DownloadPath: c.DownloadPath, LocalFolder: c.LocalFolder}
	return result, nil
}
func (s *Service) SaveClientDefaults(ctx context.Context, in ClientDefaults) (ClientDefaults, error) {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return in, e
	}
	defer tx.Rollback()
	for protocol, key := range map[string]string{"torrent": in.Torrent, "usenet": in.Usenet} {
		if key == "" {
			continue
		}
		var kind string
		var enabled bool
		if e = tx.QueryRowContext(ctx, `SELECT kind,enabled FROM download_clients WHERE id=?`, key).Scan(&kind, &enabled); e != nil {
			return in, e
		}
		if !enabled || clientProtocol(kind) != protocol {
			return in, problem("invalid_settings", "Choose an enabled client for the matching protocol.")
		}
	}
	r, e := tx.ExecContext(ctx, `UPDATE download_client_preferences SET torrent=?,usenet=?,revision=revision+1 WHERE id=1 AND revision=?`, in.Torrent, in.Usenet, in.Revision)
	if e != nil {
		return in, e
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return in, clientChanged()
	}
	if e = tx.Commit(); e != nil {
		return in, e
	}
	in.Revision++
	return in, nil
}
func (s *Service) DeleteDownloadClient(ctx context.Context, key string, revision int) error {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	if key == builtinClientID {
		return problem("invalid_selection", "The built-in client cannot be removed. Disable it instead.")
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	d, e := defaultsTx(ctx, tx)
	if e != nil {
		return e
	}
	var active int
	if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM downloads WHERE client_key=? AND state<>'completed'`, key).Scan(&active); e != nil {
		return e
	}
	if active > 0 || d.Torrent == key || d.Usenet == key {
		return problem("active_downloads", "Choose another default and finish this client's jobs before removing it. Stopped jobs retain their client for retry; pause the client instead.")
	}
	r, e := tx.ExecContext(ctx, `DELETE FROM download_clients WHERE id=? AND revision=?`, key, revision)
	if e != nil {
		return e
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return clientChanged()
	}
	if _, e = tx.ExecContext(ctx, `DELETE FROM integration_configs WHERE name=?`, clientSecretName(key)); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Service) TestDownloadClient(ctx context.Context, key string) error {
	c, e := s.managedClientConfig(ctx, key)
	if e != nil {
		return e
	}
	return s.testDownloadClient(ctx, c.kind, c)
}

func (s *Service) selectedClient(ctx context.Context, protocol string) (ClientConfig, error) {
	var c ClientConfig
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return c, e
	}
	defer tx.Rollback()
	d, e := defaultsTx(ctx, tx)
	if e != nil {
		return c, e
	}
	key := d.Torrent
	kind := "qbittorrent"
	name := "qbittorrent"
	if protocol == "usenet" {
		key = d.Usenet
		kind = "sabnzbd"
		name = "sabnzbd"
	} else if protocol != "torrent" && protocol != "" {
		return c, problem("invalid_release", "Choose a supported download protocol.")
	}
	label := map[string]string{"qbittorrent": "qBittorrent (Settings)", "sabnzbd": "SABnzbd (Settings)"}[kind]
	revision := 0
	if key != "" {
		var enabled bool
		if e = tx.QueryRowContext(ctx, `SELECT kind,name,enabled,revision FROM download_clients WHERE id=?`, key).Scan(&kind, &label, &enabled, &revision); e != nil {
			return c, e
		}
		if !enabled || clientProtocol(kind) != protocol {
			return c, problem("client_not_configured", "The selected default is paused or belongs to another protocol.")
		}
		name = clientSecretName(key)
	}
	var sealed []byte
	if e = tx.QueryRowContext(ctx, `SELECT ciphertext FROM integration_configs WHERE name=?`, name).Scan(&sealed); errors.Is(e, sql.ErrNoRows) {
		return c, problem("client_not_configured", "Connect a matching download client in Settings or Download clients.")
	} else if e != nil {
		return c, e
	}
	plain, e := s.secrets.Open("config:"+name, sealed)
	if e != nil {
		return c, e
	}
	if e = json.Unmarshal(plain, &c); e != nil {
		return c, e
	}
	c.profileKey = key
	c.kind = kind
	c.label = label
	c.profileRevision = revision
	c.defaultsRevision = d.Revision
	c.ciphertext = sealed
	return c, tx.Commit()
}
func validateSelectedClient(ctx context.Context, tx *sql.Tx, c ClientConfig) error {
	d, e := defaultsTx(ctx, tx)
	if e != nil {
		return e
	}
	key := d.Torrent
	name := "qbittorrent"
	if clientProtocol(c.kind) == "usenet" {
		key = d.Usenet
		name = "sabnzbd"
	}
	if d.Revision != c.defaultsRevision || key != c.profileKey {
		return clientChanged()
	}
	if key != "" {
		var revision int
		var enabled bool
		var kind string
		if e = tx.QueryRowContext(ctx, `SELECT revision,enabled,kind FROM download_clients WHERE id=?`, key).Scan(&revision, &enabled, &kind); e != nil {
			return e
		}
		if revision != c.profileRevision || !enabled || kind != c.kind {
			return clientChanged()
		}
		name = clientSecretName(key)
	}
	var sealed []byte
	if e = tx.QueryRowContext(ctx, `SELECT ciphertext FROM integration_configs WHERE name=?`, name).Scan(&sealed); e != nil {
		return e
	}
	if !bytes.Equal(sealed, c.ciphertext) {
		return clientChanged()
	}
	return nil
}
func (s *Service) boundClient(ctx context.Context, j Job) (ClientConfig, error) {
	var c ClientConfig
	var e error
	if j.ClientKey != "" {
		c, e = s.managedClientConfig(ctx, j.ClientKey)
	} else if j.Protocol == "usenet" {
		c, e = s.sabConfig(ctx)
		c.kind = "sabnzbd"
	} else {
		c, e = s.clientConfig(ctx)
		c.kind = "qbittorrent"
	}
	if e != nil {
		return c, e
	}
	if c.URL != j.clientURL || (j.ClientKind != "" && c.kind != j.ClientKind) || (j.clientRoot != "" && c.DownloadPath != j.clientRoot) || c.LocalFolder != j.clientLocal {
		return c, problem("client_changed", "Restore this job's original download client before resuming tracking.")
	}
	return c, nil
}
