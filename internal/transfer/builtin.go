package transfer

import (
	"context"
	"crypto/rand"
	"crypto/sha512"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"golang.org/x/crypto/pbkdf2"
)

// The built-in client is a qBittorrent daemon supervised by the application
// process. It is an ordinary named torrent client to every other subsystem:
// the same paused start, explicit per-protocol default, job binding and
// reconcile-by-hash rules apply. Only its lifecycle and configuration are
// owned by the application, so generic client editing is refused for it.
const (
	builtinClientID     = "9e1b071d4f0e4a2f8c3d5b6a7e8f9a0b"
	builtinClientName   = "Built-in qBittorrent"
	builtinWebPort      = 8085
	defaultBuiltinPeer  = 6881
	builtinReadyTimeout = 40 * time.Second
	builtinStopTimeout  = 15 * time.Second
	// tunnelDownMessage is the kill-switch explanation surfaced by both the
	// supervisor and the enable path while the VPN tunnel is down.
	tunnelDownMessage = "The VPN tunnel is down. The built-in client stays stopped until the tunnel returns."
)

// BundledStatus reports the supervised daemon and its saved connection.
type BundledStatus struct {
	Present    bool   `json:"present"`
	Enabled    bool   `json:"enabled"`
	State      string `json:"state"`
	Message    string `json:"message,omitempty"`
	Revision   int    `json:"revision"`
	PeerPort   int    `json:"peer_port"`
	ActiveJobs int    `json:"active_jobs"`
	Name       string `json:"name"`
}

// BundledUpdate enables, disables or reconfigures the built-in client.
// Enabling tests the daemon before the row is marked enabled; the connection
// still starts paused because job routing requires an explicit per-protocol
// default chosen by the owner.
type BundledUpdate struct {
	Revision int   `json:"revision"`
	Enabled  *bool `json:"enabled,omitempty"`
	PeerPort *int  `json:"peer_port,omitempty"`
}

func (s *Service) bundledRow(ctx context.Context) (present, enabled bool, revision int, err error) {
	var e bool
	var r int
	scan := s.db.QueryRowContext(ctx, `SELECT enabled,revision FROM download_clients WHERE id=?`, builtinClientID).Scan(&e, &r)
	if errors.Is(scan, sql.ErrNoRows) {
		return false, false, 0, nil
	}
	if scan != nil {
		return false, false, 0, scan
	}
	return true, e, r, nil
}

