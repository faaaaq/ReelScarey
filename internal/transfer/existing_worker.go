package transfer

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

func (s *Service) runLibrary(ctx context.Context, logger *slog.Logger) {
	for ctx.Err() == nil {
		processed, e := s.ProcessLibraryNext(ctx)
		if e != nil && ctx.Err() == nil {
			logger.Error("library review worker failed")
		}
		if processed && e == nil {
			continue
		}
		timer := time.NewTimer(2 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
func (s *Service) ProcessLibraryNext(ctx context.Context) (bool, error) {
	owner := ID()
	now := time.Now().Unix()
	var id string
	isFile := true
	e := s.db.QueryRowContext(ctx, `UPDATE library_scan_files SET state='verifying',lease_owner=?,lease_until=?,updated_at=unixepoch() WHERE id=(SELECT id FROM library_scan_files WHERE state IN ('queued','verifying') AND lease_until<? ORDER BY updated_at,id LIMIT 1) RETURNING id`, owner, now+60, now).Scan(&id)
	if errors.Is(e, sql.ErrNoRows) {
		isFile = false
		e = s.db.QueryRowContext(ctx, `UPDATE library_scans SET state='scanning',lease_owner=?,lease_until=?,updated_at=unixepoch() WHERE id=(SELECT id FROM library_scans WHERE state IN ('queued','scanning') AND lease_until<? ORDER BY created_at,id LIMIT 1) RETURNING id`, owner, now+60, now).Scan(&id)
	}
	if errors.Is(e, sql.ErrNoRows) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	table := "library_scans"
	state := "scanning"
	if isFile {
		table = "library_scan_files"
		state = "verifying"
	}
	work, cancel := context.WithTimeout(ctx, 30*time.Minute)
	done := make(chan struct{})
	go func() {
		defer close(done)
		timer := time.NewTicker(15 * time.Second)
		defer timer.Stop()
		for {
			select {
			case <-work.Done():
				return
			case <-timer.C:
				res, e := s.db.ExecContext(work, `UPDATE `+table+` SET lease_until=unixepoch()+60 WHERE id=? AND lease_owner=? AND state=? AND lease_until>=unixepoch()`, id, owner, state)
				if e != nil {
					cancel()
					return
				}
				n, _ := res.RowsAffected()
				if n != 1 {
					cancel()
					return
				}
			}
		}
	}()
	if isFile {
		e = s.verifyExisting(work, id, owner)
	} else {
		e = s.discoverDirectory(work, id, owner)
	}
	cancel()
	<-done
	finish, stop := context.WithTimeout(context.Background(), 3*time.Second)
	defer stop()
	if e != nil && ctx.Err() == nil && !errors.Is(e, ErrLease) && !errors.Is(e, context.Canceled) {
		failed := "failed"
		if isFile {
			failed = "blocked"
		}
		_, save := s.db.ExecContext(finish, `UPDATE `+table+` SET state=?,message=?,updated_at=unixepoch() WHERE id=? AND lease_owner=? AND state=?`, failed, registeredMessage(e), id, owner, state)
		if save != nil {
			return true, save
		}
	}
	_, save := s.db.ExecContext(finish, `UPDATE `+table+` SET lease_owner='',lease_until=0 WHERE id=? AND lease_owner=?`, id, owner)
	return true, save
}
func scanLease(ctx context.Context, tx *sql.Tx, id, owner string) error {
	var n int
	e := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM library_scans WHERE id=? AND lease_owner=? AND state='scanning' AND lease_until>=unixepoch()`, id, owner).Scan(&n)
	if e != nil {
		return e
	}
	if n != 1 {
		return ErrLease
	}
	return nil
}
func (s *Service) discoverDirectory(ctx context.Context, id, owner string) error {
	scan, e := s.LibraryScan(ctx, id)
	if e != nil {
		return e
	}
	root, e := s.existingRoot(ctx, &scan)
	if e != nil {
		return e
	}
	defer root.Close()
	var relative string
	e = s.db.QueryRowContext(ctx, `SELECT relative_path FROM library_scan_dirs WHERE scan_id=? AND state='pending' ORDER BY relative_path LIMIT 1`, id).Scan(&relative)
	if errors.Is(e, sql.ErrNoRows) {
		_, e = s.db.ExecContext(ctx, `UPDATE library_scans SET state='completed',message='Discovery finished. Review files before adding them.',updated_at=unixepoch() WHERE id=? AND lease_owner=? AND state='scanning'`, id, owner)
		return e
	}
	if e != nil {
		return e
	}
	if _, e = safeRelative(relative); e != nil {
		return e
	}
	if e = segments(root, relative, false); e != nil {
		return problem("unsafe_path", "A discovery folder changed or contains a symbolic link. Previous results remain available for review.")
	}
	dir, e := root.OpenRoot(filepath.FromSlash(relative))
	if e != nil {
		return problem("storage_unavailable", "A library folder could not be read. Previous results remain available for review.")
	}
	defer dir.Close()
	handle, e := dir.Open(".")
	if e != nil {
		return e
	}
	defer handle.Close()
	files := []DiscoveredFile{}
	dirs := []string{}
	examined, skipped := 0, 0
	for {
		if e = ctx.Err(); e != nil {
			return e
		}
		entries, readErr := handle.ReadDir(128)
		for _, entry := range entries {
			examined++
			if examined > maxDirectoryEntries {
				return problem("scan_limit", "This folder contains more than 20,000 entries. Discovery stopped with previous results retained; use a smaller configured library folder.")
			}
			name := entry.Name()
			rel := path.Join(relative, name)
			if !utf8.ValidString(name) || strings.HasPrefix(name, ".") {
				skipped++
				continue
			}
			if _, e = safeRelative(rel); e != nil {
				skipped++
				continue
			}
			info, e := root.Lstat(filepath.FromSlash(rel))
			if e != nil {
				return problem("source_changed", "A file or folder changed during discovery. Start a new scan after other file operations finish.")
			}
			if info.Mode()&os.ModeSymlink != 0 {
				skipped++
				continue
			}
			if info.IsDir() {
				if strings.Count(rel, "/") > 12 {
					return problem("scan_limit", "A folder exceeds the twelve-level discovery limit. Previous results remain available.")
				}
				dirs = append(dirs, rel)
				continue
			}
			if !info.Mode().IsRegular() || info.Size() <= 0 || !video(name) || extraVideo.MatchString(rel) {
				skipped++
				continue
			}
			var known int
			if e = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM media_files WHERE relative_path=? COLLATE NOCASE`, rel).Scan(&known); e != nil {
				return e
			}
			if known > 0 {
				skipped++
				continue
			}
			opened, e := openExistingFile(root, filepath.FromSlash(rel))
			if e != nil {
				return problem("storage_unavailable", "A video could not be inspected during discovery. Previous results remain available.")
			}
			openedInfo, statErr := opened.Stat()
			identity, identityErr := existingIdentity(opened)
			opened.Close()
			if statErr != nil || identityErr != nil || !openedInfo.Mode().IsRegular() || !os.SameFile(info, openedInfo) || info.Size() != openedInfo.Size() || info.ModTime() != openedInfo.ModTime() {
				return problem("source_changed", "A video changed while its identity was inspected. Start a new scan.")
			}
			files = append(files, DiscoveredFile{ID: ID(), Path: rel, Size: info.Size(), Mtime: info.ModTime().UnixNano(), identity: identity})
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return problem("storage_unavailable", "A library folder could not be fully read. Previous results remain available for review.")
		}
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = scanLease(ctx, tx, id, owner); e != nil {
		return e
	}
	var fileCount, dirCount, entryCount int
	if e = tx.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM library_scan_files WHERE scan_id=?),(SELECT COUNT(*) FROM library_scan_dirs WHERE scan_id=?),(SELECT COALESCE(SUM(examined),0) FROM library_scan_dirs WHERE scan_id=?)`, id, id, id).Scan(&fileCount, &dirCount, &entryCount); e != nil {
		return e
	}
	if fileCount+len(files) > maxDiscoveryFiles || dirCount+len(dirs) > maxDiscoveryDirs || entryCount+examined > 200000 {
		return problem("scan_limit", "Discovery reached its limit of 10,000 videos, 2,000 folders or 200,000 entries. Previous results remain available; use a smaller configured library folder.")
	}
	for _, rel := range dirs {
		if _, e = tx.ExecContext(ctx, `INSERT INTO library_scan_dirs(scan_id,relative_path) VALUES(?,?) ON CONFLICT DO NOTHING`, id, rel); e != nil {
			return e
		}
	}
	for _, f := range files {
		res, e := tx.ExecContext(ctx, `INSERT INTO library_scan_files(id,scan_id,relative_path,size_bytes,mtime,file_identity,updated_at) SELECT ?,?,?,?,?,?,unixepoch() WHERE NOT EXISTS(SELECT 1 FROM media_files WHERE relative_path=? COLLATE NOCASE) ON CONFLICT(scan_id,relative_path) DO NOTHING`, f.ID, id, f.Path, f.Size, f.Mtime, f.identity, f.Path)
		if e != nil {
			return e
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			skipped++
		}
	}
	if _, e = tx.ExecContext(ctx, `UPDATE library_scan_dirs SET state='done',examined=?,skipped=? WHERE scan_id=? AND relative_path=?`, examined, skipped, id, relative); e != nil {
		return e
	}
	res, e := tx.ExecContext(ctx, `UPDATE library_scans SET updated_at=unixepoch() WHERE id=? AND lease_owner=? AND state='scanning' AND lease_until>=unixepoch()`, id, owner)
	if e != nil {
		return e
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return ErrLease
	}
	return tx.Commit()
}
func (s *Service) verifyExisting(ctx context.Context, id, owner string) error {
	f, e := scanDiscovered(s.db.QueryRowContext(ctx, `SELECT `+discoveredColumns+` FROM library_scan_files WHERE id=?`, id))
	if e != nil {
		return e
	}
	if f.MediaID == nil || f.owner != owner {
		return ErrLease
	}
	scan, e := s.LibraryScan(ctx, f.ScanID)
	if e != nil {
		return e
	}
	if _, e = safeRelative(f.Path); e != nil {
		return e
	}
	if !strings.HasPrefix(f.Path, scan.Folder+"/") {
		return problem("unsafe_path", "This file is outside the selected discovery folder.")
	}
	root, e := s.existingRoot(ctx, &scan)
	if e != nil {
		return e
	}
	defer root.Close()
	if e = segments(root, f.Path, false); e != nil {
		return problem("source_changed", "The discovered path changed or contains a symbolic link. Start a new scan.")
	}
	before, e := root.Lstat(filepath.FromSlash(f.Path))
	if e != nil {
		return problem("source_changed", "The discovered file is no longer available. Start a new scan.")
	}
	file, e := openExistingFile(root, filepath.FromSlash(f.Path))
	if e != nil {
		return problem("storage_unavailable", "The existing video could not be opened for verification.")
	}
	defer file.Close()
	info, e := file.Stat()
	if e != nil {
		return e
	}
	if !info.Mode().IsRegular() || !os.SameFile(before, info) || info.Size() != f.Size || info.ModTime().UnixNano() != f.Mtime {
		return problem("source_changed", "The file changed since discovery. Start a new scan before adding it.")
	}
	identity, e := existingIdentity(file)
	if e != nil {
		return e
	}
	if identity != f.identity {
		return problem("source_changed", "The discovered video was replaced. Start a new scan before adding it.")
	}
	hash, e := fileHash(ctx, file)
	if e != nil {
		return e
	}
	probe, e := s.importer.Prober.Probe(ctx, file)
	if e != nil {
		return e
	}
	verified, e := fileHash(ctx, file)
	if e != nil {
		return e
	}
	after, e := file.Stat()
	if e != nil {
		return e
	}
	if hash != verified || after.Size() != f.Size || after.ModTime().UnixNano() != f.Mtime {
		return problem("source_changed", "The file changed during verification. It was not registered; start a new scan.")
	}
	if e = segments(root, f.Path, false); e != nil {
		return problem("source_changed", "The path changed during verification. Start a new scan.")
	}
	visible, e := root.Lstat(filepath.FromSlash(f.Path))
	if e != nil || !visible.Mode().IsRegular() || !os.SameFile(info, visible) {
		return problem("source_changed", "The video was replaced or moved during verification. Start a new scan.")
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var leases int
	if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM library_scan_files WHERE id=? AND state='verifying' AND lease_owner=? AND lease_until>=unixepoch()`, id, owner).Scan(&leases); e != nil {
		return e
	}
	if leases != 1 {
		return ErrLease
	}
	var kind string
	if e = tx.QueryRowContext(ctx, `SELECT kind FROM media_items WHERE id=?`, *f.MediaID).Scan(&kind); e != nil {
		return e
	}
	if kind != scan.Kind {
		return mappingError()
	}
	if e = existingTargetFree(ctx, tx, *f.MediaID, kind, f.targets); e != nil {
		return e
	}
	var exists int
	if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM media_files WHERE relative_path=? COLLATE NOCASE`, f.Path).Scan(&exists); e != nil {
		return e
	}
	if exists > 0 {
		return problem("duplicate_file", "This file was already registered by another import. Reload its catalog details.")
	}
	// Recheck the anchored name immediately before the atomic database registration.
	if e = segments(root, f.Path, false); e != nil {
		return problem("source_changed", "The path changed before registration. Start a new scan.")
	}
	latest, e := root.Lstat(filepath.FromSlash(f.Path))
	if e != nil || !os.SameFile(info, latest) || latest.Size() != f.Size || latest.ModTime().UnixNano() != f.Mtime {
		return problem("source_changed", "The file changed before registration. Start a new scan.")
	}
	raw, e := json.Marshal(probe)
	if e != nil {
		return e
	}
	var movieID, seriesID any
	if kind == "movie" {
		movieID = *f.MediaID
	} else {
		seriesID = *f.MediaID
	}
	res, e := tx.ExecContext(ctx, `INSERT INTO media_files(movie_id,series_id,relative_path,size_bytes,sha256,probe_json,imported_at,origin,source_name) VALUES(?,?,?,?,?,?,unixepoch(),'existing',?)`, movieID, seriesID, f.Path, f.Size, hash, string(raw), path.Base(f.Path))
	if e != nil {
		return e
	}
	fileID, e := res.LastInsertId()
	if e != nil {
		return e
	}
	for _, t := range f.targets {
		if _, e = tx.ExecContext(ctx, `INSERT INTO episode_files(episode_id,media_file_id) VALUES(?,?)`, t.EpisodeID, fileID); e != nil {
			return e
		}
	}
	if kind == "movie" {
		_, e = tx.ExecContext(ctx, `UPDATE media_items SET status='available' WHERE id=?`, *f.MediaID)
	} else {
		e = refreshTVStatus(ctx, tx, *f.MediaID)
	}
	if e != nil {
		return e
	}
	res, e = tx.ExecContext(ctx, `UPDATE library_scan_files SET state='registered',media_file_id=?,message='Verified and added in its existing location.',updated_at=unixepoch() WHERE id=? AND lease_owner=? AND state='verifying' AND lease_until>=unixepoch()`, fileID, id, owner)
	if e != nil {
		return e
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return ErrLease
	}
	return tx.Commit()
}
