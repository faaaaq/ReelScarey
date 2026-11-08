package transfer

import (
	"context"
	"io"
	"log/slog"
	"math"
	"strings"
	"testing"
)

func saveSharePolicy(t *testing.T, f *workflowFixture, in SharePolicyUpdate) SharePolicy {
	t.Helper()
	if in.Revision == 0 {
		in.Revision = 1
	}
	out, err := f.s.SaveSharePolicy(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func shareEventCount(t *testing.T, f *workflowFixture, hash, state string) int {
	t.Helper()
	var count int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM activity_events WHERE kind='download' AND object_id=? AND state=?`, hash, state).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestSharePolicyDefaultsAndRevisionGuard(t *testing.T) {
	ctx := context.Background()
	f := setupWorkflow(t)
	p, err := f.s.SharePolicy(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if p.Enabled || p.Action != shareActionPause || p.MaxRatio != 2 || p.MaxSeedingMinutes != 0 || p.IncludeReelscarey || p.Revision != 1 || p.UpdatedAt < 1 {
		t.Fatal(p)
	}
	for name, in := range map[string]SharePolicyUpdate{
		"action":          {Action: "delete", MaxRatio: 1, Revision: 1},
		"low ratio":       {Action: shareActionPause, MaxRatio: 0.05, Revision: 1},
		"high ratio":      {Action: shareActionPause, MaxRatio: 1001, Revision: 1},
		"nan ratio":       {Action: shareActionPause, MaxRatio: math.NaN(), Revision: 1},
		"inf ratio":       {Action: shareActionPause, MaxRatio: math.Inf(1), Revision: 1},
		"negative time":   {Action: shareActionPause, MaxSeedingMinutes: -1, Revision: 1},
		"too much time":   {Action: shareActionPause, MaxSeedingMinutes: maxSharePolicyMinutes + 1, Revision: 1},
		"enabled no rule": {Enabled: true, Action: shareActionPause, Revision: 1},
		"no revision":     {Action: shareActionPause, MaxRatio: 1},
	} {
		if _, err := f.s.SaveSharePolicy(ctx, in); problemCode(err) != "invalid_selection" && problemCode(err) != "revision_conflict" {
			t.Fatalf("%s: %v", name, err)
		}
	}
	saved := saveSharePolicy(t, f, SharePolicyUpdate{Enabled: true, Action: shareActionRemove, MaxRatio: 0, MaxSeedingMinutes: 90, IncludeReelscarey: true, Revision: 1})
	if !saved.Enabled || saved.Action != shareActionRemove || saved.MaxRatio != 0 || saved.MaxSeedingMinutes != 90 || !saved.IncludeReelscarey || saved.Revision != 2 || saved.UpdatedAt < 1 {
		t.Fatal(saved)
	}
	if _, err := f.s.SaveSharePolicy(ctx, SharePolicyUpdate{Action: shareActionPause, MaxRatio: 1, Revision: 1}); problemCode(err) != "revision_conflict" {
		t.Fatal("stale revision accepted", err)
	}
}

func TestSharePolicyRatioPausesOnceAndRecordsHistory(t *testing.T) {
	ctx := context.Background()
	f := setupWorkflow(t)
	enableFixtureBuiltin(t, f)
	saveSharePolicy(t, f, SharePolicyUpdate{Enabled: true, Action: shareActionPause, MaxRatio: 2, Revision: 1})
	f.client.list = []Torrent{{Hash: testHash, Name: "Seeded.Manual", State: "uploading", Progress: 1, AmountLeft: 0, Ratio: 2.5, Tags: "reelscarey-manual"}}
	run, err := f.s.RunSharePolicy(ctx)
	if err != nil || run.Checked != 1 || run.Acted != 1 {
		t.Fatal(run, err)
	}
	if f.client.action != "pause" || len(f.client.lastHashes) != 1 || f.client.lastHashes[0] != testHash {
		t.Fatal(f.client.action, f.client.lastHashes)
	}
	if got := shareEventCount(t, f, testHash, "seeding limit reached: pause"); got != 1 {
		t.Fatal("history entries", got)
	}
	// A repeated pass never records a second action for the same hash.
	f.client.action = ""
	run, err = f.s.RunSharePolicy(ctx)
	if err != nil || run.Acted != 0 || f.client.action != "" {
		t.Fatal(run, err, f.client.action)
	}
	if got := shareEventCount(t, f, testHash, "seeding limit reached: pause"); got != 1 {
		t.Fatal("duplicate history entries", got)
	}
}

func TestSharePolicyTimeRuleRemovesTorrentWithoutFiles(t *testing.T) {
	ctx := context.Background()
	f := setupWorkflow(t)
	enableFixtureBuiltin(t, f)
	saveSharePolicy(t, f, SharePolicyUpdate{Enabled: true, Action: shareActionRemove, MaxSeedingMinutes: 60, Revision: 1})
	f.client.list = []Torrent{
		{Hash: testHash, Name: "Below.Limit", State: "uploading", Progress: 1, AmountLeft: 0, Ratio: 0.5, SeedingTime: 3599, Tags: "linux"},
		{Hash: otherHash, Name: "Above.Limit", State: "uploading", Progress: 1, AmountLeft: 0, Ratio: 0.5, SeedingTime: 3600, Tags: "linux"},
	}
	run, err := f.s.RunSharePolicy(ctx)
	if err != nil || run.Checked != 1 || run.Acted != 1 {
		t.Fatal(run, err)
	}
	if f.client.action != "remove" || f.client.deleteFiles || len(f.client.lastHashes) != 1 || f.client.lastHashes[0] != otherHash {
		t.Fatal(f.client.action, f.client.deleteFiles, f.client.lastHashes)
	}
	if got := shareEventCount(t, f, otherHash, "seeding limit reached: remove"); got != 1 {
		t.Fatal(got)
	}
}

func TestSharePolicyNeverTouchesActiveDownloads(t *testing.T) {
	ctx := context.Background()
	f := setupWorkflow(t)
	enableFixtureBuiltin(t, f)
	saveSharePolicy(t, f, SharePolicyUpdate{Enabled: true, Action: shareActionPause, MaxRatio: 1, Revision: 1})
	f.client.list = []Torrent{
		{Hash: testHash, Name: "Still.Downloading", State: "downloading", Progress: 0.9, AmountLeft: 10, Ratio: 9, Tags: "linux"},
		{Hash: otherHash, Name: "Leftover.Bytes", State: "uploading", Progress: 1, AmountLeft: 5, Ratio: 9, Tags: "linux"},
	}
	run, err := f.s.RunSharePolicy(ctx)
	if err != nil || run.Checked != 0 || run.Acted != 0 || f.client.action != "" {
		t.Fatal(run, err, f.client.action)
	}
}

func TestSharePolicyExcludesJobTorrentsAndActiveJobs(t *testing.T) {
	ctx := context.Background()
	f := setupWorkflow(t)
	enableFixtureBuiltin(t, f)
	saveSharePolicy(t, f, SharePolicyUpdate{Enabled: true, Action: shareActionPause, MaxRatio: 1, Revision: 1})
	f.client.list = []Torrent{{Hash: testHash, Name: "Job.Source", State: "uploading", Progress: 1, AmountLeft: 0, Ratio: 3, Tags: "reelscarey-0123456789abcdef0123456789abcdef"}}
	if run, err := f.s.RunSharePolicy(ctx); err != nil || run.Acted != 0 || f.client.action != "" {
		t.Fatal("job torrent acted on", run, err, f.client.action)
	}
	// Including ReelScarey downloads pauses a completed job's seeding copy.
	policy, err := f.s.SharePolicy(ctx)
	if err != nil {
		t.Fatal(err)
	}
	saveSharePolicy(t, f, SharePolicyUpdate{Enabled: true, Action: shareActionPause, MaxRatio: 1, IncludeReelscarey: true, Revision: policy.Revision})
	if run, err := f.s.RunSharePolicy(ctx); err != nil || run.Acted != 1 || f.client.action != "pause" {
		t.Fatal(run, err, f.client.action)
	}
}

func TestSharePolicySkipsAnUnfinishedDownloadJob(t *testing.T) {
	ctx := context.Background()
	f := setupWorkflow(t)
	job := f.queue(t)
	enableFixtureBuiltin(t, f)
	saveSharePolicy(t, f, SharePolicyUpdate{Enabled: true, Action: shareActionPause, MaxRatio: 1, IncludeReelscarey: true, Revision: 1})
	f.client.list = []Torrent{{Hash: job.Hash, Name: job.Title, State: "uploading", Progress: 1, AmountLeft: 0, Ratio: 5, Tags: "reelscarey-" + job.ID}}
	if run, err := f.s.RunSharePolicy(ctx); err != nil || run.Checked != 0 || run.Acted != 0 || f.client.action != "" {
		t.Fatal("unfinished job was acted on", run, err, f.client.action)
	}
}

func TestSharePolicyProtectsMusicAwaitingImport(t *testing.T) {
	ctx := context.Background()
	f := setupWorkflow(t)
	job := musicTestQueue(t, f)
	enableFixtureBuiltin(t, f)
	saveSharePolicy(t, f, SharePolicyUpdate{Enabled: true, Action: shareActionRemoveAndDelete, MaxRatio: 1, IncludeReelscarey: true, Revision: 1})
	f.client.list = []Torrent{{Hash: job.InfoHash, Name: job.Title, State: "uploading", Progress: 1, Ratio: 5, Tags: "reelscarey-" + job.ID}}
	if _, err := f.db.Exec(`UPDATE music_downloads SET state='importing' WHERE id=?`, job.ID); err != nil {
		t.Fatal(err)
	}
	if run, err := f.s.RunSharePolicy(ctx); err != nil || run.Acted != 0 || f.client.action != "" {
		t.Fatal("music source was removed before import", run, err, f.client.action)
	}
}

func TestSharePolicyRemoveAndDeleteDegradesForJobTorrents(t *testing.T) {
	ctx := context.Background()
	f := setupWorkflow(t)
	enableFixtureBuiltin(t, f)
	saveSharePolicy(t, f, SharePolicyUpdate{Enabled: true, Action: shareActionRemoveAndDelete, MaxRatio: 1, IncludeReelscarey: true, Revision: 1})
	f.client.list = []Torrent{{Hash: testHash, Name: "Job.Source", State: "uploading", Progress: 1, AmountLeft: 0, Ratio: 3, Tags: "reelscarey-0123456789abcdef0123456789abcdef"}}
	if run, err := f.s.RunSharePolicy(ctx); err != nil || run.Acted != 1 {
		t.Fatal(run, err)
	}
	if f.client.action != "remove" || f.client.deleteFiles {
		t.Fatal("job data would have been deleted", f.client.action, f.client.deleteFiles)
	}
	if got := shareEventCount(t, f, testHash, "seeding limit reached: remove"); got != 1 {
		t.Fatal("degraded action was not recorded", got)
	}

	g := setupWorkflow(t)
	enableFixtureBuiltin(t, g)
	saveSharePolicy(t, g, SharePolicyUpdate{Enabled: true, Action: shareActionRemoveAndDelete, MaxRatio: 1, Revision: 1})
	g.client.list = []Torrent{{Hash: testHash, Name: "Foreign.Release", State: "uploading", Progress: 1, AmountLeft: 0, Ratio: 3, Tags: "linux"}}
	if run, err := g.s.RunSharePolicy(ctx); err != nil || run.Acted != 1 {
		t.Fatal(run, err)
	}
	if g.client.action != "remove" || !g.client.deleteFiles {
		t.Fatal("external data was not deleted", g.client.action, g.client.deleteFiles)
	}
}

func TestSharePolicyLeavesPausedAndDisabledTorrentsAlone(t *testing.T) {
	ctx := context.Background()
	f := setupWorkflow(t)
	enableFixtureBuiltin(t, f)
	// Disabled policy: nothing happens even far above the defaults.
	f.client.list = []Torrent{{Hash: testHash, Name: "Paused", State: "pausedUP", Progress: 1, AmountLeft: 0, Ratio: 9, Tags: "linux"}}
	if run, err := f.s.RunSharePolicy(ctx); err != nil || run.Acted != 0 || f.client.action != "" {
		t.Fatal(run, err, f.client.action)
	}
	saveSharePolicy(t, f, SharePolicyUpdate{Enabled: true, Action: shareActionPause, MaxRatio: 1, Revision: 1})
	f.client.list = []Torrent{
		{Hash: testHash, Name: "Paused", State: "pausedUP", Progress: 1, AmountLeft: 0, Ratio: 9, Tags: "linux"},
		{Hash: otherHash, Name: "Complete", State: "uploading", Progress: 1, AmountLeft: 0, Ratio: 1, Tags: "linux"},
	}
	run, err := f.s.RunSharePolicy(ctx)
	if err != nil || run.Checked != 1 || run.Acted != 1 || len(f.client.lastHashes) != 1 || f.client.lastHashes[0] != otherHash {
		t.Fatal(run, err, f.client.lastHashes)
	}
}

func TestSharePolicyWithoutBuiltinClientIsQuiet(t *testing.T) {
	ctx := context.Background()
	f := setupWorkflow(t)
	saveSharePolicy(t, f, SharePolicyUpdate{Enabled: true, Action: shareActionPause, MaxRatio: 1, Revision: 1})
	if _, err := f.s.RunSharePolicy(ctx); problemCode(err) != "builtin_unavailable" {
		t.Fatal("manual run did not report the disabled client", err)
	}
	if run, err := f.s.evaluateSharePolicy(ctx, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil || run.Checked != 0 || run.Acted != 0 {
		t.Fatal("supervised pass was not quiet", run, err)
	}
}

func TestShareLimitEvaluationUsesClientValues(t *testing.T) {
	on := SharePolicy{Enabled: true, Action: shareActionPause, MaxRatio: 2, MaxSeedingMinutes: 60}
	for name, tc := range map[string]struct {
		torrent Torrent
		policy  SharePolicy
		want    bool
	}{
		"disabled":       {Torrent{Ratio: 99, SeedingTime: 999999}, SharePolicy{Enabled: false, MaxRatio: 1, MaxSeedingMinutes: 1}, false},
		"ratio met":      {Torrent{Ratio: 2, SeedingTime: 0}, on, true},
		"ratio below":    {Torrent{Ratio: 1.99, SeedingTime: 0}, on, false},
		"time met":       {Torrent{Ratio: 0, SeedingTime: 3600}, on, true},
		"time below":     {Torrent{Ratio: 0, SeedingTime: 3599}, on, false},
		"ratio disabled": {Torrent{Ratio: 99}, SharePolicy{Enabled: true, MaxSeedingMinutes: 0}, false},
	} {
		if got := shareLimitReached(tc.torrent, tc.policy); got != tc.want {
			t.Errorf("%s: %v", name, got)
		}
	}
	job := Torrent{Origin: torrentOriginJob}
	if got := effectiveShareAction(job, shareActionRemoveAndDelete); got != shareActionRemove {
		t.Error(got)
	}
	if got := effectiveShareAction(Torrent{Origin: torrentOriginOther}, shareActionRemoveAndDelete); got != shareActionRemoveAndDelete {
		t.Error(got)
	}
	if !strings.HasPrefix(shareEventState(shareActionPause), "seeding limit reached: ") {
		t.Fatal(shareEventState(shareActionPause))
	}
	if !shareHashOK(testHash) || !shareHashOK(strings.Repeat("a", 64)) || shareHashOK("bad|hash") || shareHashOK(strings.Repeat("g", 40)) {
		t.Fatal("info hash validation")
	}
}
