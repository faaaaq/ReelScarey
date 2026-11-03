package organization

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"reflect"
	"sort"
	"strings"
	"time"
)

// Removal preserves the durable review, while deleting the catalog's dependent
// rows atomically. Only registered media and recognized companions are moved;
// extras, client sources and version backups are never deleted.
type Removal struct {
	MediaID int64         `json:"media_id"`
	Title   string        `json:"title"`
	Mode    string        `json:"mode"`
	Paths   []string      `json:"paths"`
	Rows    []RemovalRows `json:"affected_records"`
}
type RemovalRows struct {
	Table  string  `json:"table"`
	IDs    []int64 `json:"ids"`
	Digest string  `json:"digest"`
}
type foreignLink struct {
	child, parent string
	from, to      []string
}

func quoted(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }
func numbers(ids []int64) string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = fmt.Sprint(id)
	}
	return strings.Join(out, ",")
}

// Follow child references only, never parent accounts/configuration. Capture
// row contents as hashes so encrypted payloads cannot leak through the review.
func removalRows(ctx context.Context, q query, mediaID int64) ([]RemovalRows, error) {
	rows, err := q.QueryContext(ctx, `SELECT name FROM sqlite_schema WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		return nil, err
	}
	tables := []string{}
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			rows.Close()
			return nil, err
		}
		tables = append(tables, name)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	links := []foreignLink{}
	for _, table := range tables {
		r, err := q.QueryContext(ctx, `PRAGMA foreign_key_list(`+quoted(table)+`)`)
		if err != nil {
			return nil, err
		}
		groups := map[int]*foreignLink{}
		for r.Next() {
			var id, seq int
			var parent, from, to, onUpdate, onDelete, match string
			if err = r.Scan(&id, &seq, &parent, &from, &to, &onUpdate, &onDelete, &match); err != nil {
				r.Close()
				return nil, err
			}
			if to == "" {
				r.Close()
				return nil, problem("invalid_selection", "This schema has unsupported catalog references.")
			}
			if groups[id] == nil {
				groups[id] = &foreignLink{child: table, parent: parent}
			}
			groups[id].from = append(groups[id].from, from)
			groups[id].to = append(groups[id].to, to)
		}
		err = r.Err()
		r.Close()
		if err != nil {
			return nil, err
		}
		for _, v := range groups {
			links = append(links, *v)
		}
	}
	selected := map[string]map[int64]bool{"media_items": {mediaID: true}}
	changed := true
	for changed {
		changed = false
		for _, link := range links {
			parents := selected[link.parent]
			if len(parents) == 0 {
				continue
			}
			ids := []int64{}
			for id := range parents {
				ids = append(ids, id)
			}
			conditions := []string{}
			for i, from := range link.from {
				conditions = append(conditions, "c."+quoted(from)+"=p."+quoted(link.to[i]))
			}
			r, err := q.QueryContext(ctx, `SELECT c.rowid FROM `+quoted(link.child)+` c WHERE EXISTS(SELECT 1 FROM `+quoted(link.parent)+` p WHERE p.rowid IN (`+numbers(ids)+`) AND `+strings.Join(conditions, " AND ")+`)`)
			if err != nil {
				return nil, err
			}
			if selected[link.child] == nil {
				selected[link.child] = map[int64]bool{}
			}
			for r.Next() {
				var id int64
				if err = r.Scan(&id); err != nil {
					r.Close()
					return nil, err
				}
				if !selected[link.child][id] {
					selected[link.child][id] = true
					changed = true
				}
				if len(selected[link.child]) > 10000 {
					r.Close()
					return nil, problem("invalid_selection", "This title has too much history for a removal review.")
				}
			}
			err = r.Err()
			r.Close()
			if err != nil {
				return nil, err
			}
		}
		total := 0
		for _, ids := range selected {
			total += len(ids)
		}
		if total > 10000 {
			return nil, problem("invalid_selection", "This title has too much dependent history for one removal review.")
		}
	}
	out := []RemovalRows{}
	for _, table := range tables {
		if len(selected[table]) == 0 {
			continue
		}
		v := RemovalRows{Table: table, IDs: []int64{}}
		for id := range selected[table] {
			v.IDs = append(v.IDs, id)
		}
		sort.Slice(v.IDs, func(i, j int) bool { return v.IDs[i] < v.IDs[j] })
		r, err := q.QueryContext(ctx, `SELECT * FROM `+quoted(table)+` WHERE rowid IN (`+numbers(v.IDs)+`) ORDER BY rowid`)
		if err != nil {
			return nil, err
		}
		cols, err := r.Columns()
		if err != nil {
			r.Close()
			return nil, err
		}
		h := sha256.New()
		enc := json.NewEncoder(h)
		for r.Next() {
			values := make([]any, len(cols))
			dest := make([]any, len(cols))
			for i := range values {
				dest[i] = &values[i]
			}
			if err = r.Scan(dest...); err != nil {
				r.Close()
				return nil, err
			}
			if err = enc.Encode(values); err != nil {
				r.Close()
				return nil, err
			}
		}
		err = r.Err()
		r.Close()
		if err != nil {
			return nil, err
		}
		v.Digest = hex.EncodeToString(h.Sum(nil))
		out = append(out, v)
	}
	return out, nil
}

func (s *Service) validateRemoval(ctx context.Context, q query, p Plan) error {
	r := p.Removal
	if r.Mode != "keep" && r.Mode != "trash" {
		return problem("invalid_selection", "Choose whether to keep files or move them to retained trash.")
	}
	current, err := removalRows(ctx, q, r.MediaID)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(current, r.Rows) {
		return problem("revision_conflict", "The catalog or its history changed after review. Review removal again.")
	}
	ids, err := titleFiles(ctx, q, r.MediaID)
	if err != nil {
		return err
	}
	for _, id := range append(ids, 0) {
		if err = idle(ctx, q, r.MediaID, id); err != nil {
			return err
		}
	}
	var pending int
	if err = q.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM library_scans WHERE state NOT IN ('completed','cancelled','failed'))+(SELECT COUNT(*) FROM automation_runs WHERE state='running')`).Scan(&pending); err != nil {
		return err
	}
	if pending != 0 {
		return problem("media_busy", "Finish library scans and automatic searches before removing a title.")
	}
	return nil
}

