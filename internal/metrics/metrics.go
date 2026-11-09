// Package metrics provides a small dependency-free metrics registry and a
// Prometheus text-format renderer.
//
// The registry owns counters, gauges, fixed label names and a bounded number
// of distinct label sets per metric. It has no global state, no HTTP handler
// and no secrets; callers decide where Registry.Render is published. Updates
// that violate the label contract or exceed a cardinality limit are rejected
// and counted by DroppedSeries instead of growing memory without bound.
package metrics

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

const (
	defaultMaxLabelNames      = 8
	defaultMaxLabelValueBytes = 256
	defaultMaxSeries          = 1000
)

// Labels carries one sample's label values. Every name declared when the
// metric is created must be present exactly once.
type Labels map[string]string

// Option configures a Registry.
type Option func(*config)

type config struct {
	maxLabelNames      int
	maxLabelValueBytes int
	maxSeries          int
}

// WithMaxLabelNames bounds the number of label names a single metric may
// declare. Non-positive values keep the default.
func WithMaxLabelNames(n int) Option {
	return func(c *config) {
		if n > 0 {
			c.maxLabelNames = n
		}
	}
}

// WithMaxLabelValueBytes bounds the length of each label value. Non-positive
// values keep the default.
func WithMaxLabelValueBytes(n int) Option {
	return func(c *config) {
		if n > 0 {
			c.maxLabelValueBytes = n
		}
	}
}

// WithMaxSeriesPerMetric bounds the number of distinct label sets one metric
// may hold. Existing label sets keep updating after the limit is reached;
// new ones are rejected. Non-positive values keep the default.
func WithMaxSeriesPerMetric(n int) Option {
	return func(c *config) {
		if n > 0 {
			c.maxSeries = n
		}
	}
}

// Registry owns metric families. It is safe for concurrent use.
type Registry struct {
	mu       sync.RWMutex
	cfg      config
	families map[string]*family
}

// NewRegistry returns an empty registry.
func NewRegistry(options ...Option) *Registry {
	cfg := config{
		maxLabelNames:      defaultMaxLabelNames,
		maxLabelValueBytes: defaultMaxLabelValueBytes,
		maxSeries:          defaultMaxSeries,
	}
	for _, option := range options {
		if option != nil {
			option(&cfg)
		}
	}
	return &Registry{cfg: cfg, families: make(map[string]*family)}
}

type family struct {
	name               string
	help               string
	kind               string
	labelNames         []string
	maxSeries          int
	maxLabelValueBytes int
	mu                 sync.Mutex
	series             map[string]*series
	dropped            uint64
}

type series struct {
	labels []string
	value  float64
}

// Counter is a monotonically increasing metric. A nil Counter ignores updates
// so callers can keep instrumentation optional.
type Counter struct {
	family *family
}

// Gauge is a metric that can be set or adjusted in either direction.
type Gauge struct {
	family *family
}

// Counter returns the counter with the given name, or registers it with the
// fixed label names. Reusing the same definition returns the existing counter;
// a conflicting type, help text or label set is an error.
func (r *Registry) Counter(name, help string, labelNames ...string) (*Counter, error) {
	f, err := r.register("counter", name, help, labelNames)
	if err != nil {
		return nil, err
	}
	return &Counter{family: f}, nil
}

// Gauge returns the gauge with the given name, or registers it with the fixed
// label names. Reusing the same definition returns the existing gauge; a
// conflicting type, help text or label set is an error.
func (r *Registry) Gauge(name, help string, labelNames ...string) (*Gauge, error) {
	f, err := r.register("gauge", name, help, labelNames)
	if err != nil {
		return nil, err
	}
	return &Gauge{family: f}, nil
}

