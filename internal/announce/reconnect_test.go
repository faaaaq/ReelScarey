package announce

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// A dropped IRC connection must not end announce capture: the source runner
// reconnects with backoff and the next session's announce is captured and,
// for an opted-in source, becomes pending automatic work again.
func TestSourceReconnectsAfterDisconnectAndResumesCapture(t *testing.T) {
	s, done := announceFixture(t)
	defer done()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	yes := true
	source, err := s.SaveSource(ctx, "", SourceUpdate{Tracker: "iptorrents", Name: "IPT", Nick: "tester", Enabled: true, Key: "private-passkey", AutoGrab: &yes})
	if err != nil {
		t.Fatal(err)
	}

	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = listener.Close() })

	// The serve loop: the first session completes registration and joins, then
	// the server drops the link; the second session delivers one announce.
	var mu sync.Mutex
	sessions := 0
	go func() {
		for {
			conn, e := listener.Accept()
			if e != nil {
				return
			}
			mu.Lock()
			sessions++
			n := sessions
			mu.Unlock()
			go serveIRCSession(conn, n)
		}
	}()

	// runSource resolves the definition's address, so hand it a copy that
	// targets the local listener instead of the real tracker network.
	defs, e := Definitions()
	if e != nil {
		t.Fatal(e)
	}
	local := defs["iptorrents"]
	host, portStr, e := net.SplitHostPort(listener.Addr().String())
	if e != nil {
		t.Fatal(e)
	}
	var port int
	if _, e = fmt.Sscan(portStr, &port); e != nil {
		t.Fatal(e)
	}
	local.Server, local.Port, local.TLS = host, port, false

	go s.runSource(ctx, source.ID, source.Revision, local, SourceSettings{Nick: "tester"})

	// The second session's announce must be captured as pending automatic work.
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		page, err := s.Events(ctx, 1)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) > 0 {
			item := page.Items[0]
			if item.ReleaseName == "" || item.Tracker != "iptorrents" {
				t.Fatalf("wrong captured event: %+v", item)
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	mu.Lock()
	n := sessions
	mu.Unlock()
	t.Fatalf("no announce was captured after reconnect (sessions=%d)", n)
}

// serveIRCSession answers one minimal IRC session. The first session (n==1)
// drops the link right after joining; the second delivers one announce.
func serveIRCSession(conn net.Conn, n int) {
	defer conn.Close()
	reader := bufio.NewScanner(conn)
	for reader.Scan() {
		line := reader.Text()
		switch {
		case strings.HasPrefix(line, "NICK "):
			fmt.Fprintf(conn, ":server 001 tester :Welcome\r\n")
		case strings.HasPrefix(line, "JOIN "):
			fmt.Fprintf(conn, ":server 366 tester #ipt.announce :End of /NAMES\r\n")
			if n >= 2 {
				fmt.Fprintf(conn, ":Announcer!x@y PRIVMSG #ipt.announce :%s\r\n", ircTestAnnounce)
				return
			}
			// The first session dies right after joining.
			return
		}
	}
}
