package vpn

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/faaaaq/ReelScarey/internal/config"
	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/securestore"
)

const (
	testPrivateKey = "8JdLXCqZ0yrn0QOGHhIpFmHZwbuSu8Z7bzXvcRAArE0="
	testPeerKey    = "oH7ElLz1YlHNsqF9z5PZWGhEZ12zNaYttLcR9cwb1G4="
)

func vpnFixture(t *testing.T) *Service {
	t.Helper()
	root := t.TempDir()
	cfg := config.Config{ConfigDir: filepath.Join(root, "config"), MediaDir: filepath.Join(root, "media"), DownloadsDir: filepath.Join(root, "downloads")}
	db, e := database.Open(context.Background(), cfg.ConfigDir)
	if e != nil {
		t.Fatal(e)
	}
	secrets, e := securestore.New(context.Background(), db, cfg.ConfigDir)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	return New(db, secrets, cfg)
}

// wgStub builds stand-ins for wg-quick and wg that keep tunnel state in a
// file, require the route-suppressing Table setting, and exit 0 for interface
// checks only while the tunnel is up.
func wgStub(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "stubmain.go")
	source := `package main

import (
	"fmt"
	"os"
	"strings"
)

func main() {
	state := os.TempDir() + "/reelscarey-vpn-stub-state"
	if len(os.Args) < 2 {
		os.Exit(2)
	}
	switch os.Args[1] {
	case "up":
		conf, e := os.ReadFile(os.Args[2])
		if e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(1)
		}
		if !strings.Contains(string(conf), "Table = off") {
			fmt.Fprintln(os.Stderr, "missing Table = off")
			os.Exit(1)
		}
		if e := os.WriteFile(state, conf, 0o600); e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(1)
		}
	case "down":
		_ = os.Remove(state)
	case "show":
		if _, e := os.Stat(state); e != nil {
			fmt.Fprintln(os.Stderr, "unable to access interface")
			os.Exit(1)
		}
		if len(os.Args) > 2 {
			fmt.Println("interface: " + os.Args[2])
		}
	default:
		os.Exit(2)
	}
}
`
	if e := os.WriteFile(src, []byte(source), 0o600); e != nil {
		t.Fatal(e)
	}
	build := func(name string) string {
		out := filepath.Join(dir, name)
		if runtime.GOOS == "windows" {
			out += ".exe"
		}
		cmd := exec.Command("go", "build", "-o", out, src)
		if b, e := cmd.CombinedOutput(); e != nil {
			t.Fatalf("stub build: %v %s", e, b)
		}
		return out
	}
	quick, wg := build("reelscarey-wg-quick-stub"), build("reelscarey-wg-stub")
	return quick, wg
}

func awaitVPNState(t *testing.T, s *Service, state string) Status {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		out, e := s.Status(context.Background())
		if e != nil {
			t.Fatal(e)
		}
		if out.State == state {
			return out
		}
		if time.Now().After(deadline) {
			t.Fatalf("tunnel never reached %q: %+v", state, out)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestVPNTunnelLifecycleAndRedaction(t *testing.T) {
	quick, wg := wgStub(t)
	t.Setenv("REELSCAREY_WG_QUICK", quick)
	t.Setenv("REELSCAREY_WG", wg)
	s := vpnFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.Supervise(ctx, slog.New(slog.DiscardHandler))
	}()
	enabled := true
	if _, e := s.Update(ctx, Update{Enabled: &enabled, PrivateKey: "not-base64!!", Address: "10.66.0.2/32", PeerKey: testPeerKey, Endpoint: "45.83.90.9:51820"}); e == nil {
		t.Fatal("an invalid private key was accepted")
	}
	status, e := s.Update(ctx, Update{Enabled: &enabled, PrivateKey: testPrivateKey, Address: "10.66.0.2/32", PeerKey: testPeerKey, Endpoint: "45.83.90.9:51820"})
	if e != nil || !status.Present || !status.Enabled || status.State != "up" {
		t.Fatal(status, e)
	}
	if status.Interface != TunnelInterface || status.Address != "10.66.0.2/32" {
		t.Fatal(status)
	}
	// The generated configuration routes nothing and binds nothing globally.
	raw, e := os.ReadFile(filepath.Join(os.TempDir(), "reelscarey-vpn", TunnelInterface+".conf"))
	if e != nil || !strings.Contains(string(raw), "Table = off") || !strings.Contains(string(raw), testPrivateKey) {
		t.Fatal(string(raw), e)
	}
	// An echo of the configuration through a failing tool must be redacted.
	broken := redact(Config{PrivateKey: testPrivateKey}, "PrivateKey = "+testPrivateKey+" is invalid")
	if strings.Contains(broken, testPrivateKey) {
		t.Fatal("the private key leaked into an error message")
	}
	disabled := false
	saved, e := s.Update(ctx, Update{Enabled: &disabled, Revision: status.Revision})
	if e != nil || saved.Enabled || saved.State != "stopped" {
		t.Fatal(saved, e)
	}
	awaitVPNState(t, s, "stopped")
	cancel()
	<-done
}

func TestVPNUpdateRejectsIncompleteEnable(t *testing.T) {
	quick, wg := wgStub(t)
	t.Setenv("REELSCAREY_WG_QUICK", quick)
	t.Setenv("REELSCAREY_WG", wg)
	s := vpnFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.Supervise(ctx, slog.New(slog.DiscardHandler))
	}()
	enabled := true
	if _, e := s.Update(ctx, Update{Enabled: &enabled, PrivateKey: testPrivateKey}); e == nil {
		t.Fatal("an incomplete configuration was accepted")
	}
	if out, e := s.Status(ctx); e != nil || out.Present {
		t.Fatal("a refused configuration was stored", out, e)
	}
	cancel()
	<-done
}
