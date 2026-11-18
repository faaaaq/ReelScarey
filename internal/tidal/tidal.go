// Package tidal wraps the tidaler provider script into the narrow external
// downloader boundary ReelScarey uses for music acquisition. It never touches
// the HTTP layer and never logs credentials: the TIDAL token lives only in
// tidaler's own token.json inside the configured config directory.
package tidal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// DefaultQuality asks for TIDAL's best available stream. TIDAL serves the
// best the account and track actually have, so a 16-bit track is not
// fabricated into a 24-bit one.
const DefaultQuality = "HI_RES_LOSSLESS"

var qualities = map[string]bool{"LOW": true, "HIGH": true, "LOSSLESS": true, "HI_RES_LOSSLESS": true}

// TrackURL matches the tidal.com track links the provider returns and
// accepts. Non-track links are rejected before any process is started.
var TrackURL = regexp.MustCompile(`^https://(www\.)?tidal\.com/browse/track/[0-9]{1,20}$`)

// Status reports whether the provider can run and whether a TIDAL session
// exists. Available means the provider script ran, not that the account is
// usable; LoggedIn is the honest session state.
type Status struct {
	Available bool   `json:"available"`
	LoggedIn  bool   `json:"logged_in"`
	Quality   string `json:"quality,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

// Candidate is one bounded TIDAL search hit. ID is the tidal.com track URL
// the provider accepts back for download, so the acquisition journal records
// a source that names itself.
type Candidate struct {
	ID              string `json:"id"`
	Title           string `json:"title"`
	Artist          string `json:"artist"`
	Album           string `json:"album"`
	DurationSeconds int64  `json:"duration_seconds"`
}

// File is one downloaded audio file inside the caller's dedicated directory.
type File struct {
	Path   string
	Format string
}

// Entry is one track of an inspected album link.
type Entry struct {
	ID              string `json:"id"`
	Title           string `json:"title"`
	Channel         string `json:"channel"`
	DurationSeconds int64  `json:"duration_seconds"`
}

// Inspection reports what one TIDAL link resolves to.
type Inspection struct {
	Kind    string
	Title   string
	Channel string
	Entries []Entry
}

// Runner executes one provider invocation and returns its bounded stdout.
// It is a package-level variable only so tests can inject a fake; callers
// use New and must not replace it.
type Runner func(ctx context.Context, timeout time.Duration, env []string, args []string) (string, error)

// Service is the tidal provider adapter. All methods are safe for concurrent
// use because each invocation is an independent process.
type Service struct {
	python    string
	script    string
	configDir string
	quality   string
	run       Runner
}

// New validates configuration and returns a provider service. python and
// script must be non-empty; configDir must be an absolute path.
func New(python, script, configDir, quality string) (*Service, error) {
	python, script = strings.TrimSpace(python), strings.TrimSpace(script)
	quality = strings.ToUpper(strings.TrimSpace(quality))
	if quality == "" {
		quality = DefaultQuality
	}
	if !qualities[quality] {
		return nil, fmt.Errorf("REELSCAREY_TIDAL_QUALITY must be one of LOW, HIGH, LOSSLESS or HI_RES_LOSSLESS")
	}
	if python == "" || script == "" || !utf8.ValidString(python) || !utf8.ValidString(script) {
		return nil, errors.New("tidal provider requires a python executable and provider script")
	}
	absolute, err := filepath.Abs(configDir)
	if err != nil || strings.ContainsRune(absolute, 0) || utf8.RuneCountInString(absolute) > 1024 {
		return nil, errors.New("tidal provider config directory must be a valid path")
	}
	return &Service{python: python, script: script, configDir: absolute, quality: quality, run: realRun}, nil
}

// env isolates the provider: its TIDAL token lives under the configured
// ReelScarey config directory (so backups include it) and it never sees the
// owner's shell environment beyond the explicit handoff below.
func (s *Service) env() []string {
	return []string{
		"XDG_CONFIG_HOME=" + s.configDir,
		"HOME=" + s.configDir,
		"REELSCAREY_TIDAL_QUALITY=" + s.quality,
		"PYTHONNOUSERSITE=1",
		"PYTHONWARNINGS=ignore",
		"PATH=/usr/local/bin:/usr/bin:/bin",
	}
}

func (s *Service) call(ctx context.Context, timeout time.Duration, args ...string) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	return s.run(ctx, timeout, s.env(), append([]string{s.script}, args...))
}

// Status asks the provider whether it can run and whether a TIDAL session
// exists. A failing provider is a status, not a panic: music acquisition
// must keep working with the fallback source.
func (s *Service) Status(ctx context.Context) Status {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	body, err := s.call(ctx, 25*time.Second, "status")
	if err != nil {
		return Status{Detail: short(err.Error())}
	}
	var parsed Status
	if json.Unmarshal([]byte(body), &parsed) != nil {
		return Status{Detail: "the TIDAL provider returned an unreadable status"}
	}
	if !parsed.Available {
		parsed.Detail = short(parsed.Detail)
	}
	return parsed
}

// Verify performs a network login check with the stored token. This is the
// only call besides download that talks to TIDAL with credentials.
func (s *Service) Verify(ctx context.Context) Status {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	body, err := s.call(ctx, 80*time.Second, "verify")
	if err != nil {
		return Status{Available: true, Detail: short(err.Error())}
	}
	var parsed Status
	if json.Unmarshal([]byte(body), &parsed) != nil {
		return Status{Available: true, Detail: "the TIDAL provider returned an unreadable verification"}
	}
	parsed.Available = true
	return parsed
}

// LoginBegin starts TIDAL's device authorization flow in the provider's
// background thread and returns immediately. The device link and code are
// read through LoginStatus, never through this call.
func (s *Service) LoginBegin(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	body, err := s.call(ctx, 40*time.Second, "login-begin")
	if err != nil {
		return err
	}
	var parsed struct {
		Started bool   `json:"started"`
		Reason  string `json:"reason"`
	}
	if json.Unmarshal([]byte(body), &parsed) != nil {
		return errors.New("the TIDAL provider returned an unreadable login response")
	}
	if !parsed.Started {
		return fmt.Errorf("a TIDAL login is already in progress (%s)", parsed.Reason)
	}
	return nil
}

// LoginStatus reports the device flow state, including the link and code the
// owner must visit. It contains no secrets: TIDAL device codes are shown to
// the owner by design.
func (s *Service) LoginStatus(ctx context.Context) (LoginState, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	body, err := s.call(ctx, 25*time.Second, "login-status")
	if err != nil {
		return LoginState{}, err
	}
	var parsed LoginState
	if json.Unmarshal([]byte(body), &parsed) != nil {
		return LoginState{}, errors.New("the TIDAL provider returned an unreadable login status")
	}
	parsed.Lines = boundedLines(parsed.Lines)
	return parsed, nil
}

// LoginState is one snapshot of the device login flow.
type LoginState struct {
	Phase  string   `json:"phase"`
	Result string   `json:"result"`
	Error  string   `json:"error"`
	Lines  []string `json:"lines"`
}

func boundedLines(lines []string) []string {
	if len(lines) > 50 {
		lines = lines[len(lines)-50:]
	}
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if utf8.RuneCountInString(line) > 400 {
			line = string([]rune(line)[:400])
		}
		lines[i] = line
	}
	return lines
}

// SearchAudio returns TIDAL catalog hits for one bounded query.
func (s *Service) SearchAudio(ctx context.Context, query string, limit int) ([]Candidate, error) {
	query = strings.TrimSpace(query)
	if !utf8.ValidString(query) || utf8.RuneCountInString(query) < 1 || utf8.RuneCountInString(query) > 300 ||
		strings.ContainsFunc(query, func(r rune) bool { return r < 32 || r == 0x7f }) || limit < 1 || limit > 25 {
		return nil, errors.New("invalid TIDAL search query")
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	body, err := s.call(ctx, 50*time.Second, "search", query, strconv.Itoa(limit))
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Candidates []Candidate `json:"candidates"`
		Error      string      `json:"error"`
	}
	if json.Unmarshal([]byte(body), &parsed) != nil {
		return nil, errors.New("the TIDAL provider returned an unreadable search result")
	}
	if parsed.Error != "" {
		return nil, errors.New(short(parsed.Error))
	}
	out := []Candidate{}
	for _, candidate := range parsed.Candidates {
		if !TrackURL.MatchString(candidate.ID) || strings.TrimSpace(candidate.Title) == "" {
			continue
		}
		out = append(out, candidate)
	}
	return out, nil
}

// DownloadAudio downloads one TIDAL track link at the configured quality
// into the caller-owned directory. Callers verify the returned file before
// any import; this function never names a destination outside directory.
func (s *Service) DownloadAudio(ctx context.Context, id, directory, _ string) (File, error) {
	if !TrackURL.MatchString(id) {
		return File{}, errors.New("the audio source is not a valid TIDAL track link")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	body, err := s.call(ctx, 25*time.Minute, "download", id, directory)
	if err != nil {
		return File{}, err
	}
	var parsed struct {
		OK     bool   `json:"ok"`
		Path   string `json:"path"`
		Format string `json:"format"`
		Error  string `json:"error"`
	}
	if json.Unmarshal([]byte(body), &parsed) != nil {
		return File{}, errors.New("the TIDAL provider returned an unreadable download result")
	}
	if !parsed.OK {
		return File{}, errors.New(short(parsed.Error))
	}
	cleaned := filepath.Clean(directory)
	if !strings.HasPrefix(filepath.Clean(parsed.Path), cleaned+string(filepath.Separator)) && filepath.Clean(parsed.Path) != cleaned {
		return File{}, errors.New("the TIDAL download left its dedicated folder; nothing was imported")
	}
	return File{Path: parsed.Path, Format: strings.ToLower(parsed.Format)}, nil
}

// Inspect resolves one TIDAL track or album link for the direct-link flow.
func (s *Service) Inspect(ctx context.Context, rawURL string) (Inspection, error) {
	rawURL = strings.TrimSpace(rawURL)
	if !TrackURL.MatchString(rawURL) && !AlbumURL.MatchString(rawURL) {
		return Inspection{}, errors.New("the link is not a TIDAL track or album")
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	body, err := s.call(ctx, 50*time.Second, "inspect", rawURL)
	if err != nil {
		return Inspection{}, err
	}
	var parsed struct {
		Kind    string  `json:"kind"`
		Title   string  `json:"title"`
		Channel string  `json:"channel"`
		Entries []Entry `json:"entries"`
		Error   string  `json:"error"`
	}
	if json.Unmarshal([]byte(body), &parsed) != nil {
		return Inspection{}, errors.New("the TIDAL provider returned an unreadable inspection")
	}
	if parsed.Error != "" {
		return Inspection{}, errors.New(short(parsed.Error))
	}
	if parsed.Kind != "track" && parsed.Kind != "album" {
		return Inspection{}, errors.New("the TIDAL link resolved to nothing usable")
	}
	if len(parsed.Entries) > 100 {
		parsed.Entries = parsed.Entries[:100]
	}
	return Inspection{Kind: parsed.Kind, Title: parsed.Title, Channel: parsed.Channel, Entries: parsed.Entries}, nil
}

var AlbumURL = regexp.MustCompile(`^https://(www\.)?tidal\.com/browse/album/[0-9]{1,20}$`)

// Owns reports whether a candidate ID or link belongs to this provider. The
// music priority chain routes downloads and link inspection with it; it
// never starts a process. Album links are owned for inspection even though
// DownloadAudio rejects them with an honest error.
func (s *Service) Owns(id string) bool { return TrackURL.MatchString(id) || AlbumURL.MatchString(id) }

func realRun(ctx context.Context, timeout time.Duration, env []string, args []string) (string, error) {
	run, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(run, args[0], args[1:]...)
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if detail != "" {
			return "", fmt.Errorf("%s: %s", err, short(detail))
		}
		return "", err
	}
	// Bounded output: a broken provider that dumps megabytes must not be read
	// into memory unbounded.
	if stdout.Len() > 4<<20 {
		return "", errors.New("the TIDAL provider produced too much output")
	}
	return stdout.String(), nil
}

func short(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 300 {
		value = value[:300]
	}
	return value
}
