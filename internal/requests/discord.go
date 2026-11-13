package requests

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/faaaaq/ReelScarey/internal/securestore"
)

var webhookPath = regexp.MustCompile(`^/api/webhooks/[0-9]{16,22}/[A-Za-z0-9_-]{40,160}$`)
var ErrWebhook = errors.New("Enter a Discord webhook URL from discord.com.")

type Discord struct {
	db      *sql.DB
	secrets *securestore.Store
	client  *http.Client
}

func NewDiscord(db *sql.DB, secrets *securestore.Store) *Discord {
	return &Discord{db: db, secrets: secrets, client: &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirects disabled") }}}
}
func validWebhook(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host == "discord.com" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && u.RawPath == "" && webhookPath.MatchString(u.Path)
}
func (d *Discord) Save(ctx context.Context, account int64, raw string) error {
	if account < 1 || !validWebhook(raw) {
		return ErrWebhook
	}
	sealed, err := d.secrets.Seal(fmt.Sprintf("discord:%d", account), []byte(raw))
	if err != nil {
		return err
	}
	result, err := d.db.ExecContext(ctx, `INSERT INTO request_webhooks(account_id,ciphertext,updated_at) SELECT id,?,? FROM request_accounts WHERE id=? AND disabled=0 ON CONFLICT(account_id) DO UPDATE SET ciphertext=excluded.ciphertext,updated_at=excluded.updated_at`, sealed, time.Now().Unix(), account)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return sql.ErrNoRows
	}
	return nil
}
func (d *Discord) Status(ctx context.Context, account int64) (bool, error) {
	var n int
	err := d.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM request_webhooks WHERE account_id=?`, account).Scan(&n)
	return n > 0, err
}
func (d *Discord) Delete(ctx context.Context, account int64) error {
	_, err := d.db.ExecContext(ctx, `DELETE FROM request_webhooks WHERE account_id=?`, account)
	return err
}
func (d *Discord) Retry(ctx context.Context, account, id int64) error {
	result, err := d.db.ExecContext(ctx, `UPDATE request_notifications SET state='pending',next_attempt=0,updated_at=unixepoch() WHERE request_id=? AND state='uncertain' AND EXISTS(SELECT 1 FROM media_requests r JOIN request_accounts a ON a.id=r.account_id JOIN request_webhooks w ON w.account_id=a.id WHERE r.id=request_notifications.request_id AND a.id=? AND a.disabled=0)`, id, account)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrState
	}
	return nil
}

// Interrupted sends are ambiguous: Discord may have received them. They are
// marked uncertain instead of silently posting a duplicate on restart.
func (d *Discord) Recover(ctx context.Context) error {
	_, err := d.db.ExecContext(ctx, `UPDATE request_notifications SET state='uncertain',updated_at=unixepoch() WHERE state='sending'`)
	return err
}

func (d *Discord) Tick(ctx context.Context) (bool, error) {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	// A series is complete only when all monitored, non-special seasons are
	// loaded and every monitored episode has a registered file.
	_, err = tx.ExecContext(ctx, `INSERT INTO request_notifications(request_id,state,updated_at)
 SELECT r.id,'pending',unixepoch() FROM media_requests r JOIN request_webhooks w ON w.account_id=r.account_id JOIN request_accounts a ON a.id=r.account_id AND a.disabled=0 JOIN media_items m ON m.kind=r.kind AND m.tmdb_id=r.tmdb_id
 WHERE r.state='approved' AND ((r.kind='movie' AND EXISTS(SELECT 1 FROM media_files f WHERE f.movie_id=m.id)) OR (r.kind='series'
 AND EXISTS(SELECT 1 FROM seasons s JOIN episodes e ON e.season_id=s.id WHERE s.series_id=m.id AND s.number>0 AND s.monitored=1 AND e.monitored=1)
 AND NOT EXISTS(SELECT 1 FROM seasons s WHERE s.series_id=m.id AND s.number>0 AND s.monitored=1 AND (s.loaded_at='' OR s.episode_count<>(SELECT COUNT(*) FROM episodes e WHERE e.season_id=s.id)))
 AND NOT EXISTS(SELECT 1 FROM seasons s JOIN episodes e ON e.season_id=s.id WHERE s.series_id=m.id AND s.number>0 AND s.monitored=1 AND e.monitored=1 AND NOT EXISTS(SELECT 1 FROM episode_files ef WHERE ef.episode_id=e.id)))) ON CONFLICT(request_id) DO NOTHING`)
	if err != nil {
		return false, err
	}
	var id, account int64
	var title string
	var sealed []byte
	err = tx.QueryRowContext(ctx, `SELECT n.request_id,r.account_id,r.title,w.ciphertext FROM request_notifications n JOIN media_requests r ON r.id=n.request_id JOIN request_accounts a ON a.id=r.account_id AND a.disabled=0 JOIN request_webhooks w ON w.account_id=r.account_id WHERE n.state='pending' AND n.next_attempt<=unixepoch() ORDER BY n.updated_at,n.request_id LIMIT 1`).Scan(&id, &account, &title, &sealed)
	if errors.Is(err, sql.ErrNoRows) {
		return false, tx.Commit()
	}
	if err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE request_notifications SET state='sending',updated_at=unixepoch() WHERE request_id=? AND state='pending'`, id); err != nil {
		return false, err
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	plain, err := d.secrets.Open(fmt.Sprintf("discord:%d", account), sealed)
	if err != nil || !validWebhook(string(plain)) {
		_ = d.finish(ctx, id, "uncertain", 0)
		return true, errors.New("Discord notification settings could not be read.")
	}
	body, _ := json.Marshal(map[string]any{"content": "Your request is available: " + title, "allowed_mentions": map[string]any{"parse": []string{}}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, string(plain), bytes.NewReader(body))
	if err != nil {
		_ = d.finish(ctx, id, "uncertain", 0)
		return true, errors.New("Discord notification could not be prepared.")
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := d.client.Do(req)
	if err != nil {
		_ = d.finish(context.WithoutCancel(ctx), id, "uncertain", 0)
		return true, errors.New("Discord notification result is uncertain.")
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	response.Body.Close()
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return true, d.finish(ctx, id, "sent", 0)
	}
	if response.StatusCode == 429 {
		return true, d.finish(context.WithoutCancel(ctx), id, "pending", time.Now().Add(5*time.Minute).Unix())
	}
	return true, d.finish(ctx, id, "uncertain", 0)
}
func (d *Discord) finish(ctx context.Context, id int64, state string, next int64) error {
	_, err := d.db.ExecContext(ctx, `UPDATE request_notifications SET state=?,next_attempt=?,updated_at=unixepoch() WHERE request_id=? AND state='sending'`, state, next, id)
	return err
}
func (d *Discord) Run(ctx context.Context) {
	if err := d.Recover(ctx); err != nil {
		return
	}
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		for ctx.Err() == nil {
			did, err := d.Tick(ctx)
			if err != nil || !did {
				break
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Keep direct user input strictly to the canonical HTTPS webhook shape.
func ValidWebhook(raw string) bool { return raw == strings.TrimSpace(raw) && validWebhook(raw) }
