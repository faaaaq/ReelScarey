package organization

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func seedQueuedPlan(t *testing.T, f reviewFixture, id, root, snapshot string) {
	t.Helper()
	if _, err := f.db.Exec(`INSERT INTO organization_plans(id,state,message,snapshot,created_at,updated_at) VALUES(?,'queued','',?,unixepoch(),unixepoch())`, id, snapshot); err != nil {
		t.Fatal(err)
	}
}

// A plan whose execution fails must end durably 'failed' with its message,
// an Activity event, and no second execution (RS-0004).
func TestPlanFailureIsRecordedDurably(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	dead := filepath.Join(f.cfg.MediaDir, "does-not-exist")
	seedQueuedPlan(t, f, "p1", dead, `{"id":"p1","state":"queued","phase":"planned","root":"`+jsonPath(dead)+`"}`)
	if err := f.s.Tick(ctx); err == nil {
		t.Fatal("execute failure must be returned by Tick")
	}
	var state, message string
	if err := f.db.QueryRow(`SELECT state,message FROM organization_plans WHERE id='p1'`).Scan(&state, &message); err != nil {
		t.Fatal(err)
	}
	if state != "failed" || message == "" {
		t.Fatalf("plan failure was not persisted: state=%q message=%q", state, message)
	}
	var events int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM activity_events WHERE kind='organization' AND state='plan failed; review the message and retry or remove the plan'`).Scan(&events); err != nil || events != 1 {
		t.Fatal("plan failure was not journaled to Activity", events, err)
	}
	// A second tick must not re-execute the failed plan.
	if err := f.s.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM activity_events WHERE kind='organization' AND state='plan failed; review the message and retry or remove the plan'`).Scan(&events); err != nil || events != 1 {
		t.Fatalf("failed plan was re-executed by the next tick (%d failure events)", events)
	}
}

// If the failed-safe phase snapshot cannot be written, the plan still ends
// 'failed' and the missing safety snapshot is journaled instead of silently
// discarded (RS-0004).
func TestFailedSafePhaseWriteFailureIsJournaled(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	seedQueuedPlan(t, f, "p1", f.cfg.MediaDir, `{"id":"p1","state":"queued","phase":"planned","removal":{"relative_path":"nothing"},"root":"`+jsonPath(f.cfg.MediaDir)+`"}`)
	if _, err := f.db.Exec(`CREATE TRIGGER fail_plan_snapshot BEFORE UPDATE ON organization_plans WHEN NEW.snapshot<>OLD.snapshot BEGIN SELECT RAISE(ABORT, 'injected snapshot write failure'); END;`); err != nil {
		t.Fatal(err)
	}
	if err := f.s.Tick(ctx); err == nil {
		t.Fatal("the injected write failure must be returned")
	}
	var state string
	if err := f.db.QueryRow(`SELECT state FROM organization_plans WHERE id='p1'`).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "failed" {
		t.Fatalf("terminal state was not persisted despite the phase write failure: %q", state)
	}
	var events int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM activity_events WHERE state='failed-safe phase could not be recorded; the plan stays failed for review'`).Scan(&events); err != nil || events != 1 {
		t.Fatal("failed-safe phase write failure was not journaled", events, err)
	}
}

// When even the terminal failure write fails, the plan is remembered in
// memory and never re-executed by this process; the divergence is journaled
// as far as the failing database allows (RS-0004).
func TestDegradedPlanIsNeverReexecuted(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	dead := filepath.Join(f.cfg.MediaDir, "missing")
	seedQueuedPlan(t, f, "p1", dead, `{"id":"p1","state":"queued","phase":"planned","root":"`+jsonPath(dead)+`"}`)
	if _, err := f.db.Exec(`CREATE TRIGGER fail_plan_terminal BEFORE UPDATE ON organization_plans WHEN NEW.snapshot<>OLD.snapshot OR NEW.state<>'running' BEGIN SELECT RAISE(ABORT, 'injected terminal write failure'); END;`); err != nil {
		t.Fatal(err)
	}
	if err := f.s.Tick(ctx); err == nil {
		t.Fatal("the injected terminal write failure must be returned")
	}
	var events int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM activity_events WHERE state='plan failure could not be recorded; check storage, review the plan, then restart to recover'`).Scan(&events); err != nil || events != 1 {
		t.Fatal("the bookkeeping divergence was not journaled", events, err)
	}
	// The next tick must skip the degraded plan entirely.
	if err := f.s.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM activity_events WHERE state='plan failure could not be recorded; check storage, review the plan, then restart to recover'`).Scan(&events); err != nil || events != 1 {
		t.Fatalf("degraded plan was claimed again (%d divergence events)", events)
	}
	_ = errors.New
}

// A degraded plan must not starve a queued plan whose id sorts below it
// (NEW-B3): the claim cursor and the ORDER BY column must agree, so every
// queued plan is reached within bounded ticks instead of waiting for a
// process restart.
func TestDegradedPlanDoesNotStarveQueuedPlan(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	dead := filepath.Join(f.cfg.MediaDir, "missing")
	// The degraded plan sorts above the queued plan lexically and was created
	// earlier, so the old created_at-ordered cursor skipped the queued plan
	// on every tick until process restart.
	// The degraded plan stays 'running' (its terminal write failed), which is
	// the realistic shape the claim loop must survive alongside a queued plan.
	// The one-work unique index normally prevents two claimable plans; the
	// test drops it to exercise the cursor arithmetic the loop uses when a
	// degraded and a queued plan coexist.
	if _, err := f.db.Exec(`INSERT INTO organization_plans(id,state,message,snapshot,created_at,updated_at) VALUES(?,'running','',?,unixepoch()-100,unixepoch())`, "zzzz-degraded", `{"id":"zzzz-degraded","state":"running","phase":"planned","root":"`+jsonPath(dead)+`"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`DROP INDEX organization_one_work`); err != nil {
		t.Fatal(err)
	}
	seedQueuedPlan(t, f, "aaaa-queued", dead, `{"id":"aaaa-queued","state":"queued","phase":"planned","root":"`+jsonPath(dead)+`"}`)
	if _, err := f.db.Exec(`UPDATE organization_plans SET created_at=created_at-100 WHERE id='zzzz-degraded'`); err != nil {
		t.Fatal(err)
	}
	if f.s.degraded == nil {
		f.s.degraded = map[string]bool{}
	}
	f.s.degraded["zzzz-degraded"] = true
	if err := f.s.Tick(ctx); err == nil {
		t.Fatal("the queued plan must execute (and fail against the missing root)")
	}
	var state string
	if err := f.db.QueryRow(`SELECT state FROM organization_plans WHERE id='aaaa-queued'`).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state == "queued" {
		t.Fatal("the queued plan was starved by the degraded plan's cursor")
	}
	var degradedState string
	if err := f.db.QueryRow(`SELECT state FROM organization_plans WHERE id='zzzz-degraded'`).Scan(&degradedState); err != nil {
		t.Fatal(err)
	}
	if degradedState != "running" {
		t.Fatalf("the degraded plan left its claimed state: %q", degradedState)
	}
}

func jsonPath(p string) string {
	out := make([]byte, 0, len(p))
	for _, c := range []byte(p) {
		if c == '\\' {
			out = append(out, '\\', '\\')
			continue
		}
		out = append(out, c)
	}
	return string(out)
}
