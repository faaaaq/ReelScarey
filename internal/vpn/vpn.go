// Package vpn supervises the opt-in Mullvad WireGuard tunnel used by the
// built-in torrent client. The interface is brought up without installing
// routes: the torrent client binds to it directly, so tunnel traffic stays
// scoped to the torrent client and the rest of the application keeps using
// the normal network. If the interface disappears, the torrent client has no
// egress and the bundled-client supervisor stops it until the tunnel returns.
package vpn

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net"
	"strings"
	"time"

	"github.com/faaaaq/ReelScarey/internal/config"
	"github.com/faaaaq/ReelScarey/internal/securestore"
)

const (
	configName   = "mullvad"
	defaultAllow = "0.0.0.0/0"
	upTimeout    = 45 * time.Second
	downTimeout  = 15 * time.Second
)

// TunnelInterface is the fixed WireGuard interface name the tunnel uses and
// the torrent client binds to.
const TunnelInterface = "reelscarey0"

// Error carries an operator-facing problem code, mirroring the other
// domain packages.
type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string           { return e.Message }
func problem(code, message string) error { return &Error{code, message} }

// Config is the encrypted WireGuard configuration. The private key is stored
// through the encrypted integration store and never returned by the API or
// written to logs.
type Config struct {
	Revision   int    `json:"revision"`
	Enabled    bool   `json:"enabled"`
	PrivateKey string `json:"private_key,omitempty"`
	Address    string `json:"address"`
	PeerKey    string `json:"peer_key"`
	Endpoint   string `json:"endpoint"`
	AllowedIPs string `json:"allowed_ips,omitempty"`
	Keepalive  int    `json:"keepalive,omitempty"`
}

// Status reports the tunnel honestly. State is stopped, starting, up or
// failed; a failed tunnel carries its message with secrets redacted.
type Status struct {
	Present   bool   `json:"present"`
	Enabled   bool   `json:"enabled"`
	State     string `json:"state"`
	Message   string `json:"message,omitempty"`
	Interface string `json:"interface,omitempty"`
	Address   string `json:"address,omitempty"`
	Endpoint  string `json:"endpoint,omitempty"`
	Revision  int    `json:"revision"`
}

type Update struct {
	Revision   int    `json:"revision"`
	Enabled    *bool  `json:"enabled,omitempty"`
	PrivateKey string `json:"private_key,omitempty"`
	Address    string `json:"address,omitempty"`
	PeerKey    string `json:"peer_key,omitempty"`
	Endpoint   string `json:"endpoint,omitempty"`
	AllowedIPs string `json:"allowed_ips,omitempty"`
}

// load returns the saved configuration; present is false when none exists.
func (s *Service) load(ctx context.Context) (Config, bool, error) {
	plain, e := s.secrets.GetNamed(ctx, configName)
	if e != nil {
		return Config{}, false, e
	}
	if len(plain) == 0 {
		return Config{}, false, nil
	}
	var c Config
	if e = json.Unmarshal(plain, &c); e != nil {
		return Config{}, false, problem("invalid_vpn", "The saved VPN configuration is unreadable. Re-enter it.")
	}
	return c, true, nil
}

func (s *Service) save(ctx context.Context, c Config) error {
	plain, e := json.Marshal(c)
	if e != nil {
		return e
	}
	return s.secrets.SetNamed(ctx, configName, plain)
}

// Status never returns the private key.
func (s *Service) Status(ctx context.Context) (Status, error) {
	out := Status{State: "stopped", Interface: TunnelInterface}
	c, present, e := s.load(ctx)
	if e != nil {
		return out, e
	}
	out.Present = present
	if !present {
		return out, nil
	}
	out.Enabled = c.Enabled
	out.Address = c.Address
	out.Endpoint = c.Endpoint
	out.Revision = c.Revision
	state, message := s.rt.snapshot()
	out.State, out.Message = state, message
	if !c.Enabled {
		out.State = "stopped"
	}
	return out, nil
}

