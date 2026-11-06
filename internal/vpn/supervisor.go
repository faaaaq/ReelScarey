package vpn

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type runtimeState struct {
	mu        sync.Mutex
	state     string // stopped, starting, up, failed
	message   string
	up        bool
	attempts  int
	lastStart time.Time
	notify    chan struct{}
}

func (r *runtimeState) snapshot() (string, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state, r.message
}

func (r *runtimeState) set(state, message string) {
	r.mu.Lock()
	r.state, r.message = state, message
	r.mu.Unlock()
}

func (r *runtimeState) poke() {
	if r.notify == nil {
		return
	}
	select {
	case r.notify <- struct{}{}:
	default:
	}
}

func wgQuick() string {
	if v := os.Getenv("REELSCAREY_WG_QUICK"); v != "" {
		return v
	}
	if p, e := exec.LookPath("wg-quick"); e == nil {
		return p
	}
	return ""
}

func wgBinary() string {
	if v := os.Getenv("REELSCAREY_WG"); v != "" {
		return v
	}
	if p, e := exec.LookPath("wg"); e == nil {
		return p
	}
	return ""
}

func backoff(attempts int) time.Duration {
	d := time.Duration(1<<min(attempts, 6)) * time.Second
	if d > time.Minute {
		d = time.Minute
	}
	return d
}

