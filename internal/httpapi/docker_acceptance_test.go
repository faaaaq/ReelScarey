package httpapi

// C3-0001 acceptance matrix for the NEW-E2 host guard in container and
// published-port deployments. The container binds 0.0.0.0:8787 (Dockerfile
// ENV) while the host publishes 127.0.0.1:8787->8787, so a host-side client
// arrives from the Docker bridge gateway (a non-loopback address) while
// claiming the loopback Host name it was told to use. The guard must accept
// that traffic when the configured bind is known to be non-loopback, keep
// rejecting spoofed loopback claims when the bind is loopback-only or
// unknown, and never weaken authentication, CSRF or role enforcement.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/faaaaq/ReelScarey/internal/auth"
	"github.com/faaaaq/ReelScarey/internal/config"
	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/metadata"
	"github.com/faaaaq/ReelScarey/internal/securestore"
)

// dockerFixture builds the full application handler with a deployment-specific
// listen address and public URL, mirroring rawFixture otherwise.
func dockerFixture(t *testing.T, cfg config.Config) (http.Handler, *auth.Service, string) {
	t.Helper()
	dir := t.TempDir()
	db, err := database.Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	assets := fstest.MapFS{"index.html": {Data: []byte("<!doctype html><title>ReelScarey</title>")}}
	authService, err := auth.New(context.Background(), db, dir)
	if err != nil {
		t.Fatal(err)
	}
	store, err := securestore.New(context.Background(), db, dir)
	if err != nil {
		t.Fatal(err)
	}
	code, err := securestore.SecretFile(filepath.Join(dir, "setup-token"), 32, false)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ConfigDir == "" {
		cfg.ConfigDir = dir
	}
	if cfg.MediaDir == "" {
		cfg.MediaDir = "/media"
	}
	if cfg.DownloadsDir == "" {
		cfg.DownloadsDir = "/downloads"
	}
	handler := New(db, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), assets, "test", Dependencies{Auth: authService, Metadata: metadata.New(store, metadata.NewTMDb()), Secrets: store})
	return handler, authService, base64.RawURLEncoding.EncodeToString(code)
}

// dockerRequest drives one request through the full security middleware the
// way a host-side client behind a published port would appear inside the
// container: an explicit Host header and a bridge-gateway remote address.
func dockerRequest(handler http.Handler, method, target, host, remoteAddr string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, nil)
	r.Host = host
	r.RemoteAddr = remoteAddr
	out := httptest.NewRecorder()
	handler.ServeHTTP(out, r)
	return out
}

func errorCode(t *testing.T, body string) string {
	t.Helper()
	var parsed struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatalf("response body is not the API error envelope: %q", body)
	}
	return parsed.Error.Code
}

// (1) Default supported Docker deployment: compose publishes the container
// port on host loopback with no REELSCAREY_PUBLIC_URL, and the README says to
// open http://localhost:8787 from the host.
func TestDockerDefaultComposeHostSideAccess(t *testing.T) {
	handler, _, _ := dockerFixture(t, config.Config{ListenAddr: "0.0.0.0:8787"})
	for _, host := range []string{"127.0.0.1:8787", "localhost:8787", "[::1]:8787"} {
		out := dockerRequest(handler, "GET", "http://"+host+"/readyz", host, "172.17.0.1:51000")
		if out.Code != 200 {
			t.Fatalf("default compose host-side access via %s rejected: %d %s", host, out.Code, out.Body)
		}
	}
}

// (2) Host-side access through published ports: every Docker network shape
// presents the host client as a non-loopback gateway address.
func TestDockerPublishedPortGateways(t *testing.T) {
	handler, _, _ := dockerFixture(t, config.Config{ListenAddr: "0.0.0.0:8787"})
	for _, remote := range []string{
		"172.17.0.1:51000",   // default bridge gateway
		"172.18.0.1:51000",   // user-defined bridge gateway
		"192.168.65.1:51000", // Docker Desktop host gateway
		"127.0.0.1:51000",    // container-local client
	} {
		out := dockerRequest(handler, "GET", "http://127.0.0.1:8787/readyz", "127.0.0.1:8787", remote)
		if out.Code != 200 {
			t.Fatalf("published-port access from %s rejected: %d %s", remote, out.Code, out.Body)
		}
	}
}

