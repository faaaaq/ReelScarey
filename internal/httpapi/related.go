package httpapi

import (
	"database/sql"
	"errors"
	"github.com/faaaaq/ReelScarey/internal/library"
	"net/http"
)

func (s *Server) relatedTitles(w http.ResponseWriter, r *http.Request) {
	id, ok := tvID(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, e := s.library.Related(r.Context(), id)
		if e != nil {
			if errors.Is(e, sql.ErrNoRows) {
				fail(w, 404, "not_found", "Title not found.")
			} else {
				s.internal(w, "related_titles", e)
			}
			return
		}
		send(w, 200, map[string]any{"items": items})
	case http.MethodPost, http.MethodDelete:
		var in struct {
			OtherID int64 `json:"other_id"`
		}
		if !decodeJSON(w, r, &in) {
			return
		}
		if in.OtherID <= 0 || in.OtherID == id {
			fail(w, 400, "invalid_title", "Choose a different saved title.")
			return
		}
		var e error
		if r.Method == http.MethodPost {
			e = s.library.LinkRelated(r.Context(), id, in.OtherID)
		} else {
			e = s.library.UnlinkRelated(r.Context(), id, in.OtherID)
		}
		if e != nil {
			if errors.Is(e, sql.ErrNoRows) {
				fail(w, 404, "not_found", "Title not found.")
			} else if errors.Is(e, library.ErrRelatedLimit) {
				fail(w, 400, "related_limit", e.Error())
			} else {
				s.internal(w, "related_save", e)
			}
			return
		}
		send(w, 200, map[string]bool{"saved": true})
	default:
		w.Header().Set("Allow", "GET, POST, DELETE")
		fail(w, 405, "method_not_allowed", "Use GET, POST or DELETE.")
	}
}
func (s *Server) franchiseTitles(w http.ResponseWriter, r *http.Request) {
	id, ok := tvID(w, r)
	if !ok {
		return
	}
	item, e := s.library.Item(r.Context(), id)
	if errors.Is(e, sql.ErrNoRows) {
		fail(w, 404, "not_found", "Title not found.")
		return
	}
	if e != nil {
		s.internal(w, "franchise_item", e)
		return
	}
	found, e := s.franchises.FindFranchise(r.Context(), item.Kind, item.TMDbID)
	if e != nil {
		s.providerError(w, e)
		return
	}
	type result struct {
		Kind        string `json:"kind"`
		TMDbID      int64  `json:"tmdb_id"`
		Title       string `json:"title"`
		WikidataID  string `json:"wikidata_id"`
		FranchiseID string `json:"franchise_id"`
		Franchise   string `json:"franchise"`
		LibraryID   int64  `json:"library_id"`
		Status      string `json:"status"`
	}
	items := make([]result, 0, len(found.Items))
	for _, v := range found.Items {
		out := result{Kind: v.Kind, TMDbID: v.TMDbID, Title: v.Title, WikidataID: v.WikidataID, FranchiseID: v.FranchiseID, Franchise: v.Franchise}
		var local library.Item
		var err error
		if v.Kind == "movie" {
			local, err = s.library.Movie(r.Context(), v.TMDbID)
		} else {
			local, err = s.library.Series(r.Context(), v.TMDbID)
		}
		if err == nil {
			out.LibraryID = local.ID
			out.Status = local.Status
		} else if !errors.Is(err, sql.ErrNoRows) {
			s.internal(w, "franchise_library", err)
			return
		}
		items = append(items, out)
	}
	send(w, 200, map[string]any{"source": "Wikidata", "items": items, "truncated": found.Truncated})
}
