package announce

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

// ircClient is a minimal TLS IRC reader: NickServ identify, optional invite
// command, channel join, PING/PONG and announce extraction. It intentionally
// supports only the features the private tracker networks use.
type ircClient struct {
	def      Definition
	settings SourceSettings
	addr     string
	writeMu  sync.Mutex
	conn     net.Conn
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
	if c.settings.Password != "" {
		identify := "IDENTIFY " + c.settings.Password
		if c.settings.Account != "" {
			identify = "IDENTIFY " + c.settings.Account + " " + c.settings.Password
		}
		if e = c.write("PRIVMSG NickServ :" + identify); e != nil {
			return e
		}
		if e = sleep(ctx, 2*time.Second); e != nil {
			return e
		}
	}
	if c.settings.Invite != "" {
		fields := strings.SplitN(strings.TrimSpace(c.settings.Invite), " ", 2)
		if len(fields) == 2 {
			if e = c.write("PRIVMSG " + fields[0] + " :" + fields[1]); e != nil {
				return e
			}
			if e = sleep(ctx, 2*time.Second); e != nil {
				return e
			}
		}
	}
	if e = c.write("JOIN " + c.def.Channel); e != nil {
		return e
	}
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
		if message, ok := channelMessage(line, c.def.Channel); ok {
			onLine(message)
		}
	}
	if e = scanner.Err(); e != nil {
		return e
	}
	return fmt.Errorf("irc connection closed")
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
