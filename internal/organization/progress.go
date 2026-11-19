package organization

import (
	"context"
	"encoding/json"
	"time"
)

type WorkProgress struct {
	File       string `json:"file,omitempty"`
	Stage      string `json:"stage"`
	BytesDone  int64  `json:"bytes_done,omitempty"`
	BytesTotal int64  `json:"bytes_total,omitempty"`
	StartedAt  int64  `json:"started_at"`
	UpdatedAt  int64  `json:"updated_at"`
}
type progressKey struct{}
type progressReporter func(string, int64, int64)

func reportWork(ctx context.Context, stage string, done, total int64) {
	if report, ok := ctx.Value(progressKey{}).(progressReporter); ok {
		report(stage, done, total)
	}
}
func (s *Service) withWorkProgress(ctx context.Context, p *Plan) context.Context {
	file := ""
	if p.TrackCleanup != nil && p.TrackCleanup.Processed < len(p.TrackCleanup.Targets) {
		file = p.TrackCleanup.Targets[p.TrackCleanup.Processed].Path
	} else if p.Replacement != nil {
		file = p.Replacement.Original.Source
	}
	started := time.Now().Unix()
	last := time.Time{}
	stageBefore := ""
	report := progressReporter(func(stage string, done, total int64) {
		now := time.Now()
		if stage == stageBefore && done != total && now.Sub(last) < time.Second {
			return
		}
		last = now
		stageBefore = stage
		p.Progress = &WorkProgress{File: file, Stage: stage, BytesDone: done, BytesTotal: total, StartedAt: started, UpdatedAt: now.Unix()}
		raw, err := json.Marshal(p.Progress)
		if err != nil {
			return
		}
		// Telemetry never changes the journal phase or durable owner stop state.
		updateCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
		defer cancel()
		_, _ = s.db.ExecContext(updateCtx, `UPDATE organization_plans SET snapshot=json_set(snapshot,'$.progress',json(?)),updated_at=unixepoch() WHERE id=?`, string(raw), p.ID)
	})
	ctx = context.WithValue(ctx, progressKey{}, report)
	reportWork(ctx, "Inspecting file", 0, 0)
	return ctx
}
