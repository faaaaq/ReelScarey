package music

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/securestore"
)

// newMusicServerUserService opens a real database and encrypted store so the
// per-user account table, migrations and sealing are all exercised.
func newMusicServerUserService(t *testing.T) *Service {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	db, err := database.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	secrets, err := securestore.New(ctx, db, dir)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(db, "")
	service.SetSecrets(secrets)
	return service
}

func enableMusicServer(t *testing.T, service *Service) {
	t.Helper()
	ctx := context.Background()
	status, err := service.MusicServer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.SaveMusicServer(ctx, true, status.Revision); err != nil {
		t.Fatal(err)
	}
}

func TestMusicServerUserCreateAndAuthenticate(t *testing.T) {
	ctx := context.Background()
	service := newMusicServerUserService(t)
	enableMusicServer(t, service)

	user, password, err := service.CreateMusicServerUser(ctx, MusicServerUserInput{Username: "  Scarey  ", Email: "scarey@example.com"})
	if err != nil {
		t.Fatalf("creating a music server user failed: %v", err)
	}
	if user.Username != "Scarey" {
		t.Fatalf("username should be trimmed, got %q", user.Username)
	}
	if len(password) != musicServerTokenLength || !user.PasswordGenerated {
		t.Fatalf("a generated password should be %d Crockford characters: %q (%+v)", musicServerTokenLength, password, user)
	}
	// Role defaults: playback roles on, destructive roles off.
	if !user.DownloadRole || !user.PlaylistRole || !user.CoverArtRole || !user.ScrobblingEnabled {
		t.Fatalf("new accounts should keep the playback roles: %+v", user)
	}
	if user.UploadRole || user.ShareRole || user.AdminRole || user.SettingsRole {
		t.Fatalf("new accounts must not gain destructive roles: %+v", user)
	}

	status, err := service.MusicServer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if status.Users != 1 || !status.Configured {
		t.Fatalf("status should count the new account: %+v", status)
	}

	// Generated passwords are Crockford-forgiving: case and separators ignored.
	got, ok, err := service.AuthenticateMusicServer(ctx, "scarey", strings.ToLower(password), "", "")
	if err != nil || !ok {
		t.Fatalf("case-insensitive generated password should verify: ok=%v err=%v", ok, err)
	}
	if got.ID != user.ID {
		t.Fatalf("authenticated the wrong account: %+v", got)
	}

	// The md5 token form must reproduce md5(password+salt) server-side.
	salt := "salty"
	sum := md5.Sum([]byte(password + salt))
	token := hex.EncodeToString(sum[:])
	if _, ok, err := service.AuthenticateMusicServer(ctx, "Scarey", "", token, salt); err != nil || !ok {
		t.Fatalf("md5 token authentication failed: ok=%v err=%v", ok, err)
	}

	if _, ok, err := service.AuthenticateMusicServer(ctx, "Scarey", "wrong-password", "", ""); err != nil || ok {
		t.Fatalf("a wrong password must not authenticate: ok=%v err=%v", ok, err)
	}
	if _, ok, err := service.AuthenticateMusicServer(ctx, "ghost", password, "", ""); err != nil || ok {
		t.Fatalf("an unknown username must not authenticate: ok=%v err=%v", ok, err)
	}

	// The account survives a reopen with a fresh service on the same database.
	users, err := service.ListMusicServerUsers(ctx)
	if err != nil || len(users) != 1 {
		t.Fatalf("listing accounts failed: %v (%d)", err, len(users))
	}
	if users[0].LastUsedAt == 0 {
		t.Fatal("a successful authentication should record last_used_at")
	}
}

