package backups

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/faaaaq/ReelScarey/internal/config"
	"github.com/faaaaq/ReelScarey/internal/instance"
	"github.com/faaaaq/ReelScarey/internal/storage"
)

var ErrBusy = errors.New("another backup operation is running")
var ErrStale = errors.New("Backup settings changed. Refresh this page before saving again.")
var ErrChanged = errors.New("The reviewed backup changed. Review it again before restoring.")
var ErrConfirmation = errors.New("Type RESTORE to confirm replacement of the configuration and library database.")

type Service struct {
	db           *sql.DB
	cfg          config.Config
	dir, version string
	op           sync.Mutex
	jobs         chan string
	restart      func()
	restoring    atomic.Bool
}
type Schedule struct {
	Enabled      bool   `json:"enabled"`
	IntervalDays int    `json:"interval_days"`
	KeepCount    int    `json:"keep_count"`
	KeepDays     int    `json:"keep_days"`
	NextRun      int64  `json:"next_run"`
	LastAttempt  int64  `json:"last_attempt"`
	LastSuccess  int64  `json:"last_success"`
	LastError    string `json:"last_error"`
	Running      bool   `json:"running"`
	Revision     int    `json:"revision"`
}
type Status struct {
	Restoring       bool      `json:"restoring"`
	Schedule        Schedule  `json:"schedule"`
	Archives        []Archive `json:"archives"`
	Directory       string    `json:"directory"`
	MaxUploadBytes  int64     `json:"max_upload_bytes"`
	RecoveryPending bool      `json:"recovery_pending"`
	RestoreMessage  string    `json:"restore_message"`
}

func New(ctx context.Context, db *sql.DB, cfg config.Config, version string, restart func()) (*Service, error) {
	dir, e := backupDir(cfg)
	if e != nil {
		return nil, e
	}
	s := &Service{db: db, cfg: cfg, dir: dir, version: version, jobs: make(chan string, 1), restart: restart}
	_, e = db.ExecContext(ctx, `UPDATE backup_schedule SET running=0,last_error='Backup interrupted by application restart; the previous archives were preserved.' WHERE running=1`)
	return s, e
}
func (s *Service) Restoring() bool { return s.restoring.Load() }
func (s *Service) Schedule(ctx context.Context) (Schedule, error) {
	var p Schedule
	e := s.db.QueryRowContext(ctx, `SELECT enabled,interval_days,keep_count,keep_days,next_run,last_attempt,last_success,last_error,running,revision FROM backup_schedule WHERE id=1`).Scan(&p.Enabled, &p.IntervalDays, &p.KeepCount, &p.KeepDays, &p.NextRun, &p.LastAttempt, &p.LastSuccess, &p.LastError, &p.Running, &p.Revision)
	return p, e
}
func (s *Service) Status(ctx context.Context) (Status, error) {
	p, e := s.Schedule(ctx)
	if e != nil {
		return Status{}, e
	}
	list, e := s.List()
	message := ""
	if root, e := os.OpenRoot(s.cfg.ConfigDir); e == nil {
		if f, e := storage.Regular(root, "restore-result.json", 1024); e == nil {
			b, e := io.ReadAll(io.LimitReader(f, 1025))
			f.Close()
			var r struct {
				Message string `json:"message"`
			}
			if e == nil && jsonDecode(b, &r) == nil {
				message = r.Message
			}
		}
		root.Close()
	}
	return Status{Restoring: s.Restoring(), Schedule: p, Archives: list, Directory: s.dir, MaxUploadBytes: MaxArchive, RecoveryPending: instance.RecoveryPending(s.cfg.ConfigDir), RestoreMessage: message}, e
}
func (s *Service) Save(ctx context.Context, p Schedule) error {
	if !s.op.TryLock() {
		return ErrBusy
	}
	defer s.op.Unlock()
	if p.IntervalDays < 1 || p.IntervalDays > 30 || p.KeepCount < 1 || p.KeepCount > 100 || p.KeepDays < 1 || p.KeepDays > 365 {
		return errors.New("choose an interval of 1–30 days, 1–100 retained backups and 1–365 retention days")
	}
	next := int64(0)
	if p.Enabled {
		next = time.Now().Add(time.Duration(p.IntervalDays) * 24 * time.Hour).Unix()
	}
	r, e := s.db.ExecContext(ctx, `UPDATE backup_schedule SET enabled=?,interval_days=?,keep_count=?,keep_days=?,next_run=CASE WHEN enabled=1 AND ?=1 AND interval_days=? AND next_run>0 THEN next_run ELSE ? END,revision=revision+1 WHERE id=1 AND revision=?`, p.Enabled, p.IntervalDays, p.KeepCount, p.KeepDays, p.Enabled, p.IntervalDays, next, p.Revision)
	if e != nil {
		return e
	}
	n, e := r.RowsAffected()
	if e == nil && n != 1 {
		return ErrStale
	}
	return e
}
func (s *Service) Start(ctx context.Context) error { return s.start(ctx, "manual") }
func (s *Service) start(ctx context.Context, kind string) error {
	if !s.op.TryLock() {
		return ErrBusy
	}
	_, e := s.db.ExecContext(ctx, `UPDATE backup_schedule SET running=1,last_attempt=?,last_error='' WHERE id=1`, time.Now().Unix())
	if e != nil {
		s.op.Unlock()
		return e
	}
	s.jobs <- kind
	return nil
}
func (s *Service) Run(ctx context.Context, logger *slog.Logger) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	s.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case kind := <-s.jobs:
			job, cancel := context.WithTimeout(ctx, 10*time.Minute)
			_, e := s.create(job, kind)
			if e == nil && kind == "scheduled" {
				e = s.retain(job)
			}
			cancel()
			update, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			message := ""
			if e != nil {
				message = "Backup did not finish. Existing archives were preserved; check storage space and permissions."
				if ctx.Err() == nil {
					logger.Warn("backup needs attention", "error", e)
				}
			}
			_, saveErr := s.db.ExecContext(update, `UPDATE backup_schedule SET running=0,last_error=?,last_success=CASE WHEN ?='' THEN ? ELSE last_success END,next_run=CASE WHEN enabled=1 THEN ?+CASE WHEN ?='' THEN interval_days*86400 ELSE 3600 END ELSE 0 END WHERE id=1`, message, message, time.Now().Unix(), time.Now().Unix(), message)
			cancel()
			if saveErr != nil {
				logger.Warn("backup status could not be saved", "error", saveErr)
			}
			s.op.Unlock()
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

func (s *Service) tick(ctx context.Context) {
	if instance.RecoveryPending(s.cfg.ConfigDir) {
		return
	}
	p, e := s.Schedule(ctx)
	if e == nil && p.Enabled && p.NextRun <= time.Now().Unix() {
		_ = s.start(ctx, "scheduled")
	}
}