// (3) Container-local health checking: the HEALTHCHECK CLI and any
// container-network client keep working.
func TestContainerLocalHealthcheck(t *testing.T) {
	handler, _, _ := dockerFixture(t, config.Config{ListenAddr: "0.0.0.0:8787"})
	out := dockerRequest(handler, "GET", "http://127.0.0.1:8787/readyz", "127.0.0.1:8787", "127.0.0.1:41000")
	if out.Code != 200 {
		t.Fatalf("container-local healthcheck rejected: %d %s", out.Code, out.Body)
	}
	out = dockerRequest(handler, "GET", "http://localhost:8787/readyz", "localhost:8787", "172.17.0.2:41001")
	if out.Code != 200 {
		t.Fatalf("container-network client rejected: %d %s", out.Code, out.Body)
	}
}

// (4) PublicURL configured: the exact configured origin is accepted whatever
// the connection address, and host-side localhost browsing keeps working
// alongside it (the compose-smoke.sh path).
func TestPublicURLConfiguredAcceptsConfiguredOriginAndLoopback(t *testing.T) {
	handler, _, _ := dockerFixture(t, config.Config{ListenAddr: "0.0.0.0:8787", PublicURL: "http://192.168.1.20:8787"})
	for _, tc := range []struct{ host, remote string }{
		{"192.168.1.20:8787", "172.17.0.1:51000"},
		{"192.168.1.20:8787", "127.0.0.1:51000"},
		{"127.0.0.1:8787", "172.17.0.1:51000"},
		{"localhost:8787", "172.17.0.1:51000"},
	} {
		out := dockerRequest(handler, "GET", "http://"+tc.host+"/readyz", tc.host, tc.remote)
		if out.Code != 200 {
			t.Fatalf("PublicURL deployment rejected Host %s from %s: %d %s", tc.host, tc.remote, out.Code, out.Body)
		}
	}
	// compose-smoke.sh: PublicURL=http://127.0.0.1:18787 and a host-side curl
	// to http://127.0.0.1:18787/readyz.
	smoke, _, _ := dockerFixture(t, config.Config{ListenAddr: "0.0.0.0:8787", PublicURL: "http://127.0.0.1:18787"})
	out := dockerRequest(smoke, "GET", "http://127.0.0.1:18787/readyz", "127.0.0.1:18787", "172.17.0.1:51000")
	if out.Code != 200 {
		t.Fatalf("compose-smoke PublicURL path rejected: %d %s", out.Code, out.Body)
	}
}

// (5) PublicURL unset where supported: the bare-metal default loopback bind.
func TestPublicURLUnsetLoopbackBind(t *testing.T) {
	handler, _, _ := dockerFixture(t, config.Config{ListenAddr: "127.0.0.1:8787"})
	out := dockerRequest(handler, "GET", "http://localhost:8787/readyz", "localhost:8787", "127.0.0.1:51000")
	if out.Code != 200 {
		t.Fatalf("bare-metal loopback default rejected: %d %s", out.Code, out.Body)
	}
}

// (6) Reverse-proxy access: a TLS-terminating proxy forwarding the configured
// origin is accepted from any connection address, and a local proxy
// forwarding the loopback Host keeps working.
func TestReverseProxyAccess(t *testing.T) {
	handler, _, _ := dockerFixture(t, config.Config{ListenAddr: "0.0.0.0:8787", PublicURL: "https://media.example.com"})
	for _, remote := range []string{"127.0.0.1:51000", "203.0.113.7:51000", "172.17.0.1:51000"} {
		out := dockerRequest(handler, "GET", "https://media.example.com/readyz", "media.example.com", remote)
		if out.Code != 200 {
			t.Fatalf("reverse-proxy origin rejected from %s: %d %s", remote, out.Code, out.Body)
		}
	}
	local, _, _ := dockerFixture(t, config.Config{ListenAddr: "127.0.0.1:8787"})
	out := dockerRequest(local, "GET", "http://127.0.0.1:8787/readyz", "127.0.0.1:8787", "127.0.0.1:51000")
	if out.Code != 200 {
		t.Fatalf("local proxy forwarding loopback Host rejected: %d %s", out.Code, out.Body)
	}
}

