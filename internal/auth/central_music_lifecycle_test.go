package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/music"
)

func TestLegacyMusicCreateConflictRollsBackProtocolProfile(t *testing.T) {
	ctx := context.Background()
	s, musicService := centralAccountFixture(t)
	account, err := s.CreateAccount(ctx, "browser", "browser-test-password", false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	// Reproduce a pre-fix partially linked row. The second insert must fail on
	// the central Subsonic name and roll back the protocol profile as well.
	if _, err = s.db.ExecContext(ctx, `UPDATE accounts SET subsonic_access=1 WHERE id=?`, account.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err = musicService.CreateMusicServerUser(ctx, music.MusicServerUserInput{Username: "browser", Password: "legacy-test-password"}); !errors.Is(err, music.ErrMusicServerUserExists) {
		t.Fatalf("expected typed central-name conflict, got %v", err)
	}
	var count int
	if err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM music_server_users WHERE username='browser'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed central insert left a protocol profile: count=%d err=%v", count, err)
	}
}

func TestLegacyMusicUpdateConflictRollsBackProtocolProfile(t *testing.T) {
	ctx := context.Background()
	s, musicService := centralAccountFixture(t)
	if _, err := s.CreateAccount(ctx, "browser", "browser-test-password", false, false, false); err != nil {
		t.Fatal(err)
	}
	account, err := s.CreateAccount(ctx, "music-name", "music-test-password", false, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = musicService.UpdateMusicServerUser(ctx, account.MusicServerUserID, music.MusicServerUserInput{Username: "browser"}); !errors.Is(err, music.ErrMusicServerUserExists) {
		t.Fatalf("expected typed central-name conflict, got %v", err)
	}
	users, err := musicService.ListMusicServerUsers(ctx)
	if err != nil || len(users) != 1 {
		t.Fatalf("list users: %+v %v", users, err)
	}
	if users[0].ID != account.MusicServerUserID || users[0].Username != "music-name" || !users[0].DownloadRole {
		t.Fatalf("failed central update changed the protocol profile: %+v", users[0])
	}
	if _, ok, err := musicService.AuthenticateMusicServer(ctx, "music-name", "music-test-password", "", ""); err != nil || !ok {
		t.Fatalf("rolled-back identity no longer authenticates: ok=%v err=%v", ok, err)
	}
}

func TestOrphanMusicProfileFailsClosed(t *testing.T) {
	ctx := context.Background()
	s, musicService := centralAccountFixture(t)
	if _, err := s.db.ExecContext(ctx, `INSERT INTO music_server_users(username,password_sealed,created_at,updated_at) VALUES('orphan',X'01',1,1)`); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := musicService.AuthenticateMusicServer(ctx, "orphan", "anything", "", ""); err != nil || ok {
		t.Fatalf("unlinked schema-116 profile authenticated: ok=%v err=%v", ok, err)
	}
}

func TestLegacyMusicDeleteRevokesUntilExplicitRegrant(t *testing.T) {
	ctx := context.Background()
	s, musicService := centralAccountFixture(t)
	account, err := s.CreateAccount(ctx, "combined", "combined-test-password", false, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if err = musicService.DeleteMusicServerUser(ctx, account.MusicServerUserID); err != nil {
		t.Fatal(err)
	}
	account, err = s.AccountByID(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if account.Subsonic || account.MusicServerUserID != 0 {
		t.Fatalf("delete retained central Subsonic state: %+v", account)
	}
	player := false
	account, err = s.UpdateAccount(ctx, account.ID, AccountUpdate{MusicPlayer: &player})
	if err != nil {
		t.Fatal(err)
	}
	if account.MusicServerUserID != 0 {
		t.Fatalf("unrelated update recreated deleted profile: %+v", account)
	}
	var count int
	if err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM music_server_users WHERE username='combined'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("deleted profile returned after unrelated update: count=%d err=%v", count, err)
	}
	subsonic := true
	account, err = s.UpdateAccount(ctx, account.ID, AccountUpdate{Subsonic: &subsonic})
	if err != nil {
		t.Fatal(err)
	}
	if !account.Subsonic || account.MusicServerUserID < 1 {
		t.Fatalf("explicit grant did not provision a profile: %+v", account)
	}
	if _, ok, err := musicService.AuthenticateMusicServer(ctx, "combined", "combined-test-password", "", ""); err != nil || !ok {
		t.Fatalf("explicitly regranted Subsonic access failed: ok=%v err=%v", ok, err)
	}
}
