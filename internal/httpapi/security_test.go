package httpapi

import (
	"github.com/faaaaq/ReelScarey/internal/config"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTLSOriginAndUntrustedForwardedHeaders(t *testing.T) {
	server := &Server{config: config.Config{PublicURL: "https://media.example.com"}}
	r := httptest.NewRequest("POST", "http://media.example.com/api/v1/settings", nil)
	r.Header.Set("Origin", "https://media.example.com")
	if !server.sameOrigin(r) {
		t.Fatal("configured HTTPS proxy origin rejected")
	}
	out := httptest.NewRecorder()
	server.cookie(out, r, "test-cookie", time.Now().Add(time.Hour).Unix())
	if !out.Result().Cookies()[0].Secure {
		t.Fatal("HTTPS-origin cookie is not Secure")
	}
	r.Header.Set("Origin", "https://other.example.com")
	if server.sameOrigin(r) {
		t.Fatal("different origin accepted")
	}
	if server.allowedHost("evil.example.com", r.RemoteAddr) {
		t.Fatal("unconfigured Host accepted")
	}
	server.config.PublicURL = ""
	r = httptest.NewRequest("POST", "http://localhost:8787/api/v1/auth/login", nil)
	r.Header.Set("X-Forwarded-Proto", "https")
	r.Header.Set("X-Forwarded-Host", "evil.example.com")
	out = httptest.NewRecorder()
	server.cookie(out, r, "test-cookie", time.Now().Add(time.Hour).Unix())
	if out.Result().Cookies()[0].Secure {
		t.Fatal("untrusted forwarded headers changed cookie transport")
	}
	r.Header.Set("Origin", "http://evil.example.com")
	if server.sameOrigin(r) {
		t.Fatal("untrusted forwarded host bypassed origin check")
	}
}

// NEW-E2: a remote client must not be able to claim a loopback Host to bypass
// the host check. Local clients claiming localhost keep working, and a proxy
// forwarding the real configured origin is unaffected.
func TestAllowedHostRejectsSpoofedLoopbackClaim(t *testing.T) {
	server := &Server{config: config.Config{}}

	// A remote client spoofing "Host: localhost" is rejected with no PublicURL.
	if server.allowedHost("localhost", "203.0.113.7:51000") {
		t.Fatal("remote loopback Host claim accepted")
	}
	if server.allowedHost("localhost:8787", "203.0.113.7:51000") {
		t.Fatal("remote loopback Host claim with port accepted")
	}
	if server.allowedHost("127.0.0.1", "203.0.113.7:51000") {
		t.Fatal("remote loopback IP claim accepted")
	}

	// A local loopback client claiming localhost is still accepted.
	if !server.allowedHost("localhost", "127.0.0.1:51000") {
		t.Fatal("local loopback client rejected")
	}
	if !server.allowedHost("localhost:8787", "[::1]:51000") {
		t.Fatal("local IPv6 loopback client rejected")
	}

	// A proxy forwarding the real configured origin is unaffected, whatever
	// the connection address is.
	proxy := &Server{config: config.Config{PublicURL: "https://media.example.com"}}
	if !proxy.allowedHost("media.example.com", "127.0.0.1:51000") {
		t.Fatal("proxied real origin rejected")
	}
	if !proxy.allowedHost("media.example.com", "203.0.113.7:51000") {
		t.Fatal("proxied real origin from remote address rejected")
	}
}

// NEW-E3: during a reviewed restore the Subsonic surface must refuse
// everything except plain reads, mirroring the compatibility gate.
func TestSubsonicRestoreBlocked(t *testing.T) {
	if subsonicRestoreBlocked(true, "/rest/startScan.view", "GET") {
		t.Fatal("Subsonic reads must stay available during a restore")
	}
	if subsonicRestoreBlocked(true, "/rest/startScan.view", "HEAD") || subsonicRestoreBlocked(true, "/rest/startScan.view", "OPTIONS") {
		t.Fatal("HEAD and OPTIONS are reads")
	}
	for _, method := range []string{"POST", "PUT", "DELETE", "PATCH"} {
		if !subsonicRestoreBlocked(true, "/rest/startScan.view", method) {
			t.Fatalf("Subsonic %s must be refused during a restore", method)
		}
	}
	if subsonicRestoreBlocked(true, "/api/v1/settings", "POST") {
		t.Fatal("the helper must only govern /rest/ paths")
	}
	if subsonicRestoreBlocked(false, "/rest/ping.view", "POST") {
		t.Fatal("without an active restore nothing is blocked")
	}
}

// RS-0029: the insecure-bind warning condition must fire only when the bind
// accepts remote connections while the PublicURL cannot mark cookies Secure.
// Plain-HTTP loopback and LAN deployments with https PublicURL stay silent.
func TestInsecureBindWarningCondition(t *testing.T) {
	if !insecureBindWarning(config.Config{ListenAddr: "0.0.0.0:8787"}) {
		t.Fatal("wildcard bind without https PublicURL must warn")
	}
	if !insecureBindWarning(config.Config{ListenAddr: ":8787", PublicURL: "http://media.example.com"}) {
		t.Fatal("all-interfaces bind with http PublicURL must warn (cookies lack Secure behind a TLS proxy)")
	}
	if !insecureBindWarning(config.Config{ListenAddr: "192.168.1.47:8787"}) {
		t.Fatal("LAN bind without https PublicURL must warn")
	}
	if insecureBindWarning(config.Config{ListenAddr: "127.0.0.1:8787"}) {
		t.Fatal("loopback bind must never warn")
	}
	if insecureBindWarning(config.Config{ListenAddr: "localhost:8787"}) {
		t.Fatal("localhost bind must never warn")
	}
	if insecureBindWarning(config.Config{ListenAddr: "0.0.0.0:8787", PublicURL: "https://media.example.com"}) {
		t.Fatal("https PublicURL marks cookies Secure, so no warning")
	}
	if !listenLoopback("127.0.0.1:8787") || !listenLoopback("[::1]:8787") {
		t.Fatal("loopback bind detection broke")
	}
	if listenLoopback(":8787") || listenLoopback("0.0.0.0:8787") || listenLoopback("192.168.1.47:8787") {
		t.Fatal("non-loopback binds must not report loopback")
	}
}
