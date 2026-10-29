package transfer

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/faaaaq/ReelScarey/internal/library"
	"github.com/faaaaq/ReelScarey/internal/organization"
)

const maxDiscoveryFiles = 10000
const maxDiscoveryDirs = 2000
const maxDirectoryEntries = 20000

type LibraryScan struct {
	ID             string `json:"id"`
	Kind           string `json:"kind"`
	MediaRoot      string `json:"media_root"`
	ResolvedRoot   string `json:"-"`
	Folder         string `json:"folder"`
	State          string `json:"state"`
	Message        string `json:"message"`
	Created        int64  `json:"created_at"`
	Updated        int64  `json:"updated_at"`
	Discovered     int    `json:"discovered"`
	Registered     int    `json:"registered"`
	Queued         int    `json:"queued"`
	NeedsMatch     int    `json:"needs_match"`
	NeedsAttention int    `json:"needs_attention"`
	Examined       int    `json:"examined"`
	Skipped        int    `json:"skipped"`
	owner          string
}

const libraryScanColumns = `id,kind,media_root,resolved_root,folder,state,message,created_at,updated_at,lease_owner`

func scanLibraryScan(row scanner) (LibraryScan, error) {
	var s LibraryScan
	e := row.Scan(&s.ID, &s.Kind, &s.MediaRoot, &s.ResolvedRoot, &s.Folder, &s.State, &s.Message, &s.Created, &s.Updated, &s.owner)
	return s, e
}
func (s *Service) LibraryScan(ctx context.Context, id string) (LibraryScan, error) {
	out, e := scanLibraryScan(s.db.QueryRowContext(ctx, `SELECT `+libraryScanColumns+` FROM library_scans WHERE id=?`, id))
	if e != nil {
		return out, e
	}
	e = s.db.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(state='registered'),0),COALESCE(SUM(state IN ('queued','verifying')),0),COALESCE(SUM(state='review'),0),COALESCE(SUM(state='blocked'),0) FROM library_scan_files WHERE scan_id=?`, id).Scan(&out.Discovered, &out.Registered, &out.Queued, &out.NeedsMatch, &out.NeedsAttention)
	if e != nil {
		return out, e
	}
	e = s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(examined),0),COALESCE(SUM(skipped),0) FROM library_scan_dirs WHERE scan_id=?`, id).Scan(&out.Examined, &out.Skipped)
	return out, e
}
func (s *Service) LibraryScans(ctx context.Context) ([]LibraryScan, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT id FROM library_scans ORDER BY created_at DESC,id DESC LIMIT 20`)
	if e != nil {
		return nil, e
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return nil, e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	out := []LibraryScan{}
	for _, id := range ids {
		v, e := s.LibraryScan(ctx, id)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, nil
}
func (s *Service) existingRoot(ctx context.Context, scan *LibraryScan) (*os.Root, error) {
	resolved, e := filepath.EvalSymlinks(s.cfg.MediaDir)
	if e != nil {
		return nil, problem("storage_unavailable", "The configured media root is unavailable on this server.")
	}
	for _, other := range []string{s.cfg.ConfigDir, s.cfg.DownloadsDir} {
		resolvedOther, e := filepath.EvalSymlinks(other)
		if e != nil {
			resolvedOther = filepath.Clean(other)
		}
		if overlap(resolved, resolvedOther) || overlap(resolvedOther, resolved) {
			return nil, problem("unsafe_roots", "Media, downloads and configuration must use separate folders.")
		}
	}
	if scan != nil && (scan.MediaRoot != s.cfg.MediaDir || scan.ResolvedRoot != resolved) {
		return nil, problem("source_changed", "The configured media root changed. Start a new scan before registering files.")
	}
	if e = ctx.Err(); e != nil {
		return nil, e
	}
	return os.OpenRoot(s.cfg.MediaDir)
}
func (s *Service) StartLibraryScan(ctx context.Context, kind string, collection ...string) (LibraryScan, error) {
	defer organization.ReadMedia()()
	if e := organization.EnsureMediaAvailable(ctx, s.db); e != nil {
		return LibraryScan{}, e
	}
	if kind != "movie" && kind != "series" {
		return LibraryScan{}, problem("invalid_scan", "Choose movies or TV shows.")
	}
	root, e := s.existingRoot(ctx, nil)
	if e != nil {
		return LibraryScan{}, e
	}
	defer root.Close()
	prefs, e := organization.New(s.db, s.cfg).Get(ctx)
	if e != nil {
		return LibraryScan{}, e
	}
	key := "movies"
	if kind == "series" {
		key = "tv"
	}
	if len(collection) > 1 {
		return LibraryScan{}, problem("invalid_scan", "Choose one configured collection.")
	}
	if len(collection) == 1 && collection[0] != "" {
		key = collection[0]
	}
	folder := ""
	for _, l := range prefs.Libraries {
		if l.Key == key && (l.Content == "mixed" || l.Content == kind) {
			folder = l.Folder
			if l.Content == "mixed" {
				if kind == "movie" {
					folder = path.Join(folder, "Movies")
				} else {
					folder = path.Join(folder, "TV Shows")
				}
			}
		}
	}
	if folder == "" {
		return LibraryScan{}, problem("invalid_scan", "Choose a configured collection compatible with movies or shows.")
	}
	if e = segments(root, folder, false); e != nil {
		return LibraryScan{}, problem("storage_unavailable", "The selected movie or TV folder must already exist inside the media root. Check its mount and folder preference.")
	}
	dir, e := root.OpenRoot(folder)
	if e != nil {
		return LibraryScan{}, problem("storage_unavailable", "The selected library folder cannot be read.")
	}
	dir.Close()
	resolved, e := filepath.EvalSymlinks(s.cfg.MediaDir)
	if e != nil {
		return LibraryScan{}, e
	}
	id := ID()
	now := time.Now().Unix()
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return LibraryScan{}, e
	}
	defer tx.Rollback()
	if e = organization.EnsureMediaAvailableTx(ctx, tx); e != nil {
		return LibraryScan{}, e
	}
	var active int
	if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM library_scans WHERE state IN ('queued','scanning')`).Scan(&active); e != nil {
		return LibraryScan{}, e
	}
	if active > 0 {
		return LibraryScan{}, problem("scan_busy", "A library scan is already running. Wait for it or stop it first.")
	}
	// Keep bounded history without deleting pending registrations or registered files.
	var count int
	if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM library_scans`).Scan(&count); e != nil {
		return LibraryScan{}, e
	}
	if count >= 20 {
		return LibraryScan{}, problem("scan_history_full", "The scan history is full. Remove a finished scan from the review list before starting another.")
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO library_scans(id,kind,media_root,resolved_root,folder,state,created_at,updated_at) VALUES(?,?,?,?,?,'queued',?,?)`, id, kind, s.cfg.MediaDir, resolved, folder, now, now); e != nil {
		return LibraryScan{}, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO library_scan_dirs(scan_id,relative_path) VALUES(?,?)`, id, folder); e != nil {
		return LibraryScan{}, e
	}
	if e = tx.Commit(); e != nil {
		return LibraryScan{}, e
	}
	return s.LibraryScan(ctx, id)
}
func (s *Service) CancelLibraryScan(ctx context.Context, id string) error {
	res, e := s.db.ExecContext(ctx, `UPDATE library_scans SET state='cancelled',message='Discovery stopped. Reviewed registrations continue independently.',lease_owner='',lease_until=0,updated_at=unixepoch() WHERE id=? AND state IN ('queued','scanning')`, id)
	if e != nil {
		return e
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return problem("scan_busy", "This scan is no longer running. Reload its status.")
	}
	return nil
}
func (s *Service) RemoveLibraryScan(ctx context.Context, id string) error {
	defer organization.ReadMedia()()
	if e := organization.EnsureMediaAvailable(ctx, s.db); e != nil {
		return e
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = organization.EnsureMediaAvailableTx(ctx, tx); e != nil {
		return e
	}
	var active int
	if e = tx.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM library_scans WHERE id=? AND (state IN ('queued','scanning') OR lease_until>=unixepoch()))+(SELECT COUNT(*) FROM library_scan_files WHERE scan_id=? AND (state IN ('queued','verifying') OR lease_until>=unixepoch()))`, id, id).Scan(&active); e != nil {
		return e
	}
	if active > 0 {
		return problem("scan_busy", "Wait for active discovery and file verification before removing this scan.")
	}
	res, e := tx.ExecContext(ctx, `DELETE FROM library_scans WHERE id=?`, id)
	if e != nil {
		return e
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return sql.ErrNoRows
	}
	return tx.Commit()
}

