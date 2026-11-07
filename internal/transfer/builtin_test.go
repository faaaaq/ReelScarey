package transfer

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func boolPtr(v bool) *bool { return &v }

// bundledStubBinary builds a stand-in daemon that stays alive until signaled,
// so lifecycle tests run without qbittorrent-nox. Live behavior with the real
// daemon is covered by the opt-in live acceptance.
func bundledStubBinary(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "stubmain.go")
	source := `package main

import (
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	select {
	case <-c:
	case <-time.After(2 * time.Minute):
	}
}
`
	if e := os.WriteFile(src, []byte(source), 0o600); e != nil {
		t.Fatal(e)
	}
	out := filepath.Join(dir, "reelscarey-bundled-stub")
	if runtime.GOOS == "windows" {
		out += ".exe"
	}
	build := exec.Command("go", "build", "-o", out, src)
	if b, e := build.CombinedOutput(); e != nil {
		t.Fatalf("stub build: %v %s", e, b)
	}
	return out
}

func awaitBundledState(t *testing.T, f *workflowFixture, state string) BundledStatus {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		out, e := f.s.BundledStatus(context.Background())
		if e != nil {
			t.Fatal(e)
		}
		if out.State == state {
			return out
		}
		if time.Now().After(deadline) {
			t.Fatalf("built-in client never reached %q: %+v", state, out)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// The included client is provisioned by the supervision loop: a deployment
// with qbittorrent-nox downloads with it immediately, and it becomes the
// torrent default when no default is chosen. Disabling it clears that default
// and is never silently undone.
func TestBundledClientIncludedByDefault(t *testing.T) {
	f := setupWorkflow(t)
	t.Setenv("REELSCAREY_BUNDLED_QBT", bundledStubBinary(t))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		f.s.SuperviseBundled(ctx, slog.New(slog.DiscardHandler))
	}()
	status := awaitBundledState(t, f, "running")
	if !status.Present || !status.Enabled {
		t.Fatal(status)
	}
	if status.PeerPort != defaultBuiltinPeer {
		t.Fatal(status)
	}
	conf, e := os.ReadFile(filepath.Join(f.cfg.ConfigDir, "qbittorrent", "qBittorrent", "config", "qBittorrent.conf"))
	if e != nil || !containsLine(string(conf), "WebUI\\Address=127.0.0.1") || !containsLine(string(conf), "WebUI\\Port=8085") || !containsLine(string(conf), "Session\\Port=6881") {
		t.Fatal("generated configuration is missing the supervised settings", string(conf), e)
	}
	clients, e := f.s.DownloadClients(ctx)
	if e != nil {
		t.Fatal(e)
	}
	var listed *DownloadClient
	for n := range clients {
		if clients[n].ID == builtinClientID {
			listed = &clients[n]
		}
	}
	if listed == nil || !listed.Builtin || !listed.Enabled {
		t.Fatal("built-in connection missing from the client list", clients)
	}
	defaults, e := f.s.ClientDefaults(ctx)
	if e != nil || defaults.Torrent != builtinClientID {
		t.Fatal("the included client did not become the torrent default", defaults, e)
	}
	// Opting out clears the included client's default and stops the daemon.
	saved, e := f.s.UpdateBundled(ctx, BundledUpdate{Enabled: boolPtr(false), Revision: status.Revision})
	if e != nil || saved.Enabled {
		t.Fatal(saved, e)
	}
	defaults, e = f.s.ClientDefaults(ctx)
	if e != nil || defaults.Torrent != "" {
		t.Fatal("disabling the built-in client kept the torrent default", defaults, e)
	}
	awaitBundledState(t, f, "stopped")
	// The supervisor never re-enables an owner-disabled client.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		check, e := f.s.BundledStatus(ctx)
		if e != nil {
			t.Fatal(e)
		}
		if check.Enabled {
			t.Fatal("the disabled built-in client was re-enabled", check)
		}
		time.Sleep(300 * time.Millisecond)
	}
	cancel()
	<-done
	if _, e = os.Stat(filepath.Join(f.cfg.ConfigDir, "qbittorrent")); e != nil {
		t.Fatal("generated configuration vanished", e)
	}
}

func TestBundledClientPeerPortChangeRestartsDaemon(t *testing.T) {
	f := setupWorkflow(t)
	t.Setenv("REELSCAREY_BUNDLED_QBT", bundledStubBinary(t))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	go func() {
		defer close(done)
		f.s.SuperviseBundled(ctx, logger)
	}()
	status := awaitBundledState(t, f, "running")
	port := 6999
	saved, e := f.s.UpdateBundled(ctx, BundledUpdate{PeerPort: &port, Revision: status.Revision})
	if e != nil || saved.PeerPort != 6999 || saved.State != "running" {
		t.Fatal(saved, e)
	}
	conf, e := os.ReadFile(filepath.Join(f.cfg.ConfigDir, "qbittorrent", "qBittorrent", "config", "qBittorrent.conf"))
	if e != nil || !containsLine(string(conf), "Session\\Port=6999") {
		t.Fatal("peer port change did not reach the generated configuration", string(conf), e)
	}
	cancel()
	<-done
}

