package main

import (
	"context"
	"log/slog"
	"testing"
	"time"
)

func drainTestLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func TestWorkerDrainJoinsCleanWorkers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	workers := newWorkerDrain(drainTestLogger(), cancel)
	done := make(chan struct{})
	go func() {
		defer close(done)
		<-ctx.Done()
	}()
	start := time.Now()
	workers.join(done, "clean")
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("clean join took %v", elapsed)
	}
	workers.mu.Lock()
	timedOut := len(workers.timedOut)
	workers.mu.Unlock()
	if timedOut != 0 {
		t.Fatalf("clean worker recorded as timed out")
	}
}

func TestWorkerDrainBoundsStalledWorker(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	workers := newWorkerDrain(drainTestLogger(), cancel)
	workers.mu.Lock()
	workers.deadline = time.Now().Add(50 * time.Millisecond)
	workers.mu.Unlock()
	done := make(chan struct{}) // never closed: a wedged worker
	start := time.Now()
	workers.join(done, "stalled")
	elapsed := time.Since(start)
	if elapsed > 5*time.Second {
		t.Fatalf("stalled worker blocked shutdown for %v", elapsed)
	}
	if elapsed < 40*time.Millisecond {
		t.Fatalf("join returned before the deadline (%v)", elapsed)
	}
	workers.mu.Lock()
	workers.mu.Unlock()
	// A second join after the deadline must return immediately, so the rest
	// of shutdown is never delayed by earlier wedged workers.
	start = time.Now()
	workers.join(make(chan struct{}), "second")
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("post-deadline join blocked for %v", elapsed)
	}
	workers.mu.Lock()
	timedOut := len(workers.timedOut)
	workers.mu.Unlock()
	if timedOut != 2 {
		t.Fatalf("expected both stalled joins recorded, got %d", timedOut)
	}
	if err := ctx.Err(); err == nil {
		t.Fatal("join must cancel the shared worker context")
	}
}

func TestWorkerDrainDeadlineStartsOnFirstJoin(t *testing.T) {
	cancel := context.CancelFunc(func() {})
	workers := newWorkerDrain(drainTestLogger(), cancel)
	if !workers.deadline.IsZero() {
		t.Fatal("deadline must not start before the first join")
	}
	workers.reserve()
	if workers.deadline.IsZero() {
		t.Fatal("first join must start the deadline")
	}
	first := workers.deadline
	workers.reserve()
	if !workers.deadline.Equal(first) {
		t.Fatal("deadline moved after the first join")
	}
}
