package youtube

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/config"
	"github.com/faaaaq/ReelScarey/internal/database"
	"path"
)

func TestRequiredLanguagesAlwaysIncludesEnglish(t *testing.T) {
	if got := requiredLanguages(nil); len(got) != 1 || got[0] != "en" {
		t.Fatal("empty options must request English", got)
	}
	if got := requiredLanguages([]string{"ja", "ko"}); len(got) != 3 || got[2] != "en" {
		t.Fatal("requested languages lost English", got)
	}
	if got := requiredLanguages([]string{"ja", "en-US"}); len(got) != 2 {
		t.Fatal("existing English variant was duplicated", got)
	}
	// The mandatory English track never drops below the cap, so one extra
	// language is dropped instead.
	long := []string{"ja", "ko", "zh", "fr", "de", "es", "it", "ru"}
	if got := requiredLanguages(long); len(got) != maxSubtitleLanguages || got[7] != "en" || got[6] != "it" {
		t.Fatal("cap handling wrong", got)
	}
	if !isEnglishSubtitle("en") || !isEnglishSubtitle("en-US") || !isEnglishSubtitle("en-GB") {
		t.Fatal("regional English variants must satisfy the requirement")
	}
	if isEnglishSubtitle("ja") || isEnglishSubtitle("eng") || isEnglishSubtitle("english") {
		t.Fatal("non-`en` tags must not satisfy the requirement")
	}
}

// englishlessDownloader serves a verified video but only produces an English
// caption after the test flips the flag, modeling a video whose auto captions
// were not written on the first attempt.
type englishlessDownloader struct {
	hasEnglish bool
}

func (d englishlessDownloader) Download(_ context.Context, id, dir string) (string, error) {
	name := id + ".mp4"
	return name, os.WriteFile(filepath.Join(dir, name), []byte("owned generated video"), 0600)
}

func (d englishlessDownloader) DownloadWithSubtitles(ctx context.Context, id, dir string, opts DownloadOptions) (DownloadResult, error) {
	if !d.hasEnglish {
		if opts.SkipDownload {
			// Subtitle-only retry with nothing available: yt-dlp writes no
			// files, which the real downloader rejects.
			return DownloadResult{}, ErrDownload
		}
		name, err := d.Download(ctx, id, dir)
		if err != nil {
			return DownloadResult{}, err
		}
		return DownloadResult{Video: name, Subtitles: []SubtitleFile{}, MissingLanguages: []string{"en"}}, nil
	}
	if opts.SkipDownload {
		name := id + ".en.vtt"
		if err := os.WriteFile(filepath.Join(dir, name), []byte("WEBVTT\n\n00:00:00.000 --> 00:00:01.000\nGenerated English\n"), 0600); err != nil {
			return DownloadResult{}, err
		}
		return DownloadResult{Subtitles: []SubtitleFile{{Language: "en", Name: name}}, MissingLanguages: []string{}}, nil
	}
	if err := os.WriteFile(filepath.Join(dir, id+".mp4"), []byte("owned generated video"), 0600); err != nil {
		return DownloadResult{}, err
	}
	name := id + ".en.vtt"
	if err := os.WriteFile(filepath.Join(dir, name), []byte("WEBVTT\n\n00:00:00.000 --> 00:00:01.000\nGenerated English\n"), 0600); err != nil {
		return DownloadResult{}, err
	}
	return DownloadResult{Video: id + ".mp4", Subtitles: []SubtitleFile{{Language: "en", Name: name}}, MissingLanguages: []string{}}, nil
}

