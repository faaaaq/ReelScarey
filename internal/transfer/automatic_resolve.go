package transfer

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/faaaaq/ReelScarey/internal/library"
	"github.com/faaaaq/ReelScarey/internal/metadata"
	"github.com/faaaaq/ReelScarey/internal/quality"
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
	rows, err := s.savedAutomaticTitles(ctx, scan.Kind, h)
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
		item, _, err = store.AddMovieAuthorized(ctx, full, library.DefaultMovieMonitored(full), authorize, category)
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
		var created bool
		item, created, err = store.AddSeriesAuthorized(ctx, full, library.DefaultSeriesMonitored(full), authorize, category)
		if err == nil && created && library.DefaultSeriesMonitored(full) {
			if e := store.ApplyFutureOnlySeries(ctx, item.ID); e != nil {
				return out, e
			}
		}
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

// automaticEpisodeGap reports that the filename's requested season or episode
// number is not part of the saved canonical catalog. It is a candidate for the
// exact-title fallback, not a final review reason.
type automaticEpisodeGap struct {
	SeasonMissing bool
	Season        int
	Episode       int
}

func (e *automaticEpisodeGap) Error() string { return "the requested canonical episode is unavailable" }

func (e *automaticEpisodeGap) reason() string {
	if e.SeasonMissing {
		return fmt.Sprintf("TMDb has no season %d for this show", e.Season)
	}
	return fmt.Sprintf("TMDb season %d has no episode %d", e.Season, e.Episode)
}

func (e *automaticEpisodeGap) message(title string) string {
	if usableEpisodeTitle(title) {
		return fmt.Sprintf("%s and no episode is titled %q. Review the season and episode mapping.", e.reason(), strings.TrimSpace(title))
	}
	return fmt.Sprintf("%s and the filename does not include a usable episode title. Review the season and episode mapping.", e.reason())
}

func (e *automaticEpisodeGap) multiEpisodeMessage(title string) string {
	if usableEpisodeTitle(title) {
		return fmt.Sprintf("%s; the exact title identifies only one episode, so this multi-episode file needs review.", e.reason())
	}
	return fmt.Sprintf("%s; this multi-episode file has no usable episode title evidence and needs review.", e.reason())
}

func usableEpisodeTitle(title string) bool {
	return len(quality.Normalize(title)) >= 2
}

// automaticSeriesEpisodes applies the numbered mapping first. When that mapping
// is unavailable, a unique exact canonical episode title may identify the
// episode in another TMDb season. Partial titles, ranges and duplicates stay in
// manual review.
func (s *Service) automaticSeriesEpisodes(ctx context.Context, scan LibraryScan, owner string, h automaticHint, title automaticTitle) ([]int64, string, error) {
	if h.Season >= 0 && len(h.Episodes) > 0 {
		episodes, err := s.automaticEpisodes(ctx, scan, owner, h, title)
		if err == nil {
			return episodes, "", nil
		}
		var gap *automaticEpisodeGap
		if !errors.As(err, &gap) {
			return nil, "", err
		}
		if len(h.Episodes) > 1 {
			return nil, "", automaticReview(gap.multiEpisodeMessage(h.EpisodeTitle))
		}
		if !usableEpisodeTitle(h.EpisodeTitle) {
			return nil, "", automaticReview(gap.message(h.EpisodeTitle))
		}
		return s.automaticEpisodesByTitle(ctx, scan, owner, h, title, gap)
	}
	if !usableEpisodeTitle(h.EpisodeTitle) {
		return nil, "", automaticReview("The filename does not contain a usable episode title. Review the mapping.")
	}
	return s.automaticEpisodesByTitle(ctx, scan, owner, h, title, nil)
}

