package automation

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"

	"github.com/faaaaq/ReelScarey/internal/library"
	"github.com/faaaaq/ReelScarey/internal/metadata"
	"github.com/faaaaq/ReelScarey/internal/transfer"
)

type MetadataProvider interface {
	Movie(context.Context, int64) (metadata.Movie, error)
	Series(context.Context, int64) (metadata.Series, error)
	Season(context.Context, int64, int) (metadata.Season, error)
	Information(context.Context, string, int64) (metadata.Information, error)
}
type MetadataRefresher struct {
	db       *sql.DB
	provider MetadataProvider
}

func NewMetadataRefresher(db *sql.DB, p MetadataProvider) *MetadataRefresher {
	return &MetadataRefresher{db: db, provider: p}
}

type MetadataSchedule struct {
	Enabled       bool   `json:"enabled"`
	IntervalHours int    `json:"interval_hours"`
	Batch         int    `json:"batch_size"`
	OnlyMonitored bool   `json:"only_monitored"`
	Revision      int    `json:"revision"`
	Requested     bool   `json:"requested"`
	NextRun       int64  `json:"next_run"`
	LastRun       int64  `json:"last_run"`
	State         string `json:"state"`
	Message       string `json:"message"`
	Cursor        int64  `json:"cursor"`
}

const metadataScheduleColumns = `enabled,interval_hours,batch_size,only_monitored,revision,requested,next_run,last_run,state,message,cursor`

