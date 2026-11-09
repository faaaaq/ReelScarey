package metrics

import (
	"math"
	"strings"
	"sync"
	"testing"
)

func TestRenderFormattingAndSorting(t *testing.T) {
	r := NewRegistry()
	c, err := r.Counter("http_requests_total", "Total requests.", "status", "method")
	if err != nil {
		t.Fatal(err)
	}
	if !c.Add(2, Labels{"method": "GET", "status": "200"}) {
		t.Fatal("counter update rejected")
	}
	if !c.Inc(Labels{"method": "POST", "status": "500"}) {
		t.Fatal("counter update rejected")
	}
	g, err := r.Gauge("queue_depth", "Queue depth.\nLine two.", "queue")
	if err != nil {
		t.Fatal(err)
	}
	if !g.Set(3.5, Labels{"queue": "imports"}) {
		t.Fatal("gauge update rejected")
	}
	const want = `# HELP http_requests_total Total requests.
# TYPE http_requests_total counter
http_requests_total{method="GET",status="200"} 2
http_requests_total{method="POST",status="500"} 1
# HELP queue_depth Queue depth.\nLine two.
# TYPE queue_depth gauge
queue_depth{queue="imports"} 3.5
`
	if got := r.Render(); got != want {
		t.Fatalf("unexpected render:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderEscaping(t *testing.T) {
	r := NewRegistry()
	c, err := r.Counter("paths_total", "A \\ path\nnext line", "path")
	if err != nil {
		t.Fatal(err)
	}
	if !c.Inc(Labels{"path": "a\"b\\c\nd"}) {
		t.Fatal("counter update rejected")
	}
	const want = `# HELP paths_total A \\ path\nnext line
# TYPE paths_total counter
paths_total{path="a\"b\\c\nd"} 1
`
	if got := r.Render(); got != want {
		t.Fatalf("unexpected escaped render:\n%s\nwant:\n%s", got, want)
	}
}

func TestUnlabelledMetricStartsAtZero(t *testing.T) {
	r := NewRegistry()
	if _, err := r.Counter("build_info", "Build marker."); err != nil {
		t.Fatal(err)
	}
	const want = `# HELP build_info Build marker.
# TYPE build_info counter
build_info 0
`
	if got := r.Render(); got != want {
		t.Fatalf("unexpected zero render:\n%s", got)
	}
}

func TestRenderIsDeterministicAcrossInsertionOrder(t *testing.T) {
	build := func(order []string) string {
		r := NewRegistry()
		g, err := r.Gauge("requests_in_flight", "Requests in flight.", "route", "method")
		if err != nil {
			t.Fatal(err)
		}
		for _, route := range order {
			if !g.Set(1, Labels{"route": route, "method": "GET"}) {
				t.Fatal("gauge update rejected")
			}
		}
		return r.Render()
	}
	first := build([]string{"/a", "/b", "/c", "/d"})
	second := build([]string{"/d", "/c", "/b", "/a"})
	if first != second {
		t.Fatalf("output depends on insertion order:\n%s\n---\n%s", first, second)
	}
	if strings.Index(first, `route="/a"`) > strings.Index(first, `route="/b"`) {
		t.Fatal("samples not sorted")
	}
}

func TestConcurrentUpdates(t *testing.T) {
	r := NewRegistry(WithMaxSeriesPerMetric(32))
	c, err := r.Counter("work_total", "Work done.", "route")
	if err != nil {
		t.Fatal(err)
	}
	g, err := r.Gauge("workers", "Workers.", "state")
	if err != nil {
		t.Fatal(err)
	}
	const workers = 16
	const increments = 100
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < increments; i++ {
				if !c.Inc(Labels{"route": "/api/v1/library"}) {
					t.Error("counter update rejected")
					return
				}
				if !g.Add(1, Labels{"state": "running"}) {
					t.Error("gauge update rejected")
					return
				}
			}
		}(w)
	}
	wg.Wait()
	if got := c.Value(Labels{"route": "/api/v1/library"}); got != workers*increments {
		t.Fatalf("counter = %v", got)
	}
	if got := g.Value(Labels{"state": "running"}); got != workers*increments {
		t.Fatalf("gauge = %v", got)
	}
	if first, second := r.Render(), r.Render(); first != second {
		t.Fatalf("render is not deterministic:\n%s\n---\n%s", first, second)
	}
}