// TestEnglishSubtitlesGateImportAndRetry proves the mandatory policy end to
// end: a verified video without English captions is never importable, the
// failure is explicit, and a subtitle-only retry recovers the job without
// redownloading or replacing the video.
func TestEnglishSubtitlesGateImportAndRetry(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
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
	c, err := s.CreateCollection(ctx, "Generated collection")
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.Preview(ctx, Video{ID: "abcdefghijk", Title: "Generated"}, Target{Kind: "custom", CollectionID: c.ID})
	if err != nil {
		t.Fatal(err)
	}
	j, err := s.Queue(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	staging := filepath.Join(base, "staging")
	downloader := englishlessDownloader{}
	w := Worker{Store: s, Downloader: downloader, Prober: testProber{}, StagingRoot: staging}
	if _, err = w.ProcessOne(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := s.Job(ctx, j.ID)
	if err != nil || got.State != "awaiting_review" {
		t.Fatal("verified video must wait for review", got, err)
	}
	if !strings.Contains(got.Message, "English subtitles are unavailable") {
		t.Fatal("missing English subtitles not reported honestly", got.Message)
	}
	cfg := config.Config{MediaDir: media}
	review, err := s.ReviewImport(ctx, cfg, j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ApproveImport(ctx, cfg, review); err != ErrEnglishSubtitles {
		t.Fatal("import without English subtitles must be blocked", err)
	}
	jobs, err := s.Jobs(ctx)
	if err != nil || len(jobs) != 1 || jobs[0].EnglishSubtitles {
		t.Fatal("jobs list must expose the missing-English state", jobs, err)
	}
	// A failed retry keeps the verified video and reports honestly.
	if retried, err := w.RetrySubtitles(ctx, j.ID); !retried || err != nil {
		t.Fatal("failed retry", retried, err)
	}
	got, err = s.Job(ctx, j.ID)
	if err != nil || !strings.Contains(got.Message, "retry failed") {
		t.Fatal("failed retry message", got, err)
	}
	if err = s.ApproveImport(ctx, cfg, review); err != ErrEnglishSubtitles {
		t.Fatal("import still blocked after failed retry", err)
	}
	// Subtitles become available: the retry fetches them without touching
	// the video, and the import completes.
	downloader.hasEnglish = true
	w.Downloader = downloader
	var videoBefore string
	if err = db.QueryRow(`SELECT source_sha256 FROM youtube_jobs WHERE id=?`, j.ID).Scan(&videoBefore); err != nil {
		t.Fatal(err)
	}
	if retried, err := w.RetrySubtitles(ctx, j.ID); !retried || err != nil {
		t.Fatal("successful retry", retried, err)
	}
	var videoAfter string
	if err = db.QueryRow(`SELECT source_sha256 FROM youtube_jobs WHERE id=?`, j.ID).Scan(&videoAfter); err != nil {
		t.Fatal(err)
	}
	if videoAfter != videoBefore || videoAfter == "" {
		t.Fatal("video evidence changed during subtitle retry", videoBefore, videoAfter)
	}
	if got, err = s.Job(ctx, j.ID); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Message, "English subtitles acquired") {
		t.Fatal("acquired message", got.Message)
	}
	review, err = s.ReviewImport(ctx, cfg, j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ApproveImport(ctx, cfg, review); err != nil {
		t.Fatal("import with English subtitles", err)
	}
	if worked, err := w.ProcessImport(ctx, cfg); !worked || err != nil {
		t.Fatalf("import: %v %v", worked, err)
	}
	got, err = s.Job(ctx, j.ID)
	if err != nil || got.State != "completed" {
		t.Fatal("completed with English subtitles", got, err)
	}
	var languages int
	if err = db.QueryRow(`SELECT COUNT(*) FROM youtube_subtitle_files WHERE job_id=? AND language='en'`, j.ID).Scan(&languages); err != nil {
		t.Fatal(err)
	}
	if languages != 1 {
		t.Fatal("published English sidecar record missing", languages)
	}
	sidecar := filepath.Join(media, filepath.FromSlash(strings.TrimSuffix(review.Destination, path.Ext(review.Destination))+".en.vtt"))
	data, err := os.ReadFile(sidecar)
	if err != nil || !strings.HasPrefix(string(data), "WEBVTT") {
		t.Fatal("published English sidecar file missing", sidecar, err)
	}
}
