package music

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	// fpcalcTimeout bounds one Chromaprint invocation. Fingerprinting decodes
	// only the requested prefix, but a damaged file must never hang a request.
	fpcalcTimeout = 20 * time.Second
	// fpcalcMaxOutput and fpcalcMaxError bound the child process output.
	fpcalcMaxOutput = 1 << 20
	fpcalcMaxError  = 64 << 10
	// fingerprintLengthSeconds matches the reviewed scope: the first 120
	// seconds are enough for AcoustID and keep provider work bounded.
	fingerprintLengthSeconds = 120
	// fingerprintMaxLength bounds a provider/cache value before it is stored
	// or sent back to AcoustID.
	fingerprintMaxLength = 100000
	fingerprintMaxSecond = 24 * 60 * 60
)

// Fingerprint is the Chromaprint evidence used for an AcoustID lookup. Only the
// whole-file duration and the fingerprint string leave the server; audio bytes
// are never uploaded.
type Fingerprint struct {
	DurationSeconds int
	Value           string
}

// Fingerprinter resolves a local audio file to Chromaprint evidence. The
// interface is injectable so requests can be tested without ffmpeg/fpcalc and
// so a deployment can report availability before running any file.
type Fingerprinter interface {
	Available(ctx context.Context) error
	Fingerprint(ctx context.Context, filename string) (Fingerprint, error)
}

// Chromaprint runs the Debian libchromaprint-tools binary. The command is
// fpcalc unless REELSCAREY_FPCALC_PATH names an explicit executable. It never
// uses a shell and validates the media path before the caller invokes it.
type Chromaprint struct {
	Timeout time.Duration
}

func NewChromaprint() *Chromaprint { return &Chromaprint{Timeout: fpcalcTimeout} }

func fpcalcCommand() string {
	if custom := strings.TrimSpace(os.Getenv("REELSCAREY_FPCALC_PATH")); custom != "" {
		return custom
	}
	return "fpcalc"
}

// Available reports whether the configured fpcalc executable can be found. It
// does not read or fingerprint any media file.
func (c *Chromaprint) Available(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := exec.LookPath(fpcalcCommand()); err != nil {
		return &Error{
			Code:    "music_fingerprint_unavailable",
			Message: "Chromaprint fpcalc is not available. Use the ReelScarey Docker image, which includes libchromaprint-tools, or set REELSCAREY_FPCALC_PATH to the fpcalc executable.",
		}
	}
	return nil
}

// Fingerprint runs `fpcalc -json -length 120 <file>` with a bounded timeout and
// output size. The parsed duration is a whole-file value in seconds.
func (c *Chromaprint) Fingerprint(ctx context.Context, filename string) (Fingerprint, error) {
	if err := c.Available(ctx); err != nil {
		return Fingerprint{}, err
	}
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = fpcalcTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, fpcalcCommand(), "-json", "-length", strconv.Itoa(fingerprintLengthSeconds), filename)
	stdout := &boundedBuffer{max: fpcalcMaxOutput}
	stderr := &boundedBuffer{max: fpcalcMaxError}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.WaitDelay = 2 * time.Second
	err := cmd.Run()
	if err != nil {
		switch {
		case errors.Is(runCtx.Err(), context.DeadlineExceeded):
			return Fingerprint{}, &Error{Code: "music_fingerprint_unavailable", Message: "Chromaprint took too long to fingerprint this file. Try again or scan fewer files."}
		case stdout.overflow || stderr.overflow:
			return Fingerprint{}, &Error{Code: "music_fingerprint_failed", Message: "Chromaprint returned an oversized response for this file."}
		case errors.Is(err, context.Canceled):
			return Fingerprint{}, &Error{Code: "music_fingerprint_unavailable", Message: "Fingerprinting was cancelled."}
		}
		message := "Chromaprint could not fingerprint this file. It may be corrupt or use an unsupported audio codec."
		if detail := cleanProviderDetail(stderr.String()); detail != "" {
			message += " " + detail
		}
		return Fingerprint{}, &Error{Code: "music_fingerprint_failed", Message: message}
	}
	fingerprint, err := parseFPCalcOutput(stdout.Bytes())
	if err != nil {
		return Fingerprint{}, err
	}
	return fingerprint, nil
}

