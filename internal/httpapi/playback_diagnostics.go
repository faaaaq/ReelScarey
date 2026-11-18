package httpapi

import (
	"fmt"
	"net/http"
	"time"

	"github.com/faaaaq/ReelScarey/internal/music"
)

// playbackDiagnosticHeaders reports negotiation and stream-open timing without
// exposing file paths, usernames, or provider details. stream-open measures
// the service work before headers are written; it is not total listening time.
func playbackDiagnosticHeaders(w http.ResponseWriter, playback music.PlaybackStream, started time.Time) {
	requested := playback.RequestedFormat
	if requested == "" {
		requested = "source"
	}
	w.Header().Set("X-ReelScarey-Requested-Format", requested)
	w.Header().Set("X-ReelScarey-Delivered-Format", playback.Codec)
	w.Header().Set("X-ReelScarey-Playback-Path", playback.Mode)
	w.Header().Set("X-ReelScarey-Playback-Reason", playback.Reason)
	w.Header().Set("Server-Timing", fmt.Sprintf("stream-open;dur=%.1f", float64(time.Since(started).Microseconds())/1000))
}
