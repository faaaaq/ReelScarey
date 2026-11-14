//go:build race

package library

// Set when the race detector is compiled in (see browse_perf_test.go: the
// browse latency budget is not meaningful under race instrumentation).
const raceInstrumented = true
