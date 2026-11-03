package organization

import (
	"context"
	"database/sql"
	"encoding/json"
	"path"
	"strings"
	"time"
)

type AcquiredSubtitle struct {
	Language string    `json:"language"`
	File     Companion `json:"file"`
}

// ReserveAcquisition is called inside the download reservation transaction.
// Replacement is always manual, bound to an explicit registered file version.
func ReserveAcquisition(ctx context.Context, tx *sql.Tx, kind, job string, fileID, mediaID int64, episodeIDs []int64) error {
	var parent int64
	var sha, mediaKind string
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(f.movie_id,f.series_id),f.sha256,m.kind FROM media_files f JOIN media_items m ON m.id=COALESCE(f.movie_id,f.series_id) WHERE f.id=?`, fileID).Scan(&parent, &sha, &mediaKind); err != nil {
		return err
	}
	if parent != mediaID {
		return problem("invalid_selection", "Choose a registered file belonging to this title.")
	}
	rows, err := tx.QueryContext(ctx, `SELECT episode_id FROM episode_files WHERE media_file_id=? ORDER BY episode_id`, fileID)
	if err != nil {
		return err
	}
	actual := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		actual[id] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(actual) != len(episodeIDs) {
		return problem("invalid_selection", "Replacement must cover exactly the episodes in the current shared file.")
	}
	for _, id := range episodeIDs {
		if !actual[id] {
			return problem("invalid_selection", "Replacement episode mappings differ from the registered file.")
		}
	}
	if mediaKind == "movie" && len(episodeIDs) != 0 {
		return problem("invalid_selection", "Movie replacement cannot target episodes.")
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO acquisition_replacements(source_kind,job_id,media_file_id,target_sha) VALUES(?,?,?,?)`, kind, job, fileID, sha)
	return err
}

