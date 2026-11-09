package mediaanalysis

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/faaaaq/ReelScarey/internal/database"
)

func TestProcessOneReturnsNoWorkForEmptyQueue(t *testing.T) {
	db, media := setup(t)
	worked, err := (&Worker{DB: db, MediaRoot: media}).ProcessOne(context.Background())
	if err != nil || worked {
		t.Fatalf("empty queue: worked=%v err=%v", worked, err)
	}
}

func TestProcessOneInspectsGeneratedMedia(t *testing.T) {
	for _, tool := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip(tool + " is not available")
		}
	}
	ctx := context.Background()
	db, media := setup(t)
	dir := filepath.Join(media, "Movies")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(dir, "Generated.mp4")
	if output, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "color=c=black:s=64x64:r=10", "-t", "0.5", "-c:v", "mpeg4", "-movflags", "faststart", name).CombinedOutput(); err != nil {
		t.Fatalf("generate fixture: %v %s", err, output)
	}
	info, err := os.Stat(name)
	if err != nil {
		t.Fatal(err)
	}
	insertFile(t, db, 1, "Movies/Generated.mp4", info.Size())

	worked, err := (&Worker{DB: db, MediaRoot: media}).ProcessOne(ctx)
	if err != nil || !worked {
		t.Fatalf("analyze generated video: worked=%v err=%v", worked, err)
	}
	row := readRow(t, db, 1)
	if row.state != "ok" {
		t.Fatalf("state = %q, json = %q", row.state, row.json)
	}
	if row.probe != "{}" {
		t.Fatalf("probe_json was overwritten: %q", row.probe)
	}
	var result Analysis
	if err = json.Unmarshal([]byte(row.json), &result); err != nil {
		t.Fatalf("analysis_json is not valid JSON: %v (%q)", err, row.json)
	}
	if result.Width != 64 || result.Height != 64 || result.DurationSeconds <= 0 {
		t.Fatalf("unexpected analysis: %+v", result)
	}
	if row.at == 0 {
		t.Fatal("analysis_at was not stamped")
	}
}

func TestProcessOneRecordsFakeProbeSuccess(t *testing.T) {
	db, media := setup(t)
	file := writeMedia(t, media, "Movies/Probe.mkv")
	insertFile(t, db, 1, "Movies/Probe.mkv", int64(len(file)))
	body := `{"format":{"duration":"12.5","format_name":"matroska,webm"},"streams":[{"codec_type":"video","codec_name":"h264","width":1920,"height":1080,"color_transfer":"smpte2084"},{"codec_type":"audio","tags":{"language":"eng"}},{"codec_type":"audio","tags":{"language":"eng"}},{"codec_type":"subtitle","tags":{"language":"jpn"}}]}`
	w := &Worker{DB: db, MediaRoot: media, Binary: fakeTool(t, t.TempDir(), "ok", "printf '%s\\n' '"+body+"'", "echo "+body)}
	worked, err := w.ProcessOne(context.Background())
	if err != nil || !worked {
		t.Fatalf("fake probe: worked=%v err=%v", worked, err)
	}
	row := readRow(t, db, 1)
	if row.state != "ok" {
		t.Fatalf("state = %q json = %q", row.state, row.json)
	}
	var result Analysis
	if err = json.Unmarshal([]byte(row.json), &result); err != nil {
		t.Fatal(err)
	}
	if result.VideoCodec != "h264" || result.Width != 1920 || result.Height != 1080 || !result.HDR || result.Container != "matroska,webm" {
		t.Fatalf("unexpected analysis: %+v", result)
	}
	if len(result.AudioLanguages) != 1 || result.AudioLanguages[0] != "eng" || len(result.SubtitleLanguages) != 1 || result.SubtitleLanguages[0] != "jpn" {
		t.Fatalf("unexpected stream languages: %+v", result)
	}
	if len(row.json) > maxAnalysisJSON {
		t.Fatalf("analysis_json is not bounded: %d bytes", len(row.json))
	}
}

