package subtitle

import (
	"context"
	"encoding/json"
	"github.com/faaaaq/ReelScarey/internal/filecheck"
	"os"
	"testing"
	"time"
)

func TestExistingFileHasNoInventedReleaseEvidence(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	if _, e := f.db.Exec(`UPDATE media_files SET origin='existing',download_id=NULL,source_name='Test.Movie.2020.1080p.WEB.mkv' WHERE id=1`); e != nil {
		t.Fatal(e)
	}
	m, e := f.s.media(ctx, 1)
	if e != nil || m.Release != "" {
		t.Fatal("existing file inherited invented release evidence", m.Release, e)
	}
	prefs := Preferences{Tracks: []Track{{Language: "en", Variant: "full"}}, MatchPolicy: "hash_or_release"}
	candidates := []Candidate{{FileID: 1, Track: prefs.Tracks[0], Identity: true, Release: "Test.Movie.2020.1080p.WEB.mkv"}}
	ranked := rank(prefs, m.Target, candidates)
	if len(ranked) != 1 || ranked[0].Automatic {
		t.Fatal("filename alone authorized automatic subtitle", ranked)
	}
	candidates[0].HashMatch = true
	ranked = rank(prefs, m.Target, candidates)
	if !ranked[0].Automatic {
		t.Fatal("confirmed provider hash could not match existing media")
	}
}

func TestFastRegisteredSubtitleInspectionRetainsChangeDetection(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	if err := os.Truncate(f.video, 1<<20); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(f.video)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := filecheck.Capture(ctx, file)
	file.Close()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(snapshot)
	if _, err = f.db.Exec(`UPDATE media_files SET origin='existing',download_id=NULL,size_bytes=?,sha256='',verification_json=? WHERE id=1`, snapshot.Size, string(raw)); err != nil {
		t.Fatal(err)
	}
	m, err := f.s.media(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := f.s.openMedia(ctx, m)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	if opened.moviehash == "" || opened.probe.Codec != "h264" {
		t.Fatal("missing subtitle inspection facts")
	}
	// A second inspection reuses probe data but must still validate the observation.
	cached, err := f.s.openMedia(ctx, m)
	if err != nil {
		t.Fatal(err)
	}
	cached.Close()
	writer, err := os.OpenFile(f.video, os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = writer.WriteAt([]byte("changed"), 0); err != nil {
		t.Fatal(err)
	}
	writer.Close()
	original := time.Unix(0, snapshot.Mtime)
	if err = os.Chtimes(f.video, original, original); err != nil {
		t.Fatal(err)
	}
	if err = opened.unchanged(ctx, m); err == nil {
		t.Fatal("changed video authorized subtitle publication")
	}
	if opened, err = f.s.openMedia(ctx, m); err == nil {
		opened.Close()
		t.Fatal("cached inspection accepted changed content")
	}
	var sha string
	if err = f.db.QueryRow(`SELECT sha256 FROM media_files WHERE id=1`).Scan(&sha); err != nil || sha != "" {
		t.Fatal("subtitle inspection promoted full checksum", err)
	}
}
