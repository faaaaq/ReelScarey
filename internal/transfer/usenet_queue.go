package transfer

import (
	"context"
	"errors"
	"math"
	"net/url"
	"strconv"
	"strings"
)

const (
	maxUsenetQueue     = 1000
	maxUsenetQueuePick = 500
	// Usenet speed limits are bytes per second. SABnzbd interprets bare values
	// 1-100 as a percentage, so a byte limit must stay above that range.
	minUsenetSpeedLimit = 1024
	maxUsenetSpeedLimit = 2000000000
)

type sabQueueSlot struct {
	ID         string   `json:"nzo_id"`
	Filename   string   `json:"filename"`
	Name       string   `json:"name"`
	Status     string   `json:"status"`
	MB         string   `json:"mb"`
	MBLeft     string   `json:"mbleft"`
	Percentage string   `json:"percentage"`
	TimeLeft   string   `json:"timeleft"`
	Category   string   `json:"cat"`
	Priority   string   `json:"priority"`
	Labels     []string `json:"labels"`
}

type sabQueuePage struct {
	Status         string         `json:"status"`
	Paused         bool           `json:"paused"`
	PausedAll      bool           `json:"paused_all"`
	KBPerSec       string         `json:"kbpersec"`
	MB             string         `json:"mb"`
	MBLeft         string         `json:"mbleft"`
	TimeLeft       string         `json:"timeleft"`
	SpeedLimit     string         `json:"speedlimit"`
	SpeedLimitAbs  string         `json:"speedlimit_abs"`
	NoOfSlots      int            `json:"noofslots"`
	NoOfSlotsTotal int            `json:"noofslots_total"`
	Slots          []sabQueueSlot `json:"slots"`
}

// UsenetQueueItem is one queued SABnzbd job. SABnzbd downloads one job at a
// time, so only the active slot carries the global current rate.
type UsenetQueueItem struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Size      int64   `json:"size"`
	Remaining int64   `json:"remaining"`
	Progress  float64 `json:"progress"`
	Status    string  `json:"status"`
	Speed     int64   `json:"speed"`
	ETA       string  `json:"eta"`
	Category  string  `json:"category"`
	Priority  string  `json:"priority"`
}

// UsenetQueue is the read-only projection shown by the Usenet page. Available
// is false with an honest message when the daemon is disabled, unconfigured or
// not answering; it is not an error response.
type UsenetQueue struct {
	Available    bool              `json:"available"`
	State        string            `json:"state"`
	Message      string            `json:"message,omitempty"`
	Paused       bool              `json:"paused"`
	Speed        int64             `json:"speed"`
	Remaining    int64             `json:"remaining"`
	ETA          string            `json:"eta"`
	LimitBytes   int64             `json:"limit_bytes"`
	LimitPercent int               `json:"limit_percent"`
	Items        []UsenetQueueItem `json:"items"`
}

// UsenetOverview is everything the page polls: the supervised lifecycle, the
// safe provider settings and the queue. It never contains a secret.
type UsenetOverview struct {
	Status UsenetBundledStatus `json:"status"`
	Queue  UsenetQueue         `json:"queue"`
}

// UsenetQueueAction is one reviewed bulk action for up to 500 SABnzbd jobs.
type UsenetQueueAction struct {
	IDs         []string
	Action      string
	DeleteFiles bool
	Reviewed    bool
}

// UsenetSpeedLimitUpdate changes the SABnzbd download limit in bytes per
// second; zero removes the limit.
type UsenetSpeedLimitUpdate struct {
	DownloadLimit *int64 `json:"download_limit"`
}

