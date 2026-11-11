package announce

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ircClient is a minimal TLS IRC reader: NickServ identify, optional invite
// command, channel join, PING/PONG and announce extraction. It intentionally
// supports only the features the private tracker networks use.
type ircClient struct {
	def      Definition
	settings SourceSettings
	addr     string
	logger   *slog.Logger
	// registerTimeout bounds how long the server may take to send its welcome.
	// It is a field so tests can shorten it; zero means 90 seconds.
	registerTimeout time.Duration
	writeMu         sync.Mutex
	conn            net.Conn
}

func (c *ircClient) address() string {
	if c.addr != "" {
		return c.addr
	}
	return net.JoinHostPort(c.def.Server, fmt.Sprint(c.def.Port))
}

func (c *ircClient) write(line string) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.conn == nil {
		return fmt.Errorf("irc connection is closed")
	}
	_ = c.conn.SetWriteDeadline(time.Now().Add(15 * time.Second))
	_, e := c.conn.Write([]byte(line + "\r\n"))
	return e
}

// Run connects, authenticates and reads until the context ends or the
// connection fails. Each channel message is passed to onLine.
func (c *ircClient) Run(ctx context.Context, onLine func(string)) error {
	dialer := &net.Dialer{Timeout: 20 * time.Second, KeepAlive: 30 * time.Second}
	var conn net.Conn
	var e error
	if c.def.TLS {
		host := c.def.Server
		if h, _, err := net.SplitHostPort(c.address()); err == nil {
			host = h
		}
		tlsConfig := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
		// A source may opt out of certificate checking for a tracker whose IRC
		// server presents an expired or otherwise untrusted certificate. The
		// link stays encrypted, but the identity of the server is not proven.
		if c.settings.SkipTLSVerify {
			tlsConfig.InsecureSkipVerify = true
		}
		conn, e = tls.DialWithDialer(dialer, "tcp", c.address(), tlsConfig)
	} else {
		conn, e = dialer.DialContext(ctx, "tcp", c.address())
	}
	if e != nil {
		return e
	}
	c.conn = conn
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()
	defer func() {
		_ = c.write("QUIT :ReelScarey shutting down")
		_ = conn.Close()
	}()
	nick := c.settings.Nick
	if nick == "" {
		return fmt.Errorf("an IRC nick is required")
	}
	if e = c.write("NICK " + nick); e != nil {
		return e
	}
	if e = c.write("USER " + nick + " 0 * :ReelScarey announce reader"); e != nil {
		return e
	}
	// Registration must finish before JOIN. Trackers such as IPTorrents answer
	// an early JOIN with 451 and drop it, which would otherwise leave the reader
	// connected, answering PINGs, but silently receiving no announces at all.
	registered := make(chan struct{})
	var registerOnce sync.Once
	var registrationExpired atomic.Bool
	timeout := c.registerTimeout
	if timeout <= 0 {
		timeout = 90 * time.Second
	}
	timer := time.AfterFunc(timeout, func() {
		select {
		case <-registered:
			return
		default:
		}
		registrationExpired.Store(true)
		_ = conn.Close()
	})
	defer timer.Stop()
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 4096), 8192)
	for scanner.Scan() {
		if e = ctx.Err(); e != nil {
			return e
		}
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if strings.HasPrefix(line, "PING ") {
			if e = c.write("PONG " + strings.TrimPrefix(line, "PING ")); e != nil {
				return e
			}
			continue
		}
		// A server ERROR (throttling, a banned nick, a closed link) ends the
		// connection; returning its text puts the reason in the source status.
		if strings.HasPrefix(line, "ERROR") {
			return fmt.Errorf("irc server closed the connection: %s", strings.TrimSpace(strings.TrimPrefix(line, "ERROR")))
		}
		if code, rest, ok := serverNumeric(line); ok {
			switch {
			case code == 1:
				var post error
				registerOnce.Do(func() {
					close(registered)
					post = c.postRegistration(ctx)
				})
				if post != nil {
					return post
				}
			case code == 366 && strings.Contains(strings.ToLower(rest), strings.ToLower(c.def.Channel)):
				if c.logger != nil {
					c.logger.Info("announce source joined the IRC channel", "tracker", c.def.ID, "channel", c.def.Channel)
				}
			case code >= 400 && code <= 599:
				if e := c.numericError(code, rest); e != nil {
					return e
				}
			}
			continue
		}
		if message, ok := channelMessage(line, c.def.Channel); ok {
			onLine(message)
		}
	}
	if registrationExpired.Load() {
		return fmt.Errorf("irc server did not complete registration within %s", timeout)
	}
	if e = scanner.Err(); e != nil {
		return e
	}
	return fmt.Errorf("irc connection closed")
}

