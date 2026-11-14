package main

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestGuardWorkerContainsPanicAndClosesDone(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	done := make(chan struct{})
	go guardWorker(logger, "fixtureWorker", done, func() {
		panic("fixture explosion")
	})
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("done channel was not closed after a worker panic; shutdown would hang")
	}
	logged := buf.String()
	if !strings.Contains(logged, "fixtureWorker") {
		t.Fatal("panic log does not name the worker", logged)
	}
	if !strings.Contains(logged, "fixture explosion") {
		t.Fatal("panic log does not record the panic value", logged)
	}
	if !strings.Contains(logged, "TestGuardWorkerContainsPanicAndClosesDone.func1") {
		t.Fatal("panic log does not retain the goroutine stack", logged)
	}
}

func TestGuardWorkerRunsBodyAndClosesDoneWithoutPanic(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	done := make(chan struct{})
	ran := make(chan struct{})
	go guardWorker(logger, "cleanWorker", done, func() {
		close(ran)
	})
	select {
	case <-ran:
	case <-time.After(5 * time.Second):
		t.Fatal("worker body never ran")
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("done channel was not closed after a clean worker exit")
	}
	if buf.String() != "" {
		t.Fatal("clean worker logged a panic", buf.String())
	}
}
