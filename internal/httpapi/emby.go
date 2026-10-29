package httpapi

import (
	"errors"
	"github.com/faaaaq/ReelScarey/internal/emby"
	"net/http"
)

func (s *Server) embySettings(w http.ResponseWriter, r *http.Request) {
	if s.emby == nil {
		fail(w, 503, "unavailable", "Emby integration is unavailable.")
		return
	}
	switch r.Method {
	case http.MethodGet:
		out, e := s.emby.Settings(r.Context())
		if e != nil {
			s.internal(w, "emby_settings", e)
			return
		}
		send(w, 200, out)
	case http.MethodPut:
		var in emby.Config
		if !decodeJSON(w, r, &in) {
			return
		}
		if e := s.emby.Save(r.Context(), in); e != nil {
			if errors.Is(e, emby.ErrConfig) {
				fail(w, 400, "invalid_config", e.Error())
			} else {
				s.internal(w, "emby_save", e)
			}
			return
		}
		send(w, 200, map[string]bool{"saved": true})
	case http.MethodDelete:
		if !decodeJSON(w, r, &struct{}{}) {
			return
		}
		if s.embyMonitor != nil {
			status, err := s.embyMonitor.Status(r.Context())
			if err != nil {
				s.internal(w, "emby_monitor_status", err)
				return
			}
			if status.Enabled {
				if _, err = s.embyMonitor.Set(r.Context(), false, status.Revision); err != nil {
					fail(w, 409, "monitor_conflict", "Monitoring changed. Reload before removing the connection.")
					return
				}
			}
		}
		if e := s.emby.Delete(r.Context()); e != nil {
			s.internal(w, "emby_delete", e)
			return
		}
		send(w, 200, map[string]bool{"removed": true})
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		fail(w, 405, "method_not_allowed", "Use GET, PUT or DELETE.")
	}
}
func (s *Server) embyActivity(w http.ResponseWriter, r *http.Request) {
	if s.emby == nil {
		fail(w, 503, "unavailable", "Emby integration is unavailable.")
		return
	}
	out, e := s.emby.Recent(r.Context())
	if e != nil {
		if errors.Is(e, emby.ErrConfig) {
			fail(w, 400, "not_configured", "Configure Emby first.")
		} else if errors.Is(e, emby.ErrUnavailable) {
			fail(w, 502, "upstream_unavailable", e.Error())
		} else {
			s.internal(w, "emby_activity", e)
		}
		return
	}
	send(w, 200, out)
}
func (s *Server) embyMonitorSettings(w http.ResponseWriter, r *http.Request) {
	if s.embyMonitor == nil {
		fail(w, 503, "unavailable", "Emby monitoring is unavailable.")
		return
	}
	var out emby.MonitorStatus
	var e error
	switch r.Method {
	case http.MethodGet:
		out, e = s.embyMonitor.Status(r.Context())
	case http.MethodPut:
		var in struct {
			Enabled  bool `json:"enabled"`
			Revision int  `json:"revision"`
		}
		if !decodeJSON(w, r, &in) {
			return
		}
		out, e = s.embyMonitor.Set(r.Context(), in.Enabled, in.Revision)
	default:
		w.Header().Set("Allow", "GET, PUT")
		fail(w, 405, "method_not_allowed", "Use GET or PUT.")
		return
	}
	if e != nil {
		if errors.Is(e, emby.ErrConfig) || errors.Is(e, emby.ErrMonitorConflict) {
			fail(w, 409, "monitor_unavailable", e.Error())
		} else {
			s.internal(w, "emby_monitor", e)
		}
		return
	}
	send(w, 200, out)
}
func (s *Server) embyHistory(w http.ResponseWriter, r *http.Request) {
	if s.embyMonitor == nil {
		fail(w, 503, "unavailable", "Emby monitoring is unavailable.")
		return
	}
	out, e := s.embyMonitor.History(r.Context())
	if e != nil {
		s.internal(w, "emby_history", e)
		return
	}
	send(w, 200, map[string]any{"items": out})
}
