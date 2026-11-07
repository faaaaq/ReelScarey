package transfer

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
)

// Opt-in only: supervises the app-owned qBittorrent daemon with its generated
// configuration and verifies the real RPC login, then stops it cleanly. It
// never touches an owner's client or media.
func TestRealBundledClientLifecycle(t *testing.T) {
	if os.Getenv("REELSCAREY_LIVE_BUNDLED") != "1" {
		t.Skip("set REELSCAREY_LIVE_BUNDLED=1 with qbittorrent-nox installed")
	}
	if runtime.GOOS != "linux" {
		t.Skip("the built-in client ships in the Linux container")
	}
	if _, e := exec.LookPath("qbittorrent-nox"); e != nil {
		t.Fatalf("required integration binary qbittorrent-nox: %v", e)
	}
	f := setupWorkflow(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		f.s.SuperviseBundled(ctx, slog.New(slog.DiscardHandler))
	}()
	// The included client is provisioned by the supervision loop itself.
	status := awaitBundledState(t, f, "running")
	if !status.Present || !status.Enabled {
		t.Fatal(status)
	}
	conf, e := os.ReadFile(filepath.Join(f.cfg.ConfigDir, "qbittorrent", "qBittorrent", "config", "qBittorrent.conf"))
	if e != nil || !strings.Contains(string(conf), "WebUI\\Address=127.0.0.1") || !strings.Contains(string(conf), "Session\\Port=6881") {
		t.Fatal("generated configuration", string(conf), e)
	}
	// The saved connection must authenticate against the real daemon.
	if e = f.s.TestDownloadClient(ctx, builtinClientID); e != nil {
		t.Fatal("real RPC login failed", e)
	}
	// The runner may host unrelated qbittorrent-nox processes from earlier
	// steps: compare against the pre-test count instead of scanning bare.
	baseline := countQbtProcesses()
	saved, e := f.s.UpdateBundled(ctx, BundledUpdate{Enabled: boolPtr(false), Revision: status.Revision})
	if e != nil || saved.Enabled || saved.State != "stopped" {
		t.Fatal(saved, e)
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		if countQbtProcesses() <= baseline {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the supervised daemon outlived its disable")
		}
		time.Sleep(300 * time.Millisecond)
	}
	cancel()
	<-done
}

func countQbtProcesses() int {
	entries, e := os.ReadDir("/proc")
	if e != nil {
		return 0
	}
	count := 0
	for _, entry := range entries {
		b, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "comm"))
		if err == nil && strings.TrimSpace(string(b)) == "qbittorrent-nox" {
			count++
		}
	}
	return count
}
