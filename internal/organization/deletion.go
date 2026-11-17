package organization

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/faaaaq/ReelScarey/internal/filecheck"
)

type DeletionRequest struct {
	Selection string  `json:"selection"`
	IDs       []int64 `json:"ids"`
	Mode      string  `json:"mode"`
}

func deletionTable(kind string) (table, label string) {
	switch kind {
	case "titles":
		return "media_items", "title"
	case "video_files":
		return "media_files", "video file"
	case "artists":
		return "music_artists", "artist"
	case "albums":
		return "music_albums", "album"
	case "tracks":
		return "music_tracks", "track"
	case "music_files":
		return "music_files", "music file"
	}
	return "", ""
}

// deletionFiles resolves only registered paths, including every copy of a
// selected music track. Episode range files are selected by their file ID.
func deletionFiles(ctx context.Context, q query, kind string, ids []int64) ([]int64, error) {
	match := ""
	table := "music_files"
	switch kind {
	case "titles":
		table, match = "media_files", "COALESCE(movie_id,series_id)"
	case "video_files":
		return ids, nil
	case "artists":
		match = "artist_id"
	case "albums":
		match = "album_id"
	case "tracks":
		match = "track_id"
	case "music_files":
		return ids, nil
	}
	rows, err := q.QueryContext(ctx, "SELECT id FROM "+table+" WHERE "+match+" IN ("+numbers(ids)+") ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var found []int64
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		found = append(found, id)
	}
	return found, rows.Err()
}

func (s *Service) PreviewDeletion(ctx context.Context, request DeletionRequest) (Plan, error) {
	defer ReadMedia()()
	if err := EnsureMediaAvailable(ctx, s.db); err != nil {
		return Plan{}, err
	}
	table, label := deletionTable(request.Selection)
	ids := slices.Clone(request.IDs)
	slices.Sort(ids)
	if table == "" || len(ids) == 0 || len(ids) > 5000 || ids[0] < 1 ||
		len(slices.Compact(slices.Clone(ids))) != len(ids) ||
		(request.Mode != "keep" && request.Mode != "trash") {
		return Plan{}, problem("invalid_selection", "Choose saved items and a file removal option.")
	}
	root, resolved, err := s.openRoot("")
	if err != nil {
		return Plan{}, err
	}
	defer root.Close()
	video := table == "media_items" || table == "media_files"
	fileTable := "music_files"
	if video {
		fileTable = "media_files"
	}
	files, err := deletionFiles(ctx, s.db, request.Selection, ids)
	if err != nil {
		return Plan{}, err
	}
	if video && request.Mode == "trash" {
		for _, id := range files {
			if _, err := filecheck.EnsureSHA(ctx, s.db, root, id); err != nil {
				return Plan{}, problem("source_changed", "A video is missing or has changed since registration. Review its registration.")
			}
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Plan{}, err
	}
	defer tx.Rollback()
	var count int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table+" WHERE id IN ("+numbers(ids)+")").Scan(&count); err != nil {
		return Plan{}, err
	}
	if count != len(ids) {
		return Plan{}, sql.ErrNoRows
	}
	current, err := deletionFiles(ctx, tx, request.Selection, ids)
	if err != nil {
		return Plan{}, err
	}
	if !slices.Equal(files, current) {
		return Plan{}, problem("revision_conflict", "The selected registrations changed. Review the selection again.")
	}
	title := fmt.Sprintf("%d %ss", count, label)
	if count == 1 {
		title = "1 " + label
	}
	r := &Removal{Title: title, Mode: request.Mode, Selection: request.Selection, IDs: ids, Paths: []string{}}
	now := time.Now().Unix()
	p := Plan{ID: id(), State: "review", Root: resolved, Phase: "planned", Files: []PlannedFile{}, Created: now, Updated: now, Removal: r}
	seen := map[string]bool{}
	for _, fileID := range files {
		var name, sha string
		var size int64
		if err = tx.QueryRowContext(ctx, "SELECT relative_path,size_bytes,sha256 FROM "+fileTable+" WHERE id=?", fileID).Scan(&name, &size, &sha); err != nil {
			return Plan{}, err
		}
		r.Paths = append(r.Paths, name)
		if r.Mode == "keep" {
			continue
		}
		asset, err := inspect(root, name)
		if err != nil || asset.Size != size {
			return Plan{}, problem("source_changed", "A registered file is missing or its size changed. Review the file before moving it to trash.")
		}
		if sha == "" {
			sha, err = assetDigest(ctx, root, name)
			if err != nil {
				return Plan{}, err
			}
		}
		asset.SHA = sha
		asset.Destination = path.Join(".reelscarey-trash", p.ID, name)
		seen[name] = true
		r.Assets = append(r.Assets, asset)
		if video {
			subs, err := removableSidecars(ctx, root, tx, fileID, name, files)
			if err != nil {
				return Plan{}, err
			}
			for _, sub := range subs {
				if !seen[sub.Source] {
					sub.Destination = path.Join(".reelscarey-trash", p.ID, sub.Source)
					r.Assets = append(r.Assets, sub)
					r.Paths = append(r.Paths, sub.Source)
					seen[sub.Source] = true
				}
			}
		}
	}
	r.Rows, err = deletionRows(ctx, tx, r, files)
	if err != nil {
		return Plan{}, err
	}
	if err = deletionIdle(ctx, tx, r, files); err != nil {
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

func assetDigest(ctx context.Context, root *os.Root, name string) (string, error) {
	f, err := root.Open(filepath.FromSlash(name))
	if err != nil {
		return "", err
	}
	defer f.Close()
	return hash(ctx, f)
}

func deletionRows(ctx context.Context, q query, r *Removal, files []int64) ([]RemovalRows, error) {
	table, _ := deletionTable(r.Selection)
	roots := map[string]map[int64]bool{table: {}}
	for _, id := range r.IDs {
		roots[table][id] = true
	}
	if table != "media_files" && table != "media_items" {
		if roots["music_files"] == nil {
			roots["music_files"] = map[int64]bool{}
		}
		for _, id := range files {
			roots["music_files"][id] = true
		}
	}
	return dependentRows(ctx, q, roots, true)
}

func deletionIdle(ctx context.Context, q query, r *Removal, files []int64) error {
	fileIDs := numbers(append(slices.Clone(files), 0))
	var busy int
	switch r.Selection {
	case "titles", "video_files":
		if r.Selection == "titles" {
			for _, id := range r.IDs {
				if err := idle(ctx, q, id, 0); err != nil {
					return err
				}
			}
		}
		for _, fileID := range files {
			var owner int64
			if err := q.QueryRowContext(ctx, `SELECT COALESCE(movie_id,series_id) FROM media_files WHERE id=?`, fileID).Scan(&owner); err != nil {
				return err
			}
			if err := idle(ctx, q, owner, fileID); err != nil {
				return err
			}
		}
		if err := q.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM library_scans WHERE state NOT IN ('completed','cancelled','failed'))+
			(SELECT COUNT(*) FROM automation_runs WHERE state='running')`).Scan(&busy); err != nil {
			return err
		}
	default:
		if err := q.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM music_scans WHERE state='scanning' OR request<>'')+
			(SELECT COUNT(*) FROM music_import_jobs WHERE state='running')+
			(SELECT COUNT(*) FROM music_acquisition_jobs WHERE state IN ('queued','running'))+
			(SELECT COUNT(*) FROM music_downloads WHERE state NOT IN ('completed','cancelled'))+
			(SELECT COUNT(*) FROM music_conversion_jobs WHERE state IN ('queued','processing') AND kind<>'download' AND file_id IN (`+fileIDs+`))+
			(SELECT COUNT(*) FROM music_edit_jobs WHERE state IN ('queued','running'))`).Scan(&busy); err != nil {
			return err
		}
	}
	if busy != 0 {
		return problem("media_busy", "Finish the active library work before deleting these items.")
	}
	return nil
}

