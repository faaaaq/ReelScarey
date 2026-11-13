// Package mediaanalysis records one bounded FFprobe inspection for a
// registered media file. It never changes probe_json or touches media bytes.
//
// A worker claims a row by moving analysis_state to "running" and finishes it
// with "ok" or "failed". Callers may set analysis_state to "pending" to ask
// for another inspection; rows left "running" by a crashed process become
// eligible again after a short age-out. The schedule is a durable loop, not an
// in-memory queue.
package mediaanalysis

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
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

	"github.com/faaaaq/ReelScarey/internal/organization"
)

const (
	// DefaultTimeout bounds a single FFprobe process.
	DefaultTimeout = 20 * time.Second
	// maxProbeOutput bounds the parsed JSON. Real probes are far smaller.
	maxProbeOutput = 256 << 10
	// maxStderr bounds retained diagnostic output. It is never persisted.
	maxStderr = 8 << 10
	// maxAnalysisJSON keeps analysis_json small and predictable.
	maxAnalysisJSON = 8 << 10
	// maxStreams bounds reported languages.
	maxStreams = 16
	// maxLanguageRunes bounds each language label.
	maxLanguageRunes = 32
	// runningAgeOut releases a row after a crashed process. It is much longer
	// than the probe timeout so a live worker is never displaced.
	runningAgeOut = 20 * time.Minute
)

var (
	// ErrConfig reports a worker without a database or media root.
	ErrConfig = errors.New("media analysis is not configured")
	// ErrMediaRoot reports an unavailable media folder. Rows stay queued so a
	// missing mount does not permanently fail the library.
	ErrMediaRoot = errors.New("the media folder is not available")
	// ErrMissingFile reports a registered file that is no longer present.
	ErrMissingFile = errors.New("the registered file is missing from the media folder")
	// ErrUnsafePath reports a path that could leave the media root or follow a
	// symbolic link or special file.
	ErrUnsafePath = errors.New("the registered path is not safe to analyze")
	// ErrOpenFile reports a file that exists but cannot be read.
	ErrOpenFile = errors.New("the registered file could not be opened")
	// ErrProbeMissing reports that FFprobe is unavailable.
	ErrProbeMissing = errors.New("FFprobe is not installed on the server")
	// ErrProbeTimeout reports that FFprobe did not finish in time.
	ErrProbeTimeout = errors.New("FFprobe timed out")
	// ErrProbeUnreadable reports a non-zero FFprobe exit.
	ErrProbeUnreadable = errors.New("FFprobe could not read this file")
	// ErrProbeInvalid reports malformed or truncated FFprobe output.
	ErrProbeInvalid = errors.New("FFprobe returned invalid media information")
	// ErrNoVideo reports a readable file with no usable video stream.
	ErrNoVideo = errors.New("the file has no usable video stream")
)

// Worker inspects registered media files below MediaRoot.
type Worker struct {
	DB        *sql.DB
	MediaRoot string
	// Binary is the FFprobe executable. Empty means "ffprobe" from PATH.
	Binary string
	// Timeout bounds one probe. Zero means DefaultTimeout.
	Timeout time.Duration
}

// Analysis is the bounded result stored in media_files.analysis_json. Fields
// are omitted when unknown so the stored document stays small.
type Analysis struct {
	DurationSeconds   float64  `json:"duration_seconds,omitempty"`
	SizeBytes         int64    `json:"size_bytes,omitempty"`
	Container         string   `json:"container,omitempty"`
	VideoCodec        string   `json:"video_codec,omitempty"`
	Width             int      `json:"width,omitempty"`
	Height            int      `json:"height,omitempty"`
	HDR               bool     `json:"hdr,omitempty"`
	AudioLanguages    []string `json:"audio_languages,omitempty"`
	SubtitleLanguages []string `json:"subtitle_languages,omitempty"`
}

type failure struct {
	Error string `json:"error"`
}

