package httpapi

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/metrics"
)

func TestMiddlewareRecordsBoundedHTTPRequestMetric(t *testing.T) {
	registry := metrics.NewRegistry()
	instruments, err := metrics.NewInstruments(registry)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{
		logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		metricsRegistry: registry,
		instruments:     instruments,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/fixture/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	})
	handler := s.middleware(mux)
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/fixture/7", nil))

	rendered := registry.Render()
	want := `reelscarey_http_requests_total{method="GET",route="/api/v1/fixture/{id}",status="202"} 1`
	if !strings.Contains(rendered, want) {
		t.Fatalf("missing bounded request sample %q in:\n%s", want, rendered)
	}
	if strings.Contains(rendered, "/fixture/7") {
		t.Fatalf("raw path leaked into a metric label:\n%s", rendered)
	}
}

func TestRoutePatternStripsTheMethodPrefix(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/example", nil)
	r.Pattern = "GET /api/v1/example"
	if got := routePattern(r); got != "/api/v1/example" {
		t.Fatalf("routePattern = %q", got)
	}
	r.Pattern = ""
	if got := routePattern(r); got != "unknown" {
		t.Fatalf("unmatched routePattern = %q", got)
	}
}
