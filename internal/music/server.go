package music

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
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
type MusicServerStatus struct {
	Enabled    bool   `json:"enabled"`
	Configured bool   `json:"configured"`
	TokenHint  string `json:"token_hint"`
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

// VerifyMusicServerToken accepts the Subsonic token form md5(password+salt) or
// the plaintext form. The password is the generated server token, never the
// administrator's account password.
func (s *Service) VerifyMusicServerToken(ctx context.Context, username, password, token, salt string) (bool, error) {
	if strings.TrimSpace(username) == "" || len(username) > 200 || s.secrets == nil {
		return false, nil
	}
	status, err := s.MusicServer(ctx)
	if err != nil {
		return false, err
	}
	if !status.Enabled || !status.Configured {
		return false, nil
	}
	secret, err := s.secrets.GetNamed(ctx, musicServerTokenName)
	if err != nil || len(secret) == 0 {
		return false, err
	}
	expected := string(secret)
	if password != "" {
		value := password
		if strings.HasPrefix(strings.ToLower(value), "enc:") {
			decoded, err := hex.DecodeString(value[4:])
			if err != nil {
				return false, nil
			}
			value = string(decoded)
		}
		if subtle.ConstantTimeCompare([]byte(value), []byte(expected)) == 1 {
			return true, nil
		}
		// Crockford Base32 tokens are case-insensitive, and people habitually
		// group characters with spaces or dashes when typing on a phone, so a
		// normalised comparison keeps a hand-typed token working.
		normalized := normalizeMusicServerToken(value)
		return subtle.ConstantTimeCompare([]byte(normalized), []byte(normalizeMusicServerToken(expected))) == 1, nil
	}
	if token == "" || salt == "" {
		return false, nil
	}
	sum := md5.Sum([]byte(expected + salt))
	computed := hex.EncodeToString(sum[:])
	return subtle.ConstantTimeCompare([]byte(strings.ToLower(token)), []byte(computed)) == 1, nil
}
