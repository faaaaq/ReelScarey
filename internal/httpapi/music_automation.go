package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/faaaaq/ReelScarey/internal/music"
)

// musicAutomationService is satisfied by *music.Service. Asserting it locally
// keeps this file independent of the MusicService interface in server.go while
// still reporting a clear 503 when the concrete service is absent.
type musicAutomationService interface {
	GetPreferences(context.Context) (music.MusicPreferences, error)
	SavePreferences(context.Context, music.MusicPreferences) (music.MusicPreferences, error)
	WantedAlbums(context.Context, int) (music.WantedAlbumPage, error)
	GetSchedule(context.Context) (music.MusicSchedule, error)
	SaveSchedule(context.Context, music.MusicSchedule) (music.MusicSchedule, error)
}

// musicScheduleRunner is optional until a concrete runner is wired.
type musicScheduleRunner interface {
	RunSchedule(context.Context) error
}

func (s *Server) musicAutomation() (musicAutomationService, bool) {
	if s.music == nil {
		return nil, false
	}
	service, ok := s.music.(musicAutomationService)
	return service, ok
}

func (s *Server) musicAutomationError(w http.ResponseWriter, err error) {
	if errors.Is(err, music.ErrRevisionConflict) {
		fail(w, http.StatusConflict, "music_revision_conflict", err.Error())
		return
	}
	s.musicError(w, err)
}

// musicPreferences serves GET and PUT /api/v1/library/music/preferences.
func (s *Server) musicPreferences(w http.ResponseWriter, r *http.Request) {
	service, ok := s.musicAutomation()
	if !ok {
		fail(w, http.StatusServiceUnavailable, "music_unavailable", "The music preferences are unavailable.")
		return
	}
	switch r.Method {
	case http.MethodGet:
		prefs, err := service.GetPreferences(r.Context())
		if err != nil {
			s.musicAutomationError(w, err)
			return
		}
		send(w, http.StatusOK, prefs)
	case http.MethodPut:
		var body music.MusicPreferences
		if !decodeJSON(w, r, &body) {
			return
		}
		prefs, err := service.SavePreferences(r.Context(), body)
		if err != nil {
			s.musicAutomationError(w, err)
			return
		}
		send(w, http.StatusOK, prefs)
	default:
		w.Header().Set("Allow", "GET, PUT")
		fail(w, http.StatusMethodNotAllowed, "method_not_allowed", "Use GET or PUT.")
	}
}

// musicWantedAlbums serves GET /api/v1/library/music/wanted?page=.
func (s *Server) musicWantedAlbums(w http.ResponseWriter, r *http.Request) {
	service, ok := s.musicAutomation()
	if !ok {
		fail(w, http.StatusServiceUnavailable, "music_unavailable", "The music wanted list is unavailable.")
		return
	}
	page, valid := pageNumber(r, 100000)
	if !valid {
		fail(w, http.StatusBadRequest, "invalid_music", "Choose a valid page.")
		return
	}
	wanted, err := service.WantedAlbums(r.Context(), page)
	if err != nil {
		s.musicAutomationError(w, err)
		return
	}
	send(w, http.StatusOK, wanted)
}

// musicSchedule serves GET and PUT /api/v1/library/music/schedule.
func (s *Server) musicSchedule(w http.ResponseWriter, r *http.Request) {
	service, ok := s.musicAutomation()
	if !ok {
		fail(w, http.StatusServiceUnavailable, "music_unavailable", "The music schedule is unavailable.")
		return
	}
	switch r.Method {
	case http.MethodGet:
		schedule, err := service.GetSchedule(r.Context())
		if err != nil {
			s.musicAutomationError(w, err)
			return
		}
		send(w, http.StatusOK, schedule)
	case http.MethodPut:
		var body music.MusicSchedule
		if !decodeJSON(w, r, &body) {
			return
		}
		schedule, err := service.SaveSchedule(r.Context(), body)
		if err != nil {
			s.musicAutomationError(w, err)
			return
		}
		send(w, http.StatusOK, schedule)
	default:
		w.Header().Set("Allow", "GET, PUT")
		fail(w, http.StatusMethodNotAllowed, "method_not_allowed", "Use GET or PUT.")
	}
}

// musicScheduleRun serves POST /api/v1/library/music/schedule/run. The run
// action is wired only when the music service exposes
// RunSchedule(context.Context) error; otherwise it fails explicitly.
func (s *Server) musicScheduleRun(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		fail(w, http.StatusServiceUnavailable, "music_unavailable", "The music schedule is unavailable.")
		return
	}
	var body struct{}
	if !decodeJSON(w, r, &body) {
		return
	}
	runner, ok := s.music.(musicScheduleRunner)
	if !ok {
		fail(w, http.StatusServiceUnavailable, "music_schedule_unavailable", "The music schedule runner is not wired. Add RunSchedule(context.Context) error to the music service to enable it.")
		return
	}
	if err := runner.RunSchedule(r.Context()); err != nil {
		s.musicAutomationError(w, err)
		return
	}
	send(w, http.StatusAccepted, map[string]any{"started": true})
}
