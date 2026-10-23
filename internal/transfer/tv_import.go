package transfer

import (
	"context"
	"database/sql"
	"fmt"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"
)

func cleanTVName(raw string, limit int) string {
	name := strings.Trim(strings.Map(func(c rune) rune {
		if unicode.IsControl(c) || strings.ContainsRune(`<>:"/\|?*`, c) {
			return '_'
		}
		return c
	}, raw), " .")
	for len(name) > limit {
		_, n := utf8.DecodeLastRuneInString(name)
		name = name[:len(name)-n]
	}
	name = strings.Trim(name, " .")
	if name == "" {
		name = "Episode"
	}
	return name
}
func (s *Service) tvDestination(ctx context.Context, j Job, source string) (string, error) {
	if j.fileIndex == nil || len(j.fileTargets) == 0 || len(j.fileTargets) > 20 {
		return "", mappingError()
	}
	var title, folder, naming string
	var year *int
	var tmdbID int64
	var season int
	if e := s.db.QueryRowContext(ctx, `SELECT series_title,series_year,series_tmdb_id,season_number,series_folder,naming FROM download_tv WHERE download_id=?`, j.ID).Scan(&title, &year, &tmdbID, &season, &folder, &naming); e != nil {
		return "", e
	}
	for n, t := range j.fileTargets {
		if t.Season != season || (n > 0 && t.Number != j.fileTargets[n-1].Number+1) {
			return "", mappingError()
		}
	}
	label := fmt.Sprintf("S%02dE%02d", season, j.fileTargets[0].Number)
	if len(j.fileTargets) > 1 {
		label += fmt.Sprintf("-E%02d", j.fileTargets[len(j.fileTargets)-1].Number)
	}
	base := cleanTVName(title, 80) + " - " + label
	if naming == "with-title" {
		titles := []string{}
		for _, t := range j.fileTargets {
			titles = append(titles, t.Title)
		}
		base += " - " + cleanTVName(strings.Join(titles, " + "), 96)
	}
	showFolder := movieName(cleanTVName(title, 100), year, tmdbID)
	dest := path.Join(folder, showFolder, fmt.Sprintf("Season %02d", season), base+strings.ToLower(path.Ext(source)))
	_, e := safeRelative(dest)
	return dest, e
}
func (s *Service) registerImport(ctx context.Context, tx *sql.Tx, j Job, p importPlan) error {
	var movieID, seriesID any = j.MovieID, nil
	if j.Kind == "series" {
		movieID = nil
		seriesID = j.MovieID
		if len(j.fileTargets) == 0 {
			return mappingError()
		}
		for _, t := range j.fileTargets {
			if e := validateTVTarget(ctx, tx, j.MovieID, t); e != nil {
				return e
			}
			var count int
			if e := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM download_mappings m JOIN episode_downloads d ON d.episode_id=m.episode_id AND d.download_id=m.download_id WHERE m.download_id=? AND m.file_index=? AND m.episode_id=?`, j.ID, j.importIndex(), t.EpisodeID).Scan(&count); e != nil {
				return e
			}
			if count != 1 {
				return problem("episode_busy", "The episode reservation or confirmed mapping changed. The published file was retained for review.")
			}
		}
	}
	if _, e := tx.ExecContext(ctx, `INSERT INTO media_files(movie_id,series_id,download_id,file_index,relative_path,size_bytes,sha256,probe_json,imported_at) VALUES(?,?,?,?,?,?,?,?,unixepoch()) ON CONFLICT(download_id,file_index) DO NOTHING`, movieID, seriesID, j.ID, j.importIndex(), p.Destination, p.Size, p.Hash, p.Probe); e != nil {
		return e
	}
	if j.Kind == "series" {
		var fileID int64
		if e := tx.QueryRowContext(ctx, `SELECT id FROM media_files WHERE download_id=? AND file_index=? AND relative_path=? AND sha256=?`, j.ID, j.importIndex(), p.Destination, p.Hash).Scan(&fileID); e != nil {
			return e
		}
		for _, t := range j.fileTargets {
			if _, e := tx.ExecContext(ctx, `INSERT INTO episode_files(episode_id,media_file_id) VALUES(?,?) ON CONFLICT(episode_id) DO NOTHING`, t.EpisodeID, fileID); e != nil {
				return e
			}
			var existing int64
			if e := tx.QueryRowContext(ctx, `SELECT media_file_id FROM episode_files WHERE episode_id=?`, t.EpisodeID).Scan(&existing); e != nil {
				return e
			}
			if existing != fileID {
				return problem("episode_busy", "An episode already has a different imported file. It was not replaced.")
			}
		}
	}
	return nil
}
