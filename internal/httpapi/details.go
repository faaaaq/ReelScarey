package httpapi

import (
	"encoding/json"
	"net/http"
	"time"
)

func (s *Server) itemDetails(w http.ResponseWriter, r *http.Request) {
	id, ok := tvID(w, r)
	if !ok {
		return
	}
	page, ok := pageNumber(r, 100000)
	if !ok {
		fail(w, 400, "invalid_page", "Choose a valid page.")
		return
	}
	if s.transfer == nil {
		fail(w, 503, "workflow_unavailable", "Library file information is unavailable.")
		return
	}
	out, e := s.transfer.Details(r.Context(), id, page)
	if e != nil {
		s.tvError(w, e)
		return
	}
	send(w, 200, out)
}
func (s *Server) refreshInformation(w http.ResponseWriter, r *http.Request) {
	id, ok := tvID(w, r)
	if !ok {
		return
	}
	if !decodeJSON(w, r, &struct{}{}) {
		return
	}
	if s.metadata == nil {
		fail(w, 503, "metadata_unavailable", "Movie and TV metadata is unavailable.")
		return
	}
	item, e := s.library.Item(r.Context(), id)
	if e != nil {
		s.tvError(w, e)
		return
	}
	info, e := s.metadata.Information(r.Context(), item.Kind, item.TMDbID)
	if e != nil {
		s.providerError(w, e)
		return
	}
	payload, e := json.Marshal(info)
	if e != nil {
		s.internal(w, "metadata_information", e)
		return
	}
	now := time.Now().Unix()
	if _, e = s.db.ExecContext(r.Context(), `INSERT INTO media_enrichment(media_id,payload,updated_at) VALUES(?,?,?) ON CONFLICT(media_id) DO UPDATE SET payload=excluded.payload,updated_at=excluded.updated_at`, id, string(payload), now); e != nil {
		s.internal(w, "metadata_information", e)
		return
	}
	send(w, 200, map[string]any{"information": info, "metadata_updated_at": now})
}
