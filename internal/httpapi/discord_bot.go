package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/faaaaq/ReelScarey/internal/discordbot"
)

func (s *Server) discordInteraction(w http.ResponseWriter, r *http.Request) {
	if s.bot == nil {
		http.Error(w, "unavailable", 503)
		return
	}
	s.bot.ServeInteraction(w, r)
}
func (s *Server) discordBotSettings(w http.ResponseWriter, r *http.Request) {
	if s.bot == nil {
		fail(w, 503, "bot_unavailable", "Discord bot is unavailable.")
		return
	}
	switch r.Method {
	case http.MethodGet:
		c, err := s.bot.Config(r.Context())
		if err != nil {
			s.internal(w, "discord_bot_status", err)
			return
		}
		send(w, 200, c)
	case http.MethodPut:
		var in struct {
			ApplicationID  string `json:"application_id"`
			PublicKey      string `json:"public_key"`
			GuildID        string `json:"guild_id"`
			AdminUserID    string `json:"admin_user_id"`
			AdminChannelID string `json:"admin_channel_id"`
			Token          string `json:"token"`
		}
		if !decodeJSON(w, r, &in) {
			return
		}
		if !strings.HasPrefix(s.config.PublicURL, "https://") {
			fail(w, 409, "https_required", "Configure the public HTTPS URL before enabling Discord interactions.")
			return
		}
		c := discordbot.Config{ApplicationID: in.ApplicationID, PublicKey: in.PublicKey, GuildID: in.GuildID, AdminUserID: in.AdminUserID, AdminChannelID: in.AdminChannelID}
		if err := s.bot.Save(r.Context(), c, in.Token); errors.Is(err, discordbot.ErrConfig) {
			fail(w, 400, "invalid_bot", err.Error())
			return
		} else if err != nil {
			s.internal(w, "discord_bot_save", err)
			return
		}
		c.Configured = true
		send(w, 200, c)
	case http.MethodDelete:
		if err := s.bot.Remove(r.Context()); err != nil {
			s.internal(w, "discord_bot_remove", err)
			return
		}
		send(w, 200, map[string]bool{"configured": false})
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		fail(w, 405, "method_not_allowed", "Use GET, PUT or DELETE.")
	}
}
func (s *Server) discordBotRegister(w http.ResponseWriter, r *http.Request) {
	if s.bot == nil {
		fail(w, 503, "bot_unavailable", "Discord bot is unavailable.")
		return
	}
	var in struct{}
	if !decodeJSON(w, r, &in) {
		return
	}
	if err := s.bot.Register(r.Context()); err != nil {
		fail(w, 502, "bot_registration_failed", "Discord did not register the commands. Check the bot configuration and permissions.")
		return
	}
	send(w, 200, map[string]bool{"registered": true})
}
func (s *Server) discordBotRetryNotice(w http.ResponseWriter, r *http.Request) {
	if s.bot == nil {
		fail(w, 503, "bot_unavailable", "Discord bot is unavailable.")
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
	if err = s.bot.RetryNotice(r.Context(), id); errors.Is(err, sql.ErrNoRows) {
		fail(w, 409, "notice_changed", "This notice cannot be retried.")
		return
	} else if err != nil {
		s.internal(w, "discord_bot_notice", err)
		return
	}
	send(w, 202, map[string]bool{"queued": true})
}
func (s *Server) requestAccountDiscord(w http.ResponseWriter, r *http.Request) {
	if s.bot == nil {
		fail(w, 503, "bot_unavailable", "Discord bot is unavailable.")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		fail(w, 400, "invalid_account", "Choose a valid account.")
		return
	}
	var in struct {
		UserID string `json:"discord_user_id"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if err = s.bot.Link(r.Context(), id, in.UserID); errors.Is(err, sql.ErrNoRows) {
		fail(w, 404, "not_found", "Account not found.")
		return
	} else if errors.Is(err, discordbot.ErrConfig) {
		fail(w, 400, "invalid_discord_user", "Use a valid Discord user ID not linked to another account.")
		return
	} else if err != nil {
		s.internal(w, "discord_account_link", err)
		return
	}
	send(w, 200, map[string]bool{"linked": in.UserID != ""})
}