// BundledStatus never requires the daemon: a stopped or failed state is
// reported honestly with its saved message.
func (s *Service) BundledStatus(ctx context.Context) (BundledStatus, error) {
	out := BundledStatus{Name: builtinClientName, State: "stopped", PeerPort: defaultBuiltinPeer}
	present, enabled, revision, e := s.bundledRow(ctx)
	if e != nil {
		return out, e
	}
	out.Present, out.Enabled, out.Revision = present, enabled, revision
	if present {
		if c, e := s.managedClientConfig(ctx, builtinClientID); e == nil {
			if c.PeerPort > 0 {
				out.PeerPort = c.PeerPort
			}
		} else {
			var p *Error
			if errors.As(e, &p) {
				out.Message = p.Message
			} else {
				out.Message = "The built-in client configuration could not be read."
			}
		}
		if e := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM downloads WHERE client_key=? AND state<>'completed'`, builtinClientID).Scan(&out.ActiveJobs); e != nil {
			return out, e
		}
	}
	out.State, out.Message = s.bundled.snapshot()
	return out, nil
}

// UpdateBundled owns the whole lifecycle of the built-in connection. Enabling
// starts the supervised daemon, waits for its RPC and tests it before the row
// is marked enabled; a failure leaves the row disabled with the error visible
// in the status. The supervisor owns every process transition.
func (s *Service) UpdateBundled(ctx context.Context, in BundledUpdate) (BundledStatus, error) {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	present, enabled, revision, e := s.bundledRow(ctx)
	if e != nil {
		return BundledStatus{}, e
	}
	if present && in.Revision != revision {
		return BundledStatus{}, clientChanged()
	}
	if !present && in.Revision != 0 {
		return BundledStatus{}, clientChanged()
	}
	if !present {
		if in.Enabled == nil || !*in.Enabled {
			return BundledStatus{}, problem("invalid_selection", "Enable the built-in client to create its configuration.")
		}
		if in.PeerPort != nil {
			return BundledStatus{}, problem("invalid_selection", "Save the built-in client once before changing its peer port.")
		}
		if _, e = s.createBundled(ctx); e != nil {
			return BundledStatus{}, e
		}
	} else if in.Enabled != nil && *in.Enabled != enabled {
		if *in.Enabled {
			if e = s.setBundledEnabled(ctx, true); e != nil {
				return BundledStatus{}, e
			}
		} else if e = s.disableBundled(ctx); e != nil {
			return BundledStatus{}, e
		}
	}
	if in.PeerPort != nil {
		if e = s.setBundledPeerPort(ctx, *in.PeerPort); e != nil {
			return BundledStatus{}, e
		}
	}
	generation := s.bundled.generation()
	s.bundled.poke()
	if in.Enabled == nil || *in.Enabled {
		if !s.tunnelReady() {
			// The VPN kill switch keeps the daemon stopped; return the honest
			// explanation immediately instead of waiting for the next tick.
			s.bundled.setState("stopped", tunnelDownMessage)
			return s.BundledStatus(ctx)
		}
		if e = s.awaitBundled(ctx, generation); e != nil {
			return BundledStatus{}, e
		}
		c, e := s.managedClientConfig(ctx, builtinClientID)
		if e != nil {
			return BundledStatus{}, e
		}
		if e = s.testDownloadClient(ctx, "qbittorrent", c); e != nil {
			return BundledStatus{}, e
		}
	} else if e = s.awaitBundledStopped(ctx); e != nil {
		return BundledStatus{}, e
	}
	return s.BundledStatus(ctx)
}

// awaitBundledStopped waits until the supervisor reports the daemon stopped, so
// a disable returns the state it promises. A previously failed daemon still
// becomes stopped: the supervisor reconciles the disabled row.
func (s *Service) awaitBundledStopped(ctx context.Context) error {
	deadline := time.Now().Add(builtinStopTimeout + 5*time.Second)
	for {
		state, _ := s.bundled.snapshot()
		if state == "stopped" {
			return nil
		}
		if time.Now().After(deadline) {
			return problem("builtin_timeout", "The built-in client did not stop in time. Review its status before retrying.")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}
}

func (s *Service) createBundled(ctx context.Context) (BundledStatus, error) {
	raw := make([]byte, 24)
	if _, e := rand.Read(raw); e != nil {
		return BundledStatus{}, e
	}
	// The supervised daemon writes only inside its own folder under the
	// download mount, so external-client areas and the rest of the app are
	// untouched. The deployment must mount that folder writable for the
	// container user; the enable flow reports it honestly otherwise.
	folder := filepath.Join(s.cfg.DownloadsDir, "builtin")
	c := ClientConfig{
		URL:          fmt.Sprintf("http://127.0.0.1:%d", builtinWebPort),
		Username:     "reelscarey",
		Password:     hex.EncodeToString(raw),
		DownloadPath: folder,
		LocalFolder:  filepath.Base(folder),
		PeerPort:     defaultBuiltinPeer,
	}
	plain, e := json.Marshal(c)
	if e != nil {
		return BundledStatus{}, e
	}
	sealed, e := s.secrets.Seal("config:"+clientSecretName(builtinClientID), plain)
	if e != nil {
		return BundledStatus{}, e
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return BundledStatus{}, e
	}
	defer tx.Rollback()
	now := time.Now().Unix()
	var count int
	if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM download_clients`).Scan(&count); e != nil {
		return BundledStatus{}, e
	}
	if count >= 32 {
		return BundledStatus{}, problem("invalid_settings", "At most 32 named download clients are supported.")
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO download_clients(id,name,kind,enabled,builtin,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`, builtinClientID, builtinClientName, "qbittorrent", true, true, now, now); e != nil {
		return BundledStatus{}, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO integration_configs(name,ciphertext) VALUES(?,?) ON CONFLICT(name) DO UPDATE SET ciphertext=excluded.ciphertext,updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now')`, clientSecretName(builtinClientID), sealed); e != nil {
		return BundledStatus{}, e
	}
	if e = tx.Commit(); e != nil {
		return BundledStatus{}, e
	}
	return BundledStatus{}, nil
}

func (s *Service) setBundledEnabled(ctx context.Context, enabled bool) error {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	r, e := tx.ExecContext(ctx, `UPDATE download_clients SET enabled=?,revision=revision+1,updated_at=? WHERE id=?`, enabled, time.Now().Unix(), builtinClientID)
	if e != nil {
		return e
	}
	if n, _ := r.RowsAffected(); n != 1 {
		return clientChanged()
	}
	return tx.Commit()
}

// disableBundled refuses while jobs are active or the client is the torrent
// default, mirroring the named-client pause rules.
func (s *Service) disableBundled(ctx context.Context) error {
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
	if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM downloads WHERE client_key=? AND state<>'completed'`, builtinClientID).Scan(&active); e != nil {
		return e
	}
	if active > 0 || d.Torrent == builtinClientID {
		return problem("active_downloads", "Choose another torrent default and finish this client's jobs before disabling the built-in client.")
	}
	r, e := tx.ExecContext(ctx, `UPDATE download_clients SET enabled=0,revision=revision+1,updated_at=? WHERE id=?`, time.Now().Unix(), builtinClientID)
	if e != nil {
		return e
	}
	if n, _ := r.RowsAffected(); n != 1 {
		return clientChanged()
	}
	return tx.Commit()
}

func (s *Service) setBundledPeerPort(ctx context.Context, port int) error {
	if port < 1024 || port > 65535 || port == builtinWebPort {
		return problem("invalid_settings", "Choose a peer port between 1024 and 65535 that is not the built-in API port.")
	}
	c, e := s.managedClientConfig(ctx, builtinClientID)
	if e != nil {
		return e
	}
	c.PeerPort = port
	plain, e := json.Marshal(c)
	if e != nil {
		return e
	}
	sealed, e := s.secrets.Seal("config:"+clientSecretName(builtinClientID), plain)
	if e != nil {
		return e
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	r, e := tx.ExecContext(ctx, `UPDATE download_clients SET revision=revision+1,updated_at=? WHERE id=?`, time.Now().Unix(), builtinClientID)
	if e != nil {
		return e
	}
	if n, _ := r.RowsAffected(); n != 1 {
		return clientChanged()
	}
	if _, e = tx.ExecContext(ctx, `UPDATE integration_configs SET ciphertext=?,updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE name=?`, sealed, clientSecretName(builtinClientID)); e != nil {
		return e
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	return nil
}

// awaitBundled waits until the supervised daemon reports a lifecycle newer
// than the given generation and running, or surfaces the saved failure. It
// never starts the daemon itself.
func (s *Service) awaitBundled(ctx context.Context, before int64) error {
	deadline := time.Now().Add(builtinReadyTimeout)
	for {
		state, message := s.bundled.snapshot()
		generation := s.bundled.generation()
		switch {
		case state == "running" && generation > before:
			return nil
		case state == "failed" && generation > before:
			if message == "" {
				message = "The built-in client could not start."
			}
			return problem("builtin_failed", message)
		}
		if time.Now().After(deadline) {
			return problem("builtin_timeout", "The built-in client did not become ready in time. Review its status before retrying.")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}
}

// qbtPBKDF2 derives the WebUI password hash in qBittorrent's recorded format:
// PBKDF2-HMAC-SHA512, 100000 iterations, a 16-byte salt and a 64-byte key,
// wrapped in the Qt string quoting the client writes back.
func qbtPBKDF2(password string) string {
	salt := make([]byte, 16)
	if _, e := rand.Read(salt); e != nil {
		return ""
	}
	key := pbkdf2.Key([]byte(password), salt, 100000, 64, sha512.New)
	return "\"@ByteArray(" + base64.StdEncoding.EncodeToString(salt) + ":" + base64.StdEncoding.EncodeToString(key) + ")\""
}
