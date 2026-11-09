package library

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/faaaaq/ReelScarey/internal/metadata"
)

var ErrPlanningInput = errors.New("Choose valid planning filters, a page from 1–100000 and a calendar month in YYYY-MM format.")

type PlanningFilter struct {
	Kind, Status, Query, Month, Release string
	OnlyMonitored                       bool
	Page                                int
}
type PlanningEntry struct {
	Kind              string `json:"kind"`
	MovieVersion      string `json:"movie_version,omitempty"`
	ReleaseKind       string `json:"release_kind"`
	MediaID           int64  `json:"media_id"`
	EpisodeID         *int64 `json:"episode_id"`
	Title             string `json:"title"`
	Overview          string `json:"overview"`
	Year              *int   `json:"year"`
	EpisodeTitle      string `json:"episode_title"`
	Season            *int   `json:"season"`
	Episode           *int   `json:"episode"`
	Date              string `json:"date"`
	TheatricalDate    string `json:"theatrical_date"`
	DigitalDate       string `json:"digital_date"`
	PhysicalDate      string `json:"physical_date"`
	Monitored         bool   `json:"monitored"`
	MonitoringReason  string `json:"monitoring_reason"`
	Available         bool   `json:"available"`
	Status            string `json:"status"`
	DownloadID        string `json:"download_id"`
	DownloadState     string `json:"download_state"`
	Protocol          string `json:"protocol"`
	PosterURL         string `json:"poster_url"`
	RuntimeMinutes    *int   `json:"runtime_minutes"`
	SeriesStatus      string `json:"series_status"`
	MetadataUpdatedAt int64  `json:"metadata_updated_at"`
}
type PlanningCounts struct {
	Missing     int `json:"missing"`
	Upcoming    int `json:"upcoming"`
	Undated     int `json:"undated"`
	Downloading int `json:"downloading"`
	Attention   int `json:"attention"`
	Available   int `json:"available"`
}
type PlanningCoverage struct {
	UnloadedSeasons int `json:"unloaded_seasons"`
	Undated         int `json:"undated"`
}
type PlanningPage struct {
	Items         []PlanningEntry  `json:"items"`
	Counts        PlanningCounts   `json:"counts"`
	Coverage      PlanningCoverage `json:"coverage"`
	Total         int              `json:"total"`
	Page          int              `json:"page"`
	Limit         int              `json:"limit"`
	AsOfDate      string           `json:"as_of_date"`
	Month         string           `json:"month"`
	Kind          string           `json:"kind"`
	Status        string           `json:"status"`
	Query         string           `json:"query"`
	OnlyMonitored bool             `json:"monitored_only"`
}

