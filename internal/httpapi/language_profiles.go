package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"

	"github.com/faaaaq/ReelScarey/internal/subtitle"
)

func (s *Server) languageProfiles(w http.ResponseWriter, r *http.Request) {
	if s.subtitle == nil {
		fail(w, 503, "unavailable", "Subtitle language profiles are unavailable.")
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := s.subtitle.LanguageProfiles(r.Context())
		if err != nil {
			s.internal(w, "language_profiles", err)
			return
		}
		send(w, 200, map[string]any{"items": items})
	case http.MethodPost:
		var in subtitle.LanguageProfile
		if !decodeJSON(w, r, &in) {
			return
		}
		out, err := s.subtitle.SaveLanguageProfile(r.Context(), 0, in)
		if err != nil {
			s.languageProfileError(w, err)
			return
		}
		send(w, 201, out)
	default:
		w.Header().Set("Allow", "GET, POST")
		fail(w, 405, "method_not_allowed", "Use GET or POST.")
	}
}

func (s *Server) languageProfile(w http.ResponseWriter, r *http.Request) {
	if s.subtitle == nil {
		fail(w, 503, "unavailable", "Subtitle language profiles are unavailable.")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		fail(w, 400, "invalid_profile", "Choose a saved language profile.")
		return
	}
	switch r.Method {
	case http.MethodPut:
		var in subtitle.LanguageProfile
		if !decodeJSON(w, r, &in) {
			return
		}
		in.ID = id
		out, err := s.subtitle.SaveLanguageProfile(r.Context(), id, in)
		if err != nil {
			s.languageProfileError(w, err)
			return
		}
		send(w, 200, out)
	case http.MethodDelete:
		if !decodeOptionalJSON(w, r, &struct{}{}) {
			return
		}
		if err := s.subtitle.DeleteLanguageProfile(r.Context(), id); err != nil {
			s.languageProfileError(w, err)
			return
		}
		send(w, 200, map[string]bool{"removed": true})
	default:
		w.Header().Set("Allow", "PUT, DELETE")
		fail(w, 405, "method_not_allowed", "Use PUT or DELETE.")
	}
}

func (s *Server) languageDefaults(w http.ResponseWriter, r *http.Request) {
	if s.subtitle == nil {
		fail(w, 503, "unavailable", "Subtitle language profiles are unavailable.")
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := s.subtitle.LanguageDefaults(r.Context())
		if err != nil {
			s.internal(w, "language_defaults", err)
			return
		}
		send(w, 200, map[string]any{"items": items})
	case http.MethodPut:
		var body struct {
			Items []subtitle.LanguageDefault `json:"items"`
		}
		if !decodeJSON(w, r, &body) {
			return
		}
		items, err := s.subtitle.SaveLanguageDefaults(r.Context(), body.Items)
		if err != nil {
			s.languageProfileError(w, err)
			return
		}
		send(w, 200, map[string]any{"items": items})
	default:
		w.Header().Set("Allow", "GET, PUT")
		fail(w, 405, "method_not_allowed", "Use GET or PUT.")
	}
}

func (s *Server) mediaLanguageProfile(w http.ResponseWriter, r *http.Request) {
	if s.subtitle == nil {
		fail(w, 503, "unavailable", "Subtitle language profiles are unavailable.")
		return
	}
	mediaID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || mediaID < 1 {
		fail(w, 400, "invalid_media", "Choose a saved title.")
		return
	}
	switch r.Method {
	case http.MethodPut:
		var body struct {
			ProfileID *int64 `json:"profile_id"`
		}
		if !decodeJSON(w, r, &body) {
			return
		}
		if body.ProfileID != nil && *body.ProfileID < 1 {
			body.ProfileID = nil
		}
		if err := s.subtitle.SetMediaLanguageProfile(r.Context(), mediaID, body.ProfileID); err != nil {
			s.languageProfileError(w, err)
			return
		}
		send(w, 200, map[string]bool{"saved": true})
	case http.MethodDelete:
		if !decodeOptionalJSON(w, r, &struct{}{}) {
			return
		}
		if err := s.subtitle.SetMediaLanguageProfile(r.Context(), mediaID, nil); err != nil {
			s.languageProfileError(w, err)
			return
		}
		send(w, 200, map[string]bool{"removed": true})
	default:
		w.Header().Set("Allow", "PUT, DELETE")
		fail(w, 405, "method_not_allowed", "Use PUT or DELETE.")
	}
}

func (s *Server) languageProfileError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, subtitle.ErrLanguageProfile):
		fail(w, 400, "validation_error", err.Error())
	case errors.Is(err, sql.ErrNoRows):
		fail(w, 404, "not_found", "That language profile or title does not exist.")
	default:
		s.internal(w, "language_profile", err)
	}
}