type DiscoveredFile struct {
	ID          string `json:"id"`
	ScanID      string `json:"scan_id"`
	Path        string `json:"path"`
	FullPath    string `json:"full_path"`
	Hint        string `json:"hint"`
	Size        int64  `json:"size_bytes"`
	Mtime       int64  `json:"mtime"`
	State       string `json:"state"`
	MediaID     *int64 `json:"media_id"`
	MediaFileID *int64 `json:"media_file_id"`
	Message     string `json:"message"`
	targets     []TVTarget
	owner       string
	identity    string
}

const discoveredColumns = `id,scan_id,relative_path,size_bytes,mtime,state,media_id,media_file_id,message,targets,lease_owner,file_identity`

func scanDiscovered(row scanner) (DiscoveredFile, error) {
	var f DiscoveredFile
	var targets string
	e := row.Scan(&f.ID, &f.ScanID, &f.Path, &f.Size, &f.Mtime, &f.State, &f.MediaID, &f.MediaFileID, &f.Message, &targets, &f.owner, &f.identity)
	if e == nil {
		e = json.Unmarshal([]byte(targets), &f.targets)
	}
	return f, e
}

var hintYear = regexp.MustCompile(`\b(?:19|20)[0-9]{2}\b`)
var hintQuality = regexp.MustCompile(`(?i)\b(?:2160p|1080p|720p|480p|bluray|bdrip|webrip|web-dl|x264|x265|h264|h265)\b`)

