package httpapi

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/faaaaq/ReelScarey/internal/auth"
	"github.com/faaaaq/ReelScarey/internal/config"
	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/securestore"
	"github.com/faaaaq/ReelScarey/internal/tidal"
)

// fakeTIDALProvider stands in for the composition-root provider. It never
// touches a process or the network, so these tests prove only the HTTP
// boundary: authentication, CSRF, response shapes and the honest
// not-configured behavior.
type fakeTIDALProvider struct {
	status       tidal.Status
	verify       tidal.Status
	state        tidal.LoginState
	stateErr     error
	beginErr     error
	quality      string
	qualityError error
	verifyCalls  int
}

func (f *fakeTIDALProvider) Status(_ context.Context) tidal.Status { return f.status }

func (f *fakeTIDALProvider) Verify(_ context.Context) tidal.Status {
	f.verifyCalls++
	return f.verify
}

func (f *fakeTIDALProvider) SetQuality(quality string) error {
	f.quality = quality
	return f.qualityError
}

func (f *fakeTIDALProvider) LoginBegin(_ context.Context) error { return f.beginErr }

func (f *fakeTIDALProvider) LoginStatus(_ context.Context) (tidal.LoginState, error) {
	return f.state, f.stateErr
}

// tidalFixture wires the real authentication boundary against a migrated
// SQLite database with the fake provider injected. The returned database
// handle is unused; the assertions go through HTTP like the other route
// contract tests.
func tidalFixture(t *testing.T, provider TIDALProvider) (http.Handler, *auth.Session) {
	return tidalFixtureWithMusic(t, provider, nil)
}

func tidalFixtureWithMusic(t *testing.T, provider TIDALProvider, musicService MusicService) (http.Handler, *auth.Session) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	db, err := database.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	assets := fstest.MapFS{"index.html": {Data: []byte("app shell")}}
	authService, err := auth.New(ctx, db, dir)
	if err != nil {
		t.Fatal(err)
	}
	store, err := securestore.New(ctx, db, dir)
	if err != nil {
		t.Fatal(err)
	}
	code, err := securestore.SecretFile(filepath.Join(dir, "setup-token"), 32, false)
	if err != nil {
		t.Fatal(err)
	}
	session, err := authService.Setup(ctx, "admin", "test-only-password", base64.RawURLEncoding.EncodeToString(code))
	if err != nil {
		t.Fatal(err)
	}
	handler := New(db, config.Config{ConfigDir: dir, MediaDir: "/media", DownloadsDir: "/downloads", PublicURL: "http://example.com"}, slog.New(slog.NewTextHandler(io.Discard, nil)), assets, "test", Dependencies{
		Auth:    authService,
		Secrets: store,
		TIDAL:   provider,
		Music:   musicService,
	})
	return handler, &session
}

const tidalStatusPath = "/api/v1/library/music/tidal/status"
const tidalLoginPath = "/api/v1/library/music/tidal/login"

// The status route is a read, so it must still sit behind the authentication
// boundary even though it exposes no secrets.
func TestTidalStatusRouteRequiresAuthentication(t *testing.T) {
	handler, _ := tidalFixture(t, &fakeTIDALProvider{status: tidal.Status{Available: true, LoggedIn: true, Quality: "HI_RES_LOSSLESS"}})
	if out := request(handler, "GET", tidalStatusPath, "", nil, false); out.Code != http.StatusUnauthorized {
		t.Fatal("unauthenticated tidal status", out.Code)
	}
}

