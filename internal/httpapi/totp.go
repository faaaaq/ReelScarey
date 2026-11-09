package httpapi

import (
	"errors"
	"net/http"

	"github.com/faaaaq/ReelScarey/internal/auth"
)

func (s *Server) totpStatus(w http.ResponseWriter, r *http.Request) {
	if s.totp == nil {
		fail(w, 503, "totp_unavailable", "Two-factor authentication is unavailable.")
		return
	}
	status, err := s.totp.Status(r.Context())
	if err != nil {
		s.internal(w, "totp_status", err)
		return
	}
	send(w, 200, status)
}

func (s *Server) totpProvision(w http.ResponseWriter, r *http.Request) {
	if s.totp == nil {
		fail(w, 503, "totp_unavailable", "Two-factor authentication is unavailable.")
		return
	}
	session := r.Context().Value(sessionKey{}).(auth.Session)
	provision, err := s.totp.Provision(r.Context(), session.Username)
	if err != nil {
		s.totpError(w, err)
		return
	}
	send(w, 200, provision)
}

func (s *Server) totpConfirm(w http.ResponseWriter, r *http.Request) {
	if s.totp == nil {
		fail(w, 503, "totp_unavailable", "Two-factor authentication is unavailable.")
		return
	}
	var body struct {
		Code string `json:"code"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	codes, err := s.totp.Confirm(r.Context(), body.Code)
	if err != nil {
		s.totpError(w, err)
		return
	}
	send(w, 200, map[string]any{"enabled": true, "recovery_codes": codes})
}

func (s *Server) totpDisable(w http.ResponseWriter, r *http.Request) {
	if s.totp == nil {
		fail(w, 503, "totp_unavailable", "Two-factor authentication is unavailable.")
		return
	}
	var body struct {
		Code string `json:"code"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := s.totp.Verify(r.Context(), body.Code); err != nil {
		if recoveryErr := s.totp.VerifyRecoveryCode(r.Context(), body.Code); recoveryErr != nil {
			s.totpError(w, auth.ErrTOTPCode)
			return
		}
	}
	if err := s.totp.Disable(r.Context()); err != nil {
		s.internal(w, "totp_disable", err)
		return
	}
	send(w, 200, map[string]bool{"enabled": false})
}

func (s *Server) totpError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrTOTPCode):
		fail(w, 401, "invalid_totp", err.Error())
	case errors.Is(err, auth.ErrTOTPEnabled):
		fail(w, 409, "totp_enabled", err.Error())
	case errors.Is(err, auth.ErrTOTPNotEnabled), errors.Is(err, auth.ErrTOTPNotProvisioned):
		fail(w, 409, "totp_not_ready", err.Error())
	case errors.Is(err, auth.ErrTOTPRecoveryCode), errors.Is(err, auth.ErrTOTPInput):
		fail(w, 400, "invalid_totp_input", err.Error())
	default:
		s.internal(w, "totp", err)
	}
}