func (s *Service) automaticEpisodes(ctx context.Context, scan LibraryScan, owner string, h automaticHint, title automaticTitle) ([]int64, error) {
	store := library.New(s.db)
	season, err := store.TVSeason(ctx, title.MediaID, h.Season)
	if errors.Is(err, sql.ErrNoRows) {
		// Saved show metadata can predate a newly added season, which is
		// common for ongoing anime. Pull the current season list from the
		// provider and save it through the guarded refresh path, then retry.
		// On ErrNoRows TVSeason has no usable season, and any revision it
		// scanned internally is incidental. Read the current show revision
		// explicitly before refreshing.
		if s.automaticMetadata == nil {
			return nil, metadata.ErrNotConfigured
		}
		show, e := store.TVShow(ctx, title.MediaID)
		if e != nil {
			return nil, e
		}
		fresh, e := s.automaticMetadata.Series(ctx, title.Movie.TMDbID)
		if e != nil {
			if automaticInvalidProviderDetails(e) {
				return nil, automaticReview("TMDb returned invalid details for this show. Review the mapping.")
			}
			return nil, e
		}
		if fresh.Movie.TMDbID != title.Movie.TMDbID || !h.matches(fresh.Movie.Title, fresh.Movie.Year, fresh.Movie.TMDbID) {
			return nil, automaticReview("The provider show identity changed. Review the mapping.")
		}
		if e = store.RefreshSeries(ctx, title.MediaID, fresh, show.Revision); e != nil {
			if errors.Is(e, library.ErrRevision) {
				// A concurrent metadata refresh may have advanced the show
				// revision. Re-read it once and retry the same canonical write.
				current, readErr := store.TVShow(ctx, title.MediaID)
				if readErr == nil {
					e = store.RefreshSeries(ctx, title.MediaID, fresh, current.Revision)
				}
			}
			if e != nil {
				return nil, e
			}
		}
		season, err = store.TVSeason(ctx, title.MediaID, h.Season)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, &automaticEpisodeGap{SeasonMissing: true, Season: h.Season}
		}
	}
	if err != nil {
		return nil, err
	}
	if season.Season.LoadedAt == "" {
		if s.automaticMetadata == nil {
			return nil, metadata.ErrNotConfigured
		}
		fresh, e := s.automaticMetadata.Season(ctx, title.Movie.TMDbID, h.Season)
		if automaticMetadataMissing(e) {
			return nil, &automaticEpisodeGap{SeasonMissing: true, Season: h.Season}
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
			if errors.Is(err, library.ErrRevision) {
				// The season was not written; re-read its current revision and
				// retry the same guarded provider snapshot once.
				current, readErr := store.TVSeason(ctx, title.MediaID, h.Season)
				if readErr == nil && current.Season.TMDbID == season.Season.TMDbID {
					err = store.SaveSeasonAuthorized(ctx, title.MediaID, fresh, current.Revision, func(tx *sql.Tx) error { return s.automaticGuard(ctx, tx, scan.ID, owner) })
				}
			}
			if err != nil {
				return nil, err
			}
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
			return nil, &automaticEpisodeGap{Season: h.Season, Episode: number}
		}
		out = append(out, matched)
	}
	return out, nil
}

