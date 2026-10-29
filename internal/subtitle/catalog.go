package subtitle

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/faaaaq/ReelScarey/internal/transfer"
)

type media struct {
	ID int64
	Target
	Path, SHA, Probe string
	Imported         int64
}

func (s *Service) media(ctx context.Context, id int64) (media, error) {
	var m media
	// Existing filenames are not verified original-release evidence. Automatic
	// matching for these files therefore requires provider hash evidence.
	e := s.db.QueryRowContext(ctx, `SELECT f.id,m.id,m.kind,m.tmdb_id,m.title,m.year,COALESCE(d.title,''),f.relative_path,f.size_bytes,f.sha256,f.probe_json,f.imported_at FROM media_files f JOIN media_items m ON m.id=COALESCE(f.movie_id,f.series_id) LEFT JOIN downloads d ON d.id=f.download_id WHERE f.id=?`, id).Scan(&m.ID, &m.MediaID, &m.Kind, &m.TMDbID, &m.Title, &m.Year, &m.Release, &m.Path, &m.Size, &m.SHA, &m.Probe, &m.Imported)
	if e != nil {
		return m, e
	}
	if m.Kind == "series" {
		rows, e := s.db.QueryContext(ctx, `SELECT e.tmdb_id,s.number,e.number FROM episode_files ef JOIN episodes e ON e.id=ef.episode_id JOIN seasons s ON s.id=e.season_id WHERE ef.media_file_id=? ORDER BY s.number,e.number LIMIT 21`, id)
		if e != nil {
			return m, e
		}
		for rows.Next() {
			var ep Episode
			if e = rows.Scan(&ep.TMDbID, &ep.Season, &ep.Number); e != nil {
				rows.Close()
				return m, e
			}
			m.Episodes = append(m.Episodes, ep)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return m, e
		}
		if len(m.Episodes) == 0 || len(m.Episodes) > 20 {
			return m, problem("invalid_media", "The registered file has unsupported episode associations.")
		}
	}
	return m, nil
}

type SavedTrack struct {
	Track
	Source  string `json:"source"`
	Path    string `json:"path"`
	State   string `json:"state"`
	Message string `json:"message"`
	Updated int64  `json:"updated_at"`
}
type FileView struct {
	ID           int64        `json:"id"`
	MediaID      int64        `json:"media_id"`
	Kind         string       `json:"kind"`
	Title        string       `json:"title"`
	Path         string       `json:"path"`
	Episodes     string       `json:"episodes"`
	Tracks       []SavedTrack `json:"tracks"`
	Job          *Job         `json:"job"`
	Candidates   []Candidate  `json:"candidates,omitempty"`
	WantedTracks []Track      `json:"wanted_tracks"`
}

