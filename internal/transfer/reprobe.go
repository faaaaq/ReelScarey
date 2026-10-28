package transfer

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/faaaaq/ReelScarey/internal/organization"
)

// RefreshMediaProbe explicitly re-inspects a registered file after verifying its
// saved content. It never changes the file or its source provenance.
func (s *Service) RefreshMediaProbe(ctx context.Context, id int64) (Probe, error) {
	if !s.probeMu.TryLock() {
		return Probe{}, problem("media_busy", "Another media inspection is running. Try again when it finishes.")
	}
	defer s.probeMu.Unlock()
	defer organization.ReadMedia()()
	if e := organization.EnsureMediaAvailable(ctx, s.db); e != nil {
		return Probe{}, e
	}
	var name, expected string
	var size int64
	if e := s.db.QueryRowContext(ctx, `SELECT relative_path,size_bytes,sha256 FROM media_files WHERE id=?`, id).Scan(&name, &size, &expected); e != nil {
		return Probe{}, e
	}
	if _, e := safeRelative(name); e != nil {
		return Probe{}, e
	}
	downloads, root, e := roots(s.cfg.DownloadsDir, s.cfg.MediaDir, s.cfg.ConfigDir)
	if e != nil {
		return Probe{}, e
	}
	downloads.Close()
	defer root.Close()
	if e = segments(root, name, false); e != nil {
		if errors.Is(e, os.ErrNotExist) {
			return Probe{}, problem("source_changed", "The registered file is missing.")
		}
		return Probe{}, e
	}
	before, e := root.Lstat(filepath.FromSlash(name))
	if e != nil {
		if errors.Is(e, os.ErrNotExist) {
			return Probe{}, problem("source_changed", "The registered file is missing.")
		}
		return Probe{}, e
	}
	if !before.Mode().IsRegular() || before.Size() != size {
		return Probe{}, problem("source_changed", "The registered file changed; it was not inspected.")
	}
	file, e := openExistingFile(root, filepath.FromSlash(name))
	if e != nil {
		return Probe{}, e
	}
	defer file.Close()
	info, e := file.Stat()
	if e != nil {
		return Probe{}, e
	}
	if !info.Mode().IsRegular() || !os.SameFile(before, info) {
		return Probe{}, problem("source_changed", "The registered file changed; it was not inspected.")
	}
	digest, e := fileHash(ctx, file)
	if e != nil {
		return Probe{}, e
	}
	if digest != expected {
		return Probe{}, problem("source_changed", "The registered file no longer matches its saved checksum.")
	}
	result, e := s.importer.Prober.Probe(ctx, file)
	if e != nil {
		return Probe{}, e
	}
	digest, e = fileHash(ctx, file)
	if e != nil {
		return Probe{}, e
	}
	after, e := file.Stat()
	if e != nil {
		return Probe{}, e
	}
	if digest != expected || after.Size() != size || !after.ModTime().Equal(info.ModTime()) {
		return Probe{}, problem("source_changed", "The registered file changed during inspection.")
	}
	if e = segments(root, name, false); e != nil {
		if errors.Is(e, os.ErrNotExist) {
			return Probe{}, problem("source_changed", "The registered path disappeared during inspection.")
		}
		return Probe{}, e
	}
	visible, e := root.Lstat(filepath.FromSlash(name))
	if e != nil {
		if errors.Is(e, os.ErrNotExist) {
			return Probe{}, problem("source_changed", "The registered path disappeared during inspection.")
		}
		return Probe{}, e
	}
	if !visible.Mode().IsRegular() || !os.SameFile(info, visible) {
		return Probe{}, problem("source_changed", "The registered path changed during inspection.")
	}
	raw, e := json.Marshal(result)
	if e != nil {
		return Probe{}, e
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return Probe{}, e
	}
	defer tx.Rollback()
	if e = organization.EnsureMediaAvailableTx(ctx, tx); e != nil {
		return Probe{}, e
	}
	var inconsistent int
	if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM youtube_files WHERE media_file_id=? AND (relative_path<>? OR size_bytes<>? OR sha256<>?)`, id, name, size, expected).Scan(&inconsistent); e != nil {
		return Probe{}, e
	}
	if inconsistent != 0 {
		return Probe{}, problem("source_changed", "The saved video provenance no longer matches the registration.")
	}
	updated, e := tx.ExecContext(ctx, `UPDATE media_files SET probe_json=? WHERE id=? AND relative_path=? AND size_bytes=? AND sha256=?`, string(raw), id, name, size, expected)
	if e != nil {
		return Probe{}, e
	}
	n, e := updated.RowsAffected()
	if e != nil {
		return Probe{}, e
	}
	if n != 1 {
		return Probe{}, problem("source_changed", "The registration changed during inspection.")
	}
	if _, e = tx.ExecContext(ctx, `UPDATE youtube_files SET probe_json=? WHERE media_file_id=?`, string(raw), id); e != nil {
		return Probe{}, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO activity_events(occurred_at,kind,object_id,title,state) SELECT unixepoch(),'title',m.id,m.title,'audio tags inspected' FROM media_files f JOIN media_items m ON m.id=COALESCE(f.movie_id,f.series_id) WHERE f.id=?`, id); e != nil {
		return Probe{}, e
	}
	if e = tx.Commit(); e != nil {
		return Probe{}, e
	}
	return result, nil
}