// ProcessOne claims at most one media file that needs analysis. It returns
// true when a row was claimed, including when the probe failed and was recorded
// as failed. Analysis failures are returned for logging after they are stored.
func (w *Worker) ProcessOne(ctx context.Context) (bool, error) {
	if w == nil || w.DB == nil || w.MediaRoot == "" {
		return false, ErrConfig
	}
	// Analysis reads media bytes and must not race an organization move.
	defer organization.ReadMedia()()
	id, relative, err := w.claim(ctx)
	if err != nil || id == 0 {
		return false, err
	}
	timeout := w.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	work, cancel := context.WithTimeout(ctx, timeout)
	result, probeErr := w.analyze(work, relative)
	timedOut := errors.Is(work.Err(), context.DeadlineExceeded)
	cancel()

	finish, done := context.WithTimeout(context.Background(), 5*time.Second)
	defer done()

	if ctx.Err() != nil || errors.Is(probeErr, ErrMediaRoot) {
		// Shutdown or an unavailable media folder is not a file failure. Leave
		// the row queued so a later process can inspect it.
		_, releaseErr := w.DB.ExecContext(finish, `UPDATE media_files SET analysis_state='pending',analysis_json='',analysis_at=0 WHERE id=? AND analysis_state='running'`, id)
		if releaseErr != nil {
			return true, releaseErr
		}
		if ctx.Err() != nil {
			return true, ctx.Err()
		}
		return true, probeErr
	}
	if probeErr != nil {
		if timedOut {
			probeErr = ErrProbeTimeout
		}
		raw, err := json.Marshal(failure{Error: boundedText(probeErr.Error(), 300)})
		if err != nil {
			return true, err
		}
		if _, err = w.DB.ExecContext(finish, `UPDATE media_files SET analysis_state='failed',analysis_json=?,analysis_at=unixepoch() WHERE id=? AND analysis_state='running'`, string(raw), id); err != nil {
			return true, err
		}
		return true, probeErr
	}
	raw, err := json.Marshal(result)
	if err != nil || len(raw) > maxAnalysisJSON {
		// The struct is bounded by construction; keep a defensible fallback.
		probeErr = ErrProbeInvalid
		raw, _ = json.Marshal(failure{Error: probeErr.Error()})
		if _, err = w.DB.ExecContext(finish, `UPDATE media_files SET analysis_state='failed',analysis_json=?,analysis_at=unixepoch() WHERE id=? AND analysis_state='running'`, string(raw), id); err != nil {
			return true, err
		}
		return true, probeErr
	}
	_, err = w.DB.ExecContext(finish, `UPDATE media_files SET analysis_state='ok',analysis_json=?,analysis_at=unixepoch() WHERE id=? AND analysis_state='running'`, string(raw), id)
	return true, err
}

// Run inspects files until ctx is cancelled. It never creates work: only rows
// already waiting for analysis are processed.
func (w *Worker) Run(ctx context.Context, logger *slog.Logger) {
	if logger == nil {
		logger = slog.Default()
	}
	for ctx.Err() == nil {
		worked, err := w.ProcessOne(ctx)
		if err != nil && ctx.Err() == nil && analysisInfrastructureError(err) {
			logger.Warn("media analysis deferred", "error", err)
		}
		delay := 30 * time.Second
		if worked {
			delay = 200 * time.Millisecond
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// analysisInfrastructureError distinguishes a missing mount or FFprobe from a
// single unreadable file. Per-file failures are recorded on the row and must
// not flood the log for a large library.
func analysisInfrastructureError(err error) bool {
	return errors.Is(err, ErrConfig) || errors.Is(err, ErrMediaRoot) || errors.Is(err, ErrProbeMissing)
}

// claim atomically moves one eligible row to running. The same condition is
// repeated on the outer update so a concurrent worker cannot steal a row that
// the subquery selected.
func (w *Worker) claim(ctx context.Context) (int64, string, error) {
	const eligible = `(analysis_state IN ('','pending') OR (analysis_state='running' AND analysis_at<?))`
	stale := time.Now().Add(-runningAgeOut).Unix()
	var id int64
	var relative string
	err := w.DB.QueryRowContext(ctx, `UPDATE media_files SET analysis_state='running',analysis_json='',analysis_at=unixepoch() WHERE id=(SELECT id FROM media_files WHERE relative_path<>'' AND `+eligible+` ORDER BY id LIMIT 1) AND `+eligible+` RETURNING id,relative_path`, stale, stale).Scan(&id, &relative)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", nil
	}
	return id, relative, err
}

func (w *Worker) analyze(ctx context.Context, relative string) (Analysis, error) {
	name, err := safeRelative(relative)
	if err != nil {
		return Analysis{}, err
	}
	root, err := os.OpenRoot(w.MediaRoot)
	if err != nil {
		return Analysis{}, ErrMediaRoot
	}
	defer root.Close()
	file, err := openAnchored(root, name)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Analysis{}, ErrMissingFile
		}
		return Analysis{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return Analysis{}, ErrOpenFile
	}
	raw, err := runFFprobe(ctx, w.Binary, file)
	if err != nil {
		return Analysis{}, err
	}
	result, err := decode(raw)
	if err != nil {
		return Analysis{}, err
	}
	result.SizeBytes = info.Size()
	return result, nil
}

// safeRelative accepts only registered slash-separated paths that stay below
// the media root.
func safeRelative(relative string) (string, error) {
	if relative == "" || strings.ContainsAny(relative, "\\:\x00\r\n") {
		return "", ErrUnsafePath
	}
	for _, r := range relative {
		if unicode.IsControl(r) {
			return "", ErrUnsafePath
		}
	}
	clean := path.Clean(relative)
	if clean != relative || clean == "." || !fs.ValidPath(clean) {
		return "", ErrUnsafePath
	}
	return filepath.FromSlash(clean), nil
}

// openAnchored rejects symbolic links and special files on every component
// before opening the final regular file through the root.
func openAnchored(root *os.Root, name string) (*os.File, error) {
	parts := strings.Split(name, string(filepath.Separator))
	for i := range parts {
		info, err := root.Lstat(filepath.Join(parts[:i+1]...))
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, ErrUnsafePath
		}
		last := i == len(parts)-1
		if last && !info.Mode().IsRegular() || !last && !info.IsDir() {
			return nil, ErrUnsafePath
		}
	}
	file, err := openFile(root, name)
	if err != nil {
		return nil, ErrOpenFile
	}
	visible, err := root.Lstat(name)
	if err != nil {
		file.Close()
		return nil, ErrOpenFile
	}
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(opened, visible) {
		file.Close()
		return nil, ErrUnsafePath
	}
	return file, nil
}

