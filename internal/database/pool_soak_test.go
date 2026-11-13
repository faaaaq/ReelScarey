package database

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// openSized builds a database identical to Open but with a configurable pool,
// so pool sizing (RS-0006) and WAL behavior (RS-0037) can be measured without
// touching production configuration.
func openSized(t *testing.T, dir string, maxOpen int) *sql.DB {
	t.Helper()
	filename := filepath.ToSlash(filepath.Join(dir, "reelscarey.db"))
	if !strings.HasPrefix(filename, "/") {
		filename = "/" + filename
	}
	u := url.URL{Scheme: "file", Path: filename}
	q := url.Values{}
	for _, pragma := range []string{"foreign_keys(1)", "busy_timeout(5000)", "journal_mode(WAL)"} {
		q.Add("_pragma", pragma)
	}
	q.Set("_txlock", "immediate")
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(maxOpen)
	db.SetMaxIdleConns(maxOpen)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

type soakResult struct {
	reads         int64
	writes        int64
	busyErrors    int64
	readP50       time.Duration
	readP95       time.Duration
	readMax       time.Duration
	walBytesAfter int64
	walBytesFloor int64
}

// runSoak drives concurrent short writers, a long lease-style write
// transaction and parallel readers against one database, then reports reader
// latency, busy errors and WAL growth (RS-0006 / RS-0037).
func runSoak(t *testing.T, db *sql.DB, seconds int) soakResult {
	t.Helper()
	ctx := context.Background()
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS soak_items(id INTEGER PRIMARY KEY, payload TEXT NOT NULL, n INTEGER NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO soak_items(payload,n) VALUES('seed',0)`); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Duration(seconds) * time.Second)

	var reads, writes, busy atomic.Int64
	var readMu sync.Mutex
	readLatencies := []time.Duration{}

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// One writer family: short BEGIN IMMEDIATE transactions, like claim loops.
	for w := 0; w < 2; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n := 0
			for {
				select {
				case <-stop:
					return
				default:
				}
				if time.Now().After(deadline) {
					return
				}
				n++
				_, err := db.ExecContext(ctx, `INSERT INTO soak_items(payload,n) VALUES('write',?)`, n)
				if err != nil {
					if strings.Contains(err.Error(), "SQLITE_BUSY") || strings.Contains(err.Error(), "database is locked") {
						busy.Add(1)
						continue
					}
					t.Error(err)
					return
				}
				writes.Add(1)
			}
		}()
	}

	// One lease holder: periodically grabs the write lock and holds a
	// transaction open, simulating the audited long lease transactions.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			if time.Now().After(deadline) {
				return
			}
			tx, err := db.BeginTx(ctx, nil)
			if err != nil {
				if strings.Contains(err.Error(), "locked") {
					busy.Add(1)
					time.Sleep(20 * time.Millisecond)
					continue
				}
				t.Error(err)
				return
			}
			if _, err := tx.ExecContext(ctx, `UPDATE soak_items SET n=n+1 WHERE payload='seed'`); err != nil {
				tx.Rollback()
				t.Error(err)
				return
			}
			time.Sleep(150 * time.Millisecond)
			if err := tx.Commit(); err != nil {
				t.Error(err)
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	}()

	// Parallel readers, like HTTP browse traffic.
	for r := 0; r < 6; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				if time.Now().After(deadline) {
					return
				}
				start := time.Now()
				var count, maxN any
				err := db.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(MAX(n),0) FROM soak_items`).Scan(&count, &maxN)
				elapsed := time.Since(start)
				if err != nil {
					if strings.Contains(err.Error(), "locked") || strings.Contains(err.Error(), "SQLITE_BUSY") {
						busy.Add(1)
						continue
					}
					t.Error(err)
					return
				}
				reads.Add(1)
				readMu.Lock()
				readLatencies = append(readLatencies, elapsed)
				readMu.Unlock()
			}
		}()
	}

	time.Sleep(time.Duration(seconds) * time.Second)
	close(stop)
	wg.Wait()

	var out soakResult
	out.reads = reads.Load()
	out.writes = writes.Load()
	out.busyErrors = busy.Load()
	readMu.Lock()
	sort.Slice(readLatencies, func(i, j int) bool { return readLatencies[i] < readLatencies[j] })
	if len(readLatencies) > 0 {
		out.readP50 = readLatencies[len(readLatencies)*50/100]
		out.readP95 = readLatencies[len(readLatencies)*95/100]
		out.readMax = readLatencies[len(readLatencies)-1]
	}
	readMu.Unlock()

	// WAL size at the peak of activity.
	walPath := filepath.Join(soakDir(t, db), "reelscarey.db-wal")
	if info, err := os.Stat(walPath); err == nil {
		out.walBytesAfter = info.Size()
	}
	// A passive checkpoint must bring the WAL back down; if it cannot, the
	// WAL was starved by the pool configuration.
	if _, err := db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`); err == nil {
		if info, err := os.Stat(walPath); err == nil {
			out.walBytesFloor = info.Size()
		}
	}
	return out
}

func soakDir(t *testing.T, db *sql.DB) string {
	var path string
	if err := db.QueryRow(`PRAGMA database_list`).Scan(new(any), new(any), &path); err != nil {
		t.Fatal(err)
	}
	return filepath.Dir(path)
}

// TestWALStaysBoundedUnderSustainedLoad is the RS-0037 soak: concurrent
// readers and writers plus long lease transactions must not starve WAL
// checkpointing, and the recorded baseline documents the measured behavior.
func TestWALStaysBoundedUnderSustainedLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("soak skipped in -short mode")
	}
	db := openSized(t, t.TempDir(), 2) // records the audited baseline
	result := runSoak(t, db, 5)
	t.Logf("baseline pool=2: reads=%d writes=%d busy=%d readP50=%v readP95=%v readMax=%v walAfter=%dB walFloor=%dB",
		result.reads, result.writes, result.busyErrors, result.readP50, result.readP95, result.readMax, result.walBytesAfter, result.walBytesFloor)
	stats, err := WALInfo(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("WALInfo: page_count=%d freelist=%d page_size=%d autocheckpoint=%d",
		stats.PageCount, stats.FreelistPages, stats.PageSize, stats.AutoCheckpointPages)
	// The WAL must return to (near) zero after a truncating checkpoint: a
	// starved checkpoint regime would leave the file at its high-water mark.
	if result.walBytesFloor > 1<<20 {
		t.Fatalf("WAL did not return to disk floor after checkpoint: %d bytes", result.walBytesFloor)
	}
	// Sustained growth ceiling during the soak itself.
	if result.walBytesAfter > 64<<20 {
		t.Fatalf("WAL grew beyond the 64 MiB ceiling during the soak: %d bytes", result.walBytesAfter)
	}
}

// TestPoolSizingMeasuresReaderContention compares the audited 2-connection
// pool against wider pools under the same concurrent load (RS-0006). The
// numbers are recorded; a configuration change requires a demonstrated win.
func TestPoolSizingMeasuresReaderContention(t *testing.T) {
	if testing.Short() {
		t.Skip("soak skipped in -short mode")
	}
	for _, size := range []int{2, 4, 8} {
		db := openSized(t, t.TempDir(), size)
		result := runSoak(t, db, 4)
		t.Logf("pool=%d: reads=%d writes=%d busy=%d readP50=%v readP95=%v readMax=%v",
			size, result.reads, result.writes, result.busyErrors, result.readP50, result.readP95, result.readMax)
		if result.busyErrors > 0 {
			t.Logf("pool=%d recorded %d busy errors", size, result.busyErrors)
		}
	}
	_ = fmt.Sprintf
}
