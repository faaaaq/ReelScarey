package organization

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestTrackMetadataUsesReviewedInPlaceJournal(t *testing.T) {
	ctx := context.Background()
	f := newReviewFixture(t)
	const rel = "Movies/Film/Film.mkv"
	before := []byte("owned matroska bytes before")
	after := []byte("owned matroska bytes after")
	f.movie(t, 1, rel, before)
	title, language := "Commentary", "en-US"
	defaultFlag, forcedFlag := false, true
	edits := []TrackMetadataEdit{{Index: 2, Title: &title, Language: &language, Default: &defaultFlag, Forced: &forcedFlag}}
	called := false
	f.s.SetMatroskaTrackEditor(func(ctx context.Context, path, beforeProbe string, got []TrackMetadataEdit, beforeSize int64) (string, error) {
		called = true
		if beforeSize != int64(len(before)) || len(got) != 1 || got[0].Index != 2 || got[0].Title == nil || *got[0].Title != title || got[0].Language == nil || *got[0].Language != language || got[0].Default == nil || *got[0].Default || got[0].Forced == nil || !*got[0].Forced {
			t.Fatalf("unexpected editor input: %#v", got)
		}
		if err := os.WriteFile(path, after, 0600); err != nil {
			return "", err
		}
		return `{"audio_streams":[{"index":2,"language":"en-US","title":"Commentary","default":false,"forced":true}]}`, nil
	})
	f.s.SetTagger(func(context.Context, *os.File, *os.File, string, string) (string, error) {
		t.Fatal("track field edit unexpectedly fell back to title remux")
		return "", nil
	})

	plan, err := f.s.PreviewTrackMetadata(ctx, 1, edits)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Replacement.TrackEdits) != 1 || plan.Replacement.TrackEdits[0].Title == nil || *plan.Replacement.TrackEdits[0].Title != title {
		t.Fatalf("edit was not durably reviewed: %#v", plan.Replacement)
	}
	if called {
		t.Fatal("preview invoked the file editor")
	}
	content, _ := os.ReadFile(filepath.Join(f.cfg.MediaDir, filepath.FromSlash(rel)))
	if string(content) != string(before) {
		t.Fatal("preview changed the registered file")
	}
	if _, err = f.s.Apply(ctx, plan.ID); err != nil {
		t.Fatal(err)
	}
	if err = f.s.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	plan, err = f.s.Plan(ctx, plan.ID)
	if err != nil || plan.State != "completed" || !plan.Replacement.InPlace || !called {
		t.Fatalf("plan did not complete through the in-place journal: state=%s err=%v called=%v", plan.State, err, called)
	}
	content, _ = os.ReadFile(filepath.Join(f.cfg.MediaDir, filepath.FromSlash(rel)))
	if string(content) != string(after) {
		t.Fatalf("registered file was not edited: %q", content)
	}
}

func TestTrackMetadataRefusesSeedingHardlinkAndReportsCopySpace(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hardlink fixture requires a Unix disposable filesystem")
	}
	ctx := context.Background()
	f := newReviewFixture(t)
	const rel = "Movies/Film/Film.mkv"
	body := []byte("shared seeding and library bytes")
	f.movie(t, 1, rel, body)
	library := filepath.Join(f.cfg.MediaDir, filepath.FromSlash(rel))
	seed := filepath.Join(f.cfg.DownloadsDir, "seeding.mkv")
	if err := os.Link(library, seed); err != nil {
		t.Skipf("hardlinks unavailable: %v", err)
	}
	title := "Must not write"
	f.s.SetMatroskaTrackEditor(func(context.Context, string, string, []TrackMetadataEdit, int64) (string, error) {
		t.Fatal("linked file reached the editor")
		return "", nil
	})
	_, err := f.s.PreviewTrackMetadata(ctx, 1, []TrackMetadataEdit{{Index: 1, Title: &title}})
	if err == nil || !strings.Contains(err.Error(), "independent copy") || !strings.Contains(err.Error(), fmt.Sprintf("%d bytes", len(body))) {
		t.Fatalf("linked source was not refused with copy space: %v", err)
	}
	for _, name := range []string{library, seed} {
		content, readErr := os.ReadFile(name)
		if readErr != nil || string(content) != string(body) {
			t.Fatalf("%s changed: %q %v", name, content, readErr)
		}
	}
}
