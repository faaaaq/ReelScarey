package youtube

import (
	"context"
	"database/sql"
	"errors"
	"github.com/faaaaq/ReelScarey/internal/database"
	"testing"
)

type subscriptionInspector struct {
	result    Inspection
	onInspect func()
	err       error
}

func (p *subscriptionInspector) Inspect(context.Context, string) (Inspection, error) {
	if p.onInspect != nil {
		p.onInspect()
	}
	return p.result, p.err
}
func subscriptionFixture(t *testing.T) (*Store, *sql.DB, Subscription, *subscriptionInspector) {
	t.Helper()
	ctx := context.Background()
	db, e := database.Open(ctx, t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	s := NewStore(db)
	c, e := s.CreateCollection(ctx, "Owned fixtures")
	if e != nil {
		t.Fatal(e)
	}
	url := "https://www.youtube.com/playlist?list=PLabcdefghijk"
	p, e := s.SaveSubscription(ctx, SubscriptionUpdate{URL: url, Title: "Fixture source", CollectionID: c.ID, Mode: "review", Interval: 24})
	if e != nil {
		t.Fatal(e)
	}
	source, _ := NormalizeURL(url)
	inspector := &subscriptionInspector{result: Inspection{Source: source, Entries: []Entry{{Kind: "video", ID: "aaaaaaaaaaa", Title: "Existing A", Channel: "Fixture"}, {Kind: "video", ID: "bbbbbbbbbbb", Title: "Existing B", Channel: "Fixture"}}}}
	return s, db, p, inspector
}
func TestSubscriptionsBaselineOptInDedupeAndPauseDuringFetch(t *testing.T) {
	s, db, p, inspector := subscriptionFixture(t)
	ctx := context.Background()
	if p.Enabled || p.Mode != "review" || p.Initialized {
		t.Fatal(p)
	}
	if worked, e := s.ProcessSubscription(ctx, inspector); e != nil || worked {
		t.Fatal("paused subscription checked automatically", worked, e)
	}
	if e := s.RequestSubscription(ctx, p.ID, p.Revision); e != nil {
		t.Fatal(e)
	}
	if worked, e := s.ProcessSubscription(ctx, inspector); e != nil || !worked {
		t.Fatal(worked, e)
	}
	entries, e := s.SubscriptionEntries(ctx, p.ID)
	if e != nil || len(entries) != 2 || entries[0].State != "baseline" {
		t.Fatal(entries, e)
	}
	p, e = s.Subscription(ctx, p.ID)
	if e != nil {
		t.Fatal(e)
	}
	in := SubscriptionUpdate{ID: p.ID, URL: p.URL, Title: p.Title, CollectionID: p.CollectionID, Enabled: true, Mode: "acquire", Interval: 1, Revision: p.Revision}
	if _, e = s.SaveSubscription(ctx, in); !errors.Is(e, ErrChanged) {
		t.Fatal("automatic acquisition bypassed consent", e)
	}
	in.Reviewed = true
	p, e = s.SaveSubscription(ctx, in)
	if e != nil {
		t.Fatal(e)
	}
	inspector.result.Entries = append(inspector.result.Entries, Entry{Kind: "video", ID: "ccccccccccc", Title: "New fixture C", Channel: "Fixture"})
	if _, e = s.ProcessSubscription(ctx, inspector); e != nil {
		t.Fatal(e)
	}
	jobs, e := s.Jobs(ctx)
	if e != nil || len(jobs) != 1 || jobs[0].Video.ID != "ccccccccccc" {
		t.Fatal("baseline backlog was acquired", jobs, e)
	}
	if e = s.RequestSubscription(ctx, p.ID, p.Revision); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ProcessSubscription(ctx, inspector); e != nil {
		t.Fatal(e)
	}
	jobs, e = s.Jobs(ctx)
	if e != nil || len(jobs) != 1 {
		t.Fatal("duplicate video queued", jobs, e)
	}
	inspector.result.Entries = append(inspector.result.Entries, Entry{Kind: "video", ID: "ddddddddddd", Title: "Paused fixture D"})
	inspector.onInspect = func() {
		current, e := s.Subscription(ctx, p.ID)
		if e != nil {
			t.Fatal(e)
		}
		in.Revision = current.Revision
		in.Enabled = false
		if _, e = s.SaveSubscription(ctx, in); e != nil {
			t.Fatal(e)
		}
	}
	if e = s.RequestSubscription(ctx, p.ID, p.Revision); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ProcessSubscription(ctx, inspector); !errors.Is(e, ErrChanged) {
		t.Fatal("in-flight source check ignored pause", e)
	}
	var count int
	if e = db.QueryRow(`SELECT COUNT(*) FROM youtube_jobs`).Scan(&count); e != nil || count != 1 {
		t.Fatal(count, e)
	}
}
func TestBulkQueueIsAtomicAndReviewReplayIsRefused(t *testing.T) {
	s, db, p, _ := subscriptionFixture(t)
	ctx := context.Background()
	target := Target{Kind: "custom", CollectionID: p.CollectionID}
	a, e := s.Preview(ctx, Video{ID: "eeeeeeeeeee", Title: "One"}, target)
	if e != nil {
		t.Fatal(e)
	}
	b, e := s.Preview(ctx, Video{ID: "fffffffffff", Title: "Two"}, target)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(`UPDATE youtube_reviews SET expires_at=0 WHERE id=?`, b.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.QueueBulk(ctx, []string{a.ID, b.ID}); e == nil {
		t.Fatal("stale bulk review accepted")
	}
	var count int
	if e = db.QueryRow(`SELECT COUNT(*) FROM youtube_jobs`).Scan(&count); e != nil || count != 0 {
		t.Fatal("partial queue committed", count, e)
	}
	if _, e = db.Exec(`UPDATE youtube_reviews SET expires_at=unixepoch()+1800 WHERE id=?`, b.ID); e != nil {
		t.Fatal(e)
	}
	jobs, e := s.QueueBulk(ctx, []string{a.ID, b.ID})
	if e != nil || len(jobs) != 2 {
		t.Fatal(jobs, e)
	}
	if _, e = s.QueueBulk(ctx, []string{a.ID, b.ID}); e == nil {
		t.Fatal("bulk replay accepted")
	}
	if _, e = s.QueueBulk(ctx, []string{a.ID, a.ID}); e == nil {
		t.Fatal("duplicate review IDs accepted")
	}
}
func TestChangedSubscriptionTargetPreservesEntriesAndReleasesLease(t *testing.T) {
	s, db, p, inspector := subscriptionFixture(t)
	ctx := context.Background()
	if _, e := db.Exec(`UPDATE youtube_collections SET revision=revision+1 WHERE id=?`, p.CollectionID); e != nil {
		t.Fatal(e)
	}
	if e := s.RequestSubscription(ctx, p.ID, p.Revision); e != nil {
		t.Fatal(e)
	}
	if _, e := s.ProcessSubscription(ctx, inspector); !errors.Is(e, ErrChanged) {
		t.Fatal(e)
	}
	state, e := s.Subscription(ctx, p.ID)
	if e != nil || state.State != "attention" {
		t.Fatal(state, e)
	}
	var owner string
	var count int
	if e = db.QueryRow(`SELECT lease_owner FROM youtube_subscriptions WHERE id=?`, p.ID).Scan(&owner); e != nil || owner != "" {
		t.Fatal(owner, e)
	}
	if e = db.QueryRow(`SELECT COUNT(*) FROM youtube_subscription_entries`).Scan(&count); e != nil || count != 0 {
		t.Fatal("changed target saved discovery", count, e)
	}
	if _, e = s.SaveSubscription(ctx, SubscriptionUpdate{URL: "https://example.com/feed", Title: "Bad", CollectionID: p.CollectionID, Mode: "review", Interval: 1}); e == nil {
		t.Fatal("arbitrary host accepted")
	}
}
