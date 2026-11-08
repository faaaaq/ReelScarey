package transfer

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/faaaaq/ReelScarey/internal/quality"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var absoluteToken = regexp.MustCompile(`(?i)(?:^| - )([0-9]{1,6})(?:-([0-9]{1,6}))?(?:v[1-9][0-9]*)?`)

// Absolute filename labels are hints only, and only refer to explicitly saved
// aliases snapshotted with this download. No season order is inferred.
func suggestedAbsoluteEpisodes(name string, targets []TVTarget) []int64 {
	base := path.Base(strings.ReplaceAll(name, "\\", "/"))
	matches := absoluteToken.FindAllStringSubmatchIndex(base, -1)
	if len(matches) != 1 {
		return nil
	}
	m := matches[0]
	if m[1] < len(base) && !strings.ContainsRune(" .[", rune(base[m[1]])) {
		return nil
	}
	start, _ := strconv.Atoi(base[m[2]:m[3]])
	end := start
	if m[4] >= 0 {
		end, _ = strconv.Atoi(base[m[4]:m[5]])
	}
	if start < 1 || end < start || end-start >= 20 {
		return nil
	}
	out := []int64{}
	season, previous := -1, -1
	for absolute := start; absolute <= end; absolute++ {
		var match *TVTarget
		for n := range targets {
			t := &targets[n]
			if t.AbsoluteNumber != nil && *t.AbsoluteNumber == absolute {
				if match != nil {
					return nil
				}
				match = t
			}
		}
		if match == nil || (season != -1 && (match.Season != season || match.Number != previous+1)) {
			return nil
		}
		season = match.Season
		previous = match.Number
		out = append(out, match.EpisodeID)
	}
	return out
}

