package transfer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func startTransmissionFixture(t *testing.T, ctx context.Context, f *workflowFixture) (Client, ClientConfig, string) {
	t.Helper()
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	password := ID()
	profile := t.TempDir()
	client := ClientConfig{URL: fmt.Sprintf("http://127.0.0.1:%d/transmission/rpc", port), Username: "fixture", Password: password, LocalFolder: "transmission", DownloadPath: filepath.ToSlash(filepath.Join(f.cfg.DownloadsDir, "transmission"))}
	if e = os.MkdirAll(client.DownloadPath, 0700); e != nil {
		t.Fatal(e)
	}
	config := map[string]any{"rpc-enabled": true, "rpc-bind-address": "127.0.0.1", "rpc-port": port, "rpc-authentication-required": true, "rpc-username": client.Username, "rpc-password": password, "rpc-whitelist-enabled": true, "rpc-whitelist": "127.0.0.1", "dht-enabled": false, "pex-enabled": false, "lpd-enabled": false, "port-forwarding-enabled": false, "utp-enabled": false, "peer-port": 0, "download-dir": client.DownloadPath, "incomplete-dir-enabled": false, "script-torrent-added-enabled": false, "script-torrent-done-enabled": false, "script-torrent-done-seeding-enabled": false}
	raw, _ := json.Marshal(config)
	if e = os.WriteFile(filepath.Join(profile, "settings.json"), raw, 0600); e != nil {
		t.Fatal(e)
	}
	cmd := exec.CommandContext(ctx, "transmission-daemon", "--foreground", "--config-dir", profile)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() {
		_ = cmd.Process.Signal(os.Interrupt)
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
	})
	p := NewTransmission()
	for start := time.Now(); time.Since(start) < 20*time.Second; {
		if _, e = p.Test(ctx, client); e == nil {
			return p, client, password
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(200 * time.Millisecond):
		}
	}
	t.Fatal("isolated Transmission did not become ready", e)
	return nil, client, password
}
func TestTransmissionChallengeScopeCompletionAndForeignTorrent(t *testing.T) {
	hash := strings.Repeat("a", 40)
	calls, adds := 0, 0
	mode := "complete"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		u, p, ok := r.BasicAuth()
		if !ok || u != "fixture" || p != "private" {
			t.Error("missing auth")
		}
		if r.Header.Get("X-Transmission-Session-Id") != "challenge" {
			w.Header().Set("X-Transmission-Session-Id", "challenge")
			w.WriteHeader(409)
			return
		}
		var in struct {
			Method    string                     `json:"method"`
			Arguments map[string]json.RawMessage `json:"arguments"`
		}
		if json.NewDecoder(r.Body).Decode(&in) != nil {
			t.Error("request")
		}
		var args any
		switch in.Method {
		case "session-get":
			args = map[string]any{"version": "4.1.0", "rpc-version": 18, "rpc-version-minimum": 14, "script-torrent-done-enabled": mode == "script"}
		case "torrent-get":
			if string(in.Arguments["ids"]) != `["`+hash+`"]` {
				t.Error("unbounded query")
			}
			status := 6
			unchecked := 0
			if mode == "checking" {
				status = 2
				unchecked = 100
			}
			h := hash
			if mode == "foreign" {
				h = strings.Repeat("b", 40)
			}
			args = map[string]any{"torrents": []any{map[string]any{"hashString": h, "name": "Owned", "downloadDir": "/remote", "labels": []string{"reelscarey-owned"}, "status": status, "error": 0, "sizeWhenDone": 100, "leftUntilDone": 0, "haveValid": 100, "haveUnchecked": unchecked, "percentDone": 1, "metadataPercentComplete": 1, "files": []any{map[string]any{"name": "Owned.mkv", "length": 100, "bytesCompleted": 100}}, "fileStats": []any{map[string]any{"wanted": true}}}}}
		case "torrent-add":
			adds++
			args = map[string]any{"torrent-duplicate": map[string]any{"hashString": hash}}
		default:
			t.Error("unexpected mutation", in.Method)
		}
		json.NewEncoder(w).Encode(map[string]any{"result": "success", "tag": 1, "arguments": args})
	}))
	defer server.Close()
	p := NewTransmission()
	c := ClientConfig{URL: server.URL, Username: "fixture", Password: "private"}
	ctx := context.Background()
	v, e := p.Find(ctx, c, hash)
	if e != nil || v.State != "uploading" || v.Tags != "reelscarey-owned" {
		t.Fatal(v, e)
	}
	files, e := p.Files(ctx, c, hash)
	if e != nil || len(files) != 1 || files[0].Priority != 1 || files[0].Progress != 1 {
		t.Fatal(files, e)
	}
	mode = "checking"
	v, e = p.Find(ctx, c, hash)
	if e != nil || v.State == "uploading" {
		t.Fatal("unchecked bytes importable", e)
	}
	mode = "foreign"
	if _, e = p.Find(ctx, c, hash); e == nil {
		t.Fatal("foreign hash accepted")
	}
	mode = "script"
	if _, e = p.Test(ctx, c); e == nil {
		t.Fatal("scripts permitted")
	}
	mode = "complete"
	if e = p.Add(ctx, c, "reelscarey-owned", "/remote", Payload{Magnet: "magnet:?xt=urn:btih:" + hash}); e == nil {
		t.Fatal("duplicate accepted")
	}
	if adds != 1 {
		t.Fatal("submission repeated", adds)
	}
	if calls < 2 {
		t.Fatal("challenge not used")
	}
}
