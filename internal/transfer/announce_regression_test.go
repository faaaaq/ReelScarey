package transfer

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/faaaaq/ReelScarey/internal/library"
	"github.com/faaaaq/ReelScarey/internal/metadata"
)

// These tests exercise the durable candidate lifecycle owned by announce.go
// (the announce-scoped cleanup and stable-identity upsert) and the general
// release-candidate cleanup in service.go. They seed rows directly so the
// expiry, scope and download-reference dimensions are exact.

// seedReleaseCandidate inserts a minimal release candidate with an explicit
// scope and expiry. The payload is opaque because every caller either replaces
// it through the announce upsert or never decodes it.
func seedReleaseCandidate(t *testing.T, f *workflowFixture, id string, movieID int64, scope string, expires int64) {
	t.Helper()
	if _, err := f.db.Exec(`INSERT INTO release_candidates(id,movie_id,title,size_bytes,published_at,payload,expires_at,scope,info_url) VALUES(?,?,?,?,?,?,?,?,?)`,
		id, movieID, "Seeded "+id, 100, "2026-01-01T00:00:00Z", []byte{0}, expires, scope, "https://seed.invalid/"+id); err != nil {
		t.Fatal(err)
	}
}

// seedCandidateDownload attaches a download row to a candidate so the
// NOT EXISTS guard can be observed. Each download needs a unique release_id and
// info_hash.
func seedCandidateDownload(t *testing.T, f *workflowFixture, jobID, releaseID string, movieID int64, hash string) {
	t.Helper()
	// 'completed' keeps these reference-only rows out of the one_movie_download
	// partial unique index; the NOT EXISTS guard does not inspect state.
	if _, err := f.db.Exec(`INSERT INTO downloads(id,movie_id,release_id,title,info_hash,payload,client_url,save_path,state,created_at,updated_at,indexer_name,protocol) VALUES(?,?,?,?,?,?,?,?,'completed',1,1,'Seed','torrent')`,
		jobID, movieID, releaseID, "Seeded job "+jobID, hash, []byte{0}, "http://client.invalid", "/downloads"); err != nil {
		t.Fatal(err)
	}
}

