package transfer

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindArchiveSetsGroupsVolumesAndFormats(t *testing.T) {
	files := []TorrentFile{
		{Index: 0, Name: "Movie/Movie.2024.rar", Size: 100, Progress: 1, Priority: 1},
		{Index: 1, Name: "Movie/Movie.2024.r00", Size: 100, Progress: 1, Priority: 1},
		{Index: 2, Name: "Show/Show.part1.rar", Size: 100, Progress: 1, Priority: 1},
		{Index: 3, Name: "Show/Show.part2.rar", Size: 100, Progress: 1, Priority: 1},
		{Index: 4, Name: "Pack/Pack.zip", Size: 200, Progress: 1, Priority: 1},
		{Index: 5, Name: "Pack/Pack.7z", Size: 300, Progress: 1, Priority: 1},
		{Index: 6, Name: "Split/Split.001", Size: 400, Progress: 1, Priority: 1},
		{Index: 7, Name: "Movie/sample.mkv", Size: 10, Progress: 1, Priority: 1},
	}
	sets, err := findArchiveSets(files)
	if err != nil {
		t.Fatal(err)
	}
	if len(sets) != 5 {
		t.Fatal(sets)
	}
	byKind := map[string]archiveSet{}
	for _, set := range sets {
		byKind[set.Kind+":"+set.First] = set
	}
	if _, ok := byKind["rar:Movie/Movie.2024.rar"]; !ok {
		t.Fatal("old-style RAR set missing", sets)
	}
	if _, ok := byKind["rar:Show/Show.part1.rar"]; !ok {
		t.Fatal("part RAR set missing", sets)
	}
	for _, name := range []string{"zip:Pack/Pack.zip", "7z:Pack/Pack.7z", "split:Split/Split.001"} {
		if _, ok := byKind[name]; !ok {
			t.Fatal(name, sets)
		}
	}
}

func TestFindArchiveSetsRejectsIncompleteVolumes(t *testing.T) {
	for _, name := range []string{"Show.part2.rar", "Show.r01"} {
		if _, err := findArchiveSets([]TorrentFile{{Index: 0, Name: name, Size: 10, Progress: 1, Priority: 1}}); err == nil {
			t.Fatal("incomplete volume accepted", name)
		}
	}
}

func TestArchiveOnlyDownloadImportsAndCleansUp(t *testing.T) {
	if _, err := exec.LookPath("7z"); err != nil {
		t.Skip("7z is unavailable")
	}
	f := setupWorkflow(t)
	ctx := context.Background()
	job := f.queue(t)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("Movie.2024.1080p.mkv")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.Write([]byte("owned generated archive media")); err != nil {
		t.Fatal(err)
	}
	if err = zw.Close(); err != nil {
		t.Fatal(err)
	}
	f.source(t, job, "release.zip", buf.Bytes())
	owner := "fixture-owner"
	if _, err = f.db.Exec(`UPDATE downloads SET lease_owner=?,lease_until=unixepoch()+600 WHERE id=?`, owner, job.ID); err != nil {
		t.Fatal(err)
	}
	job.owner = owner
	files := []TorrentFile{{Index: 0, Name: "release.zip", Size: int64(buf.Len()), Progress: 1, Priority: 1}}
	if err = f.s.processCompleted(ctx, job, "/downloads/ReelScarey/"+job.ID, files, "/downloads"); err != nil {
		t.Fatal(err)
	}
	var saved string
	if err = f.db.QueryRow(`SELECT extract_path FROM downloads WHERE id=?`, job.ID).Scan(&saved); err != nil || saved != "" {
		t.Fatal("extract path retained", saved, err)
	}
	var count int
	if err = f.db.QueryRow(`SELECT COUNT(*) FROM media_files WHERE movie_id=?`, f.movie.ID).Scan(&count); err != nil || count != 1 {
		t.Fatal("archive media not imported", count, err)
	}
}

func TestArchiveFailureClassification(t *testing.T) {
	for output, want := range map[string]string{
		"Enter password:":                      "archive_password",
		"Checksum error in the encrypted file": "archive_password",
		"Missing volume":                       "archive_incomplete",
		"Unexpected end of archive":            "archive_incomplete",
		"CRC failed":                           "archive_invalid",
		"some other failure":                   "archive_extract",
	} {
		err := archiveFailure(output)
		var typed *Error
		if err == nil || !errors.As(err, &typed) || typed.Code != want {
			t.Fatal(output, err)
		}
	}
}

func TestArchiveExtractionAndCleanup(t *testing.T) {
	if _, err := exec.LookPath("7z"); err != nil {
		t.Skip("7z is unavailable")
	}
	f := setupWorkflow(t)
	ctx := context.Background()
	job := f.queue(t)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range map[string]string{
		"Movie.2024.1080p.mkv": "owned generated media",
		"sample.mkv":           "sample",
	} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	f.source(t, job, "release.zip", buf.Bytes())
	files := []TorrentFile{{Index: 0, Name: "release.zip", Size: int64(buf.Len()), Progress: 1, Priority: 1}}
	owner := "fixture-owner"
	if _, err := f.db.Exec(`UPDATE downloads SET lease_owner=?,lease_until=unixepoch()+600 WHERE id=?`, owner, job.ID); err != nil {
		t.Fatal(err)
	}
	job.owner = owner
	extracted, err := f.s.extractArchiveMedia(ctx, job, files, "/downloads/ReelScarey/"+job.ID, "/downloads")
	if err != nil {
		t.Fatal(err)
	}
	if len(extracted) != 1 || !strings.HasSuffix(extracted[0].Name, "Movie.2024.1080p.mkv") || extracted[0].Index != 1 {
		t.Fatal(extracted)
	}
	var saved string
	if err = f.db.QueryRow(`SELECT extract_path FROM downloads WHERE id=?`, job.ID).Scan(&saved); err != nil || saved == "" {
		t.Fatal(saved, err)
	}
	root := filepath.Join(f.cfg.DownloadsDir, filepath.FromSlash(saved))
	if info, err := os.Stat(filepath.Join(root, "set-1", "Movie.2024.1080p.mkv")); err != nil || info.Size() == 0 {
		t.Fatal(err)
	}
	if err = f.s.cleanupExtract(ctx, job); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("extracted folder was not removed", err)
	}
	if err = f.db.QueryRow(`SELECT extract_path FROM downloads WHERE id=?`, job.ID).Scan(&saved); err != nil || saved != "" {
		t.Fatal("extract path not cleared", saved, err)
	}
}
