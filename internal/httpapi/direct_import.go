package httpapi

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/faaaaq/ReelScarey/internal/transfer"
)

// Direct existing-file import: the owner points at one video inside a
// configured library root for the selected movie/episode. Browsing is
// read-only and bounded; importing keeps the reviewed confirmation and the
// shared verification worker.
func (s *Server) existingDirect(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.existingDirectBrowse(w, r)
	case http.MethodPost:
		s.existingDirectImport(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		fail(w, 405, "method_not_allowed", "Use GET or POST.")
	}
}

func (s *Server) existingDirectBrowse(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	kind := r.URL.Query().Get("kind")
	if kind != "movie" && kind != "series" {
		fail(w, 400, "invalid_scan", "Choose movies or TV shows.")
		return
	}
	collection := r.URL.Query().Get("collection")
	dir := r.URL.Query().Get("dir")
	if len(dir) > 2048 {
		fail(w, 400, "unsafe_path", "Choose a folder below a configured library root.")
		return
	}
	out, e := s.transfer.DirectImportBrowse(r.Context(), kind, collection, dir)
	if e != nil {
		s.transferError(w, e)
		return
	}
	send(w, 200, out)
}

func (s *Server) existingDirectImport(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	var in transfer.DirectImportRequest
	if !decodeBoundedJSON(w, r, &in, 16<<10) {
		return
	}
	if len(in.Episodes) > 20 || len(in.Relative) > 2048 {
		fail(w, 400, "invalid_mapping", "Review the title and episode match before importing this file.")
		return
	}
	out, e := s.transfer.QueueDirectImport(r.Context(), in)
	if e != nil {
		s.transferError(w, e)
		return
	}
	send(w, 202, out)
}

// Browser-picked import: the owner's native file picker supplies one video,
// which ReelScarey copies into the title's library folder and registers
// through the same reviewed flow. The copy is explicit, bounded and never
// overwrites anything.
func (s *Server) existingUpload(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		fail(w, 405, "method_not_allowed", "Use POST.")
		return
	}
	kind := r.URL.Query().Get("kind")
	if kind != "movie" && kind != "series" {
		fail(w, 400, "invalid_scan", "Choose movies or TV shows.")
		return
	}
	mediaID, err := strconv.ParseInt(r.URL.Query().Get("media_id"), 10, 64)
	if err != nil || mediaID < 1 {
		fail(w, 400, "invalid_mapping", "Choose a catalog title for this file.")
		return
	}
	in := transfer.DirectUploadRequest{
		Kind:       kind,
		Collection: r.URL.Query().Get("collection"),
		MediaID:    mediaID,
		Name:       r.URL.Query().Get("name"),
		Reviewed:   true,
	}
	if episodes := r.URL.Query().Get("episode_ids"); episodes != "" {
		for _, field := range strings.Split(episodes, ",") {
			id, err := strconv.ParseInt(strings.TrimSpace(field), 10, 64)
			if err != nil || id < 1 {
				fail(w, 400, "invalid_mapping", "Review the title and episode match before importing this file.")
				return
			}
			in.Episodes = append(in.Episodes, id)
		}
	}
	if len(in.Episodes) > 20 {
		fail(w, 400, "invalid_mapping", "Review the title and episode match before importing this file.")
		return
	}
	// Long transfers: the read deadline covers a slow, large copy instead of
	// the server's default idle timeout.
	c := http.NewResponseController(w)
	_ = c.SetReadDeadline(time.Now().Add(24 * time.Hour))
	_ = c.SetWriteDeadline(time.Now().Add(24 * time.Hour))
	r.Body = http.MaxBytesReader(w, r.Body, transfer.MaxDirectUploadBytes+1)
	out, e := s.transfer.DirectUpload(r.Context(), in, r.Body)
	if e != nil {
		s.transferError(w, e)
		return
	}
	send(w, 202, out)
}

// existingUploadPrepare resolves (and creates when missing) the destination
// folder for a browser-picked copy so the interface can show the exact path
// for confirmation before the upload starts.
func (s *Server) existingUploadPrepare(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		fail(w, 405, "method_not_allowed", "Use POST.")
		return
	}
	var in struct {
		Kind       string `json:"kind"`
		Collection string `json:"collection"`
		MediaID    int64  `json:"media_id"`
	}
	if !decodeBoundedJSON(w, r, &in, 4<<10) {
		return
	}
	if in.Kind != "movie" && in.Kind != "series" {
		fail(w, 400, "invalid_scan", "Choose movies or TV shows.")
		return
	}
	destination, e := s.transfer.DirectUploadPrepare(r.Context(), in.Kind, in.Collection, in.MediaID)
	if e != nil {
		s.transferError(w, e)
		return
	}
	send(w, 200, map[string]string{"destination": destination})
}

// existingReattach moves an already-registered file between catalog titles:
// the registration is re-parented and the reviewed organization plan moves
// and renames the file into the new title's canonical layout.
func (s *Server) existingReattach(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		fail(w, 405, "method_not_allowed", "Use POST.")
		return
	}
	var in transfer.ReattachRequest
	if !decodeBoundedJSON(w, r, &in, 16<<10) {
		return
	}
	if len(in.Episodes) > 20 || len(in.Relative) > 2048 {
		fail(w, 400, "invalid_mapping", "Review the title and episode match before re-attaching this file.")
		return
	}
	plan, e := s.transfer.ReattachFile(r.Context(), in)
	if e != nil {
		s.transferError(w, e)
		return
	}
	send(w, 202, plan)
}
