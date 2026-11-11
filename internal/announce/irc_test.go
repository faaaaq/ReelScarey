package announce

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"log/slog"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/securestore"
)

func TestIRCClientReceivesAndParsesAnnounce(t *testing.T) {
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer listener.Close()
	line := `[Movies/XviD] The Movie 2010 DVDRip XviD-GROUP FREELEECH - http://www.iptorrents.com/details.php?id=000000 - 7.63 GB`
	go func() {
		conn, e := listener.Accept()
		if e != nil {
			return
		}
		defer conn.Close()
		reader := bufio.NewScanner(conn)
		for reader.Scan() {
			text := reader.Text()
			if strings.HasPrefix(text, "USER ") {
				_, _ = conn.Write([]byte(":server 001 tester :welcome\r\nPING :keepalive\r\n"))
				_, _ = conn.Write([]byte(":Announcer!x@y PRIVMSG #ipt.announce :" + line + "\r\n"))
			}
		}
	}()
	defs, e := Definitions()
	if e != nil {
		t.Fatal(e)
	}
	def := defs["iptorrents"]
	def.TLS = false
	client := &ircClient{def: def, settings: SourceSettings{Nick: "tester"}, addr: listener.Addr().String()}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	received := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		done <- client.Run(ctx, func(message string) {
			select {
			case received <- message:
			default:
			}
		})
	}()
	select {
	case message := <-received:
		parsed, ok := def.Parse(message)
		if !ok || parsed.ReleaseName != "The Movie 2010 DVDRip XviD-GROUP" || parsed.TorrentID != "000000" {
			t.Fatal(parsed, ok)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("announce was not received")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("client did not stop")
	}
}

func TestChannelMessageFiltersOtherChannels(t *testing.T) {
	if _, ok := channelMessage(":x!y@z PRIVMSG #other :hello", "#announce"); ok {
		t.Fatal("message from another channel accepted")
	}
	message, ok := channelMessage(":x!y@z PRIVMSG #announce :hello world", "#announce")
	if !ok || message != "hello world" {
		t.Fatal(message, ok)
	}
}

func announceFixture(t *testing.T) (*Service, func()) {
	t.Helper()
	root := t.TempDir()
	cfgDir := filepath.Join(root, "config")
	if e := os.MkdirAll(cfgDir, 0o700); e != nil {
		t.Fatal(e)
	}
	db, e := database.Open(context.Background(), cfgDir)
	if e != nil {
		t.Fatal(e)
	}
	secrets, e := securestore.New(context.Background(), db, cfgDir)
	if e != nil {
		db.Close()
		t.Fatal(e)
	}
	return New(db, secrets, slog.New(slog.DiscardHandler)), func() { db.Close() }
}