func (s *Service) File(ctx context.Context, id int64, withCandidates bool) (FileView, error) {
	m, e := s.media(ctx, id)
	if e != nil {
		return FileView{}, e
	}
	v := FileView{ID: id, MediaID: m.MediaID, Kind: m.Kind, Title: m.Title, Path: m.Path, Tracks: []SavedTrack{}}
	prefs, e := s.effectivePreferences(ctx, m.MediaID)
	if e != nil {
		return v, e
	}
	v.WantedTracks = prefs.Tracks
	if len(m.Episodes) > 0 {
		first := m.Episodes[0]
		v.Episodes = fmt.Sprintf("S%02dE%02d", first.Season, first.Number)
		if len(m.Episodes) > 1 {
			v.Episodes += fmt.Sprintf("–E%02d", m.Episodes[len(m.Episodes)-1].Number)
		}
	}
	rows, e := s.db.QueryContext(ctx, `SELECT language,variant,source,relative_path,state,message,updated_at FROM subtitles WHERE media_file_id=? ORDER BY language,variant`, id)
	if e != nil {
		return v, e
	}
	for rows.Next() {
		var t SavedTrack
		if e = rows.Scan(&t.Language, &t.Variant, &t.Source, &t.Path, &t.State, &t.Message, &t.Updated); e != nil {
			rows.Close()
			return v, e
		}
		v.Tracks = append(v.Tracks, t)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return v, e
	}
	j, e := scanJob(s.db.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM subtitle_jobs WHERE media_file_id=?`, id))
	if e == nil {
		v.Job = &j
	} else if !errors.Is(e, sql.ErrNoRows) {
		return v, e
	}
	e = nil // A newly imported file is visible before its first subtitle job.
	if withCandidates {
		v.Candidates, e = s.candidates(ctx, id)
	}
	return v, e
}

type FilePage struct {
	Items []FileView `json:"items"`
	Page  int        `json:"page"`
	Total int        `json:"total"`
	Limit int        `json:"limit"`
}

func (s *Service) List(ctx context.Context, page int) (FilePage, error) {
	out := FilePage{Items: []FileView{}, Page: page, Limit: 24}
	if e := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM media_files`).Scan(&out.Total); e != nil {
		return out, e
	}
	rows, e := s.db.QueryContext(ctx, `SELECT id FROM media_files ORDER BY imported_at DESC,id DESC LIMIT 24 OFFSET ?`, (page-1)*24)
	if e != nil {
		return out, e
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return out, e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	for _, id := range ids {
		v, e := s.File(ctx, id, false)
		if e != nil {
			return out, e
		}
		out.Items = append(out.Items, v)
	}
	return out, nil
}
func (s *Service) candidates(ctx context.Context, id int64) ([]Candidate, error) {
	out := []Candidate{}
	rows, e := s.db.QueryContext(ctx, `SELECT payload FROM subtitle_candidates WHERE media_file_id=? AND expires_at>=? ORDER BY rowid LIMIT 50`, id, s.now().Unix())
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var b string
		var c Candidate
		if e = rows.Scan(&b); e != nil {
			return out, e
		}
		if e = json.Unmarshal([]byte(b), &c); e != nil {
			return out, e
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
func (s *Service) storeCandidates(ctx context.Context, m media, p Preferences, c Credentials, candidates []Candidate) ([]Candidate, error) {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `DELETE FROM subtitle_candidates WHERE (media_file_id=? OR expires_at<?) AND id NOT IN (SELECT selected_candidate FROM subtitle_jobs WHERE selected_candidate<>'')`, m.ID, s.now().Unix()); e != nil {
		return nil, e
	}
	for i := range candidates {
		v := &candidates[i]
		v.ID = transfer.ID()
		b, e := json.Marshal(v)
		if e != nil {
			return nil, e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO subtitle_candidates(id,media_file_id,payload,generation,revision,media_sha256,expires_at) VALUES(?,?,?,?,?,?,?)`, v.ID, m.ID, string(b), c.Generation, p.Revision, m.SHA, s.now().Add(30*time.Minute).Unix()); e != nil {
			return nil, e
		}
	}
	return candidates, tx.Commit()
}
func (s *Service) selected(ctx context.Context, j Job, p Preferences, c Credentials, m media) (Candidate, error) {
	var candidate Candidate
	var b, generation, sha string
	var revision int
	var expires int64
	e := s.db.QueryRowContext(ctx, `SELECT payload,generation,revision,media_sha256,expires_at FROM subtitle_candidates WHERE id=? AND media_file_id=?`, j.Selected, m.ID).Scan(&b, &generation, &revision, &sha, &expires)
	if errors.Is(e, sql.ErrNoRows) {
		return candidate, problem("candidate_expired", "This subtitle choice expired. Search again.")
	}
	if e != nil {
		return candidate, e
	}
	if e = json.Unmarshal([]byte(b), &candidate); e != nil {
		return candidate, e
	}
	if generation != c.Generation || revision != p.Revision || sha != m.SHA || expires < s.now().Unix() || !candidate.CanDownload || !slices.Contains(p.Tracks, candidate.Track) {
		return candidate, problem("candidate_expired", "The subtitle choice or its preferences changed. Search again before downloading.")
	}
	// Previously reviewed candidates without a format were always SRT.
	if candidate.Format == "" {
		candidate.Format = "srt"
	}
	if candidate.Format != candidateFormat(p, candidate) {
		return candidate, problem("candidate_expired", "The subtitle format choice changed. Search and review again.")
	}
	return candidate, nil
}
