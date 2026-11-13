package music

import (
	"context"
	"testing"
	"time"

	"github.com/faaaaq/ReelScarey/internal/database"
)

// The musicbrainz_provider_state cooldown write must be monotonic (NEW-D1,
// same class as the TMDb provider state): a shorter racing or later write
// never lowers the persisted deadline, it only extends or keeps it.
func TestMusicBrainzSetCooldownNeverShortensPersistedCooldown(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m := musicbrainzStore{db: db}
	long := time.Now().Add(2 * time.Hour).Truncate(time.Second)
	short := long.Add(-90 * time.Minute)
	if err := m.SetCooldown(ctx, long, "first cooldown"); err != nil {
		t.Fatal(err)
	}
	if err := m.SetCooldown(ctx, short, "shorter later write"); err != nil {
		t.Fatal(err)
	}
	until, message, err := m.Cooldown(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if until.Before(long) {
		t.Fatalf("a shorter later write shortened the persisted cooldown: %v < %v", until, long)
	}
	if message != "shorter later write" {
		t.Fatalf("the later write must still refresh the message: %q", message)
	}
}