// (7) Legitimate loopback access over IPv4 and IPv6 on a loopback-only bind.
func TestLegitimateLoopbackAccessIPv4IPv6(t *testing.T) {
	handler, _, _ := dockerFixture(t, config.Config{ListenAddr: "127.0.0.1:8787"})
	for _, tc := range []struct{ host, remote string }{
		{"localhost:8787", "127.0.0.1:51000"},
		{"127.0.0.1:8787", "127.0.0.1:51000"},
		{"localhost:8787", "[::1]:51000"},
		{"[::1]:8787", "[::1]:51000"},
		{"127.0.0.1:8787", "[::1]:51000"},
	} {
		out := dockerRequest(handler, "GET", "http://"+tc.host+"/readyz", tc.host, tc.remote)
		if out.Code != 200 {
			t.Fatalf("legitimate loopback access Host %s from %s rejected: %d %s", tc.host, tc.remote, out.Code, out.Body)
		}
	}
}

// (8) Non-loopback clients spoofing loopback Host headers: when the bind is
// loopback-only (or unknown) the NEW-E2 guard still fails closed, and the
// rejection is the host guard, not an unrelated failure.
func TestSpoofedLoopbackClaimStillRejected(t *testing.T) {
	for _, listen := range []string{"127.0.0.1:8787", "localhost:8787", ""} {
		handler, _, _ := dockerFixture(t, config.Config{ListenAddr: listen})
		for _, host := range []string{"localhost", "localhost:8787", "127.0.0.1", "[::1]"} {
			out := dockerRequest(handler, "GET", "http://"+host+"/readyz", host, "203.0.113.7:51000")
			if out.Code != 403 || errorCode(t, out.Body.String()) != "invalid_host" {
				t.Fatalf("spoofed loopback Host %q (bind %q) not rejected by the host guard: %d %s", host, listen, out.Code, out.Body)
			}
		}
	}
}

// (9) Host-header manipulation: arbitrary hostnames stay rejected with and
// without a PublicURL, including near-miss and port-mismatch variants.
func TestHostHeaderManipulationRejected(t *testing.T) {
	handler, _, _ := dockerFixture(t, config.Config{ListenAddr: "0.0.0.0:8787"})
	for _, host := range []string{"evil.example.com", "127.0.0.1.evil.com", "localhost.evil.com", "media.example.com:8443"} {
		out := dockerRequest(handler, "GET", "http://"+host+"/readyz", host, "172.17.0.1:51000")
		if out.Code != 403 || errorCode(t, out.Body.String()) != "invalid_host" {
			t.Fatalf("manipulated Host %q not rejected by the host guard: %d %s", host, out.Code, out.Body)
		}
	}
	proxy, _, _ := dockerFixture(t, config.Config{ListenAddr: "0.0.0.0:8787", PublicURL: "https://media.example.com"})
	for _, host := range []string{"evil.example.com", "media.example.com:8443", "http://media.example.com"} {
		out := dockerRequest(proxy, "GET", "https://media.example.com/readyz", host, "203.0.113.7:51000")
		if out.Code != 403 || errorCode(t, out.Body.String()) != "invalid_host" {
			t.Fatalf("manipulated Host %q with PublicURL not rejected by the host guard: %d %s", host, out.Code, out.Body)
		}
	}
	// Host matching is case-insensitive on the configured origin.
	out := dockerRequest(proxy, "GET", "https://media.example.com/readyz", "MEDIA.EXAMPLE.COM", "203.0.113.7:51000")
	if out.Code != 200 {
		t.Fatalf("case-variant of configured origin rejected: %d %s", out.Code, out.Body)
	}
}

// (10) Missing or malformed Host headers fail closed.
func TestMissingOrMalformedHostRejected(t *testing.T) {
	handler, _, _ := dockerFixture(t, config.Config{ListenAddr: "0.0.0.0:8787"})
	for _, host := range []string{"", "not a host", "exa\tple", "exa mple"} {
		out := dockerRequest(handler, "GET", "/readyz", host, "172.17.0.1:51000")
		if out.Code != 403 || errorCode(t, out.Body.String()) != "invalid_host" {
			t.Fatalf("malformed Host %q not rejected by the host guard: %d %s", host, out.Code, out.Body)
		}
	}
}