func (s *Service) validateDeletion(ctx context.Context, root *os.Root, q query, p Plan, published bool) error {
	r := p.Removal
	files, err := deletionFiles(ctx, q, r.Selection, r.IDs)
	if err != nil {
		return err
	}
	rows, err := deletionRows(ctx, q, r, files)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(rows, r.Rows) {
		return problem("revision_conflict", "The selected items or their registrations changed. Create a new review.")
	}
	if err = deletionIdle(ctx, q, r, files); err != nil {
		return err
	}
	for _, asset := range r.Assets {
		if err = checkSource(ctx, root, asset, false); err != nil {
			return err
		}
		if !published {
			exists, err := existsFolded(root, asset.Destination)
			if err != nil {
				return err
			}
			if exists {
				return problem("destination_exists", "A trash destination appeared after review.")
			}
		}
	}
	return nil
}

func selectedParentIDs(ctx context.Context, tx *sql.Tx, statement string) (string, error) {
	rows, err := tx.QueryContext(ctx, statement)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	ids := []int64{0}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			return "", err
		}
		ids = append(ids, id)
	}
	return numbers(ids), rows.Err()
}

func deleteSelectionRows(ctx context.Context, tx *sql.Tx, r *Removal) error {
	files, err := deletionFiles(ctx, tx, r.Selection, r.IDs)
	if err != nil {
		return err
	}
	fileIDs := numbers(append(files, 0))
	if _, err = tx.ExecContext(ctx, `PRAGMA defer_foreign_keys=ON`); err != nil {
		return err
	}
	var mediaIDs, artistIDs, albumIDs, trackIDs string
	switch r.Selection {
	case "titles", "video_files":
		mediaIDs, err = selectedParentIDs(ctx, tx, `SELECT DISTINCT COALESCE(movie_id,series_id) FROM media_files WHERE id IN (`+fileIDs+`)`)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE library_scan_files SET media_file_id=NULL WHERE media_file_id IN (`+fileIDs+`)`); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE series_details SET revision=revision+1 WHERE media_id IN (`+mediaIDs+`)`); err != nil {
			return err
		}
	default:
		artistIDs, err = selectedParentIDs(ctx, tx, `SELECT DISTINCT artist_id FROM music_files WHERE id IN (`+fileIDs+`)`)
		if err != nil {
			return err
		}
		albumIDs, err = selectedParentIDs(ctx, tx, `SELECT DISTINCT album_id FROM music_files WHERE id IN (`+fileIDs+`)`)
		if err != nil {
			return err
		}
		trackIDs, err = selectedParentIDs(ctx, tx, `SELECT DISTINCT track_id FROM music_files WHERE id IN (`+fileIDs+`) AND track_id IS NOT NULL`)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM music_conversion_jobs WHERE kind<>'download' AND file_id IN (`+fileIDs+`)`); err != nil {
			return err
		}
	}
	for _, rows := range r.Rows {
		if _, err = tx.ExecContext(ctx, "DELETE FROM "+quoted(rows.Table)+" WHERE rowid IN ("+numbers(rows.IDs)+")"); err != nil {
			return err
		}
	}
	switch r.Selection {
	case "video_files":
		_, err = tx.ExecContext(ctx, `UPDATE media_items SET status='missing' WHERE id IN (`+mediaIDs+`) AND status='available' AND NOT EXISTS(SELECT 1 FROM media_files WHERE movie_id=media_items.id OR series_id=media_items.id)`)
	case "artists", "albums", "tracks", "music_files":
		_, err = tx.ExecContext(ctx, `UPDATE music_albums SET status='missing' WHERE id IN (`+albumIDs+`) AND status='available' AND NOT EXISTS(SELECT 1 FROM music_files WHERE album_id=music_albums.id);
			UPDATE music_artists SET status='missing' WHERE id IN (`+artistIDs+`) AND status='available' AND NOT EXISTS(SELECT 1 FROM music_files WHERE artist_id=music_artists.id);
			UPDATE music_files SET variant_role='primary' WHERE id IN (
				SELECT MIN(f.id) FROM music_files f WHERE f.track_id IN (`+trackIDs+`)
				AND NOT EXISTS(SELECT 1 FROM music_files p WHERE p.track_id=f.track_id AND p.variant_role='primary')
				GROUP BY f.track_id)`)
	}
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'title',?,?,?)`, fmt.Sprint(r.IDs[0]), r.Title, "deleted from library")
	return err
}