func TestMusicServerUserChosenPasswordIsExact(t *testing.T) {
	ctx := context.Background()
	service := newMusicServerUserService(t)
	enableMusicServer(t, service)

	chosen := "AbC-def_123"
	if _, _, err := service.CreateMusicServerUser(ctx, MusicServerUserInput{Username: "owner", Password: chosen}); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := service.AuthenticateMusicServer(ctx, "owner", chosen, "", ""); err != nil || !ok {
		t.Fatalf("the exact chosen password should verify: ok=%v err=%v", ok, err)
	}
	// A person-chosen password is not Crockford-forgiving.
	if _, ok, err := service.AuthenticateMusicServer(ctx, "owner", "abcDEF123", "", ""); err != nil || ok {
		t.Fatalf("a chosen password must compare exactly: ok=%v err=%v", ok, err)
	}
	// The enc: hex form is honoured for clients that escape the password.
	encoded := "enc:" + hex.EncodeToString([]byte(chosen))
	if _, ok, err := service.AuthenticateMusicServer(ctx, "owner", encoded, "", ""); err != nil || !ok {
		t.Fatalf("enc: password form should verify: ok=%v err=%v", ok, err)
	}
}

func TestMusicServerUserValidationAndUpdate(t *testing.T) {
	ctx := context.Background()
	service := newMusicServerUserService(t)
	enableMusicServer(t, service)

	if _, _, err := service.CreateMusicServerUser(ctx, MusicServerUserInput{Username: "   "}); !errors.Is(err, ErrMusicServerUsername) {
		t.Fatalf("a blank username should be rejected: %v", err)
	}
	if _, _, err := service.CreateMusicServerUser(ctx, MusicServerUserInput{Username: "a\nb"}); !errors.Is(err, ErrMusicServerUsername) {
		t.Fatalf("control characters should be rejected: %v", err)
	}
	if _, _, err := service.CreateMusicServerUser(ctx, MusicServerUserInput{Username: "user", Email: strings.Repeat("x", musicServerEmailMax+1)}); !errors.Is(err, ErrMusicServerEmail) {
		t.Fatalf("an over-long email should be rejected: %v", err)
	}
	if _, _, err := service.CreateMusicServerUser(ctx, MusicServerUserInput{Username: "user", Password: strings.Repeat("p", musicServerPasswordMax+1)}); !errors.Is(err, ErrMusicServerPassword) {
		t.Fatalf("an over-long password should be rejected: %v", err)
	}

	user, _, err := service.CreateMusicServerUser(ctx, MusicServerUserInput{Username: "Dana", DownloadRole: boolPtr(false)})
	if err != nil {
		t.Fatal(err)
	}
	if user.DownloadRole {
		t.Fatalf("an explicit download_role=false should persist: %+v", user)
	}
	if _, _, err := service.CreateMusicServerUser(ctx, MusicServerUserInput{Username: "dana"}); !errors.Is(err, ErrMusicServerUserExists) {
		t.Fatalf("usernames are case-insensitive unique: %v", err)
	}

	// A partial edit that omits a role must keep the stored value.
	updated, err := service.UpdateMusicServerUser(ctx, user.ID, MusicServerUserInput{Username: "Dana", Email: "dana@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Email != "dana@example.com" {
		t.Fatalf("email did not update: %+v", updated)
	}
	if updated.DownloadRole {
		t.Fatalf("an omitted role must keep its stored value: %+v", updated)
	}
	if updated.PlaylistRole != user.PlaylistRole || updated.CoverArtRole != user.CoverArtRole {
		t.Fatalf("untouched roles changed: %+v", updated)
	}
	if _, err := service.UpdateMusicServerUser(ctx, user.ID, MusicServerUserInput{Username: "Dana", ShareRole: boolPtr(true)}); err != nil {
		t.Fatal(err)
	}
	after, err := service.musicServerUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ShareRole {
		t.Fatalf("an explicit role change should persist: %+v", after)
	}
	if _, err := service.UpdateMusicServerUser(ctx, 9999, MusicServerUserInput{Username: "nobody"}); !errors.Is(err, ErrMusicServerUserAbsent) {
		t.Fatalf("updating a missing account should fail: %v", err)
	}
}

func TestMusicServerUserPasswordResetAndDelete(t *testing.T) {
	ctx := context.Background()
	service := newMusicServerUserService(t)
	enableMusicServer(t, service)

	user, original, err := service.CreateMusicServerUser(ctx, MusicServerUserInput{Username: "Rae"})
	if err != nil {
		t.Fatal(err)
	}
	reset, password, err := service.ResetMusicServerUserPassword(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reset.PasswordGenerated || len(password) != musicServerTokenLength || password == original {
		t.Fatalf("reset should issue a fresh generated password: %q (was %q)", password, original)
	}
	if _, ok, _ := service.AuthenticateMusicServer(ctx, "Rae", original, "", ""); ok {
		t.Fatal("the previous password must stop working after a reset")
	}
	if _, ok, err := service.AuthenticateMusicServer(ctx, "Rae", password, "", ""); err != nil || !ok {
		t.Fatalf("the reset password should verify: ok=%v err=%v", ok, err)
	}
	if _, _, err := service.ResetMusicServerUserPassword(ctx, 9999); !errors.Is(err, ErrMusicServerUserAbsent) {
		t.Fatalf("resetting a missing account should fail: %v", err)
	}

	if err := service.DeleteMusicServerUser(ctx, user.ID); err != nil {
		t.Fatal(err)
	}
	if err := service.DeleteMusicServerUser(ctx, user.ID); !errors.Is(err, ErrMusicServerUserAbsent) {
		t.Fatalf("deleting twice should report the account is gone: %v", err)
	}
	if _, ok, _ := service.AuthenticateMusicServer(ctx, "Rae", password, "", ""); ok {
		t.Fatal("a deleted account must not authenticate")
	}
}

func TestMusicServerLegacyTokenFallback(t *testing.T) {
	ctx := context.Background()
	service := newMusicServerUserService(t)
	enableMusicServer(t, service)

	token, _, err := service.GenerateMusicServerToken(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Any username still works with the shared token, and resolves to the
	// unnamed full-rights account so existing clients are never locked out.
	user, ok, err := service.AuthenticateMusicServer(ctx, "anything", token, "", "")
	if err != nil || !ok {
		t.Fatalf("the legacy token should still authenticate: ok=%v err=%v", ok, err)
	}
	if !user.Anonymous() {
		t.Fatalf("the legacy token must resolve to the unnamed account: %+v", user)
	}
	if !user.CanDownload() || !user.CanUpload() || !user.CanShare() || !user.CanSettings() {
		t.Fatalf("the unnamed account keeps full rights: %+v", user)
	}

	// A named account takes precedence over the shared token for its username.
	if _, _, err := service.CreateMusicServerUser(ctx, MusicServerUserInput{Username: "Named", Password: "named-password"}); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := service.AuthenticateMusicServer(ctx, "Named", token, "", ""); ok {
		t.Fatal("the shared token must not authenticate a named account")
	}
	if _, ok, _ := service.AuthenticateMusicServer(ctx, "Named", "named-password", "", ""); !ok {
		t.Fatal("the named account password should still work")
	}

	// Removing the shared token and every account leaves nothing configured.
	if _, err := service.DeleteMusicServerToken(ctx); err != nil {
		t.Fatal(err)
	}
	users, err := service.ListMusicServerUsers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range users {
		if err := service.DeleteMusicServerUser(ctx, item.ID); err != nil {
			t.Fatal(err)
		}
	}
	status, err := service.MusicServer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if status.Configured || status.Users != 0 {
		t.Fatalf("status should be unconfigured once nothing remains: %+v", status)
	}
}

func TestMusicServerUserDisabledServerRejects(t *testing.T) {
	ctx := context.Background()
	service := newMusicServerUserService(t)

	// Disabled server: even a valid account must not authenticate.
	_, password, err := service.CreateMusicServerUser(ctx, MusicServerUserInput{Username: "Solo"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := service.AuthenticateMusicServer(ctx, "Solo", password, "", ""); err != nil || ok {
		t.Fatalf("a disabled music server must reject authentication: ok=%v err=%v", ok, err)
	}
}

func boolPtr(value bool) *bool { return &value }
