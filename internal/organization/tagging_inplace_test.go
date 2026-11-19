package organization

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// An in-place Matroska title edit rewrites the header through the injected fast
// editor, records the post-edit facts, keeps no video copy, and never falls
// back to the full demux/remux when the tool is present.
func TestInPlaceEmbeddedTitleUpdatesHeaderWithoutASecondCopy(t *testing.T) {
	ctx := context.Background()
	f := newReviewFixture(t)
	before := []byte("matroska-bytes-before")
	after := []byte("matroska-bytes-after-the-title-edit")
	const rel = "Movies/Film/Film.mkv"
	f.movie(t, 1, rel, before)

	var sawPath string
	f.s.SetInPlaceTagger(func(ctx context.Context, path, extension, title string, beforeSize int64) (string, error) {
		if extension != ".mkv" || title != "Review movie" || beforeSize != int64(len(before)) {
			t.Fatalf("in-place call: ext=%q title=%q size=%d", extension, title, beforeSize)
		}
		sawPath = path
		if e := os.WriteFile(path, after, 0600); e != nil {
			return "", e
		}
		return `{"embedded_title":"Review movie"}`, nil
	})
	// A slow remux must never run for a Matroska file when the fast tool exists.
	f.s.SetTagger(func(context.Context, *os.File, *os.File, string, string) (string, error) {
		t.Fatal("Matroska title update fell back to the remux stage")
		return "", nil
	})

	p, err := f.s.PreviewEmbeddedTitle(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.Apply(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if err = f.s.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	p, err = f.s.Plan(ctx, p.ID)
	if err != nil || p.State != "completed" || !p.Replacement.InPlace {
		t.Fatalf("plan: in_place=%v state=%s err=%v", p.Replacement.InPlace, p.State, err)
	}
	if !strings.HasSuffix(sawPath, filepath.FromSlash(rel)) {
		t.Fatalf("in-place editor saw %q", sawPath)
	}
	got, err := os.ReadFile(filepath.Join(f.cfg.MediaDir, filepath.FromSlash(rel)))
	if err != nil || string(got) != string(after) {
		t.Fatalf("registered file was not edited in place: %q err=%v", got, err)
	}
	// The catalog records the exact post-edit size and checksum.
	sum := sha256.Sum256(after)
	var size int64
	var sha string
	if err = f.db.QueryRow(`SELECT size_bytes,sha256 FROM media_files WHERE id=1`).Scan(&size, &sha); err != nil || size != int64(len(after)) || sha != hex.EncodeToString(sum[:]) {
		t.Fatalf("catalog facts: size=%d sha=%s err=%v", size, sha, err)
	}
	// The header backup stage is removed once the plan commits.
	if _, err = os.Stat(filepath.Join(f.cfg.MediaDir, filepath.FromSlash(p.Replacement.TagStage))); !os.IsNotExist(err) {
		t.Fatalf("stage retained: %v", err)
	}
}

// A refused in-place edit restores the saved header and hands the file to the
// verified remux path, so one unsupported file never fails the whole run.
func TestInPlaceTitleFailureRestoresHeaderAndFallsBackToRemux(t *testing.T) {
	ctx := context.Background()
	f := newReviewFixture(t)
	original := []byte("matroska-bytes-original")
	const rel = "Movies/Film/Film.mkv"
	f.movie(t, 1, rel, original)

	f.s.SetInPlaceTagger(func(ctx context.Context, path, extension, title string, beforeSize int64) (string, error) {
		// Simulate a tool that began editing then failed: the header is dirty.
		if e := os.WriteFile(path, []byte("clobbered"), 0600); e != nil {
			return "", e
		}
		return "", &Error{Code: "metadata_unsupported", Message: "unsupported"}
	})
	f.s.SetTagger(func(ctx context.Context, source, stage *os.File, extension, title string) (string, error) {
		if e := stage.Truncate(0); e != nil {
			return "", e
		}
		if _, e := stage.WriteAt([]byte("remuxed-with-title"), 0); e != nil {
			return "", e
		}
		return `{"embedded_title":"Review movie"}`, nil
	})

	p, err := f.s.PreviewEmbeddedTitle(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.Apply(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if err = f.s.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	p, err = f.s.Plan(ctx, p.ID)
	if err != nil || p.State != "completed" || p.Replacement.InPlace {
		t.Fatalf("plan: in_place=%v state=%s err=%v", p.Replacement.InPlace, p.State, err)
	}
	got, err := os.ReadFile(filepath.Join(f.cfg.MediaDir, filepath.FromSlash(rel)))
	if err != nil || string(got) != "remuxed-with-title" {
		t.Fatalf("fallback remux did not publish: %q err=%v", got, err)
	}
	if _, err = os.Stat(filepath.Join(f.cfg.MediaDir, filepath.FromSlash(p.Replacement.TagStage))); !os.IsNotExist(err) {
		t.Fatalf("stage retained after fallback: %v", err)
	}
}
