package transfer

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// The built-in Usenet client is a SABnzbd daemon supervised by the application
// process. It is an ordinary named Usenet client to every other subsystem: the
// same explicit per-protocol default, job binding and reconcile-by-job-name
// rules apply. Unlike the torrent client it is never enabled automatically,
// because it cannot do anything without an owner-supplied NNTP provider.
// builtinUsenetClientID is deliberately a 32-character lowercase hex string:
// the existing download-client secret namespace requires that shape.
const (
	builtinUsenetClientID     = "6f1c2b3a4d5e4f60718293a4b5c6d7e8"
	builtinUsenetName         = "Built-in SABnzbd"
	builtinUsenetFolder       = "builtin-usenet"
	builtinUsenetReadyTimeout = 60 * time.Second
	defaultNewsPort           = 563
	maxNewsConnections        = 64
	maxNewsHostLength         = 253
)

// bundledSABWebPort is a package variable so lifecycle tests can point the
// generated configuration at a disposable loopback API server.
var bundledSABWebPort = 8086

// UsenetBundledStatus is the safe status of the supervised daemon and its
// saved provider. It never contains the NNTP password or the daemon API key.
type UsenetBundledStatus struct {
	Present     bool   `json:"present"`
	Installed   bool   `json:"installed"`
	Enabled     bool   `json:"enabled"`
	Configured  bool   `json:"configured"`
	State       string `json:"state"`
	Message     string `json:"message,omitempty"`
	Revision    int    `json:"revision"`
	ActiveJobs  int    `json:"active_jobs"`
	Name        string `json:"name"`
	Host        string `json:"host,omitempty"`
	Username    string `json:"username,omitempty"`
	Port        int    `json:"port"`
	TLS         bool   `json:"tls"`
	Connections int    `json:"connections"`
}

// UsenetBundledUpdate enables or disables the supervised client. Enabling
// requires a saved news server and waits for the daemon's API before it
// reports success. Disabling refuses while jobs are active.
type UsenetBundledUpdate struct {
	Revision int   `json:"revision"`
	Enabled  *bool `json:"enabled,omitempty"`
}

// NewsServerStatus is the safe provider status. The password is deliberately
// absent; the username is shown so an owner can edit other fields without
// accidentally changing it.
type NewsServerStatus struct {
	Configured  bool   `json:"configured"`
	Host        string `json:"host"`
	Username    string `json:"username,omitempty"`
	Port        int    `json:"port"`
	TLS         bool   `json:"tls"`
	Connections int    `json:"connections"`
}

// NewsServerUpdate saves the owner's NNTP provider. A nil Password retains the
// saved one; changing the host or username requires entering it again.
type NewsServerUpdate struct {
	Host        string  `json:"host"`
	Port        int     `json:"port"`
	TLS         bool    `json:"tls"`
	Username    string  `json:"username"`
	Password    *string `json:"password,omitempty"`
	Connections int     `json:"connections"`
}

func bundledSABBinary() string {
	if v := strings.TrimSpace(os.Getenv("REELSCAREY_BUNDLED_SAB")); v != "" {
		return v
	}
	if p, e := exec.LookPath("sabnzbdplus"); e == nil {
		return p
	}
	if p, e := exec.LookPath("sabnzbd"); e == nil {
		return p
	}
	return ""
}

// redactSABText removes the generated API key and the NNTP password from
// bounded process output before it reaches a status message or log line.
func redactSABText(text string, c ClientConfig) string {
	for _, secret := range []string{c.APIKey, c.NewsPassword} {
		if secret != "" {
			text = strings.ReplaceAll(text, secret, "[redacted]")
		}
	}
	return cleanClientText(text, 1200)
}

