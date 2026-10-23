package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWorkersWaitForRecoveryAndCancellation(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "recovery-mode")
	if e := os.WriteFile(marker, []byte(`{"format":1}`), 0600); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan bool, 1)
	go func() { done <- waitRecovery(ctx, dir) }()
	select {
	case <-done:
		t.Fatal("workers bypassed recovery hold")
	case <-time.After(30 * time.Millisecond):
	}
	cancel()
	select {
	case started := <-done:
		if started {
			t.Fatal("canceled worker started")
		}
	case <-time.After(time.Second):
		t.Fatal("worker did not stop")
	}
	if e := os.Remove(marker); e != nil {
		t.Fatal(e)
	}
	if !waitRecovery(context.Background(), dir) {
		t.Fatal("workers did not resume after hold removed")
	}
}
