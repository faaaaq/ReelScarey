package transfer

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/faaaaq/ReelScarey/internal/organization"
)

// A direct import lets the owner point at one specific video that already
// lives inside a configured library root and register it in place for a chosen
// catalog title. It is the most explicit existing-library selection possible:
// the owner chooses both the file and the identity, exactly like an uploaded
// .torrent is its own release review. The path never arrives as an absolute
// client value — the request names one of the configured scan roots and a
// slash path below it, resolved server-side against the media root and
// refused if it leaves that root. The explicit reviewed confirmation and the
// durable verification worker are the same ones every discovered file goes
// through; nothing is copied, moved, renamed or replaced, and no download or
// release row is fabricated.

const maxDirectImportEntries = 20000

type DirectImportEntry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	Dir   bool   `json:"dir"`
	Size  int64  `json:"size_bytes,omitempty"`
	Mtime int64  `json:"mtime_ns,omitempty"`
}

type DirectImportBrowse struct {
	Roots []string            `json:"roots"`
	Dir   string              `json:"dir"`
	Items []DirectImportEntry `json:"items"`
}

// directImportScope validates the requested collection for the media kind and
// returns its key and configured scan roots, mirroring the scope rules that
// discovery scans enforce.
func (s *Service) directImportScope(ctx context.Context, kind, collection string) ([]string, error) {
	if kind != "movie" && kind != "series" {
		return nil, problem("invalid_scan", "Choose movies or TV shows.")
	}
	key := "movies"
	if kind == "series" {
		key = "tv"
	}
	if strings.TrimSpace(collection) != "" {
		key = strings.TrimSpace(collection)
	}
	prefs, e := organization.New(s.db, s.cfg).Get(ctx)
	if e != nil {
		return nil, e
	}
	var library organization.Library
	found := false
	for _, l := range prefs.Libraries {
		if l.Key == key {
			library, found = l, true
			break
		}
	}
	if !found {
		return nil, problem("invalid_scan", "Choose a configured collection compatible with movies or shows.")
	}
	if key == "anime" && kind != "series" {
		return nil, problem("invalid_scan", "The Anime collection holds TV shows, not movies.")
	}
	if key != "anime" && library.Content != "mixed" && library.Content != kind {
		return nil, problem("invalid_scan", "Choose a configured collection compatible with movies or shows.")
	}
	roots, e := organization.New(s.db, s.cfg).EffectiveRoots(ctx, key, kind)
	if e != nil {
		return nil, e
	}
	if len(roots) == 0 {
		return nil, problem("storage_unavailable", "This library has no scan roots. Add one in library roots first.")
	}
	return roots, nil
}

func normalizeDirectDir(dir string) string {
	dir = strings.TrimSpace(strings.ReplaceAll(dir, "\\", "/"))
	for len(dir) > 1 && strings.HasSuffix(dir, "/") {
		dir = strings.TrimSuffix(dir, "/")
	}
	return dir
}

