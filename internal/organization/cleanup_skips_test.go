package organization

import (
	"context"
	"encoding/json"
	"github.com/faaaaq/ReelScarey/internal/database"
	"os"
	"testing"
)

func saveCleanupFixture(t *testing.T, f reviewFixture, p Plan) {
	t.Helper()
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.db.Exec(`INSERT INTO organization_plans(id,state,snapshot,created_at,updated_at) VALUES(?,?,?,?,?)`, p.ID, p.State, string(raw), p.Created, p.Updated)
	if err != nil {
		t.Fatal(err)
	}
}
func skippedFixture() Plan {
	return Plan{ID: "original", State: "running", Created: 100, Updated: 200, TrackCleanup: &TrackCleanup{Request: TrackCleanupRequest{Mode: "other_subtitles", Action: "disable"}, Targets: []cleanupTarget{{ID: 1, Path: "Movies/one.mkv", Languages: []string{"en"}}}, Processed: 1, Skipped: 1, SkipAttempts: 1, SkipHistory: []CleanupSkipAttempt{{FileID: 1, Path: "Movies/one.mkv", Message: "Old verifier rejected the edit", OccurredAt: 150000}}}}
}
func retryFixture() Plan {
	return Plan{ID: "retry", State: "completed", Created: 201, Updated: 202, TrackCleanup: &TrackCleanup{Request: TrackCleanupRequest{Mode: "other_subtitles", Action: "disable"}, Targets: []cleanupTarget{{ID: 1, Path: "Movies/one.mkv", Languages: []string{"eng", "en"}}}, Processed: 1}}
}
func TestCleanupSkipResolutionRequiresMatchingSuccessfulRetry(t *testing.T) {
	for _, test := range []struct {
		name       string
		change     func(*Plan)
		unresolved int
	}{
		{"success", func(*Plan) {}, 0},
		{"failed retry", func(p *Plan) { p.TrackCleanup.Skipped = 1 }, 1},
		{"not processed", func(p *Plan) { p.TrackCleanup.Processed = 0 }, 1},
		{"different action", func(p *Plan) { p.TrackCleanup.Request.Action = "remove" }, 1},
		{"different mode", func(p *Plan) { p.TrackCleanup.Request.Mode = "all_subtitles" }, 1},
		{"different file", func(p *Plan) { p.TrackCleanup.Targets[0].ID = 2 }, 1},
		{"different path", func(p *Plan) { p.TrackCleanup.Targets[0].Path = "Movies/new.mkv" }, 1},
		{"different languages", func(p *Plan) { p.TrackCleanup.Targets[0].Languages = []string{"ja"} }, 1},
		{"older success", func(p *Plan) { p.Updated = 149 }, 1},
		{"legacy all-success batch", func(p *Plan) {
			p.TrackCleanup.Targets = append(p.TrackCleanup.Targets, cleanupTarget{ID: 2})
			p.TrackCleanup.Processed = 2
		}, 0},
		{"ambiguous legacy batch", func(p *Plan) {
			p.TrackCleanup.Targets = append(p.TrackCleanup.Targets, cleanupTarget{ID: 2})
			p.TrackCleanup.Processed = 2
			p.TrackCleanup.Skipped = 1
		}, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newReviewFixture(t)
			source := skippedFixture()
			retry := retryFixture()
			test.change(&retry)
			saveCleanupFixture(t, f, source)
			saveCleanupFixture(t, f, retry)
			for range 2 {
				got, err := f.s.Plan(context.Background(), source.ID)
				if err != nil {
					t.Fatal(err)
				}
				if got.TrackCleanup.Skipped != test.unresolved || got.TrackCleanup.SkipAttempts != 1 || len(got.TrackCleanup.SkipHistory) != 1 {
					t.Fatalf("wrong counters %+v", got.TrackCleanup)
				}
				if test.unresolved == 0 && (got.TrackCleanup.SkipHistory[0].ResolvedBy != "retry" || got.TrackCleanup.Recovered != 1) {
					t.Fatal(got.TrackCleanup)
				}
			}
			var raw string
			if err := f.db.QueryRow(`SELECT snapshot FROM organization_plans WHERE id='original'`).Scan(&raw); err != nil {
				t.Fatal(err)
			}
			var stored Plan
			json.Unmarshal([]byte(raw), &stored)
			if stored.TrackCleanup.Skipped != 1 || stored.TrackCleanup.SkipHistory[0].ResolvedBy != "" {
				t.Fatal("read projection overwrote a running worker snapshot")
			}
			active, _, err := f.s.Active(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if active.Skipped != test.unresolved || active.SkipAttempts != 1 {
				t.Fatal(active)
			}
		})
	}
}
func TestCleanupSkipCountsFilesButKeepsEveryAttempt(t *testing.T) {
	f := newReviewFixture(t)
	p := Plan{TrackCleanup: &TrackCleanup{Targets: []cleanupTarget{{ID: 1, Path: "one"}}}}
	for range 2 {
		f.s.noteCleanupIssue(&p, p.TrackCleanup.Targets[0], problem("invalid_media", "Failed"))
	}
	if p.TrackCleanup.Skipped != 1 || p.TrackCleanup.SkipAttempts != 2 || len(p.TrackCleanup.SkipHistory) != 2 {
		t.Fatal(p.TrackCleanup)
	}
}
func TestMixedRetryResolvesOnlyJournaledSuccessfulFiles(t *testing.T) {
	f := newReviewFixture(t)
	p := skippedFixture()
	retry := retryFixture()
	retry.State = "failed"
	retry.TrackCleanup.Skipped = 1
	retry.TrackCleanup.Targets = append(retry.TrackCleanup.Targets, cleanupTarget{ID: 2})
	retry.TrackCleanup.Successes = []CleanupSuccess{{FileID: 1, Path: "Movies/one.mkv", CompletedAt: 202000}}
	saveCleanupFixture(t, f, p)
	saveCleanupFixture(t, f, retry)
	got, err := f.s.Plan(context.Background(), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.TrackCleanup.Skipped != 0 || got.TrackCleanup.Recovered != 1 {
		t.Fatal(got.TrackCleanup)
	}
}
func TestCappedLegacyHistoryDoesNotEraseUnknownSkips(t *testing.T) {
	f := newReviewFixture(t)
	p := skippedFixture()
	p.TrackCleanup.SkipHistory = nil
	p.TrackCleanup.SkipAttempts = 0
	p.TrackCleanup.Skipped = 25
	p.TrackCleanup.Examples = []string{"Movies/one.mkv: Old verifier rejected the edit"}
	saveCleanupFixture(t, f, p)
	saveCleanupFixture(t, f, retryFixture())
	got, err := f.s.Plan(context.Background(), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.TrackCleanup.Skipped != 24 || got.TrackCleanup.UntrackedSkips != 24 || got.TrackCleanup.SkipAttempts != 25 {
		t.Fatal(got.TrackCleanup)
	}
}

func TestCleanupRetryAutomaticallyResolvesEarlierJobAndSurvivesReopen(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	f.movie(t, 1, "Movies/one.mkv", []byte("original"))
	f.s.SetTrackCleaner(matroskaStubStreams, nil)
	f.s.SetMatroskaTrackDisabler(func(context.Context, string, string, []int, int64) (string, error) {
		return "", problem("invalid_media", "Old verifier failed")
	})
	request := TrackCleanupRequest{Mode: "all_subtitles", Action: "disable", FileID: 1}
	failed, err := f.s.PreviewTrackCleanup(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.Apply(ctx, failed.ID); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = f.s.Tick(ctx); err != nil {
			t.Fatal(err)
		}
	}
	failed, err = f.s.Plan(ctx, failed.ID)
	if err != nil || failed.TrackCleanup.Skipped != 1 {
		t.Fatal(failed, err)
	}
	f.s.SetMatroskaTrackDisabler(func(_ context.Context, path, _ string, _ []int, _ int64) (string, error) {
		return `{}`, os.WriteFile(path, []byte("edited"), 0600)
	})
	retry, err := f.s.PreviewTrackCleanup(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.Apply(ctx, retry.ID); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = f.s.Tick(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if err = f.db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := database.Open(ctx, f.cfg.ConfigDir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got, err := New(reopened, f.cfg).Plan(ctx, failed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.TrackCleanup.Skipped != 0 || got.TrackCleanup.SkipAttempts != 1 || len(got.TrackCleanup.SkipHistory) != 1 || got.TrackCleanup.SkipHistory[0].ResolvedBy != retry.ID {
		t.Fatalf("retry did not resolve earlier failure: %+v", got.TrackCleanup)
	}
	saved, err := New(reopened, f.cfg).Plan(ctx, retry.ID)
	if err != nil || len(saved.TrackCleanup.Successes) != 1 {
		t.Fatal(saved, err)
	}
}

func TestLaterFailureIsNotClearedByAnEarlierSuccessfulRetry(t *testing.T) {
	f := newReviewFixture(t)
	p := skippedFixture()
	p.TrackCleanup.SkipAttempts = 2
	p.TrackCleanup.SkipHistory = append(p.TrackCleanup.SkipHistory, CleanupSkipAttempt{FileID: 1, Path: "Movies/one.mkv", Message: "Failed again later", OccurredAt: 300000})
	saveCleanupFixture(t, f, p)
	saveCleanupFixture(t, f, retryFixture())
	got, err := f.s.Plan(context.Background(), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	j := got.TrackCleanup
	if j.Skipped != 1 || j.SkipAttempts != 2 || j.Recovered != 0 || j.SkipHistory[0].ResolvedBy == "" || j.SkipHistory[1].ResolvedBy != "" {
		t.Fatal(j)
	}
}