// automaticEpisodesByTitle matches an episode by an exact normalized canonical
// title across the show's eligible seasons. An empty gap means no numbered
// mapping was requested; otherwise the gap explains which requested season or
// episode was unavailable. The match must be unique across the whole show.
func (s *Service) automaticEpisodesByTitle(ctx context.Context, scan LibraryScan, owner string, h automaticHint, title automaticTitle, gap *automaticEpisodeGap) ([]int64, string, error) {
	name := quality.Normalize(h.EpisodeTitle)
	if len(name) < 2 {
		return nil, "", automaticReview("The filename does not contain a usable episode title. Review the mapping.")
	}
	type seasonRow struct {
		Number   int
		Loaded   bool
		Episodes int
	}
	rows, err := s.db.QueryContext(ctx, `SELECT number,loaded_at<>'',episode_count FROM seasons WHERE series_id=? ORDER BY number`, title.MediaID)
	if err != nil {
		return nil, "", err
	}
	seasons := []seasonRow{}
	for rows.Next() {
		var season seasonRow
		if err = rows.Scan(&season.Number, &season.Loaded, &season.Episodes); err != nil {
			rows.Close()
			return nil, "", err
		}
		seasons = append(seasons, season)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, "", err
	}
	if len(seasons) == 0 {
		return nil, "", automaticReview("This show has no saved seasons. Review the show or refresh its seasons.")
	}
	store := library.New(s.db)
	fetchable := len(seasons) <= 20
	type titleMatch struct {
		ID     int64
		Season int
		Number int
	}
	matched := []titleMatch{}
	seen := map[int64]bool{}
	unloaded := 0
	for _, season := range seasons {
		if season.Episodes == 0 && !season.Loaded {
			continue
		}
		details, err := store.TVSeason(ctx, title.MediaID, season.Number)
		if err != nil {
			return nil, "", err
		}
		if details.Season.LoadedAt == "" {
			if s.automaticMetadata == nil || !fetchable {
				unloaded++
				continue
			}
			fresh, e := s.automaticMetadata.Season(ctx, title.Movie.TMDbID, season.Number)
			if automaticMetadataMissing(e) {
				continue
			}
			if e != nil {
				if automaticInvalidProviderDetails(e) {
					return nil, "", automaticReview("TMDb returned invalid season details. Review the episode mapping.")
				}
				return nil, "", e
			}
			if fresh.Number != season.Number || fresh.TMDbID != details.Season.TMDbID {
				return nil, "", automaticReview("The provider season identity changed. Review this season.")
			}
			if err = store.SaveSeasonAuthorized(ctx, title.MediaID, fresh, details.Revision, func(tx *sql.Tx) error { return s.automaticGuard(ctx, tx, scan.ID, owner) }); err != nil {
				if errors.Is(err, library.ErrRevision) {
					current, readErr := store.TVSeason(ctx, title.MediaID, season.Number)
					if readErr == nil && current.Season.TMDbID == details.Season.TMDbID {
						err = store.SaveSeasonAuthorized(ctx, title.MediaID, fresh, current.Revision, func(tx *sql.Tx) error { return s.automaticGuard(ctx, tx, scan.ID, owner) })
					}
				}
				if err != nil {
					return nil, "", err
				}
			}
			details, err = store.TVSeason(ctx, title.MediaID, season.Number)
			if err != nil {
				return nil, "", err
			}
		}
		for _, episode := range details.Episodes {
			if episode.Title == "" || quality.Normalize(episode.Title) != name || seen[episode.ID] {
				continue
			}
			seen[episode.ID] = true
			matched = append(matched, titleMatch{ID: episode.ID, Season: season.Number, Number: episode.Number})
		}
	}
	if len(matched) > 1 {
		list := []string{}
		lastSeason := -1
		for _, match := range matched {
			if match.Season != lastSeason {
				list = append(list, strconv.Itoa(match.Season))
				lastSeason = match.Season
			}
		}
		where := "seasons " + strings.Join(list, ", ")
		if len(list) == 1 {
			where = "season " + list[0]
		}
		if gap != nil {
			return nil, "", automaticReview(fmt.Sprintf("%s; more than one episode is titled %q (%s). Review the mapping.", gap.reason(), strings.TrimSpace(h.EpisodeTitle), where))
		}
		return nil, "", automaticReview(fmt.Sprintf("More than one episode is titled %q (%s). Review the mapping.", strings.TrimSpace(h.EpisodeTitle), where))
	}
	if unloaded > 0 {
		if len(matched) == 1 {
			if gap != nil {
				return nil, "", automaticReview(fmt.Sprintf("%s, and the episode titled %q appears once in the loaded seasons; another season could not be loaded. Connect TMDb or load more seasons, then retry matching.", gap.reason(), strings.TrimSpace(h.EpisodeTitle)))
			}
			return nil, "", automaticReview(fmt.Sprintf("The episode titled %q appears once in the loaded seasons, but another season could not be loaded. Connect TMDb or load more seasons, then retry matching.", strings.TrimSpace(h.EpisodeTitle)))
		}
		if gap != nil {
			return nil, "", automaticReview(fmt.Sprintf("%s and no episode is titled %q in the loaded seasons. Connect TMDb or load more seasons, then retry matching.", gap.reason(), strings.TrimSpace(h.EpisodeTitle)))
		}
		return nil, "", automaticReview(fmt.Sprintf("No episode is titled %q in the loaded seasons. Connect TMDb or load more seasons, then retry matching.", strings.TrimSpace(h.EpisodeTitle)))
	}
	if len(matched) == 1 {
		match := matched[0]
		return []int64{match.ID}, titleMatchNote(h, match.Season, match.Number), nil
	}
	if gap != nil {
		return nil, "", automaticReview(gap.message(h.EpisodeTitle))
	}
	return nil, "", automaticReview(fmt.Sprintf("No episode is titled %q. Review the mapping.", strings.TrimSpace(h.EpisodeTitle)))
}

