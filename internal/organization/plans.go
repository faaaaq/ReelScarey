package organization

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"time"
)

func id() string {
	v := make([]byte, 16)
	if _, e := rand.Read(v); e != nil {
		panic(e)
	}
	return hex.EncodeToString(v)
}
func (s *Service) episodes(ctx context.Context, q query, fileID int64) ([]Episode, error) {
	out := []Episode{}
	rows, e := q.QueryContext(ctx, `SELECT e.id,s.number,e.number,e.title,a.absolute_number FROM episode_files f JOIN episodes e ON e.id=f.episode_id JOIN seasons s ON s.id=e.season_id LEFT JOIN anime_episode_numbers a ON a.episode_id=e.id WHERE f.media_file_id=? ORDER BY s.number,e.number`, fileID)
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var v Episode
		if e = rows.Scan(&v.ID, &v.Season, &v.Number, &v.Title, &v.Absolute); e != nil {
			return out, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func idle(ctx context.Context, q query, mediaID, fileID int64) error {
	var n int
	e := q.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM downloads WHERE movie_id=? AND state NOT IN ('completed','cancelled'))+(SELECT COUNT(*) FROM subtitle_jobs WHERE media_file_id=? AND (lease_until>=unixepoch() OR (requested=1 AND state IN ('queued','running','retrying','waiting_provider'))))+(SELECT COUNT(*) FROM subtitle_imports WHERE media_file_id=? AND phase<>'committed')+(SELECT COUNT(*) FROM youtube_jobs WHERE media_id=? AND state NOT IN ('completed','cancelled','failed'))+(SELECT COUNT(*) FROM youtube_imports i JOIN youtube_jobs j ON j.id=i.job_id WHERE j.media_id=? AND i.phase<>'committed')`, mediaID, fileID, fileID, mediaID, mediaID).Scan(&n)
	if e != nil {
		return e
	}
	if n != 0 {
		return problem("media_busy", "Finish or cancel pending downloads and subtitle work before organizing these files.")
	}
	return nil
}
func (s *Service) build(ctx context.Context, root *os.Root, q query, ids []int64, target string) ([]PlannedFile, error) {
	out := []PlannedFile{}
	destinations := map[string]bool{}
	for _, fileID := range ids {
		var v PlannedFile
		if e := q.QueryRowContext(ctx, `SELECT id,COALESCE(movie_id,series_id),relative_path,size_bytes,sha256 FROM media_files WHERE id=?`, fileID).Scan(&v.FileID, &v.MediaID, &v.Source, &v.Size, &v.SHA); e != nil {
			return nil, e
		}
		if e := idle(ctx, q, v.MediaID, v.FileID); e != nil {
			return nil, e
		}
		snapshot, e := s.capture(ctx, q, v.MediaID, target)
		if e != nil {
			return nil, e
		}
		v.Snapshot = snapshot
		v.Title = snapshot.Content.Title
		v.Episodes, e = s.episodes(ctx, q, fileID)
		if e != nil {
			return nil, e
		}
		v.Destination, e = Destination(snapshot, v.Episodes, v.Source)
		if e != nil {
			return nil, e
		}
		v.Unchanged = v.Source == v.Destination
		if !v.Unchanged && strings.EqualFold(v.Source, v.Destination) {
			return nil, problem("destination_exists", "Case-only renames need a different name or folder; they are not applied automatically.")
		}
		info, e := inspect(root, v.Source)
		if e != nil {
			return nil, e
		}
		if info.Size != v.Size {
			return nil, problem("source_changed", "A file's size no longer matches its registered import.")
		}
		v.Mtime = info.Mtime
		v.Identity = info.Identity
		v.Companions, e = companions(ctx, root, v.Source, v.Destination)
		if e != nil {
			return nil, e
		}
		// Every registered external subtitle must be represented in the reviewed move.
		rows, e := q.QueryContext(ctx, `SELECT relative_path FROM subtitles WHERE media_file_id=? AND relative_path<>'' UNION SELECT destination FROM subtitle_imports WHERE media_file_id=? AND phase='committed'`, fileID, fileID)
		if e != nil {
			return nil, e
		}
		var registered []string
		for rows.Next() {
			var name string
			if e = rows.Scan(&name); e != nil {
				rows.Close()
				return nil, e
			}
			registered = append(registered, name)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return nil, e
		}
		for _, name := range registered {
			found := false
			for _, c := range v.Companions {
				if c.Source == name {
					found = true
				}
			}
			if !found {
				return nil, problem("unsupported_companion", "A registered subtitle has an unrelated name or location. Review it before organizing this video.")
			}
		}
		all := append([]Companion{{Source: v.Source, Destination: v.Destination}}, v.Companions...)
		for _, c := range all {
			key := strings.ToLower(c.Destination)
			if destinations[key] {
				return nil, problem("destination_exists", "Two selected files would use the same destination.")
			}
			destinations[key] = true
			if c.Source != c.Destination {
				exists, e := existsFolded(root, c.Destination)
				if e != nil {
					return nil, e
				}
				if exists {
					return nil, problem("destination_exists", "A destination already exists. No files were changed.")
				}
				var count int
				if e = q.QueryRowContext(ctx, `SELECT COUNT(*) FROM media_files WHERE relative_path=? COLLATE NOCASE AND id<>?`, c.Destination, fileID).Scan(&count); e != nil {
					return nil, e
				}
				if count != 0 {
					return nil, problem("destination_exists", "A destination is already registered to another media file.")
				}
			}
		}
		out = append(out, v)
	}
	return out, nil
}
func (s *Service) Preview(ctx context.Context, ids []int64, target string) (Plan, error) {
	return s.preview(ctx, ids, 0, target)
}

// PreviewTitle captures the complete registered-file set in the same transaction
// as the review. Large titles must be organized in explicit smaller selections.
func (s *Service) PreviewTitle(ctx context.Context, mediaID int64, target string) (Plan, error) {
	if mediaID < 1 || target == "" {
		return Plan{}, problem("invalid_selection", "Choose a title and a destination collection.")
	}
	return s.preview(ctx, nil, mediaID, target)
}

func titleFiles(ctx context.Context, q query, mediaID int64) ([]int64, error) {
	rows, err := q.QueryContext(ctx, `SELECT id FROM media_files WHERE movie_id=? OR series_id=? ORDER BY id LIMIT 101`, mediaID, mediaID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Service) preview(ctx context.Context, ids []int64, mediaID int64, target string) (Plan, error) {
	if mediaID == 0 {
		if len(ids) < 1 || len(ids) > 100 {
			return Plan{}, problem("invalid_selection", "Choose 1–100 registered media files.")
		}
		seen := map[int64]bool{}
		for _, n := range ids {
			if n < 1 || seen[n] {
				return Plan{}, problem("invalid_selection", "Choose distinct registered media files.")
			}
			seen[n] = true
		}
	}
	unlock := ReadMedia()
	defer unlock()
	root, resolved, e := s.openRoot("")
	if e != nil {
		return Plan{}, e
	}
	defer root.Close()
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return Plan{}, e
	}
	defer tx.Rollback()
	if mediaID != 0 {
		ids, e = titleFiles(ctx, tx, mediaID)
		if e != nil {
			return Plan{}, e
		}
		if len(ids) == 0 || len(ids) > 100 {
			return Plan{}, problem("invalid_selection", "A whole-title move requires 1–100 registered files. For larger titles, select files in batches.")
		}
	}
	files, e := s.build(ctx, root, tx, ids, target)
	if e != nil {
		return Plan{}, e
	}
	c, e := s.config(ctx, tx)
	if e != nil {
		return Plan{}, e
	}
	now := time.Now().Unix()
	p := Plan{ID: id(), State: "review", ConfigRevision: c.Revision, TargetLibrary: target, EntireMediaID: mediaID, Files: files, Created: now, Updated: now, Root: resolved, Phase: "planned"}
	raw, e := json.Marshal(p)
	if e != nil {
		return p, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO organization_plans(id,state,snapshot,created_at,updated_at) VALUES(?,'review',?,?,?)`, p.ID, string(raw), now, now); e != nil {
		return p, e
	}
	return p, tx.Commit()
}
func (s *Service) Plan(ctx context.Context, id string) (Plan, error) { return readPlan(ctx, s.db, id) }
func readPlan(ctx context.Context, q query, id string) (Plan, error) {
	var p Plan
	var raw, state, message string
	var updated int64
	if e := q.QueryRowContext(ctx, `SELECT snapshot,state,message,updated_at FROM organization_plans WHERE id=?`, id).Scan(&raw, &state, &message, &updated); e != nil {
		return p, e
	}
	if e := json.Unmarshal([]byte(raw), &p); e != nil {
		return p, e
	}
	p.State = state
	p.Message = message
	p.Updated = updated
	return p, nil
}
func (s *Service) Plans(ctx context.Context) ([]Plan, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT id FROM organization_plans ORDER BY CASE WHEN state IN ('queued','running') OR (state='failed' AND (json_extract(snapshot,'$.folder_move') IS NOT NULL OR json_extract(snapshot,'$.replacement') IS NOT NULL) AND json_extract(snapshot,'$.phase') NOT IN ('committed','failed_safe')) THEN 0 ELSE 1 END,created_at DESC,id DESC LIMIT 20`)
	if e != nil {
		return nil, e
	}
	var ids []string
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
	out := []Plan{}
	for _, id := range ids {
		p, e := s.Plan(ctx, id)
		if e != nil {
			return nil, e
		}
		out = append(out, p)
	}
	return out, nil
}
func (s *Service) Apply(ctx context.Context, id string) (Plan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	unlock := ReadMedia()
	defer unlock()
	p, e := s.Plan(ctx, id)
	if e != nil {
		return p, e
	}
	if p.State == "completed" || p.State == "queued" || p.State == "running" {
		return p, nil
	}
	if p.State != "review" && p.State != "failed" {
		return p, problem("invalid_selection", "Review a saved plan before applying.")
	}
	if p.Phase == "failed_safe" {
		return p, problem("plan_stale", "This stopped job is safe but cannot be resumed. Create a new review.")
	}
	root, _, e := s.openRoot(p.Root)
	if e != nil {
		return p, e
	}
	defer root.Close()
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return p, e
	}
	defer tx.Rollback()
	if p.Phase == "planned" {
		if p.Replacement != nil {
			e = s.validateReplacement(ctx, root, tx, p)
		} else if p.FolderMove != nil {
			src, err := folderExists(root, p.FolderMove.Source)
			if err != nil {
				return p, err
			}
			dst, err := folderExists(root, p.FolderMove.Destination)
			if err != nil {
				return p, err
			}
			if src == dst {
				return p, problem("source_changed", "The reviewed folder locations changed.")
			}
			e = s.validateFolder(ctx, root, tx, p, !src)
		} else {
			e = s.validate(ctx, root, tx, p, false)
		}
		if e != nil {
			return p, e
		}
	}
	var count int
	if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM organization_plans WHERE (state IN ('queued','running') OR (state='failed' AND (json_extract(snapshot,'$.folder_move') IS NOT NULL OR json_extract(snapshot,'$.replacement') IS NOT NULL) AND json_extract(snapshot,'$.phase') NOT IN ('committed','failed_safe'))) AND id<>?`, id).Scan(&count); e != nil {
		return p, e
	}
	if count != 0 {
		return p, problem("organization_busy", "Wait for the current organization batch to finish.")
	}
	if _, e = tx.ExecContext(ctx, `UPDATE organization_plans SET state='queued',message='',updated_at=unixepoch() WHERE id=? AND state IN ('review','failed')`, id); e != nil {
		return p, e
	}
	if e = tx.Commit(); e != nil {
		return p, e
	}
	return s.Plan(ctx, id)
}
func (s *Service) validate(ctx context.Context, root *os.Root, q query, p Plan, allowPublished bool) error {
	if p.EntireMediaID != 0 {
		ids, err := titleFiles(ctx, q, p.EntireMediaID)
		if err != nil {
			return err
		}
		if len(ids) != len(p.Files) {
			return problem("revision_conflict", "The title's registered files changed after review. Create a new review.")
		}
		for i, id := range ids {
			if id != p.Files[i].FileID {
				return problem("revision_conflict", "The title's registered files changed after review. Create a new review.")
			}
		}
	}
	owned := map[string]string{}
	if allowPublished {
		rows, e := q.QueryContext(ctx, `SELECT destination,identity FROM organization_outputs WHERE plan_id=? AND identity<>''`, p.ID)
		if e != nil {
			return e
		}
		for rows.Next() {
			var name, id string
			if e = rows.Scan(&name, &id); e != nil {
				rows.Close()
				return e
			}
			owned[name] = id
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
	}
	c, e := s.config(ctx, q)
	if e != nil {
		return e
	}
	if c.Revision != p.ConfigRevision {
		return problem("revision_conflict", "Organization rules changed after review. Create a new review.")
	}
	for _, v := range p.Files {
		snap, e := s.capture(ctx, q, v.MediaID, p.TargetLibrary)
		if e != nil {
			return e
		}
		if !reflect.DeepEqual(snap, v.Snapshot) {
			return problem("revision_conflict", "A title, category, library assignment or naming preference changed after review.")
		}
		var source, sha string
		var size int64
		if e = q.QueryRowContext(ctx, `SELECT relative_path,sha256,size_bytes FROM media_files WHERE id=?`, v.FileID).Scan(&source, &sha, &size); e != nil {
			return e
		}
		if source != v.Source || sha != v.SHA || size != v.Size {
			return problem("revision_conflict", "A registered file changed after review.")
		}
		episodes, e := s.episodes(ctx, q, v.FileID)
		if e != nil {
			return e
		}
		if !reflect.DeepEqual(episodes, v.Episodes) {
			return problem("revision_conflict", "Episode identities or absolute mappings changed after review.")
		}
		if e = idle(ctx, q, v.MediaID, v.FileID); e != nil {
			return e
		}
		all := append([]Companion{{Source: v.Source, Destination: v.Destination, Size: v.Size, Mtime: v.Mtime, SHA: v.SHA, Identity: v.Identity}}, v.Companions...)
		for _, f := range all {
			if e = checkSource(ctx, root, f, false); e != nil {
				return e
			}
			if f.Source != f.Destination && !allowPublished {
				exists, e := existsFolded(root, f.Destination)
				if e != nil {
					return e
				}
				if exists {
					return problem("destination_exists", "A destination appeared after review. It was left unchanged.")
				}
			}
		}
		current, e := companionsIgnoring(ctx, root, v.Source, v.Destination, owned)
		if e != nil {
			return e
		}
		if !reflect.DeepEqual(current, v.Companions) {
			return problem("source_changed", "Subtitle companions changed after review.")
		}
	}
	return nil
}
func (s *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		_ = s.Tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (s *Service) Tick(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	mediaGate.Lock()
	defer mediaGate.Unlock()
	var id string
	e := s.db.QueryRowContext(ctx, `SELECT id FROM organization_plans WHERE state IN ('queued','running') ORDER BY created_at LIMIT 1`).Scan(&id)
	if errors.Is(e, sql.ErrNoRows) {
		return nil
	}
	if e != nil {
		return e
	}
	p, e := s.Plan(ctx, id)
	if e != nil {
		return e
	}
	if _, e = s.db.ExecContext(ctx, `UPDATE organization_plans SET state='running',updated_at=unixepoch() WHERE id=?`, id); e != nil {
		return e
	}
	e = s.execute(ctx, &p)
	if e != nil {
		if p.Replacement != nil && (p.Phase == "planned" || p.Phase == "prepared") {
			if root, _, err := s.openRoot(p.Root); err == nil {
				// No source deletion occurs in these phases. Avoid an unbounded
				// checksum during shutdown while confirming the original path is intact.
				if checkSource(context.WithoutCancel(ctx), root, p.Replacement.Original, false) == nil {
					_ = s.savePhase(context.WithoutCancel(ctx), &p, "failed_safe")
				}
				root.Close()
			}
		}
		if p.FolderMove != nil && s.folderStillAtSource(p) {
			_ = s.savePhase(context.WithoutCancel(ctx), &p, "failed_safe")
		}
		message := "Organization stopped safely. Original files and verified outputs were retained; review the failure before retrying."
		var problem *Error
		if errors.As(e, &problem) {
			message = problem.Message
		}
		_, _ = s.db.ExecContext(context.WithoutCancel(ctx), `UPDATE organization_plans SET state='failed',message=?,updated_at=unixepoch() WHERE id=?`, message, id)
	}
	return e
}
func (s *Service) savePhase(ctx context.Context, p *Plan, phase string) error {
	p.Phase = phase
	raw, e := json.Marshal(p)
	if e != nil {
		return e
	}
	_, e = s.db.ExecContext(ctx, `UPDATE organization_plans SET snapshot=?,updated_at=unixepoch() WHERE id=?`, string(raw), p.ID)
	return e
}

// CancelQueued returns an unstarted job to review. Jobs with any journaled
// output or a moved folder must instead finish or use their recovery flow.
func (s *Service) CancelQueued(ctx context.Context, id string) (Plan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	unlock := ReadMedia()
	defer unlock()
	p, e := s.Plan(ctx, id)
	if e != nil {
		return p, e
	}
	if p.State != "queued" || p.Phase != "planned" {
		return p, problem("invalid_selection", "Only a queued job that has not started can be cancelled.")
	}
	root, _, e := s.openRoot(p.Root)
	if e != nil {
		return p, e
	}
	defer root.Close()
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return p, e
	}
	defer tx.Rollback()
	var outputs int
	if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM organization_outputs WHERE plan_id=?`, id).Scan(&outputs); e != nil {
		return p, e
	}
	if outputs != 0 {
		return p, problem("work_pending", "This job has saved file work and must be resumed.")
	}
	if p.FolderMove != nil {
		src, err := folderExists(root, p.FolderMove.Source)
		if err != nil {
			return p, err
		}
		dst, err := folderExists(root, p.FolderMove.Destination)
		if err != nil {
			return p, err
		}
		if !src || dst || p.FolderMove.Copy || p.FolderMove.Stage != "" {
			return p, problem("work_pending", "This folder job may have started and must be resumed.")
		}
		if e = s.validateFolder(ctx, root, tx, p, false); e != nil {
			return p, e
		}
	}
	res, e := tx.ExecContext(ctx, `UPDATE organization_plans SET state='review',message='Queue cancelled before file changes.',updated_at=unixepoch() WHERE id=? AND state='queued'`, id)
	if e != nil {
		return p, e
	}
	n, e := res.RowsAffected()
	if e != nil {
		return p, e
	}
	if n != 1 {
		return p, problem("work_pending", "The job started and must be resumed.")
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'organization',?,'Library organization','queue cancelled')`, id); e != nil { return p, e }
	if e = tx.Commit(); e != nil {
		return p, e
	}
	return s.Plan(ctx, id)
}
