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

// commandStubs builds stand-ins for wg and ip. The wg stub records the applied
// configuration; the ip stub records addresses and keeps interface state, so
// tests assert the real command sequence without touching the host network.
func commandStubs(t *testing.T) (wg, ip, stateDir string) {
	t.Helper()
	dir := t.TempDir()
	stateDir = filepath.Join(dir, "state")
	if e := os.MkdirAll(stateDir, 0o700); e != nil {
		t.Fatal(e)
	}
	src := filepath.Join(dir, "stubmain.go")
	source := `package main

import (
	"os"
	"path/filepath"
	"strings"
)

func main() {
	state := os.Getenv("REELSCAREY_VPN_STUB_STATE")
	name := strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")
	args := os.Args[1:]
	if name == "wg" {
		if len(args) == 0 {
			os.Exit(2)
		}
		switch args[0] {
		case "setconf":
			conf, e := os.ReadFile(args[2])
			if e != nil {
				os.Exit(1)
			}
			text := string(conf)
			if !strings.Contains(text, "[Interface]") || !strings.Contains(text, "PrivateKey = ") || !strings.Contains(text, "[Peer]") || !strings.Contains(text, "AllowedIPs = ") {
				os.Exit(1)
			}
			if e := os.WriteFile(filepath.Join(state, "applied.conf"), conf, 0o600); e != nil {
				os.Exit(1)
			}
		case "show":
			if _, e := os.Stat(filepath.Join(state, "interface")); e != nil {
				os.Exit(1)
			}
		default:
			os.Exit(2)
		}
		return
	}
	if len(args) == 0 {
		os.Exit(2)
	}
	if args[0] == "-6" {
		args = args[1:]
		if len(args) == 0 {
			os.Exit(2)
		}
	}
	switch args[0] {
	case "link":
		if len(args) < 2 {
			os.Exit(2)
		}
		if args[1] == "add" {
			_ = os.WriteFile(filepath.Join(state, "interface"), []byte("up"), 0o600)
			return
		}
		if args[1] == "set" {
			return
		}
		if args[1] == "del" {
			_ = os.Remove(filepath.Join(state, "interface"))
			_ = os.Remove(filepath.Join(state, "applied.conf"))
			_ = os.Remove(filepath.Join(state, "addresses"))
			return
		}
	case "addr":
		if len(args) < 4 || args[1] != "add" {
			os.Exit(2)
		}
		f, e := os.OpenFile(filepath.Join(state, "addresses"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if e != nil {
			os.Exit(1)
		}
		_, _ = f.WriteString(args[2] + "\n")
		_ = f.Close()
		return
	case "route", "rule":
		return
	}
	os.Exit(2)
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
	return build("wg"), build("ip"), stateDir
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
	wg, ip, stateDir := commandStubs(t)
	t.Setenv("REELSCAREY_WG", wg)
	t.Setenv("REELSCAREY_IP", ip)
	t.Setenv("REELSCAREY_VPN_STUB_STATE", stateDir)
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
	status, e := s.Update(ctx, Update{Enabled: &enabled, PrivateKey: testPrivateKey, Address: "10.66.0.2/32,fc00::2/128", PeerKey: testPeerKey, Endpoint: "45.83.90.9:51820", AllowedIPs: "0.0.0.0/0,::/0"})
	if e != nil || !status.Present || !status.Enabled || status.State != "up" {
		t.Fatal(status, e)
	}
	applied, e := os.ReadFile(filepath.Join(stateDir, "applied.conf"))
	if e != nil || !strings.Contains(string(applied), testPrivateKey) || !strings.Contains(string(applied), "Endpoint = 45.83.90.9:51820") {
		t.Fatal("the generated WireGuard configuration is incomplete", string(applied), e)
	}
	addrs, e := os.ReadFile(filepath.Join(stateDir, "addresses"))
	if e != nil || !strings.Contains(string(addrs), "10.66.0.2/32") || !strings.Contains(string(addrs), "fc00::2/128") {
		t.Fatal("both tunnel addresses were not applied", string(addrs), e)
	}
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
	if _, e := os.Stat(filepath.Join(stateDir, "interface")); !os.IsNotExist(e) {
		t.Fatal("the interface survived disable")
	}
	cancel()
	<-done
}

func TestTorrentStateHonorsKillSwitch(t *testing.T) {
	s := vpnFixture(t)
	ctx := context.Background()
	if allowed, bind := s.TorrentState(); !allowed || bind {
		t.Fatal("no VPN must allow unbound torrents", allowed, bind)
	}
	base := Config{Revision: 1, Enabled: true, PrivateKey: testPrivateKey, Address: "10.66.0.2/32", PeerKey: testPeerKey, Endpoint: "1.2.3.4:51820", AllowedIPs: "0.0.0.0/0"}
	if e := s.save(ctx, base); e != nil {
		t.Fatal(e)
	}
	s.rt.mu.Lock()
	s.rt.up = false
	s.rt.mu.Unlock()
	if allowed, bind := s.TorrentState(); allowed || bind {
		t.Fatal("kill switch on with the tunnel down must stop torrents", allowed, bind)
	}
	s.rt.mu.Lock()
	s.rt.up = true
	s.rt.mu.Unlock()
	if allowed, bind := s.TorrentState(); !allowed || !bind {
		t.Fatal("kill switch on with the tunnel up must allow and bind", allowed, bind)
	}
	off := false
	base.KillSwitch = &off
	s.rt.mu.Lock()
	s.rt.up = false
	s.rt.mu.Unlock()
	if e := s.save(ctx, base); e != nil {
		t.Fatal(e)
	}
	if allowed, bind := s.TorrentState(); !allowed || bind {
		t.Fatal("kill switch off with the tunnel down must allow unbound torrents", allowed, bind)
	}
	status, e := s.Status(ctx)
	if e != nil || status.KillSwitch {
		t.Fatal("status must report the disabled kill switch", status, e)
	}
	base.Enabled = false
	s.rt.mu.Lock()
	s.rt.up = true
	s.rt.mu.Unlock()
	if e := s.save(ctx, base); e != nil {
		t.Fatal(e)
	}
	if allowed, bind := s.TorrentState(); !allowed || bind {
		t.Fatal("a disabled VPN must allow unbound torrents", allowed, bind)
	}
}

func TestVPNUpdateRejectsIncompleteEnable(t *testing.T) {
	wg, ip, stateDir := commandStubs(t)
	t.Setenv("REELSCAREY_WG", wg)
	t.Setenv("REELSCAREY_IP", ip)
	t.Setenv("REELSCAREY_VPN_STUB_STATE", stateDir)
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
