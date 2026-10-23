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
	if server.allowedHost("evil.example.com") {
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
