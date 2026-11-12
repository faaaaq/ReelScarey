package transfer

import (
	"context"
	"testing"
	"time"
)

// submittedJob queues and submits one torrent job, leaving it in the
// 'downloading' state exactly as RunSeederWatch expects to find it.
func submittedJob(t *testing.T, f *workflowFixture) Job {
	t.Helper()
	j := f.queue(t)
	f.tick(t)
	var state string
	var attempted, submitted int
	if e := f.db.QueryRow(`SELECT state,submission_attempted,submitted_at FROM downloads WHERE id=?`, j.ID).Scan(&state, &attempted, &submitted); e != nil {
		t.Fatal(e)
	}
	if state != "downloading" || attempted != 1 || submitted <= 0 {
		t.Fatalf("job was not submitted: %s attempted=%d submitted=%d", state, attempted, submitted)
	}
	return j
}

func TestSeederAssistReannouncesSeederlessTorrent(t *testing.T) {
	f := setupWorkflow(t)
	j := submittedJob(t, f)
	if f.client.torrent.NumSeeds != 0 || f.client.torrent.NumComplete != 0 || f.client.torrent.Progress != 0 {
		t.Fatalf("fixture torrent was not seederless: %+v", f.client.torrent)
	}
	if e := f.s.AssistSeederless(context.Background(), nil); e != nil {
		t.Fatal(e)
	}
	if f.client.action != "reannounce" || f.client.commands != 1 {
		t.Fatalf("expected one re-announce, got action=%q commands=%d", f.client.action, f.client.commands)
	}
	if len(f.client.lastHashes) != 1 || f.client.lastHashes[0] != testHash {
		t.Fatalf("re-announce targeted the wrong hash: %v", f.client.lastHashes)
	}
	// A repeat pass inside the cadence window must not re-announce again.
	if e := f.s.AssistSeederless(context.Background(), nil); e != nil {
		t.Fatal(e)
	}
	if f.client.commands != 1 {
		t.Fatalf("cadence was not honored: commands=%d", f.client.commands)
	}
	// Once the swarm reports seeds the job is dropped from the ledger.
	f.client.torrent.NumSeeds = 4
	if e := f.s.AssistSeederless(context.Background(), nil); e != nil {
		t.Fatal(e)
	}
	if f.client.commands != 1 {
		t.Fatalf("seeded torrent was re-announced: commands=%d", f.client.commands)
	}
	f.s.seederMu.Lock()
	_, present := f.s.seederHelp[j.ID]
	f.s.seederMu.Unlock()
	if present {
		t.Fatal("seeded torrent was left in the assist ledger")
	}
}

func TestSeederAssistSkipsTorrentsThatAreNotStalled(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*fixtureClient)
	}{
		{"seeded swarm", func(c *fixtureClient) { c.torrent.NumSeeds = 2 }},
		{"tracker reports seeds", func(c *fixtureClient) { c.torrent.NumComplete = 3 }},
		{"already downloading", func(c *fixtureClient) { c.torrent.Progress = 0.25 }},
		{"paused by the owner", func(c *fixtureClient) { c.torrent.State = "pausedDL" }},
		{"verifying", func(c *fixtureClient) { c.torrent.State = "checkingResumeData" }},
		{"foreign torrent", func(c *fixtureClient) { c.torrent.Tags = "someone-else" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := setupWorkflow(t)
			submittedJob(t, f)
			tc.mutate(f.client)
			if e := f.s.AssistSeederless(context.Background(), nil); e != nil {
				t.Fatal(e)
			}
			if f.client.commands != 0 {
				t.Fatalf("re-announced a torrent that should be left alone: commands=%d", f.client.commands)
			}
		})
	}
}

func TestSeederAssistCapsRetriesPerJob(t *testing.T) {
	f := setupWorkflow(t)
	j := submittedJob(t, f)
	if e := f.s.AssistSeederless(context.Background(), nil); e != nil {
		t.Fatal(e)
	}
	if f.client.commands != 1 {
		t.Fatalf("first pass did not re-announce: commands=%d", f.client.commands)
	}
	// Pretend the retry budget is spent; a fresh pass must not re-announce.
	f.s.seederMu.Lock()
	f.s.seederHelp[j.ID] = seederAssistState{tries: seederAssistMaxTries, last: time.Now().Add(-time.Hour)}
	f.s.seederMu.Unlock()
	if e := f.s.AssistSeederless(context.Background(), nil); e != nil {
		t.Fatal(e)
	}
	if f.client.commands != 1 {
		t.Fatalf("retry cap was ignored: commands=%d", f.client.commands)
	}
}

// TestSeederAssistSpansWatchWindow proves the retry budget is not exhausted
// after only a minute or two. The assist must keep nudging across the whole
// watch window so a swarm that forms late still starts the transfer on its own
// rather than waiting for qBittorrent's own slow (~30 minute) re-announce.
func TestSeederAssistSpansWatchWindow(t *testing.T) {
	if time.Duration(seederAssistMaxTries)*seederAssistGap < seederWatchWindow {
		t.Fatalf("assist retry budget covers only %s of the %s window",
			time.Duration(seederAssistMaxTries)*seederAssistGap, seederWatchWindow)
	}
	f := setupWorkflow(t)
	j := submittedJob(t, f)
	// Well past the previous eight-try cap, but still inside the window, so the
	// watchdog must still re-announce.
	f.s.seederMu.Lock()
	f.s.seederHelp[j.ID] = seederAssistState{tries: 40, last: time.Now().Add(-seederAssistGap - time.Second)}
	f.s.seederMu.Unlock()
	if e := f.s.AssistSeederless(context.Background(), nil); e != nil {
		t.Fatal(e)
	}
	if f.client.commands != 1 {
		t.Fatalf("assist stopped mid-window: commands=%d", f.client.commands)
	}
}

func TestSeederAssistIgnoresStaleSubmissions(t *testing.T) {
	f := setupWorkflow(t)
	j := submittedJob(t, f)
	if _, e := f.db.Exec(`UPDATE downloads SET submitted_at=? WHERE id=?`, time.Now().Add(-2*time.Hour).Unix(), j.ID); e != nil {
		t.Fatal(e)
	}
	if e := f.s.AssistSeederless(context.Background(), nil); e != nil {
		t.Fatal(e)
	}
	if f.client.commands != 0 {
		t.Fatalf("an old submission was re-announced: commands=%d", f.client.commands)
	}
}

func TestSeederAssistCadenceGapIsHonored(t *testing.T) {
	f := setupWorkflow(t)
	j := submittedJob(t, f)
	f.s.seederMu.Lock()
	f.s.seederHelp[j.ID] = seederAssistState{tries: 1, last: time.Now().Add(-time.Second)}
	f.s.seederMu.Unlock()
	if e := f.s.AssistSeederless(context.Background(), nil); e != nil {
		t.Fatal(e)
	}
	if f.client.commands != 0 {
		t.Fatalf("re-announced before the cadence gap elapsed: commands=%d", f.client.commands)
	}
	// After the gap it is allowed again.
	f.s.seederMu.Lock()
	f.s.seederHelp[j.ID] = seederAssistState{tries: 1, last: time.Now().Add(-seederAssistGap - time.Second)}
	f.s.seederMu.Unlock()
	if e := f.s.AssistSeederless(context.Background(), nil); e != nil {
		t.Fatal(e)
	}
	if f.client.commands != 1 {
		t.Fatalf("re-announce was skipped after the cadence gap: commands=%d", f.client.commands)
	}
}
