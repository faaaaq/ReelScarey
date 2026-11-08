package metadata

import (
	"context"
	"errors"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/securestore"
)

type captureProvider struct{ token string }

func (p *captureProvider) Validate(_ context.Context, token string) error {
	p.token = token
	return nil
}
func (*captureProvider) Search(context.Context, string, string, int) (Results, error) {
	return Results{}, nil
}
func (*captureProvider) Movie(context.Context, string, int64) (Movie, error) {
	return Movie{}, nil
}

func TestDefaultTokenIsUsedUntilReplaced(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := database.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	store, err := securestore.New(ctx, db, dir)
	if err != nil {
		t.Fatal(err)
	}
	provider := &captureProvider{}
	service := New(store, provider)

	configured, _, err := service.Status(ctx)
	if err != nil || configured {
		t.Fatal("empty service reported configured", configured, err)
	}
	if err := service.Test(ctx); !errors.Is(err, ErrNotConfigured) {
		t.Fatal("empty service did not report not configured", err)
	}

	defaultToken := "test-shared-default-provider-token"
	service.SetDefaultToken(defaultToken)
	if !service.DefaultTokenAvailable() {
		t.Fatal("default token not reported")
	}
	configured, _, err = service.Status(ctx)
	if err != nil || !configured {
		t.Fatal("default token did not configure the service", configured, err)
	}
	using, err := service.UsingDefault(ctx)
	if err != nil || !using {
		t.Fatal("default token not reported as in use", using, err)
	}
	if err := service.Test(ctx); err != nil || provider.token != defaultToken {
		t.Fatal("default token was not used", provider.token, err)
	}

	userToken := "test-user-provider-token-value"
	if err := service.Configure(ctx, userToken); err != nil {
		t.Fatal(err)
	}
	if using, _ = service.UsingDefault(ctx); using {
		t.Fatal("saved token still reported the default")
	}
	if err := service.Test(ctx); err != nil || provider.token != userToken {
		t.Fatal("saved token did not replace the default", provider.token, err)
	}

	if err := service.Remove(ctx); err != nil {
		t.Fatal(err)
	}
	if using, _ = service.UsingDefault(ctx); !using {
		t.Fatal("removing the saved token did not fall back to the default")
	}
	if err := service.Test(ctx); err != nil || provider.token != defaultToken {
		t.Fatal("fallback token was not used", provider.token, err)
	}
}