func parseFPCalcOutput(body []byte) (Fingerprint, error) {
	var raw struct {
		Duration    float64 `json:"duration"`
		Fingerprint string  `json:"fingerprint"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return Fingerprint{}, &Error{Code: "music_fingerprint_failed", Message: "Chromaprint returned an unreadable fingerprint response."}
	}
	if math.IsNaN(raw.Duration) || math.IsInf(raw.Duration, 0) ||
		raw.Duration <= 0 || raw.Duration > fingerprintMaxSecond {
		return Fingerprint{}, &Error{Code: "music_fingerprint_failed", Message: "Chromaprint returned an invalid duration for this file."}
	}
	if !validFingerprintValue(raw.Fingerprint) {
		return Fingerprint{}, &Error{Code: "music_fingerprint_failed", Message: "Chromaprint returned an empty or invalid fingerprint for this file."}
	}
	seconds := int(math.Round(raw.Duration))
	if seconds < 1 {
		seconds = 1
	}
	if seconds > fingerprintMaxSecond {
		seconds = fingerprintMaxSecond
	}
	return Fingerprint{DurationSeconds: seconds, Value: raw.Fingerprint}, nil
}

func validFingerprintValue(value string) bool {
	if value == "" || len(value) > fingerprintMaxLength || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}

func validDurationSeconds(value int) bool {
	return value >= 1 && value <= fingerprintMaxSecond
}

func cleanProviderDetail(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if index := strings.LastIndexAny(value, "\r\n"); index >= 0 {
		value = strings.TrimSpace(value[index+1:])
	}
	if len(value) > 300 {
		value = value[:300]
	}
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, value)
	return strings.TrimSpace(value)
}

// boundedBuffer stops a child process from producing unbounded output; the
// shared implementation reports overflow through the overflow flag.

type cachedFingerprint struct {
	SizeBytes       int64
	ModifiedAt      int64
	DurationSeconds int
	Value           string
}

// CachedFingerprint returns the saved evidence for one relative media path. The
// caller must compare SizeBytes/ModifiedAt with the current file before reuse.
func (s *Store) CachedFingerprint(ctx context.Context, relativePath string) (cachedFingerprint, bool, error) {
	var out cachedFingerprint
	err := s.db.QueryRowContext(ctx, `SELECT size_bytes,modified_at,duration_seconds,fingerprint FROM music_fingerprints WHERE relative_path=?`, relativePath).
		Scan(&out.SizeBytes, &out.ModifiedAt, &out.DurationSeconds, &out.Value)
	if errors.Is(err, sql.ErrNoRows) {
		return cachedFingerprint{}, false, nil
	}
	if err != nil {
		return cachedFingerprint{}, false, err
	}
	if out.SizeBytes < 0 || !validDurationSeconds(out.DurationSeconds) || !validFingerprintValue(out.Value) {
		return cachedFingerprint{}, false, nil
	}
	return out, true, nil
}

// SaveFingerprint replaces the cache row for a path. A changed size or mtime is
// intentionally a replace, never an append-only history of media evidence.
func (s *Store) SaveFingerprint(ctx context.Context, relativePath string, sizeBytes, modifiedAt int64, fingerprint Fingerprint) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO music_fingerprints(relative_path,size_bytes,modified_at,duration_seconds,fingerprint,updated_at)
		VALUES(?,?,?,?,?,unixepoch())
		ON CONFLICT(relative_path) DO UPDATE SET size_bytes=excluded.size_bytes,modified_at=excluded.modified_at,duration_seconds=excluded.duration_seconds,fingerprint=excluded.fingerprint,updated_at=excluded.updated_at`,
		relativePath, sizeBytes, modifiedAt, fingerprint.DurationSeconds, fingerprint.Value)
	return err
}