// DirectImportBrowse lists one folder inside a configured library root. An
// empty dir lists the configured roots themselves. Every returned path is
// media-root relative and slash separated; browsing never follows symbolic
// links and never leaves the configured roots.
func (s *Service) DirectImportBrowse(ctx context.Context, kind, collection, dir string) (DirectImportBrowse, error) {
	defer organization.ReadMedia()()
	if e := organization.EnsureMediaAvailable(ctx, s.db); e != nil {
		return DirectImportBrowse{}, e
	}
	roots, e := s.directImportScope(ctx, kind, collection)
	if e != nil {
		return DirectImportBrowse{}, e
	}
	root, e := s.existingRoot(ctx, nil)
	if e != nil {
		return DirectImportBrowse{}, e
	}
	defer root.Close()
	dir = normalizeDirectDir(dir)
	out := DirectImportBrowse{Roots: roots, Dir: dir, Items: []DirectImportEntry{}}
	if dir == "" {
		for _, r := range roots {
			if _, e := root.Lstat(filepath.FromSlash(r)); e != nil {
				continue
			}
			out.Items = append(out.Items, DirectImportEntry{Name: path.Base(r), Path: r, Dir: true})
		}
		return out, nil
	}
	if directImportContainingRoot(roots, dir) == "" {
		return DirectImportBrowse{}, problem("unsafe_path", "Choose a folder inside one of this library's configured roots.")
	}
	if e = segments(root, dir, false); e != nil {
		return DirectImportBrowse{}, problem("unsafe_path", "The folder is missing, inaccessible or contains a symbolic link.")
	}
	count := 0
	folder, e := root.Open(filepath.FromSlash(dir))
	if e != nil {
		return DirectImportBrowse{}, problem("storage_unavailable", "The folder cannot be read.")
	}
	defer folder.Close()
	info, e := folder.Stat()
	if e != nil || !info.IsDir() {
		return DirectImportBrowse{}, problem("unsafe_path", "The selected folder is not a directory.")
	}
	// Bounded batches match the discovery walker's read pattern.
	for {
		entries, e := folder.ReadDir(128)
		batchErr := e
		for _, entry := range entries {
			name := entry.Name()
			if strings.HasPrefix(name, ".") || count >= maxDirectImportEntries {
				continue
			}
			relative := dir + "/" + name
			if entry.IsDir() {
				out.Items = append(out.Items, DirectImportEntry{Name: name, Path: relative, Dir: true})
				count++
				continue
			}
			detail, e := entry.Info()
			if e != nil || !detail.Mode().IsRegular() || !video(name) || extraVideo.MatchString(name) {
				continue
			}
			out.Items = append(out.Items, DirectImportEntry{Name: name, Path: relative, Size: detail.Size(), Mtime: detail.ModTime().UnixNano()})
			count++
		}
		if batchErr != nil {
			break
		}
	}
	return out, nil
}

// directImportContainingRoot returns the longest configured root that
// contains the slash-separated path, or "" when the path is outside every
// configured root.
func directImportContainingRoot(roots []string, relative string) string {
	best := ""
	for _, r := range roots {
		r = strings.TrimSuffix(r, "/")
		if r == "" {
			continue
		}
		if relative == r || strings.HasPrefix(relative, r+"/") {
			if len(r) > len(best) {
				best = r
			}
		}
	}
	return best
}

type DirectImportRequest struct {
	Kind       string  `json:"kind"`
	Collection string  `json:"collection"`
	MediaID    int64   `json:"media_id"`
	Episodes   []int64 `json:"episode_ids"`
	Root       string  `json:"root"`
	Relative   string  `json:"relative"`
	Reviewed   bool    `json:"reviewed"`
}

