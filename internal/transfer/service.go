package transfer

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/faaaaq/ReelScarey/internal/config"
	"github.com/faaaaq/ReelScarey/internal/library"
	"github.com/faaaaq/ReelScarey/internal/organization"
	"github.com/faaaaq/ReelScarey/internal/securestore"
)

type Service struct {
	automaticMetadata AutomaticMetadata
	db                *sql.DB
	secrets           *securestore.Store
	cfg               config.Config
	client            Client
	transmission      *Transmission
	sab               *SABnzbd
	nzbget            *NZBGet
	indexer           Indexer
	adapters          map[string]Indexer
	configMu          sync.Mutex
	healthMu          sync.RWMutex
	probeMu           sync.Mutex
	legacyError       string
	importer          *Importer
	bundled           bundledRuntime
	tunnelGate        func() bool
}

func New(db *sql.DB, secrets *securestore.Store, cfg config.Config, client Client, indexer Indexer) *Service {
	s := &Service{db: db, secrets: secrets, cfg: cfg, client: client, indexer: indexer}
	s.adapters = map[string]Indexer{"iptorrents": NewDirect("iptorrents"), "xspeeds": NewDirect("xspeeds"), "nyaa": NewDirect("nyaa"), "rss": NewDirect("rss")}
	s.adapters["newznab"] = NewNewznab()
	s.sab = NewSABnzbd()
	s.nzbget = NewNZBGet()
	s.transmission = NewTransmission()
	s.importer = NewImporter(s)
	s.bundled.notify = make(chan struct{}, 1)
	return s
}

// SetTunnelGate supplies the VPN state consulted by the built-in client
// supervisor. A nil gate means no VPN is configured.
func (s *Service) SetTunnelGate(gate func() bool) { s.tunnelGate = gate }

// tunnelReady reports whether the built-in daemon may run. When a VPN is
// configured, the tunnel must be up: the kill switch keeps the torrent client
// stopped instead of letting it fall back to the bare interface.
func (s *Service) tunnelReady() bool {
	return s.tunnelGate == nil || s.tunnelGate()
}

type IntegrationStatus struct {
	Configured   bool   `json:"configured"`
	URL          string `json:"url"`
	Username     string `json:"username,omitempty"`
	DownloadPath string `json:"download_path,omitempty"`
}

