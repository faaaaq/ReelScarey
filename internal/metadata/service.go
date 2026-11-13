package metadata

import (
	"context"
	"database/sql"

	"github.com/faaaaq/ReelScarey/internal/securestore"
)

type Service struct {
	secrets      *securestore.Store
	provider     MovieProvider
	defaultToken string
}

func New(secrets *securestore.Store, provider MovieProvider) *Service {
	return &Service{secrets: secrets, provider: provider}
}

// SetDefaultToken installs the optional shared TMDb Read Access Token from the
// deployment environment. A token saved in Settings always wins, so users can
// replace the shared default without changing the deployment.
func (s *Service) SetDefaultToken(token string) { s.defaultToken = token }

// SetCooldownDB persists provider rate-limit cooldowns in the application
// database so they survive restarts (RS-0025). Providers without cooldown
// support are left untouched.
func (s *Service) SetCooldownDB(db *sql.DB) {
	if c, ok := s.provider.(interface{ SetCooldown(ProviderCooldown) }); ok {
		c.SetCooldown(NewSQLCooldown(db))
	}
}

// DefaultTokenAvailable reports whether the deployment supplies a shared token.
func (s *Service) DefaultTokenAvailable() bool { return s.defaultToken != "" }

// UsingDefault reports whether the shared token is currently in use.
func (s *Service) UsingDefault(ctx context.Context) (bool, error) {
	token, err := s.secrets.Get(ctx)
	if err != nil {
		return false, err
	}
	return token == "" && s.defaultToken != "", nil
}

func (s *Service) Status(ctx context.Context) (bool, string, error) {
	configured, updated, err := s.secrets.Status(ctx)
	if err != nil {
		return false, "", err
	}
	if !configured && s.defaultToken != "" {
		configured = true
	}
	return configured, updated, nil
}
func (s *Service) Configure(ctx context.Context, token string) error {
	if !ValidToken(token) {
		return &Error{Code: "tmdb_invalid_token", Message: "Enter a valid TMDb API Read Access Token."}
	}
	if err := s.provider.Validate(ctx, token); err != nil {
		return err
	}
	return s.secrets.Set(ctx, token)
}
func (s *Service) Remove(ctx context.Context) error { return s.secrets.Delete(ctx) }
func (s *Service) Test(ctx context.Context) error {
	token, err := s.token(ctx)
	if err != nil {
		return err
	}
	return s.provider.Validate(ctx, token)
}
func (s *Service) token(ctx context.Context) (string, error) {
	token, err := s.secrets.Get(ctx)
	if err != nil {
		return "", err
	}
	if token == "" {
		token = s.defaultToken
	}
	if token == "" {
		return "", ErrNotConfigured
	}
	return token, nil
}
func (s *Service) Search(ctx context.Context, query string, page int) (Results, error) {
	token, err := s.token(ctx)
	if err != nil {
		return Results{}, err
	}
	return s.provider.Search(ctx, token, query, page)
}
func (s *Service) Movie(ctx context.Context, id int64) (Movie, error) {
	token, err := s.token(ctx)
	if err != nil {
		return Movie{}, err
	}
	return s.provider.Movie(ctx, token, id)
}