func runFFprobe(ctx context.Context, binary string, file *os.File) ([]byte, error) {
	if binary == "" {
		binary = "ffprobe"
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, ErrOpenFile
	}
	cmd := exec.CommandContext(ctx, binary, "-v", "error",
		"-protocol_whitelist", probeProtocols(),
		"-format_whitelist", "mov,matroska,webm,avi,mpegts",
		"-enable_drefs", "0", "-use_absolute_path", "0",
		"-probesize", "16777216", "-analyzeduration", "10000000",
		"-show_entries", "format=duration,format_name:stream=codec_type,codec_name,width,height,color_transfer:stream_tags=language",
		"-of", "json", "-i", probeInput())
	hideWindow(cmd)
	var stdout, stderr truncatingBuffer
	stdout.max, stderr.max = maxProbeOutput, maxStderr
	cmd.Stdin = file
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// A killed process must not leave Wait blocked on pipes inherited by its
	// children.
	cmd.WaitDelay = 2 * time.Second
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, ErrProbeTimeout
		}
		var missing *exec.Error
		if errors.As(err, &missing) || errors.Is(err, exec.ErrNotFound) || errors.Is(err, fs.ErrNotExist) {
			return nil, ErrProbeMissing
		}
		return nil, ErrProbeUnreadable
	}
	if stdout.dropped {
		return nil, ErrProbeInvalid
	}
	return stdout.Bytes(), nil
}

type rawProbe struct {
	Format struct {
		Duration   string `json:"duration"`
		FormatName string `json:"format_name"`
	} `json:"format"`
	Streams []struct {
		Type          string `json:"codec_type"`
		Codec         string `json:"codec_name"`
		Width         int    `json:"width"`
		Height        int    `json:"height"`
		ColorTransfer string `json:"color_transfer"`
		Tags          struct {
			Language string `json:"language"`
		} `json:"tags"`
	} `json:"streams"`
}

func decode(raw []byte) (Analysis, error) {
	var probe rawProbe
	if json.Unmarshal(raw, &probe) != nil {
		return Analysis{}, ErrProbeInvalid
	}
	out := Analysis{Container: boundedText(probe.Format.FormatName, 64)}
	if duration, err := parseFloat(probe.Format.Duration); err == nil && duration > 0 && duration <= 7*24*3600 {
		out.DurationSeconds = math.Round(duration*1000) / 1000
	}
	var video bool
	for _, stream := range probe.Streams {
		switch stream.Type {
		case "video":
			if video || stream.Codec == "" || stream.Codec == "unknown" || stream.Width <= 0 || stream.Height <= 0 || stream.Width > 32768 || stream.Height > 32768 {
				continue
			}
			video = true
			out.VideoCodec = boundedText(stream.Codec, 32)
			out.Width, out.Height = stream.Width, stream.Height
			out.HDR = stream.ColorTransfer == "smpte2084" || stream.ColorTransfer == "arib-std-b67"
		case "audio":
			appendLanguage(&out.AudioLanguages, stream.Tags.Language)
		case "subtitle":
			appendLanguage(&out.SubtitleLanguages, stream.Tags.Language)
		}
	}
	if !video {
		return Analysis{}, ErrNoVideo
	}
	return out, nil
}

func appendLanguage(list *[]string, raw string) {
	if len(*list) >= maxStreams {
		return
	}
	value := boundedText(raw, maxLanguageRunes)
	if value == "" {
		return
	}
	for _, existing := range *list {
		if existing == value {
			return
		}
	}
	*list = append(*list, value)
}

func parseFloat(raw string) (float64, error) {
	return strconv.ParseFloat(strings.TrimSpace(raw), 64)
}

func boundedText(value string, maxRunes int) string {
	value = strings.TrimSpace(value)
	if !utf8.ValidString(value) {
		value = strings.ToValidUTF8(value, "")
	}
	if utf8.RuneCountInString(value) <= maxRunes {
		return value
	}
	runes := []rune(value)
	return string(runes[:maxRunes])
}

type truncatingBuffer struct {
	bytes.Buffer
	max     int
	dropped bool
}

func (b *truncatingBuffer) Write(p []byte) (int, error) {
	original := len(p)
	if room := b.max - b.Len(); room > 0 {
		if len(p) > room {
			b.Buffer.Write(p[:room])
			b.dropped = true
		} else {
			b.Buffer.Write(p)
		}
	} else if len(p) > 0 {
		b.dropped = true
	}
	return original, nil
}
