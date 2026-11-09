// Package duplicates provides a read-only duplicate review for registered
// movie and TV episode files.
//
// Review groups files by movie identity (media_files.movie_id) or episode
// identity (episode_files.episode_id). It never deletes, moves, renames or
// otherwise modifies files or library rows; every query is a SELECT.
//
// A group is only labelled probable_duplicate when every file in it has a
// non-empty SHA-256 and exactly matches the first file's size, SHA-256 and
// inspected resolution. Anything else is needs_attention and names the first
// failed check so an owner can review it.
//
// Current schema note: episode_files uses episode_id as its primary key, so a
// registered episode normally has at most one media file. Episode groups
// therefore usually stay empty; duplicate episode variants are reviewed before
// registration in the existing-library scan flow. The query keeps the episode
// grouping so a future schema that permits more than one claim still works.
package duplicates

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
)

const (
	// DefaultLimit is used when Review receives a non-positive limit.
	DefaultLimit = 100
	// MaxLimit bounds a single review request.
	MaxLimit = 500

	// StatusProbableDuplicate means every file matched on size, SHA-256 and
	// inspected resolution.
	StatusProbableDuplicate = "probable_duplicate"
	// StatusNeedsAttention means at least one conservative check failed.
	StatusNeedsAttention = "needs_attention"
)

// File is one registered file in a review group.
type File struct {
	ID           int64  `json:"id"`
	RelativePath string `json:"relative_path"`
	SizeBytes    int64  `json:"size_bytes"`
	SHA256       string `json:"sha256"`
	Resolution   int    `json:"resolution"`
}

// Group is one movie or episode identity with more than one registered file.
type Group struct {
	Kind         string `json:"kind"`
	MediaID      int64  `json:"media_id"`
	EpisodeID    *int64 `json:"episode_id,omitempty"`
	Title        string `json:"title"`
	EpisodeTitle string `json:"episode_title,omitempty"`
	Season       *int   `json:"season,omitempty"`
	Episode      *int   `json:"episode,omitempty"`
	FileCount    int    `json:"file_count"`
	Files        []File `json:"files"`
	Status       string `json:"status"`
	Reason       string `json:"reason"`
}

// Report is a bounded page of duplicate review groups.
type Report struct {
	Groups    []Group `json:"groups"`
	Total     int     `json:"total"`
	Limit     int     `json:"limit"`
	Truncated bool    `json:"truncated"`
}

// candidateCTE lists every identity that has more than one distinct registered
// file. Episode identity is keyed by episode ID, so shared range files that
// deliberately serve several episodes are not merged into one candidate.
const candidateCTE = `WITH candidates AS (
 SELECT 'movie' AS kind,m.id AS media_id,NULL AS episode_id,m.title AS title,
   NULL AS season,NULL AS episode,'' AS episode_title
 FROM media_items m
 WHERE m.kind='movie'
   AND (SELECT COUNT(*) FROM media_files f WHERE f.movie_id=m.id)>1
 UNION ALL
 SELECT 'episode',m.id,e.id,m.title,s.number,e.number,e.title
 FROM episodes e
 JOIN seasons s ON s.id=e.season_id
 JOIN media_items m ON m.id=s.series_id
 WHERE (SELECT COUNT(DISTINCT ef.media_file_id)
        FROM episode_files ef
        JOIN media_files f ON f.id=ef.media_file_id
        WHERE ef.episode_id=e.id)>1
)`

// Review returns up to limit movie and episode identity groups whose identity
// has more than one registered file. A non-positive limit uses DefaultLimit;
// larger values are clamped to MaxLimit. The report's Total counts every
// candidate identity, not just the returned page.
func Review(ctx context.Context, db *sql.DB, limit int) (Report, error) {
	if err := ctx.Err(); err != nil {
		return Report{}, err
	}
	if db == nil {
		return Report{}, fmt.Errorf("duplicates: nil database")
	}
	limit = normalizeLimit(limit)
	out := Report{Groups: []Group{}, Limit: limit}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return Report{}, err
	}
	// Read-only usage: the transaction is only a consistent snapshot.
	defer func() { _ = tx.Rollback() }()

	if err = tx.QueryRowContext(ctx, candidateCTE+` SELECT COUNT(*) FROM candidates`).Scan(&out.Total); err != nil {
		return Report{}, err
	}
	rows, err := tx.QueryContext(ctx, candidateCTE+` SELECT kind,media_id,episode_id,title,season,episode,episode_title
 FROM candidates
 ORDER BY title COLLATE NOCASE,kind,media_id,COALESCE(episode_id,0)
 LIMIT ?`, limit)
	if err != nil {
		return Report{}, err
	}
	for rows.Next() {
		var (
			g                          Group
			episodeID, season, episode sql.NullInt64
		)
		if err = rows.Scan(&g.Kind, &g.MediaID, &episodeID, &g.Title, &season, &episode, &g.EpisodeTitle); err != nil {
			rows.Close()
			return Report{}, err
		}
		if episodeID.Valid {
			v := episodeID.Int64
			g.EpisodeID = &v
		}
		if season.Valid {
			v := int(season.Int64)
			g.Season = &v
		}
		if episode.Valid {
			v := int(episode.Int64)
			g.Episode = &v
		}
		out.Groups = append(out.Groups, g)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return Report{}, err
	}
	if err = attachFiles(ctx, tx, out.Groups); err != nil {
		return Report{}, err
	}
	for index := range out.Groups {
		g := &out.Groups[index]
		sort.Slice(g.Files, func(i, j int) bool {
			if g.Files[i].RelativePath != g.Files[j].RelativePath {
				return g.Files[i].RelativePath < g.Files[j].RelativePath
			}
			return g.Files[i].ID < g.Files[j].ID
		})
		g.FileCount = len(g.Files)
		g.Status, g.Reason = classify(g.Files)
	}
	out.Truncated = out.Total > len(out.Groups)
	return out, nil
}