// Suggestions never authorize an import. Every mapping requires owner review.
func suggestedEpisodes(name string, targets []TVTarget) []int64 {
	base := path.Base(strings.ReplaceAll(name, "\\", "/"))
	season, numbers, _, _, ok := quality.EpisodeSpan(base)
	if !ok {
		if quality.HasEpisodeLabel(base) {
			return nil
		}
		return suggestedAbsoluteEpisodes(name, targets)
	}
	out := []int64{}
	for i, n := range numbers {
		if i > 0 && n != numbers[i-1]+1 {
			return nil
		}
		found := false
		for _, t := range targets {
			if t.Season == season && t.Number == n {
				out = append(out, t.EpisodeID)
				found = true
				break
			}
		}
		if !found {
			return nil
		}
	}
	return out
}
func (s *Service) prepareTVMapping(ctx context.Context, j Job, choices []FileChoice, savePath, remote string) error {
	if len(choices) == 0 {
		return problem("no_episode_file", "The completed download has no complete supported episode videos. Archives, samples, trailers and unselected files are excluded.")
	}
	if len(choices) > 500 {
		return problem("too_many_files", "This download has more than 500 video files. Use a smaller release.")
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var confirmed int
	if e = tx.QueryRowContext(ctx, `SELECT mapping_confirmed FROM download_tv WHERE download_id=?`, j.ID).Scan(&confirmed); e != nil {
		return e
	}
	if confirmed != 0 {
		return problem("mapping_locked", "This download's episode mapping is already confirmed.")
	}
	if _, e = tx.ExecContext(ctx, `DELETE FROM download_tv_files WHERE download_id=?`, j.ID); e != nil {
		return e
	}
	for n := range choices {
		source, e := mapJobSource(j, savePath, choices[n].Name, remote)
		if e != nil {
			return e
		}
		if !sourceInJob(j, source) {
			return problem("path_mapping", "Restore this download to its dedicated ReelScarey download folder before importing.")
		}
		if j.AnimeOrder != nil {
			choices[n].Suggestions = suggestedOrderEpisodes(choices[n].Name, j.Targets, j.AnimeOrder, j.MovieTitle)
		} else {
			choices[n].Suggestions = suggestedEpisodes(choices[n].Name, j.Targets)
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO download_tv_files(download_id,file_index,name,source_path,size_bytes) VALUES(?,?,?,?,?)`, j.ID, choices[n].Index, choices[n].Name, source, choices[n].Size); e != nil {
			return e
		}
	}
	// A download imports without review when every reserved episode has exactly
	// one completed file whose parsed label names it, and no file is ambiguous.
	// This covers single episodes and multi-file season packs. Extras that do
	// not name a reserved episode are ignored. Replacements still require the
	// explicit review. The same target validation, journaled import and
	// ffprobe verification run either way.
	mapping := map[int64]int{}
	auto := len(j.Targets) > 0
	for n := range choices {
		for _, episodeID := range choices[n].Suggestions {
			if _, exists := mapping[episodeID]; exists {
				auto = false
			}
			mapping[episodeID] = choices[n].Index
		}
	}
	for _, target := range j.Targets {
		if _, ok := mapping[target.EpisodeID]; !ok {
			auto = false
		}
	}
	var replacement int
	if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM acquisition_replacements WHERE source_kind='download' AND job_id=?`, j.ID).Scan(&replacement); e != nil {
		return e
	}
	auto = auto && replacement == 0 && (j.AnimeOrder == nil || len(j.Targets) == 1)
	b, e := json.Marshal(choices)
	if e != nil {
		return e
	}
	result, e := tx.ExecContext(ctx, `UPDATE downloads SET choices=?,updated_at=unixepoch() WHERE id=? AND lease_owner=? AND lease_until>=unixepoch()`, string(b), j.ID, j.owner)
	if e != nil {
		return e
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return ErrLease
	}
	if auto {
		for _, target := range j.Targets {
			fileIndex := mapping[target.EpisodeID]
			if e = validateTVTarget(ctx, tx, j.MovieID, target); e != nil {
				return e
			}
			if _, e = tx.ExecContext(ctx, `INSERT INTO download_mappings(download_id,episode_id,file_index) VALUES(?,?,?)`, j.ID, target.EpisodeID, fileIndex); e != nil {
				return e
			}
		}
		if _, e = tx.ExecContext(ctx, `UPDATE download_tv SET mapping_confirmed=1 WHERE download_id=?`, j.ID); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE downloads SET state='importing',choices='[]',error_code='',error_message='',next_run=0,attempts=0,updated_at=unixepoch() WHERE id=? AND lease_owner=?`, j.ID, j.owner); e != nil {
			return e
		}
		message := "imported automatically: the release name matched exactly one episode"
		if len(j.Targets) > 1 || len(mapping) > 1 {
			message = "imported automatically: every pack file matched a reserved episode"
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'download',?,?,?)`, j.ID, j.Title, message); e != nil {
			return e
		}
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	if auto {
		return nil
	}
	return problem("map_episodes", "Review which completed file belongs to each episode, then confirm the import. Filename suggestions must be checked.")
}

type TVMapping struct {
	FileIndex  int     `json:"file_index"`
	EpisodeIDs []int64 `json:"episode_ids"`
}
type TVMappingUpdate struct {
	Files          []TVMapping `json:"files"`
	SkipEpisodeIDs []int64     `json:"skip_episode_ids"`
}

func mappingError() error {
	return problem("invalid_mapping", "Assign each target episode exactly once to a listed file or explicitly skip it. A shared file must contain at most 20 consecutive episodes from one season.")
}
func (s *Service) ConfirmTVMapping(ctx context.Context, id string, in TVMappingUpdate) error {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	j, e := s.Job(ctx, id)
	if e != nil {
		return e
	}
	if j.Kind != "series" || j.State != "blocked" || j.ErrorCode != "map_episodes" {
		return problem("download_busy", "This download is not waiting for episode mapping.")
	}
	if len(in.Files) == 0 || len(in.Files) > 100 || len(in.SkipEpisodeIDs) > 100 {
		return mappingError()
	}
	var replacement int
	if e = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM acquisition_replacements WHERE source_kind='download' AND job_id=?`, id).Scan(&replacement); e != nil {
		return e
	}
	if replacement > 0 && (len(in.Files) != 1 || len(in.SkipEpisodeIDs) != 0) {
		return problem("invalid_mapping", "Replacement must map one file to every episode in the registered version.")
	}
	targets := map[int64]TVTarget{}
	for _, t := range j.Targets {
		targets[t.EpisodeID] = t
	}
	seen := map[int64]bool{}
	indexes := map[int]bool{}
	for _, f := range in.Files {
		if f.FileIndex < 0 || indexes[f.FileIndex] || len(f.EpisodeIDs) == 0 || len(f.EpisodeIDs) > 20 {
			return mappingError()
		}
		indexes[f.FileIndex] = true
		nums := []int{}
		season := -1
		for _, id := range f.EpisodeIDs {
			t, ok := targets[id]
			if !ok || seen[id] || (season != -1 && t.Season != season) {
				return mappingError()
			}
			season = t.Season
			seen[id] = true
			nums = append(nums, t.Number)
		}
		sort.Ints(nums)
		for n := 1; n < len(nums); n++ {
			if nums[n] != nums[n-1]+1 {
				return mappingError()
			}
		}
	}
	for _, id := range in.SkipEpisodeIDs {
		if _, ok := targets[id]; !ok || seen[id] {
			return mappingError()
		}
		seen[id] = true
	}
	if len(seen) != len(targets) {
		return mappingError()
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var confirmed, count int
	if e = tx.QueryRowContext(ctx, `SELECT mapping_confirmed FROM download_tv WHERE download_id=?`, id).Scan(&confirmed); e != nil {
		return e
	}
	if confirmed != 0 {
		return problem("mapping_locked", "Episode mapping cannot change after confirmation.")
	}
	if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM import_operations WHERE download_id=?`, id).Scan(&count); e != nil {
		return e
	}
	if count > 0 {
		return problem("mapping_locked", "Import planning already started; its mapping cannot change.")
	}
	for _, t := range j.Targets {
		if e = validateTVTarget(ctx, tx, j.MovieID, t); e != nil {
			return e
		}
	}
	for _, f := range in.Files {
		if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM download_tv_files WHERE download_id=? AND file_index=?`, id, f.FileIndex).Scan(&count); e != nil {
			return e
		}
		if count != 1 {
			return mappingError()
		}
		for _, episodeID := range f.EpisodeIDs {
			if _, e = tx.ExecContext(ctx, `INSERT INTO download_mappings(download_id,episode_id,file_index) VALUES(?,?,?)`, id, episodeID, f.FileIndex); e != nil {
				return e
			}
		}
	}
	for _, episodeID := range in.SkipEpisodeIDs {
		if _, e = tx.ExecContext(ctx, `UPDATE download_targets SET skipped=1 WHERE download_id=? AND episode_id=?`, id, episodeID); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `DELETE FROM episode_downloads WHERE download_id=? AND episode_id=?`, id, episodeID); e != nil {
			return e
		}
	}
	result, e := tx.ExecContext(ctx, `UPDATE downloads SET state='importing',choices='[]',error_code='',error_message='',next_run=0,attempts=0,updated_at=unixepoch() WHERE id=? AND state='blocked' AND error_code='map_episodes' AND lease_until<unixepoch()`, id)
	if e != nil {
		return e
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return problem("download_busy", "This download is still finishing a step. Try again shortly.")
	}
	if _, e = tx.ExecContext(ctx, `UPDATE download_tv SET mapping_confirmed=1 WHERE download_id=?`, id); e != nil {
		return e
	}
	return tx.Commit()
}
func (j Job) importIndex() int {
	if j.fileIndex == nil {
		return -1
	}
	return *j.fileIndex
}
func (j Job) stageID() string {
	if j.fileIndex == nil {
		return j.ID
	}
	return fmt.Sprintf("%s-%d", j.ID, *j.fileIndex)
}
func (s *Service) processTVImports(ctx context.Context, j Job) error {
	var confirmed int
	if e := s.db.QueryRowContext(ctx, `SELECT mapping_confirmed FROM download_tv WHERE download_id=?`, j.ID).Scan(&confirmed); e != nil {
		return e
	}
	if confirmed != 1 {
		return problem("mapping_required", "Confirm the episode file mapping in Activity before importing.")
	}
	type file struct {
		index     int
		source    string
		size      int64
		committed bool
	}
	files := []file{}
	rows, e := s.db.QueryContext(ctx, `SELECT f.file_index,f.source_path,f.size_bytes,EXISTS(SELECT 1 FROM import_operations o WHERE o.download_id=f.download_id AND o.file_index=f.file_index AND o.phase='committed') FROM download_tv_files f WHERE f.download_id=? AND EXISTS(SELECT 1 FROM download_mappings m WHERE m.download_id=f.download_id AND m.file_index=f.file_index) ORDER BY f.file_index`, j.ID)
	if e != nil {
		return e
	}
	for rows.Next() {
		var f file
		if e = rows.Scan(&f.index, &f.source, &f.size, &f.committed); e != nil {
			rows.Close()
			return e
		}
		files = append(files, f)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	if len(files) == 0 {
		return mappingError()
	}
	for _, f := range files {
		if f.committed {
			continue
		}
		targets := []TVTarget{}
		rows, e := s.db.QueryContext(ctx, `SELECT t.episode_id,t.tmdb_id,t.season_number,t.episode_number,t.title,t.absolute_number FROM download_targets t JOIN download_mappings m ON m.download_id=t.download_id AND m.episode_id=t.episode_id WHERE m.download_id=? AND m.file_index=? ORDER BY t.season_number,t.episode_number`, j.ID, f.index)
		if e != nil {
			return e
		}
		for rows.Next() {
			var t TVTarget
			if e = rows.Scan(&t.EpisodeID, &t.TMDbID, &t.Season, &t.Number, &t.Title, &t.AbsoluteNumber); e != nil {
				rows.Close()
				return e
			}
			targets = append(targets, t)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		child := j
		child.fileIndex = &f.index
		child.fileTargets = targets
		if e = s.importer.Execute(ctx, child, f.source, f.size); e != nil {
			return e
		}
	}
	_ = s.cleanupExtract(ctx, j)
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	result, e := tx.ExecContext(ctx, `UPDATE downloads SET state='completed',progress=1,error_code='',error_message='',updated_at=unixepoch() WHERE id=? AND lease_owner=? AND lease_until>=unixepoch()`, j.ID, j.owner)
	if e != nil {
		return e
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return ErrLease
	}
	if e = recordDownloadCompleted(ctx, tx, j.ID, j.Title); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `DELETE FROM episode_downloads WHERE download_id=?`, j.ID); e != nil {
		return e
	}
	if e = refreshTVStatus(ctx, tx, j.MovieID); e != nil {
		return e
	}
	return tx.Commit()
}
