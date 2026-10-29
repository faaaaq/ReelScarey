package transfer

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/faaaaq/ReelScarey/internal/organization"
	"github.com/faaaaq/ReelScarey/internal/quality"
)

type MatchCandidate struct {
	MediaID  int64    `json:"media_id"`
	TMDbID   int64    `json:"tmdb_id"`
	Title    string   `json:"title"`
	Year     *int     `json:"year"`
	Source   string   `json:"source"`
	Score    int      `json:"score"`
	Reasons  []string `json:"reasons"`
	Episodes []int64  `json:"episode_ids"`
}
type FileSuggestions struct {
	File       DiscoveredFile   `json:"file"`
	Candidates []MatchCandidate `json:"candidates"`
	Message    string           `json:"message"`
}

func (s *Service) MatchFiles(ctx context.Context, scanID string, ids []string) (LibraryScan, []DiscoveredFile, error) {
	if len(ids) < 1 || len(ids) > 25 {
		return LibraryScan{}, nil, problem("invalid_mapping", "Select 1–25 discovered files from one scan page.")
	}
	scan, e := s.LibraryScan(ctx, scanID)
	if e != nil {
		return scan, nil, e
	}
	root, e := s.existingRoot(ctx, &scan)
	if e != nil {
		return scan, nil, e
	}
	defer root.Close()
	seen := map[string]bool{}
	out := []DiscoveredFile{}
	for _, id := range ids {
		if seen[id] || len(id) != 32 {
			return scan, nil, mappingError()
		}
		seen[id] = true
		f, e := scanDiscovered(s.db.QueryRowContext(ctx, `SELECT `+discoveredColumns+` FROM library_scan_files WHERE id=? AND scan_id=? AND state IN ('review','blocked')`, id, scanID))
		if e != nil {
			return scan, nil, e
		}
		if e = verifyMatchFile(root, f); e != nil {
			return scan, nil, e
		}
		f.Hint = discoveryHint(f.Path, scan.Kind)
		f.FullPath = filepath.Join(s.cfg.MediaDir, filepath.FromSlash(f.Path))
		out = append(out, f)
	}
	return scan, out, nil
}
func verifyMatchFile(root *os.Root, f DiscoveredFile) error {
	if e := segments(root, f.Path, false); e != nil {
		return e
	}
	before, e := root.Lstat(filepath.FromSlash(f.Path))
	if e != nil {
		return e
	}
	opened, e := root.Open(filepath.FromSlash(f.Path))
	if e != nil {
		return e
	}
	defer opened.Close()
	info, e := opened.Stat()
	if e != nil {
		return e
	}
	identity, e := existingIdentity(opened)
	if e != nil {
		return e
	}
	if !info.Mode().IsRegular() || !os.SameFile(before, info) || info.Size() != f.Size || info.ModTime().UnixNano() != f.Mtime || identity != f.identity {
		return problem("source_changed", "A selected file changed since discovery. Start a new scan before matching it.")
	}
	return nil
}
func (s *Service) LocalMatchSuggestions(ctx context.Context, scan LibraryScan, f DiscoveredFile) (FileSuggestions, error) {
	out := FileSuggestions{File: f, Candidates: []MatchCandidate{}, Message: "Suggestions are hints only. Choose each canonical title and loaded episode explicitly."}
	items, e := s.ImportCatalog(ctx, scan.Kind, f.Hint)
	if e != nil {
		return out, e
	}
	if len(items) == 0 {
		words := strings.Fields(quality.Normalize(f.Hint))
		if len(words) > 0 {
			items, e = s.ImportCatalog(ctx, scan.Kind, words[0])
			if e != nil {
				return out, e
			}
		}
	}
	for _, item := range items {
		candidate := MatchCandidate{MediaID: item.ID, TMDbID: item.TMDbID, Title: item.Title, Year: item.Year, Source: "catalog", Reasons: []string{}, Episodes: []int64{}}
		candidate.Score = MatchScore(f, item.Title, item.Year)
		if candidate.Score == 0 {
			continue
		}
		candidate.Reasons = append(candidate.Reasons, "Saved catalog title similarity; review identity.")
		if scan.Kind == "series" {
			rows, e := s.db.QueryContext(ctx, `SELECT e.id,e.tmdb_id,ss.number,e.number,e.title FROM episodes e JOIN seasons ss ON ss.id=e.season_id WHERE ss.series_id=? AND ss.loaded_at<>'' ORDER BY ss.number,e.number LIMIT 10000`, item.ID)
			if e != nil {
				return out, e
			}
			targets := []TVTarget{}
			for rows.Next() {
				var v TVTarget
				if e = rows.Scan(&v.EpisodeID, &v.TMDbID, &v.Season, &v.Number, &v.Title); e != nil {
					rows.Close()
					return out, e
				}
				targets = append(targets, v)
			}
			e = rows.Err()
			rows.Close()
			if e != nil {
				return out, e
			}
			candidate.Episodes = suggestedEpisodes(f.Path, targets)
			if len(candidate.Episodes) > 0 {
				candidate.Reasons = append(candidate.Reasons, "Explicit filename episode labels; review each loaded canonical episode.")
			}
		}
		out.Candidates = append(out.Candidates, candidate)
	}
	sort.SliceStable(out.Candidates, func(i, j int) bool { return out.Candidates[i].Score > out.Candidates[j].Score })
	if len(out.Candidates) > 5 {
		out.Candidates = out.Candidates[:5]
	}
	return out, nil
}
func MatchScore(f DiscoveredFile, title string, year *int) int {
	a, b := quality.Normalize(f.Hint), quality.Normalize(title)
	if a == "" || b == "" {
		return 0
	}
	score := 0
	if a == b {
		score = 100
	} else {
		tokens := strings.Fields(a)
		hits := 0
		for _, v := range tokens {
			if strings.Contains(" "+b+" ", " "+v+" ") {
				hits++
			}
		}
		score = hits * 60 / max(len(tokens), len(strings.Fields(b)))
	}
	if m := hintYear.FindString(f.Path); m != "" && year != nil {
		n, _ := strconv.Atoi(m)
		if n == *year {
			score += 20
		} else {
			score -= 40
		}
	}
	return max(0, score)
}