// postRegistration runs once the server has sent its welcome (001): NickServ
// identify, the optional invite command and finally the channel join.
func (c *ircClient) postRegistration(ctx context.Context) error {
	if c.settings.Password != "" {
		identify := "IDENTIFY " + c.settings.Password
		if c.settings.Account != "" {
			identify = "IDENTIFY " + c.settings.Account + " " + c.settings.Password
		}
		if e := c.write("PRIVMSG NickServ :" + identify); e != nil {
			return e
		}
		if e := sleep(ctx, 2*time.Second); e != nil {
			return e
		}
	}
	if c.settings.Invite != "" {
		fields := strings.SplitN(strings.TrimSpace(c.settings.Invite), " ", 2)
		if len(fields) == 2 {
			if e := c.write("PRIVMSG " + fields[0] + " :" + fields[1]); e != nil {
				return e
			}
			if e := sleep(ctx, 2*time.Second); e != nil {
				return e
			}
		}
	}
	return c.write("JOIN " + c.def.Channel)
}

// serverNumeric splits ":server 001 nick :welcome" into its numeric reply code
// and the remaining parameters.
func serverNumeric(line string) (int, string, bool) {
	if !strings.HasPrefix(line, ":") {
		return 0, "", false
	}
	fields := strings.SplitN(line, " ", 3)
	if len(fields) < 2 || len(fields[1]) != 3 {
		return 0, "", false
	}
	code, e := strconv.Atoi(fields[1])
	if e != nil {
		return 0, "", false
	}
	rest := ""
	if len(fields) == 3 {
		rest = fields[2]
	}
	return code, rest, true
}

// numericError turns a failed registration or a refused channel join into a
// visible source error instead of a connection that stays up but never
// announces anything.
func (c *ircClient) numericError(code int, rest string) error {
	if code == 451 {
		return fmt.Errorf("irc server rejected a command before registration completed: %s", rest)
	}
	if strings.Contains(strings.ToLower(rest), strings.ToLower(c.def.Channel)) {
		return fmt.Errorf("irc server refused %s: %s", c.def.Channel, rest)
	}
	return nil
}

// channelMessage extracts the text of a PRIVMSG for the configured channel.
func channelMessage(line, channel string) (string, bool) {
	marker := "PRIVMSG "
	index := strings.Index(line, marker)
	if index < 0 {
		return "", false
	}
	rest := line[index+len(marker):]
	split := strings.Index(rest, " :")
	if split < 0 {
		return "", false
	}
	target := rest[:split]
	if !strings.EqualFold(target, channel) {
		return "", false
	}
	return rest[split+2:], true
}

// stripIRCFormatting removes mIRC/IRC formatting without altering the text
// between those markers. In particular, color digits belong to the marker and
// must not become part of a release name or category.
func stripIRCFormatting(message string) string {
	if !strings.ContainsAny(message, "\x02\x03\x04\x0f\x11\x16\x1d\x1e\x1f") {
		return message
	}
	var out strings.Builder
	out.Grow(len(message))
	for i := 0; i < len(message); {
		switch message[i] {
		case 0x02, 0x0f, 0x11, 0x16, 0x1d, 0x1e, 0x1f:
			i++
		case 0x03: // mIRC foreground and optional background color.
			i++
			i = consumeColorDigits(message, i, 2, false)
			if i < len(message) && message[i] == ',' {
				if end := consumeColorDigits(message, i+1, 2, false); end > i+1 {
					i = end
				}
			}
		case 0x04: // IRC hex foreground and optional background color.
			i++
			i = consumeColorDigits(message, i, 6, true)
			if i < len(message) && message[i] == ',' {
				if end := consumeColorDigits(message, i+1, 6, true); end > i+1 {
					i = end
				}
			}
		default:
			out.WriteByte(message[i])
			i++
		}
	}
	return out.String()
}

func consumeColorDigits(message string, start, maximum int, hex bool) int {
	end := start
	for end < len(message) && end-start < maximum {
		c := message[end]
		if c < '0' || c > '9' {
			if !hex || (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
				break
			}
		}
		end++
	}
	return end
}

func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