// These are catalog facts, not a disk scan or download eligibility assertion.
// EXISTS avoids multiplying shared episode files or multi-target downloads.
const planningCTE = `WITH clock AS (SELECT ? AS today), modes AS (SELECT ? AS calendar), movie_base AS (
 SELECT m.id,m.title,m.year,m.release_date,m.theatrical_release_date,m.digital_release_date,m.physical_release_date,m.poster_path,m.overview,m.monitored,
 EXISTS(SELECT 1 FROM media_files f WHERE f.movie_id=m.id AND (mv.version='' OR (mv.version='hd' AND f.video_resolution BETWEEN 1 AND 1080) OR (mv.version='uhd' AND f.video_resolution=2160))) AS available,
 COALESCE((SELECT d.id FROM downloads d WHERE d.movie_id=m.id AND d.kind='movie' AND d.state NOT IN ('completed','cancelled') AND (mv.version='' OR d.movie_version IN ('',mv.version)) ORDER BY d.created_at DESC,d.id DESC LIMIT 1),'') AS download_id,mv.version AS movie_version
 FROM media_items m CROSS JOIN (SELECT '' AS version UNION ALL SELECT 'hd' UNION ALL SELECT 'uhd') mv LEFT JOIN movie_version_settings vs ON vs.media_id=m.id
 WHERE m.kind='movie' AND ((COALESCE(vs.enabled,0)=0 AND mv.version='') OR (vs.enabled=1 AND mv.version<>''))
), raw AS (
 SELECT 'movie' AS kind,b.id AS media_id,NULL AS episode_id,b.title,b.year,'' AS episode_title,NULL AS season,NULL AS episode,b.release_date AS raw_date,'' AS release_kind,b.theatrical_release_date AS raw_theatrical,b.digital_release_date AS raw_digital,b.physical_release_date AS raw_physical,b.poster_path,b.overview,
 b.monitored AS monitored,CASE WHEN b.monitored=0 THEN 'Movie monitoring is paused.' ELSE '' END AS monitoring_reason,b.available,b.download_id,b.movie_version
 FROM movie_base b CROSS JOIN modes mo
 WHERE mo.calendar=0 OR NOT EXISTS(SELECT 1 FROM (SELECT b.theatrical_release_date AS d UNION ALL SELECT b.digital_release_date UNION ALL SELECT b.physical_release_date) dr WHERE length(dr.d)=10 AND substr(dr.d,1,4) BETWEEN '0001' AND '9999' AND date(dr.d,'+0 days')=dr.d)
 UNION ALL
 SELECT 'movie',b.id,NULL,b.title,b.year,'',NULL,NULL,b.theatrical_release_date,'theatrical',b.theatrical_release_date,b.digital_release_date,b.physical_release_date,b.poster_path,b.overview,b.monitored,CASE WHEN b.monitored=0 THEN 'Movie monitoring is paused.' ELSE '' END,b.available,b.download_id,b.movie_version
 FROM movie_base b CROSS JOIN modes mo
 WHERE mo.calendar=1 AND length(b.theatrical_release_date)=10 AND substr(b.theatrical_release_date,1,4) BETWEEN '0001' AND '9999' AND date(b.theatrical_release_date,'+0 days')=b.theatrical_release_date
 UNION ALL
 SELECT 'movie',b.id,NULL,b.title,b.year,'',NULL,NULL,b.digital_release_date,'digital',b.theatrical_release_date,b.digital_release_date,b.physical_release_date,b.poster_path,b.overview,b.monitored,CASE WHEN b.monitored=0 THEN 'Movie monitoring is paused.' ELSE '' END,b.available,b.download_id,b.movie_version
 FROM movie_base b CROSS JOIN modes mo
 WHERE mo.calendar=1 AND length(b.digital_release_date)=10 AND substr(b.digital_release_date,1,4) BETWEEN '0001' AND '9999' AND date(b.digital_release_date,'+0 days')=b.digital_release_date
 UNION ALL
 SELECT 'movie',b.id,NULL,b.title,b.year,'',NULL,NULL,b.physical_release_date,'physical',b.theatrical_release_date,b.digital_release_date,b.physical_release_date,b.poster_path,b.overview,b.monitored,CASE WHEN b.monitored=0 THEN 'Movie monitoring is paused.' ELSE '' END,b.available,b.download_id,b.movie_version
 FROM movie_base b CROSS JOIN modes mo
 WHERE mo.calendar=1 AND length(b.physical_release_date)=10 AND substr(b.physical_release_date,1,4) BETWEEN '0001' AND '9999' AND date(b.physical_release_date,'+0 days')=b.physical_release_date
 UNION ALL
 SELECT m.kind,m.id,e.id,m.title,m.year,e.title,s.number,e.number,e.air_date,'' AS release_kind,'' AS raw_theatrical,'' AS raw_digital,'' AS raw_physical,m.poster_path,CASE WHEN e.overview<>'' THEN e.overview ELSE m.overview END,
 (m.monitored=1 AND s.monitored=1 AND e.monitored=1),
 CASE WHEN m.monitored=0 THEN 'Show monitoring is paused.' WHEN s.monitored=0 THEN 'Season monitoring is paused.' WHEN e.monitored=0 THEN 'Episode is not monitored.' ELSE '' END,
 EXISTS(SELECT 1 FROM episode_files ef WHERE ef.episode_id=e.id),
 COALESCE((SELECT d.id FROM episode_downloads ed JOIN downloads d ON d.id=ed.download_id WHERE ed.episode_id=e.id AND d.state NOT IN ('completed','cancelled') ORDER BY d.created_at DESC,d.id DESC LIMIT 1),''),''
 FROM episodes e JOIN seasons s ON s.id=e.season_id JOIN media_items m ON m.id=s.series_id WHERE s.loaded_at<>'' AND m.kind='series'
), normalized AS (
 SELECT r.*,CASE WHEN length(raw_date)=10 AND substr(raw_date,1,4) BETWEEN '0001' AND '9999' AND date(raw_date,'+0 days')=raw_date THEN raw_date ELSE '' END AS date,
 CASE WHEN length(raw_theatrical)=10 AND substr(raw_theatrical,1,4) BETWEEN '0001' AND '9999' AND date(raw_theatrical,'+0 days')=raw_theatrical THEN raw_theatrical ELSE '' END AS theatrical_date,
 CASE WHEN length(raw_digital)=10 AND substr(raw_digital,1,4) BETWEEN '0001' AND '9999' AND date(raw_digital,'+0 days')=raw_digital THEN raw_digital ELSE '' END AS digital_date,
 CASE WHEN length(raw_physical)=10 AND substr(raw_physical,1,4) BETWEEN '0001' AND '9999' AND date(raw_physical,'+0 days')=raw_physical THEN raw_physical ELSE '' END AS physical_date,
 COALESCE(d.state,'') AS download_state,COALESCE(d.protocol,'') AS protocol
 FROM raw r LEFT JOIN downloads d ON d.id=r.download_id
), entries AS (
 SELECT n.*,CASE WHEN available THEN 'available' WHEN download_id<>'' THEN CASE WHEN download_state IN ('blocked','failed') THEN 'attention' ELSE 'downloading' END WHEN date='' THEN 'undated' WHEN date>clock.today THEN 'upcoming' ELSE 'missing' END AS status
 FROM normalized n CROSS JOIN clock
), filtered AS (
 SELECT * FROM entries WHERE (?='all' OR kind=?) AND (?=0 OR monitored=1) AND (title LIKE ? ESCAPE '\' OR episode_title LIKE ? ESCAPE '\')
) `