func TestSourceLifecycleAndBoundedEvents(t *testing.T) {
	s, done := announceFixture(t)
	defer done()
	ctx := context.Background()
	source, e := s.SaveSource(ctx, "", SourceUpdate{Tracker: "iptorrents", Name: "IPT", Nick: "tester", Password: "secret", Key: "tracker-passkey", Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	if source.Revision != 1 || !source.HasPassword || !source.HasKey || source.Server != "irc.iptorrents.com" || source.Channel != "#ipt.announce" {
		t.Fatal(source)
	}
	if source, e = s.SaveSource(ctx, source.ID, SourceUpdate{Tracker: "iptorrents", Name: "IPT", Nick: "tester", Enabled: false, Revision: 1}); e != nil {
		t.Fatal(e)
	}
	if source.Revision != 2 || !source.HasPassword || !source.HasKey || source.Enabled {
		t.Fatal(source)
	}
	parsed := Parsed{ReleaseName: "The Movie 2010", TorrentID: "1", InfoURL: "https://tracker/details.php?id=1"}
	if e = s.Record(ctx, source.ID, "iptorrents", parsed, "raw announce line"); e != nil {
		t.Fatal(e)
	}
	if e = s.Record(ctx, source.ID, "iptorrents", parsed, "raw announce line"); e != nil {
		t.Fatal(e)
	}
	page, e := s.Events(ctx, 1)
	if e != nil || page.Total != 1 || len(page.Items) != 1 || page.Items[0].ReleaseName != "The Movie 2010" {
		t.Fatal(page, e)
	}
	if e = s.DeleteSource(ctx, source.ID, 2); e != nil {
		t.Fatal(e)
	}
	if page, e = s.Events(ctx, 1); e != nil || page.Total != 0 {
		t.Fatal("events survived source deletion", page, e)
	}
}

func TestSourceValidation(t *testing.T) {
	s, done := announceFixture(t)
	defer done()
	ctx := context.Background()
	if _, e := s.SaveSource(ctx, "", SourceUpdate{Tracker: "unknown", Name: "x", Nick: "tester"}); e == nil {
		t.Fatal("unknown tracker accepted")
	}
	if _, e := s.SaveSource(ctx, "", SourceUpdate{Tracker: "iptorrents", Name: "x", Nick: "bad nick"}); e == nil {
		t.Fatal("invalid nick accepted")
	}
	if _, e := s.SaveSource(ctx, "", SourceUpdate{Tracker: "beyondhd", Name: "BHD", Nick: "tester", Enabled: true}); e == nil {
		t.Fatal("account-required tracker enabled without credentials")
	}
}

func TestSourceSkipTLSVerifyPersists(t *testing.T) {
	s, done := announceFixture(t)
	defer done()
	ctx := context.Background()
	yes := true
	source, e := s.SaveSource(ctx, "", SourceUpdate{Tracker: "iptorrents", Name: "IPT", Nick: "tester", SkipTLSVerify: &yes})
	if e != nil {
		t.Fatal(e)
	}
	if !source.SkipTLSVerify {
		t.Fatal("skip verify was not saved")
	}
	// An update that omits the field (for example the pause/resume toggle) must
	// keep the saved value instead of clearing it.
	if source, e = s.SaveSource(ctx, source.ID, SourceUpdate{Tracker: "iptorrents", Name: "IPT", Nick: "tester", Enabled: true, Revision: 1}); e != nil {
		t.Fatal(e)
	}
	if !source.SkipTLSVerify {
		t.Fatal("skip verify was cleared by an unrelated update")
	}
	no := false
	if source, e = s.SaveSource(ctx, source.ID, SourceUpdate{Tracker: "iptorrents", Name: "IPT", Nick: "tester", Enabled: true, Revision: 2, SkipTLSVerify: &no}); e != nil {
		t.Fatal(e)
	}
	if source.SkipTLSVerify {
		t.Fatal("skip verify was not cleared")
	}
}

// tlsTestServer starts a TLS IRC stand-in that announces one line, using a
// self-signed certificate that no client is configured to trust.
func tlsTestServer(t *testing.T, line string) string {
	t.Helper()
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, e := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}
	listener, e := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, e := listener.Accept()
			if e != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				reader := bufio.NewScanner(conn)
				for reader.Scan() {
					if strings.HasPrefix(reader.Text(), "USER ") {
						_, _ = conn.Write([]byte(":server 001 tester :welcome\r\n"))
						_, _ = conn.Write([]byte(":Announcer!x@y PRIVMSG #ipt.announce :" + line + "\r\n"))
					}
				}
			}(conn)
		}
	}()
	return listener.Addr().String()
}

func TestIRCTLSCertificateHandling(t *testing.T) {
	defs, e := Definitions()
	if e != nil {
		t.Fatal(e)
	}
	line := `[Movies/XviD] The Movie 2010 DVDRip XviD-GROUP - http://www.iptorrents.com/details.php?id=000001 - 7.63 GB`

	t.Run("an untrusted certificate is rejected by default", func(t *testing.T) {
		addr := tlsTestServer(t, line)
		client := &ircClient{def: defs["iptorrents"], settings: SourceSettings{Nick: "tester"}, addr: addr}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if e := client.Run(ctx, func(string) { t.Error("an announce was read over an untrusted connection") }); e == nil || !strings.Contains(e.Error(), "certificate") {
			t.Fatalf("expected a certificate error, got %v", e)
		}
	})

	t.Run("skip verify reads through an untrusted certificate", func(t *testing.T) {
		addr := tlsTestServer(t, line)
		client := &ircClient{def: defs["iptorrents"], settings: SourceSettings{Nick: "tester", SkipTLSVerify: true}, addr: addr}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		received := make(chan string, 1)
		done := make(chan error, 1)
		go func() {
			done <- client.Run(ctx, func(message string) {
				select {
				case received <- message:
				default:
				}
			})
		}()
		select {
		case message := <-received:
			if !strings.Contains(message, "The Movie 2010") {
				t.Fatal(message)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("announce was not received with skip verify")
		}
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("client did not stop")
		}
	})
}
