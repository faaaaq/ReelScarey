package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/faaaaq/ReelScarey/internal/library"
	"github.com/faaaaq/ReelScarey/internal/quality"
)

func (s *Server) animeDubError(w http.ResponseWriter, err error) {
	if errors.Is(err, library.ErrAnimePreferencesRevision) {
		fail(w, 409, "revision_conflict", err.Error())
		return
	}
	if errors.Is(err, library.ErrAnimePreferences) || errors.Is(err, quality.ErrDubRule) {
		fail(w, 400, "invalid_anime_dub_rule", err.Error())
		return
	}
	s.internal(w, "anime_dub", err)
}

func (s *Server) animePreferences(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		prefs, err := s.library.AnimePreferences(r.Context())
		if err != nil {
			s.internal(w, "anime_preferences", err)
			return
		}
		send(w, 200, prefs)
	case http.MethodPut:
		var body struct {
			DubRule  string `json:"dub_rule"`
			Revision int    `json:"revision"`
		}
		if !decodeJSON(w, r, &body) {
			return
		}
		prefs, err := s.library.SaveAnimePreferences(r.Context(), library.AnimePreferences{DubRule: body.DubRule, Revision: body.Revision})
		if err != nil {
			s.animeDubError(w, err)
			return
		}
		send(w, 200, prefs)
	default:
		w.Header().Set("Allow", "GET, PUT")
		fail(w, 405, "method_not_allowed", "Use GET or PUT.")
	}
}

func (s *Server) animeDubTest(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Rule  string `json:"rule"`
		Title string `json:"title"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	title := strings.TrimSpace(body.Title)
	if title == "" {
		fail(w, 400, "invalid_anime_dub_rule", "Enter a release title to test.")
		return
	}
	ruleText := body.Rule
	if strings.TrimSpace(ruleText) == "" {
		prefs, err := s.library.AnimePreferences(r.Context())
		if err != nil {
			s.internal(w, "anime_preferences", err)
			return
		}
		ruleText = prefs.DubRule
	}
	rule, err := quality.ParseDubRule(ruleText)
	if err != nil {
		s.animeDubError(w, err)
		return
	}
	send(w, 200, map[string]bool{"matches": rule.Matches(title)})
}

func (s *Server) animeEnglishDubAll(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Enabled  bool `json:"enabled"`
		Reviewed bool `json:"reviewed"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if !body.Reviewed {
		fail(w, 400, "review_required", "Review the change before applying it to every Anime show.")
		return
	}
	shows, err := s.library.SetAllAnimeEnglishDub(r.Context(), body.Enabled)
	if err != nil {
		s.internal(w, "anime_english_dub", err)
		return
	}
	send(w, 200, map[string]any{"shows": shows})
}
