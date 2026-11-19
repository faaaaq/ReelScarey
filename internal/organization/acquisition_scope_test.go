package organization

import (
	"context"
	"testing"
)

func TestAcquisitionFenceOnlyReservesCleanupTitles(t *testing.T) {
	f := newReviewFixture(t)
	f.movie(t, 1, "Movies/One/one.mkv", []byte("video"))
	ctx := context.Background()
	_, err := f.db.Exec(`INSERT INTO organization_plans(id,state,snapshot,created_at,updated_at) VALUES('cleanup','running','{"track_cleanup":{"targets":[{"id":1}]},"phase":"planned"}',1,1)`)
	if err != nil {
		t.Fatal(err)
	}
	if err := EnsureAcquisitionAvailable(ctx, f.db, 1); err == nil {
		t.Fatal("selected title was not reserved")
	}
	for _, id := range []int64{0, 99} {
		if err := EnsureAcquisitionAvailable(ctx, f.db, id); err != nil {
			t.Fatalf("unrelated acquisition %d blocked: %v", id, err)
		}
	}
	if _, err := f.db.Exec(`UPDATE organization_plans SET state='failed',snapshot='{"replacement":{},"phase":"published"}'`); err != nil {
		t.Fatal(err)
	}
	if err := EnsureAcquisitionAvailable(ctx, f.db, 99); err == nil {
		t.Fatal("unsafe recovery did not block unrelated acquisition")
	}
}