func TestBoundedLabelSets(t *testing.T) {
	r := NewRegistry(
		WithMaxLabelNames(2),
		WithMaxLabelValueBytes(4),
		WithMaxSeriesPerMetric(2),
	)
	c, err := r.Counter("requests_total", "Requests.", "route")
	if err != nil {
		t.Fatal(err)
	}
	if !c.Inc(Labels{"route": "a"}) || !c.Inc(Labels{"route": "b"}) {
		t.Fatal("updates within the bound were rejected")
	}
	if c.Inc(Labels{"route": "c"}) {
		t.Fatal("series beyond the cap was accepted")
	}
	if c.Value(Labels{"route": "c"}) != 0 {
		t.Fatal("rejected series has a value")
	}
	if strings.Contains(r.Render(), `route="c"`) {
		t.Fatal("rejected series rendered")
	}
	if c.Inc(Labels{"route": "toolong"}) {
		t.Fatal("oversized label value accepted")
	}
	if c.Inc(Labels{}) {
		t.Fatal("missing labels accepted")
	}
	if c.Inc(Labels{"route": "a", "extra": "x"}) {
		t.Fatal("unknown label accepted")
	}
	if got := r.DroppedSeries(); got != 4 {
		t.Fatalf("dropped = %d, want 4", got)
	}
	// Existing series still update after the cap is reached.
	if !c.Inc(Labels{"route": "a"}) || c.Value(Labels{"route": "a"}) != 2 {
		t.Fatal("existing series stopped updating")
	}
}

func TestMetricCreationValidation(t *testing.T) {
	r := NewRegistry(WithMaxLabelNames(2))
	cases := []struct {
		name    string
		labels  []string
		wantErr bool
	}{
		{"bad-name", nil, true},
		{"1bad", nil, true},
		{"good_name:total", nil, false},
		{"too_many", []string{"a", "b", "c"}, true},
		{"duplicate_labels", []string{"a", "a"}, true},
		{"reserved", []string{"__trace"}, true},
		{"bad label", []string{"bad-label"}, true},
	}
	for _, tc := range cases {
		_, err := r.Counter(tc.name, "Help.", tc.labels...)
		if (err != nil) != tc.wantErr {
			t.Fatalf("Counter(%q, %v) error = %v, wantErr %v", tc.name, tc.labels, err, tc.wantErr)
		}
	}
	if _, err := r.Counter("same", "Help.", "label"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Counter("same", "Help.", "label"); err != nil {
		t.Fatalf("identical re-registration failed: %v", err)
	}
	if _, err := r.Gauge("same", "Help.", "label"); err == nil {
		t.Fatal("type conflict accepted")
	}
	if _, err := r.Counter("same", "Different help.", "label"); err == nil {
		t.Fatal("help conflict accepted")
	}
	if _, err := r.Counter("same", "Help.", "other"); err == nil {
		t.Fatal("label conflict accepted")
	}
	var nilRegistry *Registry
	if _, err := nilRegistry.Counter("x", "x"); err == nil {
		t.Fatal("nil registry accepted")
	}
}

func TestCounterAndGaugeValueRules(t *testing.T) {
	r := NewRegistry()
	c, err := r.Counter("work_total", "Work.", "kind")
	if err != nil {
		t.Fatal(err)
	}
	for _, delta := range []float64{-1, math.NaN(), math.Inf(1)} {
		if c.Add(delta, Labels{"kind": "x"}) {
			t.Fatalf("counter accepted %v", delta)
		}
	}
	if c.Value(Labels{"kind": "x"}) != 0 {
		t.Fatal("rejected counter delta changed the value")
	}
	g, err := r.Gauge("temperature", "Temperature.", "room")
	if err != nil {
		t.Fatal(err)
	}
	if !g.Set(math.Inf(1), Labels{"room": "a"}) || !g.Set(math.NaN(), Labels{"room": "b"}) || !g.Set(-2.5, Labels{"room": "c"}) {
		t.Fatal("gauge rejected a valid value")
	}
	out := r.Render()
	for _, want := range []string{`temperature{room="a"} +Inf`, `temperature{room="b"} NaN`, `temperature{room="c"} -2.5`} {
		if !strings.Contains(out, want) {
			t.Fatalf("render missing %q:\n%s", want, out)
		}
	}
}

func TestDroppedSeriesNilRegistry(t *testing.T) {
	var r *Registry
	if r.DroppedSeries() != 0 || r.Render() != "" {
		t.Fatal("nil registry did not behave as empty")
	}
}
