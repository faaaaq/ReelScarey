package nfoexport

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/faaaaq/ReelScarey/internal/nfo"
)

func TestCleanupOneBlanksEpisodeSortTitleWhenExportDisabled(t *testing.T) {
	f := newFixture(t)
	f.exec(t, `UPDATE settings SET nfo_export=0 WHERE id=1`)
	relative := "Anime/Test Show/Season 1/Test Show.S01E02.mkv"
	f.writeMedia(t, relative, "owned anime video")
	f.addSeries(t, 10, "Test Show", 2024, 20, 1)
	f.addEpisode(t, 30, 20, 2, "Second", "Plot stays", "2024-01-08", 1002)
	// The existing origin represents an in-place/manual registration. Cleanup
	// deliberately does not depend on a download or on NFO generation.
	f.addEpisodeFile(t, 40, 10, 30, relative)
	target := sidecarName(t, f.root, relative)
	input := `<?xml version="1.0"?>
<episodedetails>
  <title>Second &amp; unchanged</title>
  <sorttitle>0001 - inconsistent provider order</sorttitle>
  <plot>Plot &lt;sorttitle&gt;text&lt;/sorttitle&gt; stays</plot>
</episodedetails>
`
	if err := os.WriteFile(target, []byte(input), 0o600); err != nil {
		t.Fatal(err)
	}

	worked, err := f.worker().CleanupOne(context.Background())
	if err != nil || !worked {
		t.Fatalf("CleanupOne = (%v, %v), want (true, nil)", worked, err)
	}
	raw, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(input, "<sorttitle>0001 - inconsistent provider order</sorttitle>", "<sorttitle></sorttitle>", 1)
	if string(raw) != want {
		t.Fatalf("unexpected cleaned NFO:\n%s\nwant:\n%s", raw, want)
	}
	media, err := os.ReadFile(filepath.Join(f.root, filepath.FromSlash(relative)))
	if err != nil || string(media) != "owned anime video" {
		t.Fatalf("media changed: %q, %v", media, err)
	}
	worked, err = f.worker().ProcessOne(context.Background())
	if err != nil || worked {
		t.Fatalf("disabled ProcessOne = (%v, %v)", worked, err)
	}
	after, err := os.ReadFile(target)
	if err != nil || string(after) != want {
		t.Fatalf("disabled export rewrote cleaned metadata: %q, %v", after, err)
	}
}

