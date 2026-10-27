package organization

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
)

func (s *Service) copiedStage(ctx context.Context, root *os.Root, p *Plan) error {
	stage := p.FolderMove.Stage
	parent := path.Dir(stage)
	if err := segments(root, parent, true); err != nil {
		return err
	}
	if err := root.MkdirAll(filepath.FromSlash(parent), 0755); err != nil {
		return err
	}
	if err := segments(root, parent, false); err != nil {
		return err
	}
	exists, err := folderExists(root, stage)
	if err != nil {
		return err
	}
	if !exists {
		if p.FolderMove.StageID != "" {
			return problem("staging_conflict", "The saved folder stage disappeared.")
		}
		if err = root.Mkdir(filepath.FromSlash(stage), 0700); err != nil {
			return err
		}
		f, e := root.Open(filepath.FromSlash(stage))
		if e != nil {
			return e
		}
		p.FolderMove.StageID, e = identity(f)
		f.Close()
		if e != nil {
			return e
		}
		if e = s.savePhase(ctx, p, "copying"); e != nil {
			return e
		}
	} else if p.FolderMove.StageID == "" {
		return problem("staging_conflict", "An unverified folder stage was left untouched for review.")
	}
	if err = folderIdentity(root, stage, p.FolderMove.StageID); err != nil {
		return err
	}
	for _, d := range p.FolderMove.Inventory.Directories[1:] {
		name := folderDestination(p.FolderMove.Source, stage, d.Path)
		if err = segments(root, name, true); err != nil {
			return err
		}
		if err = root.MkdirAll(filepath.FromSlash(name), 0755); err != nil {
			return err
		}
		if err = segments(root, name, false); err != nil {
			return err
		}
	}
	for n, entry := range p.FolderMove.Inventory.Files {
		if err = checkSource(ctx, root, entry, true); err != nil {
			return err
		}
		v := entry
		v.Destination = folderDestination(p.FolderMove.Source, stage, entry.Source)
		if err = s.publish(ctx, root, *p, v, n); err != nil {
			return err
		}
		if err = s.checkOutput(ctx, root, s.db, p.ID, v); err != nil {
			return err
		}
		// Preserve the original file's permissions on the verified owned copy.
		source, e := root.Open(filepath.FromSlash(entry.Source))
		if e != nil {
			return e
		}
		sourceID, e := identity(source)
		if e != nil || sourceID != entry.Identity {
			source.Close()
			return problem("source_changed", "A source file was replaced during copying.")
		}
		sourceInfo, e := source.Stat()
		source.Close()
		if e != nil {
			return e
		}
		output, e := root.Open(filepath.FromSlash(v.Destination))
		if e != nil {
			return e
		}
		var recorded string
		if e = s.db.QueryRowContext(ctx, `SELECT identity FROM organization_outputs WHERE plan_id=? AND destination=?`, p.ID, v.Destination).Scan(&recorded); e != nil {
			output.Close()
			return e
		}
		outputID, e := identity(output)
		if e != nil || outputID != recorded {
			output.Close()
			return problem("destination_changed", "A copied file was replaced and retained.")
		}
		e = output.Chmod(sourceInfo.Mode().Perm())
		output.Close()
		if e != nil {
			return e
		}
		part := path.Join(path.Dir(v.Destination), fmt.Sprintf(".reelscarey-organize-%s-%d.part", p.ID, n))
		var saved string
		if err = s.db.QueryRowContext(ctx, `SELECT identity FROM organization_outputs WHERE plan_id=? AND destination=?`, p.ID, v.Destination).Scan(&saved); err != nil {
			return err
		}
		f, e := root.Open(filepath.FromSlash(part))
		if errors.Is(e, os.ErrNotExist) {
			continue
		}
		if e != nil {
			return e
		}
		actual, e := identity(f)
		f.Close()
		if e != nil || actual != saved {
			return problem("staging_conflict", "A staging file was replaced and was retained.")
		}
		if err = root.Remove(filepath.FromSlash(part)); err != nil {
			return err
		}
	}
	// Set directory permissions only after the contents are built so that even
	// read-only source directories can be copied without broadening their access.
	for i := len(p.FolderMove.Inventory.Directories) - 1; i >= 0; i-- {
		d := p.FolderMove.Inventory.Directories[i]
		if err = folderIdentity(root, d.Path, d.Identity); err != nil {
			return err
		}
		src, e := root.Open(filepath.FromSlash(d.Path))
		if e != nil {
			return e
		}
		info, e := src.Stat()
		src.Close()
		if e != nil {
			return e
		}
		name := folderDestination(p.FolderMove.Source, stage, d.Path)
		if err = segments(root, name, false); err != nil {
			return err
		}
		before, e := root.Lstat(filepath.FromSlash(name))
		if e != nil || !before.IsDir() {
			return problem("staging_conflict", "A staged directory changed and was retained.")
		}
		target, e := root.Open(filepath.FromSlash(name))
		if e != nil {
			return e
		}
		actual, e := target.Stat()
		if e != nil || !actual.IsDir() || !os.SameFile(before, actual) {
			target.Close()
			return problem("staging_conflict", "A staged directory changed and was retained.")
		}
		e = target.Chmod(info.Mode().Perm())
		target.Close()
		if e != nil {
			return e
		}
	}
	return nil
}

func folderIdentity(root *os.Root, name, expected string) error {
	if expected == "" {
		return problem("staging_conflict", "The saved folder identity is missing.")
	}
	if err := segments(root, name, false); err != nil {
		return err
	}
	f, err := root.Open(filepath.FromSlash(name))
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return problem("staging_conflict", "The reviewed folder is no longer a directory.")
	}
	id, err := identity(f)
	if err != nil {
		return err
	}
	if id != expected {
		return problem("staging_conflict", "The staged folder was replaced and retained.")
	}
	return nil
}

