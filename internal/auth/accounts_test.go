package auth

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/music"
	"github.com/faaaaq/ReelScarey/internal/securestore"
)

func centralAccountFixture(t *testing.T) (*Service, *music.Service) {
	t.Helper()
	s, db, dir, code := authFixture(t)
	if _, err := s.Setup(context.Background(), "admin", "admin-test-password", code); err != nil {
		t.Fatal(err)
	}
	secrets, err := securestore.New(context.Background(), db, dir)
	if err != nil {
		t.Fatal(err)
	}
	s.SetSecrets(secrets)
	musicService := music.NewService(db, "")
	musicService.SetSecrets(secrets)
	status, err := musicService.MusicServer(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = musicService.SaveMusicServer(context.Background(), true, status.Revision); err != nil {
		t.Fatal(err)
	}
	return s, musicService
}

func TestCentralAccountSharesCredentialsAndFencesGrants(t *testing.T) {
	ctx := context.Background()
	s, musicService := centralAccountFixture(t)
	a, err := s.CreateAccount(ctx, "household", "shared-test-password", true, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if a.RequestAccountID < 1 || a.MusicServerUserID < 1 || !a.PasswordSet {
		t.Fatalf("missing system profiles: %+v", a)
	}
	session, err := s.Login(ctx, "household", "shared-test-password")
	if err != nil {
		t.Fatal(err)
	}
	if session.Role != "requester" || session.AccountID != a.RequestAccountID || session.UserAccountID != a.ID || session.RequestAccountID() != a.RequestAccountID {
		t.Fatalf("request-compatible central session changed identity: %+v", session)
	}
	for _, access := range []string{AccessRequests, AccessMusicPlayer, AccessSubsonic} {
		if !session.HasAccess(access) {
			t.Fatalf("missing %s grant: %+v", access, session.Access)
		}
	}
	if _, ok, err := musicService.AuthenticateMusicServer(ctx, "household", "shared-test-password", "", ""); err != nil || !ok {
		t.Fatalf("shared Subsonic password failed: ok=%v err=%v", ok, err)
	}

	player := false
	if _, err = s.UpdateAccount(ctx, a.ID, AccountUpdate{MusicPlayer: &player}); err != nil {
		t.Fatal(err)
	}
	resolved, err := s.Resolve(ctx, session.Token)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.HasAccess(AccessMusicPlayer) || !resolved.HasAccess(AccessRequests) || !resolved.HasAccess(AccessSubsonic) {
		t.Fatalf("permission update did not apply independently: %+v", resolved.Access)
	}

	if err = s.ResetAccountPassword(ctx, a.ID, "replacement-test-password"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Resolve(ctx, session.Token); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("password reset retained browser session: %v", err)
	}
	if _, err = s.Login(ctx, "household", "shared-test-password"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("old browser password still works: %v", err)
	}
	if _, ok, err := musicService.AuthenticateMusicServer(ctx, "household", "shared-test-password", "", ""); err != nil || ok {
		t.Fatalf("old Subsonic password still works: ok=%v err=%v", ok, err)
	}
	session, err = s.Login(ctx, "household", "replacement-test-password")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := musicService.AuthenticateMusicServer(ctx, "household", "replacement-test-password", "", ""); err != nil || !ok {
		t.Fatalf("replacement Subsonic password failed: ok=%v err=%v", ok, err)
	}

	disabled := true
	if _, err = s.UpdateAccount(ctx, a.ID, AccountUpdate{Disabled: &disabled}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Resolve(ctx, session.Token); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("disabled account retained session: %v", err)
	}
	if _, ok, err := musicService.AuthenticateMusicServer(ctx, "household", "replacement-test-password", "", ""); err != nil || ok {
		t.Fatalf("disabled account retained Subsonic access: ok=%v err=%v", ok, err)
	}
}

func TestPlayerOnlyAccountCanGainRequestsWithoutChangingIdentity(t *testing.T) {
	ctx := context.Background()
	s, _ := centralAccountFixture(t)
	a, err := s.CreateAccount(ctx, "listener", "listener-test-password", false, true, false)
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.Login(ctx, "listener", "listener-test-password")
	if err != nil {
		t.Fatal(err)
	}
	if session.Role != "user" || session.AccountID != a.ID || session.RequestAccountID() != 0 || !slices.Equal(session.Access, []string{AccessMusicPlayer}) {
		t.Fatalf("unexpected player-only session: %+v", session)
	}
	if err = s.ChangeAccountPassword(ctx, a.ID, "listener-test-password", "listener-new-password"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Resolve(ctx, session.Token); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("self password change retained session: %v", err)
	}
	requests := true
	updated, err := s.UpdateAccount(ctx, a.ID, AccountUpdate{Requests: &requests})
	if err != nil {
		t.Fatal(err)
	}
	if updated.RequestAccountID < 1 {
		t.Fatal("enabling Requests did not create its system profile")
	}
	session, err = s.Login(ctx, "listener", "listener-new-password")
	if err != nil {
		t.Fatal(err)
	}
	if session.RequestAccountID() != updated.RequestAccountID || !session.HasAccess(AccessMusicPlayer) || !session.HasAccess(AccessRequests) {
		t.Fatalf("combined grants not resolved: %+v", session)
	}
}

func TestLegacySubsonicAccountUsesCentralGrantAndPasswordReset(t *testing.T) {
	ctx := context.Background()
	s, musicService := centralAccountFixture(t)
	user, generated, err := musicService.CreateMusicServerUser(ctx, music.MusicServerUserInput{Username: "legacy-music"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := musicService.AuthenticateMusicServer(ctx, user.Username, generated, "", ""); err != nil || !ok {
		t.Fatalf("legacy generated credential failed: ok=%v err=%v", ok, err)
	}
	accounts, err := s.Accounts(ctx)
	if err != nil || len(accounts) != 1 || accounts[0].PasswordSet {
		t.Fatalf("legacy account was not projected honestly: %+v %v", accounts, err)
	}
	if _, err = s.Login(ctx, user.Username, generated); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Subsonic-only credential unexpectedly enabled browser login: %v", err)
	}
	if err = s.ResetAccountPassword(ctx, accounts[0].ID, "central-music-password"); err != nil {
		t.Fatal(err)
	}
	session, err := s.Login(ctx, user.Username, "central-music-password")
	if err != nil || session.Role != "user" || !session.HasAccess(AccessSubsonic) || session.HasAccess(AccessMusicPlayer) {
		t.Fatalf("central Subsonic-only browser session is wrong: %+v %v", session, err)
	}
	if _, ok, err := musicService.AuthenticateMusicServer(ctx, user.Username, "central-music-password", "", ""); err != nil || !ok {
		t.Fatalf("central password did not synchronize to Subsonic: ok=%v err=%v", ok, err)
	}
	granted := false
	if _, err = s.UpdateAccount(ctx, accounts[0].ID, AccountUpdate{Subsonic: &granted}); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := musicService.AuthenticateMusicServer(ctx, user.Username, "central-music-password", "", ""); err != nil || ok {
		t.Fatalf("revoked Subsonic grant still authenticates: ok=%v err=%v", ok, err)
	}
}