func TestProcessOneRecordsFailures(t *testing.T) {
	tests := []struct {
		name    string
		binary  func(t *testing.T) string
		wantErr error
		want    string
	}{
		{
			name: "unreadable file",
			binary: func(t *testing.T) string {
				return fakeTool(t, t.TempDir(), "fail", "echo boom >&2\nexit 1", "echo boom 1>&2\r\nexit /b 1")
			},
			wantErr: ErrProbeUnreadable,
			want:    "could not read",
		},
		{
			name:    "invalid json",
			binary:  func(t *testing.T) string { return fakeTool(t, t.TempDir(), "bad", "echo not-json", "echo not-json") },
			wantErr: ErrProbeInvalid,
			want:    "invalid media information",
		},
		{
			name: "missing executable",
			binary: func(t *testing.T) string {
				return filepath.Join(t.TempDir(), "missing-ffprobe")
			},
			wantErr: ErrProbeMissing,
			want:    "not installed",
		},
		{
			name: "no video stream",
			binary: func(t *testing.T) string {
				return fakeTool(t, t.TempDir(), "audio-only", `echo '{"format":{"duration":"3"},"streams":[{"codec_type":"audio"}]}'`, `echo {"format":{"duration":"3"},"streams":[{"codec_type":"audio"}]}`)
			},
			wantErr: ErrNoVideo,
			want:    "no usable video stream",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, media := setup(t)
			file := writeMedia(t, media, "Movies/Movie.mkv")
			insertFile(t, db, 1, "Movies/Movie.mkv", int64(len(file)))
			worked, err := (&Worker{DB: db, MediaRoot: media, Binary: test.binary(t)}).ProcessOne(context.Background())
			if !worked || !errors.Is(err, test.wantErr) {
				t.Fatalf("worked=%v err=%v, want %v", worked, err, test.wantErr)
			}
			row := readRow(t, db, 1)
			if row.state != "failed" {
				t.Fatalf("state = %q", row.state)
			}
			var failure failure
			if json.Unmarshal([]byte(row.json), &failure) != nil || failure.Error == "" {
				t.Fatalf("failure JSON = %q", row.json)
			}
			if !contains(failure.Error, test.want) || row.probe != "{}" {
				t.Fatalf("failure = %q, probe_json = %q", failure.Error, row.probe)
			}
		})
	}
}

func TestProcessOneKillsWedgedProbe(t *testing.T) {
	db, media := setup(t)
	file := writeMedia(t, media, "Movies/Wedged.mkv")
	insertFile(t, db, 1, "Movies/Wedged.mkv", int64(len(file)))
	hang := fakeTool(t, t.TempDir(), "hang", "exec sleep 30", "ping -n 30 127.0.0.1 >nul")
	w := &Worker{DB: db, MediaRoot: media, Binary: hang, Timeout: 100 * time.Millisecond}
	started := time.Now()
	worked, err := w.ProcessOne(context.Background())
	if !worked || !errors.Is(err, ErrProbeTimeout) {
		t.Fatalf("worked=%v err=%v", worked, err)
	}
	if elapsed := time.Since(started); elapsed > 10*time.Second {
		t.Fatalf("probe was not killed promptly: %v", elapsed)
	}
	row := readRow(t, db, 1)
	if row.state != "failed" || !contains(row.json, "timed out") {
		t.Fatalf("state = %q json = %q", row.state, row.json)
	}
}

func TestProcessOneReleasesCancelledRow(t *testing.T) {
	db, media := setup(t)
	file := writeMedia(t, media, "Movies/Cancelled.mkv")
	insertFile(t, db, 1, "Movies/Cancelled.mkv", int64(len(file)))
	hang := fakeTool(t, t.TempDir(), "hang", "exec sleep 30", "ping -n 30 127.0.0.1 >nul")
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(150 * time.Millisecond)
		cancel()
	}()
	worked, err := (&Worker{DB: db, MediaRoot: media, Binary: hang, Timeout: time.Minute}).ProcessOne(ctx)
	if !worked || !errors.Is(err, context.Canceled) {
		t.Fatalf("worked=%v err=%v", worked, err)
	}
	row := readRow(t, db, 1)
	if row.state != "pending" || row.json != "" {
		t.Fatalf("cancelled row was not released: state=%q json=%q", row.state, row.json)
	}
}

func TestProcessOneLeavesRowsQueuedWhenMediaRootIsUnavailable(t *testing.T) {
	db, media := setup(t)
	writeMedia(t, media, "Movies/Movie.mkv")
	insertFile(t, db, 1, "Movies/Movie.mkv", 1)
	if err := os.RemoveAll(media); err != nil {
		t.Fatal(err)
	}
	worked, err := (&Worker{DB: db, MediaRoot: media}).ProcessOne(context.Background())
	if !worked || !errors.Is(err, ErrMediaRoot) {
		t.Fatalf("worked=%v err=%v", worked, err)
	}
	if row := readRow(t, db, 1); row.state != "pending" || row.json != "" {
		t.Fatalf("row was not left queued: state=%q json=%q", row.state, row.json)
	}
}

