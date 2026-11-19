//go:build linux

package transfer

import (
	"context"
	"encoding/json"
	"github.com/faaaaq/ReelScarey/internal/filecheck"
	"github.com/faaaaq/ReelScarey/internal/organization"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

// generateTrackFixture writes a Matroska file with one video, two audio tracks
// (eng, spa) and two subtitle tracks (eng, spa), in that stream order.
func generateTrackFixture(t *testing.T, path string) {
	t.Helper()
	dir := filepath.Dir(path)
	for _, lang := range []string{"eng", "spa"} {
		body := "1\n00:00:00,000 --> 00:00:00,400\n" + lang + "\n"
		if err := os.WriteFile(filepath.Join(dir, lang+".srt"), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	args := []string{
		"-v", "error", "-nostdin",
		"-f", "lavfi", "-t", "1", "-i", "color=c=black:s=64x64:r=5",
		"-f", "lavfi", "-t", "1", "-i", "sine=frequency=200",
		"-f", "lavfi", "-t", "1", "-i", "sine=frequency=400",
		"-i", filepath.Join(dir, "eng.srt"),
		"-i", filepath.Join(dir, "spa.srt"),
		"-map", "0:v", "-map", "1:a", "-map", "2:a", "-map", "3", "-map", "4",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-c:s", "srt",
		"-metadata:s:a:0", "language=eng", "-metadata:s:a:1", "language=spa",
		"-metadata:s:s:0", "language=eng", "-metadata:s:s:1", "language=spa",
		"-shortest", "-y", path,
	}
	if b, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg: %v %s", err, b)
	}
}

func TestLiveMatroskaDisableTracksWithLegacyProbe(t *testing.T) {
	if os.Getenv("REELSCAREY_LIVE_TRACKS") != "1" {
		t.Skip("requires ffmpeg and mkvtoolnix")
	}
	if !MatroskaDisableToolsAvailable() {
		t.Fatal("mkvmerge/mkvpropedit missing")
	}
	ctx := context.Background()
	src := filepath.Join(t.TempDir(), "original.mkv")
	generateTrackFixture(t, src)
	f, err := os.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	live, err := (FFprobe{}).Probe(ctx, f)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	// Simulate catalog JSON written before indexes and default/forced flags
	// were recorded, rather than merely zeroing the current Go fields.
	raw, err := json.Marshal(live)
	if err != nil {
		t.Fatal(err)
	}
	var legacy map[string]any
	if err = json.Unmarshal(raw, &legacy); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"audio_streams", "subtitle_streams"} {
		for _, value := range legacy[key].([]any) {
			stream := value.(map[string]any)
			delete(stream, "index")
			delete(stream, "default")
			delete(stream, "forced")
		}
	}
	raw, err = json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	var old Probe
	if err = json.Unmarshal(raw, &old); err != nil {
		t.Fatal(err)
	}
	if verifyUnchangedStreams(old, live) == nil {
		t.Fatal("fixture does not reproduce the legacy comparison mismatch")
	}
	for _, mode := range []string{"audio", "subtitles", "unexpected_audio_change"} {
		t.Run(mode, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "edit.mkv")
			copyFixture(t, src, path)
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			beforeIdent, err := matroskaIdentify(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			index, kind := 4, "subtitles"
			if mode == "audio" {
				index, kind = 2, "audio"
			}
			if mode == "unexpected_audio_change" {
				tool, err := exec.LookPath("mkvpropedit")
				if err != nil {
					t.Fatal(err)
				}
				wrapper := filepath.Join(t.TempDir(), "editor")
				body := "#!/bin/sh\nset -eu\n\"$REAL_MKVPROPEDIT\" \"$@\"\n\"$REAL_MKVPROPEDIT\" \"$1\" --edit track:a1 --set language=fra\n"
				if err := os.WriteFile(wrapper, []byte(body), 0700); err != nil {
					t.Fatal(err)
				}
				t.Setenv("REAL_MKVPROPEDIT", tool)
				t.Setenv("REELSCAREY_MKVPROPEDIT_PATH", wrapper)
			}
			result, err := DisableTracksMatroskaInPlaceVerified(ctx, path, string(raw), []int{index}, info.Size())
			if mode == "unexpected_audio_change" {
				if problemCode(err) != "metadata_changed_streams" {
					t.Fatalf("unexpected audio change accepted: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var current Probe
			if err := json.Unmarshal([]byte(result), &current); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(current, live) {
				t.Fatalf("returned probe lost current indexes/flags: got %+v want %+v", current, live)
			}
			afterIdent, err := matroskaIdentify(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			if err := verifyDisabled(beforeIdent, afterIdent, []mkvTarget{{Kind: kind, Ordinal: 2}}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func copyFixture(t *testing.T, src, dst string) {
	t.Helper()
	in, err := os.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	if _, err = io.Copy(out, in); err != nil {
		t.Fatal(err)
	}
}

// audioSubtitleLayout lists the audio and subtitle tracks in order.
func audioSubtitleLayout(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	m, err := inspectManifest(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, s := range m.Streams {
		if s.Type == "audio" || s.Type == "subtitle" {
			out = append(out, s.Type+":"+s.Tags["language"])
		}
	}
	return out
}

// TestLiveMatroskaDisableTracksInPlace exercises the real mkvpropedit in-place
// disable against a generated Matroska file, verified through mkvmerge. It is
// opt-in because it needs ffmpeg and mkvtoolnix.
func TestLiveMatroskaDisableTracksInPlace(t *testing.T) {
	if os.Getenv("REELSCAREY_LIVE_TRACKS") != "1" {
		t.Skip("requires ffmpeg and mkvtoolnix")
	}
	if !MatroskaDisableToolsAvailable() {
		t.Skip("mkvmerge/mkvpropedit missing")
	}
	ctx := context.Background()
	dir := t.TempDir()
	src := filepath.Join(dir, "in.mkv")
	generateTrackFixture(t, src)
	if got := audioSubtitleLayout(t, src); len(got) != 4 {
		t.Fatalf("fixture layout: %v", got)
	}
	dis := filepath.Join(dir, "dis.mkv")
	copyFixture(t, src, dis)
	info, err := os.Stat(dis)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(dis)
	if err != nil {
		t.Fatal(err)
	}
	probe, err := (FFprobe{}).Probe(ctx, f)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(probe)
	if err != nil {
		t.Fatal(err)
	}
	// Disable the second audio track (ffprobe index 2) in place.
	if _, err := DisableTracksMatroskaInPlaceVerified(ctx, dis, string(raw), []int{2}, info.Size()); err != nil {
		t.Fatalf("disable: %v", err)
	}
	ident, err := matroskaIdentify(ctx, dis)
	if err != nil {
		t.Fatal(err)
	}
	audio, disabled := 0, 0
	for _, track := range ident.Tracks {
		if track.Type != "audio" {
			continue
		}
		audio++
		if !enabledOf(track) {
			disabled++
		}
	}
	if audio != 2 || disabled != 1 {
		t.Fatalf("audio=%d disabled=%d", audio, disabled)
	}
	// Every track is still present (nothing deleted).
	if got := audioSubtitleLayout(t, dis); len(got) != 4 {
		t.Fatalf("disable changed the track list: %v", got)
	}
	// The header edit is small and the disabled file is still readable.
	after, err := os.Stat(dis)
	if err != nil {
		t.Fatal(err)
	}
	if delta := after.Size() - info.Size(); delta > 1<<20 || delta < -1<<20 {
		t.Fatalf("unexpected size change: %d", delta)
	}
}

func TestLiveMatroskaBoundedCleanupJournal(t *testing.T) {
	if os.Getenv("REELSCAREY_LIVE_TRACKS") != "1" {
		t.Skip("requires real MKVToolNix")
	}
	ctx := context.Background()
	f := setupWorkflow(t)
	const rel = "Movies/Fixture/movie.mkv"
	full := filepath.Join(f.cfg.MediaDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
		t.Fatal(err)
	}
	generateTrackFixture(t, full)
	input, err := os.Open(full)
	if err != nil {
		t.Fatal(err)
	}
	probe, err := (FFprobe{}).Probe(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	observation, err := filecheck.Capture(ctx, input)
	input.Close()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(probe)
	verification, _ := json.Marshal(observation)
	_, err = f.db.Exec(`INSERT INTO media_files(id,movie_id,relative_path,size_bytes,sha256,probe_json,verification_json,imported_at,origin,source_name) VALUES(1,?,?,?,'',?,?,1,'existing','movie.mkv')`, f.movie.ID, rel, observation.Size, string(raw), string(verification))
	if err != nil {
		t.Fatal(err)
	}
	service := organization.New(f.db, f.cfg)
	service.SetTrackCleaner(ReadCleanupStreams, nil)
	service.SetMatroskaTrackDisabler(DisableTracksMatroskaInPlaceVerified)
	plan, err := service.PreviewTrackCleanup(ctx, organization.TrackCleanupRequest{Mode: "all_subtitles", Action: "disable"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Apply(ctx, plan.ID); err != nil {
		t.Fatal(err)
	}
	if err = service.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	service = organization.New(f.db, f.cfg)
	if err = service.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	plan, err = service.Plan(ctx, plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if plan.State != "completed" || plan.TrackCleanup.Disabled != 2 || plan.TrackCleanup.Skipped != 0 {
		t.Fatalf("cleanup state=%s result=%+v", plan.State, plan.TrackCleanup)
	}
	var sha, saved string
	if err = f.db.QueryRow(`SELECT sha256,verification_json FROM media_files WHERE id=1`).Scan(&sha, &saved); err != nil {
		t.Fatal(err)
	}
	if sha != "" || saved == "{}" {
		t.Fatalf("full checksum claimed for bounded edit: %q %s", sha, saved)
	}
	ident, err := matroskaIdentify(ctx, full)
	if err != nil {
		t.Fatal(err)
	}
	for _, track := range ident.Tracks {
		if enabledOf(track) == (track.Type == "subtitles") {
			t.Fatalf("unexpected enabled flag: %+v", track)
		}
	}
}

func TestLiveMatroskaUnicodePathWithCLocale(t *testing.T) {
	if os.Getenv("REELSCAREY_LIVE_TRACKS") != "1" {
		t.Skip("requires real MKVToolNix")
	}
	t.Setenv("LC_ALL", "C")
	path := filepath.Join(t.TempDir(), "Nausicaä – 字幕.mkv")
	generateTrackFixture(t, path)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DisableTracksMatroskaInPlaceVerified(context.Background(), path, `{}`, []int{4}, info.Size()); err != nil {
		t.Fatal(err)
	}
	ident, err := matroskaIdentify(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if enabledOf(ident.Tracks[4]) {
		t.Fatal("unicode path track remained enabled")
	}
}