// (11) IPv4/IPv6 loopback claims from a non-loopback bind are accepted (the
// container cannot attribute them), while a loopback bind keeps the strict
// remote check for both families.
func TestLoopbackClaimAddressFamilyMatrix(t *testing.T) {
	container, _, _ := dockerFixture(t, config.Config{ListenAddr: "0.0.0.0:8787"})
	for _, tc := range []struct{ host, remote string }{
		{"[::1]:8787", "172.17.0.1:51000"},
		{"127.0.0.1:8787", "172.17.0.1:51000"},
		{"localhost:8787", "172.17.0.1:51000"},
	} {
		out := dockerRequest(container, "GET", "http://"+tc.host+"/readyz", tc.host, tc.remote)
		if out.Code != 200 {
			t.Fatalf("container loopback claim Host %s from %s rejected: %d %s", tc.host, tc.remote, out.Code, out.Body)
		}
	}
	local, _, _ := dockerFixture(t, config.Config{ListenAddr: "127.0.0.1:8787"})
	out := dockerRequest(local, "GET", "http://[::1]:8787/readyz", "[::1]:8787", "203.0.113.7:51000")
	if out.Code != 403 || errorCode(t, out.Body.String()) != "invalid_host" {
		t.Fatalf("IPv6 loopback spoof against loopback bind not rejected: %d %s", out.Code, out.Body)
	}
}