// Supervise reconciles the tunnel with the saved configuration. Enabled means
// the interface exists; anything else means it does not. The tunnel comes up
// without routes, so only clients bound to it use it.
func (s *Service) Supervise(ctx context.Context, logger *slog.Logger) {
	if s.rt.notify == nil {
		s.rt.notify = make(chan struct{}, 1)
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	defer s.teardown(logger)
	for {
		c, present, e := s.load(ctx)
		if e != nil {
			if ctx.Err() != nil {
				return
			}
			logger.Warn("vpn state unavailable", "error", e)
			s.rt.set("failed", "The VPN configuration could not be read. Review the application logs.")
		} else {
			s.reconcile(ctx, logger, c, present)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-s.rt.notify:
		}
	}
}

func (s *Service) reconcile(ctx context.Context, logger *slog.Logger, c Config, present bool) {
	enabled := present && c.Enabled
	up, checkErr := s.interfaceUp()
	if wgBinary() == "" || wgQuick() == "" {
		if enabled {
			s.rt.set("failed", "The container does not include the WireGuard tools. Use the Docker image or install wg-quick and wg.")
		}
		return
	}
	if checkErr != nil {
		// The interface probe itself failed (permissions, missing kernel
		// support): fail closed instead of pretending the tunnel is up.
		if enabled {
			s.rt.set("failed", "The tunnel state could not be verified. Ensure the container has the NET_ADMIN capability and REELSCAREY_KEEP_NET_ADMIN=1.")
		}
		return
	}
	if !enabled {
		if up {
			s.teardown(logger)
		}
		return
	}
	if up {
		s.rt.mu.Lock()
		s.rt.up = true
		s.rt.attempts = 0
		s.rt.mu.Unlock()
		s.rt.set("up", "")
		return
	}
	if time.Since(s.rt.lastStart) < backoff(s.rt.attempts) {
		return
	}
	s.rt.mu.Lock()
	s.rt.attempts++
	s.rt.lastStart = time.Now()
	s.rt.mu.Unlock()
	if e := s.bringUp(ctx, c); e != nil {
		s.rt.set("failed", redact(c, e.Error()))
		logger.Warn("vpn tunnel failed", "error", redact(c, e.Error()))
		return
	}
	if tunnelUp, e := s.interfaceUp(); e != nil || !tunnelUp {
		s.rt.set("failed", "The tunnel interface did not appear. Review the VPN settings and container capabilities.")
		return
	}
	s.rt.mu.Lock()
	s.rt.up = true
	s.rt.attempts = 0
	s.rt.mu.Unlock()
	s.rt.set("up", "")
	logger.Info("vpn tunnel is up", "interface", TunnelInterface)
}

// bringUp writes the generated configuration to temporary storage and brings
// the interface up with routes suppressed: only clients bound to the
// interface use the tunnel.
func (s *Service) bringUp(ctx context.Context, c Config) error {
	dir := filepath.Join(os.TempDir(), "reelscarey-vpn")
	if e := os.MkdirAll(dir, 0o700); e != nil {
		return e
	}
	conf := filepath.Join(dir, TunnelInterface+".conf")
	allowed := c.AllowedIPs
	if allowed == "" {
		allowed = defaultAllow
	}
	var builder strings.Builder
	builder.WriteString("[Interface]\nPrivateKey = " + c.PrivateKey + "\nAddress = " + c.Address + "\nTable = off\n")
	builder.WriteString("[Peer]\nPublicKey = " + c.PeerKey + "\nAllowedIPs = " + allowed + "\nEndpoint = " + c.Endpoint + "\n")
	if c.Keepalive > 0 {
		builder.WriteString("PersistentKeepalive = " + fmt.Sprint(c.Keepalive) + "\n")
	}
	if e := os.WriteFile(conf, []byte(builder.String()), 0o600); e != nil {
		return e
	}
	quick := wgQuick()
	if quick == "" {
		return problem("vpn_missing", "The container does not include the WireGuard tools.")
	}
	wgCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(wgCtx, quick, "up", conf)
	output, e := cmd.CombinedOutput()
	if e != nil {
		detail := "The tunnel could not be started."
		if len(output) > 0 {
			detail += " " + strings.TrimSpace(redact(c, string(output)))
		}
		return problem("vpn_start", detail)
	}
	deadline := time.Now().Add(upTimeout)
	for {
		up, e := s.interfaceUp()
		if e == nil && up {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if time.Now().After(deadline) {
			return problem("vpn_timeout", "The tunnel interface did not appear in time. Review the VPN settings and container capabilities.")
		}
		time.Sleep(400 * time.Millisecond)
	}
}

// teardown brings the interface down. The generated file lives in temporary
// storage, so a missing file falls back to removing the interface directly.
func (s *Service) teardown(logger *slog.Logger) {
	s.rt.mu.Lock()
	wasUp := s.rt.up
	s.rt.up = false
	s.rt.mu.Unlock()
	conf := filepath.Join(os.TempDir(), "reelscarey-vpn", TunnelInterface+".conf")
	quick := wgQuick()
	if quick != "" {
		if _, e := os.Stat(conf); e == nil {
			cmd := exec.Command(quick, "down", conf)
			if output, e := cmd.CombinedOutput(); e != nil {
				logger.Warn("vpn teardown failed", "error", strings.TrimSpace(redact(Config{}, string(output))))
			}
		} else if wasUp {
			if ip, e := exec.LookPath("ip"); e == nil {
				_ = exec.Command(ip, "link", "del", TunnelInterface).Run()
			}
		}
	}
	s.rt.set("stopped", "")
}

// interfaceUp asks the wg tool whether the interface exists. A failing probe
// is reported as an error so the caller can fail closed.
func (s *Service) interfaceUp() (bool, error) {
	wg := wgBinary()
	if wg == "" {
		return false, problem("vpn_missing", "The container does not include the WireGuard tools.")
	}
	cmd := exec.Command(wg, "show", TunnelInterface)
	if _, e := cmd.CombinedOutput(); e != nil {
		var exit *exec.ExitError
		if errors.As(e, &exit) {
			return false, nil
		}
		return false, e
	}
	return true, nil
}

// redact removes the private key from any captured tool output before it
// reaches logs or status messages.
func redact(c Config, message string) string {
	if c.PrivateKey == "" {
		return message
	}
	return strings.ReplaceAll(message, c.PrivateKey, "[redacted]")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
