// Package backups owns private, bounded ZIP snapshots, scheduling and reviewed
// restore preparation. Media and download files are never read or modified.
package backups

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	"github.com/faaaaq/ReelScarey/internal/config"
	"github.com/faaaaq/ReelScarey/internal/maintenance"
)

const MaxArchive = int64(512 << 20)

var validID = regexp.MustCompile(`^[a-f0-9]{32}$`)

type Archive struct {
	ID       string               `json:"id"`
	Kind     string               `json:"kind"`
	Created  int64                `json:"created_at"`
	Order    int64                `json:"created_order"`
	Size     int64                `json:"size_bytes"`
	SHA      string               `json:"sha256"`
	Manifest maintenance.Manifest `json:"manifest"`
}

func id() string { return hex.EncodeToString(randomBytes()) }
func randomBytes() []byte {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b
}

func sameTree(a, b string) bool {
	r, e := filepath.Rel(a, b)
	return e == nil && (r == "." || filepath.IsLocal(r))
}

func storage(cfg config.Config) (string, error) {
	p := cfg.BackupsDir
	if p == "" {
		p = filepath.Join(filepath.Dir(cfg.ConfigDir), "backups")
	}
	p, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	// Resolve the parent before creating anything, and refuse links in the final
	// component. os.Root confines every subsequent archive operation.
	parent, err := filepath.EvalSymlinks(filepath.Dir(p))
	if err != nil {
		return "", err
	}
	p = filepath.Join(parent, filepath.Base(p))
	if info, e := os.Lstat(p); e == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
		return "", errors.New("backup storage must be a separate regular directory")
	} else if e != nil && !errors.Is(e, os.ErrNotExist) {
		return "", e
	}
	for _, other := range []string{cfg.ConfigDir, cfg.MediaDir, cfg.DownloadsDir, os.Getenv("REELSCAREY_YOUTUBE_STAGING_DIR")} {
		if other == "" {
			continue
		}
		other, err = filepath.Abs(other)
		if err != nil {
			return "", err
		}
		if resolved, e := filepath.EvalSymlinks(other); e == nil {
			other = resolved
		}
		if sameTree(p, other) || sameTree(other, p) {
			return "", errors.New("backup storage must be separate from configuration, media, downloads and staging")
		}
	}
	if err = os.MkdirAll(p, 0700); err != nil {
		return "", err
	}
	return p, nil
}

func regular(root *os.Root, name string, limit int64) (*os.File, error) {
	before, e := root.Lstat(name)
	if e != nil {
		return nil, e
	}
	if !before.Mode().IsRegular() || before.Size() > limit {
		return nil, errors.New("unsupported backup file")
	}
	f, e := root.Open(name)
	if e != nil {
		return nil, e
	}
	after, e := f.Stat()
	if e != nil || !os.SameFile(before, after) {
		f.Close()
		return nil, errors.New("backup changed during access")
	}
	return f, nil
}

func persist(root *os.Root, name string, v any) error {
	f, e := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	e = json.NewEncoder(f).Encode(v)
	if e == nil {
		e = f.Sync()
	}
	return errors.Join(e, f.Close())
}

// Readers must see a complete record or no record. Publishing the final name
// before encoding allowed a concurrent backup-list refresh to see partial JSON.
func publishRecord(root *os.Root, name string, v any) error {
	tmp := ".record-" + id()
	defer root.Remove(tmp)
	if e := persist(root, tmp, v); e != nil {
		return e
	}
	return root.Link(tmp, name)
}

func (s *Service) record(archiveID string) (Archive, error) {
	var a Archive
	if !validID.MatchString(archiveID) {
		return a, errors.New("invalid backup ID")
	}
	root, e := os.OpenRoot(s.dir)
	if e != nil {
		return a, e
	}
	defer root.Close()
	f, e := regular(root, archiveID+".json", 32768)
	if e != nil {
		return a, e
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, 32769))
	d.DisallowUnknownFields()
	if e = d.Decode(&a); e != nil {
		return a, e
	}
	if d.Decode(&struct{}{}) != io.EOF || a.ID != archiveID || a.Created <= 0 || a.Size <= 0 || a.Size > MaxArchive || len(a.SHA) != 64 {
		return a, errors.New("invalid backup record")
	}
	switch a.Kind {
	case "manual", "scheduled", "upload", "pre_restore":
	default:
		return a, errors.New("invalid backup type")
	}
	return a, nil
}

