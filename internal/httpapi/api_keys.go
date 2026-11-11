package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/faaaaq/ReelScarey/internal/auth"
)

func (s *Server) apiKeys(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		items, err := s.auth.ListAPIKeys(r.Context())
		if err != nil {
			s.internal(w, "api_key_list", err)
			return
		}
		send(w, 200, map[string]any{"items": items, "scopes": auth.SupportedAPIKeyScopes})
	case http.MethodPost:
		var in auth.APIKeyInput
		if !decodeJSON(w, r, &in) {
			return
		}
		created, err := s.auth.CreateAPIKey(r.Context(), in)
		if err != nil {
			s.apiKeyError(w, err)
			return
		}
		// The token is shown exactly once and never stored in cleartext.
		send(w, 201, created)
	default:
		w.Header().Set("Allow", "GET, POST")
		fail(w, 405, "method_not_allowed", "Use GET to list keys or POST to create one.")
	}
}

func (s *Server) apiKey(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		fail(w, 400, "invalid_api_key", "Choose a saved API key.")
		return
	}
	if err := s.auth.RevokeAPIKey(r.Context(), id); err != nil {
		s.apiKeyError(w, err)
		return
	}
	send(w, 200, map[string]bool{"revoked": true})
}

func (s *Server) apiKeyError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrAPIKeyName), errors.Is(err, auth.ErrAPIKeyScope), errors.Is(err, auth.ErrAPIKeyExpiry):
		fail(w, 400, "validation_error", err.Error())
	case errors.Is(err, auth.ErrAPIKeyLimit):
		fail(w, 409, "api_key_limit", err.Error())
	case errors.Is(err, auth.ErrAPIKeyAbsent):
		fail(w, 404, "api_key_not_found", err.Error())
	default:
		s.internal(w, "api_key", err)
	}
}
