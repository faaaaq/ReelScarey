package transfer

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	announceScope        = "announce:iptorrents"
	announceCandidateTTL = 7 * 24 * time.Hour
	announceOrigin       = "https://www.iptorrents.com"
)

// AnnounceRef binds an encrypted release selection to the exact captured
// announce and source revision. It contains no credential or download URL.
type AnnounceRef struct {
	EventID        int64  `json:"event_id"`
	SourceID       string `json:"source_id"`
	SourceRevision int    `json:"source_revision"`
	TorrentID      string `json:"torrent_id"`
}

func announceReleaseID(sourceID string, eventID int64) string {
	return announceScope + ":" + sourceID + ":" + strconv.FormatInt(eventID, 10)
}

func validAnnounceTorrentID(id string) bool {
	if len(id) < 1 || len(id) > 20 {
		return false
	}
	for _, r := range id {
		if r < '0' || r > '9' {
			return false
		}
	}
	n, err := strconv.ParseUint(id, 10, 64)
	return err == nil && n > 0
}

func validAnnounceRef(ref *AnnounceRef) bool {
	return ref != nil && ref.EventID > 0 && ref.SourceRevision > 0 && textOK(ref.SourceID, 128) && validAnnounceTorrentID(ref.TorrentID)
}

// QueueAnnounceAfter persists an exact IPTorrents announce before applying the
// ordinary reservation and delayed-submission workflow. The stable candidate
// ID makes repeated delivery of one source/event an acknowledgement.
func (s *Service) QueueAnnounceAfter(ctx context.Context, eventID int64, sourceID string, sourceRevision int, mediaID int64, episodeID *int64, version string, delay time.Duration, guard QueueGuard) (Job, bool, error) {
	if guard == nil {
		return Job{}, false, problem("invalid_release", "Automatic downloads require a current release-check policy.")
	}
	if mediaID < 1 || eventID < 1 || sourceRevision < 1 || !textOK(sourceID, 128) || (version != "" && version != "hd" && version != "uhd") {
		return Job{}, false, problem("invalid_release", "This announce release has no valid source or target.")
	}
	if episodeID != nil && version != "" {
		return Job{}, false, problem("invalid_version", "Movie versions cannot target TV episodes.")
	}

	var releaseName, freeleech, torrentID, infoURL string
	var size, receivedAt int64
	err := s.db.QueryRowContext(ctx, `SELECT e.release_name,e.size_bytes,e.freeleech,e.torrent_id,e.info_url,e.received_at
FROM announce_events e JOIN announce_sources s ON s.id=e.source_id
WHERE e.id=? AND e.source_id=? AND e.source_revision=? AND e.tracker='iptorrents'
AND s.tracker='iptorrents' AND s.enabled=1 AND s.auto_grab=1 AND s.revision=?`, eventID, sourceID, sourceRevision, sourceRevision).Scan(&releaseName, &size, &freeleech, &torrentID, &infoURL, &receivedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, false, problem("release_expired", "This announce source changed or automatic grabbing is no longer enabled.")
	}
	if err != nil {
		return Job{}, false, err
	}
	if !textOK(releaseName, 500) || size < 0 || !validAnnounceTorrentID(torrentID) {
		return Job{}, false, problem("invalid_release", "This announce does not contain a valid IPTorrents release.")
	}

	ref := &AnnounceRef{EventID: eventID, SourceID: sourceID, SourceRevision: sourceRevision, TorrentID: torrentID}
	v := Candidate{
		Release: Release{
			ID:          announceReleaseID(sourceID, eventID),
			MovieID:     mediaID,
			Title:       releaseName,
			Size:        size,
			Freeleech:   freeleech != "",
			InfoURL:     infoURL,
			IndexerName: "IPTorrents announce",
			Protocol:    "torrent",
		},
		Announce: ref,
	}
	if receivedAt > 0 {
		v.Published = time.Unix(receivedAt, 0).UTC().Format(time.RFC3339)
	}
	if episodeID != nil {
		target, err := s.tvTargetForRSS(ctx, mediaID, *episodeID)
		if err != nil {
			return Job{}, false, err
		}
		v.Kind = "series"
		v.Targets = []TVTarget{target}
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return Job{}, false, err
	}
	sealed, err := s.secrets.Seal("release", raw)
	if err != nil {
		return Job{}, false, err
	}
	now := time.Now()
	// Candidates are durable crash-recovery evidence, not permanent history.
	// Keep rows referenced by a download and bound all other announce candidates
	// to their TTL. This also removes an abandoned candidate before its stable
	// event identity is reused after a retry.
	if _, err = s.db.ExecContext(ctx, `DELETE FROM release_candidates WHERE scope=? AND expires_at<? AND NOT EXISTS (SELECT 1 FROM downloads d WHERE d.release_id=release_candidates.id)`, announceScope, now.Unix()); err != nil {
		return Job{}, false, err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO release_candidates(id,movie_id,title,size_bytes,seeders,peers,published_at,info_url,payload,expires_at,scope)
VALUES(?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET movie_id=excluded.movie_id,title=excluded.title,size_bytes=excluded.size_bytes,seeders=excluded.seeders,peers=excluded.peers,published_at=excluded.published_at,info_url=excluded.info_url,payload=excluded.payload,expires_at=excluded.expires_at,scope=excluded.scope`, v.ID, mediaID, v.Title, v.Size, v.Seeders, v.Peers, v.Published, v.InfoURL, sealed, now.Add(announceCandidateTTL).Unix(), announceScope)
	if err != nil {
		return Job{}, false, err
	}
	return s.queueVersion(ctx, v.ID, guard, 0, version, delay)
}

type announceQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// announceSettings revalidates both the captured event and the current source.
// It is used before network I/O and again inside the reservation transaction.
func announceSettings(ctx context.Context, q announceQueryer, ref *AnnounceRef) ([]byte, error) {
	if !validAnnounceRef(ref) {
		return nil, problem("invalid_release", "The saved announce release identity is invalid.")
	}
	var sealed []byte
	err := q.QueryRowContext(ctx, `SELECT s.settings FROM announce_events e JOIN announce_sources s ON s.id=e.source_id
WHERE e.id=? AND e.source_id=? AND e.source_revision=? AND e.tracker='iptorrents' AND e.torrent_id=?
AND s.tracker='iptorrents' AND s.enabled=1 AND s.auto_grab=1 AND s.revision=?`, ref.EventID, ref.SourceID, ref.SourceRevision, ref.TorrentID, ref.SourceRevision).Scan(&sealed)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, problem("release_expired", "This announce source changed or automatic grabbing is no longer enabled.")
	}
	return sealed, err
}

func (s *Service) validateAnnounce(ctx context.Context, q announceQueryer, ref *AnnounceRef) error {
	_, err := announceSettings(ctx, q, ref)
	return err
}

func announceDownloadURL(ref *AnnounceRef, releaseName, key string) (*url.URL, error) {
	if !validAnnounceRef(ref) || !textOK(releaseName, 500) || len(key) < 1 || len(key) > 512 || strings.ContainsAny(key, "\x00\r\n") {
		return nil, problem("invalid_settings", "The announce source download credential or release identity is invalid. Review its settings.")
	}
	target := &url.URL{
		Scheme:   "https",
		Host:     "www.iptorrents.com",
		Path:     "/download.php/" + ref.TorrentID + "/" + releaseName + ".torrent",
		RawPath:  "/download.php/" + ref.TorrentID + "/" + url.PathEscape(releaseName) + ".torrent",
		RawQuery: "torrent_pass=" + url.QueryEscape(key),
	}
	return target, nil
}

// fetchAnnounce deliberately ignores InfoURL and every indexer setting. The
// fixed HTTPS origin and Direct.request provide bounded I/O, no redirects and
// normal certificate verification; IRC SkipTLSVerify is not consulted.
func (s *Service) fetchAnnounce(ctx context.Context, v Candidate) (Payload, string, error) {
	if v.Manual || v.Announce == nil || v.ID != announceReleaseID(v.Announce.SourceID, v.Announce.EventID) {
		return Payload{}, "", problem("invalid_release", "The saved announce release identity is invalid.")
	}
	sealed, err := announceSettings(ctx, s.db, v.Announce)
	if err != nil {
		return Payload{}, "", err
	}
	raw, err := s.secrets.Open("announce:"+v.Announce.SourceID, sealed)
	if err != nil {
		return Payload{}, "", err
	}
	var settings struct {
		Key string `json:"key"`
	}
	if err = json.Unmarshal(raw, &settings); err != nil {
		return Payload{}, "", problem("invalid_settings", "The announce source download settings could not be read. Review its settings.")
	}
	target, err := announceDownloadURL(v.Announce, v.Title, settings.Key)
	if err != nil {
		return Payload{}, "", err
	}
	adapter, ok := s.adapters["iptorrents"].(*Direct)
	if !ok || adapter == nil {
		return Payload{}, "", problem("release_expired", "The IPTorrents announce adapter is unavailable.")
	}
	body, err := adapter.request(ctx, IndexerConfig{URL: announceOrigin, Kind: "iptorrents"}, http.MethodGet, target, nil, 4<<20)
	if err != nil {
		return Payload{}, "", err
	}
	hash, err := TorrentHash(body)
	if err != nil {
		return Payload{}, "", err
	}
	return Payload{Torrent: body}, hash, nil
}

func (s *Service) fenceAnnounce(guard QueueGuard, ref *AnnounceRef) QueueGuard {
	if ref == nil {
		return guard
	}
	return func(ctx context.Context, tx *sql.Tx, v Candidate) error {
		if err := s.validateAnnounce(ctx, tx, ref); err != nil {
			return err
		}
		if guard != nil {
			return guard(ctx, tx, v)
		}
		return nil
	}
}
