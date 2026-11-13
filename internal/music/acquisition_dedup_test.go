package music

import (
	"context"
	"sync"
	"testing"
)

// Double-clicked or retried acquisition submits must return the existing
// queued/running job instead of creating a second one that re-downloads every
// track (CS-7). A completed or failed job must remain re-submittable.
func TestAcquisitionDeduplicatesActiveJobsAndAllowsResubmission(t *testing.T) {
	ctx := context.Background()
	s, db, _ := editFixture(t)
	s.SetAudioSource(fakeAudioSource{})
	tracks := []AcquisitionTrackInput{{
		Title: "Song", Artist: "Artist", Album: "Album", AlbumArtist: "Artist",
		SearchQuery: "Artist Song",
	}}

	// Concurrent duplicate album submissions produce exactly one job.
	var wg sync.WaitGroup
	results := make([]AcquisitionJob, 4)
	errs := make([]error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = s.StartAlbumAcquisition(ctx, 2, "Music", "source")
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent submit %d failed: %v", i, err)
		}
		if results[i].ID != results[0].ID {
			t.Fatalf("concurrent submits created distinct jobs: %d vs %d", results[i].ID, results[0].ID)
		}
	}
	var jobs int
	if err := db.QueryRow(`SELECT COUNT(*) FROM music_acquisition_jobs`).Scan(&jobs); err != nil || jobs != 1 {
		t.Fatal("duplicate album submits created multiple jobs", jobs, err)
	}

	// Sequential duplicate link submits deduplicate on the source URL.
	first, err := s.StartAcquisition(ctx, "https://www.deezer.com/track/42", "monitor", "Song", "Music", "source", tracks)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.StartAcquisition(ctx, "https://www.deezer.com/track/42", "monitor", "Song", "Music", "source", tracks)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("link submits created distinct jobs: %d vs %d", first.ID, second.ID)
	}

	// A completed job can be intentionally submitted again.
	if _, err := db.Exec(`UPDATE music_acquisition_jobs SET state='completed',dedup_key=NULL WHERE id=?`, first.ID); err != nil {
		t.Fatal(err)
	}
	third, err := s.StartAcquisition(ctx, "https://www.deezer.com/track/42", "monitor", "Song", "Music", "source", tracks)
	if err != nil {
		t.Fatal("resubmission after completion must be allowed:", err)
	}
	if third.ID == first.ID {
		t.Fatal("completed job was reused instead of starting fresh")
	}
}
