package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/faaaaq/ReelScarey/internal/vpn"
)

// The VPN gates the built-in torrent client, so the endpoints live with the
// download client controls. The private key is never returned.
func (s *Server) vpnMullvad(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	if s.vpn == nil {
		fail(w, 503, "vpn_unavailable", "The VPN service is not available in this deployment.")
		return
	}
	switch r.Method {
	case http.MethodGet:
		out, e := s.vpn.Status(r.Context())
		if e != nil {
			s.vpnError(w, e)
			return
		}
		send(w, 200, out)
	case http.MethodPut:
		var in vpn.Update
		if !decodeJSON(w, r, &in) {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
		defer cancel()
		_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(95 * time.Second))
		out, e := s.vpn.Update(ctx, in)
		if e != nil {
			s.vpnError(w, e)
			return
		}
		send(w, 200, out)
	default:
		w.Header().Set("Allow", "GET, PUT")
		fail(w, 405, "method_not_allowed", "Use GET or PUT.")
	}
}

func (s *Server) vpnError(w http.ResponseWriter, e error) {
	var problem *vpn.Error
	if !errors.As(e, &problem) {
		fail(w, 500, "vpn_unavailable", "The VPN service could not complete the request.")
		return
	}
	switch problem.Code {
	case "revision_conflict":
		fail(w, 409, problem.Code, problem.Message)
	case "vpn_start", "vpn_timeout", "vpn_missing":
		fail(w, 503, problem.Code, problem.Message)
	default:
		fail(w, 400, problem.Code, problem.Message)
	}
}