func normalizeLimit(limit int) int {
	if limit <= 0 {
		return DefaultLimit
	}
	if limit > MaxLimit {
		return MaxLimit
	}
	return limit
}

// attachFiles loads every file for the selected identities in bounded chunks,
// keeping placeholders well below SQLite's parameter limit.
func attachFiles(ctx context.Context, tx *sql.Tx, groups []Group) error {
	const chunk = 400
	movieIDs := make([]int64, 0, len(groups))
	episodeIDs := make([]int64, 0, len(groups))
	for _, g := range groups {
		if g.Kind == "movie" {
			movieIDs = append(movieIDs, g.MediaID)
		} else if g.EpisodeID != nil {
			episodeIDs = append(episodeIDs, *g.EpisodeID)
		}
	}
	byMovie := make(map[int64][]File, len(movieIDs))
	byEpisode := make(map[int64][]File, len(episodeIDs))
	for start := 0; start < len(movieIDs); start += chunk {
		end := min(start+chunk, len(movieIDs))
		query := `SELECT movie_id,id,relative_path,size_bytes,sha256,video_resolution
 FROM media_files WHERE movie_id IN (` + placeholders(end-start) + `)
 ORDER BY movie_id,relative_path COLLATE NOCASE,id`
		rows, err := tx.QueryContext(ctx, query, int64Args(movieIDs[start:end])...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var movieID int64
			var f File
			if err = rows.Scan(&movieID, &f.ID, &f.RelativePath, &f.SizeBytes, &f.SHA256, &f.Resolution); err != nil {
				rows.Close()
				return err
			}
			byMovie[movieID] = append(byMovie[movieID], f)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	for start := 0; start < len(episodeIDs); start += chunk {
		end := min(start+chunk, len(episodeIDs))
		query := `SELECT ef.episode_id,f.id,f.relative_path,f.size_bytes,f.sha256,f.video_resolution
 FROM episode_files ef
 JOIN media_files f ON f.id=ef.media_file_id
 WHERE ef.episode_id IN (` + placeholders(end-start) + `)
 ORDER BY ef.episode_id,f.relative_path COLLATE NOCASE,f.id`
		rows, err := tx.QueryContext(ctx, query, int64Args(episodeIDs[start:end])...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var episodeID int64
			var f File
			if err = rows.Scan(&episodeID, &f.ID, &f.RelativePath, &f.SizeBytes, &f.SHA256, &f.Resolution); err != nil {
				rows.Close()
				return err
			}
			byEpisode[episodeID] = append(byEpisode[episodeID], f)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	for index := range groups {
		g := &groups[index]
		if g.Kind == "movie" {
			g.Files = byMovie[g.MediaID]
		} else if g.EpisodeID != nil {
			g.Files = byEpisode[*g.EpisodeID]
		}
	}
	return nil
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	buf := make([]byte, 0, n*2-1)
	for i := 0; i < n; i++ {
		if i > 0 {
			buf = append(buf, ',')
		}
		buf = append(buf, '?')
	}
	return string(buf)
}

func int64Args(values []int64) []any {
	args := make([]any, len(values))
	for i, v := range values {
		args[i] = v
	}
	return args
}

// classify applies conservative checks in a fixed order and reports the first
// failed one. Files are already ordered by path, so reasons are deterministic.
func classify(files []File) (string, string) {
	if len(files) < 2 {
		return StatusNeedsAttention, "fewer than two registered files"
	}
	for _, f := range files {
		if f.SHA256 == "" {
			return StatusNeedsAttention, fmt.Sprintf("file %d has no SHA-256 checksum", f.ID)
		}
	}
	first := files[0]
	for _, f := range files[1:] {
		if f.SizeBytes != first.SizeBytes {
			return StatusNeedsAttention, fmt.Sprintf("file %d size %d differs from file %d size %d", f.ID, f.SizeBytes, first.ID, first.SizeBytes)
		}
	}
	for _, f := range files[1:] {
		if f.SHA256 != first.SHA256 {
			return StatusNeedsAttention, fmt.Sprintf("file %d SHA-256 differs from file %d despite matching size", f.ID, first.ID)
		}
	}
	for _, f := range files[1:] {
		if f.Resolution != first.Resolution {
			return StatusNeedsAttention, fmt.Sprintf("file %d resolution %dp differs from file %d resolution %dp", f.ID, f.Resolution, first.ID, first.Resolution)
		}
	}
	return StatusProbableDuplicate, fmt.Sprintf("%d files have identical size, SHA-256 and resolution.", len(files))
}
