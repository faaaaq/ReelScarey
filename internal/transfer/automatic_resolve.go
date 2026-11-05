package transfer

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"

	"github.com/faaaaq/ReelScarey/internal/library"
	"github.com/faaaaq/ReelScarey/internal/metadata"
)

type automaticTitle struct {
	MediaID int64          `json:"media_id"`
	Movie   metadata.Movie `json:"movie"`
	Reason  string         `json:"reason"`
}

func automaticInvalidProviderDetails(err error) bool {
	var provider *metadata.Error
	return errors.As(err, &provider) && provider.Code == "tmdb_invalid_response"
}

func (s *Service) automaticTitle(ctx context.Context, scan LibraryScan, owner string, h automaticHint) (automaticTitle, error) {
	var out automaticTitle
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT payload FROM library_match_cache WHERE scan_id=? AND identity=?`, scan.ID, h.key()).Scan(&raw)
	if err == nil {
		err = json.Unmarshal([]byte(raw), &out)
		return out, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	out, err = s.resolveAutomaticTitle(ctx, scan, owner, h)
	if err != nil {
		var review automaticReview
		switch {
		case errors.As(err, &review):
			out.Reason = review.Error()
		case automaticMetadataMissing(err):
			out.Reason = "TMDb no longer lists this title. Choose a match manually."
		default:
			if automaticInvalidProviderDetails(err) {
				// One malformed provider record must not pause a scan containing
				// thousands of otherwise valid files.
				if h.ProviderID > 0 {
					out.Reason = "TMDb could not read this folder's ID. Review the title manually."
				} else {
					out.Reason = "TMDb returned invalid details for this title. Review it manually."
				}
				break
			}
			return out, err
		}
	}
	rawBytes, err := json.Marshal(out)
	if err != nil {
		return out, err
	}
	err = s.automaticTx(ctx, scan.ID, owner, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO library_match_cache(scan_id,identity,payload) VALUES(?,?,?)`, scan.ID, h.key(), string(rawBytes))
		return err
	})
	return out, err
}
func (s *Service) resolveAutomaticTitle(ctx context.Context, scan LibraryScan, owner string, h automaticHint) (automaticTitle, error) {
	out := automaticTitle{}
	year := h.Year
	if h.ProviderID > 0 {
		year = 0
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,tmdb_id,title,year FROM media_items WHERE kind=? AND (?=0 OR tmdb_id=?) AND (?=0 OR year=?)`, scan.Kind, h.ProviderID, h.ProviderID, year, h.Year)
	if err != nil {
		return out, err
	}
	found := []automaticTitle{}
	for rows.Next() {
		var v automaticTitle
		if err = rows.Scan(&v.MediaID, &v.Movie.TMDbID, &v.Movie.Title, &v.Movie.Year); err != nil {
			rows.Close()
			return out, err
		}
		if h.matches(v.Movie.Title, v.Movie.Year, v.Movie.TMDbID) {
			found = append(found, v)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	if len(found) > 1 {
		return out, automaticReview("More than one saved title matches this name. Choose the intended movie or show.")
	}
	if len(found) == 1 {
		return found[0], nil
	}
	if s.automaticMetadata == nil {
		return out, metadata.ErrNotConfigured
	}
	candidates := map[int64]metadata.Movie{}
	var directMovie *metadata.Movie
	var directSeries *metadata.Series
	if h.ProviderID > 0 {
		var m metadata.Movie
		if scan.Kind == "movie" {
			m, err = s.automaticMetadata.Movie(ctx, h.ProviderID)
			directMovie = &m
		} else {
			var show metadata.Series
			show, err = s.automaticMetadata.Series(ctx, h.ProviderID)
			m = show.Movie
			directSeries = &show
		}
		if err != nil {
			return out, err
		}
		if h.matches(m.Title, m.Year, m.TMDbID) {
			candidates[m.TMDbID] = m
		}
	} else {
		for page := 1; page <= 3; page++ {
			total := 1
			items := []metadata.Movie{}
			if scan.Kind == "movie" {
				var result metadata.Results
				result, err = s.automaticMetadata.Search(ctx, h.Title, page)
				items = result.Items
				total = result.TotalPages
			} else {
				var result metadata.SeriesResults
				result, err = s.automaticMetadata.SearchSeries(ctx, h.Title, page)
				total = result.TotalPages
				for _, show := range result.Items {
					items = append(items, show.Movie)
				}
			}
			if err != nil {
				return out, err
			}
			if total > 3 {
				return out, automaticReview("The provider search is too broad for a unique automatic match. Choose a title or use its TMDb ID in the title folder.")
			}
			for _, m := range items {
				if m.TMDbID > 0 && h.matches(m.Title, m.Year, m.TMDbID) {
					candidates[m.TMDbID] = m
				}
			}
			if page >= total {
				break
			}
		}
	}
	if len(candidates) != 1 {
		return out, automaticReview("No unique exact title/year match was found. Review the suggested titles.")
	}
	var chosen metadata.Movie
	for _, m := range candidates {
		chosen = m
	}
	store := library.New(s.db)
	authorize := func(tx *sql.Tx) error { return s.automaticGuard(ctx, tx, scan.ID, owner) }
	var category string
	if err = s.db.QueryRowContext(ctx, `SELECT category FROM library_match_jobs WHERE scan_id=?`, scan.ID).Scan(&category); err != nil {
		return out, err
	}
	var item library.Item
	if scan.Kind == "movie" {
		var full metadata.Movie
		var e error
		if directMovie != nil {
			full = *directMovie
		} else {
			full, e = s.automaticMetadata.Movie(ctx, chosen.TMDbID)
		}
		if e != nil {
			if automaticInvalidProviderDetails(e) {
				return out, automaticReview("TMDb returned invalid details for this title. Review it manually.")
			}
			return out, e
		}
		if full.TMDbID != chosen.TMDbID || !h.matches(full.Title, full.Year, full.TMDbID) {
			return out, automaticReview("Provider details disagree with the title match; review it.")
		}
		item, _, err = store.AddMovieAuthorized(ctx, full, false, authorize, category)
	} else {
		var full metadata.Series
		var e error
		if directSeries != nil {
			full = *directSeries
		} else {
			full, e = s.automaticMetadata.Series(ctx, chosen.TMDbID)
		}
		if e != nil {
			if automaticInvalidProviderDetails(e) {
				return out, automaticReview("TMDb returned invalid details for this show. Review it manually.")
			}
			return out, e
		}
		if full.TMDbID != chosen.TMDbID || !h.matches(full.Title, full.Year, full.TMDbID) {
			return out, automaticReview("Provider details disagree with the show match; review it.")
		}
		item, _, err = store.AddSeriesAuthorized(ctx, full, false, authorize, category)
	}
	if err != nil {
		return out, err
	}
	if !h.matches(item.Title, item.Year, item.TMDbID) {
		return out, automaticReview("The saved title changed during lookup. Review its identity.")
	}
	out.MediaID = item.ID
	out.Movie = metadata.Movie{TMDbID: item.TMDbID, Title: item.Title, Year: item.Year}
	return out, nil
}
func automaticMetadataMissing(err error) bool {
	var provider *metadata.Error
	return errors.As(err, &provider) && (provider.Code == "movie_not_found" || provider.Code == "series_not_found")
}

func (s *Service) automaticEpisodes(ctx context.Context, scan LibraryScan, owner string, h automaticHint, title automaticTitle) ([]int64, error) {
	store := library.New(s.db)
	season, err := store.TVSeason(ctx, title.MediaID, h.Season)
	if err != nil {
		return nil, automaticReview("The numbered season is not part of this show's saved metadata. Review the show or refresh its seasons.")
	}
	if season.Season.LoadedAt == "" {
		if s.automaticMetadata == nil {
			return nil, metadata.ErrNotConfigured
		}
		fresh, e := s.automaticMetadata.Season(ctx, title.Movie.TMDbID, h.Season)
		if automaticMetadataMissing(e) {
			return nil, automaticReview("TMDb no longer lists this season. Review the episode match.")
		}
		if e != nil {
			if automaticInvalidProviderDetails(e) {
				return nil, automaticReview("TMDb returned invalid season details. Review the episode mapping.")
			}
			return nil, e
		}
		if fresh.Number != h.Season || fresh.TMDbID != season.Season.TMDbID {
			return nil, automaticReview("The provider season identity changed. Review this season.")
		}
		if err = store.SaveSeasonAuthorized(ctx, title.MediaID, fresh, season.Revision, func(tx *sql.Tx) error { return s.automaticGuard(ctx, tx, scan.ID, owner) }); err != nil {
			return nil, err
		}
		season, err = store.TVSeason(ctx, title.MediaID, h.Season)
		if err != nil {
			return nil, err
		}
	}
	out := []int64{}
	for _, number := range h.Episodes {
		matched := int64(0)
		for _, episode := range season.Episodes {
			if episode.Number == number {
				if matched != 0 {
					return nil, automaticReview("Episode numbers are ambiguous in the saved season.")
				}
				matched = episode.ID
			}
		}
		if matched == 0 {
			return nil, automaticReview("The filename names an episode missing from the canonical season. Review its numbering.")
		}
		out = append(out, matched)
	}
	return out, nil
}
func (s *Service) resolveAutomaticNext(ctx context.Context, scanID, owner string) error {
	scan, err := s.automaticScan(ctx, scanID)
	if err != nil {
		return err
	}
	var id string
	err = s.db.QueryRowContext(ctx, `SELECT f.id FROM library_scan_files f WHERE f.scan_id=? AND f.state='review' AND NOT EXISTS(SELECT 1 FROM library_match_candidates c WHERE c.file_id=f.id) ORDER BY f.relative_path LIMIT 1`, scanID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return s.finishAutomaticResolution(ctx, scanID, owner)
	}
	if err != nil {
		return err
	}
	f, err := scanDiscovered(s.db.QueryRowContext(ctx, `SELECT `+discoveredColumns+` FROM library_scan_files WHERE id=?`, id))
	if err != nil {
		return err
	}
	root, err := s.existingRoot(ctx, &scan)
	if err != nil {
		return err
	}
	defer root.Close()
	if err = verifyMatchFile(root, f); err != nil {
		return s.saveAutomaticReview(ctx, scanID, owner, id, "The file changed since discovery. Start a fresh scan before matching it.")
	}
	h, err := automaticIdentity(scan, f)
	if err != nil {
		return s.saveAutomaticReview(ctx, scanID, owner, id, err.Error())
	}
	title, err := s.automaticTitle(ctx, scan, owner, h)
	if err != nil {
		return err
	}
	if title.Reason != "" {
		return s.saveAutomaticReview(ctx, scanID, owner, id, title.Reason)
	}
	choice := BulkMatchChoice{FileID: id, MediaID: title.MediaID, Episodes: []int64{}}
	if scan.Kind == "series" {
		choice.Episodes, err = s.automaticEpisodes(ctx, scan, owner, h, title)
		if err != nil {
			var review automaticReview
			if errors.As(err, &review) {
				return s.saveAutomaticReview(ctx, scanID, owner, id, review.Error())
			}
			return err
		}
	}
	err = s.automaticTx(ctx, scanID, owner, func(tx *sql.Tx) error {
		payload, e := s.matchSnapshot(ctx, tx, scan, []BulkMatchChoice{choice}, root)
		if e != nil {
			return e
		}
		now := payload.Items[0]
		year := now.Year
		if !h.matches(now.Title, &year, now.ProviderID) {
			return automaticReview("The title changed while resolving the match.")
		}
		fresh, e := automaticIdentity(scan, now.File)
		if e != nil {
			return e
		}
		if !reflect.DeepEqual(fresh, h) {
			return automaticReview("The filename identity changed during matching.")
		}
		raw, e := json.Marshal(automaticCandidate{Hint: h, Snapshot: now})
		if e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO library_match_candidates(file_id,scan_id,state,payload) VALUES(?,?,'ready',?)`, id, scanID, string(raw)); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE library_scan_files SET message='Title matched. Waiting for the scan-wide duplicate check before file verification.' WHERE id=? AND state='review'`, id); e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `UPDATE library_match_jobs SET attempts=0,retry_at=0,message='Resolving title and episode identities.' WHERE scan_id=?`, scanID)
		return e
	})
	if err != nil && !errors.Is(err, ErrLease) && ctx.Err() == nil {
		var review automaticReview
		var problem *Error
		if errors.As(err, &review) || errors.As(err, &problem) {
			return s.saveAutomaticReview(ctx, scanID, owner, id, "The match conflicts with an existing file or changed catalog identity. Review it.")
		}
	}
	return err
}
