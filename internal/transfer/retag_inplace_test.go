package transfer

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/organization"
)

func TestMatroskaTitleExtensionOnlyClaimsMatroskaContainers(t *testing.T) {
	for _, ext := range []string{".mkv", ".webm", ".MKV", ".WebM"} {
		if !MatroskaTitleExtension(ext) {
			t.Fatalf("%q must use the in-place editor", ext)
		}
	}
	for _, ext := range []string{".mp4", ".m4v", ".mov", ".avi", "", ".mkv.part"} {
		if MatroskaTitleExtension(ext) {
			t.Fatalf("%q must keep the verified remux path", ext)
		}
	}
}

func TestEditTitleInPlaceVerifiedRejectsUnsafeInputBeforeRunningATool(t *testing.T) {
	ctx := context.Background()
	// A title the container cannot store safely is refused. The file is never
	// opened, so the check holds even where mkvpropedit is not installed.
	for _, title := range []string{"", "   ", "line\nbreak", "carriage\rreturn", "nul\x00byte"} {
		_, err := EditTitleInPlaceVerified(ctx, "unused.mkv", title, 0)
		if code := problemCode(err); code != "invalid_metadata" {
			t.Fatalf("title %q: code=%q err=%v", title, code, err)
		}
	}
	long := make([]byte, 1025)
	for i := range long {
		long[i] = 'a'
	}
	if code := problemCode(mustErr(EditTitleInPlaceVerified(ctx, "unused.mkv", string(long), 0))); code != "invalid_metadata" {
		t.Fatalf("over-long title: code=%q", code)
	}
}

