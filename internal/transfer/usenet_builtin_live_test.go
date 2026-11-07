package transfer

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// Opt-in only: supervises the packaged SABnzbd binary with the generated
// configuration and verifies its real loopback API and dedicated category.
// It never contacts a news provider and never uses owner media or accounts.
func TestRealBundledUsenetLifecycle(t *testing.T) {
	if os.Getenv("REELSCAREY_LIVE_BUNDLED_SAB") != "1" {
		t.Skip("set REELSCAREY_LIVE_BUNDLED_SAB=1 with sabnzbdplus installed")
	}
	if runtime.GOOS != "linux" {
		t.Skip("the bundled Usenet client ships in the Linux container")
	}
	if _, e := exec.LookPath("sabnzbdplus"); e != nil {
		t.Fatalf("required integration binary sabnzbdplus: %v", e)
	}
	f := setupWorkflow(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		f.s.SuperviseBundledUsenet(ctx, slog.New(slog.DiscardHandler))
	}()
	if _, e := f.s.SaveNewsServer(ctx, NewsServerUpdate{Host: "news.invalid", Port: 119, Connections: 2}); e != nil {
		t.Fatal(e)
	}
	before, e := f.s.BundledUsenetStatus(ctx)
	if e != nil {
		t.Fatal(e)
	}
	status, e := f.s.UpdateBundledUsenet(ctx, UsenetBundledUpdate{Revision: before.Revision, Enabled: boolPtr(true)})
	if e != nil {
		t.Fatal("real SABnzbd did not start", e)
	}
	if status.State != "running" || !status.Configured {
		t.Fatal(status)
	}
	if e = f.s.TestDownloadClient(ctx, builtinUsenetClientID); e != nil {
		t.Fatal("real SABnzbd API/category test failed", e)
	}
	ini := filepath.Join(f.cfg.ConfigDir, "sabnzbd", "sabnzbd.ini")
	if _, e = os.Stat(ini); e != nil {
		t.Fatal("generated SABnzbd configuration is missing", e)
	}
	deadline := time.Now().Add(30 * time.Second)
	saved, e := f.s.UpdateBundledUsenet(ctx, UsenetBundledUpdate{Revision: status.Revision, Enabled: boolPtr(false)})
	if e != nil || saved.Enabled || saved.State != "stopped" {
		t.Fatal(saved, e)
	}
	for {
		if check, e := f.s.BundledUsenetStatus(ctx); e == nil && check.State == "stopped" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the supervised SABnzbd daemon outlived its disable")
		}
		time.Sleep(300 * time.Millisecond)
	}
	cancel()
	<-done
}
