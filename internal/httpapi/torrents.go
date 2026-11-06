package httpapi

import (
	"net/http"
)

func (s *Server) torrents(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.torrentsList(w, r)
	case http.MethodPost:
		s.torrentAdd(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		fail(w, 405, "method_not_allowed", "Use GET or POST.")
	}
}

func (s *Server) torrentsList(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	out, err := s.transfer.Torrents(r.Context())
	if err != nil {
		s.transferError(w, err)
		return
	}
	send(w, 200, out)
}

func (s *Server) torrentDetail(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	out, err := s.transfer.Torrent(r.Context(), r.PathValue("hash"))
	if err != nil {
		s.transferError(w, err)
		return
	}
	send(w, 200, out)
}

func (s *Server) torrentAddMagnet(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	var in struct {
		Magnet string `json:"magnet"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if err := s.transfer.AddManualTorrent(r.Context(), in.Magnet, nil); err != nil {
		s.transferError(w, err)
		return
	}
	send(w, 202, map[string]bool{"added": true})
}

func (s *Server) torrentAddUpload(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	data, ok := readTorrentBody(w, r)
	if !ok {
		return
	}
	if err := s.transfer.AddManualTorrent(r.Context(), "", data); err != nil {
		s.transferError(w, err)
		return
	}
	send(w, 202, map[string]bool{"added": true})
}

func (s *Server) torrentAction(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	var in struct {
		DeleteFiles bool `json:"delete_files"`
		Reviewed    bool `json:"reviewed"`
	}
	if r.ContentLength != 0 {
		if !decodeJSON(w, r, &in) {
			return
		}
	}
	if err := s.transfer.TorrentAction(r.Context(), r.PathValue("hash"), r.PathValue("action"), in.DeleteFiles, in.Reviewed); err != nil {
		s.transferError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// torrentAdd keeps one POST route for the two explicit manual inputs.
func (s *Server) torrentAdd(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Content-Type") == "application/x-bittorrent" {
		s.torrentAddUpload(w, r)
		return
	}
	s.torrentAddMagnet(w, r)
}