// bundledSABAvailable reports whether the resolved binary can actually be
// executed by this process. REELSCAREY_BUNDLED_SAB may name a command in PATH.
func bundledSABAvailable() bool {
	binary := bundledSABBinary()
	if binary == "" {
		return false
	}
	if _, e := os.Stat(binary); e == nil {
		return true
	}
	if !strings.ContainsAny(binary, `/\`) {
		_, e := exec.LookPath(binary)
		return e == nil
	}
	return false
}

func (s *Service) bundledUsenetRow(ctx context.Context) (present, enabled bool, revision int, err error) {
	var e bool
	var r int
	scan := s.db.QueryRowContext(ctx, `SELECT enabled,revision FROM download_clients WHERE id=?`, builtinUsenetClientID).Scan(&e, &r)
	if errors.Is(scan, sql.ErrNoRows) {
		return false, false, 0, nil
	}
	if scan != nil {
		return false, false, 0, scan
	}
	return true, e, r, nil
}

func newsConfigured(c ClientConfig) bool {
	return strings.TrimSpace(c.NewsHost) != "" && c.NewsPort > 0 && c.NewsConnections > 0 && (c.NewsUsername == "" || c.NewsPassword != "")
}

func newsTextOK(v string, max int) bool {
	return len(v) <= max && !strings.ContainsFunc(v, unicode.IsControl)
}

// iniQuoteOK mirrors the quoting fallback used by sabINI: a value must not
// contain both triple-quote styles, and it can never contain control bytes.
func iniQuoteOK(v string) bool {
	if strings.ContainsAny(v, "\r\n\x00") {
		return false
	}
	return !(strings.Contains(v, `"""`) && strings.Contains(v, `'''`))
}

func newsHostOK(host string) bool {
	if host == "" || len(host) > maxNewsHostLength {
		return false
	}
	if net.ParseIP(host) != nil {
		return true
	}
	for _, r := range host {
		if r == '.' || r == '-' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}

func normalizeNewsServer(in NewsServerUpdate) (NewsServerUpdate, error) {
	in.Host = strings.TrimSpace(in.Host)
	in.Username = strings.TrimSpace(in.Username)
	if !newsHostOK(in.Host) {
		return in, problem("invalid_settings", "Enter the news server hostname or IP address without a scheme, port or path.")
	}
	if in.Port < 1 || in.Port > 65535 {
		return in, problem("invalid_settings", "Enter the news server port between 1 and 65535 (563 for TLS, 119 without TLS).")
	}
	if in.Connections < 1 || in.Connections > maxNewsConnections {
		return in, problem("invalid_settings", "Enter between 1 and 64 news server connections.")
	}
	if !newsTextOK(in.Username, 200) {
		return in, problem("invalid_settings", "Enter a valid news server username, or leave it blank for IP-based access.")
	}
	if !iniQuoteOK(in.Username) {
		return in, problem("invalid_settings", "The news server username contains an unsupported quote sequence.")
	}
	if in.Password != nil && (!newsTextOK(*in.Password, 2048) || !iniQuoteOK(*in.Password)) {
		return in, problem("invalid_settings", "The news server password is too long or contains unsupported characters.")
	}
	return in, nil
}

// TestNewsServer only proves that the configured host and port accept a TCP
// connection. It does not authenticate or download an article: an unrelated
// listener, an expired account or a missing newsgroup can still fail later.
func TestNewsServer(ctx context.Context, host string, port int) error {
	host = strings.TrimSpace(host)
	if !newsHostOK(host) || port < 1 || port > 65535 {
		return problem("invalid_settings", "Enter a valid news server host and port before testing.")
	}
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	dialer := net.Dialer{Timeout: 5 * time.Second}
	conn, e := dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if e != nil {
		return unavailable("The news server did not accept a TCP connection. Check the host, port, TLS setting and the container's network access.")
	}
	_ = conn.Close()
	return nil
}

// BundledUsenetStatus never requires the daemon: a stopped or failed state is
// reported honestly with its saved message.
func (s *Service) BundledUsenetStatus(ctx context.Context) (UsenetBundledStatus, error) {
	out := UsenetBundledStatus{Name: builtinUsenetName, State: "stopped", Port: defaultNewsPort, Installed: bundledSABAvailable()}
	present, enabled, revision, e := s.bundledUsenetRow(ctx)
	if e != nil {
		return out, e
	}
	out.Present, out.Enabled, out.Revision = present, enabled, revision
	if present {
		c, e := s.managedClientConfig(ctx, builtinUsenetClientID)
		if e != nil {
			var p *Error
			if errors.As(e, &p) {
				out.Message = p.Message
			} else {
				out.Message = "The built-in Usenet client configuration could not be read."
			}
		} else {
			out.Host = c.NewsHost
			out.Username = c.NewsUsername
			if c.NewsPort > 0 {
				out.Port = c.NewsPort
			}
			out.TLS = c.NewsTLS
			out.Connections = c.NewsConnections
			out.Configured = newsConfigured(c)
		}
		if e := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM downloads WHERE client_key=? AND state NOT IN ('completed','cancelled')`, builtinUsenetClientID).Scan(&out.ActiveJobs); e != nil {
			return out, e
		}
	}
	state, snapshotMessage := s.usenet.snapshot()
	out.State = state
	if snapshotMessage != "" {
		out.Message = snapshotMessage
	}
	return out, nil
}

// NewsServer reads the safe saved provider settings.
func (s *Service) NewsServer(ctx context.Context) (NewsServerStatus, error) {
	var out NewsServerStatus
	present, _, _, e := s.bundledUsenetRow(ctx)
	if e != nil {
		return out, e
	}
	if !present {
		return out, nil
	}
	c, e := s.managedClientConfig(ctx, builtinUsenetClientID)
	if e != nil {
		return out, e
	}
	out = NewsServerStatus{Configured: newsConfigured(c), Host: c.NewsHost, Username: c.NewsUsername, Port: c.NewsPort, TLS: c.NewsTLS, Connections: c.NewsConnections}
	return out, nil
}

// SaveNewsServer validates and encrypts the NNTP provider, creating the paused
// built-in client row on first use. It never enables the daemon and never
// contacts the provider: saving an offline but valid provider is allowed, and
// the separate Test action reports reachability honestly.
func (s *Service) SaveNewsServer(ctx context.Context, in NewsServerUpdate) (UsenetBundledStatus, error) {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	in, e := normalizeNewsServer(in)
	if e != nil {
		return UsenetBundledStatus{}, e
	}
	present, _, revision, e := s.bundledUsenetRow(ctx)
	if e != nil {
		return UsenetBundledStatus{}, e
	}
	var c ClientConfig
	if present {
		c, e = s.managedClientConfig(ctx, builtinUsenetClientID)
		if e != nil {
			return UsenetBundledStatus{}, e
		}
	}
	password := c.NewsPassword
	if in.Password != nil {
		password = *in.Password
	}
	if !newsTextOK(password, 2048) || !iniQuoteOK(password) {
		return UsenetBundledStatus{}, problem("invalid_settings", "The news server password is too long or contains unsupported characters.")
	}
	if in.Username != "" && password == "" {
		return UsenetBundledStatus{}, problem("invalid_settings", "Enter the news server password, or leave the username blank for IP-based access.")
	}
	if present && c.NewsHost != "" && (c.NewsHost != in.Host || c.NewsUsername != in.Username) && in.Password == nil && c.NewsPassword != "" {
		return UsenetBundledStatus{}, problem("invalid_settings", "Re-enter the news server password when changing the host or username.")
	}
	if !present {
		folder := filepath.Join(s.cfg.DownloadsDir, builtinUsenetFolder)
		key, e := randomHex(16)
		if e != nil {
			return UsenetBundledStatus{}, e
		}
		c = ClientConfig{
			URL:          fmt.Sprintf("http://127.0.0.1:%d", bundledSABWebPort),
			APIKey:       key,
			DownloadPath: folder,
			LocalFolder:  builtinUsenetFolder,
		}
	}
	c.NewsHost = in.Host
	c.NewsPort = in.Port
	c.NewsTLS = in.TLS
	c.NewsUsername = in.Username
	c.NewsPassword = password
	c.NewsConnections = in.Connections
	if e = s.saveBuiltinUsenetConfig(ctx, c, present, revision); e != nil {
		return UsenetBundledStatus{}, e
	}
	s.usenet.poke()
	return s.BundledUsenetStatus(ctx)
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	return hex.EncodeToString(b), nil
}

func (s *Service) saveBuiltinUsenetConfig(ctx context.Context, c ClientConfig, present bool, revision int) error {
	plain, e := json.Marshal(c)
	if e != nil {
		return e
	}
	sealed, e := s.secrets.Seal("config:"+clientSecretName(builtinUsenetClientID), plain)
	if e != nil {
		return e
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var duplicate int
	if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM download_clients WHERE name=? COLLATE NOCASE AND id<>?`, builtinUsenetName, builtinUsenetClientID).Scan(&duplicate); e != nil {
		return e
	}
	if duplicate > 0 {
		return problem("invalid_settings", "Another download client already uses the name "+builtinUsenetName+". Rename it first.")
	}
	if present {
		var builtin bool
		if e = tx.QueryRowContext(ctx, `SELECT builtin FROM download_clients WHERE id=?`, builtinUsenetClientID).Scan(&builtin); e != nil {
			return e
		}
		if !builtin {
			return problem("invalid_selection", "That download client is not the supervised Usenet client.")
		}
		r, e := tx.ExecContext(ctx, `UPDATE download_clients SET revision=revision+1,updated_at=? WHERE id=? AND revision=?`, time.Now().Unix(), builtinUsenetClientID, revision)
		if e != nil {
			return e
		}
		if n, _ := r.RowsAffected(); n != 1 {
			return clientChanged()
		}
	} else {
		var count int
		if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM download_clients`).Scan(&count); e != nil {
			return e
		}
		if count >= 32 {
			return problem("invalid_settings", "At most 32 named download clients are supported.")
		}
		now := time.Now().Unix()
		if _, e = tx.ExecContext(ctx, `INSERT INTO download_clients(id,name,kind,enabled,builtin,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`, builtinUsenetClientID, builtinUsenetName, "sabnzbd", false, true, now, now); e != nil {
			return e
		}
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO integration_configs(name,ciphertext) VALUES(?,?) ON CONFLICT(name) DO UPDATE SET ciphertext=excluded.ciphertext,updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now')`, clientSecretName(builtinUsenetClientID), sealed); e != nil {
		return e
	}
	return tx.Commit()
}

// UpdateBundledUsenet owns the lifecycle of the supervised client. Enabling
// requires a saved provider, starts the daemon and waits for its API before the
// row is marked enabled. A failure leaves the honest failed status visible.
func (s *Service) UpdateBundledUsenet(ctx context.Context, in UsenetBundledUpdate) (UsenetBundledStatus, error) {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	present, enabled, revision, e := s.bundledUsenetRow(ctx)
	if e != nil {
		return UsenetBundledStatus{}, e
	}
	if present && in.Revision != revision {
		return UsenetBundledStatus{}, clientChanged()
	}
	if !present && in.Revision != 0 {
		return UsenetBundledStatus{}, clientChanged()
	}
	if !present {
		return UsenetBundledStatus{}, problem("client_not_configured", "Save a news server before turning on the built-in Usenet client.")
	}
	target := enabled
	if in.Enabled != nil {
		target = *in.Enabled
	}
	if target != enabled {
		if target {
			c, e := s.managedClientConfig(ctx, builtinUsenetClientID)
			if e != nil {
				return UsenetBundledStatus{}, e
			}
			if !newsConfigured(c) {
				return UsenetBundledStatus{}, problem("client_not_configured", "Save a news server before turning on the built-in Usenet client.")
			}
			if e = s.setBundledUsenetEnabled(ctx, true); e != nil {
				return UsenetBundledStatus{}, e
			}
		} else if e = s.disableBundledUsenet(ctx); e != nil {
			return UsenetBundledStatus{}, e
		}
	}
	generation := s.usenet.generation()
	s.usenet.poke()
	if target {
		c, e := s.managedClientConfig(ctx, builtinUsenetClientID)
		if e != nil {
			return UsenetBundledStatus{}, e
		}
		if !newsConfigured(c) {
			return UsenetBundledStatus{}, problem("client_not_configured", "Save a news server before turning on the built-in Usenet client.")
		}
		_, _, currentRevision, e := s.bundledUsenetRow(ctx)
		if e != nil {
			return UsenetBundledStatus{}, e
		}
		s.usenet.procMu.Lock()
		applied := s.usenet.cmd != nil && s.usenet.appliedRevision == currentRevision
		s.usenet.procMu.Unlock()
		if !applied {
			if e = s.awaitBundledUsenet(ctx, generation); e != nil {
				return UsenetBundledStatus{}, e
			}
		}
		if e = s.sab.Test(ctx, c); e != nil {
			return UsenetBundledStatus{}, e
		}
	} else if e = s.awaitBundledUsenetStopped(ctx); e != nil {
		return UsenetBundledStatus{}, e
	}
	return s.BundledUsenetStatus(ctx)
}

func (s *Service) setBundledUsenetEnabled(ctx context.Context, enabled bool) error {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	r, e := tx.ExecContext(ctx, `UPDATE download_clients SET enabled=?,revision=revision+1,updated_at=? WHERE id=?`, enabled, time.Now().Unix(), builtinUsenetClientID)
	if e != nil {
		return e
	}
	if n, _ := r.RowsAffected(); n != 1 {
		return clientChanged()
	}
	return tx.Commit()
}

// disableBundledUsenet refuses while jobs are active and clears its Usenet
// default so the owner is prompted to connect a client. Existing jobs keep the
// client they were reserved with.
func (s *Service) disableBundledUsenet(ctx context.Context) error {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var active int
	if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM downloads WHERE client_key=? AND state NOT IN ('completed','cancelled')`, builtinUsenetClientID).Scan(&active); e != nil {
		return e
	}
	if active > 0 {
		return problem("active_downloads", "Finish this client's jobs or choose another Usenet default before turning off the built-in client.")
	}
	d, e := defaultsTx(ctx, tx)
	if e != nil {
		return e
	}
	if d.Usenet == builtinUsenetClientID {
		if _, e = tx.ExecContext(ctx, `UPDATE download_client_preferences SET usenet='',revision=revision+1 WHERE id=1 AND usenet=?`, builtinUsenetClientID); e != nil {
			return e
		}
	}
	r, e := tx.ExecContext(ctx, `UPDATE download_clients SET enabled=0,revision=revision+1,updated_at=? WHERE id=?`, time.Now().Unix(), builtinUsenetClientID)
	if e != nil {
		return e
	}
	if n, _ := r.RowsAffected(); n != 1 {
		return clientChanged()
	}
	return tx.Commit()
}

func (s *Service) awaitBundledUsenet(ctx context.Context, before int64) error {
	deadline := time.Now().Add(builtinUsenetReadyTimeout + 10*time.Second)
	for {
		state, message := s.usenet.snapshot()
		generation := s.usenet.generation()
		switch {
		case state == "running" && generation > before:
			return nil
		case state == "failed" && generation > before:
			if message == "" {
				message = "The built-in Usenet client could not start."
			}
			return problem("builtin_failed", message)
		}
		if time.Now().After(deadline) {
			return problem("builtin_timeout", "The built-in Usenet client did not become ready in time. Review its status before retrying.")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}
}

func (s *Service) awaitBundledUsenetStopped(ctx context.Context) error {
	deadline := time.Now().Add(builtinStopTimeout + 10*time.Second)
	for {
		state, _ := s.usenet.snapshot()
		if state == "stopped" {
			return nil
		}
		if time.Now().After(deadline) {
			return problem("builtin_timeout", "The built-in Usenet client did not stop in time. Review its status before retrying.")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}
}

// SuperviseBundledUsenet reconciles the supervised SABnzbd daemon with the
// saved built-in connection. It never enables an owner-disabled client.
func (s *Service) SuperviseBundledUsenet(ctx context.Context, logger *slog.Logger) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	defer s.stopRuntime(&s.usenet)
	for {
		c, revision, desired, e := s.bundledUsenetDesired(ctx)
		if e != nil {
			if ctx.Err() != nil {
				return
			}
			logger.Warn("built-in Usenet client state unavailable", "error", e)
			s.usenet.finishFailed("The built-in Usenet client state could not be read. Review the application logs.")
		} else {
			s.reconcileBundledUsenet(ctx, logger, c, revision, desired)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-s.usenet.notify:
		}
	}
}

func (s *Service) bundledUsenetDesired(ctx context.Context) (ClientConfig, int, bool, error) {
	present, enabled, revision, e := s.bundledUsenetRow(ctx)
	if e != nil || !present || !enabled {
		return ClientConfig{}, 0, false, e
	}
	c, e := s.managedClientConfig(ctx, builtinUsenetClientID)
	if e != nil {
		return ClientConfig{}, 0, false, e
	}
	if !newsConfigured(c) {
		return ClientConfig{}, 0, false, problem("client_not_configured", "The built-in Usenet client is enabled but has no news server. Save a news server or turn the client off.")
	}
	return c, revision, true, nil
}

func (s *Service) reconcileBundledUsenet(ctx context.Context, logger *slog.Logger, c ClientConfig, revision int, desired bool) {
	r := &s.usenet
	r.reap()
	r.procMu.Lock()
	wasAlive := r.cmd != nil
	stale := wasAlive && r.appliedRevision != revision
	r.procMu.Unlock()
	if !desired {
		if wasAlive {
			s.stopRuntime(r)
		} else {
			r.setState("stopped", "")
		}
		return
	}
	if wasAlive && !stale {
		return
	}
	if stale {
		s.stopRuntime(r)
	}
	if time.Since(r.lastAttempt) < bundledBackoff(r.attempts) {
		return
	}
	r.attempts++
	r.lastAttempt = time.Now()
	if e := s.startBundledUsenet(ctx, c, revision); e != nil {
		var p *Error
		message := "The built-in Usenet client could not start."
		if errors.As(e, &p) {
			message = p.Message
		} else {
			message = fmt.Sprintf("The built-in Usenet client could not start: %v", e)
		}
		r.finishFailed(message)
		logger.Warn("built-in Usenet client start failed", "error", e)
		return
	}
	r.attempts = 0
	logger.Info("built-in Usenet client is running")
}

// sabINI renders the dedicated SABnzbd configuration. Only this application
// ever reads it; the file is 0600 inside a 0700 profile directory. SABnzbd
// needs the NNTP password in clear text to authenticate, exactly like any
// other SABnzbd installation.
func (s *Service) sabINI(c ClientConfig) (string, error) {
	profile := filepath.Join(s.cfg.ConfigDir, "sabnzbd")
	downloadDir := filepath.Join(c.DownloadPath, "incomplete")
	quote := func(v string) (string, error) {
		if strings.ContainsAny(v, "\r\n\x00") {
			return "", problem("invalid_settings", "The news server settings contain unsupported control characters.")
		}
		if !strings.Contains(v, `"""`) {
			return `"""` + v + `"""`, nil
		}
		if !strings.Contains(v, `'''`) {
			return `'''` + v + `'''`, nil
		}
		return "", problem("invalid_settings", "The news server credentials contain an unsupported quote sequence.")
	}
	for _, v := range []string{c.DownloadPath, downloadDir, profile, c.NewsUsername, c.NewsPassword} {
		if _, e := quote(v); e != nil {
			return "", e
		}
	}
	qDownload, _ := quote(c.DownloadPath)
	qIncomplete, _ := quote(downloadDir)
	qAdmin, _ := quote(filepath.Join(profile, "admin"))
	qUser, _ := quote(c.NewsUsername)
	qPass, _ := quote(c.NewsPassword)
	qCategoryDir, _ := quote("ReelScarey")
	ssl := 0
	if c.NewsTLS {
		ssl = 1
	}
	var b strings.Builder
	b.WriteString("__version__ = 19\n")
	b.WriteString("[misc]\n")
	b.WriteString("api_key = " + c.APIKey + "\n")
	b.WriteString("nzb_key = " + c.APIKey + "\n")
	b.WriteString("host = 127.0.0.1\n")
	b.WriteString("port = " + strconv.Itoa(bundledSABWebPort) + "\n")
	b.WriteString("download_dir = " + qIncomplete + "\n")
	b.WriteString("complete_dir = " + qDownload + "\n")
	b.WriteString("admin_dir = " + qAdmin + "\n")
	b.WriteString("folder_rename = 1\n")
	b.WriteString("enable_par_cleanup = 1\n")
	b.WriteString("enable_unrar = 1\n")
	b.WriteString("enable_7zip = 1\n")
	b.WriteString("[servers]\n")
	b.WriteString("[[ReelScarey]]\n")
	b.WriteString("displayname = ReelScarey\n")
	b.WriteString("host = " + c.NewsHost + "\n")
	b.WriteString("port = " + strconv.Itoa(c.NewsPort) + "\n")
	b.WriteString("username = " + qUser + "\n")
	b.WriteString("password = " + qPass + "\n")
	b.WriteString("connections = " + strconv.Itoa(c.NewsConnections) + "\n")
	b.WriteString("ssl = " + strconv.Itoa(ssl) + "\n")
	b.WriteString("enable = 1\n")
	b.WriteString("[categories]\n")
	b.WriteString("[[" + sabCategory + "]]\n")
	b.WriteString("order = 0\n")
	b.WriteString("pp = 2\n")
	b.WriteString("script = None\n")
	b.WriteString("dir = " + qCategoryDir + "\n")
	b.WriteString("priority = 0\n")
	return b.String(), nil
}

func (s *Service) startBundledUsenet(ctx context.Context, c ClientConfig, revision int) error {
	binary := bundledSABBinary()
	if binary == "" {
		return problem("builtin_missing", "sabnzbdplus is not installed in this deployment. The Docker image ships it; native installs must provide it or set REELSCAREY_BUNDLED_SAB.")
	}
	if !newsConfigured(c) {
		return problem("client_not_configured", "The built-in Usenet client has no news server. Save one before starting it.")
	}
	profile := filepath.Join(s.cfg.ConfigDir, "sabnzbd")
	if e := os.MkdirAll(filepath.Join(profile, "admin"), 0o700); e != nil {
		return e
	}
	if e := os.MkdirAll(filepath.Join(c.DownloadPath, "incomplete"), 0o700); e != nil {
		return problem("invalid_settings", "The built-in Usenet download folder is not writable. Mount downloads writable for this container user.")
	}
	ini, e := s.sabINI(c)
	if e != nil {
		return e
	}
	iniPath := filepath.Join(profile, "sabnzbd.ini")
	if e = os.WriteFile(iniPath, []byte(ini), 0o600); e != nil {
		return e
	}
	cmd := exec.Command(binary, "--config-file", iniPath, "--server", fmt.Sprintf("127.0.0.1:%d", bundledSABWebPort), "--nobrowser", "--new")
	// Give the daemon a writable home inside its private profile; the runtime
	// image's application user deliberately has no home directory.
	cmd.Env = append(os.Environ(), "HOME="+profile)
	hideProbeWindow(cmd)
	stderr := &lockedBuffer{}
	stderr.buf.max = 64 << 10
	cmd.Stderr = stderr
	var stdout lockedBuffer
	stdout.buf.max = 16 << 10
	cmd.Stdout = &stdout
	if e := cmd.Start(); e != nil {
		return e
	}
	r := &s.usenet
	wait := make(chan error, 1)
	r.procMu.Lock()
	r.cmd = cmd
	r.wait = wait
	r.stderr = stderr
	r.redact = []string{c.APIKey, c.NewsPassword}
	r.appliedRevision = revision
	r.setState("starting", "")
	r.procMu.Unlock()
	go func() { wait <- cmd.Wait() }()
	deadline := time.Now().Add(builtinUsenetReadyTimeout)
	for {
		if e := s.sab.Test(ctx, c); e == nil {
			r.stateMu.Lock()
			r.lifecycle++
			r.state, r.message = "running", ""
			r.stateMu.Unlock()
			return nil
		} else if ctx.Err() != nil {
			return ctx.Err()
		}
		select {
		case <-wait:
			r.procMu.Lock()
			r.cmd, r.wait = nil, nil
			r.procMu.Unlock()
			detail := "SABnzbd exited during startup."
			if strings.TrimSpace(stderr.String()) != "" {
				detail += " " + strings.TrimSpace(stderr.String())
			} else if strings.TrimSpace(stdout.String()) != "" {
				detail += " " + strings.TrimSpace(stdout.String())
			}
			return problem("builtin_start", redactSABText(detail, c))
		case <-time.After(500 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			_ = cmd.Process.Kill()
			<-wait
			r.procMu.Lock()
			r.cmd, r.wait = nil, nil
			r.procMu.Unlock()
			return problem("builtin_timeout", "The built-in Usenet client API did not become reachable in time. Review its status before retrying.")
		}
	}
}