// Verify every output against its own journal identity and the exact reviewed
// directory/file set. Source and copy identities necessarily differ.
func (s *Service) verifyCopiedFolder(ctx context.Context, root *os.Root, p Plan, base string) error {
	if err := folderIdentity(root, base, p.FolderMove.StageID); err != nil {
		return err
	}
	actual, err := inventoryFolder(ctx, root, s.db, base, p.EntireMediaID)
	if err != nil {
		return err
	}
	if len(actual.Directories) != len(p.FolderMove.Inventory.Directories) || len(actual.Files) != len(p.FolderMove.Inventory.Files) {
		return problem("destination_changed", "The staged folder contents changed and were retained.")
	}
	for i, d := range p.FolderMove.Inventory.Directories {
		if actual.Directories[i].Path != folderDestination(p.FolderMove.Source, base, d.Path) {
			return problem("destination_changed", "The staged folder directories changed.")
		}
	}
	for i, v := range p.FolderMove.Inventory.Files {
		want := folderDestination(p.FolderMove.Source, base, v.Source)
		got := actual.Files[i]
		if got.Source != want || got.Size != v.Size || got.SHA != v.SHA {
			return problem("destination_changed", "A staged file changed and was retained.")
		}
		stageName := folderDestination(p.FolderMove.Source, p.FolderMove.Stage, v.Source)
		var id, phase string
		if err = s.db.QueryRowContext(ctx, `SELECT identity,phase FROM organization_outputs WHERE plan_id=? AND destination=?`, p.ID, stageName).Scan(&id, &phase); err != nil {
			return err
		}
		if id == "" || phase != "published" || id != got.Identity {
			return problem("destination_changed", "A staged file was replaced and retained.")
		}
	}
	return nil
}

func (s *Service) executeCopiedFolder(ctx context.Context, root *os.Root, p *Plan) error {
	if p.Phase == "committed" {
		return s.cleanupCopiedFolder(ctx, root, p)
	}
	stage, err := folderExists(root, p.FolderMove.Stage)
	if err != nil {
		return err
	}
	dest, err := folderExists(root, p.FolderMove.Destination)
	if err != nil {
		return err
	}
	if stage && dest {
		return problem("destination_exists", "Both stage and destination exist; they were retained for review.")
	}
	if !dest {
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
		if err = s.copiedStage(ctx, root, p); err != nil {
			return err
		}
		if err = s.verifyCopiedFolder(ctx, root, *p, p.FolderMove.Stage); err != nil {
			return err
		}
		tx, e = s.db.BeginTx(ctx, nil)
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
			return problem("destination_exists", "A destination folder appeared and was left untouched.")
		}
		if err = renameFolderNoReplace(root, p.FolderMove.Stage, p.FolderMove.Destination); err != nil {
			return err
		}
	}
	if err = s.verifyCopiedFolder(ctx, root, *p, p.FolderMove.Destination); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = s.validateFolder(ctx, root, tx, *p, false); err != nil {
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
	if _, err = tx.ExecContext(ctx, `UPDATE organization_plans SET snapshot=?,updated_at=unixepoch() WHERE id=?`, string(raw), p.ID); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return s.cleanupCopiedFolder(ctx, root, p)
}

func (s *Service) cleanupCopiedFolder(ctx context.Context, root *os.Root, p *Plan) error {
	if err := s.verifyCopiedFolder(ctx, root, *p, p.FolderMove.Destination); err != nil {
		return err
	}
	for _, v := range p.FolderMove.Inventory.Files {
		if err := s.verifyCopiedFile(ctx, root, *p, v); err != nil {
			return err
		}
		if err := checkSource(ctx, root, v, true); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return problem("cleanup_pending", "A source file changed after copying and was retained for review.")
		}
		if err := root.Remove(filepath.FromSlash(v.Source)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return problem("cleanup_pending", "The copied folder is registered, but an old source could not be removed.")
		}
	}
	dirs := append([]folderDirectory(nil), p.FolderMove.Inventory.Directories...)
	sort.SliceStable(dirs, func(i, j int) bool { return len(dirs[i].Path) > len(dirs[j].Path) })
	for _, d := range dirs {
		if err := folderIdentity(root, d.Path, d.Identity); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return problem("cleanup_pending", "An original directory changed and was retained for review.")
		}
		if err := root.Remove(filepath.FromSlash(d.Path)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return problem("cleanup_pending", "An original directory is not empty or could not be removed; it was retained.")
		}
	}
	_, err := s.db.ExecContext(ctx, `UPDATE organization_plans SET state='completed',message='',updated_at=unixepoch() WHERE id=?`, p.ID)
	return err
}

func (s *Service) verifyCopiedFile(ctx context.Context, root *os.Root, p Plan, source Companion) error {
	name := folderDestination(p.FolderMove.Source, p.FolderMove.Destination, source.Source)
	entry, err := inspect(root, name)
	if err != nil {
		return err
	}
	var saved, phase string
	stage := folderDestination(p.FolderMove.Source, p.FolderMove.Stage, source.Source)
	if err = s.db.QueryRowContext(ctx, `SELECT identity,phase FROM organization_outputs WHERE plan_id=? AND destination=?`, p.ID, stage).Scan(&saved, &phase); err != nil {
		return err
	}
	if entry.Identity != saved || phase != "published" || entry.Size != source.Size {
		return problem("destination_changed", "A copied file was replaced and the source was retained.")
	}
	entry.SHA = source.SHA
	return checkSource(ctx, root, entry, true)
}
