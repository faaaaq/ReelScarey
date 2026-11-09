package releaseguard

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestBlacklistBlockedHonorsExpiryAndProtocol(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	store := New(db)
	now := time.Now().Unix()

	if err := store.Block(ctx, Entry{
		Protocol: ProtocolTorrent, ReleaseKey: "torrent-key", Indexer: "indexer-a",
		Title: "Permanent", Reason: "bad release", CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Block(ctx, Entry{
		Protocol: ProtocolUsenet, ReleaseKey: "expired-key", Indexer: "indexer-a",
		Title: "Expired", Reason: "old", CreatedAt: now, ExpiresAt: now - 1,
	}); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		protocol  string
		release   string
		indexer   string
		want      bool
		wantError bool
	}{
		{name: "permanent exact match", protocol: ProtocolTorrent, release: "torrent-key", indexer: "indexer-a", want: true},
		{name: "wrong protocol", protocol: ProtocolUsenet, release: "torrent-key", indexer: "indexer-a", want: false},
		{name: "wrong indexer", protocol: ProtocolTorrent, release: "torrent-key", indexer: "indexer-b", want: false},
		{name: "expired", protocol: ProtocolUsenet, release: "expired-key", indexer: "indexer-a", want: false},
		{name: "invalid protocol", protocol: "ftp", release: "torrent-key", indexer: "indexer-a", wantError: true},
		{name: "empty key", protocol: ProtocolTorrent, release: " ", indexer: "indexer-a", wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := store.Blocked(ctx, test.protocol, test.release, test.indexer)
			if test.wantError {
				if err == nil {
					t.Fatal("expected validation error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("Blocked() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestBlacklistBlockUpsertsAndPreservesID(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	store := New(db)
	now := time.Now().Unix()

	if err := store.Block(ctx, Entry{
		Protocol: ProtocolTorrent, ReleaseKey: "duplicate", Indexer: "indexer",
		Title: "old title", Reason: "old reason", CreatedAt: 100,
	}); err != nil {
		t.Fatal(err)
	}
	before, err := store.List(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 1 {
		t.Fatalf("got %d entries, want 1", len(before))
	}
	if err := store.Block(ctx, Entry{
		Protocol: ProtocolUsenet, ReleaseKey: "duplicate", Indexer: "indexer",
		Title: "new title", Reason: "new reason", CreatedAt: 200, ExpiresAt: now + 3600,
	}); err != nil {
		t.Fatal(err)
	}
	after, err := store.List(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 1 {
		t.Fatalf("upsert created %d entries, want 1", len(after))
	}
	entry := after[0]
	if entry.ID != before[0].ID {
		t.Fatalf("upsert changed ID from %d to %d", before[0].ID, entry.ID)
	}
	if entry.Protocol != ProtocolUsenet || entry.Title != "new title" || entry.Reason != "new reason" {
		t.Fatalf("upsert did not refresh the decision: %+v", entry)
	}
	if entry.CreatedAt != 200 || entry.ExpiresAt != now+3600 {
		t.Fatalf("upsert did not refresh timestamps: %+v", entry)
	}
	if blocked, err := store.Blocked(ctx, ProtocolUsenet, "duplicate", "indexer"); err != nil || !blocked {
		t.Fatalf("upserted entry is not blocked: %v, %v", blocked, err)
	}
}

func TestBlacklistListLimitOrderRemoveAndCleanup(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	store := New(db)
	now := time.Now().Unix()

	for index, key := range []string{"first", "second", "third"} {
		if err := store.Block(ctx, Entry{
			Protocol: ProtocolTorrent, ReleaseKey: key, Indexer: "indexer",
			Title: key, CreatedAt: now + int64(index),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Block(ctx, Entry{
		Protocol: ProtocolUsenet, ReleaseKey: "expired", Indexer: "indexer",
		Title: "expired", CreatedAt: now, ExpiresAt: now - 1,
	}); err != nil {
		t.Fatal(err)
	}

	limited, err := store.List(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(limited) != 2 || limited[0].ReleaseKey != "third" || limited[1].ReleaseKey != "second" {
		t.Fatalf("unexpected limited newest-first list: %+v", limited)
	}
	defaulted, err := store.List(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(defaulted) != 4 {
		t.Fatalf("default list returned %d entries, want 4", len(defaulted))
	}

	if err := store.Remove(ctx, limited[0].ID); err != nil {
		t.Fatal(err)
	}
	if blocked, err := store.Blocked(ctx, ProtocolTorrent, "third", "indexer"); err != nil || blocked {
		t.Fatalf("removed entry is still blocked: %v, %v", blocked, err)
	}

	removed, err := store.CleanupExpired(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("CleanupExpired removed %d rows, want 1", removed)
	}
	remaining, err := store.List(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 2 {
		t.Fatalf("after cleanup got %d entries, want 2: %+v", len(remaining), remaining)
	}
	for _, entry := range remaining {
		if entry.ExpiresAt != 0 {
			t.Fatalf("cleanup removed the wrong row or kept an expired one: %+v", entry)
		}
	}
}

func TestBlacklistRejectsInvalidEntries(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	store := New(db)

	tests := []struct {
		name  string
		entry Entry
	}{
		{name: "protocol", entry: Entry{Protocol: "ftp", ReleaseKey: "key"}},
		{name: "empty key", entry: Entry{Protocol: ProtocolTorrent, ReleaseKey: "  "}},
		{name: "negative expiry", entry: Entry{Protocol: ProtocolTorrent, ReleaseKey: "key", ExpiresAt: -1}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := store.Block(ctx, test.entry)
			if err == nil {
				t.Fatal("expected validation error")
			}
			var validation *ValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("got %T, want *ValidationError", err)
			}
		})
	}
}