func (s *Service) List() ([]Archive, error) {
	root, e := os.OpenRoot(s.dir)
	if e != nil {
		return nil, e
	}
	defer root.Close()
	f, e := root.Open(".")
	if e != nil {
		return nil, e
	}
	defer f.Close()
	entries, e := f.ReadDir(10001)
	if e != nil && e != io.EOF {
		return nil, e
	}
	if len(entries) > 10000 {
		return nil, errors.New("backup storage contains too many entries")
	}
	out := []Archive{}
	for _, entry := range entries {
		name := entry.Name()
		if filepath.Ext(name) != ".json" {
			continue
		}
		archiveID := name[:len(name)-5]
		if !validID.MatchString(archiveID) {
			continue
		}
		a, e := s.record(archiveID)
		if e != nil {
			return nil, errors.New("a managed backup record is damaged; preserve the storage folder and inspect it")
		}
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Created == out[j].Created {
			if out[i].Order == out[j].Order {
				return out[i].ID > out[j].ID
			}
			return out[i].Order > out[j].Order
		}
		return out[i].Created > out[j].Created
	})
	return out, nil
}

func (s *Service) Open(archiveID string) (*os.File, Archive, error) {
	a, e := s.record(archiveID)
	if e != nil {
		return nil, a, e
	}
	root, e := os.OpenRoot(s.dir)
	if e != nil {
		return nil, a, e
	}
	defer root.Close()
	f, e := regular(root, archiveID+".zip", MaxArchive)
	if e != nil {
		return nil, a, e
	}
	info, e := f.Stat()
	if e != nil || info.Size() != a.Size {
		f.Close()
		return nil, a, errors.New("backup archive changed")
	}
	return f, a, nil
}

func unpack(ctx context.Context, f *os.File, dest string) (maintenance.Manifest, error) {
	var empty maintenance.Manifest
	info, e := f.Stat()
	if e != nil {
		return empty, e
	}
	if e = boundedDirectory(f, info.Size()); e != nil {
		return empty, e
	}
	z, e := zip.NewReader(f, info.Size())
	if e != nil {
		return empty, errors.New("upload must be a ReelScarey ZIP backup")
	}
	if len(z.File) < 2 || len(z.File) > 4 {
		return empty, errors.New("backup ZIP has unsupported entries")
	}
	root, e := os.OpenRoot(dest)
	if e != nil {
		return empty, e
	}
	defer root.Close()
	seen := map[string]bool{}
	var total uint64
	for _, entry := range z.File {
		limit := int64(1024)
		switch entry.Name {
		case "reelscarey.db":
			limit = MaxArchive
		case "backup-manifest.json":
			limit = 32768
		case "secrets.key", "setup-token":
		default:
			return empty, errors.New("backup ZIP contains an unexpected path")
		}
		if seen[entry.Name] || !entry.Mode().IsRegular() || entry.UncompressedSize64 == 0 || entry.UncompressedSize64 > uint64(limit) || entry.Flags&1 != 0 {
			return empty, errors.New("backup ZIP contains duplicate, linked, encrypted or oversized entries")
		}
		seen[entry.Name] = true
		total += entry.UncompressedSize64
		if total > uint64(MaxArchive)+34816 {
			return empty, errors.New("expanded backup exceeds its size limit")
		}
		if e = ctx.Err(); e != nil {
			return empty, e
		}
		in, e := entry.Open()
		if e != nil {
			return empty, e
		}
		out, e := root.OpenFile(entry.Name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			in.Close()
			return empty, e
		}
		n, ce := io.Copy(out, io.LimitReader(in, limit+1))
		e = errors.Join(ce, out.Sync(), out.Close(), in.Close())
		if e != nil || n != int64(entry.UncompressedSize64) {
			return empty, errors.New("backup ZIP entry failed size or checksum verification")
		}
	}
	return maintenance.Verify(ctx, dest)
}

