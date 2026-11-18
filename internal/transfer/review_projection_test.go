package transfer

import (
	"context"
	"encoding/json"
	"os"
	"path"
	"strings"
	"testing"
)

func TestDiscoveredReviewProjectionCountsEvidenceAndRestart(t *testing.T) {
	f := setupWorkflow(t)
	ctx := context.Background()
	body := []byte("owned disposable review projection media")
	readyPath := existingVideo(t, f, "Movies/Test Movie (2026)/A.Test.Movie.2026.mkv", body)
	blockedPath := existingVideo(t, f, "Movies/Test Movie (2026)/B.Test.Movie.2026.mkv", body)
	existingVideo(t, f, "Movies/Test Movie (2026)/Z.Test.Movie.2026.mkv", body)
	scan, err := f.s.StartLibraryScan(ctx, "movie")
	if err != nil {
		t.Fatal(err)
	}
	page := finishScan(t, f, scan.ID)
	if len(page.Items) != 3 {
		t.Fatalf("discovered %d files, want 3", len(page.Items))
	}
	byName := map[string]DiscoveredFile{}
	for _, item := range page.Items {
		byName[path.Base(item.Path)] = item
		if item.Destination != item.FullPath {
			t.Fatalf("registration destination changed: %#v", item)
		}
		if !strings.Contains(item.PlannedOperation, "rename, move and retag remain separate") {
			t.Fatalf("in-place operation was not explicit: %q", item.PlannedOperation)
		}
		if len(item.MatchEvidence) == 0 || !strings.Contains(item.MatchEvidence[0], "supporting evidence") {
			t.Fatalf("filename evidence missing: %#v", item.MatchEvidence)
		}
	}
	ready := byName["A.Test.Movie.2026.mkv"]
	blocked := byName["B.Test.Movie.2026.mkv"]
	skipped := byName["Z.Test.Movie.2026.mkv"]
	candidate := automaticCandidate{
		Snapshot: reviewedMatch{
			Choice: BulkMatchChoice{FileID: ready.ID, MediaID: f.movie.ID, Episodes: []int64{}},
			Kind:   "movie",
			Title:  f.movie.Title,
		},
		Note: "Trusted TMDb folder ID agreed with the saved catalog identity.",
	}
	payload, err := json.Marshal(candidate)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(`INSERT INTO library_match_jobs(scan_id,state,phase,category,created_at,updated_at) VALUES(?,'completed','queue','standard',unixepoch(),unixepoch())`, scan.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(`INSERT INTO library_match_candidates(file_id,scan_id,state,payload,message) VALUES(?,?,'ready',?,'Exact identity snapshot saved.')`, ready.ID, scan.ID, payload); err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(`UPDATE library_scan_files SET state='blocked',message='The trusted download target conflicts with the folder TMDb ID; review both identities.' WHERE id=?`, blocked.ID); err != nil {
		t.Fatal(err)
	}
	if err = f.s.SkipDiscovered(ctx, skipped.ID); err != nil {
		t.Fatal(err)
	}

	page, err = f.s.DiscoveredFiles(ctx, scan.ID, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	if page.Review.Ready != 1 || page.Review.NeedsDecision != 1 || page.Review.Skipped != 1 {
		t.Fatalf("unexpected review counts: %#v", page.Review)
	}
	if len(page.Items) != 3 || page.Items[0].ID != blocked.ID || page.Items[1].ID != ready.ID || page.Items[2].ID != skipped.ID {
		t.Fatalf("exceptions were not ordered first: %#v", page.Items)
	}
	if page.Items[0].ProposedIdentity == "" || !strings.Contains(strings.Join(page.Items[0].MatchEvidence, " "), "conflicts with") {
		t.Fatalf("plain-language conflict missing: %#v", page.Items[0])
	}
	if page.Items[1].ProposedIdentity != f.movie.Title || !strings.Contains(strings.Join(page.Items[1].MatchEvidence, " "), "Trusted TMDb") {
		t.Fatalf("saved identity or evidence missing: %#v", page.Items[1])
	}
	if !strings.Contains(page.Items[2].PlannedOperation, "unchanged") {
		t.Fatalf("skip operation unclear: %#v", page.Items[2])
	}

	f.reopen(t)
	reopened, err := f.s.DiscoveredFiles(ctx, scan.ID, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Review != page.Review || len(reopened.Items) != 3 || reopened.Items[1].ProposedIdentity != f.movie.Title {
		t.Fatalf("review did not survive reopen without rescanning: %#v", reopened)
	}
	for _, name := range []string{readyPath, blockedPath} {
		actual, err := os.ReadFile(name)
		if err != nil || string(actual) != string(body) {
			t.Fatalf("review projection changed source %s: %v", name, err)
		}
	}
}
