package transfer

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

const (
	announceTestSource   = "announce-source-fixture"
	announceTestRevision = 7
	announceTestKey      = "fixture pass/key"
	announceTestTorrent  = "d4:infod6:lengthi3e4:name9:movie.mkv12:piece lengthi16384e6:pieces20:01234567890123456789ee"
)

type announceTestTransport struct {
	body     []byte
	before   func()
	requests int
	gotURL   string
	cookie   string
}

func (t *announceTestTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	t.requests++
	t.gotURL = r.URL.String()
	t.cookie = r.Header.Get("Cookie")
	if t.before != nil {
		t.before()
	}
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(t.body))}, nil
}

func prepareAnnounceTestSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	// The parent change owns the released migration. Until it lands, add only
	// the two columns consumed here to disposable test databases.
	for _, statement := range []string{
		`ALTER TABLE announce_sources ADD COLUMN auto_grab INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE announce_events ADD COLUMN source_revision INTEGER NOT NULL DEFAULT 0`,
	} {
		if _, err := db.Exec(statement); err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
			t.Fatal(err)
		}
	}
}

func addAnnounceTestEvent(t *testing.T, f *workflowFixture, releaseName, torrentID string) int64 {
	t.Helper()
	prepareAnnounceTestSchema(t, f.db)
	sealed, err := f.s.secrets.Seal("announce:"+announceTestSource, []byte(`{"key":"fixture pass/key","skip_tls_verify":true}`))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	if _, err = f.db.Exec(`INSERT INTO announce_sources(id,tracker,name,enabled,auto_grab,revision,settings,created_at,updated_at) VALUES(?,'iptorrents','Fixture IPT',1,1,?,?,?,?)`, announceTestSource, announceTestRevision, sealed, now, now); err != nil {
		t.Fatal(err)
	}
	result, err := f.db.Exec(`INSERT INTO announce_events(source_id,tracker,release_name,size_bytes,freeleech,torrent_id,info_url,dedupe,received_at,source_revision) VALUES(?,'iptorrents',?,123456,'100',?,'https://foreign.invalid/details?credential=never-fetch','fixture-dedupe',1760000000,?)`, announceTestSource, releaseName, torrentID, announceTestRevision)
	if err != nil {
		t.Fatal(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func useAnnounceTestTransport(f *workflowFixture, body []byte) *announceTestTransport {
	transport := &announceTestTransport{body: body}
	f.s.adapters["iptorrents"].(*Direct).HTTP = &http.Client{Transport: transport, Timeout: 5 * time.Second}
	return transport
}

func savedAnnounceCandidate(t *testing.T, f *workflowFixture, id string) Candidate {
	t.Helper()
	var sealed []byte
	if err := f.db.QueryRow(`SELECT payload FROM release_candidates WHERE id=?`, id).Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	raw, err := f.s.secrets.Open("release", sealed)
	if err != nil {
		t.Fatal(err)
	}
	var candidate Candidate
	if err = json.Unmarshal(raw, &candidate); err != nil {
		t.Fatal(err)
	}
	return candidate
}

func TestQueueAnnounceAfterMovieDeduplicatesAndUsesFixedOrigin(t *testing.T) {
	f := setupWorkflow(t)
	ctx := context.Background()
	eventID := addAnnounceTestEvent(t, f, "Fixture Movie/2026?", "12345")
	transport := useAnnounceTestTransport(f, []byte(announceTestTorrent))
	guardCalls := 0
	guard := func(context.Context, *sql.Tx, Candidate) error {
		guardCalls++
		return nil
	}
	job, created, err := f.s.QueueAnnounceAfter(ctx, eventID, announceTestSource, announceTestRevision, f.movie.ID, nil, "", 90*time.Second, guard)
	if err != nil || !created {
		t.Fatal(job, created, err)
	}
	if job.Kind != "movie" || job.MovieVersion != "" || job.Protocol != "torrent" || job.State != "queued" || guardCalls != 1 {
		t.Fatal("unexpected announce job", job, guardCalls)
	}
	wantURL := "https://www.iptorrents.com/download.php/12345/Fixture%20Movie%2F2026%3F.torrent?torrent_pass=fixture+pass%2Fkey"
	if transport.requests != 1 || transport.gotURL != wantURL || transport.cookie != "" {
		t.Fatal("announce did not use the exact fixed credential URL", transport.requests, transport.gotURL, transport.cookie)
	}
	wantHash, err := TorrentHash([]byte(announceTestTorrent))
	if err != nil || job.Hash != wantHash {
		t.Fatal("announce metainfo hash mismatch", job.Hash, wantHash, err)
	}

	releaseID := announceReleaseID(announceTestSource, eventID)
	candidate := savedAnnounceCandidate(t, f, releaseID)
	if candidate.Announce == nil || *candidate.Announce != (AnnounceRef{EventID: eventID, SourceID: announceTestSource, SourceRevision: announceTestRevision, TorrentID: "12345"}) {
		t.Fatal("saved announce identity mismatch", candidate.Announce)
	}
	if candidate.Title != "Fixture Movie/2026?" || candidate.Size != 123456 || !candidate.Freeleech || candidate.InfoURL != "https://foreign.invalid/details?credential=never-fetch" || candidate.IndexerName != "IPTorrents announce" || candidate.Protocol != "torrent" || candidate.Seeders != nil || candidate.Peers != nil || candidate.Published != time.Unix(1760000000, 0).UTC().Format(time.RFC3339) {
		t.Fatal("saved announce metadata mismatch", candidate)
	}
	var scope string
	var expires int64
	var seeders, peers sql.NullInt64
	if err = f.db.QueryRow(`SELECT scope,expires_at,seeders,peers FROM release_candidates WHERE id=?`, releaseID).Scan(&scope, &expires, &seeders, &peers); err != nil {
		t.Fatal(err)
	}
	if scope != announceScope || seeders.Valid || peers.Valid || expires < time.Now().Add(announceCandidateTTL-time.Minute).Unix() || expires > time.Now().Add(announceCandidateTTL+time.Minute).Unix() {
		t.Fatal("announce candidate retention mismatch", scope, expires, seeders, peers)
	}
	var createdAt, nextRun int64
	var indexerPublic int
	var download []byte
	if err = f.db.QueryRow(`SELECT created_at,next_run,indexer_public,payload FROM downloads WHERE id=?`, job.ID).Scan(&createdAt, &nextRun, &indexerPublic, &download); err != nil {
		t.Fatal(err)
	}
	if nextRun-createdAt != 90 || indexerPublic != 0 {
		t.Fatal("announce delay/privacy mismatch", createdAt, nextRun, indexerPublic)
	}
	rawDownload, err := f.s.secrets.Open("download", download)
	if err != nil {
		t.Fatal(err)
	}
	var payload Payload
	if err = json.Unmarshal(rawDownload, &payload); err != nil || !bytes.Equal(payload.Torrent, []byte(announceTestTorrent)) {
		t.Fatal("announce payload was not preserved", err)
	}

	duplicate, created, err := f.s.QueueAnnounceAfter(ctx, eventID, announceTestSource, announceTestRevision, f.movie.ID, nil, "", 90*time.Second, guard)
	if err != nil || created || duplicate.ID != job.ID || transport.requests != 1 || guardCalls != 1 {
		t.Fatal("stable announce retry was not idempotent", duplicate, created, err, transport.requests, guardCalls)
	}
	var candidates int
	if err = f.db.QueryRow(`SELECT COUNT(*) FROM release_candidates WHERE id=?`, releaseID).Scan(&candidates); err != nil || candidates != 1 {
		t.Fatal("stable announce candidate was duplicated", candidates, err)
	}
}

func TestQueueAnnounceAfterRevalidatesSourceInsideMovieReservation(t *testing.T) {
	for _, test := range []struct {
		name   string
		update string
	}{
		{name: "revision", update: `UPDATE announce_sources SET revision=revision+1 WHERE id='` + announceTestSource + `'`},
		{name: "disabled", update: `UPDATE announce_sources SET enabled=0 WHERE id='` + announceTestSource + `'`},
		{name: "auto grab disabled", update: `UPDATE announce_sources SET auto_grab=0 WHERE id='` + announceTestSource + `'`},
		{name: "event changed", update: `UPDATE announce_events SET torrent_id='999'`},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := setupWorkflow(t)
			eventID := addAnnounceTestEvent(t, f, "Fixture.Movie.2026.1080p", "12345")
			transport := useAnnounceTestTransport(f, []byte(announceTestTorrent))
			transport.before = func() {
				if _, err := f.db.Exec(test.update); err != nil {
					t.Error(err)
				}
			}
			guardCalls := 0
			_, created, err := f.s.QueueAnnounceAfter(context.Background(), eventID, announceTestSource, announceTestRevision, f.movie.ID, nil, "", 0, func(context.Context, *sql.Tx, Candidate) error {
				guardCalls++
				return nil
			})
			var apiError *Error
			if !errors.As(err, &apiError) || apiError.Code != "release_expired" || created || guardCalls != 0 {
				t.Fatal("changed announce source reached reservation", created, guardCalls, err)
			}
			var downloads int
			if err = f.db.QueryRow(`SELECT COUNT(*) FROM downloads`).Scan(&downloads); err != nil || downloads != 0 {
				t.Fatal("changed announce source committed a job", downloads, err)
			}
		})
	}
}

func TestQueueAnnounceAfterTVTargetAndTransactionFence(t *testing.T) {
	t.Run("exact episode", func(t *testing.T) {
		f, seriesID, targets := setupTV(t)
		eventID := addAnnounceTestEvent(t, f, "Fixture.Show.S01E02.1080p", "12345")
		useAnnounceTestTransport(f, []byte(announceTestTorrent))
		episodeID := targets[1].EpisodeID
		job, created, err := f.s.QueueAnnounceAfter(context.Background(), eventID, announceTestSource, announceTestRevision, seriesID, &episodeID, "", 30*time.Second, func(context.Context, *sql.Tx, Candidate) error { return nil })
		if err != nil || !created || job.Kind != "series" || len(job.Targets) != 1 || job.Targets[0].EpisodeID != episodeID {
			t.Fatal("announce did not reserve the exact TV episode", job, created, err)
		}
		candidate := savedAnnounceCandidate(t, f, announceReleaseID(announceTestSource, eventID))
		if candidate.Kind != "series" || len(candidate.Targets) != 1 || candidate.Targets[0] != targets[1] {
			t.Fatal("saved TV target mismatch", candidate.Targets, targets[1])
		}
	})
	t.Run("source changed after fetch", func(t *testing.T) {
		f, seriesID, targets := setupTV(t)
		eventID := addAnnounceTestEvent(t, f, "Fixture.Show.S01E01.1080p", "12345")
		transport := useAnnounceTestTransport(f, []byte(announceTestTorrent))
		transport.before = func() {
			if _, err := f.db.Exec(`UPDATE announce_sources SET auto_grab=0 WHERE id=?`, announceTestSource); err != nil {
				t.Error(err)
			}
		}
		episodeID := targets[0].EpisodeID
		_, created, err := f.s.QueueAnnounceAfter(context.Background(), eventID, announceTestSource, announceTestRevision, seriesID, &episodeID, "", 0, func(context.Context, *sql.Tx, Candidate) error { return nil })
		var apiError *Error
		if !errors.As(err, &apiError) || apiError.Code != "release_expired" || created {
			t.Fatal("TV announce bypassed its transaction fence", created, err)
		}
		var reservations int
		if err = f.db.QueryRow(`SELECT COUNT(*) FROM episode_downloads`).Scan(&reservations); err != nil || reservations != 0 {
			t.Fatal("fenced TV announce left an episode reservation", reservations, err)
		}
	})
}

func TestAnnounceFailureDoesNotLeakKeyAndSurvivesGenericCleanup(t *testing.T) {
	f := setupWorkflow(t)
	eventID := addAnnounceTestEvent(t, f, "Fixture.Movie.2026.1080p", "12345")
	useAnnounceTestTransport(f, []byte("not torrent metadata"))
	_, created, err := f.s.QueueAnnounceAfter(context.Background(), eventID, announceTestSource, announceTestRevision, f.movie.ID, nil, "", 0, func(context.Context, *sql.Tx, Candidate) error { return nil })
	if err == nil || created || strings.Contains(err.Error(), announceTestKey) || strings.Contains(err.Error(), "foreign.invalid") {
		t.Fatal("unsafe announce retrieval failure", created, err)
	}
	releaseID := announceReleaseID(announceTestSource, eventID)
	if _, err = f.db.Exec(`UPDATE release_candidates SET expires_at=1 WHERE id=?`, releaseID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.Search(context.Background(), f.movie.ID, ""); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = f.db.QueryRow(`SELECT COUNT(*) FROM release_candidates WHERE id=? AND scope=?`, releaseID, announceScope).Scan(&count); err != nil || count != 1 {
		t.Fatal("generic cleanup deleted an unqueued announce candidate", count, err)
	}
}

func TestAnnounceIdentityValidation(t *testing.T) {
	for _, id := range []string{"", "0", "000", "12x", "-1", "1/2", "123456789012345678901", "18446744073709551616"} {
		if validAnnounceTorrentID(id) {
			t.Fatal("invalid torrent ID accepted", id)
		}
	}
	for _, id := range []string{"1", "0001", "18446744073709551615"} {
		if !validAnnounceTorrentID(id) {
			t.Fatal("valid torrent ID rejected", id)
		}
	}
}
