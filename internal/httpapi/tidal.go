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

// tidalStatus reports whether the TIDAL provider is installed, logged in and
// which quality it requests. When the provider is not configured the response
// says so honestly instead of implying a hidden feature.
func (s *Server) tidalStatus(w http.ResponseWriter, r *http.Request) {
	if s.tidal == nil {
		send(w, 200, tidal.Status{
			Available: false,
			Detail:    "The TIDAL provider is not installed. Music acquisition uses the default source until it is configured.",
		})
		return
	}
	send(w, 200, s.tidal.Status(r.Context()))
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