// ListQueue reads the bounded SABnzbd queue. Values are parsed defensively:
// malformed progress or sizes are refused rather than displayed as facts.
func (s *SABnzbd) ListQueue(ctx context.Context, c ClientConfig) (UsenetQueue, error) {
	out := UsenetQueue{Items: []UsenetQueueItem{}}
	var v struct {
		Queue sabQueuePage `json:"queue"`
	}
	if e := s.api(ctx, c, url.Values{"mode": {"queue"}, "limit": {strconv.Itoa(maxUsenetQueue)}, "start": {"0"}}, nil, &v); e != nil {
		return out, e
	}
	q := v.Queue
	if len(q.Slots) > maxUsenetQueue || q.NoOfSlots > maxUsenetQueue || q.NoOfSlotsTotal > maxUsenetQueue {
		return out, problem("client_response", "SABnzbd reported more than 1000 queued jobs. Reduce its queue before using this page.")
	}
	speed, ok := parseSABFloat(q.KBPerSec)
	if !ok || speed < 0 || speed > 2<<30 {
		return out, problem("client_response", "SABnzbd returned an invalid download rate.")
	}
	size, ok := parseSABFloat(q.MB)
	if !ok || size < 0 || size > 2<<20 {
		return out, problem("client_response", "SABnzbd returned an invalid queue size.")
	}
	left, ok := parseSABFloat(q.MBLeft)
	if !ok || left < 0 || left > size+1 {
		return out, problem("client_response", "SABnzbd returned an invalid remaining size.")
	}
	limit, ok := parseSABFloat(q.SpeedLimitAbs)
	if !ok || limit < 0 || limit > maxUsenetSpeedLimit {
		return out, problem("client_response", "SABnzbd returned an invalid speed limit.")
	}
	percent, ok := parseSABFloat(q.SpeedLimit)
	if !ok || percent < 0 || percent > 100 {
		return out, problem("client_response", "SABnzbd returned an invalid speed limit percentage.")
	}
	out.Paused = q.Paused || q.PausedAll
	out.Speed = int64(speed * (1 << 10)) // SABnzbd reports kbpersec in KiB/s
	out.Remaining = int64(left * (1 << 20))
	out.ETA = cleanClientText(q.TimeLeft, 40)
	out.LimitBytes = int64(limit)
	out.LimitPercent = int(percent)
	out.State = cleanClientText(q.Status, 40)
	if out.State == "" {
		out.State = "Idle"
	}
	for _, slot := range q.Slots {
		if !sabID.MatchString(slot.ID) {
			return out, problem("client_response", "SABnzbd returned an invalid job identity.")
		}
		name := slot.Filename
		if name == "" {
			name = slot.Name
		}
		name = cleanClientText(name, 500)
		if name == "" {
			return out, problem("client_response", "SABnzbd returned a queued job without a name.")
		}
		mb, ok := parseSABFloat(slot.MB)
		if !ok || mb < 0 || mb > 2<<20 {
			return out, problem("client_response", "SABnzbd returned an invalid job size.")
		}
		leftMB, ok := parseSABFloat(slot.MBLeft)
		if !ok || leftMB < 0 || leftMB > mb+1 {
			return out, problem("client_response", "SABnzbd returned an invalid remaining job size.")
		}
		pct, ok := parseSABFloat(slot.Percentage)
		if !ok || pct < 0 || pct > 100 {
			return out, problem("client_response", "SABnzbd returned invalid job progress.")
		}
		progress := pct / 100
		itemSize := int64(mb * (1 << 20))
		itemLeft := int64(leftMB * (1 << 20))
		if itemSize > 0 && (pct == 0 || itemLeft > 0) {
			progress = math.Max(0, math.Min(1, float64(itemSize-itemLeft)/float64(itemSize)))
		}
		if slot.Status == "" {
			return out, problem("client_response", "SABnzbd returned a queued job without a status.")
		}
		itemSpeed := int64(0)
		if strings.EqualFold(slot.Status, "Downloading") || strings.EqualFold(slot.Status, "Fetching") {
			itemSpeed = out.Speed
		}
		out.Items = append(out.Items, UsenetQueueItem{
			ID:        slot.ID,
			Name:      name,
			Size:      itemSize,
			Remaining: itemLeft,
			Progress:  progress,
			Status:    cleanClientText(slot.Status, 40),
			Speed:     itemSpeed,
			ETA:       cleanClientText(slot.TimeLeft, 40),
			Category:  cleanClientText(slot.Category, 100),
			Priority:  cleanClientText(slot.Priority, 40),
		})
	}
	return out, nil
}

// parseSABFloat accepts the numeric strings SABnzbd emits and rejects NaN,
// infinities and empty fields instead of coercing them to zero.
func parseSABFloat(v string) (float64, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, true
	}
	f, e := strconv.ParseFloat(v, 64)
	if e != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, false
	}
	return f, true
}

func (s *SABnzbd) queueAction(ctx context.Context, c ClientConfig, name string, ids []string, deleteFiles bool) error {
	values := url.Values{"mode": {"queue"}, "name": {name}, "value": {strings.Join(ids, ",")}}
	if name == "delete" && deleteFiles {
		values.Set("del_files", "1")
	}
	var out struct {
		Status bool     `json:"status"`
		IDs    []string `json:"nzo_ids"`
	}
	return s.api(ctx, c, values, nil, &out)
}

