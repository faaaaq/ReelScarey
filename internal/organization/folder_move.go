package organization

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"time"
)

// PreviewFolder snapshots an entire title folder, without changing any files.
// The review deliberately preserves its existing names and all nested extras.
func (s *Service) PreviewFolder(ctx context.Context, mediaID int64, target string) (Plan, error) {
	if mediaID < 1 || target == "" {
		return Plan{}, problem("invalid_selection", "Choose a title and destination collection.")
	}
	unlock := ReadMedia()
	defer unlock()
	root, resolved, err := s.openRoot("")
	if err != nil {
		return Plan{}, err
	}
	defer root.Close()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Plan{}, err
	}
	defer tx.Rollback()
	ids, err := titleFiles(ctx, tx, mediaID)
	if err != nil {
		return Plan{}, err
	}
	if len(ids) == 0 || len(ids) > 100 {
		return Plan{}, problem("invalid_selection", "A folder review requires 1–100 registered files.")
	}
	config, err := s.config(ctx, tx)
	if err != nil {
		return Plan{}, err
	}
	snapshot, err := s.capture(ctx, tx, mediaID, target)
	if err != nil {
		return Plan{}, err
	}
	files := make([]PlannedFile, 0, len(ids))
	paths := make([]string, 0, len(ids))
	for _, fileID := range ids {
		var v PlannedFile
		if err = tx.QueryRowContext(ctx, `SELECT id,COALESCE(movie_id,series_id),relative_path,size_bytes,sha256 FROM media_files WHERE id=?`, fileID).Scan(&v.FileID, &v.MediaID, &v.Source, &v.Size, &v.SHA); err != nil {
			return Plan{}, err
		}
		if err = idle(ctx, tx, mediaID, fileID); err != nil {
			return Plan{}, err
		}
		v.Snapshot = snapshot
		v.Title = snapshot.Content.Title
		v.Companions = []Companion{}
		v.Episodes, err = s.episodes(ctx, tx, fileID)
		if err != nil {
			return Plan{}, err
		}
		paths = append(paths, v.Source)
		files = append(files, v)
	}
	source, err := titleFolder(config, snapshot.Content.Kind, paths)
	if err != nil {
		return Plan{}, err
	}
	dest := path.Join(leaf(snapshot.Library, snapshot.Content.Kind), path.Base(source))
	if !validRelative(dest) || strings.EqualFold(source, dest) || strings.HasPrefix(strings.ToLower(dest), strings.ToLower(source)+"/") || strings.HasPrefix(strings.ToLower(source), strings.ToLower(dest)+"/") {
		return Plan{}, problem("invalid_selection", "Choose a different collection for this folder.")
	}
	if exists, e := existsFolded(root, dest); e != nil {
		return Plan{}, e
	} else if exists {
		return Plan{}, problem("destination_exists", "The destination folder already exists. No files were changed.")
	}
	inventory, err := inventoryFolder(ctx, root, tx, source, mediaID)
	if err != nil {
		return Plan{}, err
	}
	byPath := map[string]Companion{}
	for _, entry := range inventory.Files {
		byPath[entry.Source] = entry
	}
	for i := range files {
		v := &files[i]
		info, ok := byPath[v.Source]
		if !ok || info.Size != v.Size || info.SHA != v.SHA {
			return Plan{}, problem("source_changed", "A registered file no longer matches its saved checksum.")
		}
		v.Identity = info.Identity
		v.Mtime = info.Mtime
		v.Destination = folderDestination(source, dest, v.Source)
		rows, e := tx.QueryContext(ctx, `SELECT relative_path FROM subtitles WHERE media_file_id=? AND relative_path<>'' UNION SELECT destination FROM subtitle_imports WHERE media_file_id=? AND phase='committed'`, v.FileID, v.FileID)
		if e != nil {
			return Plan{}, e
		}
		for rows.Next() {
			var sub string
			if e = rows.Scan(&sub); e != nil {
				rows.Close()
				return Plan{}, e
			}
			entry, ok := byPath[sub]
			if !ok {
				rows.Close()
				return Plan{}, problem("unsupported_companion", "A registered subtitle is outside this folder or is missing.")
			}
			entry.Destination = folderDestination(source, dest, sub)
			v.Companions = append(v.Companions, entry)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return Plan{}, e
		}
		rows, e = tx.QueryContext(ctx, `SELECT sf.relative_path FROM youtube_subtitle_files sf JOIN youtube_files yf ON yf.job_id=sf.job_id WHERE yf.media_file_id=? UNION SELECT si.destination_path FROM youtube_subtitle_imports si JOIN youtube_files yf ON yf.job_id=si.job_id WHERE yf.media_file_id=? AND si.phase='committed'`, v.FileID, v.FileID)
		if e != nil {
			return Plan{}, e
		}
		for rows.Next() {
			var name string
			if e = rows.Scan(&name); e != nil {
				rows.Close()
				return Plan{}, e
			}
			found := false
			for _, sub := range v.Companions {
				if sub.Source == name {
					found = true
					break
				}
			}
			if !found {
				rows.Close()
				return Plan{}, problem("unsupported_companion", "A YouTube subtitle is not registered with this video; resolve it before moving the folder.")
			}
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return Plan{}, e
		}
	}
	now := time.Now().Unix()
	p := Plan{ID: id(), State: "review", ConfigRevision: config.Revision, TargetLibrary: target, EntireMediaID: mediaID, Files: files, Created: now, Updated: now, Root: resolved, Phase: "planned", FolderMove: &FolderMove{Source: source, Destination: dest, Inventory: inventory}}
	raw, err := json.Marshal(p)
	if err != nil {
		return Plan{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO organization_plans(id,state,snapshot,created_at,updated_at) VALUES(?,'review',?,?,?)`, p.ID, string(raw), now, now); err != nil {
		return Plan{}, err
	}
	return p, tx.Commit()
}

func folderDestination(source, destination, name string) string {
	return destination + strings.TrimPrefix(name, source)
}

func (s *Service) validateFolder(ctx context.Context, root *os.Root, q query, p Plan, moved bool) error {
	ids, err := titleFiles(ctx, q, p.EntireMediaID)
	if err != nil {
		return err
	}
	if len(ids) != len(p.Files) {
		return problem("revision_conflict", "The title's registered files changed after review.")
	}
	c, err := s.config(ctx, q)
	if err != nil {
		return err
	}
	if c.Revision != p.ConfigRevision {
		return problem("revision_conflict", "Organization settings changed after review.")
	}
	for i, v := range p.Files {
		if ids[i] != v.FileID {
			return problem("revision_conflict", "The title's registered files changed after review.")
		}
		snap, e := s.capture(ctx, q, v.MediaID, p.TargetLibrary)
		if e != nil {
			return e
		}
		if !reflect.DeepEqual(snap, v.Snapshot) {
			return problem("revision_conflict", "Title settings changed after review.")
		}
		var name, sha string
		var size int64
		if e = q.QueryRowContext(ctx, `SELECT relative_path,sha256,size_bytes FROM media_files WHERE id=?`, v.FileID).Scan(&name, &sha, &size); e != nil {
			return e
		}
		if name != v.Source || sha != v.SHA || size != v.Size {
			return problem("revision_conflict", "A registered file changed after review.")
		}
		eps, e := s.episodes(ctx, q, v.FileID)
		if e != nil {
			return e
		}
		if !reflect.DeepEqual(eps, v.Episodes) {
			return problem("revision_conflict", "Episode mappings changed after review.")
		}
		if e = idle(ctx, q, v.MediaID, v.FileID); e != nil {
			return e
		}
		rows, e := q.QueryContext(ctx, `SELECT relative_path FROM subtitles WHERE media_file_id=? AND relative_path<>'' UNION SELECT destination FROM subtitle_imports WHERE media_file_id=? AND phase='committed'`, v.FileID, v.FileID)
		if e != nil {
			return e
		}
		var paths []string
		for rows.Next() {
			var name string
			if e = rows.Scan(&name); e != nil {
				rows.Close()
				return e
			}
			paths = append(paths, name)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		if len(paths) != len(v.Companions) {
			return problem("revision_conflict", "Registered subtitles changed after review.")
		}
		for i, name := range paths {
			if name != v.Companions[i].Source {
				return problem("revision_conflict", "Registered subtitles changed after review.")
			}
		}
	}
	currentPath := p.FolderMove.Source
	expected := p.FolderMove.Inventory
	if moved {
		currentPath = p.FolderMove.Destination
		expected.Directories = append([]folderDirectory(nil), expected.Directories...)
		expected.Files = append([]Companion(nil), expected.Files...)
		for i := range expected.Directories {
			expected.Directories[i].Path = folderDestination(p.FolderMove.Source, currentPath, expected.Directories[i].Path)
		}
		for i := range expected.Files {
			expected.Files[i].Source = folderDestination(p.FolderMove.Source, currentPath, expected.Files[i].Source)
		}
	}
	actual, err := inventoryFolder(ctx, root, q, currentPath, p.EntireMediaID)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(expected, actual) {
		return problem("source_changed", "The folder or its contents changed after review.")
	}
	return nil
}

func folderExists(root *os.Root, name string) (bool, error) {
	if e := segments(root, name, true); e != nil {
		return false, e
	}
	_, e := root.Lstat(filepath.FromSlash(name))
	if errors.Is(e, os.ErrNotExist) {
		return false, nil
	}
	return e == nil, e
}

// A failure before the atomic rename can release the media-work fence only if
// the original directory still has its reviewed identity and no target exists.
func (s *Service) folderStillAtSource(p Plan) bool {
	root, _, err := s.openRoot(p.Root)
	if err != nil {
		return false
	}
	defer root.Close()
	exists, err := existsFolded(root, p.FolderMove.Destination)
	if err != nil || exists || len(p.FolderMove.Inventory.Directories) == 0 {
		return false
	}
	if err = segments(root, p.FolderMove.Source, false); err != nil {
		return false
	}
	f, err := root.Open(filepath.FromSlash(p.FolderMove.Source))
	if err != nil {
		return false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.IsDir() {
		return false
	}
	id, err := identity(f)
	return err == nil && id == p.FolderMove.Inventory.Directories[0].Identity
}

func (s *Service) executeFolder(ctx context.Context, p *Plan) error {
	root, _, err := s.openRoot(p.Root)
	if err != nil {
		return err
	}
	defer root.Close()
	if p.FolderMove.Copy {
		return s.executeCopiedFolder(ctx, root, p)
	}
	if p.Phase == "committed" {
		_, err = s.db.ExecContext(ctx, `UPDATE organization_plans SET state='completed',message='',updated_at=unixepoch() WHERE id=?`, p.ID)
		return err
	}
	src, err := folderExists(root, p.FolderMove.Source)
	if err != nil {
		return err
	}
	dst, err := folderExists(root, p.FolderMove.Destination)
	if err != nil {
		return err
	}
	if src && dst || !src && !dst {
		return problem("source_changed", "The reviewed folder locations changed; both locations were retained for review.")
	}
	if src {
		tx, e := s.db.BeginTx(ctx, nil)
		if e != nil {
			return e
		}
		e = s.validateFolder(ctx, root, tx, *p, false)
		if e == nil {
			e = tx.Commit()
		} else {
			tx.Rollback()
		}
		if e != nil {
			return e
		}
		if exists, e := existsFolded(root, p.FolderMove.Destination); e != nil {
			return e
		} else if exists {
			return problem("destination_exists", "A destination folder appeared after review.")
		}
		if err = renameFolderNoReplace(root, p.FolderMove.Source, p.FolderMove.Destination); err != nil {
			var typed *Error
			if !errors.As(err, &typed) || typed.Code != "cross_device" {
				return err
			}
			p.FolderMove.Copy = true
			p.FolderMove.Stage = path.Join(path.Dir(p.FolderMove.Destination), ".reelscarey-folder-"+p.ID)
			if err = s.savePhase(ctx, p, "copying"); err != nil {
				return err
			}
			return s.executeCopiedFolder(ctx, root, p)
		}
	}
	// A moved directory must still have exactly the reviewed directory identities
	// and bytes. This also recognizes our own rename after a crash.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = s.validateFolder(ctx, root, tx, *p, true); err != nil {
		return err
	}
	if err = s.commitFolderReferences(ctx, tx, p); err != nil {
		return err
	}
	p.Phase = "committed"
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE organization_plans SET snapshot=?,state='completed',message='',updated_at=unixepoch() WHERE id=?`, string(raw), p.ID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) commitFolderReferences(ctx context.Context, tx *sql.Tx, p *Plan) error {
	var err error
	for _, v := range p.Files {
		if _, err = tx.ExecContext(ctx, `UPDATE media_files SET relative_path=? WHERE id=? AND relative_path=? AND sha256=?`, v.Destination, v.FileID, v.Source, v.SHA); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE youtube_imports SET destination_path=? WHERE phase='committed' AND destination_path=? AND job_id IN (SELECT job_id FROM youtube_files WHERE media_file_id=?)`, v.Destination, v.Source, v.FileID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE youtube_files SET relative_path=? WHERE media_file_id=? AND relative_path=? AND sha256=?`, v.Destination, v.FileID, v.Source, v.SHA); err != nil {
			return err
		}
		for _, sub := range v.Companions {
			if _, err = tx.ExecContext(ctx, `UPDATE subtitles SET relative_path=? WHERE media_file_id=? AND relative_path=?`, sub.Destination, v.FileID, sub.Source); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `UPDATE subtitle_imports SET destination=? WHERE media_file_id=? AND destination=? AND phase='committed'`, sub.Destination, v.FileID, sub.Source); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `UPDATE youtube_subtitle_imports SET destination_path=? WHERE phase='committed' AND destination_path=?`, sub.Destination, sub.Source); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `UPDATE youtube_subtitle_files SET relative_path=? WHERE relative_path=?`, sub.Destination, sub.Source); err != nil {
				return err
			}
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM subtitle_inspections WHERE media_file_id=?`, v.FileID); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO media_organization(media_id,library_key,revision) VALUES(?,?,1) ON CONFLICT(media_id) DO UPDATE SET library_key=excluded.library_key,revision=media_organization.revision+1`, p.EntireMediaID, p.TargetLibrary); err != nil {
		return err
	}
	return nil
}
