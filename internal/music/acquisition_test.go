package music

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/database"
)

type fakeAudioSource struct {
	failDownload bool
}

func (f fakeAudioSource) SearchAudio(context.Context, string, int) ([]AudioCandidate, error) {
	return []AudioCandidate{{ID: "abcdefghijk", Title: "Song", DurationSeconds: 200}}, nil
}

func (f fakeAudioSource) DownloadAudio(_ context.Context, _ string, directory, _ string) (AudioFile, error) {
	if f.failDownload {
		return AudioFile{}, errors.New("download failed")
	}
	path := filepath.Join(directory, "track.mp3")
	if err := os.WriteFile(path, []byte("audio"), 0o600); err != nil {
		return AudioFile{}, err
	}
	return AudioFile{Path: path, Format: "mp3"}, nil
}

func (f fakeAudioSource) InspectAudio(context.Context, string) (AudioInspection, error) {
	return AudioInspection{}, errors.New("not used")
}

func TestChooseAudioCandidateRequiresTitleMatch(t *testing.T) {
	candidates := []AudioCandidate{
		{ID: "dependswik", Title: "XG - DEPENDS (Official Music Video)", DurationSeconds: 185},
		{ID: "sadwikwxyz", Title: "XG - S.A.D (Official Audio)", DurationSeconds: 202},
	}
	chosen := chooseAudioCandidate(candidates, 202000, "S.A.D")
	if chosen == nil || chosen.ID != "sadwikwxyz" {
		t.Fatal("expected the S.A.D candidate", chosen)
	}
	// A result set that only names a different song yields no candidate instead
	// of a duration-only guess.
	if chosen := chooseAudioCandidate(candidates[:1], 202000, "S.A.D"); chosen != nil {
		t.Fatal("expected no candidate for the wrong title", chosen)
	}
	// Without any title the closest duration is still the best available.
	if chosen := chooseAudioCandidate(candidates, 202000, ""); chosen == nil || chosen.ID != "sadwikwxyz" {
		t.Fatal("duration-only fallback", chosen)
	}
}

func TestAudioCandidateMatchesTitleTokenBoundaries(t *testing.T) {
	cases := []struct {
		candidateTitle string
		title          string
		want           bool
	}{
		// The S.A.D. / DEPENDS conflation must stay impossible in both
		// directions regardless of duration evidence.
		{"XG - DEPENDS (Official Music Video)", "S.A.D.", false},
		{"Tech N9ne - DEPENDS", "S.A.D.", false},
		{"XG - DEPENDS", "SAD", false},
		{"XG - S.A.D.", "DEPENDS", false},
		{"XG - S.A.D. (from DEPENDS)", "DEPENDS", false},
		// Punctuation-only differences are the same title.
		{"SAD", "S.A.D.", true},
		{"XG - S.A.D. (Official Audio)", "S.A.D.", true},
		{"XG - S.A.D", "S.A.D.", true},
		{"DEPENDS", "DEPENDS", true},
		// The requested title must land on token boundaries, never inside a
		// larger word.
		{"XG - SADDEST (Lyric Video)", "S.A.D.", false},
		{"XG - SADLY (Official Video)", "SAD", false},
		{"XG - MASSACRE", "SAD", false},
		// Blank sides keep the documented behavior.
		{"", "S.A.D.", false},
		{"Any Song", "", true},
	}
	for _, c := range cases {
		if got := audioCandidateMatchesTitle(c.candidateTitle, c.title); got != c.want {
			t.Errorf("audioCandidateMatchesTitle(%q, %q) = %v, want %v", c.candidateTitle, c.title, got, c.want)
		}
	}
}

func TestChooseAudioCandidatePrefersTrackTitleOverExactDuration(t *testing.T) {
	// A wrong-title candidate whose duration matches exactly must lose to a
	// correct-title candidate with a farther duration.
	candidates := []AudioCandidate{
		{ID: "dependswik", Title: "Tech N9ne - DEPENDS (Official Music Video)", DurationSeconds: 202},
		{ID: "sadwikwxyz", Title: "Tech N9ne - S.A.D. (Official Audio)", DurationSeconds: 195},
	}
	chosen := chooseAudioCandidate(candidates, 202000, "S.A.D.")
	if chosen == nil || chosen.ID != "sadwikwxyz" {
		t.Fatal("expected the correctly titled S.A.D. candidate", chosen)
	}
	// A result set that only offers S.A.D. must never satisfy a DEPENDS
	// request, and vice versa.
	if chosen := chooseAudioCandidate(candidates[1:], 202000, "DEPENDS"); chosen != nil {
		t.Fatal("expected no candidate for the DEPENDS request", chosen)
	}
	if chosen := chooseAudioCandidate([]AudioCandidate{{ID: "dependswik", Title: "Tech N9ne - DEPENDS", DurationSeconds: 202}}, 202000, "S.A.D."); chosen != nil {
		t.Fatal("expected no candidate for the S.A.D. request", chosen)
	}
}

func TestChooseAudioCandidateDurationTieBreak(t *testing.T) {
	// When several candidates pass the title check the closest duration wins.
	candidates := []AudioCandidate{
		{ID: "far", Title: "XG - S.A.D. (Live)", DurationSeconds: 185},
		{ID: "near", Title: "XG - S.A.D. (Official Audio)", DurationSeconds: 195},
		{ID: "exact", Title: "XG - S.A.D. (MV)", DurationSeconds: 202},
	}
	chosen := chooseAudioCandidate(candidates, 202000, "S.A.D.")
	if chosen == nil || chosen.ID != "exact" {
		t.Fatal("expected the closest-duration S.A.D. candidate", chosen)
	}
	chosen = chooseAudioCandidate(candidates[:2], 202000, "S.A.D.")
	if chosen == nil || chosen.ID != "near" {
		t.Fatal("expected the closest-duration candidate without the exact match", chosen)
	}
}

