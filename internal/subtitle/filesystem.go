package subtitle

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/faaaaq/ReelScarey/internal/quality"
	"github.com/faaaaq/ReelScarey/internal/transfer"
)

type openedMedia struct {
	root, folder *os.Root
	file         *os.File
	info         os.FileInfo
	probe        transfer.Probe
	moviehash    string
}

func (o *openedMedia) Close() { o.file.Close(); o.folder.Close(); o.root.Close() }
func local(name string) bool {
	if !filepath.IsLocal(filepath.FromSlash(name)) || strings.ContainsAny(name, "\\:\x00\r\n") {
		return false
	}
	for _, p := range strings.Split(name, "/") {
		if p == "" || p == "." || p == ".." {
			return false
		}
	}
	return true
}
func segments(root *os.Root, name string) error {
	parts := strings.Split(filepath.ToSlash(name), "/")
	for i := range parts {
		info, e := root.Lstat(filepath.FromSlash(strings.Join(parts[:i+1], "/")))
		if e != nil {
			return problem("storage_unavailable", "The registered media file or folder is missing or inaccessible.")
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return problem("unsafe_path", "Subtitle paths cannot use symbolic links.")
		}
		if i < len(parts)-1 && !info.IsDir() {
			return problem("unsafe_path", "A subtitle path contains a non-directory component.")
		}
	}
	return nil
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(b []byte) (int, error) {
	if e := r.ctx.Err(); e != nil {
		return 0, e
	}
	return r.r.Read(b)
}
func digest(ctx context.Context, f *os.File) (string, error) {
	if _, e := f.Seek(0, io.SeekStart); e != nil {
		return "", e
	}
	h := sha256.New()
	if _, e := io.Copy(h, contextReader{ctx, f}); e != nil {
		return "", e
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func movieHash(f io.ReaderAt, size int64) (string, error) {
	if size < 131072 {
		return "", nil
	}
	sum := uint64(size)
	b := make([]byte, 65536)
	for _, offset := range []int64{0, size - 65536} {
		if _, e := f.ReadAt(b, offset); e != nil {
			return "", e
		}
		for i := 0; i < len(b); i += 8 {
			sum += binary.LittleEndian.Uint64(b[i : i+8])
		}
	}
	return fmt.Sprintf("%016x", sum), nil
}
func (s *Service) openMedia(ctx context.Context, m media) (*openedMedia, error) {
	if !local(m.Path) {
		return nil, problem("unsafe_path", "The registered media path is not a safe relative path.")
	}
	mediaRoot, e := filepath.EvalSymlinks(s.cfg.MediaDir)
	if e != nil {
		return nil, problem("storage_unavailable", "The media root must exist and be accessible.")
	}
	configRoot, e := filepath.EvalSymlinks(s.cfg.ConfigDir)
	if e != nil {
		return nil, e
	}
	for _, pair := range [][2]string{{mediaRoot, configRoot}, {configRoot, mediaRoot}} {
		rel, e := filepath.Rel(pair[0], pair[1])
		if e == nil && (rel == "." || filepath.IsLocal(rel)) {
			return nil, problem("unsafe_path", "Media and config folders must not overlap.")
		}
	}
	root, e := os.OpenRoot(s.cfg.MediaDir)
	if e != nil {
		return nil, e
	}
	failed := true
	var folder *os.Root
	var file *os.File
	defer func() {
		if failed {
			if file != nil {
				file.Close()
			}
			if folder != nil {
				folder.Close()
			}
			root.Close()
		}
	}()
	if e = segments(root, m.Path); e != nil {
		return nil, e
	}
	folder, e = root.OpenRoot(filepath.FromSlash(path.Dir(m.Path)))
	if e != nil {
		return nil, e
	}
	before, e := folder.Lstat(path.Base(m.Path))
	if e != nil || !before.Mode().IsRegular() {
		return nil, problem("media_changed", "The registered media is not a regular video file.")
	}
	file, e = folder.Open(path.Base(m.Path))
	if e != nil {
		return nil, e
	}
	info, e := file.Stat()
	if e != nil {
		return nil, e
	}
	if !os.SameFile(before, info) || !info.Mode().IsRegular() || info.Size() != m.Size {
		return nil, problem("media_changed", "The video no longer matches its registered size or identity.")
	}
	var cachedSHA, cachedHash, cachedProbe string
	var cachedSize, cachedMtime int64
	e = s.db.QueryRowContext(ctx, `SELECT sha256,size_bytes,mtime,moviehash,probe_json FROM subtitle_inspections WHERE media_file_id=?`, m.ID).Scan(&cachedSHA, &cachedSize, &cachedMtime, &cachedHash, &cachedProbe)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return nil, e
	}
	var probe transfer.Probe
	hash := ""
	if e == nil && cachedSHA == m.SHA && cachedSize == info.Size() && cachedMtime == info.ModTime().UnixNano() {
		if e = json.Unmarshal([]byte(cachedProbe), &probe); e != nil {
			return nil, e
		}
		hash = cachedHash
	} else {
		sha, e := digest(ctx, file)
		if e != nil {
			return nil, e
		}
		if sha != m.SHA {
			return nil, problem("media_changed", "The video content differs from its registered import. No subtitle was written.")
		}
		hash, e = movieHash(file, m.Size)
		if e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(m.Probe), &probe); e != nil || probe.Subtitles == nil {
			probe, e = s.Prober.Probe(ctx, file)
			if e != nil {
				return nil, problem("inspection_failed", "FFprobe could not inspect existing subtitle streams. Check the file and FFprobe installation.")
			}
		}
		b, _ := json.Marshal(probe)
		if _, e = s.db.ExecContext(ctx, `INSERT INTO subtitle_inspections(media_file_id,sha256,size_bytes,mtime,moviehash,probe_json) VALUES(?,?,?,?,?,?) ON CONFLICT(media_file_id) DO UPDATE SET sha256=excluded.sha256,size_bytes=excluded.size_bytes,mtime=excluded.mtime,moviehash=excluded.moviehash,probe_json=excluded.probe_json`, m.ID, m.SHA, m.Size, info.ModTime().UnixNano(), hash, string(b)); e != nil {
			return nil, e
		}
	}
	after, e := file.Stat()
	if e != nil || after.Size() != info.Size() || after.ModTime() != info.ModTime() {
		return nil, problem("media_changed", "The video changed during subtitle inspection.")
	}
	failed = false
	return &openedMedia{root: root, folder: folder, file: file, info: info, probe: probe, moviehash: hash}, nil
}
func (o *openedMedia) unchanged(m media) error {
	info, e := o.file.Stat()
	if e != nil || info.Size() != o.info.Size() || info.ModTime() != o.info.ModTime() {
		return problem("media_changed", "The video changed while subtitles were being prepared.")
	}
	if e = segments(o.root, m.Path); e != nil {
		return e
	}
	visible, e := o.root.Stat(filepath.FromSlash(m.Path))
	if e != nil || !os.SameFile(visible, o.info) {
		return problem("media_changed", "The registered video or its folder moved during subtitle work.")
	}
	return nil
}

var cueLine = regexp.MustCompile(`^((?:[0-9]{1,2}:)?[0-9]{2}:[0-9]{2}[,.][0-9]{3})[ \t]+-->[ \t]+((?:[0-9]{1,2}:)?[0-9]{2}:[0-9]{2}[,.][0-9]{3})(?:[ \t].*)?$`)

func timestamp(value string) (float64, bool) {
	parts := strings.Split(strings.ReplaceAll(value, ",", "."), ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, false
	}
	last, e := strconv.ParseFloat(parts[len(parts)-1], 64)
	if e != nil || last >= 60 {
		return 0, false
	}
	minutes, e := strconv.Atoi(parts[len(parts)-2])
	if e != nil || minutes >= 60 {
		return 0, false
	}
	hours := 0
	if len(parts) == 3 {
		hours, e = strconv.Atoi(parts[0])
		if e != nil || hours > 99 {
			return 0, false
		}
	}
	return float64(hours*3600+minutes*60) + last, true
}
func normalizeSRT(body []byte, duration float64) ([]byte, error) {
	if len(body) == 0 || len(body) > MaxSubtitle || !utf8.Valid(body) || bytes.ContainsRune(body, 0) {
		return nil, problem("invalid_subtitle", "The subtitle is not supported UTF-8 text or exceeds 4 MiB.")
	}
	body = bytes.TrimPrefix(body, []byte{0xef, 0xbb, 0xbf})
	start := strings.ToLower(strings.TrimSpace(string(body[:min(len(body), 100)])))
	if strings.HasPrefix(start, "<html") || strings.HasPrefix(start, "<!doctype") || strings.HasPrefix(start, "webvtt") {
		return nil, problem("invalid_subtitle", "The provider did not return an SRT subtitle.")
	}
	body = bytes.ReplaceAll(body, []byte("\r\n"), []byte("\n"))
	if bytes.ContainsRune(body, '\r') {
		return nil, problem("invalid_subtitle", "The subtitle contains unsupported line endings.")
	}
	found := 0
	lines := bytes.Split(body, []byte("\n"))
	for i, line := range lines {
		if len(line) > 16384 {
			return nil, problem("invalid_subtitle", "A subtitle line exceeds the supported size.")
		}
		if !bytes.Contains(line, []byte("-->")) {
			continue
		}
		m := cueLine.FindStringSubmatch(string(line))
		if m == nil {
			return nil, problem("invalid_subtitle", "The subtitle contains an invalid cue timestamp.")
		}
		a, okA := timestamp(m[1])
		b, okB := timestamp(m[2])
		if !okA || !okB || b <= a || (duration > 0 && b > duration+180) || i+1 >= len(lines) || len(bytes.TrimSpace(lines[i+1])) == 0 {
			return nil, problem("invalid_subtitle", "Subtitle timing or cue content does not fit this video.")
		}
		found++
	}
	if found == 0 {
		return nil, problem("invalid_subtitle", "The provider did not return a timed SRT subtitle.")
	}
	return body, nil
}
func sidecar(name, stem string) (Track, bool, bool) {
	ext := strings.ToLower(path.Ext(name))
	if !slices.Contains([]string{".srt", ".vtt", ".ass", ".ssa"}, ext) {
		return Track{}, false, false
	}
	base := strings.TrimSuffix(name, path.Ext(name))
	if strings.EqualFold(base, stem) {
		return Track{Language: "und", Variant: "full"}, false, true
	}
	prefix := stem + "."
	if len(base) <= len(prefix) || !strings.EqualFold(base[:len(prefix)], prefix) {
		return Track{}, false, false
	}
	parts := strings.Split(strings.ToLower(base[len(prefix):]), ".")
	lang := language(parts[0])
	if lang == "" {
		return Track{}, false, false
	}
	variant := "full"
	hi := false
	for _, part := range parts[1:] {
		switch part {
		case "signs", "signs-songs", "songs":
			variant = "signs"
		case "forced", "foreign":
			if variant != "signs" {
				variant = "forced"
			}
		case "sdh", "hi", "cc":
			hi = true
		default:
			return Track{}, false, false
		}
	}
	return Track{Language: lang, Variant: variant}, hi, true
}

type detection struct {
	tracks  map[Track]SavedTrack
	unknown bool
}

func detectExisting(ctx context.Context, m media, o *openedMedia, p Preferences) (detection, error) {
	d := detection{tracks: map[Track]SavedTrack{}}
	stem := strings.TrimSuffix(path.Base(m.Path), path.Ext(m.Path))
	for _, stream := range o.probe.Subtitles {
		lang := language(stream.Language)
		if lang == "" {
			continue
		}
		variant := "full"
		if strings.Contains(" "+quality.Normalize(stream.Title)+" ", " signs ") {
			variant = "signs"
		} else if stream.Forced {
			variant = "forced"
		}
		if stream.HearingImpaired && p.HearingImpaired == "exclude" {
			continue
		}
		t := Track{Language: lang, Variant: variant}
		d.tracks[t] = SavedTrack{Track: t, Source: "embedded", State: "available", Message: "Existing embedded subtitle stream."}
	}
	dir, e := o.folder.Open(".")
	if e != nil {
		return d, e
	}
	entries, e := dir.ReadDir(2001)
	dir.Close()
	if e != nil && e != io.EOF {
		return d, e
	}
	if len(entries) > 2000 {
		return d, problem("too_many_files", "This media folder has more than 2000 entries. Subtitle checks need a smaller folder.")
	}
	for _, entry := range entries {
		if e = ctx.Err(); e != nil {
			return d, e
		}
		t, hi, ok := sidecar(entry.Name(), stem)
		if !ok {
			continue
		}
		if hi && p.HearingImpaired == "exclude" {
			continue
		}
		before, e := o.folder.Lstat(entry.Name())
		if e != nil {
			return d, e
		}
		if !before.Mode().IsRegular() {
			return d, problem("unsafe_path", "A matching subtitle sidecar is not a regular file. It was left unchanged.")
		}
		file, e := o.folder.Open(entry.Name())
		if e != nil {
			return d, e
		}
		info, e := file.Stat()
		if e != nil || !os.SameFile(before, info) {
			file.Close()
			return d, problem("unsafe_path", "A subtitle sidecar changed during inspection.")
		}
		b, e := io.ReadAll(io.LimitReader(file, MaxSubtitle+1))
		file.Close()
		if e != nil {
			return d, e
		}
		state, message := "available", "Existing subtitle sidecar; no download needed."
		ext := strings.ToLower(path.Ext(entry.Name()))
		valid := false
		if ext == ".srt" || ext == ".vtt" {
			check := b
			if ext == ".vtt" {
				check = bytes.TrimPrefix(bytes.TrimSpace(b), []byte("WEBVTT"))
			}
			_, err := normalizeSRT(check, 0)
			valid = err == nil
			if ext == ".vtt" {
				valid = valid && bytes.HasPrefix(bytes.TrimSpace(b), []byte("WEBVTT"))
			}
		} else {
			valid = len(b) <= MaxSubtitle && utf8.Valid(b) && bytes.Contains(b, []byte("[Events]")) && bytes.Contains(b, []byte("Dialogue:"))
		}
		if !valid {
			state = "unverified"
			message = "An existing sidecar could not be verified. It will not be replaced."
		}
		if t.Language == "und" {
			state = "unverified"
			message = "An existing sidecar has no language label. Review it before automatic downloads."
			d.unknown = true
		}
		if old, ok := d.tracks[t]; ok && old.State == "available" {
			continue
		}
		d.tracks[t] = SavedTrack{Track: t, Source: "existing", Path: path.Join(path.Dir(m.Path), entry.Name()), State: state, Message: message}
	}
	return d, nil
}
