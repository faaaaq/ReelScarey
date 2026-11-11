package httpapi

import (
	"errors"
	"net/http"

	"github.com/faaaaq/ReelScarey/internal/jellyfin"
	"github.com/faaaaq/ReelScarey/internal/plex"
)

func (s *Server) jellyfinSettings(w http.ResponseWriter, r *http.Request) {
	if s.jellyfin == nil {
		fail(w, 503, "unavailable", "Jellyfin integration is unavailable.")
		return
	}
	switch r.Method {
	case http.MethodGet:
		out, err := s.jellyfin.Settings(r.Context())
		if err != nil {
			s.internal(w, "jellyfin_settings", err)
			return
		}
		send(w, 200, out)
	case http.MethodPut:
		var in jellyfin.Config
		if !decodeJSON(w, r, &in) {
			return
		}
		if err := s.jellyfin.Save(r.Context(), in); err != nil {
			if errors.Is(err, jellyfin.ErrConfig) {
				fail(w, 400, "invalid_config", err.Error())
			} else {
				s.internal(w, "jellyfin_save", err)
			}
			return
		}
		send(w, 200, map[string]bool{"saved": true})
	case http.MethodDelete:
		if !decodeJSON(w, r, &struct{}{}) {
			return
		}
		if err := s.jellyfin.Delete(r.Context()); err != nil {
			s.internal(w, "jellyfin_delete", err)
			return
		}
		send(w, 200, map[string]bool{"removed": true})
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		fail(w, 405, "method_not_allowed", "Use GET, PUT or DELETE.")
	}
}

func (s *Server) jellyfinActivity(w http.ResponseWriter, r *http.Request) {
	if s.jellyfin == nil {
		fail(w, 503, "unavailable", "Jellyfin integration is unavailable.")
		return
	}
	out, err := s.jellyfin.Recent(r.Context())
	if err != nil {
		s.mediaServerError(w, "jellyfin_activity", err, jellyfin.ErrConfig, jellyfin.ErrUnavailable, "Jellyfin")
		return
	}
	send(w, 200, out)
}

func (s *Server) jellyfinRefresh(w http.ResponseWriter, r *http.Request) {
	if s.jellyfin == nil {
		fail(w, 503, "unavailable", "Jellyfin integration is unavailable.")
		return
	}
	if err := s.jellyfin.Refresh(r.Context()); err != nil {
		s.mediaServerError(w, "jellyfin_refresh", err, jellyfin.ErrConfig, jellyfin.ErrUnavailable, "Jellyfin")
		return
	}
	send(w, 200, map[string]bool{"requested": true})
}

func (s *Server) plexSettings(w http.ResponseWriter, r *http.Request) {
	if s.plex == nil {
		fail(w, 503, "unavailable", "Plex integration is unavailable.")
		return
	}
	switch r.Method {
	case http.MethodGet:
		out, err := s.plex.Settings(r.Context())
		if err != nil {
			s.internal(w, "plex_settings", err)
			return
		}
		send(w, 200, out)
	case http.MethodPut:
		var in plex.Config
		if !decodeJSON(w, r, &in) {
			return
		}
		if err := s.plex.Save(r.Context(), in); err != nil {
			if errors.Is(err, plex.ErrConfig) {
				fail(w, 400, "invalid_config", err.Error())
			} else {
				s.internal(w, "plex_save", err)
			}
			return
		}
		send(w, 200, map[string]bool{"saved": true})
	case http.MethodDelete:
		if !decodeJSON(w, r, &struct{}{}) {
			return
		}
		if err := s.plex.Delete(r.Context()); err != nil {
			s.internal(w, "plex_delete", err)
			return
		}
		send(w, 200, map[string]bool{"removed": true})
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		fail(w, 405, "method_not_allowed", "Use GET, PUT or DELETE.")
	}
}

func (s *Server) plexActivity(w http.ResponseWriter, r *http.Request) {
	if s.plex == nil {
		fail(w, 503, "unavailable", "Plex integration is unavailable.")
		return
	}
	out, err := s.plex.Recent(r.Context())
	if err != nil {
		s.mediaServerError(w, "plex_activity", err, plex.ErrConfig, plex.ErrUnavailable, "Plex")
		return
	}
	send(w, 200, out)
}

func (s *Server) plexRefresh(w http.ResponseWriter, r *http.Request) {
	if s.plex == nil {
		fail(w, 503, "unavailable", "Plex integration is unavailable.")
		return
	}
	if err := s.plex.Refresh(r.Context()); err != nil {
		s.mediaServerError(w, "plex_refresh", err, plex.ErrConfig, plex.ErrUnavailable, "Plex")
		return
	}
	send(w, 200, map[string]bool{"requested": true})
}

func (s *Server) plexSections(w http.ResponseWriter, r *http.Request) {
	if s.plex == nil {
		fail(w, 503, "unavailable", "Plex integration is unavailable.")
		return
	}
	out, err := s.plex.Sections(r.Context())
	if err != nil {
		s.mediaServerError(w, "plex_sections", err, plex.ErrConfig, plex.ErrUnavailable, "Plex")
		return
	}
	send(w, 200, map[string]any{"items": out})
}

func (s *Server) mediaServerError(w http.ResponseWriter, operation string, err, configErr, unavailableErr error, name string) {
	switch {
	case errors.Is(err, configErr):
		fail(w, 400, "not_configured", "Configure "+name+" first.")
	case errors.Is(err, unavailableErr):
		fail(w, 502, "upstream_unavailable", err.Error())
	default:
		s.internal(w, operation, err)
	}
}
