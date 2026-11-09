// Package releaseguard contains small, self-contained release reliability
// primitives: a durable release blacklist, stable release identity helpers,
// conservative stalled-download detection and bounded completion-webhook
// parsing.
//
// The package deliberately has no HTTP handlers, worker loops or logger. It
// only reads and writes the tables owned by the existing application schema,
// and callers remain responsible for logging. Parsed webhook values are bounded
// and contain only the provider fields needed for identity and state; raw
// payloads and credentials are never returned, logged or stored here.
package releaseguard

import "fmt"

const (
	// ProtocolTorrent identifies BitTorrent releases.
	ProtocolTorrent = "torrent"
	// ProtocolUsenet identifies Usenet/NZB releases.
	ProtocolUsenet = "usenet"

	// StateCompleted means the provider reported a successful completion.
	StateCompleted = "completed"
	// StateFailed means the provider reported a failure or an unclean result.
	StateFailed = "failed"
)

// ValidationError reports malformed caller input or an unusable payload.
// It never contains raw webhook bodies or credentials.
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

func invalid(format string, args ...any) error {
	return &ValidationError{Message: fmt.Sprintf(format, args...)}
}
