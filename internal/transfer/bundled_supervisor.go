package transfer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/faaaaq/ReelScarey/internal/vpn"
)

// bundledRuntime owns the supervised daemon process. The supervisor loop is
// the only writer; status readers get an honest state without touching the
// process.
// lockedBuffer wraps the bounded stderr capture so the supervisor can read it
// while the child process is still running and the exec copier is still
// writing. Without the lock the race detector correctly flags the reads.
type lockedBuffer struct {
	mu  sync.Mutex
	buf limitedBuffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type bundledRuntime struct {
	procMu           sync.Mutex
	stateMu          sync.Mutex
	cmd              *exec.Cmd
	wait             chan error
	stderr           *lockedBuffer
	state            string
	message          string
	lifecycle        int64
	attempts         int
	lastAttempt      time.Time
	appliedRevision  int
	tunnelReadyCache bool
	tunnelBindCache  bool
	notify           chan struct{}
	// label names the supervised daemon in honest process-exit messages.
	label string
	// redact lists secrets removed from captured crash output.
	redact []string
}

func (r *bundledRuntime) snapshot() (string, string) {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	return r.state, r.message
}

func (r *bundledRuntime) generation() int64 {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	return r.lifecycle
}

func (r *bundledRuntime) setState(state, message string) {
	r.stateMu.Lock()
	r.state, r.message = state, message
	r.stateMu.Unlock()
}

// finishFailed closes a lifecycle attempt with a failure: awaiting callers see
// the generation advance and surface the message instead of timing out.
func (r *bundledRuntime) finishFailed(message string) {
	r.stateMu.Lock()
	r.lifecycle++
	r.state, r.message = "failed", message
	r.stateMu.Unlock()
}

func (r *bundledRuntime) poke() {
	if r.notify == nil {
		return
	}
	select {
	case r.notify <- struct{}{}:
	default:
	}
}

func (r *bundledRuntime) alive() bool {
	r.procMu.Lock()
	defer r.procMu.Unlock()
	return r.cmd != nil
}

// reap detects an exited child without blocking. A crash increments the
// backoff so a failing daemon does not spin.
func (r *bundledRuntime) reap() {
	r.procMu.Lock()
	cmd, wait := r.cmd, r.wait
	if cmd == nil || wait == nil {
		r.procMu.Unlock()
		return
	}
	select {
	case <-wait:
		label := r.label
		if label == "" {
			label = "built-in client"
		}
		detail := "The " + label + " stopped unexpectedly."
		if r.stderr != nil && strings.TrimSpace(r.stderr.String()) != "" {
			detail += " " + strings.TrimSpace(r.stderr.String())
		}
		for _, secret := range r.redact {
			if secret != "" {
				detail = strings.ReplaceAll(detail, secret, "[redacted]")
			}
		}
		detail = cleanClientText(detail, 1200)
		r.cmd, r.wait = nil, nil
		r.attempts++
		r.lastAttempt = time.Now()
		r.procMu.Unlock()
		r.finishFailed(detail)
	default:
		r.procMu.Unlock()
	}
}

func bundledBinary() string {
	if v := os.Getenv("REELSCAREY_BUNDLED_QBT"); v != "" {
		return v
	}
	if p, e := exec.LookPath("qbittorrent-nox"); e == nil {
		return p
	}
	return ""
}

func bundledBackoff(attempts int) time.Duration {
	d := time.Duration(1<<min(attempts, 6)) * time.Second
	if d > time.Minute {
		d = time.Minute
	}
	return d
}

// SuperviseBundled reconciles the supervised daemon with the saved built-in
// connection: enabled means a tested, running daemon; disabled or absent means
// no process. Every transition is journaled in the runtime state so the API
// reports what actually happened.
func (s *Service) SuperviseBundled(ctx context.Context, logger *slog.Logger) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	defer s.stopBundled()
	for {
		s.provisionBundled(ctx, logger)
		c, revision, desired, e := s.bundledDesired(ctx)
		if e != nil {
			if ctx.Err() != nil {
				return
			}
			logger.Warn("built-in client state unavailable", "error", e)
			s.bundled.finishFailed("The built-in client state could not be read. Review the application logs.")
		} else {
			s.reconcileBundled(ctx, logger, c, revision, desired)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-s.bundled.notify:
		}
	}
}

