package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/faaaaq/ReelScarey/internal/music"
)

// musicPlaylistService keeps this optional capability out of MusicService so
// existing music test doubles and alternate compositions remain source
// compatible. The concrete music.Service assertion is checked at compile time.
type musicPlaylistService interface {
	ListLibraryPlaylists(context.Context) (music.LibraryPlaylistList, error)
	LibraryPlaylist(context.Context, int64) (music.LibraryPlaylistDetails, error)
	CreateLibraryPlaylist(context.Context, string, *int64) (music.LibraryPlaylistDetails, error)
	RenameLibraryPlaylist(context.Context, int64, string, int) (music.LibraryPlaylistDetails, error)
	DeleteLibraryPlaylist(context.Context, int64, int) error
	AddLibraryPlaylistTrack(context.Context, int64, int64, int) (music.LibraryPlaylistDetails, error)
	RemoveLibraryPlaylistTrack(context.Context, int64, int64, int) (music.LibraryPlaylistDetails, error)
	ReorderLibraryPlaylist(context.Context, int64, []int64, int) (music.LibraryPlaylistDetails, error)
}

var _ musicPlaylistService = (*music.Service)(nil)

func (s *Server) libraryPlaylistService(w http.ResponseWriter) (musicPlaylistService, bool) {
	if s.music == nil {
		fail(w, http.StatusServiceUnavailable, "music_unavailable", "The music catalog is unavailable.")
		return nil, false
	}
	service, ok := s.music.(musicPlaylistService)
	if !ok {
		fail(w, http.StatusServiceUnavailable, "music_playlists_unavailable", "Music library playlists are unavailable.")
		return nil, false
	}
	return service, true
}

func (s *Server) musicPlaylistError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, music.ErrRevisionConflict):
		fail(w, http.StatusConflict, "music_revision_conflict", err.Error())
	case errors.Is(err, music.ErrLibraryPlaylistTrackExists):
		fail(w, http.StatusConflict, "music_playlist_track_exists", err.Error())
	default:
		s.musicError(w, err)
	}
}

func (s *Server) musicLibraryPlaylists(w http.ResponseWriter, r *http.Request) {
	service, ok := s.libraryPlaylistService(w)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := service.ListLibraryPlaylists(r.Context())
		if err != nil {
			s.musicPlaylistError(w, err)
			return
		}
		send(w, http.StatusOK, items)
	case http.MethodPost:
		var body struct {
			Name    string `json:"name"`
			TrackID *int64 `json:"track_id"`
		}
		if !decodeJSON(w, r, &body) {
			return
		}
		details, err := service.CreateLibraryPlaylist(r.Context(), body.Name, body.TrackID)
		if err != nil {
			s.musicPlaylistError(w, err)
			return
		}
		send(w, http.StatusCreated, details)
	default:
		w.Header().Set("Allow", "GET, POST")
		fail(w, http.StatusMethodNotAllowed, "method_not_allowed", "Use GET or POST.")
	}
}

func (s *Server) musicLibraryPlaylist(w http.ResponseWriter, r *http.Request) {
	service, ok := s.libraryPlaylistService(w)
	if !ok {
		return
	}
	id, ok := musicID(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		details, err := service.LibraryPlaylist(r.Context(), id)
		if err != nil {
			s.musicPlaylistError(w, err)
			return
		}
		send(w, http.StatusOK, details)
	case http.MethodPut:
		var body struct {
			Name     string `json:"name"`
			Revision int    `json:"revision"`
		}
		if !decodeJSON(w, r, &body) {
			return
		}
		details, err := service.RenameLibraryPlaylist(r.Context(), id, body.Name, body.Revision)
		if err != nil {
			s.musicPlaylistError(w, err)
			return
		}
		send(w, http.StatusOK, details)
	case http.MethodDelete:
		var body struct {
			Revision int `json:"revision"`
		}
		if !decodeJSON(w, r, &body) {
			return
		}
		if err := service.DeleteLibraryPlaylist(r.Context(), id, body.Revision); err != nil {
			s.musicPlaylistError(w, err)
			return
		}
		send(w, http.StatusOK, map[string]bool{"deleted": true})
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		fail(w, http.StatusMethodNotAllowed, "method_not_allowed", "Use GET, PUT or DELETE.")
	}
}

func (s *Server) musicLibraryPlaylistTracks(w http.ResponseWriter, r *http.Request) {
	service, ok := s.libraryPlaylistService(w)
	if !ok {
		return
	}
	id, ok := musicID(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodPost:
		var body struct {
			TrackID  int64 `json:"track_id"`
			Revision int   `json:"revision"`
		}
		if !decodeJSON(w, r, &body) {
			return
		}
		details, err := service.AddLibraryPlaylistTrack(r.Context(), id, body.TrackID, body.Revision)
		if err != nil {
			s.musicPlaylistError(w, err)
			return
		}
		send(w, http.StatusOK, details)
	case http.MethodPut:
		var body struct {
			TrackIDs []int64 `json:"track_ids"`
			Revision int     `json:"revision"`
		}
		if !decodeJSON(w, r, &body) {
			return
		}
		details, err := service.ReorderLibraryPlaylist(r.Context(), id, body.TrackIDs, body.Revision)
		if err != nil {
			s.musicPlaylistError(w, err)
			return
		}
		send(w, http.StatusOK, details)
	case http.MethodDelete:
		var body struct {
			TrackID  int64 `json:"track_id"`
			Revision int   `json:"revision"`
		}
		if !decodeJSON(w, r, &body) {
			return
		}
		details, err := service.RemoveLibraryPlaylistTrack(r.Context(), id, body.TrackID, body.Revision)
		if err != nil {
			s.musicPlaylistError(w, err)
			return
		}
		send(w, http.StatusOK, details)
	default:
		w.Header().Set("Allow", "POST, PUT, DELETE")
		fail(w, http.StatusMethodNotAllowed, "method_not_allowed", "Use POST, PUT or DELETE.")
	}
}