// Bound the central directory before archive/zip allocates entry metadata. The
// interface format fits ordinary single-disk ZIP; ZIP64 is unnecessary below
// 512 MiB. This also rejects a forged small entry count hiding a huge directory.
func boundedDirectory(f *os.File, size int64) error {
	const maximum = int64(65535 + 22)
	n := size
	if n > maximum {
		n = maximum
	}
	if n < 22 {
		return errors.New("backup ZIP directory is missing")
	}
	tail := make([]byte, int(n))
	if _, e := f.ReadAt(tail, size-n); e != nil {
		return e
	}
	for i := len(tail) - 22; i >= 0; i-- {
		if !bytes.Equal(tail[i:i+4], []byte{'P', 'K', 5, 6}) {
			continue
		}
		end := tail[i:]
		if int(binary.LittleEndian.Uint16(end[20:22])) != len(end)-22 {
			continue
		}
		disk, centralDisk := binary.LittleEndian.Uint16(end[4:6]), binary.LittleEndian.Uint16(end[6:8])
		count, total := binary.LittleEndian.Uint16(end[8:10]), binary.LittleEndian.Uint16(end[10:12])
		span, offset := int64(binary.LittleEndian.Uint32(end[12:16])), int64(binary.LittleEndian.Uint32(end[16:20]))
		if disk != 0 || centralDisk != 0 || count != total || total < 2 || total > 4 || span > 65536 || offset+span != size-n+int64(i) {
			return errors.New("backup ZIP directory exceeds the supported bounds")
		}
		return nil
	}
	return errors.New("backup ZIP directory is invalid")
}

func (s *Service) inspect(ctx context.Context, archiveID string) (Archive, string, error) {
	f, a, e := s.Open(archiveID)
	if e != nil {
		return a, "", e
	}
	defer f.Close()
	h := sha256.New()
	if _, e = io.Copy(h, f); e != nil {
		return a, "", e
	}
	if hex.EncodeToString(h.Sum(nil)) != a.SHA {
		return a, "", errors.New("backup archive failed checksum verification")
	}
	dir, e := os.MkdirTemp(s.dir, ".inspect-")
	if e != nil {
		return a, "", e
	}
	m, e := unpack(ctx, f, dir)
	if e == nil && (m.Created != a.Manifest.Created || m.Schema != a.Manifest.Schema) {
		e = errors.New("backup metadata changed")
	}
	if e != nil {
		os.RemoveAll(dir)
		return a, "", e
	}
	return a, dir, nil
}

func (s *Service) publish(ctx context.Context, filename, kind string, m maintenance.Manifest) (Archive, error) {
	var a Archive
	root, e := os.OpenRoot(s.dir)
	if e != nil {
		return a, e
	}
	defer root.Close()
	f, e := regular(root, filepath.Base(filename), MaxArchive)
	if e != nil {
		return a, e
	}
	info, e := f.Stat()
	if e != nil {
		f.Close()
		return a, e
	}
	h := sha256.New()
	_, e = io.Copy(h, f)
	f.Close()
	if e != nil {
		return a, e
	}
	now := time.Now()
	a = Archive{ID: id(), Kind: kind, Created: now.Unix(), Order: now.UnixNano(), Size: info.Size(), SHA: hex.EncodeToString(h.Sum(nil)), Manifest: m}
	if e = ctx.Err(); e != nil {
		return a, e
	}
	// Link publication refuses an existing destination rather than replacing it.
	if e = root.Link(filepath.Base(filename), a.ID+".zip"); e != nil {
		return a, e
	}
	if e = publishRecord(root, a.ID+".json", a); e != nil {
		return a, e
	}
	if e = syncDir(root); e != nil {
		return a, e
	}
	return a, nil
}

