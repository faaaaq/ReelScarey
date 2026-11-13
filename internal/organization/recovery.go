package organization

import (
	"context"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// The episode-file removal (internal/transfer stageMediaRemoval) renames the
// library copy beside itself before its removal transaction commits. A crash
// between that rename and the commit leaves the catalog referencing a path
// whose content lives only under a `.reelscarey-removing-<ns>` neighbour
// name; the in-process restore and its Activity journal ran with the process
// that crashed, so no durable warning exists (NEW-B1). The sweep below
// re-derives the state from the filesystem and the catalog at startup.
const (
	stagedRemovalSuffix   = ".reelscarey-removing-"
	stagedRemovalGrace    = 5 * time.Minute
	stagedRemovalRepaired = "staged removal reconciled at startup"
	stagedRemovalOrphan   = "orphaned staged copy; delete manually"
	stagedRemovalLeft     = "leftover staged copy; review its location"
)

// RecoverStagedRemovals reconciles every staging copy an interrupted
// episode-file removal left inside the media root. The in-process restore
// path covers failures the process survived; this sweep covers the crash
// windows it cannot see:
//   - original path missing on disk and registered to a media_files row:
//     the removal transaction never committed, so the staged copy is the
//     file's only content — rename it back and journal the reconciliation;
//   - original path missing on disk and unregistered: the commit landed but
//     the post-commit cleanup did not — journal the orphan for manual
//     deletion; content is never auto-deleted (NEW-B2);
//   - original path present on disk: a leftover copy beside a live file —
//     journal its location and leave both untouched.
//
// Staged copies younger than the grace period are ignored entirely: an
// in-flight removal stages inside an open transaction and restores itself
// on failure, so a fresh staging name is not evidence of a crash.
func (s *Service) RecoverStagedRemovals(ctx context.Context) error {
	root, _, err := s.openRoot("")
	if err != nil {
		return err
	}
	defer root.Close()
	return s.sweepStagedRemovals(ctx, root, ".", 0)
}

func (s *Service) sweepStagedRemovals(ctx context.Context, root *os.Root, dir string, depth int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if depth > 20 {
		return nil
	}
	f, err := root.Open(filepath.FromSlash(dir))
	if err != nil {
		return err
	}
	entries, err := f.ReadDir(-1)
	f.Close()
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		name := entry.Name()
		child := path.Join(dir, name)
		if entry.IsDir() {
			if strings.HasPrefix(name, ".") {
				continue
			}
			if err := s.sweepStagedRemovals(ctx, root, child, depth+1); err != nil {
				return err
			}
			continue
		}
		if !entry.Type().IsRegular() {
			continue
		}
		idx := strings.LastIndex(name, stagedRemovalSuffix)
		if idx < 1 {
			continue
		}
		ns, err := strconv.ParseInt(name[idx+len(stagedRemovalSuffix):], 10, 64)
		if err != nil || ns <= 0 {
			continue
		}
		if time.Since(time.Unix(0, ns)) < stagedRemovalGrace {
			continue
		}
		original := path.Join(dir, name[:idx])
		s.reconcileStagedRemoval(ctx, root, original, child)
	}
	return nil
}

// reconcileStagedRemoval applies the three-case repair and journals the
// outcome as an Activity event. The journal insert is deduplicated on
// (object_id, state) so a leftover an operator has not yet resolved
// produces one event per state instead of one per startup.
func (s *Service) reconcileStagedRemoval(ctx context.Context, root *os.Root, original, staged string) {
	journal := func(objectID, message, state string) {
		_, _ = s.db.ExecContext(context.WithoutCancel(ctx), `INSERT INTO activity_events(occurred_at,kind,object_id,title,state)
			SELECT unixepoch(),'title',?,?,? WHERE NOT EXISTS (SELECT 1 FROM activity_events WHERE kind='title' AND object_id=? AND state=?)`,
			objectID, message, state, objectID, state)
	}
	exists, err := existsFolded(root, original)
	if err != nil {
		journal(staged, "A staged removal copy could not be checked against its original path ("+err.Error()+").", stagedRemovalLeft)
		return
	}
	if exists {
		journal(staged, "A staged removal copy remains beside a live file at "+original+". Delete the staged copy or restore it manually, then rescan.", stagedRemovalLeft)
		return
	}
	var registered int
	if err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM media_files WHERE relative_path=? COLLATE NOCASE`, original).Scan(&registered); err != nil {
		journal(staged, "A staged removal copy could not be checked against the catalog ("+err.Error()+").", stagedRemovalLeft)
		return
	}
	if registered == 0 {
		journal(staged, "An orphaned staged removal copy remains at "+staged+" after its removal committed. The catalog no longer references it; delete the file manually.", stagedRemovalOrphan)
		return
	}
	// The removal never committed: the catalog still references the original
	// path and the staged copy is its only content. Rename it back.
	if err = root.Rename(filepath.FromSlash(staged), filepath.FromSlash(original)); err != nil {
		journal(staged, "The staged removal copy at "+staged+" could not be restored to its registered path "+original+" ("+err.Error()+"). Restore or delete it manually, then rescan.", stagedRemovalLeft)
		return
	}
	journal(original, "The staged removal copy of "+original+" was restored to its registered path after an interrupted removal.", stagedRemovalRepaired)
}
