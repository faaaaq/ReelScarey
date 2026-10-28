package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/faaaaq/ReelScarey/internal/auth"
	"github.com/faaaaq/ReelScarey/internal/requests"
)

func (s *Server) requestAccounts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		items, err := s.auth.RequestAccounts(r.Context())
		if err != nil {
			s.internal(w, "request_accounts", err)
			return
		}
		send(w, 200, map[string]any{"items": items})
	case http.MethodPost:
		var in struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if !decodeJSON(w, r, &in) {
			return
		}
		item, err := s.auth.CreateRequestAccount(r.Context(), in.Username, in.Password)
		if err != nil {
			if errors.Is(err, auth.ErrAccountExists) {
				fail(w, 409, "username_exists", err.Error())
			} else {
				s.authError(w, err)
			}
			return
		}
		send(w, 201, item)
	default:
		w.Header().Set("Allow", "GET, POST")
		fail(w, 405, "method_not_allowed", "Use GET or POST.")
	}
}
func (s *Server) requestAccountState(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		fail(w, 400, "invalid_account", "Choose a valid account.")
		return
	}
	var in struct {
		Disabled *bool `json:"disabled"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Disabled == nil {
		fail(w, 400, "invalid_account", "Choose whether the account is disabled.")
		return
	}
	if s.bot != nil {
		err = s.bot.DisableAccount(r.Context(), id, *in.Disabled)
	} else {
		err = s.auth.SetRequestAccountDisabled(r.Context(), id, *in.Disabled)
	}
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, 404, "not_found", "Account not found.")
		return
	} else if err != nil {
		s.internal(w, "request_account", err)
		return
	}
	send(w, 200, map[string]bool{"disabled": *in.Disabled})
}
func (s *Server) requesterRequests(w http.ResponseWriter, r *http.Request) {
	session := r.Context().Value(sessionKey{}).(auth.Session)
	if session.Role != "requester" || session.AccountID < 1 {
		fail(w, 403, "request_account_required", "Sign in with a request account.")
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := s.requests.List(r.Context(), session.AccountID)
		if err != nil {
			s.internal(w, "requests", err)
			return
		}
		send(w, 200, map[string]any{"items": items})
	case http.MethodPost:
		var in struct {
			Kind   string `json:"kind"`
			TMDbID int64  `json:"tmdb_id"`
		}
		if !decodeJSON(w, r, &in) {
			return
		}
		if in.TMDbID < 1 || (in.Kind != "movie" && in.Kind != "series") {
			fail(w, 400, "invalid_request", requests.ErrInvalid.Error())
			return
		}
		var title string
		if in.Kind == "movie" {
			v, err := s.metadata.Movie(r.Context(), in.TMDbID)
			if err != nil {
				s.providerError(w, err)
				return
			}
			if v.TMDbID != in.TMDbID {
				fail(w, 502, "provider_mismatch", "The provider returned a different title; try again.")
				return
			}
			title = v.Title
		} else {
			v, err := s.metadata.Series(r.Context(), in.TMDbID)
			if err != nil {
				s.tvError(w, err)
				return
			}
			if v.TMDbID != in.TMDbID {
				fail(w, 502, "provider_mismatch", "The provider returned a different title; try again.")
				return
			}
			title = v.Title
		}
		item, err := s.requests.Create(r.Context(), session.AccountID, in.Kind, in.TMDbID, title)
		if err != nil {
			if errors.Is(err, requests.ErrInvalid) {
				fail(w, 400, "invalid_request", err.Error())
			} else {
				s.internal(w, "request_create", err)
			}
			return
		}
		send(w, 201, item)
	default:
		w.Header().Set("Allow", "GET, POST")
		fail(w, 405, "method_not_allowed", "Use GET or POST.")
	}
}
func (s *Server) adminRequests(w http.ResponseWriter, r *http.Request) {
	items, err := s.requests.List(r.Context(), 0)
	if err != nil {
		s.internal(w, "requests", err)
		return
	}
	send(w, 200, map[string]any{"items": items})
}

func (s *Server) requesterDiscord(w http.ResponseWriter, r *http.Request) {
	session := r.Context().Value(sessionKey{}).(auth.Session)
	if session.Role != "requester" || session.AccountID < 1 {
		fail(w, 403, "request_account_required", "Sign in with a request account.")
		return
	}
	if s.discord == nil {
		fail(w, 503, "discord_unavailable", "Discord notifications are unavailable.")
		return
	}
	switch r.Method {
	case http.MethodGet:
		configured, err := s.discord.Status(r.Context(), session.AccountID)
		if err != nil {
			s.internal(w, "discord_status", err)
			return
		}
		send(w, 200, map[string]bool{"configured": configured})
	case http.MethodPut:
		var in struct {
			Webhook string `json:"webhook"`
		}
		if !decodeJSON(w, r, &in) {
			return
		}
		if err := s.discord.Save(r.Context(), session.AccountID, in.Webhook); errors.Is(err, requests.ErrWebhook) {
			fail(w, 400, "invalid_webhook", err.Error())
			return
		} else if err != nil {
			s.internal(w, "discord_save", err)
			return
		}
		send(w, 200, map[string]bool{"configured": true})
	case http.MethodDelete:
		if err := s.discord.Delete(r.Context(), session.AccountID); err != nil {
			s.internal(w, "discord_delete", err)
			return
		}
		send(w, 200, map[string]bool{"configured": false})
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		fail(w, 405, "method_not_allowed", "Use GET, PUT or DELETE.")
	}
}
func (s *Server) requesterPassword(w http.ResponseWriter, r *http.Request) {
	session := r.Context().Value(sessionKey{}).(auth.Session)
	if session.Role != "requester" || session.AccountID < 1 {
		fail(w, 403, "request_account_required", "Sign in with a request account.")
		return
	}
	var in struct {
		Current string `json:"current_password"`
		New     string `json:"new_password"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if err := s.auth.ChangeRequestPassword(r.Context(), session.AccountID, in.Current, in.New); errors.Is(err, auth.ErrUnauthorized) {
		fail(w, 400, "invalid_password", "The current password is incorrect or the account is unavailable.")
		return
	} else if err != nil {
		s.authError(w, err)
		return
	}
	s.cookie(w, r, "", 0)
	send(w, 200, map[string]bool{"changed": true})
}
func (s *Server) requesterRetryNotification(w http.ResponseWriter, r *http.Request) {
	session := r.Context().Value(sessionKey{}).(auth.Session)
	if session.Role != "requester" || session.AccountID < 1 {
		fail(w, 403, "request_account_required", "Sign in with a request account.")
		return
	}
	if s.discord == nil {
		fail(w, 503, "discord_unavailable", "Discord notifications are unavailable.")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		fail(w, 400, "invalid_request", "Choose a valid request.")
		return
	}
	var in struct{}
	if !decodeJSON(w, r, &in) {
		return
	}
	if err = s.discord.Retry(r.Context(), session.AccountID, id); errors.Is(err, requests.ErrState) {
		fail(w, 409, "notification_changed", "This notification cannot be retried.")
		return
	} else if err != nil {
		s.internal(w, "discord_retry", err)
		return
	}
	send(w, 202, map[string]bool{"queued": true})
}