func (s *Service) indexerConfig(ctx context.Context) (IndexerConfig, error) {
	var c IndexerConfig
	b, e := s.secrets.GetNamed(ctx, "torznab")
	if e != nil {
		return c, e
	}
	if len(b) == 0 {
		return c, problem("indexer_not_configured", "Connect a Torznab indexer in Settings first.")
	}
	e = json.Unmarshal(b, &c)
	return c, e
}
func (s *Service) clientConfig(ctx context.Context) (ClientConfig, error) {
	var c ClientConfig
	b, e := s.secrets.GetNamed(ctx, "qbittorrent")
	if e != nil {
		return c, e
	}
	if len(b) == 0 {
		return c, problem("client_not_configured", "Connect qBittorrent in Settings first.")
	}
	e = json.Unmarshal(b, &c)
	return c, e
}
func (s *Service) Status(ctx context.Context, name string) (IntegrationStatus, error) {
	if name == "sabnzbd" {
		return s.sabStatus(ctx)
	}
	if name == "clients" {
		d, e := s.ClientDefaults(ctx)
		if e != nil {
			return IntegrationStatus{}, e
		}
		if d.Torrent != "" || d.Usenet != "" {
			return IntegrationStatus{Configured: true}, nil
		}
		a, e := s.Status(ctx, "qbittorrent")
		if e != nil {
			return a, e
		}
		b, e := s.sabStatus(ctx)
		return IntegrationStatus{Configured: a.Configured || b.Configured}, e
	}
	if name == "indexers" {
		var enabled int
		if e := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM indexers WHERE enabled=1`).Scan(&enabled); e != nil {
			return IntegrationStatus{}, e
		}
		_, e := s.indexerConfig(ctx)
		var p *Error
		if errors.As(e, &p) && p.Code == "indexer_not_configured" {
			return IntegrationStatus{Configured: enabled > 0}, nil
		}
		return IntegrationStatus{Configured: e == nil || enabled > 0}, e
	}
	if name == "torznab" {
		c, e := s.indexerConfig(ctx)
		var p *Error
		if errors.As(e, &p) && p.Code == "indexer_not_configured" {
			return IntegrationStatus{}, nil
		}
		return IntegrationStatus{Configured: e == nil, URL: c.URL}, e
	}
	c, e := s.clientConfig(ctx)
	var p *Error
	if errors.As(e, &p) && p.Code == "client_not_configured" {
		return IntegrationStatus{}, nil
	}
	return IntegrationStatus{Configured: e == nil, URL: c.URL, Username: c.Username, DownloadPath: c.DownloadPath}, e
}

type IndexerUpdate struct {
	URL    string  `json:"url"`
	APIKey *string `json:"api_key"`
}
type ClientUpdate struct {
	URL          string  `json:"url"`
	Username     string  `json:"username"`
	Password     *string `json:"password"`
	DownloadPath string  `json:"download_path"`
}

func (s *Service) ConfigureIndexer(ctx context.Context, in IndexerUpdate) error {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	u, e := endpoint(in.URL)
	if e != nil {
		return e
	}
	old, oldErr := s.indexerConfig(ctx)
	var missing *Error
	if oldErr != nil && !(errors.As(oldErr, &missing) && missing.Code == "indexer_not_configured") {
		return oldErr
	}
	key := old.APIKey
	if in.APIKey != nil {
		key = *in.APIKey
	}
	if len(key) > 2048 || strings.ContainsAny(key, "\r\n\x00") {
		return problem("invalid_settings", "Enter a valid indexer API key.")
	}
	if old.URL != "" && old.URL != u.String() && in.APIKey == nil {
		return problem("invalid_settings", "Re-enter the API key when changing the indexer URL.")
	}
	c := IndexerConfig{URL: u.String(), APIKey: key, Generation: ID()}
	if e = s.indexer.Test(ctx, c); e != nil {
		return e
	}
	b, e := json.Marshal(c)
	if e != nil {
		return e
	}
	return s.secrets.SetNamed(ctx, "torznab", b)
}
func (s *Service) ConfigureClient(ctx context.Context, in ClientUpdate) error {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	u, e := endpoint(in.URL)
	if e != nil {
		return e
	}
	remote, e := remoteRoot(in.DownloadPath)
	if e != nil {
		return e
	}
	old, oldErr := s.clientConfig(ctx)
	var missing *Error
	if oldErr != nil && !(errors.As(oldErr, &missing) && missing.Code == "client_not_configured") {
		return oldErr
	}
	password := old.Password
	if in.Password != nil {
		password = *in.Password
	}
	if !textOK(in.Username, 200) || !textOK(password, 2048) {
		return problem("invalid_settings", "Enter your qBittorrent Web UI username and password.")
	}
	if old.URL != "" && (old.URL != u.String() || old.Username != in.Username) && in.Password == nil {
		return problem("invalid_settings", "Re-enter the password when changing the qBittorrent URL or username.")
	}
	if old.URL != "" && (old.URL != u.String() || old.DownloadPath != remote) {
		var active int
		if e = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM downloads WHERE protocol='torrent' AND client_key='' AND state NOT IN ('completed','cancelled')`).Scan(&active); e != nil {
			return e
		}
		if active > 0 {
			return problem("active_downloads", "Finish or stop tracking active downloads before changing the client URL or download root.")
		}
	}
	c := ClientConfig{URL: u.String(), Username: in.Username, Password: password, DownloadPath: remote}
	if _, e = s.client.Test(ctx, c); e != nil {
		return e
	}
	b, e := json.Marshal(c)
	if e != nil {
		return e
	}
	return s.secrets.SetNamed(ctx, "qbittorrent", b)
}
func (s *Service) Test(ctx context.Context, name string) error {
	if name == "sabnzbd" {
		c, e := s.sabConfig(ctx)
		if e != nil {
			return e
		}
		return s.sab.Test(ctx, c)
	}
	if name == "torznab" {
		c, e := s.indexerConfig(ctx)
		if e != nil {
			return e
		}
		return s.indexer.Test(ctx, c)
	}
	c, e := s.clientConfig(ctx)
	if e != nil {
		return e
	}
	_, e = s.client.Test(ctx, c)
	return e
}
func (s *Service) Remove(ctx context.Context, name string) error {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	if name == "qbittorrent" || name == "sabnzbd" {
		protocol := "torrent"
		if name == "sabnzbd" {
			protocol = "usenet"
		}
		var active int
		if e := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM downloads WHERE protocol=? AND client_key='' AND state NOT IN ('completed','cancelled')`, protocol).Scan(&active); e != nil {
			return e
		}
		if active > 0 {
			return problem("active_downloads", "Finish or stop tracking this client's active downloads before disconnecting it.")
		}
	}
	return s.secrets.DeleteNamed(ctx, name)
}
func (s *Service) Search(ctx context.Context, movieID int64, query string) ([]Release, error) {
	var title string
	var year *int
	if e := s.db.QueryRowContext(ctx, `SELECT title,year FROM media_items WHERE id=? AND kind='movie'`, movieID).Scan(&title, &year); e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return nil, problem("movie_not_found", "Choose a movie already in your library.")
		}
		return nil, e
	}
	query = strings.TrimSpace(query)
	if query == "" {
		query = title
		if year != nil {
			b, _ := json.Marshal(*year)
			query += " " + string(b)
		}
	}
	if !textOK(query, 200) {
		return nil, problem("invalid_search", "Enter a release search with 1–200 characters.")
	}
	candidates, e := s.searchIndexers(ctx, query, "2000")
	if e != nil {
		return nil, e
	}
	out := make([]Release, 0, len(candidates))
	now := time.Now().Unix()
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = organization.EnsureMediaAvailableTx(ctx, tx); e != nil {
		return nil, e
	}
	if _, e = tx.ExecContext(ctx, `DELETE FROM release_candidates WHERE id NOT IN (SELECT release_id FROM downloads) AND (movie_id=? OR expires_at<?)`, movieID, now); e != nil {
		return nil, e
	}
	for _, v := range candidates {
		v.ID = ID()
		v.MovieID = movieID
		b, e := json.Marshal(v)
		if e != nil {
			return nil, e
		}
		sealed, e := s.secrets.Seal("release", b)
		if e != nil {
			return nil, e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO release_candidates(id,movie_id,title,size_bytes,seeders,peers,published_at,payload,expires_at) VALUES(?,?,?,?,?,?,?,?,?)`, v.ID, movieID, v.Title, v.Size, v.Seeders, v.Peers, v.Published, sealed, now+1800); e != nil {
			return nil, e
		}
		out = append(out, v.Release)
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return out, nil
}
func (s *Service) Queue(ctx context.Context, releaseID string) (Job, bool, error) {
	return s.queue(ctx, releaseID, nil)
}

