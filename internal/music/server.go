package music

import (
	"context"
	"crypto/rand"
	"strings"
)

const musicServerTokenName = "music-server-token"

// musicServerTokenAlphabet is Crockford's Base32 alphabet: digits and uppercase
// letters with the easily confused characters I, L, O and U removed. Tokens are
// short enough to type on a phone and unambiguous when read or copied by hand.
const musicServerTokenAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// musicServerTokenLength is the number of characters in a generated token. Each
// character carries five bits (32 symbols), so 20 characters is 100 bits of
// entropy while staying easy to enter on a mobile keyboard.
const musicServerTokenLength = 20

// newMusicServerToken returns a random token drawn uniformly from the Crockford
// Base32 alphabet. Because 256 is a whole multiple of 32, masking a uniform
// random byte to its low five bits selects an alphabet index without bias.
func newMusicServerToken() (string, error) {
	raw := make([]byte, musicServerTokenLength)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	out := make([]byte, len(raw))
	for i, b := range raw {
		out[i] = musicServerTokenAlphabet[b&0x1f]
	}
	return string(out), nil
}

// normalizeMusicServerToken upper-cases a token and drops the separators people
// habitually add (spaces, dashes and underscores) so a hand-typed token still
// matches. Crockford Base32 is case-insensitive by design.
func normalizeMusicServerToken(token string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(strings.TrimSpace(token)) {
		switch r {
		case ' ', '-', '_':
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// MusicServerStatus describes the opt-in Subsonic-compatible endpoint. The
// endpoint is disabled by default and has no listener of its own: it uses the
// existing ReelScarey HTTP port.
//
// Configured is true once any credential exists: either a named account or the
// legacy shared token, so a client can sign in as soon as the administrator has
// created one. Users counts the named accounts.
type MusicServerStatus struct {
	Enabled    bool   `json:"enabled"`
	Configured bool   `json:"configured"`
	TokenHint  string `json:"token_hint"`
	Users      int    `json:"users"`
	Revision   int    `json:"revision"`
	UpdatedAt  int64  `json:"updated_at"`
}

func (s *Service) MusicServer(ctx context.Context) (MusicServerStatus, error) {
	var out MusicServerStatus
	var enabled int
	if err := s.store.db.QueryRowContext(ctx, `SELECT enabled,revision,updated_at FROM music_server_settings WHERE id=1`).
		Scan(&enabled, &out.Revision, &out.UpdatedAt); err != nil {
		return MusicServerStatus{}, err
	}
	out.Enabled = enabled == 1
	if err := s.store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM music_server_users`).Scan(&out.Users); err != nil {
		return MusicServerStatus{}, err
	}
	if s.secrets != nil {
		token, err := s.secrets.GetNamed(ctx, musicServerTokenName)
		if err != nil {
			return MusicServerStatus{}, err
		}
		out.Configured = len(token) > 0
		if out.Configured {
			out.TokenHint = tokenHint(string(token))
		}
	}
	out.Configured = out.Configured || out.Users > 0
	return out, nil
}

func tokenHint(token string) string {
	if len(token) <= 4 {
		return "••••"
	}
	return "••••" + token[len(token)-4:]
}

func (s *Service) SaveMusicServer(ctx context.Context, enabled bool, revision int) (MusicServerStatus, error) {
	if revision < 1 {
		return MusicServerStatus{}, ErrInvalidInput
	}
	result, err := s.store.db.ExecContext(ctx, `UPDATE music_server_settings SET enabled=?,revision=revision+1,updated_at=unixepoch() WHERE id=1 AND revision=?`, boolInt(enabled), revision)
	if err != nil {
		return MusicServerStatus{}, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return MusicServerStatus{}, err
	}
	if changed != 1 {
		return MusicServerStatus{}, ErrPlanChanged
	}
	return s.MusicServer(ctx)
}

func (s *Service) GenerateMusicServerToken(ctx context.Context) (string, MusicServerStatus, error) {
	if s.secrets == nil {
		return "", MusicServerStatus{}, &Error{Code: "music_server_unavailable", Message: "Encrypted credential storage is unavailable, so a music server token cannot be created."}
	}
	token, err := newMusicServerToken()
	if err != nil {
		return "", MusicServerStatus{}, err
	}
	if err := s.secrets.SetNamed(ctx, musicServerTokenName, []byte(token)); err != nil {
		return "", MusicServerStatus{}, err
	}
	status, err := s.MusicServer(ctx)
	return token, status, err
}

func (s *Service) DeleteMusicServerToken(ctx context.Context) (MusicServerStatus, error) {
	if s.secrets == nil {
		return MusicServerStatus{}, &Error{Code: "music_server_unavailable", Message: "Encrypted credential storage is unavailable."}
	}
	if err := s.secrets.DeleteNamed(ctx, musicServerTokenName); err != nil {
		return MusicServerStatus{}, err
	}
	return s.MusicServer(ctx)
}

// VerifyMusicServerToken reports whether a Subsonic request authenticates. It
// accepts either the named-account credentials or the legacy shared token; see
// AuthenticateMusicServer for the full resolution including account roles.
func (s *Service) VerifyMusicServerToken(ctx context.Context, username, password, token, salt string) (bool, error) {
	_, ok, err := s.AuthenticateMusicServer(ctx, username, password, token, salt)
	return ok, err
}
