package music

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMusicDigestToleratesAccessTimeRefresh(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, "track.mp3")
	body := make([]byte, 4096)
	for i := range body {
		body[i] = byte(i)
	}
	if err := os.WriteFile(name, body, 0o600); err != nil {
		t.Fatal(err)
	}
	// Refreshing atime updates ctime on some Linux and FUSE filesystems. The
	// digest must still succeed because identity, size, mtime and full bytes
	// are unchanged.
	modified := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(name, modified.Add(time.Hour), modified); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	sha, size, err := musicDigest(context.Background(), root, "track.mp3")
	if err != nil {
		t.Fatal(err)
	}
	if size != int64(len(body)) || len(sha) != 64 {
		t.Fatalf("unexpected digest %q size %d", sha, size)
	}
}
