package httpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/faaaaq/ReelScarey/internal/tidal"
)

// TIDALProvider is the narrow provider surface the settings UI needs:
// availability/login status and the interactive device-flow login. Search
// and download stay inside the music acquisition chain, which routes by
// ownership, so this interface never exposes them.
type TIDALProvider interface {
	Status(ctx context.Context) tidal.Status
	Verify(ctx context.Context) tidal.Status
	SetQuality(quality string) error
	LoginBegin(ctx context.Context) error
	LoginStatus(ctx context.Context) (tidal.LoginState, error)
}

func tidalDetail(err error) string {
	value := strings.TrimSpace(err.Error())
	if len(value) > 300 {
		value = value[:300]
	}
	return value
}

// tidalStatus distinguishes provider installation, device authorization,
// credential-file presence and a real remote session check. A token file is
// only a reason to verify; it is never sufficient to claim a connection.
func (s *Server) tidalStatus(w http.ResponseWriter, r *http.Request) {
	if s.tidal == nil {
		send(w, 200, tidal.Status{
			Available:         false,
			VerificationState: "unavailable",
			Detail:            "The TIDAL provider is not installed. Music acquisition uses the default source until it is configured.",
		})
		return
	}
	current := s.tidal.Status(r.Context())
	if !current.Available {
		current.LoggedIn = false
		current.VerificationState = "unavailable"
		send(w, 200, current)
		return
	}
	if login, err := s.tidal.LoginStatus(r.Context()); err == nil {
		current.Login = &login
	} else if current.Detail == "" {
		current.Detail = "The provider is installed, but its device-authorization state could not be read: " + tidalDetail(err)
	}
	if current.TokenPresent {
		verified := s.tidal.Verify(r.Context())
		verified.TokenPresent = current.TokenPresent
		verified.Login = current.Login
		if verified.Quality == "" {
			verified.Quality = current.Quality
		}
		if verified.VerificationState == "" {
			verified.VerificationState = "failed"
		}
		if verified.VerificationState != "verified" {
			verified.LoggedIn = false
		}
		current = verified
	} else {
		current.LoggedIn = false
		current.VerificationState = "not_checked"
		if current.Detail == "" {
			current.Detail = "No TIDAL credential file is stored. Connect TIDAL to authorize this server."
		}
	}
	send(w, 200, current)
}

// tidalLogin starts TIDAL's device login (POST) and reports the current
// login state (GET). The device link and code travel in the state lines so
// the owner can finish the login in a browser.
func (s *Server) tidalLogin(w http.ResponseWriter, r *http.Request) {
	if s.tidal == nil {
		fail(w, 503, "tidal_unavailable", "The TIDAL provider is not installed.")
		return
	}
	switch r.Method {
	case http.MethodGet:
		state, err := s.tidal.LoginStatus(r.Context())
		if err != nil {
			fail(w, 500, "tidal_login_status_failed", tidalDetail(err))
			return
		}
		send(w, 200, state)
	case http.MethodPost:
		if err := s.tidal.LoginBegin(r.Context()); err != nil {
			fail(w, 500, "tidal_login_begin_failed", tidalDetail(err))
			return
		}
		state, err := s.tidal.LoginStatus(r.Context())
		if err != nil {
			fail(w, 500, "tidal_login_status_failed", tidalDetail(err))
			return
		}
		send(w, 202, state)
	default:
		w.Header().Set("Allow", "GET, POST")
		fail(w, 405, "method_not_allowed", "Use GET or POST.")
	}
}
