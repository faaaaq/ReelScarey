package httpapi

import (
	"net/http"
	"strconv"

	"github.com/faaaaq/ReelScarey/internal/releaseguard"
)

func (s *Server) releaseBlacklist(w http.ResponseWriter, r *http.Request) {
	if s.transfer == nil {
		fail(w, 503, "transfer_unavailable", "Download management is unavailable.")
		return
	}
	entries, err := s.transfer.Blacklist(r.Context(), 500)
	if err != nil {
		s.internal(w, "release_blacklist", err)
		return
	}
	if entries == nil {
		entries = []releaseguard.Entry{}
	}
	send(w, 200, map[string]any{"items": entries})
}

func (s *Server) releaseBlacklistEntry(w http.ResponseWriter, r *http.Request) {
	if s.transfer == nil {
		fail(w, 503, "transfer_unavailable", "Download management is unavailable.")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		fail(w, 400, "invalid_release", "Choose a valid blacklist entry.")
		return
	}
	if err := s.transfer.RemoveBlacklist(r.Context(), id); err != nil {
		s.internal(w, "release_blacklist_remove", err)
		return
	}
	send(w, 200, map[string]bool{"removed": true})
}
