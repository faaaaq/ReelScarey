package organization

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var internalStageRE = regexp.MustCompile(`^\.reelscarey-organize-[a-f0-9]{32}-[0-9]+\.part$`)
var internalFolderStageRE = regexp.MustCompile(`^\.reelscarey-folder-[a-f0-9]{32}$`)

func validManagedRelative(name string) bool {
	parts := strings.Split(name, "/")
	for i, part := range parts {
		if internalFolderStageRE.MatchString(part) || i == len(parts)-1 && internalStageRE.MatchString(part) {
			parts[i] = "safe-stage"
		}
	}
	return validRelative(strings.Join(parts, "/"))
}

func segments(root *os.Root, name string, missing bool) error {
	if !validManagedRelative(name) {
		return problem("unsafe_path", "A media path is not a safe relative path.")
	}
	parts := strings.Split(name, "/")
	for n := range parts {
		info, e := root.Lstat(filepath.FromSlash(strings.Join(parts[:n+1], "/")))
		if errors.Is(e, os.ErrNotExist) && missing {
			return nil
		}
		if e != nil {
			return e
		}
		if info.Mode()&os.ModeSymlink != 0 || n < len(parts)-1 && !info.IsDir() {
			return problem("unsafe_path", "Symbolic links and non-directory path components are not organized.")
		}
	}
	return nil
}
func (s *Service) openRoot(expected string) (*os.Root, string, error) {
	resolved, e := filepath.EvalSymlinks(s.cfg.MediaDir)
	if e != nil {
		return nil, "", problem("storage_unavailable", "The main media directory must exist and be accessible.")
	}
	resolved, e = filepath.Abs(resolved)
	if e != nil {
		return nil, "", e
	}
	if expected != "" && expected != resolved {
		return nil, "", problem("source_changed", "The main media mount changed since review.")
	}
	for _, other := range []string{s.cfg.ConfigDir, s.cfg.DownloadsDir} {
		v, e := filepath.EvalSymlinks(other)
		if e != nil {
			return nil, "", problem("storage_unavailable", "Config and downloads directories must exist before organization.")
		}
		v, e = filepath.Abs(v)
		if e != nil {
			return nil, "", e
		}
		for _, pair := range [][2]string{{v, resolved}, {resolved, v}} {
			rel, e := filepath.Rel(pair[0], pair[1])
			if e == nil && (rel == "." || filepath.IsLocal(rel)) {
				return nil, "", problem("unsafe_roots", "Config, downloads and main media directories must not overlap.")
			}
		}
	}
	root, e := os.OpenRoot(resolved)
	return root, resolved, e
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
func hash(ctx context.Context, f *os.File) (string, error) {
	if _, e := f.Seek(0, 0); e != nil {
		return "", e
	}
	h := sha256.New()
	if _, e := io.Copy(h, contextReader{ctx, f}); e != nil {
		return "", e
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func inspect(root *os.Root, name string) (Companion, error) {
	var out Companion
	if e := segments(root, name, false); e != nil {
		return out, e
	}
	before, e := root.Lstat(filepath.FromSlash(name))
	if e != nil {
		return out, e
	}
	if !before.Mode().IsRegular() {
		return out, problem("unsafe_path", "Only regular registered files and subtitle companions can be organized.")
	}
	f, e := root.Open(filepath.FromSlash(name))
	if e != nil {
		return out, e
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil {
		return out, e
	}
	if !os.SameFile(before, info) || !info.Mode().IsRegular() {
		return out, problem("source_changed", "A media file changed while it was opened.")
	}
	id, e := identity(f)
	if e != nil {
		return out, e
	}
	return Companion{Source: name, Size: info.Size(), Mtime: info.ModTime().UnixNano(), Identity: id}, nil
}
func checkSource(ctx context.Context, root *os.Root, v Companion, verifyHash bool) error {
	a, e := inspect(root, v.Source)
	if e != nil {
		return e
	}
	if a.Identity != v.Identity || a.Size != v.Size || a.Mtime != v.Mtime {
		return problem("source_changed", "A reviewed file changed. Its original path was left unchanged.")
	}
	if verifyHash {
		f, e := root.Open(filepath.FromSlash(v.Source))
		if e != nil {
			return e
		}
		defer f.Close()
		id, e := identity(f)
		if e != nil || id != v.Identity {
			return problem("source_changed", "A reviewed file was replaced.")
		}
		h, e := hash(ctx, f)
		if e != nil {
			return e
		}
		after, e := f.Stat()
		if e != nil {
			return e
		}
		if h != v.SHA || after.Size() != v.Size || after.ModTime().UnixNano() != v.Mtime {
			return problem("source_changed", "A reviewed file no longer matches its registered checksum.")
		}
	}
	return nil
}
func existsFolded(root *os.Root, name string) (bool, error) {
	dir := path.Dir(name)
	if dir != "." {
		if e := segments(root, dir, true); e != nil {
			return false, e
		}
	}
	f, e := root.Open(filepath.FromSlash(dir))
	if errors.Is(e, os.ErrNotExist) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	defer f.Close()
	names, e := f.Readdirnames(10001)
	if e != nil && !errors.Is(e, io.EOF) {
		return false, e
	}
	if len(names) > 10000 {
		return false, problem("too_many_companions", "A destination folder has too many entries for bounded organization.")
	}
	for _, n := range names {
		if strings.EqualFold(n, path.Base(name)) {
			return true, nil
		}
	}
	return false, nil
}
func companions(ctx context.Context, root *os.Root, source, destination string) ([]Companion, error) {
	return companionsIgnoring(ctx, root, source, destination, nil)
}
func companionsIgnoring(ctx context.Context, root *os.Root, source, destination string, owned map[string]string) ([]Companion, error) {
	out := []Companion{}
	dir := path.Dir(source)
	f, e := root.Open(filepath.FromSlash(dir))
	if e != nil {
		return nil, e
	}
	names, e := f.Readdirnames(10001)
	f.Close()
	if e != nil && !errors.Is(e, io.EOF) {
		return nil, e
	}
	if len(names) > 10000 {
		return nil, problem("too_many_companions", "The source folder has too many entries for bounded organization.")
	}
	sort.Strings(names)
	stem := strings.TrimSuffix(path.Base(source), path.Ext(source))
	targetStem := strings.TrimSuffix(path.Base(destination), path.Ext(destination))
	for _, name := range names {
		fullName := path.Join(dir, name)
		if saved := owned[fullName]; saved != "" {
			info, e := inspect(root, fullName)
			if e == nil && info.Identity == saved {
				continue
			}
		}
		if name == path.Base(source) || !strings.HasPrefix(strings.ToLower(name), strings.ToLower(stem)+".") {
			continue
		}
		switch strings.ToLower(path.Ext(name)) {
		case ".srt", ".vtt", ".ass", ".ssa":
		default:
			return nil, problem("unsupported_companion", "A matching artwork, metadata, attachment or other companion needs manual review before renaming this video.")
		}
		suffix := name[len(stem):]
		v, e := inspect(root, path.Join(dir, name))
		if e != nil {
			return nil, e
		}
		if v.Size < 1 || v.Size > 16<<20 {
			return nil, problem("unsupported_companion", "Subtitle companions must be regular files between 1 byte and 16 MiB.")
		}
		file, e := root.Open(filepath.FromSlash(v.Source))
		if e != nil {
			return nil, e
		}
		v.SHA, e = hash(ctx, file)
		file.Close()
		if e != nil {
			return nil, e
		}
		v.Destination = path.Join(path.Dir(destination), targetStem+suffix)
		if !validRelative(v.Destination) {
			return nil, problem("unsafe_path", "A companion suffix produces an unsafe destination.")
		}
		out = append(out, v)
		if len(out) > 50 {
			return nil, problem("too_many_companions", "A video has more than 50 subtitle companions.")
		}
	}
	return out, nil
}
