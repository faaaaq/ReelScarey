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