func TestProcessOneRejectsUnsafeAndMissingPaths(t *testing.T) {
	t.Run("traversal", func(t *testing.T) {
		db, media := setup(t)
		outside := filepath.Join(filepath.Dir(media), "outside.mkv")
		if err := os.WriteFile(outside, []byte("not media"), 0o600); err != nil {
			t.Fatal(err)
		}
		insertFile(t, db, 1, "../outside.mkv", 1)
		worked, err := (&Worker{DB: db, MediaRoot: media}).ProcessOne(context.Background())
		if !worked || !errors.Is(err, ErrUnsafePath) {
			t.Fatalf("traversal: worked=%v err=%v", worked, err)
		}
		if row := readRow(t, db, 1); row.state != "failed" || !contains(row.json, "not safe") {
			t.Fatalf("traversal row: state=%q json=%q", row.state, row.json)
		}
	})

	t.Run("missing file", func(t *testing.T) {
		db, media := setup(t)
		insertFile(t, db, 1, "Movies/Missing.mkv", 1)
		worked, err := (&Worker{DB: db, MediaRoot: media}).ProcessOne(context.Background())
		if !worked || !errors.Is(err, ErrMissingFile) {
			t.Fatalf("missing file: worked=%v err=%v", worked, err)
		}
	})

	t.Run("symbolic link", func(t *testing.T) {
		db, media := setup(t)
		target := writeMedia(t, media, "Movies/Target.mkv")
		link := filepath.Join(media, "Movies", "Link.mkv")
		if err := os.Symlink(target, link); err != nil {
			t.Skipf("symlink creation unavailable: %v", err)
		}
		insertFile(t, db, 1, "Movies/Link.mkv", 1)
		worked, err := (&Worker{DB: db, MediaRoot: media}).ProcessOne(context.Background())
		if !worked || !errors.Is(err, ErrUnsafePath) {
			t.Fatalf("symlink: worked=%v err=%v", worked, err)
		}
		if row := readRow(t, db, 1); row.state != "failed" || !contains(row.json, "not safe") {
			t.Fatalf("symlink row: state=%q json=%q", row.state, row.json)
		}
	})
}

func TestRunStopsWhenContextIsCancelled(t *testing.T) {
	db, media := setup(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		(&Worker{DB: db, MediaRoot: media}).Run(ctx, nil)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop after cancellation")
	}
}

type savedRow struct {
	state string
	json  string
	probe string
	at    int64
}

func readRow(t *testing.T, db *sql.DB, id int64) savedRow {
	t.Helper()
	var row savedRow
	if err := db.QueryRow(`SELECT analysis_state,analysis_json,probe_json,analysis_at FROM media_files WHERE id=?`, id).Scan(&row.state, &row.json, &row.probe, &row.at); err != nil {
		t.Fatal(err)
	}
	return row
}

func setup(t *testing.T) (*sql.DB, string) {
	t.Helper()
	ctx := context.Background()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err = db.ExecContext(ctx, `INSERT INTO media_items(id,kind,tmdb_id,title) VALUES(1,'movie',1,'Fixture')`); err != nil {
		t.Fatal(err)
	}
	return db, t.TempDir()
}

func insertFile(t *testing.T, db *sql.DB, id int64, relative string, size int64) {
	t.Helper()
	if size < 1 {
		size = 1
	}
	_, err := db.Exec(`INSERT INTO media_files(id,movie_id,relative_path,size_bytes,sha256,probe_json,imported_at,origin,verification_json) VALUES(?,1,?,?,'','{}',1,'existing','')`, id, relative, size)
	if err != nil {
		t.Fatal(err)
	}
}

func writeMedia(t *testing.T, root, relative string) string {
	t.Helper()
	name := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte("placeholder media"), 0o600); err != nil {
		t.Fatal(err)
	}
	return name
}

// fakeTool creates a small executable that stands in for FFprobe. Windows
// shells and POSIX shells need different bodies, so each caller passes both.
func fakeTool(t *testing.T, dir, name, unixBody, windowsBody string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		name = filepath.Join(dir, name+".cmd")
		if err := os.WriteFile(name, []byte("@echo off\r\n"+windowsBody+"\r\n"), 0o700); err != nil {
			t.Fatal(err)
		}
		return name
	}
	name = filepath.Join(dir, name+".sh")
	if err := os.WriteFile(name, []byte("#!/bin/sh\n"+unixBody+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return name
}

func contains(value, part string) bool {
	for i := 0; i+len(part) <= len(value); i++ {
		if value[i:i+len(part)] == part {
			return true
		}
	}
	return false
}