// bundledDesired returns the saved configuration when the built-in connection
// is enabled. An enabled row with an unreadable configuration fails closed:
// the daemon must not run against guessed settings.
func (s *Service) bundledDesired(ctx context.Context) (ClientConfig, int, bool, error) {
	present, enabled, revision, e := s.bundledRow(ctx)
	if e != nil || !present || !enabled {
		return ClientConfig{}, 0, false, e
	}
	c, e := s.managedClientConfig(ctx, builtinClientID)
	if e != nil {
		return ClientConfig{}, 0, false, e
	}
	return c, revision, true, nil
}

func (s *Service) reconcileBundled(ctx context.Context, logger *slog.Logger, c ClientConfig, revision int, desired bool) {
	r := &s.bundled
	r.reap()
	r.procMu.Lock()
	wasAlive := r.cmd != nil
	stale := wasAlive && r.appliedRevision != revision
	prevAllowed, prevBind := r.tunnelReadyCache, r.tunnelBindCache
	currentAllowed, currentBind := s.tunnelReady(), s.tunnelBound()
	r.tunnelReadyCache, r.tunnelBindCache = currentAllowed, currentBind
	r.procMu.Unlock()
	tunnelChanged := wasAlive && (prevAllowed != currentAllowed || prevBind != currentBind)
	if !desired {
		if wasAlive {
			s.stopBundled()
		} else {
			r.setState("stopped", "")
		}
		return
	}
	// The VPN kill switch: with the tunnel down and the switch on, the daemon
	// does not run at all, so torrent traffic can never fall back to the bare
	// interface. With the kill switch off, it may run unbound.
	if !currentAllowed {
		if wasAlive {
			s.stopBundled()
		}
		r.setState("stopped", tunnelDownMessage)
		return
	}
	if wasAlive && !stale && !tunnelChanged {
		return
	}
	if stale || tunnelChanged {
		s.stopBundled()
	}
	if time.Since(r.lastAttempt) < bundledBackoff(r.attempts) {
		return
	}
	r.attempts++
	r.lastAttempt = time.Now()
	if e := s.startBundled(ctx, c, revision); e != nil {
		var p *Error
		message := "The built-in client could not start."
		if errors.As(e, &p) {
			message = p.Message
		} else {
			message = fmt.Sprintf("The built-in client could not start: %v", e)
		}
		r.finishFailed(message)
		logger.Warn("built-in client start failed", "error", e)
		return
	}
	r.attempts = 0
	logger.Info("built-in qBittorrent client is running", "peer_port", c.PeerPort)
}

