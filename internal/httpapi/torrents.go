package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"

	"github.com/faaaaq/ReelScarey/internal/transfer"
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

// decodeTorrentJSON is decodeJSON with a 128 KiB bound: bulk selections of 500
// hashes and file selections of 10000 indexes exceed the default 16 KiB.
func decodeTorrentJSON(w http.ResponseWriter, r *http.Request, out any) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		fail(w, 415, "unsupported_media_type", "Use application/json.")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 128<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	err = decoder.Decode(out)
	if err == nil {
		var extra any
		if err = decoder.Decode(&extra); err == io.EOF {
			return true
		}
	}
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		fail(w, 413, "body_too_large", "The request body must be at most 128 KiB.")
		return false
	}
	fail(w, 400, "invalid_json", "Send one valid JSON object with only the documented fields.")
	return false
}

func (s *Server) torrentBulkAction(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	var in struct {
		Hashes           []string `json:"hashes"`
		Action           string   `json:"action"`
		Enabled          *bool    `json:"enabled"`
		Limit            *int64   `json:"limit"`
		RatioLimit       *float64 `json:"ratio_limit"`
		SeedingTimeLimit *int64   `json:"seeding_time_limit"`
		DeleteFiles      bool     `json:"delete_files"`
		Reviewed         bool     `json:"reviewed"`
	}
	if !decodeTorrentJSON(w, r, &in) {
		return
	}
	err := s.transfer.TorrentBulk(r.Context(), transfer.TorrentBulkAction{Hashes: in.Hashes, Action: in.Action, Enabled: in.Enabled, Limit: in.Limit, RatioLimit: in.RatioLimit, SeedingTimeLimit: in.SeedingTimeLimit, DeleteFiles: in.DeleteFiles, Reviewed: in.Reviewed})
	if err != nil {
		s.transferError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) torrentFilePriority(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	var in struct {
		Indexes  []int `json:"indexes"`
		Priority *int  `json:"priority"`
	}
	if !decodeTorrentJSON(w, r, &in) {
		return
	}
	if in.Priority == nil {
		fail(w, 400, "invalid_selection", "Choose a file priority.")
		return
	}
	if err := s.transfer.SetTorrentFilePriority(r.Context(), r.PathValue("hash"), in.Indexes, *in.Priority); err != nil {
		s.transferError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) torrentSpeedLimits(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	var in struct {
		AltEnabled    *bool  `json:"alt_enabled"`
		DownloadLimit *int64 `json:"download_limit"`
		UploadLimit   *int64 `json:"upload_limit"`
	}
	if !decodeTorrentJSON(w, r, &in) {
		return
	}
	if err := s.transfer.SetTorrentSpeedLimits(r.Context(), transfer.TorrentSpeedLimits{AltEnabled: in.AltEnabled, DownloadLimit: in.DownloadLimit, UploadLimit: in.UploadLimit}); err != nil {
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
