package transfer

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"github.com/faaaaq/ReelScarey/internal/metadata"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/faaaaq/ReelScarey/internal/filecheck"
	"github.com/faaaaq/ReelScarey/internal/organization"
)

// insufficientSpace reports disk-full errors portably. Windows maps
// SetEndOfFile failures to ERROR_DISK_FULL rather than ENOSPC, and a full
// extent cannot be created on filesystems without sparse support.
func insufficientSpace(err error) bool {
	if errors.Is(err, syscall.ENOSPC) {
		return true
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "not enough space") || strings.Contains(text, "disk full")
}

func TestLargeExistingRegistrationDoesNotReadWholeVideo(t *testing.T) {
	f := setupWorkflow(t)
	name := existingVideo(t, f, "Movies/Test Movie (2026)/obfuscated.mp4", []byte("owned sparse fixture"))
	if err := os.Truncate(name, 64<<30); err != nil {
		if insufficientSpace(err) {
			t.Skipf("the 64 GiB sparse fixture needs more free disk space: %v", err)
		}
		t.Fatal(err)
	}
	scan, err := f.s.StartLibraryScanWithMatching(context.Background(), "movie", "", true)
	if err != nil {
		t.Fatal(err)
	}
	finishScan(t, f, scan.ID)
	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for range 5 {
		if _, err = f.s.ProcessAutomaticMatchNext(ctx); err != nil {
			t.Fatal(err)
		}
	}
	progress, err := f.s.LibraryScan(ctx, scan.ID)
	if err != nil || progress.NeedsMatch != 0 || progress.Queued != 1 {
		t.Fatal(progress, err)
	}
	if _, err = f.s.ProcessLibraryNext(ctx); err != nil {
		t.Fatal(err)
	}
	progress, err = f.s.LibraryScan(ctx, scan.ID)
	if err != nil || progress.Registered != 1 {
		t.Fatal(progress, err)
	}
	var sha, raw string
	if err = f.db.QueryRow(`SELECT sha256,verification_json FROM media_files`).Scan(&sha, &raw); err != nil {
		t.Fatal(err)
	}
	var observation filecheck.Snapshot
	if sha != "" || json.Unmarshal([]byte(raw), &observation) != nil || observation.Size != 64<<30 || observation.Sample == "" {
		t.Fatal("missing bounded observation")
	}
	t.Logf("64 GiB sparse fixture registered in %s (fixture prober; no full-content read)", time.Since(started))
	// Forgetting a scan must not discard the observation required for later work.
	if err = f.s.RemoveLibraryScan(ctx, scan.ID); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err = filecheck.Verify(ctx, file, raw); err != nil {
		t.Fatal(err)
	}
}

func TestMatchedFilesAreNotPresentedAsUnmatched(t *testing.T) {
	f := setupWorkflow(t)
	scan := automaticMovieScan(t, f, "Movies/Test Movie (2026)/Test.Movie.2026.mp4")
	ctx := context.Background()
	if _, err := f.s.ProcessAutomaticMatchNext(ctx); err != nil {
		t.Fatal(err)
	}
	review, err := f.s.DiscoveredFiles(ctx, scan.ID, 1, "review")
	if err != nil || review.Total != 0 || review.Scan.NeedsMatch != 0 || review.Scan.Matched != 1 {
		t.Fatal(review, err)
	}
	matched, err := f.s.DiscoveredFiles(ctx, scan.ID, 1, "matched")
	if err != nil || matched.Total != 1 || matched.Items[0].State != "matched" {
		t.Fatal(matched, err)
	}
	if err = f.s.CancelAutomaticMatching(ctx, scan.ID); err != nil {
		t.Fatal(err)
	}
	if err = f.s.StartAutomaticMatching(ctx, scan.ID); err != nil {
		t.Fatal(err)
	}
	finishAutomatic(t, f, scan.ID)
}

func TestFastRegisteredFileCanBeOrganizedWithFullIntegrity(t *testing.T) {
	f := setupWorkflow(t)
	name := existingVideo(t, f, "Movies/Test Movie (2026)/obfuscated.mp4", make([]byte, 1<<20))
	scan, err := f.s.StartLibraryScanWithMatching(context.Background(), "movie", "", true)
	if err != nil {
		t.Fatal(err)
	}
	finishScan(t, f, scan.ID)
	finishAutomatic(t, f, scan.ID)
	var id int64
	var sha string
	if err = f.db.QueryRow(`SELECT id,sha256 FROM media_files`).Scan(&id, &sha); err != nil || sha != "" {
		t.Fatal(sha, err)
	}
	org := organization.New(f.db, f.cfg)
	plan, err := org.Preview(context.Background(), []int64{id}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Files) != 1 || len(plan.Files[0].SHA) != 64 {
		t.Fatal("review lacks full checksum")
	}
	file, err := os.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	expected, err := filecheck.Digest(context.Background(), file)
	if err != nil || expected != plan.Files[0].SHA {
		t.Fatal("wrong review checksum", err)
	}
}