func (s *Server) requesterLists(w http.ResponseWriter, r *http.Request) {
	session := r.Context().Value(sessionKey{}).(auth.Session)
	if session.Role != "requester" || session.AccountID < 1 {
		fail(w, 403, "request_account_required", "Sign in with a request account.")
		return
	}
	if s.requestLists == nil {
		fail(w, 503, "lists_unavailable", "List imports are unavailable.")
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := s.requestLists.All(r.Context(), session.AccountID)
		if err != nil {
			s.internal(w, "request_lists", err)
			return
		}
		send(w, 200, map[string]any{"items": items})
	case http.MethodPost:
		var in struct {
			Provider string `json:"provider"`
			Owner    string `json:"owner"`
			ListID   string `json:"list_id"`
			ClientID string `json:"client_id"`
		}
		if !decodeJSON(w, r, &in) {
			return
		}
		item, err := s.requestLists.Add(r.Context(), session.AccountID, in.Provider, in.Owner, in.ListID, in.ClientID)
		if errors.Is(err, requests.ErrList) {
			fail(w, 400, "invalid_list", err.Error())
			return
		} else if err != nil {
			s.internal(w, "request_list_save", err)
			return
		}
		send(w, 201, item)
	default:
		w.Header().Set("Allow", "GET, POST")
		fail(w, 405, "method_not_allowed", "Use GET or POST.")
	}
}
func (s *Server) requesterListAction(w http.ResponseWriter, r *http.Request, refresh bool) {
	session := r.Context().Value(sessionKey{}).(auth.Session)
	if session.Role != "requester" || session.AccountID < 1 {
		fail(w, 403, "request_account_required", "Sign in with a request account.")
		return
	}
	if s.requestLists == nil {
		fail(w, 503, "lists_unavailable", "List imports are unavailable.")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		fail(w, 400, "invalid_list", "Choose a valid list.")
		return
	}
	if refresh {
		var in struct{}
		if !decodeJSON(w, r, &in) {
			return
		}
		err = s.requestLists.Refresh(r.Context(), session.AccountID, id)
	} else {
		err = s.requestLists.Delete(r.Context(), session.AccountID, id)
	}
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, 404, "not_found", "List not found.")
		return
	} else if err != nil {
		s.internal(w, "request_list", err)
		return
	}
	send(w, 200, map[string]bool{"ok": true})
}
func (s *Server) requesterDeleteList(w http.ResponseWriter, r *http.Request) {
	s.requesterListAction(w, r, false)
}
func (s *Server) requesterRefreshList(w http.ResponseWriter, r *http.Request) {
	s.requesterListAction(w, r, true)
}
func (s *Server) requesterIMDbCSV(w http.ResponseWriter, r *http.Request) {
	session := r.Context().Value(sessionKey{}).(auth.Session)
	if session.Role != "requester" || session.AccountID < 1 {
		fail(w, 403, "request_account_required", "Sign in with a request account.")
		return
	}
	var in struct {
		CSV string `json:"csv"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	ids, err := requests.IMDbIDs(in.CSV)
	if err != nil {
		fail(w, 400, "invalid_imdb_csv", err.Error())
		return
	}
	items := make([]requests.Request, 0, len(ids))
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	for _, id := range ids {
		v, e := s.metadata.FindIMDb(ctx, id)
		if e != nil {
			s.providerError(w, e)
			return
		}
		items = append(items, requests.Request{Kind: v.Kind, TMDbID: v.TMDbID, Title: v.Title})
	}
	count, err := s.requests.ImportResolved(ctx, session.AccountID, items)
	if errors.Is(err, requests.ErrIMDbCSV) {
		fail(w, 400, "invalid_imdb_csv", err.Error())
		return
	} else if err != nil {
		s.internal(w, "imdb_import", err)
		return
	}
	send(w, 201, map[string]int{"added": count, "matched": len(ids)})
}

func (s *Server) requestAction(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		fail(w, 400, "invalid_request", "Choose a valid request.")
		return
	}
	var in struct{}
	if !decodeJSON(w, r, &in) {
		return
	}
	switch r.PathValue("action") {
	case "decline":
		err = s.requests.Decline(r.Context(), id)
	case "approve":
		var item requests.Request
		item, err = s.requests.Claim(r.Context(), id)
		if err == nil && item.State != "approved" {
			if item.Kind == "movie" {
				var movieErr error
				movie, e := s.metadata.Movie(r.Context(), item.TMDbID)
				if e == nil {
					if movie.TMDbID != item.TMDbID {
						fail(w, 502, "provider_mismatch", "The provider returned a different title; retry approval.")
						return
					}
					_, _, movieErr = s.library.AddMovie(r.Context(), movie, true)
				} else {
					movieErr = e
				}
				err = movieErr
			} else {
				var seriesErr error
				series, e := s.metadata.Series(r.Context(), item.TMDbID)
				if e == nil {
					if series.TMDbID != item.TMDbID {
						fail(w, 502, "provider_mismatch", "The provider returned a different title; retry approval.")
						return
					}
					_, _, seriesErr = s.library.AddSeries(r.Context(), series, true)
				} else {
					seriesErr = e
				}
				err = seriesErr
			}
			if err == nil {
				err = s.requests.Finish(r.Context(), id)
			}
		}
	default:
		fail(w, 400, "invalid_action", "Choose approve or decline.")
		return
	}
	if errors.Is(err, requests.ErrState) {
		fail(w, 409, "request_changed", err.Error())
		return
	}
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, 404, "not_found", "Request not found.")
		return
	}
	if err != nil {
		s.providerError(w, err)
		return
	}
	item, err := s.requests.Get(r.Context(), id)
	if err != nil {
		s.internal(w, "request_read", err)
		return
	}
	send(w, 200, item)
}