func (s *Service) PreviewRemoval(ctx context.Context, mediaID int64, mode string) (Plan, error) {
	unlock := ReadMedia()
	defer unlock()
	if err := EnsureMediaAvailable(ctx, s.db); err != nil {
		return Plan{}, err
	}
	if mediaID < 1 || mode != "keep" && mode != "trash" {
		return Plan{}, problem("invalid_selection", "Choose a title and a file-removal option.")
	}
	root, resolved, err := s.openRoot("")
	if err != nil {
		return Plan{}, err
	}
	defer root.Close()
	if mode == "trash" {
		if err = s.prepareChecksums(ctx, root, nil, mediaID); err != nil {
			return Plan{}, err
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Plan{}, err
	}
	defer tx.Rollback()
	r := &Removal{MediaID: mediaID, Mode: mode, Paths: []string{}}
	if err = tx.QueryRowContext(ctx, `SELECT title FROM media_items WHERE id=?`, mediaID).Scan(&r.Title); err != nil {
		return Plan{}, err
	}
	r.Rows, err = removalRows(ctx, tx, mediaID)
	if err != nil {
		return Plan{}, err
	}
	config, err := s.config(ctx, tx)
	if err != nil {
		return Plan{}, err
	}
	now := time.Now().Unix()
	p := Plan{ID: id(), State: "review", Root: resolved, ConfigRevision: config.Revision, Phase: "planned", Files: []PlannedFile{}, Created: now, Updated: now, Removal: r}
	ids, err := titleFiles(ctx, tx, mediaID)
	if err != nil {
		return Plan{}, err
	}
	if len(ids) > 100 {
		return Plan{}, problem("invalid_selection", "Remove at most 100 registered files in one title review.")
	}
	for _, id := range ids {
		var name string
		if err = tx.QueryRowContext(ctx, `SELECT relative_path FROM media_files WHERE id=?`, id).Scan(&name); err != nil {
			return Plan{}, err
		}
		r.Paths = append(r.Paths, name)
	}
	if mode == "trash" && len(ids) > 0 {
		p.Files, err = s.build(ctx, root, tx, ids, "")
		if err != nil {
			return Plan{}, err
		}
		for i := range p.Files {
			v := &p.Files[i]
			v.Destination = path.Join(".reelscarey-trash", p.ID, v.Source)
			v.Unchanged = false
			for j := range v.Companions {
				v.Companions[j].Destination = path.Join(".reelscarey-trash", p.ID, v.Companions[j].Source)
			}
		}
	}
	if err = s.validateRemoval(ctx, tx, p); err != nil {
		return Plan{}, err
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return Plan{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO organization_plans(id,state,snapshot,created_at,updated_at) VALUES(?,'review',?,?,?)`, p.ID, string(raw), now, now); err != nil {
		return Plan{}, err
	}
	return p, tx.Commit()
}

func deleteRemovalRows(ctx context.Context, tx *sql.Tx, r *Removal) error {
	if _, err := tx.ExecContext(ctx, `PRAGMA defer_foreign_keys=ON`); err != nil {
		return err
	}
	for _, rows := range r.Rows {
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+quoted(rows.Table)+` WHERE rowid IN (`+numbers(rows.IDs)+`)`); err != nil {
			return err
		}
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'title',?,?,'removed from catalog')`, r.MediaID, r.Title)
	return err
}
