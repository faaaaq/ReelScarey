package metrics

import (
	"strings"
	"testing"
)

func TestInstruments(t *testing.T) {
	r := NewRegistry()
	instruments, err := NewInstruments(r)
	if err != nil {
		t.Fatal(err)
	}
	if !instruments.ObserveHTTPRequest(" get ", " /api/v1/movies ", 200) {
		t.Fatal("request was rejected")
	}
	if !instruments.ObserveHTTPRequest("BREW", "/x", 999) {
		t.Fatal("request was rejected")
	}
	if !instruments.ObserveHTTPRequest("", "", 0) {
		t.Fatal("request was rejected")
	}
	if !instruments.SetJobState("import", "running", 2) {
		t.Fatal("job state was rejected")
	}
	if !instruments.SetQueueDepth("imports", 3) || !instruments.AddQueueDepth("imports", -1) {
		t.Fatal("queue depth was rejected")
	}
	if instruments.QueueDepth.Value(Labels{"queue": "imports"}) != 2 {
		t.Fatal("queue depth value is wrong")
	}

	out := r.Render()
	for _, want := range []string{
		`reelscarey_http_requests_total{method="GET",route="/api/v1/movies",status="200"} 1`,
		`reelscarey_http_requests_total{method="other",route="/x",status="unknown"} 1`,
		`reelscarey_http_requests_total{method="other",route="unknown",status="unknown"} 1`,
		`reelscarey_jobs{kind="import",state="running"} 2`,
		`reelscarey_queue_depth{queue="imports"} 2`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("render missing %q:\n%s", want, out)
		}
	}

	again, err := NewInstruments(r)
	if err != nil {
		t.Fatalf("identical registration failed: %v", err)
	}
	if !again.SetQueueDepth("imports", 5) || again.QueueDepth.Value(Labels{"queue": "imports"}) != 5 {
		t.Fatal("reused instruments do not share the registry")
	}
}

func TestInstrumentsNilSafety(t *testing.T) {
	if _, err := NewInstruments(nil); err == nil {
		t.Fatal("nil registry accepted")
	}
	var nilInstruments *Instruments
	if nilInstruments.ObserveHTTPRequest("GET", "/", 200) {
		t.Fatal("nil instruments accepted a request")
	}
	if nilInstruments.SetJobState("import", "running", 1) {
		t.Fatal("nil instruments accepted a job state")
	}
	if nilInstruments.SetQueueDepth("imports", 1) || nilInstruments.AddQueueDepth("imports", 1) {
		t.Fatal("nil instruments accepted a queue depth")
	}
}