// startBundled writes the generated configuration and supervises one daemon
// lifetime. The RPC address stays inside the container loopback, so the
// application is the only API client.
func (s *Service) startBundled(ctx context.Context, c ClientConfig, revision int) error {
	binary := bundledBinary()
	if binary == "" {
		return problem("builtin_missing", "qbittorrent-nox is not installed in this deployment. The Docker image ships it; native installs must provide it.")
	}
	if c.PeerPort < 1 {
		c.PeerPort = defaultBuiltinPeer
	}
	profile := filepath.Join(s.cfg.ConfigDir, "qbittorrent")
	confDir := filepath.Join(profile, "qBittorrent", "config")
	if e := os.MkdirAll(confDir, 0o700); e != nil {
		return e
	}
	conf := "[LegalNotice]\nAccepted=true\n[Preferences]\n" +
		"WebUI\\Address=127.0.0.1\n" +
		"WebUI\\Port=" + strconv.Itoa(builtinWebPort) + "\n" +
		"WebUI\\Username=" + c.Username + "\n" +
		"WebUI\\Password_PBKDF2=" + qbtPBKDF2(c.Password) + "\n" +
		"WebUI\\LocalHostAuth=true\n" +
		"WebUI\\CSRFProtection=true\n" +
		"WebUI\\HostHeaderValidation=true\n" +
		"[BitTorrent]\n" +
		"Session\\Port=" + strconv.Itoa(c.PeerPort) + "\n" +
		"Session\\DefaultSavePath=" + c.DownloadPath + "\n"
	// The VPN kill switch: when the tunnel is up, the daemon binds every
	// torrent socket to the tunnel interface, so it has no egress without it.
	if s.tunnelBound() {
		conf += "Session\\Interface=" + vpn.TunnelInterface + "\n"
		conf += "Session\\InterfaceName=" + vpn.TunnelInterface + "\n"
	}
	if e := os.WriteFile(filepath.Join(confDir, "qBittorrent.conf"), []byte(conf), 0o600); e != nil {
		return e
	}
	// The accepted notice lives in the generated configuration: the
	// command-line notice flag is unavailable on supported clients such as
	// Ubuntu's qBittorrent 4.6.3.
	cmd := exec.Command(binary, "--profile="+profile, "--webui-port="+strconv.Itoa(builtinWebPort))
	hideProbeWindow(cmd)
	stderr := &lockedBuffer{}
	stderr.buf.max = 64 << 10
	cmd.Stderr = stderr
	if e := cmd.Start(); e != nil {
		return e
	}
	r := &s.bundled
	wait := make(chan error, 1)
	r.procMu.Lock()
	r.cmd = cmd
	r.wait = wait
	r.stderr = stderr
	r.appliedRevision = revision
	r.setState("starting", "")
	r.procMu.Unlock()
	go func() { wait <- cmd.Wait() }()
	deadline := time.Now().Add(builtinReadyTimeout)
	for {
		if e := s.testDownloadClient(ctx, "qbittorrent", c); e == nil {
			r.stateMu.Lock()
			r.lifecycle++
			r.state, r.message = "running", ""
			r.stateMu.Unlock()
			return nil
		} else if ctx.Err() != nil {
			return ctx.Err()
		}
		select {
		case <-wait:
			r.procMu.Lock()
			r.cmd, r.wait = nil, nil
			r.procMu.Unlock()
			detail := "The built-in client exited during startup."
			if strings.TrimSpace(stderr.String()) != "" {
				detail += " " + strings.TrimSpace(stderr.String())
			}
			return problem("builtin_start", detail)
		case <-time.After(400 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			_ = cmd.Process.Kill()
			<-wait
			r.procMu.Lock()
			r.cmd, r.wait = nil, nil
			r.procMu.Unlock()
			return problem("builtin_timeout", "The built-in client API did not become reachable in time. Review its status before retrying.")
		}
	}
}

// stopBundled terminates the supervised daemon gracefully, then forces it.
func (s *Service) stopBundled() { s.stopRuntime(&s.bundled) }

// stopRuntime is shared by the supervised torrent and Usenet daemons.
func (s *Service) stopRuntime(r *bundledRuntime) {
	r.procMu.Lock()
	cmd := r.cmd
	wait := r.wait
	if cmd == nil {
		r.procMu.Unlock()
		r.setState("stopped", "")
		return
	}
	r.cmd, r.wait = nil, nil
	r.procMu.Unlock()
	// The bundled client ships only in the Linux container, where the daemon
	// stops gracefully on interrupt and writes its fastresume state. Windows
	// development cannot deliver that signal to an arbitrary child, so it
	// terminates directly.
	if runtime.GOOS == "windows" {
		_ = cmd.Process.Kill()
	} else {
		_ = cmd.Process.Signal(os.Interrupt)
	}
	if wait != nil {
		select {
		case <-wait:
		case <-time.After(builtinStopTimeout):
			_ = cmd.Process.Kill()
			<-wait
		}
	}
	r.setState("stopped", "")
}