func candidateExists(t *testing.T, f *workflowFixture, id string) bool {
	t.Helper()
	var n int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM release_candidates WHERE id=?`, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n == 1
}

func announceGuard() QueueGuard {
	return func(context.Context, *sql.Tx, Candidate) error { return nil }
}

// TestQueueAnnounceAfterRefreshesConflictAndQueues proves the ON CONFLICT(id)
// DO UPDATE branch refreshes every stored column (title, info_url, payload and
// expiry) for an unexpired candidate that already owns the stable identity,
// and that the refreshed row then queues normally.
func TestQueueAnnounceAfterRefreshesConflictAndQueues(t *testing.T) {
	f := setupWorkflow(t)
	ctx := context.Background()
	eventID := addAnnounceTestEvent(t, f, "Fixture.Movie.2026.1080p", "12345")
	releaseID := announceReleaseID(announceTestSource, eventID)
	// The stale row is unexpired, so the announce cleanup leaves it in place and
	// the insert must land on the conflict branch rather than a fresh insert.
	seedReleaseCandidate(t, f, releaseID, f.movie.ID, announceScope, time.Now().Add(time.Hour).Unix())
	useAnnounceTestTransport(f, []byte(announceTestTorrent))

	job, created, err := f.s.QueueAnnounceAfter(ctx, eventID, announceTestSource, announceTestRevision, f.movie.ID, nil, "", 0, announceGuard())
	if err != nil || !created || job.Kind != "movie" {
		t.Fatal("conflict refresh did not queue a movie job", job, created, err)
	}

	var count int
	var title, infoURL string
	var expires int64
	if err = f.db.QueryRow(`SELECT COUNT(*),MAX(title),MAX(info_url),MAX(expires_at) FROM release_candidates WHERE id=?`, releaseID).Scan(&count, &title, &infoURL, &expires); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatal("conflict upsert duplicated the candidate row", count)
	}
	if title != "Fixture.Movie.2026.1080p" || infoURL != "https://foreign.invalid/details?credential=never-fetch" {
		t.Fatal("conflict upsert did not refresh candidate metadata", title, infoURL)
	}
	if expires < time.Now().Add(announceCandidateTTL-time.Minute).Unix() || expires > time.Now().Add(announceCandidateTTL+time.Minute).Unix() {
		t.Fatal("conflict upsert did not refresh the candidate expiry", expires)
	}
	candidate := savedAnnounceCandidate(t, f, releaseID)
	if candidate.Announce == nil || *candidate.Announce != (AnnounceRef{EventID: eventID, SourceID: announceTestSource, SourceRevision: announceTestRevision, TorrentID: "12345"}) {
		t.Fatal("conflict upsert did not refresh the sealed payload", candidate.Announce)
	}
}

// TestQueueAnnounceAfterCleansExpiredUnreferencedCandidate proves an abandoned
// expired candidate is removed before its stable event identity is reused, so
// the retry leaves exactly one fresh row rather than a stale/duplicate pair.
func TestQueueAnnounceAfterCleansExpiredUnreferencedCandidate(t *testing.T) {
	f := setupWorkflow(t)
	ctx := context.Background()
	eventID := addAnnounceTestEvent(t, f, "Fixture.Movie.2026.1080p", "12345")
	releaseID := announceReleaseID(announceTestSource, eventID)
	seedReleaseCandidate(t, f, releaseID, f.movie.ID, announceScope, time.Now().Add(-time.Hour).Unix())
	useAnnounceTestTransport(f, []byte(announceTestTorrent))

	job, created, err := f.s.QueueAnnounceAfter(ctx, eventID, announceTestSource, announceTestRevision, f.movie.ID, nil, "", 0, announceGuard())
	if err != nil || !created || job.Kind != "movie" {
		t.Fatal("expired candidate retry did not queue", job, created, err)
	}
	var count int
	var expires int64
	if err = f.db.QueryRow(`SELECT COUNT(*),MAX(expires_at) FROM release_candidates WHERE id=?`, releaseID).Scan(&count, &expires); err != nil {
		t.Fatal(err)
	}
	if count != 1 || expires <= time.Now().Unix() {
		t.Fatal("expired abandoned candidate was not replaced by a fresh row", count, expires)
	}
}

// TestAnnounceCleanupPreservesUnexpiredAndReferencedCandidates exercises the
// announce-scoped cleanup: only expired, unreferenced announce candidates are
// removed. Unexpired candidates, download-referenced candidates and every
// non-announce scope must survive.
func TestAnnounceCleanupPreservesUnexpiredAndReferencedCandidates(t *testing.T) {
	f := setupWorkflow(t)
	ctx := context.Background()
	eventID := addAnnounceTestEvent(t, f, "Fixture.Movie.2026.1080p", "12345")
	past := time.Now().Add(-time.Hour).Unix()
	future := time.Now().Add(time.Hour).Unix()

	seedReleaseCandidate(t, f, "ann-expired-orphan", f.movie.ID, announceScope, past)
	seedReleaseCandidate(t, f, "ann-unexpired-orphan", f.movie.ID, announceScope, future)
	seedReleaseCandidate(t, f, "ann-expired-referenced", f.movie.ID, announceScope, past)
	seedReleaseCandidate(t, f, "ann-unexpired-referenced", f.movie.ID, announceScope, future)
	seedReleaseCandidate(t, f, "movie-expired-orphan", f.movie.ID, "movie", past)
	seedCandidateDownload(t, f, "job-expired-ref", "ann-expired-referenced", f.movie.ID, testHash)
	seedCandidateDownload(t, f, "job-unexpired-ref", "ann-unexpired-referenced", f.movie.ID, otherHash)

	useAnnounceTestTransport(f, []byte(announceTestTorrent))
	if _, created, err := f.s.QueueAnnounceAfter(ctx, eventID, announceTestSource, announceTestRevision, f.movie.ID, nil, "", 0, announceGuard()); err != nil || !created {
		t.Fatal("announce cleanup did not run to completion", created, err)
	}

	if candidateExists(t, f, "ann-expired-orphan") {
		t.Fatal("expired unreferenced announce candidate survived cleanup")
	}
	for _, id := range []string{"ann-unexpired-orphan", "ann-expired-referenced", "ann-unexpired-referenced", "movie-expired-orphan"} {
		if !candidateExists(t, f, id) {
			t.Fatal("announce cleanup removed a protected candidate", id)
		}
	}
}

// TestSearchCleanupRemovesExpiredOrphanAndPreservesReferenced exercises the
// general (non-announce) cleanup in Search. Its NOT EXISTS guard must keep a
// download-referenced candidate while still removing an expired ordinary orphan
// even though other downloads exist. Announce-scope rows are never touched.
func TestSearchCleanupRemovesExpiredOrphanAndPreservesReferenced(t *testing.T) {
	f := setupWorkflow(t)
	ctx := context.Background()
	year := 2026
	other, _, err := library.New(f.db).AddMovie(ctx, metadata.Movie{TMDbID: 124, Title: "Second: Movie", Year: &year}, true)
	if err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour).Unix()
	future := time.Now().Add(time.Hour).Unix()

	// Same movie as the search: expired or not, unreferenced rows are replaced.
	seedReleaseCandidate(t, f, "movie-expired-orphan", f.movie.ID, "movie", past)
	seedReleaseCandidate(t, f, "movie-unexpired-same", f.movie.ID, "movie", future)
	// A referenced row is protected by NOT EXISTS despite matching the movie.
	seedReleaseCandidate(t, f, "movie-expired-referenced", f.movie.ID, "movie", past)
	// Another movie's row is only removed when expired.
	seedReleaseCandidate(t, f, "movie-other-unexpired", other.ID, "movie", future)
	seedReleaseCandidate(t, f, "movie-other-expired", other.ID, "movie", past)
	// Announce candidates are out of scope for this cleanup entirely.
	seedReleaseCandidate(t, f, "announce-expired-orphan", f.movie.ID, announceScope, past)
	seedCandidateDownload(t, f, "job-movie-ref", "movie-expired-referenced", f.movie.ID, testHash)

	if _, err = f.s.Search(ctx, f.movie.ID, ""); err != nil {
		t.Fatal(err)
	}

	for _, id := range []string{"movie-expired-orphan", "movie-unexpired-same", "movie-other-expired"} {
		if candidateExists(t, f, id) {
			t.Fatal("search cleanup kept an expired/refreshable unreferenced candidate", id)
		}
	}
	for _, id := range []string{"movie-expired-referenced", "movie-other-unexpired", "announce-expired-orphan"} {
		if !candidateExists(t, f, id) {
			t.Fatal("search cleanup removed a protected candidate", id)
		}
	}
}

// TestQueueVersionSameInfoHashTargetTypeHandling covers the second dedup gate
// in queueVersion: a torrent whose info hash already belongs to a job is only
// reattached when the target selection matches. A different movie must be
// rejected with duplicate_hash, and the movie candidate's empty Kind is
// compared against the stored job Kind.
func TestQueueVersionSameInfoHashTargetTypeHandling(t *testing.T) {
	t.Run("different movie is rejected", func(t *testing.T) {
		f := setupWorkflow(t)
		ctx := context.Background()
		year := 2026
		other, _, err := library.New(f.db).AddMovie(ctx, metadata.Movie{TMDbID: 124, Title: "Second: Movie", Year: &year}, true)
		if err != nil {
			t.Fatal(err)
		}
		eventID := addAnnounceTestEvent(t, f, "Fixture.Movie.2026.1080p", "12345")
		useAnnounceTestTransport(f, []byte(announceTestTorrent))
		job, created, err := f.s.QueueAnnounceAfter(ctx, eventID, announceTestSource, announceTestRevision, f.movie.ID, nil, "", 0, announceGuard())
		if err != nil || !created {
			t.Fatal(job, created, err)
		}
		// Force the fixture indexer to hand back the same info hash for a
		// different movie.
		f.indexer.hash = job.Hash
		out, err := f.s.Search(ctx, other.ID, "")
		if err != nil || len(out) != 1 {
			t.Fatal(out, err)
		}
		_, created, err = f.s.Queue(ctx, out[0].ID)
		var apiError *Error
		if !errors.As(err, &apiError) || apiError.Code != "duplicate_hash" || created {
			t.Fatal("same hash for a different movie was not rejected", created, err)
		}
		var downloads int
		if err = f.db.QueryRow(`SELECT COUNT(*) FROM downloads WHERE movie_id=?`, other.ID).Scan(&downloads); err != nil || downloads != 0 {
			t.Fatal("rejected same-hash selection still committed a job", downloads, err)
		}
	})

	t.Run("equivalent empty movie kind acknowledges existing job", func(t *testing.T) {
		f := setupWorkflow(t)
		ctx := context.Background()
		eventID := addAnnounceTestEvent(t, f, "Fixture.Movie.2026.1080p", "12345")
		useAnnounceTestTransport(f, []byte(announceTestTorrent))
		job, created, err := f.s.QueueAnnounceAfter(ctx, eventID, announceTestSource, announceTestRevision, f.movie.ID, nil, "", 0, announceGuard())
		if err != nil || !created || job.Kind != "movie" {
			t.Fatal(job, created, err)
		}
		// Search and announce movie candidates leave Kind empty while the
		// stored job uses the database default. The same movie and hash must be
		// acknowledged rather than misreported as a conflicting target.
		f.indexer.hash = job.Hash
		out, err := f.s.Search(ctx, f.movie.ID, "")
		if err != nil || len(out) != 1 {
			t.Fatal(out, err)
		}
		existing, created, err := f.s.Queue(ctx, out[0].ID)
		if err != nil || created || existing.ID != job.ID {
			t.Fatal("equivalent same-hash movie was not acknowledged", existing, created, err)
		}
		var downloads int
		if err = f.db.QueryRow(`SELECT COUNT(*) FROM downloads`).Scan(&downloads); err != nil || downloads != 1 {
			t.Fatal("same-hash acknowledgement changed the download count", downloads, err)
		}
	})
}