// titleMatchNote records an accepted cross-number mapping for the review UI.
// It never changes canonical IDs; registration still uses the matched episode.
func titleMatchNote(h automaticHint, season, number int) string {
	if h.Season < 0 {
		return ""
	}
	if len(h.Episodes) == 0 {
		if h.Season == season {
			return ""
		}
		return fmt.Sprintf("Mapped the unnumbered file from season %d to TMDb S%02dE%02d using the exact episode title %q.", h.Season, season, number, strings.TrimSpace(h.EpisodeTitle))
	}
	if h.Season == season && h.Episodes[0] == number {
		return ""
	}
	return fmt.Sprintf("Mapped S%02dE%02d to TMDb S%02dE%02d using the exact episode title %q.", h.Season, h.Episodes[0], season, number, strings.TrimSpace(h.EpisodeTitle))
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
	note := ""
	if scan.Kind == "series" {
		choice.Episodes, note, err = s.automaticSeriesEpisodes(ctx, scan, owner, h, title)
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
		raw, e := json.Marshal(automaticCandidate{Hint: h, Snapshot: now, Note: note})
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

// The provider ID uses the existing unique (kind, tmdb_id) index. The
// name path maintains a normalized key lazily so upgrades don't do a full
// library rewrite at startup. A title edit invalidates only its own key.
func (s *Service) savedAutomaticTitles(ctx context.Context, kind string, h automaticHint) (*sql.Rows, error) {
	const fields = `SELECT id,tmdb_id,title,year FROM media_items WHERE kind=?`
	if h.ProviderID > 0 {
		return s.db.QueryContext(ctx, fields+` AND tmdb_id=?`, kind, h.ProviderID)
	}
	for {
		n, err := s.indexAutomaticTitles(ctx, kind)
		if err != nil {
			return nil, err
		}
		if n < 256 {
			break
		}
	}
	title := quality.Normalize(h.Title)
	if h.Year != 0 {
		return s.db.QueryContext(ctx, fields+` AND match_title=? AND year=? LIMIT 2`, kind, title, h.Year)
	}
	return s.db.QueryContext(ctx, fields+` AND match_title=? LIMIT 2`, kind, title)
}

func (s *Service) indexAutomaticTitles(ctx context.Context, kind string) (int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,title FROM media_items WHERE kind=? AND match_title IS NULL ORDER BY id LIMIT 256`, kind)
	if err != nil {
		return 0, err
	}
	type titleRow struct {
		id    int64
		title string
	}
	var batch []titleRow
	for rows.Next() {
		var row titleRow
		if err := rows.Scan(&row.id, &row.title); err != nil {
			rows.Close()
			return 0, err
		}
		batch = append(batch, row)
	}
	rows.Close()
	if err := rows.Err(); err != nil || len(batch) == 0 {
		return 0, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	for _, row := range batch {
		if _, err := tx.ExecContext(ctx, `UPDATE media_items SET match_title=? WHERE id=? AND title=? AND match_title IS NULL`,
			quality.Normalize(row.title), row.id, row.title); err != nil {
			return 0, err
		}
	}
	return len(batch), tx.Commit()
}
