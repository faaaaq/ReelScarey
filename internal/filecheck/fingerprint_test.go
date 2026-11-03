package filecheck

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/faaaaq/ReelScarey/internal/database"
)

func TestObservationDetectsChangesAndRejectsMissingEvidence(t *testing.T) {
	for _, mode := range []string{"sample", "unsampled-restored-time", "replacement", "invalid", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			if mode == "unsampled-restored-time" && runtime.GOOS != "linux" {
				t.Skip("Linux change timestamps")
			}
			name := filepath.Join(t.TempDir(), "video.mkv")
			if err := os.WriteFile(name, make([]byte, 1<<20), 0600); err != nil {
				t.Fatal(err)
			}
			f, err := os.Open(name)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			observation, err := Capture(context.Background(), f)
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(observation)
			if err = Verify(context.Background(), f, string(raw)); err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			// Overlay filesystems can coalesce change timestamps within one clock tick.
			if mode == "unsampled-restored-time" {
				time.Sleep(20 * time.Millisecond)
			}
			switch mode {
			case "sample", "unsampled-restored-time":
				offset := int64(0)
				if mode == "unsampled-restored-time" {
					offset = 256 << 10
				}
				writer, err := os.OpenFile(name, os.O_WRONLY, 0600)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = writer.WriteAt([]byte{1}, offset); err != nil {
					t.Fatal(err)
				}
				writer.Close()
				original := time.Unix(0, observation.Mtime)
				if err = os.Chtimes(name, original, original); err != nil {
					t.Fatal(err)
				}
			case "replacement":
				f.Close()
				if err = os.Rename(name, name+".old"); err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(name, make([]byte, 1<<20), 0600); err != nil {
					t.Fatal(err)
				}
				original := time.Unix(0, observation.Mtime)
				if err = os.Chtimes(name, original, original); err != nil {
					t.Fatal(err)
				}
				f, err = os.Open(name)
				if err != nil {
					t.Fatal(err)
				}
				defer f.Close()
			case "invalid":
				raw = []byte(`{}`)
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if err = Verify(ctx, f, string(raw)); err == nil {
				t.Fatal("changed/unverifiable observation accepted")
			}
		})
	}
}

func TestEnsureFullChecksumPreservesPendingReplacementAndRejectsChangedFile(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(map[bool]string{true: "changed", false: "unchanged"}[changed], func(t *testing.T) {
			ctx := context.Background()
			base := t.TempDir()
			db, err := database.Open(ctx, filepath.Join(base, "config"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if err = os.Mkdir(filepath.Join(base, "media"), 0700); err != nil {
				t.Fatal(err)
			}
			body := make([]byte, 1<<20)
			body[512] = 42
			name := filepath.Join(base, "media", "owned.mkv")
			if err = os.WriteFile(name, body, 0600); err != nil {
				t.Fatal(err)
			}
			f, err := os.Open(name)
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := Capture(ctx, f)
			f.Close()
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(snapshot)
			if _, err = db.Exec(`INSERT INTO media_items(id,kind,tmdb_id,title) VALUES(1,'movie',1,'Owned'); INSERT INTO media_files(id,movie_id,relative_path,size_bytes,sha256,probe_json,imported_at,origin,verification_json) VALUES(1,1,'owned.mkv',1048576,'','{}',1,'existing',?)`, string(raw)); err != nil {
				t.Fatal(err)
			}
			if _, err = db.Exec(`INSERT INTO acquisition_replacements(source_kind,job_id,media_file_id,target_sha) VALUES('download','pending',1,'')`); err != nil {
				t.Fatal(err)
			}
			if changed {
				body[512] = 43
				if err = os.WriteFile(name, body, 0600); err != nil {
					t.Fatal(err)
				}
			}
			root, err := os.OpenRoot(filepath.Join(base, "media"))
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			got, err := EnsureSHA(ctx, db, root, 1)
			if changed {
				if !errors.Is(err, ErrChanged) {
					t.Fatal(err)
				}
				var saved string
				if err = db.QueryRow(`SELECT sha256 FROM media_files WHERE id=1`).Scan(&saved); err != nil || saved != "" {
					t.Fatal("changed file promoted", err)
				}
				return
			}
			sum := sha256.Sum256(body)
			if err != nil || got != hex.EncodeToString(sum[:]) {
				t.Fatal(got, err)
			}
			var target, observation string
			if err = db.QueryRow(`SELECT target_sha,verification_json FROM acquisition_replacements JOIN media_files ON media_files.id=media_file_id`).Scan(&target, &observation); err != nil || target != got || observation != "" {
				t.Fatal("reservation/observation not updated", err)
			}
			again, err := EnsureSHA(ctx, db, root, 1)
			if err != nil || again != got {
				t.Fatal("not idempotent", err)
			}
		})
	}
}