func discoveryHint(name, kind string) string {
	base := strings.TrimSuffix(path.Base(name), path.Ext(name))
	if kind == "series" {
		if token := episodeToken.FindStringIndex(base); token != nil {
			base = base[:token[0]]
		}
	}
	if i := hintYear.FindStringIndex(base); i != nil {
		base = base[:i[0]]
	} else if i := hintQuality.FindStringIndex(base); i != nil {
		base = base[:i[0]]
	}
	base = strings.NewReplacer(".", " ", "_", " ", "(", " ", ")", " ").Replace(base)
	base = strings.Join(strings.Fields(base), " ")
	if base == "" {
		base = path.Base(path.Dir(name))
	}
	if len(base) > 200 {
		base = base[:200]
	}
	return base
}

type DiscoveredPage struct {
	Items []DiscoveredFile `json:"items"`
	Page  int              `json:"page"`
	Total int              `json:"total"`
	Limit int              `json:"limit"`
	Scan  LibraryScan      `json:"scan"`
}

func (s *Service) DiscoveredFiles(ctx context.Context, id string, page int, state string) (DiscoveredPage, error) {
	out := DiscoveredPage{Items: []DiscoveredFile{}, Page: page, Limit: 50}
	scan, e := s.LibraryScan(ctx, id)
	out.Scan = scan
	if e != nil {
		return out, e
	}
	if page < 1 || page > 100000 || (state != "" && state != "review" && state != "registered" && state != "blocked" && state != "skipped") {
		return out, problem("invalid_scan", "Choose a valid scan page and filter.")
	}
	if e = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM library_scan_files WHERE scan_id=? AND (?='' OR state=?)`, id, state, state).Scan(&out.Total); e != nil {
		return out, e
	}
	rows, e := s.db.QueryContext(ctx, `SELECT `+discoveredColumns+` FROM library_scan_files WHERE scan_id=? AND (?='' OR state=?) ORDER BY relative_path LIMIT 50 OFFSET ?`, id, state, state, (page-1)*50)
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		f, e := scanDiscovered(rows)
		if e != nil {
			return out, e
		}
		if _, e = safeRelative(f.Path); e != nil {
			return out, e
		}
		f.FullPath = filepath.Join(scan.MediaRoot, filepath.FromSlash(f.Path))
		f.Hint = discoveryHint(f.Path, scan.Kind)
		out.Items = append(out.Items, f)
	}
	return out, rows.Err()
}
func (s *Service) SkipDiscovered(ctx context.Context, id string) error {
	defer organization.ReadMedia()()
	if e := organization.EnsureMediaAvailable(ctx, s.db); e != nil {
		return e
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = organization.EnsureMediaAvailableTx(ctx, tx); e != nil {
		return e
	}
	res, e := tx.ExecContext(ctx, `UPDATE library_scan_files SET state='skipped',message='Skipped during review.',updated_at=unixepoch() WHERE id=? AND state IN ('review','blocked') AND lease_until<unixepoch()`, id)
	if e != nil {
		return e
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return problem("scan_busy", "Only unregistered files awaiting review can be skipped.")
	}
	return tx.Commit()
}

type ExistingRegistration struct {
	MediaID  int64   `json:"media_id"`
	Episodes []int64 `json:"episode_ids"`
	Reviewed bool    `json:"reviewed"`
}

func (s *Service) RegisterDiscovered(ctx context.Context, id string, in ExistingRegistration) error {
	defer organization.ReadMedia()()
	if e := organization.EnsureMediaAvailable(ctx, s.db); e != nil {
		return e
	}
	if !in.Reviewed || in.MediaID < 1 || len(in.Episodes) > 20 {
		return problem("invalid_mapping", "Review the title and episode match before adding this file.")
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = organization.EnsureMediaAvailableTx(ctx, tx); e != nil {
		return e
	}
	if e = s.registerDiscoveredTx(ctx, tx, id, in, false); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Service) registerDiscoveredTx(ctx context.Context, tx *sql.Tx, id string, in ExistingRegistration, dryRun bool) error {
	if !in.Reviewed || in.MediaID < 1 || len(in.Episodes) > 20 {
		return mappingError()
	}
	f, e := scanDiscovered(tx.QueryRowContext(ctx, `SELECT `+discoveredColumns+` FROM library_scan_files WHERE id=?`, id))
	if e != nil {
		return e
	}
	if f.State == "registered" {
		same := f.MediaID != nil && *f.MediaID == in.MediaID && len(f.targets) == len(in.Episodes)
		ids := map[int64]bool{}
		for _, id := range in.Episodes {
			ids[id] = true
		}
		if len(ids) != len(in.Episodes) {
			same = false
		}
		for _, t := range f.targets {
			if !ids[t.EpisodeID] {
				same = false
			}
		}
		if same {
			return nil
		}
		return problem("duplicate_file", "This file is already registered with a different match.")
	}
	if f.State != "review" && f.State != "blocked" {
		return problem("scan_busy", "This file is already queued, skipped or being verified.")
	}
	var kind, targetKind string
	if e = tx.QueryRowContext(ctx, `SELECT kind FROM library_scans WHERE id=?`, f.ScanID).Scan(&kind); e != nil {
		return e
	}
	if e = tx.QueryRowContext(ctx, `SELECT kind FROM media_items WHERE id=?`, in.MediaID).Scan(&targetKind); e != nil {
		return e
	}
	if kind != targetKind {
		return problem("invalid_mapping", "Choose a catalog title of the same type as this scan.")
	}
	targets := []TVTarget{}
	seen := map[int64]bool{}
	for _, id := range in.Episodes {
		if seen[id] {
			return mappingError()
		}
		seen[id] = true
		var t TVTarget
		if e = tx.QueryRowContext(ctx, `SELECT e.id,e.tmdb_id,s.number,e.number,e.title FROM episodes e JOIN seasons s ON s.id=e.season_id WHERE e.id=? AND s.series_id=? AND s.loaded_at<>''`, id, in.MediaID).Scan(&t.EpisodeID, &t.TMDbID, &t.Season, &t.Number, &t.Title); e != nil {
			return problem("invalid_mapping", "Load the selected show's season metadata and choose its episodes.")
		}
		targets = append(targets, t)
	}
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].Season != targets[j].Season {
			return targets[i].Season < targets[j].Season
		}
		return targets[i].Number < targets[j].Number
	})
	if (kind == "movie" && len(targets) != 0) || (kind == "series" && len(targets) == 0) {
		return mappingError()
	}
	for n, t := range targets {
		if n > 0 && (t.Season != targets[0].Season || t.Number != targets[n-1].Number+1) {
			return problem("invalid_mapping", "One file can cover consecutive episodes from one season, up to 20 episodes.")
		}
	}
	if e = existingTargetFree(ctx, tx, in.MediaID, kind, targets); e != nil {
		return e
	}
	payload, e := json.Marshal(targets)
	if e != nil {
		return e
	}
	if dryRun {
		return nil
	}
	res, e := tx.ExecContext(ctx, `UPDATE library_scan_files SET state='queued',media_id=?,targets=?,message='Waiting for file verification.',updated_at=unixepoch() WHERE id=? AND state IN ('review','blocked') AND lease_until<unixepoch()`, in.MediaID, string(payload), id)
	if e != nil {
		return e
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return problem("scan_busy", "The file is still finishing a step. Reload and try again.")
	}
	return nil
}
func existingTargetFree(ctx context.Context, tx *sql.Tx, id int64, kind string, targets []TVTarget) error {
	var count int
	if kind == "movie" {
		if e := tx.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM media_files WHERE movie_id=?)+(SELECT COUNT(*) FROM downloads WHERE movie_id=? AND state NOT IN ('completed','cancelled'))+(SELECT COUNT(*) FROM youtube_jobs WHERE media_id=? AND target_kind='movie' AND state NOT IN ('completed','cancelled','failed'))`, id, id, id).Scan(&count); e != nil {
			return e
		}
		if count > 0 {
			return problem("movie_busy", "This movie already has a registered file or active download. Stop tracking its download first or choose another title.")
		}
		return nil
	}
	for _, t := range targets {
		if e := validateTVTarget(ctx, tx, id, t); e != nil {
			return e
		}
		if e := tx.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM episode_files WHERE episode_id=?)+(SELECT COUNT(*) FROM episode_downloads WHERE episode_id=?)+(SELECT COUNT(*) FROM youtube_jobs WHERE episode_id=? AND target_kind='episode' AND state NOT IN ('completed','cancelled','failed'))`, t.EpisodeID, t.EpisodeID, t.EpisodeID).Scan(&count); e != nil {
			return e
		}
		if count > 0 {
			return problem("episode_busy", "An episode is already registered or reserved by a download. Review that episode first.")
		}
	}
	return nil
}

func (s *Service) ImportCatalog(ctx context.Context, kind, query string) ([]library.Item, error) {
	if (kind != "movie" && kind != "series") || len(query) > 200 {
		return nil, problem("invalid_search", "Choose a media type and a title search of up to 200 characters.")
	}
	rows, e := s.db.QueryContext(ctx, `SELECT id FROM media_items WHERE kind=? AND (?='' OR instr(lower(title),lower(?))>0) ORDER BY title,id LIMIT 20`, kind, strings.TrimSpace(query), strings.TrimSpace(query))
	if e != nil {
		return nil, e
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return nil, e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	out := []library.Item{}
	for _, id := range ids {
		item, e := library.New(s.db).Item(ctx, id)
		if e != nil {
			return nil, e
		}
		out = append(out, item)
	}
	return out, nil
}

func registeredMessage(e error) string {
	var p *Error
	if errors.As(e, &p) {
		return p.Message
	}
	return "This file could not be registered. Check media access and retry from review."
}