func (s *SABnzbd) PauseQueue(ctx context.Context, c ClientConfig, ids []string) error {
	return s.queueAction(ctx, c, "pause", ids, false)
}

func (s *SABnzbd) ResumeQueue(ctx context.Context, c ClientConfig, ids []string) error {
	return s.queueAction(ctx, c, "resume", ids, false)
}

// DeleteQueue removes jobs. del_files=1 also removes the job's downloaded
// bytes in SABnzbd's incomplete folder; ReelScarey never touches completed
// output itself.
func (s *SABnzbd) DeleteQueue(ctx context.Context, c ClientConfig, ids []string, deleteFiles bool) error {
	return s.queueAction(ctx, c, "delete", ids, deleteFiles)
}

// SetQueueSpeedLimit sets an absolute bytes-per-second limit. Zero omits the
// value so SABnzbd removes the limit instead of interpreting an ambiguous
// percentage; SABnzbd's own saved percentage is not changed.
func (s *SABnzbd) SetQueueSpeedLimit(ctx context.Context, c ClientConfig, bytesPerSecond int64) error {
	values := url.Values{"mode": {"config"}, "name": {"speedlimit"}}
	if bytesPerSecond > 0 {
		values.Set("value", strconv.FormatInt(bytesPerSecond, 10))
	}
	var out map[string]any
	return s.api(ctx, c, values, nil, &out)
}

// builtinUsenetClient resolves the supervised daemon only when the owner has
// enabled it and a news server is saved. Queue routes never fall back to an
// external SABnzbd or NZBGet connection.
func (s *Service) builtinUsenetClient(ctx context.Context) (ClientConfig, error) {
	present, enabled, _, e := s.bundledUsenetRow(ctx)
	if e != nil {
		return ClientConfig{}, e
	}
	if !present || !enabled {
		return ClientConfig{}, problem("builtin_unavailable", "The built-in Usenet client is not enabled. Save a news server and turn it on first.")
	}
	c, e := s.managedClientConfig(ctx, builtinUsenetClientID)
	if e != nil {
		return ClientConfig{}, e
	}
	if !newsConfigured(c) {
		return ClientConfig{}, problem("client_not_configured", "The built-in Usenet client has no news server. Save one first.")
	}
	return c, nil
}

// UsenetQueue returns the daemon queue. A disabled or unreachable daemon is
// reported as unavailable with a message, not as a failed request.
func (s *Service) UsenetQueue(ctx context.Context) (UsenetQueue, error) {
	out := UsenetQueue{State: "disabled", Items: []UsenetQueueItem{}}
	present, enabled, _, e := s.bundledUsenetRow(ctx)
	if e != nil {
		return out, e
	}
	if !present {
		out.Message = "The built-in Usenet client is not configured. Save a news server first."
		return out, nil
	}
	if !enabled {
		out.Message = "The built-in Usenet client is not enabled. Turn it on after saving a news server."
		return out, nil
	}
	c, e := s.managedClientConfig(ctx, builtinUsenetClientID)
	if e != nil {
		out.State = "failed"
		var p *Error
		if errors.As(e, &p) {
			out.Message = p.Message
		} else {
			out.Message = "The built-in Usenet client configuration could not be read."
		}
		return out, nil
	}
	if !newsConfigured(c) {
		out.State = "unconfigured"
		out.Message = "The built-in Usenet client has no news server. Save one before using it."
		return out, nil
	}
	if state, message := s.usenet.snapshot(); state != "running" {
		out.State = state
		out.Message = message
		if out.Message == "" {
			out.Message = "The built-in Usenet client is not running."
		}
		return out, nil
	}
	q, e := s.sab.ListQueue(ctx, c)
	if e != nil {
		out.State = "unavailable"
		var p *Error
		if errors.As(e, &p) {
			out.Message = p.Message
		} else {
			out.Message = "SABnzbd did not return its queue."
		}
		return out, nil
	}
	q.Available = true
	return q, nil
}

// UsenetOverview is the page's single polling read.
func (s *Service) UsenetOverview(ctx context.Context) (UsenetOverview, error) {
	status, e := s.BundledUsenetStatus(ctx)
	if e != nil {
		return UsenetOverview{}, e
	}
	queue, e := s.UsenetQueue(ctx)
	if e != nil {
		return UsenetOverview{}, e
	}
	return UsenetOverview{Status: status, Queue: queue}, nil
}

