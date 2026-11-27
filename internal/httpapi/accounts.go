package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"

	"github.com/faaaaq/ReelScarey/internal/auth"
	"github.com/faaaaq/ReelScarey/internal/discordbot"
)

func accountError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sql.ErrNoRows):
		fail(w, http.StatusNotFound, "not_found", "Account not found.")
	case errors.Is(err, auth.ErrAccountExists):
		fail(w, http.StatusConflict, "username_exists", err.Error())
	case errors.Is(err, auth.ErrPasswordSetupRequired):
		fail(w, http.StatusConflict, "password_setup_required", err.Error())
	case errors.Is(err, auth.ErrSubsonicProfileMissing):
		fail(w, http.StatusConflict, "subsonic_profile_missing", err.Error())
	default:
		// Reuse the existing authentication validation/rate-limit mapping.
		// The caller handles unexpected errors separately before reaching here.
	}
}

func (s *Server) accounts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		items, err := s.auth.Accounts(r.Context())
		if err != nil {
			s.internal(w, "accounts", err)
			return
		}
		send(w, http.StatusOK, map[string]any{"items": items})
	case http.MethodPost:
		var in struct {
			Username    string `json:"username"`
			Password    string `json:"password"`
			Requests    bool   `json:"requests"`
			MusicPlayer bool   `json:"music_player"`
			Subsonic    bool   `json:"subsonic"`
		}
		if !decodeJSON(w, r, &in) {
			return
		}
		item, err := s.auth.CreateAccount(r.Context(), in.Username, in.Password, in.Requests, in.MusicPlayer, in.Subsonic)
		if err != nil {
			accountError(w, err)
			if errors.Is(err, auth.ErrAccountExists) || errors.Is(err, auth.ErrPasswordSetupRequired) || errors.Is(err, auth.ErrSubsonicProfileMissing) {
				return
			}
			s.authError(w, err)
			return
		}
		send(w, http.StatusCreated, item)
	default:
		w.Header().Set("Allow", "GET, POST")
		fail(w, http.StatusMethodNotAllowed, "method_not_allowed", "Use GET or POST.")
	}
}

func accountID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		fail(w, http.StatusBadRequest, "invalid_account", "Choose a valid account.")
		return 0, false
	}
	return id, true
}

func (s *Server) accountItem(w http.ResponseWriter, r *http.Request) {
	id, ok := accountID(w, r)
	if !ok {
		return
	}
	var in struct {
		Username    string `json:"username"`
		Disabled    *bool  `json:"disabled"`
		Requests    *bool  `json:"requests"`
		MusicPlayer *bool  `json:"music_player"`
		Subsonic    *bool  `json:"subsonic"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	update := auth.AccountUpdate{Username: in.Username, Disabled: in.Disabled, Requests: in.Requests, MusicPlayer: in.MusicPlayer, Subsonic: in.Subsonic}
	// Discord workers hold this lock while checking whether a queued action is
	// still authorized. Fence status/request-grant changes against that check.
	if s.bot != nil && (in.Disabled != nil || in.Requests != nil) {
		if current, err := s.auth.AccountByID(r.Context(), id); err == nil && current.RequestAccountID > 0 {
			var item auth.Account
			err = s.bot.WithAccountLock(current.RequestAccountID, func() error {
				var updateErr error
				item, updateErr = s.auth.UpdateAccount(r.Context(), id, update)
				return updateErr
			})
			if err == nil {
				send(w, http.StatusOK, item)
				return
			}
			accountError(w, err)
			if errors.Is(err, sql.ErrNoRows) || errors.Is(err, auth.ErrAccountExists) || errors.Is(err, auth.ErrPasswordSetupRequired) || errors.Is(err, auth.ErrSubsonicProfileMissing) {
				return
			}
			s.authError(w, err)
			return
		}
	}
	item, err := s.auth.UpdateAccount(r.Context(), id, update)
	if err == nil {
		send(w, http.StatusOK, item)
		return
	}
	accountError(w, err)
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, auth.ErrAccountExists) || errors.Is(err, auth.ErrPasswordSetupRequired) || errors.Is(err, auth.ErrSubsonicProfileMissing) {
		return
	}
	s.authError(w, err)
}

func (s *Server) accountPassword(w http.ResponseWriter, r *http.Request) {
	id, ok := accountID(w, r)
	if !ok {
		return
	}
	var in struct {
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if err := s.auth.ResetAccountPassword(r.Context(), id, in.Password); err != nil {
		accountError(w, err)
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, auth.ErrAccountExists) || errors.Is(err, auth.ErrPasswordSetupRequired) || errors.Is(err, auth.ErrSubsonicProfileMissing) {
			return
		}
		s.authError(w, err)
		return
	}
	send(w, http.StatusOK, map[string]bool{"changed": true})
}

func (s *Server) accountDiscord(w http.ResponseWriter, r *http.Request) {
	id, ok := accountID(w, r)
	if !ok {
		return
	}
	account, err := s.auth.AccountByID(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, http.StatusNotFound, "not_found", "Account not found.")
		return
	}
	if err != nil {
		s.internal(w, "account_read", err)
		return
	}
	if account.RequestAccountID < 1 {
		fail(w, http.StatusConflict, "requests_not_enabled", "Enable Requests for this account before linking Discord.")
		return
	}
	var in struct {
		DiscordUserID string `json:"discord_user_id"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if s.bot == nil {
		fail(w, http.StatusServiceUnavailable, "discord_unavailable", "Discord account linking is unavailable.")
		return
	}
	if err = s.bot.Link(r.Context(), account.RequestAccountID, in.DiscordUserID); errors.Is(err, sql.ErrNoRows) {
		fail(w, http.StatusNotFound, "not_found", "Account not found.")
		return
	} else if errors.Is(err, discordbot.ErrConfig) {
		fail(w, http.StatusBadRequest, "invalid_discord_user", "Use a valid Discord user ID not linked to another account.")
		return
	} else if err != nil {
		s.internal(w, "account_discord", err)
		return
	}
	send(w, http.StatusOK, map[string]bool{"linked": in.DiscordUserID != ""})
}

// accountSelfPassword is available to every central account, including a
// player-only account that has no access to requester routes.
func (s *Server) accountSelfPassword(w http.ResponseWriter, r *http.Request) {
	session := r.Context().Value(sessionKey{}).(auth.Session)
	if session.UserAccountID < 1 || session.Role == "admin" {
		fail(w, http.StatusForbidden, "account_required", "Sign in with a centralized account.")
		return
	}
	var in struct {
		Current string `json:"current_password"`
		New     string `json:"new_password"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if err := s.auth.ChangeAccountPassword(r.Context(), session.UserAccountID, in.Current, in.New); errors.Is(err, auth.ErrUnauthorized) {
		fail(w, http.StatusBadRequest, "invalid_password", "The current password is incorrect or the account is unavailable.")
		return
	} else if err != nil {
		s.authError(w, err)
		return
	}
	s.cookie(w, r, "", 0)
	send(w, http.StatusOK, map[string]bool{"changed": true})
}
