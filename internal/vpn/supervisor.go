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

func wgBinary() string {
	if v := os.Getenv("REELSCAREY_WG"); v != "" {
		return v
	}
	if p, e := exec.LookPath("wg"); e == nil {
		return p
	}
	return ""
}

func ipBinary() string {
	if v := os.Getenv("REELSCAREY_IP"); v != "" {
		return v
	}
	if p, e := exec.LookPath("ip"); e == nil {
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
// the interface exists; anything else means it does not. The tunnel is brought
// up with plain wg/ip calls because wg-quick insists on root (it re-execs via
// sudo), while the application intentionally runs as an unprivileged user with
// only NET_ADMIN retained.
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
	if wgBinary() == "" || ipBinary() == "" {
		if enabled {
			s.rt.set("failed", "The container does not include wireguard-tools and iproute2. Use the Docker image or install them.")
		}
		return
	}
	up, checkErr := s.interfaceUp()
	if checkErr != nil {
		// The interface probe itself failed (permissions, missing kernel
		// support): fail closed instead of pretending the tunnel is up.
		if enabled {
			s.rt.set("failed", "The tunnel state could not be verified. Ensure the container has NET_ADMIN and REELSCAREY_KEEP_NET_ADMIN=1.")
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

// run executes a bounded helper command as the application user. wg and ip
// need NET_ADMIN, which the entrypoint keeps as an ambient capability when the
// operator opts in; they never need root.
func run(ctx context.Context, binary string, args ...string) (string, error) {
	runCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	out, e := exec.CommandContext(runCtx, binary, args...).CombinedOutput()
	return strings.TrimSpace(string(out)), e
}

// addresses splits the saved comma-separated address list and reports which
// families are present. Mullvad supplies an IPv4 and an IPv6 address.
func addresses(raw string) (list []string, has4, has6 bool) {
	for _, part := range strings.Split(raw, ",") {
		addr := strings.TrimSpace(part)
		if addr == "" {
			continue
		}
		list = append(list, addr)
		if strings.Contains(addr, ":") {
			has6 = true
		} else {
			has4 = true
		}
	}
	return list, has4, has6
}

// bringUp creates the interface, applies the WireGuard configuration and
// installs a separate routing table selected only for sockets bound to the
// interface. Everything else keeps using the main table, so the tunnel never
// captures application or web traffic.
func (s *Service) bringUp(ctx context.Context, c Config) error {
	wg, ip := wgBinary(), ipBinary()
	if wg == "" || ip == "" {
		return problem("vpn_missing", "The container does not include wireguard-tools and iproute2.")
	}
	s.linkDown(ctx, ip)

	if out, e := run(ctx, ip, "link", "add", TunnelInterface, "type", "wireguard"); e != nil {
		return problem("vpn_start", "The WireGuard interface could not be created. "+out)
	}
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
	builder.WriteString("[Interface]\nPrivateKey = " + c.PrivateKey + "\n")
	builder.WriteString("[Peer]\nPublicKey = " + c.PeerKey + "\nAllowedIPs = " + allowed + "\nEndpoint = " + c.Endpoint + "\n")
	if c.Keepalive > 0 {
		builder.WriteString("PersistentKeepalive = " + fmt.Sprint(c.Keepalive) + "\n")
	}
	if e := os.WriteFile(conf, []byte(builder.String()), 0o600); e != nil {
		return e
	}
	if out, e := run(ctx, wg, "setconf", TunnelInterface, conf); e != nil {
		_ = os.Remove(conf)
		return problem("vpn_start", "The WireGuard configuration was rejected. "+out)
	}
	_ = os.Remove(conf)

	list, has4, has6 := addresses(c.Address)
	for _, addr := range list {
		if out, e := run(ctx, ip, "addr", "add", addr, "dev", TunnelInterface); e != nil {
			// IPv6 is optional: a container without IPv6 should still run the
			// IPv4 tunnel rather than failing the whole connection.
			if strings.Contains(addr, ":") {
				continue
			}
			return problem("vpn_start", "The tunnel address "+addr+" could not be applied. "+out)
		}
	}
	if out, e := run(ctx, ip, "link", "set", "mtu", "1420", "up", "dev", TunnelInterface); e != nil {
		return problem("vpn_start", "The tunnel interface could not be enabled. "+out)
	}
	// Policy routing: only sockets bound to the tunnel use its default route.
	if has4 {
		if out, e := run(ctx, ip, "route", "add", "default", "dev", TunnelInterface, "table", routeTable); e != nil {
			return problem("vpn_start", "The tunnel route could not be installed. "+out)
		}
		_, _ = run(ctx, ip, "rule", "del", "oif", TunnelInterface, "table", routeTable, "priority", rulePriority)
		if out, e := run(ctx, ip, "rule", "add", "oif", TunnelInterface, "table", routeTable, "priority", rulePriority); e != nil {
			return problem("vpn_start", "The tunnel routing rule could not be installed. "+out)
		}
	}
	if has6 {
		if out, e := run(ctx, ip, "-6", "route", "add", "default", "dev", TunnelInterface, "table", routeTable); e == nil {
			_, _ = run(ctx, ip, "-6", "rule", "del", "oif", TunnelInterface, "table", routeTable, "priority", rulePriority)
			_, _ = run(ctx, ip, "-6", "rule", "add", "oif", TunnelInterface, "table", routeTable, "priority", rulePriority)
		} else if !strings.Contains(out, "not supported") && !strings.Contains(out, "Cannot assign") {
			return problem("vpn_start", "The IPv6 tunnel route could not be installed. "+out)
		}
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

// teardown removes the routing rules and interface. Each step is best-effort:
// a partially created interface must never block recovery.
func (s *Service) teardown(logger *slog.Logger) {
	s.rt.mu.Lock()
	s.rt.up = false
	s.rt.mu.Unlock()
	s.linkDown(context.Background(), ipBinary())
	s.rt.set("stopped", "")
}

func (s *Service) linkDown(ctx context.Context, ip string) {
	if ip == "" {
		return
	}
	_, _ = run(ctx, ip, "rule", "del", "oif", TunnelInterface, "table", routeTable, "priority", rulePriority)
	_, _ = run(ctx, ip, "-6", "rule", "del", "oif", TunnelInterface, "table", routeTable, "priority", rulePriority)
	_, _ = run(ctx, ip, "link", "del", TunnelInterface)
	_ = os.Remove(filepath.Join(os.TempDir(), "reelscarey-vpn", TunnelInterface+".conf"))
}

// interfaceUp asks wg whether the interface exists. A failing probe is
// reported as an error so the caller can fail closed.
func (s *Service) interfaceUp() (bool, error) {
	wg := wgBinary()
	if wg == "" {
		return false, problem("vpn_missing", "The container does not include wireguard-tools.")
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
