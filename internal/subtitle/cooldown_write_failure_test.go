package subtitle

import (
	"context"
	"strings"
	"testing"
	"time"
)

// A failed provider-cooldown write must be observable: the job fails with a
// message that names the persistence failure instead of silently continuing
// to contact a provider that asked us to back off (RS-0014).
func TestProviderCooldownWriteFailureIsReported(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	enable(t, f, true, Track{"en", "full"})
	if _, err := f.db.Exec(`CREATE TRIGGER fail_provider_state BEFORE UPDATE ON subtitle_provider_state BEGIN SELECT RAISE(ABORT, 'injected cooldown write failure'); END;`); err != nil {
		t.Fatal(err)
	}
	f.p.searchErr = &Error{Code: "provider_rate_limit", Message: "Fixture provider asked for patience", RetryAt: time.Now().Add(12 * time.Hour).Unix()}
	process(t, f.s)
	v, e := f.s.File(ctx, 1, false)
	if e != nil {
		t.Fatal(e)
	}
	if v.Job.State == "completed" || v.Job.State == "waiting_provider" {
		t.Fatalf("job must not present an unrecorded cooldown as usable: %+v", v.Job)
	}
	if !strings.Contains(v.Job.Message, "provider cooldown could not be saved") {
		t.Fatalf("persistence failure was not surfaced on the job: %+v", v.Job)
	}
	var cooldownUntil int64
	if err := f.db.QueryRow(`SELECT cooldown_until FROM subtitle_provider_state WHERE id=1`).Scan(&cooldownUntil); err != nil {
		t.Fatal(err)
	}
	if cooldownUntil != 0 {
		t.Fatal("cooldown was written despite the injected failure")
	}
	if f.p.searches != 1 {
		t.Fatalf("unexpected provider traffic: %d searches", f.p.searches)
	}
}

// The connection test reports a failed cooldown write alongside the provider
// result instead of dropping it.
func TestCooldownWriteFailureSurfacesThroughConnectionTest(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	if _, err := f.db.Exec(`CREATE TRIGGER fail_provider_state BEFORE UPDATE ON subtitle_provider_state BEGIN SELECT RAISE(ABORT, 'injected cooldown write failure'); END;`); err != nil {
		t.Fatal(err)
	}
	f.p.testErr = &Error{Code: "provider_auth", Message: "Fixture provider rejected the credentials"}
	e := f.s.Test(ctx)
	if e == nil {
		t.Fatal("provider failure must be returned")
	}
	if !strings.Contains(e.Error(), "provider cooldown could not be saved") {
		t.Fatalf("cooldown write failure was dropped: %v", e)
	}
	if !strings.Contains(e.Error(), "Fixture provider rejected the credentials") {
		t.Fatalf("provider cause lost: %v", e)
	}
}