func planningText(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "%", `\%`)
	s = strings.ReplaceAll(s, "_", `\_`)
	return "%" + s + "%"
}
func (s *Store) Wanted(ctx context.Context, f PlanningFilter, now time.Time) (PlanningPage, error) {
	return s.planning(ctx, f, now, false)
}
func (s *Store) Calendar(ctx context.Context, f PlanningFilter, now time.Time) (PlanningPage, error) {
	return s.planning(ctx, f, now, true)
}
func (s *Store) planning(ctx context.Context, f PlanningFilter, now time.Time, calendar bool) (PlanningPage, error) {
	if len(f.Query) > 200 || !utf8.ValidString(f.Query) || strings.ContainsFunc(f.Query, unicode.IsControl) {
		return PlanningPage{}, ErrPlanningInput
	}
	f.Query = strings.TrimSpace(f.Query)
	if f.Kind == "" {
		f.Kind = "all"
	}
	if f.Status == "" {
		f.Status = "missing"
		if calendar {
			f.Status = "all"
		}
	}
	if f.Release == "" {
		f.Release = "all"
	}
	if f.Page == 0 {
		f.Page = 1
	}
	validStatus := f.Status == "all" || f.Status == "missing" || f.Status == "upcoming" || f.Status == "undated" || f.Status == "downloading" || f.Status == "attention" || (calendar && f.Status == "available")
	validRelease := f.Release == "all" || (calendar && (f.Release == "theatrical" || f.Release == "digital" || f.Release == "physical"))
	if !validStatus || !validRelease || (f.Kind != "all" && f.Kind != "movie" && f.Kind != "series") || f.Page < 1 || f.Page > 100000 || len(f.Query) > 200 || !utf8.ValidString(f.Query) || strings.ContainsFunc(f.Query, unicode.IsControl) || (!calendar && f.Month != "") {
		return PlanningPage{}, ErrPlanningInput
	}
	monthEnd := ""
	if calendar {
		if f.Month == "" {
			f.Month = now.Format("2006-01")
		}
		month, e := time.Parse("2006-01", f.Month)
		if e != nil || len(f.Month) != 7 || month.Year() < 1 || month.Year() > 9999 {
			return PlanningPage{}, ErrPlanningInput
		}
		monthEnd = month.AddDate(0, 1, -1).Format(time.DateOnly)
	}
	out := PlanningPage{Items: []PlanningEntry{}, Page: f.Page, Limit: 50, AsOfDate: now.Format(time.DateOnly), Month: f.Month, Kind: f.Kind, Status: f.Status, Query: f.Query, OnlyMonitored: f.OnlyMonitored}
	match := planningText(f.Query)
	calendarValue := 0
	if calendar {
		calendarValue = 1
	}
	args := []any{out.AsOfDate, calendarValue, f.Kind, f.Kind, f.OnlyMonitored, match, match}
	scope := ` WHERE available=0`
	if calendar {
		scope = ` WHERE date>=? AND date<=? AND (?='all' OR release_kind=?)`
		args = append(args, f.Month+"-01", monthEnd, f.Release, f.Release)
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	rows, e := tx.QueryContext(ctx, planningCTE+`SELECT status,COUNT(*) FROM filtered`+scope+` GROUP BY status`, args...)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		var status string
		var n int
		if e = rows.Scan(&status, &n); e != nil {
			rows.Close()
			return out, e
		}
		switch status {
		case "missing":
			out.Counts.Missing = n
		case "upcoming":
			out.Counts.Upcoming = n
		case "undated":
			out.Counts.Undated = n
		case "downloading":
			out.Counts.Downloading = n
		case "attention":
			out.Counts.Attention = n
		case "available":
			out.Counts.Available = n
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	if e = tx.QueryRowContext(ctx, planningCTE+`SELECT COUNT(*) FROM filtered WHERE status='undated'`, args[:7]...).Scan(&out.Coverage.Undated); e != nil {
		return out, e
	}
	if f.Kind != "movie" {
		e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM seasons s JOIN media_items m ON m.id=s.series_id WHERE s.loaded_at='' AND (?=0 OR (m.monitored=1 AND s.monitored=1)) AND m.title LIKE ? ESCAPE '\'`, f.OnlyMonitored, match).Scan(&out.Coverage.UnloadedSeasons)
		if e != nil {
			return out, e
		}
	}
	selected := scope + ` AND (?='all' OR status=?)`
	selectedArgs := append(append([]any{}, args...), f.Status, f.Status)
	if e = tx.QueryRowContext(ctx, planningCTE+`SELECT COUNT(*) FROM filtered`+selected, selectedArgs...).Scan(&out.Total); e != nil {
		return out, e
	}
	selectedArgs = append(selectedArgs, out.Limit, (out.Page-1)*out.Limit)
	rows, e = tx.QueryContext(ctx, planningCTE+`SELECT kind,media_id,episode_id,title,year,episode_title,season,episode,date,release_kind,theatrical_date,digital_date,physical_date,monitored,monitoring_reason,available,status,download_id,download_state,protocol,poster_path,movie_version,overview FROM filtered`+selected+` ORDER BY date='',date,release_kind,`+TitleSort+` COLLATE NOCASE,media_id,COALESCE(episode_id,0),movie_version LIMIT ? OFFSET ?`, selectedArgs...)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		var v PlanningEntry
		var poster string
		if e = rows.Scan(&v.Kind, &v.MediaID, &v.EpisodeID, &v.Title, &v.Year, &v.EpisodeTitle, &v.Season, &v.Episode, &v.Date, &v.ReleaseKind, &v.TheatricalDate, &v.DigitalDate, &v.PhysicalDate, &v.Monitored, &v.MonitoringReason, &v.Available, &v.Status, &v.DownloadID, &v.DownloadState, &v.Protocol, &poster, &v.MovieVersion, &v.Overview); e != nil {
			rows.Close()
			return out, e
		}
		v.PosterURL = metadata.PosterURL(poster)
		out.Items = append(out.Items, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	for i := range out.Items {
		v := &out.Items[i]
		if e = tx.QueryRowContext(ctx, `SELECT CASE WHEN ? IS NOT NULL THEN (SELECT runtime_minutes FROM episodes WHERE id=?) ELSE (SELECT runtime_minutes FROM media_items WHERE id=?) END,COALESCE((SELECT series_status FROM series_details WHERE media_id=?),''),MAX(COALESCE((SELECT last_success FROM metadata_refresh_state WHERE media_id=?),0),COALESCE((SELECT updated_at FROM media_enrichment WHERE media_id=?),0))`, v.EpisodeID, v.EpisodeID, v.MediaID, v.MediaID, v.MediaID, v.MediaID).Scan(&v.RuntimeMinutes, &v.SeriesStatus, &v.MetadataUpdatedAt); e != nil {
			return out, e
		}
	}
	return out, tx.Commit()
}
