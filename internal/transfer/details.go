package transfer

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/faaaaq/ReelScarey/internal/library"
	"github.com/faaaaq/ReelScarey/internal/metadata"
	"github.com/faaaaq/ReelScarey/internal/organization"
	"github.com/faaaaq/ReelScarey/internal/quality"
	"path/filepath"
)

type LocalFile struct {
	VersionLabel string `json:"version_label,omitempty"`
	ID           int64  `json:"id"`
	Path         string `json:"path"`
	Directory    string `json:"directory"`
	Size         int64  `json:"size_bytes"`
	Imported     int64  `json:"imported_at"`
	DownloadID   string `json:"download_id"`
	Release      string `json:"release"`
	Probe        Probe  `json:"probe"`
	Episodes     string `json:"episodes"`
	Origin       string `json:"origin"`
}
type History struct {
	Protocol string  `json:"protocol"`
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	Indexer  string  `json:"indexer"`
	State    string  `json:"state"`
	Progress float64 `json:"progress"`
	Size     int64   `json:"size_bytes"`
	Created  int64   `json:"created_at"`
	Updated  int64   `json:"updated_at"`
	Error    string  `json:"error"`
}
type ItemDetails struct {
	Item            library.Item          `json:"item"`
	Information     *metadata.Information `json:"information"`
	MetadataUpdated int64                 `json:"metadata_updated_at"`
	MediaRoot       string                `json:"media_root"`
	LibraryRoot     string                `json:"library_root"`
	TotalSize       int64                 `json:"total_size_bytes"`
	FileCount       int                   `json:"file_count"`
	HistoryCount    int                   `json:"history_count"`
	Files           []LocalFile           `json:"files"`
	History         []History             `json:"history"`
	Page            int                   `json:"page"`
	// ProfileContext carries the schedule default and every saved quality
	// profile so the title's own page can review or change its assignment.
	ProfileContext *library.ProfileContext `json:"profile_context,omitempty"`
}

func (s *Service) Details(ctx context.Context, id int64, page int) (ItemDetails, error) {
	out := ItemDetails{Files: []LocalFile{}, History: []History{}, Page: page, MediaRoot: s.cfg.MediaDir}
	item, e := library.New(s.db).Item(ctx, id)
	if e != nil {
		return out, e
	}
	out.Item = item
	if e = s.assignmentProfiles(ctx, id, &out); e != nil {
		return out, e
	}
	assignment, e := organization.New(s.db, s.cfg).GetAssignment(ctx, id)
	if e != nil {
		return out, e
	}
	folder := organization.Directory(assignment.EffectiveLibrary, item.Kind)
	out.LibraryRoot = filepath.Join(s.cfg.MediaDir, folder)
	var raw string
	e = s.db.QueryRowContext(ctx, `SELECT payload,updated_at FROM media_enrichment WHERE media_id=?`, id).Scan(&raw, &out.MetadataUpdated)
	if e == nil {
		var info metadata.Information
		if e = json.Unmarshal([]byte(raw), &info); e != nil {
			return out, e
		}
		out.Information = &info
	} else if !errors.Is(e, sql.ErrNoRows) {
		return out, e
	}
	if e = s.db.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(size_bytes),0) FROM media_files WHERE movie_id=? OR series_id=?`, id, id).Scan(&out.FileCount, &out.TotalSize); e != nil {
		return out, e
	}
	rows, e := s.db.QueryContext(ctx, `SELECT f.id,f.relative_path,f.size_bytes,f.imported_at,COALESCE(f.download_id,''),COALESCE(d.title,f.source_name),f.probe_json,COALESCE((SELECT group_concat(label,', ') FROM (SELECT printf('S%02dE%02d',s.number,e.number) label FROM episode_files ef JOIN episodes e ON e.id=ef.episode_id JOIN seasons s ON s.id=e.season_id WHERE ef.media_file_id=f.id ORDER BY s.number,e.number)),''),CASE WHEN EXISTS(SELECT 1 FROM youtube_files yf WHERE yf.media_file_id=f.id) THEN 'youtube' ELSE f.origin END FROM media_files f LEFT JOIN downloads d ON d.id=f.download_id WHERE f.movie_id=? OR f.series_id=? ORDER BY f.relative_path LIMIT 50 OFFSET ?`, id, id, (page-1)*50)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		var f LocalFile
		var rel, probe string
		if e = rows.Scan(&f.ID, &rel, &f.Size, &f.Imported, &f.DownloadID, &f.Release, &probe, &f.Episodes, &f.Origin); e != nil {
			rows.Close()
			return out, e
		}
		safe, e := safeRelative(rel)
		if e != nil {
			rows.Close()
			return out, e
		}
		f.Path = filepath.Join(s.cfg.MediaDir, filepath.FromSlash(safe))
		f.Directory = filepath.Dir(f.Path)
		if e = json.Unmarshal([]byte(probe), &f.Probe); e != nil {
			rows.Close()
			return out, e
		}
		if item.Kind == "movie" {
			resolution := quality.VerifiedQuality(f.Probe.Width, f.Probe.Height)
			f.VersionLabel = "Resolution unknown"
			if resolution > 0 {
				f.VersionLabel = fmt.Sprintf("%dp", resolution)
			}
			if resolution == 2160 {
				f.VersionLabel = "4K (2160p)"
			}
		}
		out.Files = append(out.Files, f)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	if e = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM downloads WHERE movie_id=?`, id).Scan(&out.HistoryCount); e != nil {
		return out, e
	}
	rows, e = s.db.QueryContext(ctx, `SELECT id,title,indexer_name,state,progress,total_bytes,created_at,updated_at,error_message,protocol FROM downloads WHERE movie_id=? ORDER BY created_at DESC,id LIMIT 50 OFFSET ?`, id, (page-1)*50)
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var h History
		if e = rows.Scan(&h.ID, &h.Title, &h.Indexer, &h.State, &h.Progress, &h.Size, &h.Created, &h.Updated, &h.Error, &h.Protocol); e != nil {
			return out, e
		}
		out.History = append(out.History, h)
	}
	return out, rows.Err()
}

// assignmentProfiles attaches the title's effective quality profile, its
// assignment revision and the saved profile list. It reads a single snapshot
// so the reviewed assignment and the later write cannot disagree.
func (s *Service) assignmentProfiles(ctx context.Context, id int64, out *ItemDetails) error {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	profileContext, profiles, e := library.CatalogProfiles(ctx, tx)
	if e != nil {
		return e
	}
	assignment := &library.CatalogProfile{}
	effectiveID := profileContext.Default.ID
	var profileID sql.NullInt64
	var revision int
	e = tx.QueryRowContext(ctx, `SELECT profile_id,revision FROM item_automation WHERE media_id=?`, id).Scan(&profileID, &revision)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return e
	}
	if profileID.Valid {
		value := profileID.Int64
		assignment.ProfileID = &value
		effectiveID = value
	}
	effective, ok := profiles[effectiveID]
	if !ok {
		return errors.New("saved profile assignment is unavailable")
	}
	assignment.Effective = effective
	assignment.Revision = revision
	out.Item.QualityProfile = assignment
	out.ProfileContext = profileContext
	return tx.Commit()
}
