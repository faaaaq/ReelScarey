package emby

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type Monitor struct {
	db      *sql.DB
	service *Service
}

var ErrMonitorConflict = errors.New("Monitoring settings changed. Reload before saving.")

func NewMonitor(db *sql.DB, s *Service) *Monitor { return &Monitor{db: db, service: s} }

type MonitorStatus struct {
	Enabled     bool   `json:"enabled"`
	Revision    int    `json:"revision"`
	LastSuccess int64  `json:"last_success"`
	Status      string `json:"status"`
}
type Play struct {
	ID            int64  `json:"id"`
	User          string `json:"user"`
	Title         string `json:"title"`
	Series        string `json:"series"`
	Kind          string `json:"kind"`
	Started       int64  `json:"started_at"`
	LastSeen      int64  `json:"last_seen"`
	Ended         *int64 `json:"ended_at"`
	State         string `json:"state"`
	Paused        bool   `json:"paused"`
	PositionTicks int64  `json:"position_ticks"`
}

func (m *Monitor) Status(ctx context.Context) (MonitorStatus, error) {
	var v MonitorStatus
	e := m.db.QueryRowContext(ctx, `SELECT enabled,revision,last_success,status FROM emby_monitor WHERE id=1`).Scan(&v.Enabled, &v.Revision, &v.LastSuccess, &v.Status)
	return v, e
}
func (m *Monitor) Set(ctx context.Context, enabled bool, revision int) (MonitorStatus, error) {
	tx, e := m.db.BeginTx(ctx, nil)
	if e != nil {
		return MonitorStatus{}, e
	}
	defer tx.Rollback()
	if enabled {
		var n int
		if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM integration_configs WHERE name='emby'`).Scan(&n); e != nil {
			return MonitorStatus{}, e
		}
		if n != 1 {
			return MonitorStatus{}, ErrConfig
		}
	}
	res, e := tx.ExecContext(ctx, `UPDATE emby_monitor SET enabled=?,revision=revision+1,status=?,last_success=0,config_fingerprint='' WHERE id=1 AND revision=?`, enabled, map[bool]string{true: "waiting", false: "paused"}[enabled], revision)
	if e != nil {
		return MonitorStatus{}, e
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return MonitorStatus{}, ErrMonitorConflict
	}
	if _, e = tx.ExecContext(ctx, `UPDATE emby_play_history SET state='uncertain',ended_at=last_seen WHERE state='active'`); e != nil {
		return MonitorStatus{}, e
	}
	if e = tx.Commit(); e != nil {
		return MonitorStatus{}, e
	}
	return m.Status(ctx)
}
func (m *Monitor) History(ctx context.Context) ([]Play, error) {
	rows, e := m.db.QueryContext(ctx, `SELECT id,username,title,series,kind,started_at,last_seen,ended_at,state,paused,position_ticks FROM emby_play_history ORDER BY started_at DESC,id DESC LIMIT 200`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Play{}
	for rows.Next() {
		var p Play
		if e = rows.Scan(&p.ID, &p.User, &p.Title, &p.Series, &p.Kind, &p.Started, &p.LastSeen, &p.Ended, &p.State, &p.Paused, &p.PositionTicks); e != nil {
			return nil, e
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

type session struct {
	ID   string `json:"Id"`
	User string `json:"UserName"`
	Item *struct {
		ID     string `json:"Id"`
		Name   string `json:"Name"`
		Series string `json:"SeriesName"`
		Kind   string `json:"Type"`
	} `json:"NowPlayingItem"`
	PlayState struct {
		Paused   bool  `json:"IsPaused"`
		Position int64 `json:"PositionTicks"`
	} `json:"PlayState"`
}

func (m *Monitor) Poll(ctx context.Context) error {
	status, e := m.Status(ctx)
	if e != nil || !status.Enabled {
		return e
	}
	var sealed []byte
	e = m.db.QueryRowContext(ctx, `SELECT ciphertext FROM integration_configs WHERE name='emby'`).Scan(&sealed)
	if e != nil {
		m.failure(ctx, status.Revision)
		return ErrUnavailable
	}
	raw, e := m.service.secrets.Open("config:emby", sealed)
	if e != nil {
		return e
	}
	var c Config
	if json.Unmarshal(raw, &c) != nil || c.APIKey == "" {
		m.failure(ctx, status.Revision)
		return ErrUnavailable
	}
	sum := sha256.Sum256(sealed)
	fingerprint := hex.EncodeToString(sum[:])
	var sessions []session
	if e = m.service.get(ctx, c, "/Sessions", &sessions); e != nil {
		m.failure(ctx, status.Revision)
		return e
	}
	if sessions == nil || len(sessions) > 200 {
		m.failure(ctx, status.Revision)
		return ErrUnavailable
	}
	seen := map[string]session{}
	for _, v := range sessions {
		if v.Item == nil || v.Item.ID == "" {
			continue
		}
		if len(v.ID) == 0 || len(v.ID) > 160 || len(v.Item.ID) > 160 || len(v.User) > 200 || len(v.Item.Name) > 500 || len(v.Item.Series) > 500 || len(v.Item.Kind) > 80 || v.PlayState.Position < 0 {
			m.failure(ctx, status.Revision)
			return ErrUnavailable
		}
		if _, ok := seen[v.ID]; ok {
			m.failure(ctx, status.Revision)
			return ErrUnavailable
		}
		seen[v.ID] = v
	}
	tx, e := m.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var revision int
	var enabled bool
	var previous int64
	var oldFingerprint string
	var current []byte
	if e = tx.QueryRowContext(ctx, `SELECT enabled,revision,last_success,config_fingerprint FROM emby_monitor WHERE id=1`).Scan(&enabled, &revision, &previous, &oldFingerprint); e != nil {
		return e
	}
	if !enabled || revision != status.Revision {
		return nil
	}
	if e = tx.QueryRowContext(ctx, `SELECT ciphertext FROM integration_configs WHERE name='emby'`).Scan(&current); e != nil || !equalBytes(current, sealed) {
		return nil
	}
	now := time.Now().Unix()
	if previous > 0 && (now-previous > 90 || oldFingerprint != fingerprint || status.Status == "unavailable") {
		if _, e = tx.ExecContext(ctx, `UPDATE emby_play_history SET state='uncertain',ended_at=last_seen WHERE state='active'`); e != nil {
			return e
		}
	}
	rows, e := tx.QueryContext(ctx, `SELECT session_id,item_id FROM emby_play_history WHERE state='active'`)
	if e != nil {
		return e
	}
	active := map[string]string{}
	for rows.Next() {
		var id, item string
		if e = rows.Scan(&id, &item); e != nil {
			rows.Close()
			return e
		}
		active[id] = item
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for id, item := range active {
		v, ok := seen[id]
		if !ok || v.Item.ID != item {
			if _, e = tx.ExecContext(ctx, `UPDATE emby_play_history SET state='ended',ended_at=last_seen WHERE session_id=? AND state='active'`, id); e != nil {
				return e
			}
		}
	}
	for id, v := range seen {
		if active[id] == v.Item.ID {
			_, e = tx.ExecContext(ctx, `UPDATE emby_play_history SET last_seen=?,paused=?,position_ticks=? WHERE session_id=? AND state='active'`, now, v.PlayState.Paused, v.PlayState.Position, id)
		} else {
			_, e = tx.ExecContext(ctx, `INSERT INTO emby_play_history(session_id,item_id,username,title,series,kind,started_at,last_seen,state,paused,position_ticks) VALUES(?,?,?,?,?,?,?,?,'active',?,?)`, id, v.Item.ID, strings.TrimSpace(v.User), strings.TrimSpace(v.Item.Name), strings.TrimSpace(v.Item.Series), v.Item.Kind, now, now, v.PlayState.Paused, v.PlayState.Position)
		}
		if e != nil {
			return e
		}
	}
	if _, e = tx.ExecContext(ctx, `UPDATE emby_monitor SET last_success=?,status='connected',config_fingerprint=? WHERE id=1`, now, fingerprint); e != nil {
		return e
	}
	return tx.Commit()
}
func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
func (m *Monitor) failure(ctx context.Context, revision int) {
	_, _ = m.db.ExecContext(ctx, `UPDATE emby_monitor SET status='unavailable' WHERE id=1 AND enabled=1 AND revision=?`, revision)
}
func (m *Monitor) Run(ctx context.Context) {
	// A restart cannot establish what happened during downtime.
	_, _ = m.db.ExecContext(ctx, `UPDATE emby_play_history SET state='uncertain',ended_at=last_seen WHERE state='active'`)
	_, _ = m.db.ExecContext(ctx, `UPDATE emby_monitor SET last_success=0,status='waiting' WHERE id=1 AND enabled=1`)
	for ctx.Err() == nil {
		work, cancel := context.WithTimeout(ctx, 20*time.Second)
		_ = m.Poll(work)
		cancel()
		timer := time.NewTimer(30 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
