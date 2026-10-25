package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/faaaaq/ReelScarey/internal/library"
	"io"
	"mime"
	"net/http"
	"time"
)

func (s *Server) animeSettings(w http.ResponseWriter, r *http.Request) {
	id, ok := tvID(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	var out library.AnimeSettings
	var e error
	switch r.Method {
	case http.MethodGet:
		out, e = s.library.Anime(ctx, id)
	case http.MethodPut:
		mediaType, _, parseError := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if parseError != nil || mediaType != "application/json" {
			fail(w, 415, "unsupported_media_type", "Use application/json.")
			return
		}
		// A complete long-running show's explicit aliases require more room than
		// ordinary mutations. Fields, trailing JSON and the 5000-entry bound stay strict.
		r.Body = http.MaxBytesReader(w, r.Body, 512<<10)
		var in library.AnimeUpdate
		d := json.NewDecoder(r.Body)
		d.DisallowUnknownFields()
		invalid := func(err error) {
			var large *http.MaxBytesError
			if errors.As(err, &large) {
				fail(w, 413, "body_too_large", "Anime settings must be at most 512 KiB.")
			} else {
				fail(w, 400, "invalid_json", "Send one documented anime settings object, at most 512 KiB.")
			}
		}
		if err := d.Decode(&in); err != nil {
			invalid(err)
			return
		}
		var extra any
		if err := d.Decode(&extra); err != io.EOF {
			invalid(err)
			return
		}
		out, e = s.library.SaveAnime(ctx, id, in)
	default:
		w.Header().Set("Allow", "GET, PUT")
		fail(w, 405, "method_not_allowed", "Use GET or PUT.")
		return
	}
	if errors.Is(e, library.ErrAnimeInput) {
		fail(w, 400, "invalid_anime", e.Error())
		return
	}
	if errors.Is(e, library.ErrAnimeRevision) {
		fail(w, 409, "revision_conflict", e.Error())
		return
	}
	if errors.Is(e, sql.ErrNoRows) {
		fail(w, 404, "not_found", "Choose a saved movie or show.")
		return
	}
	if e != nil {
		s.internal(w, "anime_settings", e)
		return
	}
	send(w, 200, out)
}