// (12)-(15) The relaxation never weakens the authenticated boundary: host
// acceptance, setup state, session authentication, CSRF, same-origin and
// requester confinement behave exactly as before behind the accepted Host.
func TestDockerHostAcceptanceKeepsAuthenticationBoundary(t *testing.T) {
	handler, service, code := dockerFixture(t, config.Config{ListenAddr: "0.0.0.0:8787"})
	gateway := "172.17.0.1:51000"

	// Setup-state behavior before configuration: session status is public and
	// reports setup_required; the setup endpoint itself is reachable.
	out := dockerRequest(handler, "GET", "http://127.0.0.1:8787/api/v1/auth/session", "127.0.0.1:8787", gateway)
	if out.Code != 200 {
		t.Fatalf("session status rejected behind accepted Host: %d %s", out.Code, out.Body)
	}
	if !strings.Contains(out.Body.String(), `"setup_required":true`) {
		t.Fatalf("expected setup_required before configuration: %s", out.Body)
	}

	// Unauthorized requests are rejected with the authentication error, not
	// the host guard.
	out = dockerRequest(handler, "GET", "http://127.0.0.1:8787/api/v1/system", "127.0.0.1:8787", gateway)
	if out.Code != 401 || errorCode(t, out.Body.String()) != "authentication_required" {
		t.Fatalf("unauthenticated API behind accepted Host: %d %s", out.Code, out.Body)
	}

	// A wrong setup token is refused for the right reason.
	body := `{"username":"testadmin","password":"test-only-password","setup_token":"wrong-token"}`
	r := httptest.NewRequest("POST", "http://127.0.0.1:8787/api/v1/auth/setup", strings.NewReader(body))
	r.Host = "127.0.0.1:8787"
	r.RemoteAddr = gateway
	r.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, r)
	if rec.Code != 403 || errorCode(t, rec.Body.String()) != "invalid_setup_token" {
		t.Fatalf("wrong setup token behind accepted Host: %d %s", rec.Code, rec.Body)
	}

	// Normal setup through the accepted Host mints a working session.
	r = httptest.NewRequest("POST", "http://127.0.0.1:8787/api/v1/auth/setup", strings.NewReader(`{"username":"testadmin","password":"test-only-password","setup_token":"`+code+`"}`))
	r.Host = "127.0.0.1:8787"
	r.RemoteAddr = gateway
	r.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, r)
	if rec.Code != 201 {
		t.Fatalf("setup behind accepted Host failed: %d %s", rec.Code, rec.Body)
	}
	// The session token travels only in the HttpOnly cookie, never in the
	// response body.
	var session auth.Session
	for _, c := range rec.Result().Cookies() {
		if c.Name == cookieName {
			session.Token = c.Value
		}
	}
	if session.Token == "" {
		t.Fatalf("setup did not mint a session cookie: %d %s", rec.Code, rec.Body)
	}
	session.CSRFToken = func() string {
		var parsed struct {
			CSRFToken string `json:"csrf_token"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &parsed)
		return parsed.CSRFToken
	}()

	// Normal authorized request with the session cookie.
	r = httptest.NewRequest("GET", "http://127.0.0.1:8787/api/v1/auth/session", nil)
	r.Host = "127.0.0.1:8787"
	r.RemoteAddr = gateway
	r.AddCookie(&http.Cookie{Name: cookieName, Value: session.Token})
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, r)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"authenticated":true`) {
		t.Fatalf("authenticated session behind accepted Host: %d %s", rec.Code, rec.Body)
	}

	// Mutations still require CSRF.
	r = httptest.NewRequest("PUT", "http://127.0.0.1:8787/api/v1/settings", strings.NewReader(validSettings))
	r.Host = "127.0.0.1:8787"
	r.RemoteAddr = gateway
	r.AddCookie(&http.Cookie{Name: cookieName, Value: session.Token})
	r.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, r)
	if rec.Code != 403 || errorCode(t, rec.Body.String()) != "csrf_required" {
		t.Fatalf("mutation without CSRF behind accepted Host: %d %s", rec.Code, rec.Body)
	}

	// Cross-origin mutations are refused.
	r = httptest.NewRequest("PUT", "http://127.0.0.1:8787/api/v1/settings", strings.NewReader(validSettings))
	r.Host = "127.0.0.1:8787"
	r.RemoteAddr = gateway
	r.AddCookie(&http.Cookie{Name: cookieName, Value: session.Token})
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", session.CSRFToken)
	r.Header.Set("Origin", "http://evil.example.com")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, r)
	if rec.Code != 403 || errorCode(t, rec.Body.String()) != "cross_origin" {
		t.Fatalf("cross-origin mutation behind accepted Host: %d %s", rec.Code, rec.Body)
	}

	// Same-origin mutation succeeds.
	r = httptest.NewRequest("PUT", "http://127.0.0.1:8787/api/v1/settings", strings.NewReader(validSettings))
	r.Host = "127.0.0.1:8787"
	r.RemoteAddr = gateway
	r.AddCookie(&http.Cookie{Name: cookieName, Value: session.Token})
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", session.CSRFToken)
	r.Header.Set("Origin", "http://127.0.0.1:8787")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, r)
	if rec.Code != 200 {
		t.Fatalf("same-origin mutation behind accepted Host: %d %s", rec.Code, rec.Body)
	}

	// Requester confinement is unchanged: a request account is denied
	// administrator APIs even from an accepted Host.
	ctx := context.Background()
	if _, err := service.CreateRequestAccount(ctx, "viewer", "viewer-test-password"); err != nil {
		t.Fatal(err)
	}
	requester, err := service.Login(ctx, "viewer", "viewer-test-password")
	if err != nil {
		t.Fatal(err)
	}
	r = httptest.NewRequest("GET", "http://127.0.0.1:8787/api/v1/settings", nil)
	r.Host = "127.0.0.1:8787"
	r.RemoteAddr = gateway
	r.AddCookie(&http.Cookie{Name: cookieName, Value: requester.Token})
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, r)
	if rec.Code != 403 || errorCode(t, rec.Body.String()) != "administrator_required" {
		t.Fatalf("requester reached administrator API behind accepted Host: %d %s", rec.Code, rec.Body)
	}
}

// The relaxed path must not accept an arbitrary hostname even from a
// container-network address: only loopback names and the configured PublicURL
// are ever accepted.
func TestDockerRelaxationStaysBounded(t *testing.T) {
	handler, _, _ := dockerFixture(t, config.Config{ListenAddr: "0.0.0.0:8787"})
	out := dockerRequest(handler, "GET", "http://evil.example.com/readyz", "evil.example.com", "172.17.0.1:51000")
	if out.Code != 403 || errorCode(t, out.Body.String()) != "invalid_host" {
		t.Fatalf("arbitrary Host accepted in container deployment: %d %s", out.Code, out.Body)
	}
}