func normalizeSABIDs(in []string) ([]string, error) {
	if len(in) == 0 || len(in) > maxUsenetQueuePick {
		return nil, problem("invalid_selection", "Choose between 1 and 500 queued jobs.")
	}
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, id := range in {
		if !sabID.MatchString(id) {
			return nil, problem("invalid_selection", "Choose valid SABnzbd jobs.")
		}
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out, nil
}

// UsenetQueueAction applies one reviewed action to the supervised queue.
// Deleting a job that an unfinished ReelScarey download still tracks is
// refused so the page cannot silently break an import; use Activity instead.
func (s *Service) UsenetQueueAction(ctx context.Context, in UsenetQueueAction) error {
	ids, e := normalizeSABIDs(in.IDs)
	if e != nil {
		return e
	}
	switch in.Action {
	case "pause", "resume":
	case "delete":
		if !in.Reviewed {
			return problem("review_required", "Review the queue deletion before removing these jobs.")
		}
	default:
		return problem("invalid_selection", "Choose pause, resume or delete.")
	}
	c, e := s.builtinUsenetClient(ctx)
	if e != nil {
		return e
	}
	if in.Action == "delete" {
		tracked, e := s.activeUsenetJobs(ctx, ids)
		if e != nil {
			return e
		}
		if tracked {
			return problem("active_downloads", "One or more selected jobs belong to an unfinished ReelScarey download. Stop or finish that download from Activity first.")
		}
		return s.sab.DeleteQueue(ctx, c, ids, in.DeleteFiles)
	}
	if in.Action == "pause" {
		return s.sab.PauseQueue(ctx, c, ids)
	}
	return s.sab.ResumeQueue(ctx, c, ids)
}

func (s *Service) activeUsenetJobs(ctx context.Context, ids []string) (bool, error) {
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}
	var count int
	e := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM downloads WHERE protocol='usenet' AND state NOT IN ('completed','cancelled') AND client_id IN (`+placeholders+`)`, args...).Scan(&count)
	return count > 0, e
}

// AddManualNZB accepts only a bounded uploaded NZB. It uses the same dedicated
// category and unique job-name rule as tracked downloads, but it is not linked
// to a library job and is never imported automatically.
//
// Submission is idempotent by content: the NZB checksum is claimed in a fenced
// transaction before the client submission, so a repeated upload of the same
// NZB returns the friendly duplicate problem instead of enqueueing a second
// identical job. A failed submission releases the claim so a retry stays
// possible, and stale claims older than a day are purged so a re-upload after
// the queue job has run its course is not blocked forever.
func (s *Service) AddManualNZB(ctx context.Context, nzb []byte) (string, error) {
	identity, e := NZBIdentity(nzb)
	if e != nil {
		return "", e
	}
	c, e := s.builtinUsenetClient(ctx)
	if e != nil {
		return "", e
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return "", e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `DELETE FROM usenet_manual_nzbs WHERE created_at<unixepoch()-86400`); e != nil {
		return "", e
	}
	result, e := tx.ExecContext(ctx, `INSERT INTO usenet_manual_nzbs(nzb_identity,created_at) VALUES(?,unixepoch()) ON CONFLICT(nzb_identity) DO NOTHING`, identity)
	if e != nil {
		return "", e
	}
	claimed, e := result.RowsAffected()
	if e != nil {
		return "", e
	}
	if claimed != 1 {
		return "", problem("duplicate_nzb", "This NZB was already submitted recently and may still be in the built-in Usenet client's queue. Check the Usenet page before submitting it again.")
	}
	if e = tx.Commit(); e != nil {
		return "", e
	}
	id, e := s.sab.Add(ctx, c, "reelscarey-manual-"+identity, nzb)
	if e != nil {
		// The submission was not accepted; release the identity claim so an
		// explicit retry can submit again.
		_, _ = s.db.ExecContext(context.WithoutCancel(ctx), `DELETE FROM usenet_manual_nzbs WHERE nzb_identity=?`, identity)
		return "", e
	}
	return id, nil
}

func (s *Service) SetUsenetSpeedLimit(ctx context.Context, in UsenetSpeedLimitUpdate) error {
	if in.DownloadLimit == nil {
		return problem("invalid_selection", "Enter a download limit in bytes per second, or 0 for no limit.")
	}
	limit := *in.DownloadLimit
	if limit != 0 && (limit < minUsenetSpeedLimit || limit > maxUsenetSpeedLimit) {
		return problem("invalid_selection", "Enter a limit of 0 (no limit) or between 1024 and 2000000000 bytes per second.")
	}
	c, e := s.builtinUsenetClient(ctx)
	if e != nil {
		return e
	}
	return s.sab.SetQueueSpeedLimit(ctx, c, limit)
}