// QueueGuard runs inside the reservation transaction, after fetching metainfo.
// Automation uses it to fence a paused/expired run and changed target policy.
type QueueGuard func(context.Context, *sql.Tx, Candidate) error

func (s *Service) QueueAutomatic(ctx context.Context, releaseID string, guard QueueGuard) (Job, bool, error) {
	if guard == nil {
		return Job{}, false, problem("invalid_release", "Automatic downloads require a current search policy.")
	}
	return s.queue(ctx, releaseID, guard)
}
func (s *Service) QueueReplacement(ctx context.Context, releaseID string, fileID int64) (Job, bool, error) {
	if fileID < 1 {
		return Job{}, false, problem("invalid_file", "Choose a registered replacement target.")
	}
	return s.queue(ctx, releaseID, nil, fileID)
}
func (s *Service) queue(ctx context.Context, releaseID string, guard QueueGuard, replacement ...int64) (Job, bool, error) {
	var replacementID int64
	if len(replacement) > 0 {
		replacementID = replacement[0]
	}
	version := ""
	if replacementID > 0 {
		version = "replacement"
	}
	return s.queueVersion(ctx, releaseID, guard, replacementID, version)
}
func (s *Service) queueVersion(ctx context.Context, releaseID string, guard QueueGuard, replacementID int64, version string) (Job, bool, error) {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	if existing, e := s.jobBy(ctx, "d.release_id", releaseID); e == nil {
		if existing.Kind == "series" && version != "" && version != "replacement" {
			return Job{}, false, problem("invalid_version", "Movie versions cannot target TV episodes.")
		}
		if guard != nil && existing.AnimeOrder != nil && (existing.AnimeSearch == nil || existing.AnimeSearch.Mode != "mapped") {
			return Job{}, false, problem("manual_anime_selection", "Reviewed Anime packs and specials require an explicit manual download choice.")
		}
		if version != "" && existing.Kind != "series" && existing.MovieVersion != version {
			return Job{}, false, problem("duplicate_release", "This release is already attached to a different movie version. Choose another release.")
		}
		if e := s.checkReplacementSelection(ctx, existing.ID, replacementID); e != nil {
			return Job{}, false, e
		}
		return existing, false, nil
	} else if !errors.Is(e, sql.ErrNoRows) {
		return Job{}, false, e
	}
	var sealed []byte
	var expires int64
	if e := s.db.QueryRowContext(ctx, `SELECT payload,expires_at FROM release_candidates WHERE id=?`, releaseID).Scan(&sealed, &expires); e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return Job{}, false, problem("release_expired", "Search again to get a fresh release selection.")
		}
		return Job{}, false, e
	}
	if expires < time.Now().Unix() {
		return Job{}, false, problem("release_expired", "This release selection expired. Search again.")
	}
	b, e := s.secrets.Open("release", sealed)
	if e != nil {
		return Job{}, false, e
	}
	var v Candidate
	if e = json.Unmarshal(b, &v); e != nil {
		return Job{}, false, e
	}
	if v.ID != releaseID {
		return Job{}, false, problem("invalid_release", "The encrypted release selection does not match its record. Search again.")
	}
	if guard != nil && v.AnimeOrder != nil && (v.AnimeSearch == nil || v.AnimeSearch.Mode != "mapped") {
		return Job{}, false, problem("manual_anime_selection", "Reviewed Anime packs and specials cannot be queued automatically.")
	}
	var protocol string
	var client ClientConfig
	var payload Payload
	var hash string
	if v.Manual {
		// A manually uploaded torrent carries its own metainfo: no indexer is
		// involved, so generation fencing and network fetch do not apply.
		if v.Generation != "" || len(v.Torrent) == 0 || v.Hash == "" || v.Protocol != "" && v.Protocol != "torrent" {
			return Job{}, false, problem("invalid_release", "The saved manual torrent is incomplete. Upload it again.")
		}
		protocol = "torrent"
		v.Protocol = protocol
		payload = Payload{Torrent: v.Torrent}
		hash = v.Hash
	} else {
		indexer, e := s.candidateIndexer(ctx, v)
		if e != nil {
			return Job{}, false, e
		}
		if v.Generation != indexer.Generation {
			return Job{}, false, problem("release_expired", "The indexer settings changed. Search again before downloading.")
		}
		protocol = "torrent"
		if indexer.Kind == "newznab" {
			protocol = "usenet"
		}
		if v.Protocol != "" && v.Protocol != protocol {
			return Job{}, false, problem("invalid_release", "The release protocol does not match its indexer. Search again.")
		}
		v.Protocol = protocol
		client, e = s.downloadConfig(ctx, protocol)
		if e != nil {
			return Job{}, false, e
		}
		adapter := s.adapter(indexer)
		if adapter == nil {
			return Job{}, false, problem("release_expired", "The source adapter is unavailable. Search again.")
		}
		payload, hash, e = adapter.Fetch(ctx, indexer, v)
		if e != nil {
			return Job{}, false, e
		}
	}
	if client, e = s.downloadConfig(ctx, protocol); e != nil {
		return Job{}, false, e
	}
	if existing, e := s.jobBy(ctx, "d.info_hash", hash); e == nil {
		if existing.Kind == "series" && version != "" && version != "replacement" {
			return Job{}, false, problem("invalid_version", "Movie versions cannot target TV episodes.")
		}
		if existing.MovieID != v.MovieID || (v.Kind == "series" && (existing.Kind != "series" || !sameTargets(existing.Targets, v.Targets))) {
			return Job{}, false, problem("duplicate_hash", "This torrent is already attached to a different target selection. Open its existing Activity job or choose another release.")
		}
		if version != "" && existing.Kind != "series" && existing.MovieVersion != version {
			return Job{}, false, problem("duplicate_release", "This release is already attached to a different movie version. Choose another release.")
		}
		if e := s.checkReplacementSelection(ctx, existing.ID, replacementID); e != nil {
			return Job{}, false, e
		}
		return existing, false, nil
	} else if !errors.Is(e, sql.ErrNoRows) {
		return Job{}, false, e
	}
	b, e = json.Marshal(payload)
	if e != nil {
		return Job{}, false, e
	}
	sealed, e = s.secrets.Seal("download", b)
	if e != nil {
		return Job{}, false, e
	}
	if v.Kind == "series" {
		if version != "" && version != "replacement" {
			return Job{}, false, problem("invalid_version", "Movie versions cannot target TV episodes.")
		}
		return s.queueTV(ctx, v, releaseID, sealed, hash, client, guard, replacementID)
	}
	if _, e = s.MovieByID(ctx, v.MovieID); e != nil {
		return Job{}, false, e
	}
	id := ID()
	save := path.Join(client.DownloadPath, "ReelScarey", id)
	now := time.Now().Unix()
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return Job{}, false, e
	}
	defer tx.Rollback()
	if e = organization.EnsureMediaAvailableTx(ctx, tx); e != nil {
		return Job{}, false, e
	}
	if e = validateSelectedClient(ctx, tx, client); e != nil {
		return Job{}, false, e
	}
	if guard != nil {
		if e = guard(ctx, tx, v); e != nil {
			return Job{}, false, e
		}
	}
	if e = reserveMovieVersion(ctx, tx, v.MovieID, version, v.Title, "", replacementID); e != nil {
		return Job{}, false, e
	}
	naming, e := organization.New(s.db, s.cfg).CaptureTx(ctx, tx, v.MovieID)
	if e != nil {
		return Job{}, false, e
	}
	namingJSON, e := json.Marshal(naming)
	if e != nil {
		return Job{}, false, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO downloads(id,movie_id,release_id,title,info_hash,payload,client_url,save_path,state,created_at,updated_at,indexer_name,protocol,organization_snapshot,movie_version) VALUES(?,?,?,?,?,?,?,?,'queued',?,?,?,?,?,?)`, id, v.MovieID, releaseID, v.Title, hash, sealed, client.URL, save, now, now, v.IndexerName, protocol, string(namingJSON), version); e != nil {
		return Job{}, false, e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE downloads SET client_key=?,client_kind=?,client_label=?,client_download_root=?,client_local_folder=? WHERE id=?`, client.profileKey, client.kind, client.label, client.DownloadPath, client.LocalFolder, id); e != nil {
		return Job{}, false, e
	}
	if replacementID > 0 {
		if e = organization.ReserveAcquisition(ctx, tx, "download", id, replacementID, v.MovieID, nil); e != nil {
			return Job{}, false, e
		}
	}
	if _, e = tx.ExecContext(ctx, `UPDATE media_items SET status='downloading' WHERE id=? AND status<>'available'`, v.MovieID); e != nil {
		return Job{}, false, e
	}
	if e = tx.Commit(); e != nil {
		return Job{}, false, e
	}
	job, e := s.Job(ctx, id)
	return job, true, e
}

// MovieByID is the catalog read used by the release browser; it does not need a
// fresh TMDb request and remains usable during provider outages.
func (s *Service) MovieByID(ctx context.Context, id int64) (library.Item, error) {
	return library.New(s.db).ByID(ctx, id)
}
