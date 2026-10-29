package httpapi

import (
	"errors"
	"net/http"

	"github.com/faaaaq/ReelScarey/internal/instance"
	"github.com/faaaaq/ReelScarey/internal/operations"
)

func (s *Server) setupGuide(w http.ResponseWriter, r *http.Request) {
	guide, err := s.operations.Guide(r.Context())
	if err != nil {
		s.internal(w, "setup_guide", err)
		return
	}
	send(w, 200, guide)
}

func (s *Server) installationChecks(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		send(w, 200, map[string]any{"report": s.operations.Last(), "recovery_pending": instance.RecoveryPending(s.config.ConfigDir)})
	case http.MethodPost:
		if !decodeJSON(w, r, &struct{}{}) {
			return
		}
		report, err := s.operations.Run(r.Context())
		if errors.Is(err, operations.ErrBusy) {
			fail(w, 409, "checks_busy", "Installation checks are already running.")
			return
		}
		if err != nil {
			fail(w, 503, "checks_unavailable", "Installation checks could not finish. Try again.")
			return
		}
		send(w, 200, map[string]any{"report": report, "recovery_pending": instance.RecoveryPending(s.config.ConfigDir)})
	default:
		w.Header().Set("Allow", "GET, POST")
		fail(w, 405, "method_not_allowed", "This method is not supported.")
	}
}

func (s *Server) resumeRecovery(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Reviewed bool `json:"reviewed"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if !body.Reviewed {
		fail(w, 400, "review_required", "Review restored settings, storage paths and download activity before resuming workers.")
		return
	}
	if err := s.operations.Resume(r.Context(), body.Reviewed); err != nil {
		fail(w, 409, "recovery_held", "Recovery is still paused. Run the system checks and review the recovery marker and storage configuration.")
		return
	}
	send(w, 200, map[string]bool{"recovery_pending": false})
}