func TestTidalStatusRouteReportsProviderState(t *testing.T) {
	handler, session := tidalFixture(t, &fakeTIDALProvider{
		status: tidal.Status{Available: true, TokenPresent: true, Quality: "HI_RES_LOSSLESS"},
		verify: tidal.Status{Available: true, LoggedIn: true, VerificationState: "verified", VerifiedAt: 123, Quality: "HI_RES_LOSSLESS"},
	})
	out := request(handler, "GET", tidalStatusPath, "", session, false)
	if out.Code != http.StatusOK {
		t.Fatal(out.Code, out.Body)
	}
	var status tidal.Status
	if err := json.Unmarshal(out.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if !status.Available || !status.LoggedIn || status.Quality != "HI_RES_LOSSLESS" || status.VerificationState != "verified" || status.VerifiedAt != 123 {
		t.Fatalf("wrong status: %+v", status)
	}
}

func TestTidalStatusTokenFileIsNotAVerifiedConnection(t *testing.T) {
	provider := &fakeTIDALProvider{
		// Simulate an older/misbehaving local adapter: even an incorrect local
		// logged_in bit must not bypass the remote verification result.
		status: tidal.Status{Available: true, LoggedIn: true, TokenPresent: true, Quality: "LOSSLESS"},
		verify: tidal.Status{Available: true, LoggedIn: true, VerificationState: "failed", VerifiedAt: 456, Detail: "TIDAL rejected the stored session"},
		state:  tidal.LoginState{Phase: "done", Result: "failed", Error: "authorization expired"},
	}
	handler, session := tidalFixture(t, provider)
	out := request(handler, "GET", tidalStatusPath, "", session, false)
	if out.Code != http.StatusOK {
		t.Fatal(out.Code, out.Body)
	}
	var status tidal.Status
	if err := json.Unmarshal(out.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status.LoggedIn || !status.TokenPresent || status.VerificationState != "failed" || status.VerifiedAt != 456 {
		t.Fatalf("token presence was treated as a verified connection: %+v", status)
	}
	if provider.verifyCalls != 1 || status.Detail == "" || status.Login == nil || status.Login.Result != "failed" {
		t.Fatalf("verbose verification/login detail missing: calls=%d status=%+v", provider.verifyCalls, status)
	}
}

func TestTidalStatusWithoutCredentialsDoesNotClaimVerification(t *testing.T) {
	provider := &fakeTIDALProvider{status: tidal.Status{Available: true, LoggedIn: true, Quality: "LOSSLESS"}}
	handler, session := tidalFixture(t, provider)
	out := request(handler, "GET", tidalStatusPath, "", session, false)
	var status tidal.Status
	if err := json.Unmarshal(out.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status.LoggedIn || status.TokenPresent || status.VerificationState != "not_checked" || provider.verifyCalls != 0 || status.Detail == "" {
		t.Fatalf("credential-free status is not honest: calls=%d status=%+v", provider.verifyCalls, status)
	}
}

// Without the provider installed the response must say so honestly instead
// of implying a hidden feature, and the login route must refuse to start.
func TestTidalRoutesWithoutProvider(t *testing.T) {
	handler, session := tidalFixture(t, nil)
	out := request(handler, "GET", tidalStatusPath, "", session, false)
	if out.Code != http.StatusOK {
		t.Fatal(out.Code, out.Body)
	}
	var status tidal.Status
	if err := json.Unmarshal(out.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status.Available || status.LoggedIn || status.Detail == "" {
		t.Fatalf("not-configured status must be honest: %+v", status)
	}
	if out := request(handler, "POST", tidalLoginPath, "", session, true); out.Code != http.StatusServiceUnavailable {
		t.Fatal("login without provider", out.Code, out.Body)
	}
}

// The login route is a mutation, so it must refuse an authenticated session
// without a CSRF token and accept the same request with one.
func TestTidalLoginRouteRequiresCSRF(t *testing.T) {
	handler, session := tidalFixture(t, &fakeTIDALProvider{state: tidal.LoginState{Phase: "waiting", Lines: []string{"https://link.tidal.com/ABCD", "Code: EFGH"}}})
	if out := request(handler, "POST", tidalLoginPath, "", session, false); out.Code != http.StatusForbidden || !strings.Contains(out.Body.String(), "csrf_required") {
		t.Fatalf("login without CSRF: %d %s", out.Code, out.Body)
	}
	out := request(handler, "POST", tidalLoginPath, "", session, true)
	if out.Code != http.StatusAccepted {
		t.Fatal(out.Code, out.Body)
	}
	var state tidal.LoginState
	if err := json.Unmarshal(out.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	if state.Phase != "waiting" || len(state.Lines) != 2 {
		t.Fatalf("wrong login state: %+v", state)
	}
	if out := request(handler, "GET", tidalLoginPath, "", session, false); out.Code != http.StatusOK {
		t.Fatal(out.Code, out.Body)
	}
}

func TestMusicPreferencesApplyTidalQualityToRunningProvider(t *testing.T) {
	provider := &fakeTIDALProvider{}
	handler, session := tidalFixtureWithMusic(t, provider, &fakeMusic{})
	out := request(handler, "PUT", "/api/v1/library/music/preferences", `{"allow_lossless":true,"preferred_formats":["FLAC"],"download_source":"tidal","tidal_quality":"LOSSLESS","revision":1}`, session, true)
	if out.Code != http.StatusOK {
		t.Fatal(out.Code, out.Body)
	}
	if provider.quality != "LOSSLESS" || !strings.Contains(out.Body.String(), `"download_source":"tidal"`) || !strings.Contains(out.Body.String(), `"tidal_quality":"LOSSLESS"`) {
		t.Fatalf("saved quality/source were not applied and returned: quality=%q body=%s", provider.quality, out.Body.String())
	}
}

// Provider failures surface as bounded 500s, never raw stderr.
func TestTidalLoginRouteReportsProviderFailure(t *testing.T) {
	handler, session := tidalFixture(t, &fakeTIDALProvider{beginErr: sql.ErrConnDone})
	out := request(handler, "POST", tidalLoginPath, "", session, true)
	if out.Code != http.StatusInternalServerError {
		t.Fatal(out.Code, out.Body)
	}
}
