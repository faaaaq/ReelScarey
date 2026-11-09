package metrics

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Instruments are the standard application metrics a caller can update. They
// are ordinary counters/gauges registered on a caller-owned Registry; this
// package still exposes no HTTP handler.
type Instruments struct {
	// HTTPRequests counts completed HTTP requests by method, route pattern and
	// status code.
	HTTPRequests *Counter
	// JobStates holds the current job count by kind and state.
	JobStates *Gauge
	// QueueDepth holds the current queued item count by queue name.
	QueueDepth *Gauge
}

// NewInstruments registers (or reuses) the standard metrics on registry.
func NewInstruments(registry *Registry) (*Instruments, error) {
	if registry == nil {
		return nil, fmt.Errorf("metrics: nil registry")
	}
	requests, err := registry.Counter(
		"reelscarey_http_requests_total",
		"Completed HTTP requests by method, route pattern and status code.",
		"method", "route", "status",
	)
	if err != nil {
		return nil, err
	}
	jobs, err := registry.Gauge(
		"reelscarey_jobs",
		"Current jobs by kind and state.",
		"kind", "state",
	)
	if err != nil {
		return nil, err
	}
	queue, err := registry.Gauge(
		"reelscarey_queue_depth",
		"Current queued items by queue name.",
		"queue",
	)
	if err != nil {
		return nil, err
	}
	return &Instruments{HTTPRequests: requests, JobStates: jobs, QueueDepth: queue}, nil
}

// ObserveHTTPRequest counts one completed request. Methods are normalized to a
// small known set, and status values outside 100-599 become "unknown". Callers
// should pass a route pattern (for example "/api/v1/movies/{id}") rather than a
// raw path so label cardinality stays bounded.
func (i *Instruments) ObserveHTTPRequest(method, route string, status int) bool {
	if i == nil || i.HTTPRequests == nil {
		return false
	}
	return i.HTTPRequests.Inc(Labels{
		"method": normalizeMethod(method),
		"route":  labelText(route),
		"status": normalizeStatus(status),
	})
}

// SetJobState replaces the recorded count for one job kind and state.
func (i *Instruments) SetJobState(kind, state string, count float64) bool {
	if i == nil || i.JobStates == nil {
		return false
	}
	return i.JobStates.Set(count, Labels{"kind": labelText(kind), "state": labelText(state)})
}

// SetQueueDepth replaces the recorded depth for one queue.
func (i *Instruments) SetQueueDepth(queue string, depth float64) bool {
	if i == nil || i.QueueDepth == nil {
		return false
	}
	return i.QueueDepth.Set(depth, Labels{"queue": labelText(queue)})
}

// AddQueueDepth adjusts the recorded depth for one queue.
func (i *Instruments) AddQueueDepth(queue string, delta float64) bool {
	if i == nil || i.QueueDepth == nil {
		return false
	}
	return i.QueueDepth.Add(delta, Labels{"queue": labelText(queue)})
}

func normalizeMethod(method string) string {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case "GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "CONNECT", "TRACE":
		return strings.ToUpper(strings.TrimSpace(method))
	default:
		return "other"
	}
}

func normalizeStatus(status int) string {
	if status >= 100 && status <= 599 {
		return strconv.Itoa(status)
	}
	return "unknown"
}

func labelText(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	if !utf8.ValidString(value) {
		return "invalid"
	}
	return value
}