func scanMetadataSchedule(row interface{ Scan(...any) error }) (MetadataSchedule, error) {
	var p MetadataSchedule
	e := row.Scan(&p.Enabled, &p.IntervalHours, &p.Batch, &p.OnlyMonitored, &p.Revision, &p.Requested, &p.NextRun, &p.LastRun, &p.State, &p.Message, &p.Cursor)
	return p, e
}
func (s *MetadataRefresher) Get(ctx context.Context) (MetadataSchedule, error) {
	return scanMetadataSchedule(s.db.QueryRowContext(ctx, `SELECT `+metadataScheduleColumns+` FROM metadata_refresh_schedule WHERE id=1`))
}
func (s *MetadataRefresher) Save(ctx context.Context, p MetadataSchedule) (MetadataSchedule, error) {
	if p.IntervalHours < 1 || p.IntervalHours > 720 || p.Batch < 1 || p.Batch > 25 {
		return p, invalid("Choose 1–720 hours and 1–25 titles per batch.")
	}
	r, e := s.db.ExecContext(ctx, `UPDATE metadata_refresh_schedule SET enabled=?,interval_hours=?,batch_size=?,only_monitored=?,revision=revision+1,requested=0,next_run=?,cursor=0,lease_owner='',lease_until=0,state=CASE WHEN ? THEN 'scheduled' ELSE 'paused' END,message='Metadata schedule saved.' WHERE id=1 AND revision=?`, p.Enabled, p.IntervalHours, p.Batch, p.OnlyMonitored, time.Now().Unix()+int64(p.IntervalHours)*3600, p.Enabled, p.Revision)
	if e != nil {
		return p, e
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return p, ErrLease
	}
	return s.Get(ctx)
}
func (s *MetadataRefresher) Request(ctx context.Context) error {
	r, e := s.db.ExecContext(ctx, `UPDATE metadata_refresh_schedule SET requested=1,next_run=0,state='queued',message='Metadata refresh requested.' WHERE id=1 AND lease_until<unixepoch() AND requested=0`)
	if e != nil {
		return e
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return ErrLease
	}
	return nil
}
func (s *MetadataRefresher) Run(ctx context.Context, logger *slog.Logger) {
	for ctx.Err() == nil {
		_, e := s.ProcessNext(ctx)
		if e != nil && ctx.Err() == nil {
			logger.Warn("metadata refresh deferred", "error", e)
		}
		timer := time.NewTimer(30 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
func (s *MetadataRefresher) ProcessNext(ctx context.Context) (bool, error) {
	owner := transfer.ID()
	now := time.Now().Unix()
	p, e := scanMetadataSchedule(s.db.QueryRowContext(ctx, `UPDATE metadata_refresh_schedule SET lease_owner=?,lease_until=?,state='running' WHERE id=1 AND lease_until<? AND (requested=1 OR (enabled=1 AND next_run<=?)) RETURNING `+metadataScheduleColumns, owner, now+60, now, now))
	if errors.Is(e, sql.ErrNoRows) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	work, cancel := context.WithTimeout(ctx, 8*time.Minute)
	done := make(chan struct{})
	go func() {
		defer close(done)
		tick := time.NewTicker(15 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-work.Done():
				return
			case <-tick.C:
				r, e := s.db.ExecContext(work, `UPDATE metadata_refresh_schedule SET lease_until=? WHERE id=1 AND lease_owner=? AND revision=?`, time.Now().Unix()+60, owner, p.Revision)
				if e != nil {
					cancel()
					return
				}
				n, _ := r.RowsAffected()
				if n != 1 {
					cancel()
					return
				}
			}
		}
	}()
	defer func() { cancel(); <-done }()
	rows, e := s.db.QueryContext(work, `SELECT id FROM media_items WHERE tmdb_id>0 AND id>? AND (?=0 OR monitored=1) ORDER BY id LIMIT ?`, p.Cursor, p.OnlyMonitored, p.Batch)
	if e != nil {
		return true, e
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if e = rows.Scan(&id); e != nil {
			break
		}
		ids = append(ids, id)
	}
	e = errors.Join(e, rows.Err(), rows.Close())
	if e != nil {
		return true, e
	}
	state, message := "ready", "Metadata pass finished; monitoring and files preserved."
	for _, id := range ids {
		if e = work.Err(); e != nil {
			break
		}
		e = s.refresh(work, id, p, owner)
		if e != nil {
			state = "attention"
			message = "Some metadata could not refresh. Check the provider connection; saved data was preserved."
			var provider *metadata.Error
			if errors.As(e, &provider) && provider.Code == "tmdb_not_configured" {
				state = "waiting_provider"
				message = "Connect TMDb to refresh metadata."
				break
			}
			_, _ = s.db.ExecContext(work, `INSERT INTO metadata_refresh_state(media_id,state,message) VALUES(?,'attention','Refresh deferred; saved metadata retained.') ON CONFLICT(media_id) DO UPDATE SET state=excluded.state,message=excluded.message`, id)
		}
		p.Cursor = id
	}
	next := time.Now().Unix() + int64(p.IntervalHours)*3600
	more := len(ids) == p.Batch && state != "waiting_provider"
	if more {
		next = time.Now().Unix() + 30
		message = "Metadata batch finished; continuing with the next titles."
	} else {
		p.Cursor = 0
	}
	if state == "waiting_provider" || state == "attention" {
		next = time.Now().Unix() + 3600
	}
	finish, c := context.WithTimeout(context.Background(), 5*time.Second)
	defer c()
	_, saveErr := s.db.ExecContext(finish, `UPDATE metadata_refresh_schedule SET cursor=?,next_run=?,requested=CASE WHEN ? THEN requested ELSE 0 END,last_run=?,state=?,message=?,lease_owner='',lease_until=0 WHERE id=1 AND lease_owner=? AND revision=?`, p.Cursor, next, more, time.Now().Unix(), state, message, owner, p.Revision)
	return true, saveErr
}
func (s *MetadataRefresher) refresh(ctx context.Context, id int64, p MetadataSchedule, owner string) error {
	store := library.New(s.db)
	item, e := store.Item(ctx, id)
	if e != nil {
		return e
	}
	fresh := library.MetadataRefresh{SeasonCursor: -1}
	revision := int64(0)
	if item.Kind == "movie" {
		fresh.Movie, e = s.provider.Movie(ctx, item.TMDbID)
	} else {
		show, e := store.TVShow(ctx, id)
		if e != nil {
			return e
		}
		revision = show.Revision
		series, e := s.provider.Series(ctx, item.TMDbID)
		if e != nil {
			return e
		}
		fresh.Movie = series.Movie
		fresh.Series = &series
		cursor := -1
		_ = s.db.QueryRowContext(ctx, `SELECT season_cursor FROM metadata_refresh_state WHERE media_id=?`, id).Scan(&cursor)
		eligible := []int{}
		for _, v := range show.Seasons {
			if v.LoadedAt != "" || (v.Monitored && v.AirDate != "" && v.AirDate <= time.Now().AddDate(0, 0, 90).UTC().Format(time.DateOnly)) {
				eligible = append(eligible, v.Number)
			}
		}
		for pass := 0; pass < 2 && len(fresh.Seasons) < min(5, len(eligible)); pass++ {
			for _, n := range eligible {
				if (pass == 0 && n <= cursor) || (pass == 1 && n > cursor) {
					continue
				}
				if len(fresh.Seasons) >= min(5, len(eligible)) {
					break
				}
				season, e := s.provider.Season(ctx, item.TMDbID, n)
				if e != nil {
					return e
				}
				if season.Number != n {
					return library.ErrRevision
				}
				fresh.Seasons = append(fresh.Seasons, season)
				fresh.SeasonCursor = n
			}
		}
	}
	if e != nil {
		return e
	}
	fresh.Information, e = s.provider.Information(ctx, item.Kind, item.TMDbID)
	if e != nil {
		return e
	}
	return store.ApplyMetadataRefresh(ctx, item, revision, fresh, func(tx *sql.Tx) error {
		var n int
		e := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM metadata_refresh_schedule WHERE id=1 AND lease_owner=? AND revision=? AND lease_until>=? AND (enabled=1 OR requested=1)`, owner, p.Revision, time.Now().Unix()).Scan(&n)
		if e != nil {
			return e
		}
		if n != 1 {
			return ErrLease
		}
		return nil
	})
}