func (s *Service) PreviewAcquisition(ctx context.Context, kind, job string) (Plan, error) {
	unlock := ReadMedia()
	defer unlock()
	if err := EnsureMediaAvailable(ctx, s.db); err != nil {
		return Plan{}, err
	}
	root, resolved, err := s.openRoot("")
	if err != nil {
		return Plan{}, err
	}
	defer root.Close()
	var pendingFile int64
	if err = s.db.QueryRowContext(ctx, `SELECT media_file_id FROM acquisition_replacements WHERE source_kind=? AND job_id=?`, kind, job).Scan(&pendingFile); err != nil {
		return Plan{}, err
	}
	if err = s.prepareChecksums(ctx, root, []int64{pendingFile}, 0); err != nil {
		return Plan{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Plan{}, err
	}
	defer tx.Rollback()
	var fileID, size int64
	var targetSHA, source, sha, probe, savedPlan string
	var index int
	if err = tx.QueryRowContext(ctx, `SELECT media_file_id,target_sha,source_path,size_bytes,sha256,probe_json,file_index,plan_id FROM acquisition_replacements WHERE source_kind=? AND job_id=?`, kind, job).Scan(&fileID, &targetSHA, &source, &size, &sha, &probe, &index, &savedPlan); err != nil {
		return Plan{}, err
	}
	if savedPlan != "" {
		return readPlan(ctx, tx, savedPlan)
	}
	before, mediaID, err := replacementMetadata(ctx, tx, fileID)
	if err != nil {
		return Plan{}, err
	}
	if before.SHA != targetSHA || source == "" || size < 1 || len(sha) != 64 {
		return Plan{}, problem("revision_conflict", "The target changed or the acquisition is incomplete. Retained acquisition files were left untouched.")
	}
	if err = acquisitionReady(ctx, tx, kind, job); err != nil {
		return Plan{}, err
	}
	old, err := inspect(root, before.Path)
	if err != nil {
		return Plan{}, err
	}
	old.SHA = before.SHA
	incoming, err := inspect(root, source)
	if err != nil {
		return Plan{}, err
	}
	incoming.SHA = sha
	if incoming.Size != size || old.Size != before.Size {
		return Plan{}, problem("source_changed", "A replacement file changed.")
	}
	eps, err := s.episodes(ctx, tx, fileID)
	if err != nil {
		return Plan{}, err
	}
	yt, err := youtubeVersion(ctx, tx, fileID)
	if err != nil {
		return Plan{}, err
	}
	var title string
	if err = tx.QueryRowContext(ctx, `SELECT title FROM media_items WHERE id=?`, mediaID).Scan(&title); err != nil {
		return Plan{}, err
	}
	now := time.Now().Unix()
	pid := id()
	after := ReplacementMetadata{Path: strings.TrimSuffix(before.Path, path.Ext(before.Path)) + strings.ToLower(path.Ext(source)), Size: size, SHA: sha, Probe: probe, FileIndex: index, ImportedAt: now, Origin: "existing", SourceName: path.Base(source)}
	r := &Replacement{FileID: fileID, MediaID: mediaID, Title: title, Before: before, After: after, BeforeYouTube: yt, Original: old, Incoming: incoming, Backup: path.Join(".reelscarey-versions", ".reelscarey-version-"+pid), Episodes: eps, AcquisitionKind: kind, AcquisitionJob: job}
	if kind == "download" {
		r.After.DownloadID = &job
		r.After.Origin = "download"
		if err = tx.QueryRowContext(ctx, `SELECT title FROM downloads WHERE id=?`, job).Scan(&r.After.SourceName); err != nil {
			return Plan{}, err
		}
	}
	if kind == "youtube" {
		var vid, videoTitle string
		if err = tx.QueryRowContext(ctx, `SELECT video_id,video_title FROM youtube_jobs WHERE id=?`, job).Scan(&vid, &videoTitle); err != nil {
			return Plan{}, err
		}
		r.After.Origin = "existing"
		r.After.SourceName = videoTitle
		r.AfterYouTube = &YouTubeVersion{JobID: job, VideoID: vid, Title: videoTitle, Path: strings.TrimSuffix(before.Path, path.Ext(before.Path)) + strings.ToLower(path.Ext(source)), Size: size, SHA: sha, Probe: probe, ImportedAt: now}
		rows, e := tx.QueryContext(ctx, `SELECT language,destination_path,size_bytes,sha256 FROM youtube_subtitle_imports WHERE job_id=? AND phase='committed' ORDER BY language`, job)
		if e != nil {
			return Plan{}, e
		}
		type sub struct {
			lang, name, sha string
			size            int64
		}
		subs := []sub{}
		for rows.Next() {
			var v sub
			if e = rows.Scan(&v.lang, &v.name, &v.size, &v.sha); e != nil {
				rows.Close()
				return Plan{}, e
			}
			subs = append(subs, v)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return Plan{}, e
		}
		for _, v := range subs {
			c, e := inspect(root, v.name)
			if e != nil {
				return Plan{}, e
			}
			if c.Size != v.size {
				return Plan{}, problem("source_changed", "An acquired subtitle changed.")
			}
			c.SHA = v.sha
			c.Destination = strings.TrimSuffix(before.Path, path.Ext(before.Path)) + ".youtube-" + job + "." + v.lang + ".vtt"
			if exists, e := existsFolded(root, c.Destination); e != nil {
				return Plan{}, e
			} else if exists {
				return Plan{}, problem("destination_exists", "An acquired subtitle destination is occupied.")
			}
			r.AcquisitionSubtitles = append(r.AcquisitionSubtitles, AcquiredSubtitle{v.lang, c})
		}
	}
	p := Plan{ID: pid, State: "review", Root: resolved, Phase: "planned", Created: now, Updated: now, Files: []PlannedFile{}, Replacement: r}
	if err = s.validateReplacement(ctx, root, tx, p); err != nil {
		return Plan{}, err
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return Plan{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO organization_plans(id,state,snapshot,created_at,updated_at) VALUES(?,'review',?,?,?)`, pid, string(raw), now, now); err != nil {
		return Plan{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE acquisition_replacements SET plan_id=? WHERE source_kind=? AND job_id=?`, pid, kind, job); err != nil {
		return Plan{}, err
	}
	return p, tx.Commit()
}

func validateAcquisitionTarget(ctx context.Context, q query, r *Replacement) error {
	var fileID, size int64
	var sha, source, targetSHA string
	var index int
	if err := q.QueryRowContext(ctx, `SELECT media_file_id,target_sha,source_path,size_bytes,sha256,file_index FROM acquisition_replacements WHERE source_kind=? AND job_id=?`, r.AcquisitionKind, r.AcquisitionJob).Scan(&fileID, &targetSHA, &source, &size, &sha, &index); err != nil {
		return err
	}
	if fileID != r.FileID || targetSHA != r.Before.SHA || source != r.Incoming.Source || size != r.Incoming.Size || sha != r.Incoming.SHA {
		return problem("revision_conflict", "The reviewed acquisition or target version changed.")
	}
	ids := map[int64]bool{}
	for _, ep := range r.Episodes {
		ids[ep.ID] = true
	}
	if r.AcquisitionKind == "download" {
		var media int64
		var kind string
		if err := q.QueryRowContext(ctx, `SELECT movie_id,kind FROM downloads WHERE id=?`, r.AcquisitionJob).Scan(&media, &kind); err != nil {
			return err
		}
		if media != r.MediaID {
			return problem("revision_conflict", "The download belongs to a different title.")
		}
		if kind == "series" {
			rows, err := q.QueryContext(ctx, `SELECT episode_id FROM download_mappings WHERE download_id=? AND file_index=?`, r.AcquisitionJob, index)
			if err != nil {
				return err
			}
			n := 0
			valid := true
			for rows.Next() {
				var id int64
				if err = rows.Scan(&id); err != nil {
					rows.Close()
					return err
				}
				n++
				if !ids[id] {
					valid = false
				}
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return err
			}
			if !valid || n != len(ids) {
				return problem("revision_conflict", "The acquired episode mapping differs from the registered version.")
			}
		} else if len(ids) != 0 {
			return problem("revision_conflict", "The download kind changed.")
		}
	} else {
		var media, episode int64
		if err := q.QueryRowContext(ctx, `SELECT media_id,COALESCE(episode_id,0) FROM youtube_jobs WHERE id=?`, r.AcquisitionJob).Scan(&media, &episode); err != nil {
			return err
		}
		if media != r.MediaID || episode == 0 && len(ids) != 0 || episode != 0 && (len(ids) != 1 || !ids[episode]) {
			return problem("revision_conflict", "The YouTube acquisition target changed.")
		}
	}
	return nil
}
func acquisitionReady(ctx context.Context, q query, kind, job string) error {
	var n int
	var err error
	if kind == "download" {
		err = q.QueryRowContext(ctx, `SELECT COUNT(*) FROM downloads WHERE id=? AND state='blocked' AND error_code='replacement_review' AND lease_until<unixepoch()`, job).Scan(&n)
	} else if kind == "youtube" {
		err = q.QueryRowContext(ctx, `SELECT COUNT(*) FROM youtube_jobs WHERE id=? AND state='awaiting_review' AND lease_until<unixepoch()`, job).Scan(&n)
	} else {
		return problem("invalid_selection", "Choose a downloaded replacement source.")
	}
	if err != nil {
		return err
	}
	if n != 1 {
		return problem("media_busy", "The acquisition is not ready for replacement review.")
	}
	return nil
}

func completeAcquisition(ctx context.Context, tx *sql.Tx, r *Replacement) error {
	if r.AcquisitionKind == "" {
		return nil
	}
	state := "completed"
	if r.Recovering {
		state = "cancelled"
	}
	if r.AcquisitionKind == "download" {
		if _, err := tx.ExecContext(ctx, `UPDATE downloads SET state=?,error_code='',error_message='',progress=1,updated_at=unixepoch() WHERE id=?`, state, r.AcquisitionJob); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE import_operations SET phase='committed' WHERE download_id=?`, r.AcquisitionJob); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM episode_downloads WHERE download_id=?`, r.AcquisitionJob); err != nil {
			return err
		}
	} else {
		if _, err := tx.ExecContext(ctx, `UPDATE youtube_jobs SET state=?,message='Reviewed replacement finished.',updated_at=unixepoch() WHERE id=?`, state, r.AcquisitionJob); err != nil {
			return err
		}
	}
	_, err := tx.ExecContext(ctx, `UPDATE media_items SET status='available' WHERE id=?`, r.MediaID)
	return err
}
