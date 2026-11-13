package main

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// workerDrainTimeout bounds the total time serve() spends waiting for the
// background workers after cancellation. The HTTP server drain (10s on
// SIGINT, 5m for restore restarts) happens before these joins, and Docker
// grants 60s of stop grace in total, so a 30s drain keeps the whole shutdown
// comfortably inside the grace period while leaving time for database close
// and instance-lock release. A worker that ignores cancellation cannot hang
// shutdown forever: after the deadline the remaining joins return and the
// process exits, letting durable leases and recovery journals reconcile the
// interrupted work on the next start.
const workerDrainTimeout = 30 * time.Second

// workerDrain joins cancelled workers against one shared deadline so the
// overall shutdown is bounded even when individual workers stall. The
// deadline starts when the first join runs, which is when server shutdown has
// already completed.
type workerDrain struct {
	mu       sync.Mutex
	cancel   context.CancelFunc
	deadline time.Time
	logger   *slog.Logger
	timedOut []string
}

func newWorkerDrain(logger *slog.Logger, cancel context.CancelFunc) *workerDrain {
	return &workerDrain{cancel: cancel, logger: logger}
}

// join cancels the shared worker context (idempotent; every worker shares it)
// and waits for the named worker up to the remaining shared deadline.
func (d *workerDrain) join(done <-chan struct{}, name string) {
	d.cancel()
	remaining := d.reserve()
	select {
	case <-done:
	case <-time.After(remaining):
		d.mu.Lock()
		d.timedOut = append(d.timedOut, name)
		d.mu.Unlock()
		d.logger.Error("worker did not stop within the shutdown deadline; continuing shutdown, the worker will be terminated with the process", "worker", name)
	}
}

// reserve lazily starts the shared deadline on the first join and returns the
// time still available to this join (zero once the deadline has passed).
func (d *workerDrain) reserve() time.Duration {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.deadline.IsZero() {
		d.deadline = time.Now().Add(workerDrainTimeout)
	}
	remaining := time.Until(d.deadline)
	if remaining < 0 {
		return 0
	}
	return remaining
}

// report logs the distinction between a graceful drain and a timed-out one.
// Callers must not describe a shutdown that hit the deadline as graceful.
func (d *workerDrain) report() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.timedOut) > 0 {
		d.logger.Error("shutdown finished after the worker drain deadline expired; the listed workers were still running at exit and their durable state will be reconciled on the next start", "workers", d.timedOut)
		return
	}
	d.logger.Info("all background workers stopped cleanly")
}
