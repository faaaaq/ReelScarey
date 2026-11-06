// Package youtube provides bounded public YouTube source inspection.
package youtube

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var (
	ErrInvalidURL = errors.New("choose a valid public YouTube video, playlist, or channel URL")
	ErrInspection = errors.New("YouTube source inspection failed")
	videoID       = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)
	playlistID    = regexp.MustCompile(`^[A-Za-z0-9_-]{10,128}$`)
	channelID     = regexp.MustCompile(`^UC[A-Za-z0-9_-]{22}$`)
	handle        = regexp.MustCompile(`^@[A-Za-z0-9_.-]{3,100}$`)
)

type Source struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
	URL  string `json:"url"`
}
type Entry struct {
	Kind            string `json:"kind"`
	ID              string `json:"id"`
	URL             string `json:"url"`
	Title           string `json:"title"`
	Channel         string `json:"channel"`
	DurationSeconds int64  `json:"duration_seconds"`
	Published       string `json:"published"`
	Thumbnail       string `json:"thumbnail"`
}
type Inspection struct {
	Source    Source  `json:"source"`
	Title     string  `json:"title"`
	Channel   string  `json:"channel"`
	Entries   []Entry `json:"entries"`
	Truncated bool    `json:"truncated"`
}

// NormalizeURL strips incidental tracking parameters and rejects all hosts,
// paths, schemes, credentials and ports outside the explicit public forms.
func NormalizeURL(raw string) (Source, error) {
	if len(raw) > 2048 || !utf8.ValidString(raw) {
		return Source{}, ErrInvalidURL
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || u.Opaque != "" {
		return Source{}, ErrInvalidURL
	}
	host := strings.ToLower(u.Hostname())
	if host != "youtube.com" && host != "www.youtube.com" && host != "m.youtube.com" && host != "music.youtube.com" && host != "youtu.be" {
		return Source{}, ErrInvalidURL
	}
	var kind, id string
	path := strings.TrimSuffix(u.EscapedPath(), "/")
	if host == "youtu.be" {
		id = strings.TrimPrefix(path, "/")
		if path != "/"+id || !videoID.MatchString(id) {
			return Source{}, ErrInvalidURL
		}
		kind = "video"
	} else if path == "/watch" {
		values := u.Query()["v"]
		if len(values) != 1 || !videoID.MatchString(values[0]) {
			return Source{}, ErrInvalidURL
		}
		kind, id = "video", values[0]
	} else if path == "/playlist" {
		values := u.Query()["list"]
		if len(values) != 1 || !playlistID.MatchString(values[0]) {
			return Source{}, ErrInvalidURL
		}
		kind, id = "playlist", values[0]
	} else if strings.HasPrefix(path, "/channel/") && channelID.MatchString(strings.TrimPrefix(path, "/channel/")) {
		kind, id = "channel", strings.TrimPrefix(path, "/channel/")
	} else if strings.HasPrefix(path, "/@") && handle.MatchString(strings.TrimPrefix(path, "/")) {
		kind, id = "channel", strings.TrimPrefix(path, "/")
	} else {
		return Source{}, ErrInvalidURL
	}
	return source(kind, id), nil
}

func source(kind, id string) Source {
	base := "https://www.youtube.com/"
	switch kind {
	case "video":
		base += "watch?v=" + id
	case "playlist":
		base += "playlist?list=" + id
	case "channel":
		if strings.HasPrefix(id, "@") {
			base += id
		} else {
			base += "channel/" + id
		}
	}
	return Source{Kind: kind, ID: id, URL: base}
}

type Inspector struct {
	executable string
	command    func(context.Context, string, ...string) *exec.Cmd
}

// NewInspector resolves a locally installed executable, never downloads one.
func NewInspector(executable string) (*Inspector, error) {
	if strings.TrimSpace(executable) == "" {
		return nil, errors.New("configure the yt-dlp executable")
	}
	path, err := exec.LookPath(executable)
	if err != nil {
		return nil, fmt.Errorf("yt-dlp executable unavailable: %w", err)
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	return &Inspector{executable: path, command: exec.CommandContext}, nil
}

const maxInspectionBytes = 4 << 20
const maxEntries = 100

type boundedBuffer struct {
	bytes.Buffer
	max      int
	exceeded bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if len(p) > b.max-b.Len() {
		b.exceeded = true
		return 0, errors.New("output limit exceeded")
	}
	return b.Buffer.Write(p)
}

type rawThumb struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type rawEntry struct {
	ID         string            `json:"id"`
	Title      string            `json:"title"`
	Channel    string            `json:"channel"`
	Uploader   string            `json:"uploader"`
	Duration   float64           `json:"duration"`
	UploadDate string            `json:"upload_date"`
	Type       string            `json:"_type"`
	IEKey      string            `json:"ie_key"`
	Thumbnails []rawThumb        `json:"thumbnails"`
	Entries    []json.RawMessage `json:"entries"`
}

// Inspect runs a bounded, read-only metadata request. No browser cookies,
// configuration files, shell commands or caller-provided flags are accepted.
func (i *Inspector) Inspect(ctx context.Context, input string) (Inspection, error) {
	s, err := NormalizeURL(input)
	if err != nil {
		return Inspection{}, err
	}
	if i == nil || i.command == nil {
		return Inspection{}, ErrInspection
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	args := []string{"--ignore-config", "--no-cache-dir", "--no-cookies", "--no-cookies-from-browser", "--no-warnings", "--no-progress", "--skip-download", "--flat-playlist", "--dump-single-json", "--playlist-end", "101"}
	if s.Kind == "video" {
		args = append(args, "--no-playlist")
	}
	queryURL := s.URL
	if s.Kind == "channel" {
		// A channel's top-level tabs are not playlists; select its actual videos.
		queryURL += "/videos"
	}
	args = append(args, "--", queryURL)
	cmd := i.command(ctx, i.executable, args...)
	cmd.Env = append(os.Environ(), "YTDLP_NO_PLUGINS=1")
	stdout := &boundedBuffer{max: maxInspectionBytes}
	stderr := &boundedBuffer{max: 64 << 10}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err = cmd.Run(); err != nil || stdout.exceeded || stderr.exceeded || ctx.Err() != nil {
		return Inspection{}, ErrInspection
	}
	decoder := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	var data rawEntry
	if err = decoder.Decode(&data); err != nil {
		return Inspection{}, ErrInspection
	}
	var trailing any
	if err = decoder.Decode(&trailing); err != io.EOF {
		return Inspection{}, ErrInspection
	}
	out := Inspection{Source: s, Title: cleanText(data.Title, 300), Channel: cleanText(first(data.Channel, data.Uploader), 200), Entries: []Entry{}}
	if s.Kind == "video" {
		if data.ID != s.ID {
			return Inspection{}, ErrInspection
		}
		out.Entries = append(out.Entries, entry(data, "video"))
		return out, nil
	}
	if s.Kind == "playlist" && data.ID != s.ID {
		return Inspection{}, ErrInspection
	}
	if len(data.Entries) > maxEntries {
		out.Truncated = true
		data.Entries = data.Entries[:maxEntries]
	}
	for _, item := range data.Entries {
		var e rawEntry
		if err := json.Unmarshal(item, &e); err != nil {
			continue
		}
		kind := "video"
		if e.Type == "playlist" || strings.Contains(strings.ToLower(e.IEKey), "playlist") {
			kind = "playlist"
		}
		if kind == "video" && !videoID.MatchString(e.ID) || kind == "playlist" && !playlistID.MatchString(e.ID) {
			continue
		}
		out.Entries = append(out.Entries, entry(e, kind))
	}
	return out, nil
}

func entry(e rawEntry, kind string) Entry {
	var duration int64
	if e.Duration > 0 && e.Duration < 1209600 {
		duration = int64(e.Duration)
	}
	published := ""
	if d, err := time.Parse("20060102", e.UploadDate); err == nil {
		published = d.Format("2006-01-02")
	}
	s := source(kind, e.ID)
	return Entry{Kind: kind, ID: e.ID, URL: s.URL, Title: cleanText(e.Title, 300), Channel: cleanText(first(e.Channel, e.Uploader), 200), DurationSeconds: duration, Published: published, Thumbnail: thumbnail(e)}
}

// thumbnail accepts only well-known YouTube image hosts and falls back to the
// fixed video thumbnail path, so an inspection cannot inject an arbitrary URL.
func thumbnail(e rawEntry) string {
	best, area := "", -1
	for _, t := range e.Thumbnails {
		u, err := url.Parse(t.URL)
		if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" {
			continue
		}
		host := strings.ToLower(u.Hostname())
		if host != "i.ytimg.com" && host != "img.youtube.com" && host != "yt3.ggpht.com" && !strings.HasSuffix(host, ".ytimg.com") {
			continue
		}
		if t.Width*t.Height > area {
			best, area = t.URL, t.Width*t.Height
		}
	}
	if best == "" && videoID.MatchString(e.ID) {
		best = "https://i.ytimg.com/vi/" + e.ID + "/mqdefault.jpg"
	}
	return best
}
func first(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
func cleanText(s string, limit int) string {
	s = strings.ToValidUTF8(s, "")
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) > limit {
		s = string(r[:limit])
	}
	return s
}
