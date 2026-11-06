package transfer

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestArchiveExtractionRARLive(t *testing.T) {
	if os.Getenv("REELSCAREY_LIVE_ARCHIVE") != "1" {
		t.Skip("requires generated or downloaded RAR fixtures")
	}
	if _, err := exec.LookPath("unrar"); err != nil {
		t.Skip("unrar is unavailable")
	}
	ctx := context.Background()
	for _, fixture := range []struct {
		env  string
		name string
		size int64
	}{
		{"REELSCAREY_RAR_FIXTURE", "elf-Linux-ARMv7-ls", 90808},
		{"REELSCAREY_RAR_VOLUME_FIXTURE", "LibarchiveAddingTest.html", 20111},
	} {
		archive := os.Getenv(fixture.env)
		if archive == "" {
			continue
		}
		out := t.TempDir()
		if err := runArchiveExtractor(ctx, archive, "rar", out); err != nil {
			t.Fatal(fixture.env, err)
		}
		found := false
		err := filepath.WalkDir(out, func(name string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil || entry.IsDir() {
				return walkErr
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if filepath.Base(name) == fixture.name && info.Size() == fixture.size {
				found = true
			}
			return nil
		})
		if err != nil || !found {
			t.Fatal(fixture.env, "extracted fixture missing", err)
		}
	}
	if encrypted := os.Getenv("REELSCAREY_RAR_ENCRYPTED_FIXTURE"); encrypted != "" {
		err := runArchiveExtractor(ctx, encrypted, "rar", t.TempDir())
		var typed *Error
		if err == nil || !errors.As(err, &typed) || typed.Code != "archive_password" {
			t.Fatal("encrypted fixture accepted", err)
		}
	}
}