func (s *Service) create(ctx context.Context, kind string) (Archive, error) {
	var a Archive
	stage, e := os.MkdirTemp(s.dir, ".snapshot-")
	if e != nil {
		return a, e
	}
	defer os.RemoveAll(stage)
	m, e := maintenance.BackupOwned(ctx, s.cfg, filepath.Join(stage, "snapshot"), s.version)
	if e != nil {
		return a, e
	}
	filename := filepath.Join(s.dir, ".archive-"+id())
	out, e := os.OpenFile(filename, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return a, e
	}
	defer os.Remove(filename)
	z := zip.NewWriter(out)
	names := []string{"backup-manifest.json"}
	for _, f := range m.Files {
		names = append(names, f.Name)
	}
	for _, name := range names {
		f, e := os.Open(filepath.Join(stage, "snapshot", name))
		if e != nil {
			z.Close()
			out.Close()
			return a, e
		}
		info, e := f.Stat()
		if e != nil || info.Size() > MaxArchive {
			f.Close()
			z.Close()
			out.Close()
			return a, errors.New("database exceeds the 512 MiB interface limit; use offline backup tools")
		}
		entry, e := z.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate})
		if e == nil {
			_, e = io.Copy(entry, f)
		}
		f.Close()
		if e != nil {
			z.Close()
			out.Close()
			return a, e
		}
	}
	e = errors.Join(z.Close(), out.Sync(), out.Close())
	if e != nil {
		return a, e
	}
	return s.publish(ctx, filename, kind, m)
}

func (s *Service) Upload(ctx context.Context, in io.Reader) (Archive, error) {
	var a Archive
	if !s.op.TryLock() {
		return a, ErrBusy
	}
	defer s.op.Unlock()
	f, e := os.CreateTemp(s.dir, ".upload-")
	if e != nil {
		return a, e
	}
	defer os.Remove(f.Name())
	defer f.Close()
	n, e := io.Copy(f, io.LimitReader(in, MaxArchive+1))
	if e != nil {
		return a, e
	}
	if n > MaxArchive {
		return a, errors.New("backup uploads are limited to 512 MiB")
	}
	if e = f.Sync(); e != nil {
		return a, e
	}
	dir, e := os.MkdirTemp(s.dir, ".inspect-")
	if e != nil {
		return a, e
	}
	defer os.RemoveAll(dir)
	m, e := unpack(ctx, f, dir)
	if e != nil {
		return a, e
	}
	return s.publish(ctx, f.Name(), "upload", m)
}

func (s *Service) delete(archiveID string) error {
	if _, e := s.record(archiveID); e != nil {
		return e
	}
	root, e := os.OpenRoot(s.dir)
	if e != nil {
		return e
	}
	defer root.Close()
	// Never recurse or follow a path from uploaded metadata.
	for _, name := range []string{archiveID + ".zip", archiveID + ".json"} {
		info, e := root.Lstat(name)
		if e != nil || !info.Mode().IsRegular() {
			return errors.New("backup files changed; deletion refused")
		}
	}
	if e = root.Remove(archiveID + ".zip"); e != nil {
		return e
	}
	if e = root.Remove(archiveID + ".json"); e != nil {
		return e
	}
	return syncDir(root)
}
func (s *Service) Delete(archiveID string) error {
	if !s.op.TryLock() {
		return ErrBusy
	}
	defer s.op.Unlock()
	return s.delete(archiveID)
}

func (s *Service) retain(ctx context.Context) error {
	state, e := s.Schedule(ctx)
	if e != nil {
		return e
	}
	list, e := s.List()
	if e != nil {
		return e
	}
	count := 0
	for _, a := range list {
		if a.Kind != "scheduled" {
			continue
		}
		count++
		if count <= 1 {
			continue
		}
		if count > state.KeepCount || a.Created < time.Now().Add(-time.Duration(state.KeepDays)*24*time.Hour).Unix() {
			if _, dir, e := s.inspect(ctx, a.ID); e != nil {
				return fmt.Errorf("retention preserved an unverifiable archive: %w", e)
			} else {
				os.RemoveAll(dir)
			}
			if e = s.delete(a.ID); e != nil {
				return e
			}
		}
	}
	return nil
}
