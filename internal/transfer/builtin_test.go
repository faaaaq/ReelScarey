package transfer

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

func TestBundledClientEnableDisableLifecycle(t *testing.T) {
	f := setupWorkflow(t)
	t.Setenv("REELSCAREY_BUNDLED_QBT", bundledStubBinary(t))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		f.s.SuperviseBundled(ctx, slog.New(slog.DiscardHandler))
	}()
	status, e := f.s.UpdateBundled(ctx, BundledUpdate{Enabled: boolPtr(true)})
	if e != nil || !status.Present || !status.Enabled {
		t.Fatal(status, e)
	}
	if status.PeerPort != defaultBuiltinPeer || status.State != "running" {
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
	// The connection starts paused: it becomes the torrent default only after
	// an explicit owner choice.
	defaults, e := f.s.ClientDefaults(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.SaveClientDefaults(ctx, ClientDefaults{Torrent: builtinClientID, Revision: defaults.Revision}); e != nil {
		t.Fatal("the tested built-in client could not become the default", e)
	}
	if _, e = f.s.UpdateBundled(ctx, BundledUpdate{Enabled: boolPtr(false), Revision: status.Revision}); e == nil {
		t.Fatal("disabling the torrent default was accepted")
	}
	defaults, e = f.s.ClientDefaults(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.SaveClientDefaults(ctx, ClientDefaults{Torrent: "", Revision: defaults.Revision}); e != nil {
		t.Fatal(e)
	}
	saved, e := f.s.UpdateBundled(ctx, BundledUpdate{Enabled: boolPtr(false), Revision: status.Revision})
	if e != nil || saved.Enabled {
		t.Fatal(saved, e)
	}
	awaitBundledState(t, f, "stopped")
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
	status, e := f.s.UpdateBundled(ctx, BundledUpdate{Enabled: boolPtr(true)})
	if e != nil || status.State != "running" {
		t.Fatal(status, e)
	}
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
