package httpapi

import (
	"database/sql"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"

	"github.com/faaaaq/ReelScarey/internal/automation"
	"github.com/faaaaq/ReelScarey/internal/quality"
)

func (s *Server) automationReady(w http.ResponseWriter) bool {
	if s.automation == nil {
		fail(w, 503, "automation_unavailable", "Automation is unavailable.")
		return false
	}
	return true
}
func (s *Server) automationError(w http.ResponseWriter, err error) {
	var p *automation.Error
	if errors.As(err, &p) {
		status := 400
		if p.Code != "invalid_automation" {
			status = 409
		}
		fail(w, status, p.Code, p.Message)
		return
	}
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, 404, "not_found", "This automation item does not exist.")
		return
	}
	s.internal(w, "automation", err)
}
func (s *Server) automationSettings(w http.ResponseWriter, r *http.Request) {
	if !s.automationReady(w) {
		return
	}
	if r.Method == http.MethodGet {
		v, e := s.automation.Get(r.Context())
		if e != nil {
			s.automationError(w, e)
			return
		}
		send(w, 200, v)
		return
	}
	if r.Method == http.MethodPut {
		var in automation.Update
		if !decodeJSON(w, r, &in) {
			return
		}
		v, e := s.automation.Update(r.Context(), in)
		if e != nil {
			s.automationError(w, e)
			return
		}
		send(w, 200, v)
		return
	}
	w.Header().Set("Allow", "GET, PUT")
	fail(w, 405, "method_not_allowed", "Use GET or PUT.")
}
func (s *Server) automationRequest(w http.ResponseWriter, r *http.Request) {
	if !s.automationReady(w) {
		return
	}
	var in struct{}
	if !decodeJSON(w, r, &in) {
		return
	}
	if e := s.automation.Request(r.Context()); e != nil {
		s.automationError(w, e)
		return
	}
	send(w, 202, map[string]bool{"requested": true})
}
func (s *Server) automationRuns(w http.ResponseWriter, r *http.Request) {
	if !s.automationReady(w) {
		return
	}
	v, e := s.automation.Runs(r.Context())
	if e != nil {
		s.automationError(w, e)
		return
	}
	send(w, 200, map[string]any{"items": v})
}
func (s *Server) automationResults(w http.ResponseWriter, r *http.Request) {
	if !s.automationReady(w) {
		return
	}
	id := r.PathValue("id")
	_, parseErr := hex.DecodeString(id)
	if len(id) != 32 || parseErr != nil {
		fail(w, 400, "invalid_id", "Choose a search run.")
		return
	}
	v, e := s.automation.Results(r.Context(), id)
	if e != nil {
		s.automationError(w, e)
		return
	}
	send(w, 200, map[string]any{"items": v})
}
func (s *Server) automationItems(w http.ResponseWriter, r *http.Request) {
	if !s.automationReady(w) {
		return
	}
	page, ok := pageNumber(r, 100000)
	if !ok {
		fail(w, 400, "invalid_page", "Choose a valid page.")
		return
	}
	v, e := s.automation.Assignments(r.Context(), page)
	if e != nil {
		s.automationError(w, e)
		return
	}
	send(w, 200, v)
}
func (s *Server) automationAssign(w http.ResponseWriter, r *http.Request) {
	if !s.automationReady(w) {
		return
	}
	id, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if e != nil || id < 1 {
		fail(w, 400, "invalid_id", "Choose a library item.")
		return
	}
	var in automation.AssignmentUpdate
	if !decodeJSON(w, r, &in) {
		return
	}
	if e = s.automation.Assign(r.Context(), id, in); e != nil {
		s.automationError(w, e)
		return
	}
	send(w, 200, map[string]bool{"saved": true})
}
func (s *Server) seriesReleaseRules(w http.ResponseWriter, r *http.Request) {
	if !s.automationReady(w) {
		return
	}
	id, ok := tvID(w, r)
	if !ok {
		return
	}
	var out automation.SeriesRules
	var e error
	switch r.Method {
	case http.MethodGet:
		out, e = s.automation.SeriesRules(r.Context(), id)
	case http.MethodPut:
		var in automation.SeriesRules
		if !decodeJSON(w, r, &in) {
			return
		}
		out, e = s.automation.SaveSeriesRules(r.Context(), id, in)
	default:
		w.Header().Set("Allow", "GET, PUT")
		fail(w, 405, "method_not_allowed", "Use GET or PUT.")
		return
	}
	if e != nil {
		s.automationError(w, e)
		return
	}
	send(w, 200, out)
}
func (s *Server) qualityProfiles(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		v, e := quality.New(s.db).List(r.Context())
		if e != nil {
			s.internal(w, "quality_profiles", e)
			return
		}
		send(w, 200, map[string]any{"items": v})
		return
	}
	if r.Method == http.MethodPost {
		s.saveQualityProfile(w, r, 0)
		return
	}
	w.Header().Set("Allow", "GET, POST")
	fail(w, 405, "method_not_allowed", "Use GET or POST.")
}
func (s *Server) qualityProfileUpdate(w http.ResponseWriter, r *http.Request) {
	id, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if e != nil || id < 1 {
		fail(w, 400, "invalid_id", "Choose a quality profile.")
		return
	}
	s.saveQualityProfile(w, r, id)
}
func (s *Server) saveQualityProfile(w http.ResponseWriter, r *http.Request, id int64) {
	var in quality.Profile
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.ID != 0 && in.ID != id {
		fail(w, 400, "invalid_profile", "Profile IDs must match.")
		return
	}
	in.ID = id
	if e := quality.Validate(in); e != nil {
		fail(w, 400, "invalid_profile", e.Error())
		return
	}
	v, e := quality.New(s.db).Save(r.Context(), in)
	if errors.Is(e, quality.ErrConflict) {
		fail(w, 409, "revision_conflict", e.Error())
		return
	}
	if e != nil {
		var invalid *quality.ValidationError
		if errors.As(e, &invalid) {
			fail(w, 400, "invalid_profile", invalid.Error())
			return
		}
		s.internal(w, "quality_profile_save", e)
		return
	}
	status := 200
	if id == 0 {
		status = 201
	}
	send(w, status, v)
}
