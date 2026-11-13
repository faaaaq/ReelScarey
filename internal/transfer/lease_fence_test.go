package transfer

import (
	"context"
	"errors"
	"testing"
)

// TestLeaseExpiryRefusesTorrentWrites covers the untested expired-lease branch
// of owned(): a worker that still holds the owner label but whose lease_until
// has lapsed must be refused with ErrLease, exactly like a foreign owner.
func TestLeaseExpiryRefusesTorrentWrites(t *testing.T) {
	f := setupWorkflow(t)
	j := f.queue(t)

	// Same owner, expired lease.
	if _, e := f.db.Exec(`UPDATE downloads SET lease_until=unixepoch()-1 WHERE id=?`, j.ID); e != nil {
		t.Fatal(e)
	}
	e := f.s.owned(context.Background(), j, `UPDATE downloads SET submission_attempted=1`)
	if !errors.Is(e, ErrLease) {
		t.Fatalf("expired lease with same owner: want ErrLease, got %v", e)
	}

	// Foreign owner with a valid lease must stay refused as well.
	if _, e := f.db.Exec(`UPDATE downloads SET lease_owner='foreign-worker',lease_until=unixepoch()+60 WHERE id=?`, j.ID); e != nil {
		t.Fatal(e)
	}
	e = f.s.owned(context.Background(), j, `UPDATE downloads SET submission_attempted=1`)
	if !errors.Is(e, ErrLease) {
		t.Fatalf("valid lease with foreign owner: want ErrLease, got %v", e)
	}
}

// TestLeaseExpiryRefusesMusicWrites covers the expired-lease branch of
// ownedMusic(), which shares the fence style with the torrent queue.
func TestLeaseExpiryRefusesMusicWrites(t *testing.T) {
	f := setupWorkflow(t)
	j := musicTestQueue(t, f)

	if _, e := f.db.Exec(`UPDATE music_downloads SET lease_until=unixepoch()-1 WHERE id=?`, j.ID); e != nil {
		t.Fatal(e)
	}
	e := f.s.ownedMusic(context.Background(), j, `UPDATE music_downloads SET submission_attempted=1`)
	if !errors.Is(e, ErrLease) {
		t.Fatalf("expired lease with same owner: want ErrLease, got %v", e)
	}

	if _, e := f.db.Exec(`UPDATE music_downloads SET lease_owner='foreign-worker',lease_until=unixepoch()+60 WHERE id=?`, j.ID); e != nil {
		t.Fatal(e)
	}
	e = f.s.ownedMusic(context.Background(), j, `UPDATE music_downloads SET submission_attempted=1`)
	if !errors.Is(e, ErrLease) {
		t.Fatalf("valid lease with foreign owner: want ErrLease, got %v", e)
	}
}