func TestFFprobeSeeksOverLargeMP4Payload(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux container descriptor inspection")
	}
	for _, tool := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip(tool + " unavailable")
		}
	}
	name := filepath.Join(t.TempDir(), "owned.mp4")
	if output, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "color=c=blue:s=160x90:d=1", "-c:v", "mpeg4", name).CombinedOutput(); err != nil {
		t.Fatalf("generated fixture: %v %s", err, output)
	}
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	moov := -1
	for offset := 0; offset+8 <= len(b); {
		size := int(binary.BigEndian.Uint32(b[offset : offset+4]))
		if size < 8 || offset+size > len(b) {
			t.Fatal("invalid generated MP4")
		}
		if string(b[offset+4:offset+8]) == "moov" {
			moov = offset
			if offset+size != len(b) {
				t.Fatal("expected trailing metadata")
			}
			break
		}
		offset += size
	}
	if moov < 0 {
		t.Fatal("no metadata box")
	}
	file, err := os.OpenFile(name, os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	const padding int64 = 4 << 30
	header := make([]byte, 16)
	binary.BigEndian.PutUint32(header, 1)
	copy(header[4:8], "free")
	binary.BigEndian.PutUint64(header[8:], uint64(padding))
	if _, err = file.WriteAt(header, int64(moov)); err != nil {
		t.Fatal(err)
	}
	if _, err = file.WriteAt(b[moov:], int64(moov)+padding); err != nil {
		t.Fatal(err)
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	started := time.Now()
	probe, err := (FFprobe{}).Probe(ctx, file)
	if err != nil || probe.Width != 160 || probe.Height != 90 || probe.Duration < 0.9 {
		t.Fatal(probe, err)
	}
	t.Logf("4 GiB sparse MP4 with trailing metadata inspected in %s", time.Since(started))
}

func TestFolderProviderIDFetchesDetailsOnceDespiteAlternateTitle(t *testing.T) {
	f := setupWorkflow(t)
	year := 2020
	calls := 0
	provider := &automaticProviderFixture{movie: metadata.Movie{TMDbID: 9001, Title: "Canonical provider title", Year: &year}, beforeMovie: func() { calls++ }}
	f.s.SetAutomaticMetadata(provider)
	scan := automaticMovieScan(t, f, "Movies/Local folder title (2026) [tmdb-9001]/release.mp4")
	finishAutomatic(t, f, scan.ID)
	result, err := f.s.LibraryScan(context.Background(), scan.ID)
	if err != nil || result.Registered != 1 || calls != 1 || provider.searches != 0 {
		t.Fatal(result, calls, provider.searches, err)
	}
}

func TestMatchingQueueDoesNotWaitForDiskInspection(t *testing.T) {
	f := setupWorkflow(t)
	if _, err := f.db.Exec(`INSERT INTO media_items(kind,tmdb_id,title,year) VALUES('movie',9001,'Second Movie',2026)`); err != nil {
		t.Fatal(err)
	}
	scan := automaticMovieScan(t, f, "Movies/Test Movie (2026)/movie.mp4", "Movies/Second Movie (2026)/movie.mp4")
	started := make(chan struct{})
	f.s.importer.Prober = existingProbeFunc(func(ctx context.Context, _ *os.File) (Probe, error) {
		close(started)
		<-ctx.Done()
		return Probe{}, ctx.Err()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	done := make(chan struct{})
	go func() { defer close(done); f.s.Run(ctx, slog.New(slog.NewTextHandler(io.Discard, nil))) }()
	defer func() { cancel(); <-done }()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("inspection did not start")
	}
	for {
		status, err := f.s.AutomaticMatchStatus(ctx, scan.ID)
		if err != nil {
			t.Fatal(err)
		}
		if status.State == "completed" && status.Queued == 2 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("matching was blocked by inspection")
		case <-time.After(10 * time.Millisecond):
		}
	}
}