// safeMediaPath revalidates containment and regular-file status immediately
// before any external process reads the file. The resolved path must stay below
// the resolved media root, so a symlinked parent folder cannot escape it.
func (s *Service) safeMediaPath(relativePath string) (string, os.FileInfo, error) {
	if s.mediaDir == "" {
		return "", nil, &Error{Code: "music_fingerprint_input", Message: "The media root is not configured."}
	}
	relativePath = strings.TrimSpace(strings.ReplaceAll(relativePath, "\\", "/"))
	if relativePath == "" || relativePath == "." || strings.HasPrefix(relativePath, "/") || strings.Contains(relativePath, "..") {
		return "", nil, &Error{Code: "music_fingerprint_input", Message: "The scanned file path is not a safe path below the media root."}
	}
	clean := path.Clean(relativePath)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", nil, &Error{Code: "music_fingerprint_input", Message: "The scanned file path is not a safe path below the media root."}
	}
	absolute := filepath.Join(s.mediaDir, filepath.FromSlash(clean))
	if !contained(s.mediaDir, absolute) {
		return "", nil, &Error{Code: "music_fingerprint_input", Message: "The scanned file is outside the media root."}
	}
	info, err := os.Lstat(absolute)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", nil, &Error{Code: "music_fingerprint_input", Message: "The audio file is missing or is not a regular file. Rescan it and try again."}
	}
	resolvedMedia, err := filepath.EvalSymlinks(s.mediaDir)
	if err != nil {
		return "", nil, &Error{Code: "music_fingerprint_input", Message: "The configured media root is unavailable."}
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil || !contained(resolvedMedia, resolved) {
		return "", nil, &Error{Code: "music_fingerprint_input", Message: "The audio file resolves outside the media root."}
	}
	return absolute, info, nil
}

// fingerprintScanFile reuses cached evidence only while the current size and
// mtime match, otherwise runs the fingerprinter and replaces the cache row.
func (s *Service) fingerprintScanFile(ctx context.Context, file ScanFile) (Fingerprint, error) {
	if s.fingerprinter == nil {
		return Fingerprint{}, &Error{Code: "music_fingerprint_unavailable", Message: "Chromaprint fingerprinting is not available in this deployment."}
	}
	absolute, info, err := s.safeMediaPath(file.RelativePath)
	if err != nil {
		return Fingerprint{}, err
	}
	size, modified := info.Size(), info.ModTime().Unix()
	if size != file.SizeBytes || modified != file.ModifiedAt {
		return Fingerprint{}, &Error{Code: "music_fingerprint_input", Message: "The file changed after the scan. Rescan it and try again."}
	}
	cached, found, err := s.store.CachedFingerprint(ctx, file.RelativePath)
	if err != nil {
		return Fingerprint{}, err
	}
	if found && cached.SizeBytes == size && cached.ModifiedAt == modified {
		return Fingerprint{DurationSeconds: cached.DurationSeconds, Value: cached.Value}, nil
	}
	fingerprint, err := s.fingerprinter.Fingerprint(ctx, absolute)
	if err != nil {
		return Fingerprint{}, err
	}
	if !validDurationSeconds(fingerprint.DurationSeconds) || !validFingerprintValue(fingerprint.Value) {
		return Fingerprint{}, &Error{Code: "music_fingerprint_failed", Message: "The fingerprinter returned invalid evidence for this file."}
	}
	after, err := os.Lstat(absolute)
	if err != nil || !os.SameFile(info, after) || after.Size() != size || after.ModTime().Unix() != modified {
		return Fingerprint{}, &Error{Code: "music_fingerprint_input", Message: "The audio file changed while it was fingerprinted. Rescan it and try again."}
	}
	if err := s.store.SaveFingerprint(ctx, file.RelativePath, size, modified, fingerprint); err != nil {
		return Fingerprint{}, err
	}
	return fingerprint, nil
}
