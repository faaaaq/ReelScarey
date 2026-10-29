package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/faaaaq/ReelScarey/internal/library"
)

func (s *Server) planning(w http.ResponseWriter, r *http.Request) {
	calendar := r.URL.Path == "/api/v1/calendar"
	allowed := map[string]bool{"kind": true, "status": true, "query": true, "monitored": true, "page": true}
	if calendar {
		allowed["month"] = true
	}
	values, parseErr := url.ParseQuery(r.URL.RawQuery)
	if parseErr != nil {
		fail(w, 400, "invalid_planning", library.ErrPlanningInput.Error())
		return
	}
	for k, v := range values {
		if !allowed[k] || len(v) != 1 {
			fail(w, 400, "invalid_planning", library.ErrPlanningInput.Error())
			return
		}
	}
	page, ok := pageNumber(r, 100000)
	if !ok {
		fail(w, 400, "invalid_planning", library.ErrPlanningInput.Error())
		return
	}
	monitored := values.Get("monitored")
	if monitored != "" && monitored != "all" && monitored != "monitored" {
		fail(w, 400, "invalid_planning", library.ErrPlanningInput.Error())
		return
	}
	f := library.PlanningFilter{Kind: values.Get("kind"), Status: values.Get("status"), Query: values.Get("query"), Month: values.Get("month"), OnlyMonitored: monitored != "all", Page: page}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	var out library.PlanningPage
	var e error
	if calendar {
		out, e = s.library.Calendar(ctx, f, time.Now())
	} else {
		out, e = s.library.Wanted(ctx, f, time.Now())
	}
	if errors.Is(e, library.ErrPlanningInput) {
		fail(w, 400, "invalid_planning", e.Error())
		return
	}
	if e != nil {
		s.internal(w, "planning_read", e)
		return
	}
	send(w, 200, out)
}
