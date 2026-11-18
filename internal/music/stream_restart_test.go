package music

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// The composition root prunes immediately after constructing a fresh Service.
// At that point no fresh timestamp can indicate a live encoder: every staging
// file is crash residue, including one written just before an abrupt restart.
func TestStreamCacheStartupPruneRemovesFreshCrashPart(t *testing.T) {
	service, db, mediaDir := cacheTestService(t)
	fileID := registerCacheFile(t, db, "Music/Artist/Album/01 Song.flac", "sha")
	dir := filepath.Join(mediaDir, streamCacheDirName, strconv.FormatInt(fileID, 10))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	part := filepath.Join(dir, "opus-128.part")
	if err := os.WriteFile(part, []byte("interrupted"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := service.PruneStreamCache(context.Background(), time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(part); !os.IsNotExist(err) {
		t.Fatalf("fresh crash staging survived startup prune: %v", err)
	}
}