// Match a sidecar to its video by basename or an explicit registration. A
// sidecar shared with an unselected registration stays at its original path.
func removableSidecars(ctx context.Context, root *os.Root, q query, fileID int64, video string, selected []int64) ([]Companion, error) {
	stem := strings.TrimSuffix(video, path.Ext(video))
	folder, err := root.Open(filepath.FromSlash(path.Dir(video)))
	if err != nil {
		return nil, err
	}
	names, err := folder.Readdirnames(10001)
	folder.Close()
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if len(names) > 10000 {
		return nil, problem("too_many_companions", "The video folder has too many entries.")
	}
	rows, err := q.QueryContext(ctx, `SELECT relative_path FROM subtitles WHERE media_file_id=? AND relative_path<>''`, fileID)
	if err != nil {
		return nil, err
	}
	candidates := map[string]bool{}
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			rows.Close()
			return nil, err
		}
		candidates[name] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for _, name := range names {
		full := path.Join(path.Dir(video), name)
		if strings.HasPrefix(strings.ToLower(full), strings.ToLower(stem)+".") && isSubtitleExtension(full) {
			candidates[full] = true
		}
	}
	keys := make([]string, 0, len(candidates))
	for name := range candidates {
		keys = append(keys, name)
	}
	slices.Sort(keys)
	out := []Companion{}
	for _, name := range keys {
		var shared int
		if err = q.QueryRowContext(ctx, `SELECT COUNT(*) FROM subtitles WHERE relative_path=? COLLATE NOCASE AND media_file_id NOT IN (`+numbers(selected)+`)`, name).Scan(&shared); err != nil {
			return nil, err
		}
		if shared == 0 {
			if err = q.QueryRowContext(ctx, `SELECT COUNT(*) FROM media_files WHERE id NOT IN (`+numbers(selected)+`)
				AND relative_path COLLATE NOCASE<>?
				AND lower(relative_path) IN (lower(?)||'.mkv',lower(?)||'.mp4',lower(?)||'.m4v',lower(?)||'.mov',lower(?)||'.avi',lower(?)||'.webm',lower(?)||'.ts',lower(?)||'.m2ts')`, video, stem, stem, stem, stem, stem, stem, stem, stem).Scan(&shared); err != nil {
				return nil, err
			}
		}
		if shared > 0 || path.Dir(name) != path.Dir(video) || !isSubtitleExtension(name) {
			continue
		}
		asset, err := inspect(root, name)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		asset.SHA, err = assetDigest(ctx, root, name)
		if err != nil {
			return nil, err
		}
		out = append(out, asset)
	}
	return out, nil
}

func isSubtitleExtension(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".srt", ".vtt", ".ass", ".ssa", ".sub", ".idx", ".sup", ".smi", ".ttml":
		return true
	}
	return false
}