// titleAudioSource returns one candidate named after the requested track so the
// acquisition can match a title-based YouTube result.
type titleAudioSource struct {
	title    string
	duration int64
}

func (f titleAudioSource) SearchAudio(context.Context, string, int) ([]AudioCandidate, error) {
	return []AudioCandidate{{ID: "abcdefghijk", Title: f.title, DurationSeconds: f.duration}}, nil
}

func (f titleAudioSource) DownloadAudio(_ context.Context, _ string, directory, _ string) (AudioFile, error) {
	path := filepath.Join(directory, "track.mp3")
	if err := os.WriteFile(path, []byte("audio"), 0o600); err != nil {
		return AudioFile{}, err
	}
	return AudioFile{Path: path, Format: "mp3"}, nil
}

func (f titleAudioSource) InspectAudio(context.Context, string) (AudioInspection, error) {
	return AudioInspection{}, errors.New("not used")
}

func TestAcquisitionAddsNewSongBesideOccupiedNumber(t *testing.T) {
	ctx := context.Background()
	downloads := t.TempDir()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	service := NewService(db, t.TempDir())
	service.SetDownloadsDir(downloads)

	acquire := func(source titleAudioSource, title, album string, number, durationMS int) {
		t.Helper()
		service.SetAudioSource(source)
		_, err := service.StartAcquisition(ctx, "", "top_track", title, "Music", "source", []AcquisitionTrackInput{{
			Title: title, Artist: "XG", Album: album, AlbumArtist: "XG",
			TrackNumber: number, DiscNumber: 1, DurationMS: durationMS, SearchQuery: "XG " + title,
		}})
		if err != nil {
			t.Fatal(err)
		}
		if worked, err := service.ProcessAcquisition(ctx); err != nil || !worked {
			t.Fatal("process", worked, err)
		}
	}

	// The album already holds "DEPENDS" at track 1, the same slot the "S.A.D"
	// request asks for. The new song must be added at the next free number
	// instead of being conflated with the existing track.
	acquire(titleAudioSource{title: "DEPENDS", duration: 185}, "DEPENDS", "DEPENDS", 1, 185000)
	acquire(titleAudioSource{title: "S.A.D", duration: 202}, "S.A.D", "DEPENDS", 1, 202000)

	rows, err := db.Query(`SELECT disc,number,title FROM music_tracks ORDER BY disc,number`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var disc, number int
		var title string
		if err := rows.Scan(&disc, &number, &title); err != nil {
			t.Fatal(err)
		}
		got = append(got, fmt.Sprintf("%d/%d:%s", disc, number, title))
	}
	if len(got) != 2 || got[0] != "1/1:DEPENDS" || got[1] != "1/2:S.A.D" {
		t.Fatal("catalog tracks", got)
	}
}

func TestAcquisitionQueuesDownloadAndCatalogImport(t *testing.T) {
	ctx := context.Background()
	downloads := t.TempDir()
	media := t.TempDir()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	service := NewService(db, media)
	service.SetDownloadsDir(downloads)
	service.SetAudioSource(fakeAudioSource{})

	job, err := service.StartAcquisition(ctx, "", "top_track", "Song", "Music", "source", []AcquisitionTrackInput{{
		Title: "Song", Artist: "Artist", Album: "Album", AlbumArtist: "Artist",
		TrackNumber: 1, DiscNumber: 1, DurationMS: 200000, SearchQuery: "Artist Song",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if job.State != "queued" || len(job.Tracks) != 1 {
		t.Fatal("job", job)
	}
	worked, err := service.ProcessAcquisition(ctx)
	if err != nil || !worked {
		t.Fatal("process", worked, err)
	}
	job, err = service.Acquisition(ctx, job.ID)
	if err != nil || job.Tracks[0].State != "importing" || job.Tracks[0].ImportJobID == nil {
		t.Fatal("import queue", job, err)
	}
	var albums, tracks, imports int
	if err := db.QueryRow(`SELECT (SELECT COUNT(*) FROM music_albums),(SELECT COUNT(*) FROM music_tracks),(SELECT COUNT(*) FROM music_import_jobs)`).
		Scan(&albums, &tracks, &imports); err != nil {
		t.Fatal(err)
	}
	if albums != 1 || tracks != 1 || imports != 1 {
		t.Fatal("catalog/import rows", albums, tracks, imports)
	}

	failing := NewService(db, media)
	failing.SetDownloadsDir(downloads)
	failing.SetAudioSource(fakeAudioSource{failDownload: true})
	failedJob, err := failing.StartAcquisition(ctx, "", "top_track", "Other", "Music", "source", []AcquisitionTrackInput{{
		Title: "Other", Artist: "Artist", Album: "Album", TrackNumber: 2, DiscNumber: 1, SearchQuery: "Artist Other",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if worked, err := failing.ProcessAcquisition(ctx); err != nil || !worked {
		t.Fatal("failed process", worked, err)
	}
	failedJob, err = failing.Acquisition(ctx, failedJob.ID)
	if err != nil || failedJob.Tracks[0].State != "failed" || failedJob.State != "failed" {
		t.Fatal("failed acquisition", failedJob, err)
	}
}
