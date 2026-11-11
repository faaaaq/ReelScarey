package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/faaaaq/ReelScarey/internal/notify"
)

func (s *Server) notifications(w http.ResponseWriter, r *http.Request) {
	if s.notify == nil {
		fail(w, 503, "unavailable", "Notification channels are unavailable.")
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := s.notify.List(r.Context())
		if err != nil {
			s.internal(w, "notifications_list", err)
			return
		}
		send(w, 200, map[string]any{"items": items, "events": notify.SupportedEvents, "kinds": notify.SupportedKinds})
	case http.MethodPost:
		var in notify.ChannelInput
		if !decodeJSON(w, r, &in) {
			return
		}
		out, err := s.notify.Save(r.Context(), 0, in)
		if err != nil {
			s.notifyError(w, err)
			return
		}
		send(w, 201, out)
	default:
		w.Header().Set("Allow", "GET, POST")
		fail(w, 405, "method_not_allowed", "Use GET to list channels or POST to create one.")
	}
}

func (s *Server) notification(w http.ResponseWriter, r *http.Request) {
	if s.notify == nil {
		fail(w, 503, "unavailable", "Notification channels are unavailable.")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		fail(w, 400, "invalid_notification", "Choose a saved notification channel.")
		return
	}
	switch r.Method {
	case http.MethodPut:
		var in notify.ChannelInput
		if !decodeJSON(w, r, &in) {
			return
		}
		out, err := s.notify.Save(r.Context(), id, in)
		if err != nil {
			s.notifyError(w, err)
			return
		}
		send(w, 200, out)
	case http.MethodDelete:
		if !decodeOptionalJSON(w, r, &struct{}{}) {
			return
		}
		if err := s.notify.Delete(r.Context(), id); err != nil {
			s.notifyError(w, err)
			return
		}
		send(w, 200, map[string]bool{"removed": true})
	default:
		w.Header().Set("Allow", "PUT, DELETE")
		fail(w, 405, "method_not_allowed", "Use PUT to save a channel or DELETE to remove it.")
	}
}

func (s *Server) notificationTest(w http.ResponseWriter, r *http.Request) {
	if s.notify == nil {
		fail(w, 503, "unavailable", "Notification channels are unavailable.")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		fail(w, 400, "invalid_notification", "Choose a saved notification channel.")
		return
	}
	if err := s.notify.Test(r.Context(), id); err != nil {
		s.notifyError(w, err)
		return
	}
	send(w, 200, map[string]bool{"sent": true})
}

func (s *Server) notificationOutbox(w http.ResponseWriter, r *http.Request) {
	if s.notify == nil {
		fail(w, 503, "unavailable", "Notification channels are unavailable.")
		return
	}
	limit := 100
	if value := r.URL.Query().Get("limit"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 || parsed > 200 {
			fail(w, 400, "invalid_limit", "Choose a limit between 1 and 200.")
			return
		}
		limit = parsed
	}
	items, err := s.notify.Outbox(r.Context(), limit)
	if err != nil {
		s.internal(w, "notification_outbox", err)
		return
	}
	send(w, 200, map[string]any{"items": items})
}

func (s *Server) notificationRetry(w http.ResponseWriter, r *http.Request) {
	if s.notify == nil {
		fail(w, 503, "unavailable", "Notification channels are unavailable.")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		fail(w, 400, "invalid_notification", "Choose a delivery record.")
		return
	}
	if err := s.notify.Retry(r.Context(), id); err != nil {
		s.notifyError(w, err)
		return
	}
	send(w, 200, map[string]bool{"queued": true})
}

func (s *Server) notifyError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, notify.ErrConfig):
		fail(w, 400, "validation_error", err.Error())
	case errors.Is(err, notify.ErrNotFound):
		fail(w, 404, "notification_not_found", err.Error())
	case errors.Is(err, notify.ErrConflict):
		fail(w, 409, "revision_conflict", err.Error())
	default:
		s.internal(w, "notification", err)
	}
}