// QueueDirectImport queues one owner-chosen file's verified in-place
// registration for the selected title. The explicit reviewed confirmation is
// required, and the same target validation, bounded probe inspection and
// registration transaction apply as for discovered files.
func (s *Service) QueueDirectImport(ctx context.Context, in DirectImportRequest) (LibraryScan, error) {
	defer organization.ReadMedia()()
	if e := organization.EnsureMediaAvailable(ctx, s.db); e != nil {
		return LibraryScan{}, e
	}
	roots, e := s.directImportScope(ctx, in.Kind, in.Collection)
	if e != nil {
		return LibraryScan{}, e
	}
	if !in.Reviewed || in.MediaID < 1 || len(in.Episodes) > 20 {
		return LibraryScan{}, problem("invalid_mapping", "Confirm the title and episode match before importing this file.")
	}
	relative := strings.TrimSpace(strings.ReplaceAll(in.Relative, "\\", "/"))
	if relative == "" {
		return LibraryScan{}, problem("invalid_selection", "Choose a video file to import.")
	}
	if _, e = safeRelative(relative); e != nil {
		return LibraryScan{}, e
	}
	selected := normalizeDirectDir(in.Root)
	if selected != "" {
		known := false
		for _, r := range roots {
			if r == selected {
				known = true
				break
			}
		}
		if !known {
			return LibraryScan{}, problem("unsafe_path", "Choose one of this library's configured roots.")
		}
		if relative != selected && !strings.HasPrefix(relative, selected+"/") {
			return LibraryScan{}, problem("unsafe_path", "The file must live inside the selected configured root.")
		}
	} else if selected = directImportContainingRoot(roots, relative); selected == "" {
		return LibraryScan{}, problem("unsafe_path", "The file must live inside one of this library's configured roots.")
	}
	if relative == selected {
		return LibraryScan{}, problem("invalid_selection", "Choose a video file below the configured root.")
	}
	root, e := s.existingRoot(ctx, nil)
	if e != nil {
		return LibraryScan{}, e
	}
	defer root.Close()
	if e = segments(root, relative, false); e != nil {
		return LibraryScan{}, problem("unsafe_path", "The file is missing, inaccessible or contains a symbolic link.")
	}
	info, e := root.Lstat(filepath.FromSlash(relative))
	if e != nil {
		return LibraryScan{}, problem("source_unavailable", "The selected file is missing or inaccessible.")
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || !video(relative) || extraVideo.MatchString(relative) {
		return LibraryScan{}, problem("invalid_file", "Choose a complete, supported video file. Samples and trailers are not imported.")
	}
	var registered int
	if e = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM media_files WHERE relative_path=? COLLATE NOCASE`, relative).Scan(&registered); e != nil {
		return LibraryScan{}, e
	}
	if registered > 0 {
		return LibraryScan{}, problem("duplicate_file", "This file is already registered in the library.")
	}
	var pending int
	if e = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM library_scan_files WHERE relative_path=? AND state IN ('review','queued','verifying')`, relative).Scan(&pending); e != nil {
		return LibraryScan{}, e
	}
	if pending > 0 {
		return LibraryScan{}, problem("scan_busy", "This file already has a review or verification in progress.")
	}
	// The anchored identity check mirrors discovery: a stable filesystem
	// identity proves the owner reviewed this exact file.
	opened, e := openExistingFile(root, filepath.FromSlash(relative))
	if e != nil {
		return LibraryScan{}, problem("source_unavailable", "The selected file could not be opened.")
	}
	openedInfo, statErr := opened.Stat()
	identity, identityErr := existingIdentity(opened)
	opened.Close()
	if statErr != nil || identityErr != nil || !openedInfo.Mode().IsRegular() || !os.SameFile(info, openedInfo) || info.Size() != openedInfo.Size() || info.ModTime() != openedInfo.ModTime() {
		return LibraryScan{}, problem("source_changed", "The file changed while its identity was inspected. Review it again.")
	}
	// Only the title kind is pre-validated here; the reviewed registration
	// transaction below re-checks every target rule.
	var targetKind string
	if e = s.db.QueryRowContext(ctx, `SELECT kind FROM media_items WHERE id=?`, in.MediaID).Scan(&targetKind); e != nil {
		if e == sql.ErrNoRows {
			return LibraryScan{}, problem("invalid_mapping", "Choose a catalog title for this file.")
		}
		return LibraryScan{}, e
	}
	if targetKind != in.Kind {
		return LibraryScan{}, problem("invalid_mapping", "Choose a catalog title of the same type as this import.")
	}
	resolved, e := filepath.EvalSymlinks(s.cfg.MediaDir)
	if e != nil {
		return LibraryScan{}, e
	}
	encodedRoots, e := json.Marshal([]string{selected})
	if e != nil {
		return LibraryScan{}, e
	}
	scanID, fileID := ID(), ID()
	now := time.Now().Unix()
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return LibraryScan{}, e
	}
	defer tx.Rollback()
	if e = organization.EnsureMediaAvailableTx(ctx, tx); e != nil {
		return LibraryScan{}, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO library_scans(id,kind,media_root,resolved_root,folder,roots,state,message,created_at,updated_at) VALUES(?,?,?,?,?,?,'completed','Manual import review for one file.',?,?)`, scanID, in.Kind, s.cfg.MediaDir, resolved, selected, string(encodedRoots), now, now); e != nil {
		return LibraryScan{}, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO library_scan_files(id,scan_id,relative_path,size_bytes,mtime,file_identity,updated_at) VALUES(?,?,?,?,?,?,?)`, fileID, scanID, relative, info.Size(), info.ModTime().UnixNano(), identity, now); e != nil {
		return LibraryScan{}, e
	}
	// The reviewed identity goes through the exact reviewed-registration fence
	// a discovered file uses: loaded seasons, consecutive targets up to 20,
	// episode/movie reservation conflicts and the durable verification queue.
	if e = s.registerDiscoveredTx(ctx, tx, fileID, ExistingRegistration{MediaID: in.MediaID, Episodes: in.Episodes, Reviewed: true}, false); e != nil {
		return LibraryScan{}, e
	}
	if e = tx.Commit(); e != nil {
		return LibraryScan{}, e
	}
	return s.LibraryScan(ctx, scanID)
}
