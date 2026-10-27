package youtube

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/config"
	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/transfer"
)

// This opt-in test uses owned generated media and a subprocess fixture, not a
// public video or account. Public XG subtitle retrieval is a separate check.
func TestRealYouTubeGeneratedFixture(t *testing.T) {
	if os.Getenv("REELSCAREY_LIVE_YOUTUBE_FIXTURE") != "1" {
		t.Skip("opt-in generated-media acceptance")
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = exec.LookPath("ffprobe"); err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	video := filepath.Join(base, "fixture.mp4")
	cmd := exec.Command(ffmpeg, "-v", "error", "-f", "lavfi", "-i", "color=c=black:s=320x240:r=10", "-t", "1", "-c:v", "libx264", "-pix_fmt", "yuv420p", video)
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("generate video: %v %s", e, out)
	}
	t.Setenv("REELSCAREY_YT_VIDEO", video)
	t.Setenv("REELSCAREY_YT_SUBS", "1")
	inspector := helperInspector(t)
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(base, "config"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	media := filepath.Join(base, "media")
	if err = os.Mkdir(media, 0700); err != nil {
		t.Fatal(err)
	}
	s := NewStore(db)
	c, err := s.CreateCollection(ctx, "Generated")
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.PreviewWithSubtitles(ctx, Video{ID: "abcdefghijk", Title: "Generated"}, Target{Kind: "custom", CollectionID: c.ID}, DownloadOptions{Languages: []string{"en"}})
	if err != nil {
		t.Fatal(err)
	}
	j, err := s.Queue(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	w := Worker{Store: s, Downloader: inspector, Prober: transfer.FFprobe{}, StagingRoot: filepath.Join(base, "staging")}
	if worked, e := w.ProcessOne(ctx); !worked || e != nil {
		t.Fatalf("acquisition: %v %v", worked, e)
	}
	cfg := config.Config{MediaDir: media, ConfigDir: filepath.Join(base, "config")}
	review, err := s.ReviewImport(ctx, cfg, j.ID)
	if err != nil || len(review.Subtitles) != 1 {
		t.Fatalf("review: %+v %v", review, err)
	}
	if err = s.ApproveImport(ctx, cfg, review); err != nil {
		t.Fatal(err)
	}
	if worked, e := w.ProcessImport(ctx, cfg); !worked || e != nil {
		t.Fatalf("import: %v %v", worked, e)
	}
	data, err := os.ReadFile(filepath.Join(media, filepath.FromSlash(review.Subtitles[0].Destination)))
	if err != nil || !strings.HasPrefix(string(data), "WEBVTT") {
		t.Fatalf("subtitle: %q %v", data, err)
	}
	job, err := s.Job(ctx, j.ID)
	if err != nil || job.State != "completed" {
		t.Fatalf("job: %+v %v", job, err)
	}
}