func TestSortTitleBatchIsResumableAndContinuesAfterBadNFO(t *testing.T) {
	f := newFixture(t)
	f.addSeries(t, 10, "Batch Show", 2024, 20, 1)
	relatives := []string{
		"TV/Batch/Season 1/Batch.S01E01.mkv",
		"TV/Batch/Season 1/Batch.S01E02.mkv",
		"TV/Batch/Season 1/Batch.S01E03.mkv",
	}
	for index, relative := range relatives {
		episodeID := int64(30 + index)
		fileID := int64(40 + index)
		f.writeMedia(t, relative, "episode")
		f.addEpisode(t, episodeID, 20, index+1, "Episode", "", "", episodeID)
		f.addEpisodeFile(t, fileID, 10, episodeID, relative)
	}
	first := sidecarName(t, f.root, relatives[0])
	if err := os.WriteFile(first, []byte(`<episodedetails><title>One</title><sorttitle>wrong</sorttitle></episodedetails>`), 0o644); err != nil {
		t.Fatal(err)
	}
	// The second file has no NFO. The third has invalid XML and must remain
	// untouched while earlier valid files are still cleaned.
	third := sidecarName(t, f.root, relatives[2])
	invalid := `<episodedetails><sorttitle>broken</episodedetails>`
	if err := os.WriteFile(third, []byte(invalid), 0o644); err != nil {
		t.Fatal(err)
	}
	w := f.worker()
	w.batchLimit = 2

	page, err := w.ClearSortTitles(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if page.Done || page.Scanned != 2 || page.Changed != 1 || page.Missing != 1 || page.Failed != 0 || page.NextAfterID != 41 {
		t.Fatalf("unexpected first page: %+v", page)
	}
	page, err = w.ClearSortTitles(context.Background(), page.NextAfterID)
	if err != nil {
		t.Fatal(err)
	}
	if !page.Done || page.Scanned != 1 || page.Failed != 1 || len(page.Issues) != 1 || page.NextAfterID != 42 {
		t.Fatalf("unexpected second page: %+v", page)
	}
	raw, err := os.ReadFile(first)
	if err != nil || !strings.Contains(string(raw), "<sorttitle></sorttitle>") {
		t.Fatalf("valid NFO was not cleaned: %q, %v", raw, err)
	}
	raw, err = os.ReadFile(third)
	if err != nil || string(raw) != invalid {
		t.Fatalf("invalid NFO changed: %q, %v", raw, err)
	}
	last, err := w.ClearSortTitles(context.Background(), page.NextAfterID)
	if err != nil || !last.Done || last.Scanned != 0 || last.NextAfterID != 42 {
		t.Fatalf("empty final page = %+v, %v", last, err)
	}
}

func TestSortTitleCleanupLeavesMissingBlankAndMovieNFOsAlone(t *testing.T) {
	f := newFixture(t)
	f.addSeries(t, 10, "Blank Show", 2024, 20, 1)
	blankRelative := "TV/Blank/Season 1/Blank.S01E01.mkv"
	f.writeMedia(t, blankRelative, "blank")
	f.addEpisode(t, 30, 20, 1, "Blank", "", "", 1001)
	f.addEpisodeFile(t, 40, 10, 30, blankRelative)
	blankTarget := sidecarName(t, f.root, blankRelative)
	blank := `<episodedetails><title>Blank</title><sorttitle/></episodedetails>`
	if err := os.WriteFile(blankTarget, []byte(blank), 0o644); err != nil {
		t.Fatal(err)
	}
	missingRelative := "TV/Blank/Season 1/Blank.S01E02.mkv"
	f.writeMedia(t, missingRelative, "missing")
	f.addEpisode(t, 31, 20, 2, "Missing", "", "", 1002)
	f.addEpisodeFile(t, 41, 10, 31, missingRelative)

	movieRelative := "Movies/Movie/Movie.mkv"
	f.writeMedia(t, movieRelative, "movie")
	f.addMovie(t, 50, 50, "Movie", 2020, 500, "", "", movieRelative)
	movieTarget := sidecarName(t, f.root, movieRelative)
	movie := `<movie><title>Movie</title><sorttitle>custom movie order</sorttitle></movie>`
	if err := os.WriteFile(movieTarget, []byte(movie), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := f.worker().ClearSortTitles(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Done || report.Scanned != 2 || report.AlreadyBlank != 1 || report.Missing != 1 || report.Changed != 0 || report.Failed != 0 {
		t.Fatalf("unexpected report: %+v", report)
	}
	for target, want := range map[string]string{blankTarget: blank, movieTarget: movie} {
		raw, readErr := os.ReadFile(target)
		if readErr != nil || string(raw) != want {
			t.Fatalf("already blank/movie NFO changed at %s: %q, %v", target, raw, readErr)
		}
	}
	if _, statErr := os.Stat(sidecarName(t, f.root, missingRelative)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("missing NFO was created: %v", statErr)
	}
}

func TestCleanupOneRejectsMalformedAndUnsafeSidecars(t *testing.T) {
	t.Run("malformed", func(t *testing.T) {
		f := newFixture(t)
		relative := "TV/Invalid/Season 1/Invalid.S01E01.mkv"
		f.writeMedia(t, relative, "video")
		f.addSeries(t, 10, "Invalid", 2024, 20, 1)
		f.addEpisode(t, 30, 20, 1, "Invalid", "", "", 1001)
		f.addEpisodeFile(t, 40, 10, 30, relative)
		target := sidecarName(t, f.root, relative)
		input := `<episodedetails><sorttitle>broken</episodedetails>`
		if err := os.WriteFile(target, []byte(input), 0o644); err != nil {
			t.Fatal(err)
		}
		worked, err := f.worker().CleanupOne(context.Background())
		if !worked || !errors.Is(err, nfo.ErrInvalid) {
			t.Fatalf("CleanupOne = (%v, %v), want invalid NFO error", worked, err)
		}
		raw, readErr := os.ReadFile(target)
		if readErr != nil || string(raw) != input {
			t.Fatalf("malformed NFO changed: %q, %v", raw, readErr)
		}
	})

	t.Run("symlink", func(t *testing.T) {
		f := newFixture(t)
		relative := "TV/Link/Season 1/Link.S01E01.mkv"
		f.writeMedia(t, relative, "video")
		f.addSeries(t, 10, "Link", 2024, 20, 1)
		f.addEpisode(t, 30, 20, 1, "Link", "", "", 1001)
		f.addEpisodeFile(t, 40, 10, 30, relative)
		target := sidecarName(t, f.root, relative)
		outside := filepath.Join(t.TempDir(), "outside.nfo")
		input := `<episodedetails><sorttitle>do not touch</sorttitle></episodedetails>`
		if err := os.WriteFile(outside, []byte(input), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, target); err != nil {
			t.Skipf("symbolic links are unavailable: %v", err)
		}
		worked, err := f.worker().CleanupOne(context.Background())
		if !worked || !errors.Is(err, ErrUnsafePath) {
			t.Fatalf("CleanupOne = (%v, %v), want unsafe path", worked, err)
		}
		raw, readErr := os.ReadFile(outside)
		if readErr != nil || string(raw) != input {
			t.Fatalf("symlink target changed: %q, %v", raw, readErr)
		}
	})
}

func TestRunAutomaticallyCleansNewRegistration(t *testing.T) {
	f := newFixture(t)
	f.exec(t, `UPDATE settings SET nfo_export=0 WHERE id=1`)
	f.addSeries(t, 10, "Running Show", 2024, 20, 1)
	f.addEpisode(t, 30, 20, 1, "Pilot", "", "", 1001)
	relative := "TV/Running/Season 1/Running.S01E01.mkv"
	f.writeMedia(t, relative, "video")
	target := sidecarName(t, f.root, relative)
	input := `<episodedetails><title>Pilot</title><sorttitle>new registration order</sorttitle></episodedetails>`
	if err := os.WriteFile(target, []byte(input), 0o644); err != nil {
		t.Fatal(err)
	}

	w := f.worker()
	w.Interval = 5 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		w.Run(ctx, slog.New(slog.NewTextHandler(io.Discard, nil)))
		close(done)
	}()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("worker did not stop")
		}
	}()
	// Let the worker observe the initially empty catalog, then add the same
	// registration row produced by a completed manual/existing import.
	time.Sleep(20 * time.Millisecond)
	f.addEpisodeFile(t, 40, 10, 30, relative)

	deadline := time.Now().Add(3 * time.Second)
	for {
		raw, err := os.ReadFile(target)
		if err == nil && strings.Contains(string(raw), "<sorttitle></sorttitle>") {
			if !strings.Contains(string(raw), "<title>Pilot</title>") {
				t.Fatalf("unrelated metadata changed: %s", raw)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("new registration was not cleaned automatically: %q, %v", raw, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestClearSortTitlesRejectsNegativeCursor(t *testing.T) {
	f := newFixture(t)
	if _, err := f.worker().ClearSortTitles(context.Background(), -1); !errors.Is(err, ErrSortTitleCursor) {
		t.Fatalf("negative cursor error = %v", err)
	}
}