func (r *Registry) register(kind, name, help string, labelNames []string) (*family, error) {
	if r == nil {
		return nil, fmt.Errorf("metrics: nil registry")
	}
	if !validMetricName(name) {
		return nil, fmt.Errorf("metrics: invalid metric name %q", name)
	}
	if !utf8.ValidString(help) {
		return nil, fmt.Errorf("metrics: help text for %q is not valid UTF-8", name)
	}
	if len(labelNames) > r.cfg.maxLabelNames {
		return nil, fmt.Errorf("metrics: metric %q declares %d labels; the limit is %d", name, len(labelNames), r.cfg.maxLabelNames)
	}
	names := append([]string(nil), labelNames...)
	sort.Strings(names)
	seen := make(map[string]bool, len(names))
	for _, label := range names {
		if !validLabelName(label) {
			return nil, fmt.Errorf("metrics: invalid label name %q on %q", label, name)
		}
		if strings.HasPrefix(label, "__") {
			return nil, fmt.Errorf("metrics: label %q on %q uses a reserved prefix", label, name)
		}
		if seen[label] {
			return nil, fmt.Errorf("metrics: duplicate label %q on %q", label, name)
		}
		seen[label] = true
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.families[name]; ok {
		if existing.kind != kind || existing.help != help || !sameStrings(existing.labelNames, names) {
			return nil, fmt.Errorf("metrics: metric %q already registered with a different definition", name)
		}
		return existing, nil
	}
	f := &family{
		name:               name,
		help:               help,
		kind:               kind,
		labelNames:         names,
		maxSeries:          r.cfg.maxSeries,
		maxLabelValueBytes: r.cfg.maxLabelValueBytes,
		series:             make(map[string]*series),
	}
	if len(names) == 0 {
		// An unlabelled metric is visible immediately, matching how a caller
		// thinks about a process-wide counter.
		f.series[""] = &series{}
	}
	r.families[name] = f
	return f, nil
}

// Inc adds one to the counter.
func (c *Counter) Inc(labels Labels) bool { return c.Add(1, labels) }

// Add adds a non-negative finite delta to the counter. Negative, NaN and
// infinite deltas are rejected.
func (c *Counter) Add(delta float64, labels Labels) bool {
	if c == nil || c.family == nil {
		return false
	}
	if delta < 0 || math.IsNaN(delta) || math.IsInf(delta, 0) {
		c.family.mu.Lock()
		c.family.dropped++
		c.family.mu.Unlock()
		return false
	}
	return c.family.update(labels, delta, false)
}

// Value returns the current value for labels, or zero when the label set has
// not been recorded or violates the label contract.
func (c *Counter) Value(labels Labels) float64 {
	if c == nil {
		return 0
	}
	return c.family.value(labels)
}

// Set replaces the gauge value. NaN and infinities are allowed, as in
// Prometheus text format.
func (g *Gauge) Set(value float64, labels Labels) bool {
	if g == nil || g.family == nil {
		return false
	}
	return g.family.update(labels, value, true)
}

// Add adjusts the gauge by delta.
func (g *Gauge) Add(delta float64, labels Labels) bool {
	if g == nil || g.family == nil {
		return false
	}
	return g.family.update(labels, delta, false)
}

// Inc adds one to the gauge.
func (g *Gauge) Inc(labels Labels) bool { return g.Add(1, labels) }

// Dec subtracts one from the gauge.
func (g *Gauge) Dec(labels Labels) bool { return g.Add(-1, labels) }

// Value returns the current value for labels, or zero when absent.
func (g *Gauge) Value(labels Labels) float64 {
	if g == nil {
		return 0
	}
	return g.family.value(labels)
}

// DroppedSeries reports how many updates were rejected by label validation or
// the per-metric series bound. It is a diagnostic, not a Prometheus metric.
func (r *Registry) DroppedSeries() uint64 {
	if r == nil {
		return 0
	}
	r.mu.RLock()
	families := make([]*family, 0, len(r.families))
	for _, f := range r.families {
		families = append(families, f)
	}
	r.mu.RUnlock()
	var total uint64
	for _, f := range families {
		f.mu.Lock()
		total += f.dropped
		f.mu.Unlock()
	}
	return total
}

func (f *family) update(labels Labels, amount float64, set bool) bool {
	values, key, ok := f.lookup(labels)
	if !ok {
		f.mu.Lock()
		f.dropped++
		f.mu.Unlock()
		return false
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	s, exists := f.series[key]
	if !exists {
		if len(f.series) >= f.maxSeries {
			f.dropped++
			return false
		}
		s = &series{labels: append([]string(nil), values...)}
		f.series[key] = s
	}
	if set {
		s.value = amount
		return true
	}
	next := s.value + amount
	if math.IsInf(next, 0) || math.IsNaN(next) {
		f.dropped++
		return false
	}
	s.value = next
	return true
}

func (f *family) value(labels Labels) float64 {
	_, key, ok := f.lookup(labels)
	if !ok {
		return 0
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if s, exists := f.series[key]; exists {
		return s.value
	}
	return 0
}

// lookup validates the supplied label set and returns its values in the
// family's sorted label-name order plus a collision-free map key.
func (f *family) lookup(labels Labels) ([]string, string, bool) {
	if len(labels) != len(f.labelNames) {
		return nil, "", false
	}
	values := make([]string, len(f.labelNames))
	for index, name := range f.labelNames {
		value, ok := labels[name]
		if !ok || !utf8.ValidString(value) || len(value) > f.maxLabelValueBytes {
			return nil, "", false
		}
		values[index] = value
	}
	return values, seriesKey(values), true
}

func seriesKey(values []string) string {
	if len(values) == 0 {
		return ""
	}
	var b strings.Builder
	for _, value := range values {
		b.WriteString(strconv.Itoa(len(value)))
		b.WriteByte(':')
		b.WriteString(value)
	}
	return b.String()
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func validMetricName(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if isAlpha(c) || c == '_' || c == ':' {
			continue
		}
		if i > 0 && (isDigit(c) || c == '_' || c == ':') {
			continue
		}
		return false
	}
	return true
}

func validLabelName(name string) bool {
	if name == "" || !isAlpha(name[0]) && name[0] != '_' {
		return false
	}
	for i := 1; i < len(name); i++ {
		c := name[i]
		if !isAlpha(c) && !isDigit(c) && c != '_' {
			return false
		}
	}
	return true
}

func isAlpha(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func isDigit(c byte) bool {
	return c >= '0' && c <= '9'
}