type BulkMatchChoice struct {
	FileID   string  `json:"file_id"`
	MediaID  int64   `json:"media_id"`
	Episodes []int64 `json:"episode_ids"`
}
type reviewedMatch struct {
	Choice           BulkMatchChoice `json:"choice"`
	File             DiscoveredFile  `json:"file"`
	Identity         string          `json:"identity"`
	Kind             string          `json:"kind"`
	ProviderID       int64           `json:"provider_id"`
	Title            string          `json:"title"`
	Year             int             `json:"year"`
	Monitored        bool            `json:"monitored"`
	CategoryRevision int             `json:"category_revision"`
	SeriesRevision   int64           `json:"series_revision"`
	EpisodeSnapshot  []TVTarget      `json:"episodes"`
}
type BulkMatchReview struct {
	ID      string          `json:"review_id"`
	ScanID  string          `json:"scan_id"`
	Items   []reviewedMatch `json:"items"`
	Expires int64           `json:"expires_at"`
}
type matchPayload struct {
	Scan         LibraryScan     `json:"scan"`
	ResolvedRoot string          `json:"resolved_root"`
	Items        []reviewedMatch `json:"items"`
}

func (s *Service) matchSnapshot(ctx context.Context, tx *sql.Tx, scan LibraryScan, choices []BulkMatchChoice, root *os.Root) (matchPayload, error) {
	p := matchPayload{Scan: scan, ResolvedRoot: scan.ResolvedRoot, Items: []reviewedMatch{}}
	p.Scan.ResolvedRoot = ""
	p.Scan.owner = ""
	seenFiles := map[string]bool{}
	seenTargets := map[string]bool{}
	for _, choice := range choices {
		if seenFiles[choice.FileID] || len(choice.FileID) != 32 {
			return p, mappingError()
		}
		seenFiles[choice.FileID] = true
		v := reviewedMatch{Choice: choice, EpisodeSnapshot: []TVTarget{}}
		f, e := scanDiscovered(tx.QueryRowContext(ctx, `SELECT `+discoveredColumns+` FROM library_scan_files WHERE id=? AND scan_id=?`, choice.FileID, scan.ID))
		if e != nil {
			return p, e
		}
		v.File = f
		v.Identity = f.identity
		v.File.targets = nil
		v.File.owner = ""
		v.File.identity = ""
		if e = verifyMatchFile(root, f); e != nil {
			return p, e
		}
		if e = s.registerDiscoveredTx(ctx, tx, choice.FileID, ExistingRegistration{MediaID: choice.MediaID, Episodes: choice.Episodes, Reviewed: true}, true); e != nil {
			return p, e
		}
		if e = tx.QueryRowContext(ctx, `SELECT m.kind,m.tmdb_id,m.title,COALESCE(m.year,0),m.monitored,m.category_revision,COALESCE(d.revision,0) FROM media_items m LEFT JOIN series_details d ON d.media_id=m.id WHERE m.id=?`, choice.MediaID).Scan(&v.Kind, &v.ProviderID, &v.Title, &v.Year, &v.Monitored, &v.CategoryRevision, &v.SeriesRevision); e != nil {
			return p, e
		}
		keys := []string{v.Kind + ":" + strconv.FormatInt(choice.MediaID, 10)}
		if v.Kind == "series" {
			keys = []string{}
			for _, eid := range choice.Episodes {
				var target TVTarget
				if e = tx.QueryRowContext(ctx, `SELECT e.id,e.tmdb_id,ss.number,e.number,e.title FROM episodes e JOIN seasons ss ON ss.id=e.season_id WHERE e.id=? AND ss.series_id=?`, eid, choice.MediaID).Scan(&target.EpisodeID, &target.TMDbID, &target.Season, &target.Number, &target.Title); e != nil {
					return p, e
				}
				v.EpisodeSnapshot = append(v.EpisodeSnapshot, target)
				keys = append(keys, "episode:"+strconv.FormatInt(eid, 10))
			}
		}
		for _, key := range keys {
			if seenTargets[key] {
				return p, problem("invalid_mapping", "Two selected files claim the same movie or episode. Resolve the overlap before reviewing.")
			}
			seenTargets[key] = true
		}
		var busy int
		if v.Kind == "movie" {
			e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM library_scan_files WHERE id<>? AND media_id=? AND state IN ('queued','verifying')`, f.ID, choice.MediaID).Scan(&busy)
		} else {
			for _, eid := range choice.Episodes {
				var n int
				if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM library_scan_files f,json_each(f.targets) t WHERE f.id<>? AND f.media_id=? AND f.state IN ('queued','verifying') AND json_extract(t.value,'$.episode_id')=?`, f.ID, choice.MediaID, eid).Scan(&n); e != nil {
					return p, e
				}
				busy += n
			}
		}
		if e != nil {
			return p, e
		}
		if busy > 0 {
			return p, problem("scan_busy", "Another registration already reserves a selected movie or episode.")
		}
		p.Items = append(p.Items, v)
	}
	return p, nil
}
func (s *Service) ReviewMatches(ctx context.Context, scanID string, choices []BulkMatchChoice) (BulkMatchReview, error) {
	defer organization.ReadMedia()()
	if len(choices) < 1 || len(choices) > 25 {
		return BulkMatchReview{}, mappingError()
	}
	scan, e := s.LibraryScan(ctx, scanID)
	if e != nil {
		return BulkMatchReview{}, e
	}
	root, e := s.existingRoot(ctx, &scan)
	if e != nil {
		return BulkMatchReview{}, e
	}
	defer root.Close()
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return BulkMatchReview{}, e
	}
	defer tx.Rollback()
	if e = organization.EnsureMediaAvailableTx(ctx, tx); e != nil {
		return BulkMatchReview{}, e
	}
	p, e := s.matchSnapshot(ctx, tx, scan, choices, root)
	if e != nil {
		return BulkMatchReview{}, e
	}
	payload, e := json.Marshal(p)
	if e != nil {
		return BulkMatchReview{}, e
	}
	v := BulkMatchReview{ID: ID(), ScanID: scanID, Items: p.Items, Expires: time.Now().Unix() + 1800}
	_, _ = tx.ExecContext(ctx, `DELETE FROM library_match_reviews WHERE expires_at<unixepoch()`)
	if _, e = tx.ExecContext(ctx, `INSERT INTO library_match_reviews(id,scan_id,payload,expires_at) VALUES(?,?,?,?)`, v.ID, scanID, string(payload), v.Expires); e != nil {
		return v, e
	}
	return v, tx.Commit()
}
func (s *Service) ApplyMatches(ctx context.Context, scanID, reviewID string, reviewed bool) error {
	if !reviewed || len(reviewID) != 32 {
		return problem("invalid_mapping", "Review the complete file/title/episode batch before registering.")
	}
	defer organization.ReadMedia()()
	scan, e := s.LibraryScan(ctx, scanID)
	if e != nil {
		return e
	}
	root, e := s.existingRoot(ctx, &scan)
	if e != nil {
		return e
	}
	defer root.Close()
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = organization.EnsureMediaAvailableTx(ctx, tx); e != nil {
		return e
	}
	var raw string
	if e = tx.QueryRowContext(ctx, `SELECT payload FROM library_match_reviews WHERE id=? AND scan_id=? AND expires_at>=unixepoch() AND consumed_at=0`, reviewID, scanID).Scan(&raw); e != nil {
		return problem("scan_busy", "The match review expired or was already applied. Review again.")
	}
	var old matchPayload
	if e = json.Unmarshal([]byte(raw), &old); e != nil {
		return e
	}
	choices := []BulkMatchChoice{}
	for _, v := range old.Items {
		choices = append(choices, v.Choice)
	}
	current, e := s.matchSnapshot(ctx, tx, scan, choices, root)
	if e != nil {
		return e
	}
	// Scan progress counts can change independently; root, scope and selected rows cannot.
	old.Scan.Discovered = current.Scan.Discovered
	old.Scan.Registered = current.Scan.Registered
	old.Scan.Queued = current.Scan.Queued
	old.Scan.NeedsMatch = current.Scan.NeedsMatch
	old.Scan.NeedsAttention = current.Scan.NeedsAttention
	old.Scan.Examined = current.Scan.Examined
	old.Scan.Skipped = current.Scan.Skipped
	old.Scan.Updated = current.Scan.Updated
	if !reflect.DeepEqual(old, current) {
		return problem("scan_busy", "A selected file, title, episode or scan changed. Review the batch again.")
	}
	for _, v := range current.Items {
		if e = s.registerDiscoveredTx(ctx, tx, v.Choice.FileID, ExistingRegistration{MediaID: v.Choice.MediaID, Episodes: v.Choice.Episodes, Reviewed: true}, false); e != nil {
			return e
		}
	}
	if _, e = tx.ExecContext(ctx, `UPDATE library_match_reviews SET consumed_at=unixepoch() WHERE id=?`, reviewID); e != nil {
		return e
	}
	return tx.Commit()
}