func TestEditTitleInPlaceVerifiedRequiresLiveProbeBeforeEdit(t *testing.T) {
	// A missing FFprobe must fail the baseline inspection before the editor runs.
	dir := t.TempDir()
	editor := filepath.Join(dir, "mkvpropedit.exe")
	if err := os.WriteFile(editor, []byte("invalid executable"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("REELSCAREY_MKVPROPEDIT_PATH", editor)
	t.Setenv("REELSCAREY_FFPROBE_PATH", filepath.Join(dir, "missing-ffprobe"))
	path := filepath.Join(dir, "movie.mkv")
	if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := EditTitleInPlaceVerified(context.Background(), path, "Title", 7)
	if code := problemCode(err); code != "ffprobe_missing" && code != "invalid_media" {
		t.Fatalf("code=%q err=%v", code, err)
	}
}

func TestEmbeddedTitleWithLegacyTrackIndexesStaysInPlace(t *testing.T) {
	for _, tool := range []string{"ffmpeg", trackTool("REELSCAREY_FFPROBE_PATH", "ffprobe"), trackTool("REELSCAREY_MKVPROPEDIT_PATH", "mkvpropedit")} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("requires %s", tool)
		}
	}
	f := setupWorkflow(t)
	sub := filepath.Join(t.TempDir(), "captions.srt")
	if err := os.WriteFile(sub, []byte("1\n00:00:00,000 --> 00:00:00,400\nFixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	const rel = "Movies/Fixture/movie.mkv"
	path := filepath.Join(f.cfg.MediaDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("ffmpeg", "-nostdin", "-v", "error",
		"-f", "lavfi", "-i", "color=s=64x64:r=5", "-f", "lavfi", "-i", "sine",
		"-i", sub, "-map", "0:v", "-map", "1:a", "-map", "1:a", "-map", "1:a", "-map", "1:a",
		"-map", "2:s", "-map", "2:s", "-map", "2:s", "-t", "0.5",
		"-c:v", "mpeg4", "-c:a", "aac", "-c:s", "srt", path)
	hideProbeWindow(cmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate Matroska fixture: %v: %s", err, out)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	source, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	legacyProbe, err := (FFprobe{}).Probe(ctx, source)
	source.Close()
	if err != nil {
		t.Fatal(err)
	}
	// Version 0.54.29 recorded these same stream facts without the index field.
	for i := range legacyProbe.Audio {
		legacyProbe.Audio[i].Index = 0
	}
	for i := range legacyProbe.Subtitles {
		legacyProbe.Subtitles[i].Index = 0
	}
	legacy, err := json.Marshal(legacyProbe)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.db.Exec(`INSERT INTO media_files(id,movie_id,relative_path,size_bytes,sha256,probe_json,imported_at,origin,source_name)
		VALUES(1,?,?,?,?,?,1,'existing','movie.mkv')`, f.movie.ID, rel, len(data), fmt.Sprintf("%x", sha256.Sum256(data)), string(legacy))
	if err != nil {
		t.Fatal(err)
	}
	service := organization.New(f.db, f.cfg)
	service.SetInPlaceTagger(func(ctx context.Context, path, extension, title string, size int64) (string, error) {
		probe, err := EditTitleInPlaceVerified(ctx, path, title, size)
		if err != nil {
			t.Fatalf("in-place edit: %v", err)
		}
		raw, err := json.Marshal(probe)
		return string(raw), err
	})
	service.SetTagger(func(context.Context, *os.File, *os.File, string, string) (string, error) {
		t.Fatal("legacy inspection triggered a remux")
		return "", nil
	})
	plan, err := service.PreviewEmbeddedTitle(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Apply(ctx, plan.ID); err != nil {
		t.Fatal(err)
	}
	if err = service.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	plan, err = service.Plan(ctx, plan.ID)
	if err != nil || plan.State != "completed" || !plan.Replacement.InPlace {
		t.Fatalf("expected completed in-place edit, state=%s err=%v", plan.State, err)
	}
	var raw string
	if err = f.db.QueryRow("SELECT probe_json FROM media_files WHERE id=1").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var got Probe
	if err = json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatal(err)
	}
	if got.Title == nil || *got.Title != f.movie.Title || len(got.Audio) != 4 || len(got.Subtitles) != 3 {
		t.Fatalf("updated catalog probe: %+v", got)
	}
	for i, stream := range got.Audio {
		if stream.Index != i+1 {
			t.Errorf("audio track %d has index %d", i, stream.Index)
		}
	}
	for i, stream := range got.Subtitles {
		if stream.Index != i+5 {
			t.Errorf("subtitle track %d has index %d", i, stream.Index)
		}
	}
}

func TestVerifyUnchangedStreamsRejectsAnyStructuralChange(t *testing.T) {
	title := "Old"
	base := Probe{
		Title:     &title,
		Codec:     "h264",
		Width:     1920,
		Height:    1080,
		Audio:     []AudioStream{{Codec: "aac", Language: "eng"}},
		Subtitles: []SubtitleStream{{Language: "eng"}},
	}
	if err := verifyUnchangedStreams(base, base); err != nil {
		t.Fatalf("identical probes rejected: %v", err)
	}
	cases := map[string]Probe{
		"codec":         {Codec: "hevc", Width: 1920, Height: 1080, Audio: base.Audio, Subtitles: base.Subtitles},
		"resolution":    {Codec: "h264", Width: 1280, Height: 720, Audio: base.Audio, Subtitles: base.Subtitles},
		"audio count":   {Codec: "h264", Width: 1920, Height: 1080, Audio: nil, Subtitles: base.Subtitles},
		"audio content": {Codec: "h264", Width: 1920, Height: 1080, Audio: []AudioStream{{Codec: "ac3", Language: "eng"}}, Subtitles: base.Subtitles},
		"subtitles":     {Codec: "h264", Width: 1920, Height: 1080, Audio: base.Audio},
	}
	for name, after := range cases {
		if code := problemCode(verifyUnchangedStreams(base, after)); code != "metadata_changed_streams" {
			t.Fatalf("%s: code=%q err=%v", name, code, verifyUnchangedStreams(base, after))
		}
	}
	// A title change is exactly what this function must allow.
	changed := base
	next := "New"
	changed.Title = &next
	if err := verifyUnchangedStreams(base, changed); err != nil {
		t.Fatalf("title-only change rejected: %v", err)
	}
}

func mustErr(_ Probe, err error) error { return err }
