package music

import (
	"context"
	"errors"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/database"
)

func TestPlaylistMonitorStartsPausedAndUsesRevisions(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	service := NewService(db, t.TempDir())
	settings, err := service.MonitorSettings(ctx)
	if err != nil || settings.Enabled || settings.IntervalHours != 24 {
		t.Fatal("default monitor settings", settings, err)
	}
	if worked, err := service.ProcessMonitor(ctx); err != nil || worked {
		t.Fatal("paused monitor ran", worked, err)
	}
	settings, err = service.SaveMonitorSettings(ctx, true, 12, settings.Revision)
	if err != nil || !settings.Enabled || settings.IntervalHours != 12 {
		t.Fatal("enable monitor", settings, err)
	}
	if _, err := service.SaveMonitorSettings(ctx, false, 24, 1); !errors.Is(err, ErrPlanChanged) {
		t.Fatal("stale monitor revision accepted", err)
	}
	sources, err := service.ListMonitorSources(ctx)
	if err != nil || len(sources.Items) != 0 {
		t.Fatal("default sources", sources, err)
	}
}