// Update saves the configuration and optionally enables the tunnel. Enabling
// waits for the interface and reverts to disabled when it does not come up,
// leaving the error visible instead of a silently broken tunnel.
func (s *Service) Update(ctx context.Context, in Update) (Status, error) {
	c, present, e := s.load(ctx)
	if e != nil {
		return Status{}, e
	}
	if present && in.Revision != c.Revision {
		return Status{}, problem("revision_conflict", "The VPN settings changed. Reload and try again.")
	}
	if !present && in.Revision != 0 {
		return Status{}, problem("revision_conflict", "The VPN settings changed. Reload and try again.")
	}
	enabled := c.Enabled
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	next := Config{Revision: c.Revision + 1, Enabled: enabled, Address: c.Address, PeerKey: c.PeerKey, Endpoint: c.Endpoint, AllowedIPs: c.AllowedIPs, Keepalive: c.Keepalive}
	if in.Address != "" {
		next.Address = strings.TrimSpace(in.Address)
	}
	if in.PeerKey != "" {
		next.PeerKey = strings.TrimSpace(in.PeerKey)
	}
	if in.Endpoint != "" {
		next.Endpoint = strings.TrimSpace(in.Endpoint)
	}
	if in.AllowedIPs != "" {
		next.AllowedIPs = strings.TrimSpace(in.AllowedIPs)
	}
	if in.PrivateKey != "" {
		next.PrivateKey = strings.TrimSpace(in.PrivateKey)
	} else if present {
		next.PrivateKey = c.PrivateKey
	}
	if next.AllowedIPs == "" {
		next.AllowedIPs = defaultAllow
	}
	if next.Keepalive == 0 {
		next.Keepalive = 25
	}
	if e = validate(&next, present && in.PrivateKey == ""); e != nil {
		return Status{}, e
	}
	if e = s.save(ctx, next); e != nil {
		return Status{}, e
	}
	s.poke()
	if enabled {
		if e = s.awaitUp(ctx); e != nil {
			// The tunnel did not come up: revert to disabled so nothing waits
			// on a broken configuration, and surface the failure honestly.
			next.Enabled = false
			_ = s.save(ctx, next)
			s.poke()
			return Status{}, e
		}
	} else if e = s.awaitStopped(ctx); e != nil {
		return Status{}, e
	}
	return s.Status(ctx)
}

func awaitState(ctx context.Context, s *Service, state string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if got, _ := s.rt.snapshot(); got == state {
			return nil
		}
		if time.Now().After(deadline) {
			return problem("vpn_timeout", "The tunnel did not reach the expected state in time. Review the VPN status.")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}
}

func (s *Service) awaitUp(ctx context.Context) error {
	if e := awaitState(ctx, s, "up", upTimeout); e != nil {
		return e
	}
	if _, message := s.rt.snapshot(); message != "" {
		return problem("vpn_start", message)
	}
	return nil
}

func (s *Service) awaitStopped(ctx context.Context) error {
	return awaitState(ctx, s, "stopped", downTimeout)
}

func validate(c *Config, keyRetained bool) error {
	if c.Enabled {
		if c.PrivateKey == "" && !keyRetained {
			return problem("invalid_vpn", "Enter the WireGuard private key from your Mullvad configuration.")
		}
		if c.PrivateKey != "" && !validKey(c.PrivateKey) {
			return problem("invalid_vpn", "The WireGuard private key must be the 44-character base64 key from your Mullvad configuration.")
		}
		if _, _, e := net.ParseCIDR(c.Address); e != nil {
			return problem("invalid_vpn", "The tunnel address must be a CIDR such as 10.66.123.45/32 from your Mullvad configuration.")
		}
		if !validKey(c.PeerKey) {
			return problem("invalid_vpn", "The server public key must be the 44-character base64 key from your Mullvad configuration.")
		}
		if _, _, e := net.SplitHostPort(c.Endpoint); e != nil {
			return problem("invalid_vpn", "The endpoint must be host:port, for example: 45.83.90.9:51820.")
		}
		for _, allowed := range strings.Split(c.AllowedIPs, ",") {
			if _, _, e := net.ParseCIDR(strings.TrimSpace(allowed)); e != nil {
				return problem("invalid_vpn", "Allowed addresses must be comma-separated CIDRs such as 0.0.0.0/0.")
			}
		}
	}
	return nil
}

func validKey(v string) bool {
	b, e := base64.StdEncoding.DecodeString(strings.TrimSpace(v))
	return e == nil && len(b) == 32
}

// Service owns the encrypted configuration and the tunnel lifecycle.
type Service struct {
	db      *sql.DB
	secrets *securestore.Store
	cfg     config.Config
	rt      runtimeState
}

// New builds the VPN service. The supervisor runs separately from the main
// worker loop so tunnel state changes do not queue behind transfers.
func New(db *sql.DB, secrets *securestore.Store, cfg config.Config) *Service {
	return &Service{db: db, secrets: secrets, cfg: cfg, rt: runtimeState{notify: make(chan struct{}, 1)}}
}

func (s *Service) poke() {
	select {
	case s.rt.notify <- struct{}{}:
	default:
	}
}

// TunnelUp reports whether torrent work may proceed: true when the VPN is not
// enabled, or when the tunnel interface is present. The bundled-client
// supervisor consumes this as its kill-switch gate.
func (s *Service) TunnelUp() bool {
	s.rt.mu.Lock()
	defer s.rt.mu.Unlock()
	return s.rt.up
}