func containsLine(text, want string) bool {
	start := 0
	for start <= len(text) {
		end := len(text)
		for i := start; i < len(text); i++ {
			if text[i] == '\n' {
				end = i
				break
			}
		}
		if text[start:end] == want {
			return true
		}
		start = end + 1
		if end == len(text) {
			break
		}
	}
	return false
}

func TestBundledClientMissingBinaryFailsHonestly(t *testing.T) {
	f := setupWorkflow(t)
	t.Setenv("REELSCAREY_BUNDLED_QBT", filepath.Join(t.TempDir(), "missing-binary"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		f.s.SuperviseBundled(ctx, slog.New(slog.DiscardHandler))
	}()
	if _, e := f.s.UpdateBundled(ctx, BundledUpdate{Enabled: boolPtr(true)}); e == nil {
		t.Fatal("enabling without a daemon binary succeeded")
	} else {
		var p *Error
		if !errors.As(e, &p) || (p.Code != "builtin_failed" && p.Code != "builtin_missing") {
			t.Fatal(e)
		}
	}
	status := awaitBundledState(t, f, "failed")
	if !status.Enabled || status.Message == "" {
		t.Fatal(status)
	}
	saved, e := f.s.UpdateBundled(ctx, BundledUpdate{Enabled: boolPtr(false), Revision: status.Revision})
	if e != nil || saved.Enabled {
		t.Fatal(saved, e)
	}
	awaitBundledState(t, f, "stopped")
	cancel()
	<-done
}

func TestBundledClientWaitsForVPNTunnel(t *testing.T) {
	f := setupWorkflow(t)
	t.Setenv("REELSCAREY_BUNDLED_QBT", bundledStubBinary(t))
	allowed, bind := false, false
	f.s.SetTunnelState(func() (bool, bool) { return allowed, bind })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		f.s.SuperviseBundled(ctx, slog.New(slog.DiscardHandler))
	}()
	// The included client is provisioned enabled; with the kill switch on and
	// the tunnel down it stays stopped with the honest explanation.
	deadline := time.Now().Add(20 * time.Second)
	var status BundledStatus
	for {
		s, e := f.s.BundledStatus(ctx)
		if e != nil {
			t.Fatal(e)
		}
		if s.Enabled {
			status = s
			if s.State == "stopped" {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("the included client never became enabled and stopped: %+v", s)
		}
		time.Sleep(200 * time.Millisecond)
	}
	if status.State != "stopped" {
		t.Fatal(status)
	}
	if !strings.Contains(status.Message, "kill switch") {
		t.Fatal("the kill-switch state was not honest", status)
	}
	if d, e := f.s.ClientDefaults(ctx); e != nil || d.Torrent != builtinClientID {
		t.Fatal("the included client did not become the torrent default", d, e)
	}
	allowed, bind = true, true
	f.s.bundled.poke()
	awaitBundledState(t, f, "running")
	saved, e := f.s.BundledStatus(ctx)
	if e != nil || saved.State != "running" || saved.Message != "" {
		t.Fatal(saved, e)
	}
	// The generated configuration binds the daemon to the tunnel interface.
	awaitBundledConfig(t, f, true)
	// With the kill switch off and the tunnel down, torrents may run, but must
	// not claim the tunnel binding.
	allowed, bind = true, false
	f.s.bundled.poke()
	awaitBundledConfig(t, f, false)
	if state := awaitBundledState(t, f, "running"); state.Message != "" {
		t.Fatal(state)
	}
	// With the kill switch back on and the tunnel down, the daemon stops.
	allowed, bind = false, false
	f.s.bundled.poke()
	stopped := awaitBundledState(t, f, "stopped")
	if !strings.Contains(stopped.Message, "kill switch") {
		t.Fatal(stopped)
	}
	cancel()
	<-done
}

func awaitBundledConfig(t *testing.T, f *workflowFixture, wantBound bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		conf, e := os.ReadFile(filepath.Join(f.cfg.ConfigDir, "qbittorrent", "qBittorrent", "config", "qBittorrent.conf"))
		if e == nil {
			bound := strings.Contains(string(conf), "Session\\Interface=reelscarey0")
			if bound == wantBound {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("tunnel binding never became %v", wantBound)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestBundledClientRefusesGenericEditing(t *testing.T) {
	f := setupWorkflow(t)
	ctx := context.Background()
	if _, e := f.db.Exec(`INSERT INTO download_clients(id,name,kind,enabled,builtin,revision,created_at,updated_at) VALUES(?,?,?,0,1,1,1,1)`, builtinClientID, builtinClientName, "qbittorrent"); e != nil {
		t.Fatal(e)
	}
	if _, e := f.s.SaveDownloadClient(ctx, DownloadClientUpdate{ID: builtinClientID, Name: "Renamed", Kind: "qbittorrent", URL: "http://127.0.0.1:1", DownloadPath: "/downloads", Revision: 1}); e == nil {
		t.Fatal("generic edit accepted for the built-in client")
	}
	if e := f.s.DeleteDownloadClient(ctx, builtinClientID, 1); e == nil {
		t.Fatal("delete accepted for the built-in client")
	}
	if e := f.s.TestDownloadClient(ctx, builtinClientID); e == nil {
		// Testing is permitted, but without a saved configuration it must fail.
		t.Fatal("testing the unconfigured built-in client unexpectedly succeeded")
	}
}
