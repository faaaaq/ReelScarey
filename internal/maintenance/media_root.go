package maintenance

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/faaaaq/ReelScarey/internal/config"
	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/filecheck"
	"github.com/faaaaq/ReelScarey/internal/instance"
	"github.com/faaaaq/ReelScarey/internal/organization"
	"github.com/faaaaq/ReelScarey/internal/storage"
)

const rootPlanName = "media-root-migration.json"
const rootCopyMarker = ".media-root-copy-in-progress"
const maxRootPlan = 64 << 20

var ErrRootMigration = errors.New("Media-root migration changed or is unsafe. Stop the app, keep both copies and review the saved plan before retrying.")

type RootEntry struct {
	Path      string `json:"path"`
	Directory bool   `json:"directory"`
	Size      int64  `json:"size_bytes"`
	Modified  int64  `json:"modified_ns"`
	Mode      uint32 `json:"mode"`
	SHA       string `json:"sha256"`
	Identity  string `json:"identity"`
}
type RootMigration struct {
	ID             string      `json:"id"`
	State          string      `json:"state"`
	Digest         string      `json:"digest"`
	Source         string      `json:"source"`
	Destination    string      `json:"destination"`
	SourceIdentity string      `json:"source_identity"`
	ParentIdentity string      `json:"parent_identity"`
	RootMode       uint32      `json:"root_mode"`
	Bytes          int64       `json:"bytes"`
	Created        int64       `json:"created_at"`
	Backup         string      `json:"backup"`
	Entries        []RootEntry `json:"entries"`
}
type RootMigrationStatus struct {
	State       string `json:"state"`
	Digest      string `json:"digest"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Files       int    `json:"files"`
	Directories int    `json:"directories"`
	Bytes       int64  `json:"bytes"`
	Backup      string `json:"backup"`
	Scratch     string `json:"temporary_copy_folder"`
}

func (p RootMigration) Status() RootMigrationStatus {
	s := RootMigrationStatus{State: p.State, Digest: p.Digest, Source: p.Source, Destination: p.Destination, Bytes: p.Bytes, Backup: p.Backup, Scratch: filepath.Join(filepath.Dir(p.Destination), ".reelscarey-root-work-"+p.ID)}
	for _, v := range p.Entries {
		if v.Directory {
			s.Directories++
		} else {
			s.Files++
		}
	}
	return s
}
func randomRootID() string {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b[:])
}
func rootDigest(p RootMigration) string {
	p.Digest = ""
	p.State = ""
	p.Backup = ""
	b, _ := json.Marshal(p)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func rootIdentity(root *os.Root) (string, error) {
	f, e := root.Open(".")
	if e != nil {
		return "", e
	}
	defer f.Close()
	return organization.FileIdentity(f)
}
func migrationChild(parent *os.Root, name string) (*os.Root, error) {
	before, e := parent.Lstat(name)
	if e != nil || !before.IsDir() {
		return nil, ErrRootMigration
	}
	child, e := parent.OpenRoot(name)
	if e != nil {
		return nil, e
	}
	actual, e := child.Stat(".")
	visible, ve := parent.Lstat(name)
	if e != nil || ve != nil || !os.SameFile(before, actual) || !os.SameFile(before, visible) || !visible.IsDir() {
		child.Close()
		return nil, ErrRootMigration
	}
	return child, nil
}
func rootDir(dir string) (*os.Root, error) {
	absolute, e := filepath.Abs(dir)
	if e != nil {
		return nil, e
	}
	resolved, e := filepath.EvalSymlinks(absolute)
	if e != nil || resolved != absolute || filepath.Dir(absolute) == absolute {
		return nil, ErrRootMigration
	}
	return os.OpenRoot(absolute)
}
func rootPlanRead(dir string) (RootMigration, error) {
	var p RootMigration
	root, e := os.OpenRoot(dir)
	if e != nil {
		return p, e
	}
	defer root.Close()
	f, e := storage.Regular(root, rootPlanName, maxRootPlan)
	if e != nil {
		return p, e
	}
	defer f.Close()
	decoder := json.NewDecoder(io.LimitReader(f, maxRootPlan+1))
	decoder.DisallowUnknownFields()
	if e = decoder.Decode(&p); e != nil {
		return p, e
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF || len(p.ID) != 32 || len(p.Entries) > 100000 || rootDigest(p) != p.Digest {
		return p, ErrRootMigration
	}
	if _, e := hex.DecodeString(p.ID); e != nil {
		return p, ErrRootMigration
	}
	if !filepath.IsAbs(p.Source) || !filepath.IsAbs(p.Destination) {
		return p, ErrRootMigration
	}
	for _, entry := range p.Entries {
		if !fs.ValidPath(entry.Path) || entry.Path == "." || strings.Contains(entry.Path, "\\") || entry.Size < 0 || entry.Mode > 0777 {
			return p, ErrRootMigration
		}
	}
	return p, nil
}
func RootMigrationInfo(dir string) (RootMigrationStatus, error) {
	p, e := rootPlanRead(dir)
	if errors.Is(e, os.ErrNotExist) {
		return RootMigrationStatus{State: "none"}, nil
	}
	return p.Status(), e
}
func saveRootPlan(cfg config.Config, p RootMigration) error {
	encoded, e := json.Marshal(p)
	if e != nil || len(encoded) > maxRootPlan {
		return errors.New("Storage migration review exceeds the 64 MiB manifest limit.")
	}
	root, e := os.OpenRoot(cfg.ConfigDir)
	if e != nil {
		return e
	}
	defer root.Close()
	name := ".root-plan-" + randomRootID()
	if e = storage.WriteJSON(root, name, p); e != nil {
		return e
	}
	if info, e := root.Lstat(rootPlanName); e == nil && !info.Mode().IsRegular() {
		return ErrRootMigration
	} else if e != nil && !errors.Is(e, os.ErrNotExist) {
		return e
	}
	if e = root.Rename(name, rootPlanName); e != nil {
		return e
	}
	return storage.SyncDir(root)
}
func lockRootMigration(cfg config.Config, continuing bool) (func(), error) {
	lock, e := instance.Acquire(cfg.ConfigDir)
	if e != nil {
		return nil, e
	}
	guard, e := instance.Guard(cfg.ConfigDir, false)
	if e != nil {
		lock.Close()
		return nil, e
	}
	close := func() { guard.Close(); lock.Close() }
	if continuing {
		e = instance.CheckMaintenanceStart(cfg.ConfigDir)
	} else {
		e = instance.CheckStart(cfg.ConfigDir)
	}
	if e != nil {
		close()
		return nil, e
	}
	return close, nil
}
func idleRootMigration(ctx context.Context, cfg config.Config) error {
	db, e := database.OpenCurrent(ctx, filepath.Join(cfg.ConfigDir, "reelscarey.db"))
	if e != nil {
		return e
	}
	defer db.Close()
	if _, e = database.Inspect(ctx, db); e != nil {
		return e
	}
	if e = organization.EnsureMediaAvailable(ctx, db); e != nil {
		return e
	}
	for _, query := range []string{`SELECT COUNT(*) FROM import_operations WHERE phase<>'committed'`, `SELECT COUNT(*) FROM subtitle_imports WHERE phase<>'committed'`, `SELECT COUNT(*) FROM youtube_imports WHERE phase<>'committed'`, `SELECT COUNT(*) FROM library_scans WHERE state IN ('queued','scanning')`, `SELECT COUNT(*) FROM library_scan_files WHERE state IN ('queued','verifying')`} {
		var n int
		if e = db.QueryRowContext(ctx, query).Scan(&n); e != nil {
			return e
		}
		if n > 0 {
			return errors.New("Finish or recover pending imports and library scans before migrating storage.")
		}
	}
	return nil
}
func rootMeasure(ctx context.Context, root *os.Root, name string) (RootEntry, error) {
	var v RootEntry
	f, e := storage.Open(root, name)
	if e != nil {
		return v, e
	}
	defer f.Close()
	before, e := f.Stat()
	if e != nil || !before.Mode().IsRegular() {
		return v, ErrRootMigration
	}
	id, e := organization.FileIdentity(f)
	if e != nil {
		return v, e
	}
	h := sha256.New()
	n, e := io.Copy(h, &contextReader{ctx, f})
	if e != nil {
		return v, e
	}
	after, e := f.Stat()
	visible, ve := root.Lstat(filepath.FromSlash(name))
	if e != nil || ve != nil || !os.SameFile(before, visible) || before.Size() != n || before.Size() != after.Size() || before.ModTime() != after.ModTime() {
		return v, ErrRootMigration
	}
	return RootEntry{Path: name, Size: n, Modified: before.ModTime().UnixNano(), Mode: uint32(before.Mode().Perm()), SHA: hex.EncodeToString(h.Sum(nil)), Identity: id}, nil
}
func rootInventory(ctx context.Context, root *os.Root, skip string) ([]RootEntry, error) {
	out := []RootEntry{}
	dirs := []string{"."}
	seen := map[string]bool{}
	manifestBytes := 0
	for len(dirs) > 0 {
		name := dirs[0]
		dirs = dirs[1:]
		f, e := storage.Open(root, name)
		if e != nil {
			return nil, e
		}
		for {
			if e = ctx.Err(); e != nil {
				f.Close()
				return nil, e
			}
			entries, re := f.ReadDir(128)
			for _, entry := range entries {
				relative := path.Join(name, entry.Name())
				if relative == skip {
					continue
				}
				if !fs.ValidPath(relative) || strings.ContainsAny(relative, "\\\x00") || strings.IndexFunc(relative, func(r rune) bool { return r < 32 }) >= 0 || len(relative) > 2048 {
					f.Close()
					return nil, ErrRootMigration
				}
				folded := strings.ToLower(relative)
				if seen[folded] {
					f.Close()
					return nil, errors.New("Case-colliding paths must be resolved before migrating storage.")
				}
				seen[folded] = true
				manifestBytes += len(relative) + 256
				if manifestBytes > 32<<20 {
					f.Close()
					return nil, errors.New("Storage inventory exceeds the bounded manifest size; use a separately verified offline migration for this installation.")
				}
				if len(seen) > 100000 {
					f.Close()
					return nil, errors.New("Storage migration is limited to 100000 entries per review.")
				}
				info, e := root.Lstat(filepath.FromSlash(relative))
				if e != nil {
					f.Close()
					return nil, e
				}
				if info.IsDir() {
					out = append(out, RootEntry{Path: relative, Directory: true, Mode: uint32(info.Mode().Perm()), Modified: info.ModTime().UnixNano()})
					dirs = append(dirs, relative)
				} else if info.Mode().IsRegular() {
					v, e := rootMeasure(ctx, root, relative)
					if e != nil {
						f.Close()
						return nil, e
					}
					out = append(out, v)
				} else {
					f.Close()
					return nil, errors.New("Links, devices and other special files must be resolved before migrating storage.")
				}
			}
			if re == io.EOF {
				break
			}
			if re != nil {
				f.Close()
				return nil, re
			}
		}
		f.Close()
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}
func PlanMediaRoot(ctx context.Context, cfg config.Config, destination string) (RootMigration, error) {
	var p RootMigration
	close, e := lockRootMigration(cfg, false)
	if e != nil {
		return p, e
	}
	defer close()
	if e = idleRootMigration(ctx, cfg); e != nil {
		return p, e
	}
	if previous, e := rootPlanRead(cfg.ConfigDir); e == nil && previous.State != "cancelled" && previous.State != "completed" {
		return p, errors.New("Finish or cancel the existing storage plan first.")
	} else if e != nil && !errors.Is(e, os.ErrNotExist) {
		return p, e
	}
	destination, e = filepath.Abs(destination)
	if e != nil || filepath.Dir(destination) == destination {
		return p, ErrRootMigration
	}
	parent, e := rootDir(filepath.Dir(destination))
	if e != nil {
		return p, e
	}
	defer parent.Close()
	if _, e = parent.Lstat(filepath.Base(destination)); !errors.Is(e, os.ErrNotExist) {
		return p, errors.New("The destination must be a new folder under an existing mounted parent.")
	}
	for _, dir := range []string{cfg.MediaDir, cfg.ConfigDir, cfg.DownloadsDir, cfg.BackupsDir} {
		if dir == "" {
			continue
		}
		resolved, e := filepath.EvalSymlinks(dir)
		if e != nil {
			return p, e
		}
		if storage.Overlap(resolved, destination) || storage.Overlap(destination, resolved) {
			return p, ErrRootMigration
		}
	}
	source, e := rootDir(cfg.MediaDir)
	if e != nil {
		return p, e
	}
	defer source.Close()
	if e = prepareRootChecksums(ctx, cfg, source); e != nil {
		return p, e
	}
	entries, e := rootInventory(ctx, source, "")
	if e != nil {
		return p, e
	}
	sourceID, e := rootIdentity(source)
	if e != nil {
		return p, e
	}
	parentID, e := rootIdentity(parent)
	if e != nil {
		return p, e
	}
	info, e := source.Stat(".")
	if e != nil {
		return p, e
	}
	p = RootMigration{ID: randomRootID(), State: "planned", Source: cfg.MediaDir, Destination: destination, SourceIdentity: sourceID, ParentIdentity: parentID, RootMode: uint32(info.Mode().Perm()), Created: time.Now().Unix(), Entries: entries}
	for _, v := range entries {
		p.Bytes += v.Size
	}
	p.Digest = rootDigest(p)
	return p, saveRootPlan(cfg, p)
}
func migrationReceipt(p RootMigration) string { return ".reelscarey-root-" + p.ID + ".json" }
func checkReceipt(root *os.Root, p RootMigration) error {
	f, e := storage.Regular(root, migrationReceipt(p), 1024)
	if e != nil {
		return ErrRootMigration
	}
	defer f.Close()
	b, e := io.ReadAll(f)
	if e != nil || string(b) != p.Digest+"\n" {
		return ErrRootMigration
	}
	return nil
}
func createReceipt(root *os.Root, p RootMigration) error {
	f, e := root.OpenFile(migrationReceipt(p), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	_, e = f.WriteString(p.Digest + "\n")
	e = errors.Join(e, f.Sync(), f.Close())
	if e != nil {
		return e
	}
	return storage.SyncDir(root)
}
func verifyRootTree(ctx context.Context, root *os.Root, p RootMigration, source bool) error {
	skip := ""
	if !source {
		if e := checkReceipt(root, p); e != nil {
			return e
		}
		skip = migrationReceipt(p)
	}
	entries, e := rootInventory(ctx, root, skip)
	if e != nil {
		return e
	}
	expected := append([]RootEntry{}, p.Entries...)
	if !source {
		for n := range entries {
			entries[n].Identity = ""
		}
		for n := range expected {
			expected[n].Identity = ""
		}
	}
	if !reflect.DeepEqual(entries, expected) {
		return ErrRootMigration
	}
	return nil
}
func sourceRoot(ctx context.Context, cfg config.Config, p RootMigration) (*os.Root, error) {
	if cfg.MediaDir != p.Source {
		return nil, ErrRootMigration
	}
	root, e := rootDir(cfg.MediaDir)
	if e != nil {
		return nil, e
	}
	id, e := rootIdentity(root)
	if e != nil || id != p.SourceIdentity {
		root.Close()
		return nil, ErrRootMigration
	}
	if e = verifyRootTree(ctx, root, p, true); e != nil {
		root.Close()
		return nil, e
	}
	return root, nil
}
func ownMigrationMarker(cfg config.Config, p RootMigration, create bool) error {
	root, e := os.OpenRoot(cfg.ConfigDir)
	if e != nil {
		return e
	}
	defer root.Close()
	f, e := storage.Regular(root, rootCopyMarker, 1024)
	if errors.Is(e, os.ErrNotExist) && create {
		f, e = root.OpenFile(rootCopyMarker, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return e
		}
		_, e = f.WriteString(p.Digest + "\n")
		e = errors.Join(e, f.Sync(), f.Close())
		if e != nil {
			return e
		}
		return storage.SyncDir(root)
	}
	if e != nil {
		return e
	}
	defer f.Close()
	b, e := io.ReadAll(f)
	if e != nil || string(b) != p.Digest+"\n" {
		return ErrRootMigration
	}
	return nil
}
func CopyMediaRoot(ctx context.Context, cfg config.Config, digest, version string) (RootMigration, error) {
	var p RootMigration
	close, e := lockRootMigration(cfg, true)
	if e != nil {
		return p, e
	}
	defer close()
	p, e = rootPlanRead(cfg.ConfigDir)
	if e != nil {
		return p, e
	}
	if digest != p.Digest || (p.State != "planned" && p.State != "copying" && p.State != "copied") {
		return p, ErrRootMigration
	}
	if e = idleRootMigration(ctx, cfg); e != nil {
		return p, e
	}
	source, e := sourceRoot(ctx, cfg, p)
	if e != nil {
		return p, e
	}
	defer source.Close()
	parent, e := rootDir(filepath.Dir(p.Destination))
	if e != nil {
		return p, e
	}
	defer parent.Close()
	parentID, e := rootIdentity(parent)
	if e != nil || parentID != p.ParentIdentity {
		return p, ErrRootMigration
	}
	if p.State == "planned" {
		if e = instance.CheckStart(cfg.ConfigDir); e != nil {
			if e = ownMigrationMarker(cfg, p, false); e != nil {
				return p, e
			}
		}
		if p.Backup == "" {
			if cfg.BackupsDir == "" {
				return p, errors.New("Configure a separate private backup folder before copying storage.")
			}
			p.Backup = filepath.Join(cfg.BackupsDir, "media-root-"+p.ID+"-"+randomRootID())
			if _, e = BackupOwned(ctx, cfg, p.Backup, version); e != nil {
				return p, e
			}
			if e = saveRootPlan(cfg, p); e != nil {
				return p, e
			}
		}
		if _, e = Verify(ctx, p.Backup); e != nil {
			return p, e
		}
		if e = ownMigrationMarker(cfg, p, true); e != nil {
			return p, e
		}
		p.State = "copying"
		if e = saveRootPlan(cfg, p); e != nil {
			return p, e
		}
	} else if e = ownMigrationMarker(cfg, p, false); e != nil {
		return p, e
	}
	base := filepath.Base(p.Destination)
	stage := ".reelscarey-root-copy-" + p.ID
	if info, e := parent.Lstat(base); e == nil {
		if !info.IsDir() {
			return p, ErrRootMigration
		}
		target, e := migrationChild(parent, base)
		if e != nil {
			return p, e
		}
		e = verifyRootTree(ctx, target, p, false)
		target.Close()
		if e != nil {
			return p, e
		}
		p.State = "copied"
		return p, saveRootPlan(cfg, p)
	} else if !errors.Is(e, os.ErrNotExist) {
		return p, e
	}
	if _, e = parent.Lstat(stage); errors.Is(e, os.ErrNotExist) {
		if e = parent.Mkdir(stage, 0700); e != nil {
			return p, e
		}
		target, e := migrationChild(parent, stage)
		if e != nil {
			return p, e
		}
		e = createReceipt(target, p)
		target.Close()
		if e != nil {
			return p, e
		}
		if e = storage.SyncDir(parent); e != nil {
			return p, e
		}
	} else if e != nil {
		return p, e
	}
	target, e := migrationChild(parent, stage)
	if e != nil {
		return p, e
	}
	defer target.Close()
	if e = checkReceipt(target, p); e != nil {
		return p, e
	}
	for _, v := range p.Entries {
		if !v.Directory {
			continue
		}
		if info, e := target.Lstat(filepath.FromSlash(v.Path)); errors.Is(e, os.ErrNotExist) {
			if e = target.MkdirAll(filepath.FromSlash(v.Path), 0700); e != nil {
				return p, e
			}
		} else if e != nil || !info.IsDir() {
			return p, ErrRootMigration
		}
	}
	work := ".reelscarey-root-work-" + p.ID
	if _, e = parent.Lstat(work); errors.Is(e, os.ErrNotExist) {
		if e = parent.Mkdir(work, 0700); e != nil {
			return p, e
		}
		scratch, e := migrationChild(parent, work)
		if e != nil {
			return p, e
		}
		e = createReceipt(scratch, p)
		scratch.Close()
		if e != nil {
			return p, e
		}
	} else if e != nil {
		return p, e
	}
	scratch, e := migrationChild(parent, work)
	if e != nil {
		return p, e
	}
	e = checkReceipt(scratch, p)
	scratch.Close()
	if e != nil {
		return p, e
	}
	for _, v := range p.Entries {
		if v.Directory {
			continue
		}
		if e = copyRootEntry(ctx, source, target, parent, stage, work, v); e != nil {
			return p, e
		}
	}
	for i := len(p.Entries) - 1; i >= 0; i-- {
		v := p.Entries[i]
		if !v.Directory {
			continue
		}
		if e = target.Chmod(filepath.FromSlash(v.Path), os.FileMode(v.Mode)); e != nil {
			return p, e
		}
		stamp := time.Unix(0, v.Modified)
		if e = target.Chtimes(filepath.FromSlash(v.Path), stamp, stamp); e != nil {
			return p, e
		}
		f, e := storage.Open(target, v.Path)
		if e != nil {
			return p, e
		}
		e = errors.Join(f.Sync(), f.Close())
		if e != nil {
			return p, e
		}
	}
	if e = verifyRootTree(ctx, source, p, true); e != nil {
		return p, e
	}
	if e = verifyRootTree(ctx, target, p, false); e != nil {
		return p, e
	}
	if e = target.Chmod(".", os.FileMode(p.RootMode)); e != nil {
		return p, e
	}
	if e = storage.SyncDir(target); e != nil {
		return p, e
	}
	if e = publishRootFolder(parent, stage, base); e != nil {
		return p, e
	}
	p.State = "copied"
	return p, saveRootPlan(cfg, p)
}
func copyRootEntry(ctx context.Context, source, target, parent *os.Root, stage, work string, v RootEntry) error {
	if _, e := target.Lstat(filepath.FromSlash(v.Path)); e == nil {
		got, e := rootMeasure(ctx, target, v.Path)
		if e != nil || got.Size != v.Size || got.SHA != v.SHA || got.Mode != v.Mode || got.Modified != v.Modified {
			return ErrRootMigration
		}
		return nil
	} else if !errors.Is(e, os.ErrNotExist) {
		return e
	}
	input, e := storage.Open(source, v.Path)
	if e != nil {
		return e
	}
	defer input.Close()
	identity, e := organization.FileIdentity(input)
	if e != nil || identity != v.Identity {
		return ErrRootMigration
	}
	temporary := path.Join(work, "copy-"+randomRootID())
	out, e := parent.OpenFile(filepath.FromSlash(temporary), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	h := sha256.New()
	n, e := io.Copy(io.MultiWriter(out, h), &contextReader{ctx, input})
	stamp := time.Unix(0, v.Modified)
	if e == nil {
		e = out.Chmod(os.FileMode(v.Mode))
	}
	if e == nil {
		e = parent.Chtimes(filepath.FromSlash(temporary), stamp, stamp)
	}
	owned, statErr := out.Stat()
	e = errors.Join(e, statErr, out.Sync(), out.Close())
	if e != nil {
		return e
	}
	if n != v.Size || hex.EncodeToString(h.Sum(nil)) != v.SHA {
		return ErrRootMigration
	}
	if e = parent.Link(filepath.FromSlash(temporary), filepath.FromSlash(path.Join(stage, v.Path))); e != nil {
		return e
	}
	visible, e := parent.Lstat(filepath.FromSlash(temporary))
	if e != nil || !os.SameFile(owned, visible) {
		return ErrRootMigration
	}
	if e = parent.Remove(filepath.FromSlash(temporary)); e != nil {
		return e
	}
	dir, e := storage.Open(target, path.Dir(v.Path))
	if e != nil {
		return e
	}
	return errors.Join(dir.Sync(), dir.Close())
}
func ActivateMediaRoot(ctx context.Context, cfg config.Config, digest string) (RootMigration, error) {
	var p RootMigration
	close, e := lockRootMigration(cfg, true)
	if e != nil {
		return p, e
	}
	defer close()
	p, e = rootPlanRead(cfg.ConfigDir)
	if e != nil {
		return p, e
	}
	if p.Digest != digest || (p.State != "copied" && p.State != "completed") || cfg.MediaDir != p.Source {
		return p, ErrRootMigration
	}
	root, e := rootDir(cfg.MediaDir)
	if e != nil {
		return p, e
	}
	defer root.Close()
	if e = verifyRootTree(ctx, root, p, false); e != nil {
		return p, e
	}
	configRoot, e := os.OpenRoot(cfg.ConfigDir)
	if e != nil {
		return p, e
	}
	defer configRoot.Close()
	if p.State == "completed" {
		if _, e = configRoot.Lstat(rootCopyMarker); errors.Is(e, os.ErrNotExist) {
			return p, nil
		} else if e != nil {
			return p, e
		}
	}
	if info, e := configRoot.Lstat("recovery-mode"); errors.Is(e, os.ErrNotExist) {
		if e = storage.WriteJSON(configRoot, "recovery-mode", map[string]int{"format": 1}); e != nil {
			return p, e
		}
	} else if e != nil || !info.Mode().IsRegular() {
		return p, ErrRootMigration
	}
	p.State = "completed"
	if e = saveRootPlan(cfg, p); e != nil {
		return p, e
	}
	if _, e = configRoot.Lstat(rootCopyMarker); e == nil {
		if e = ownMigrationMarker(cfg, p, false); e != nil {
			return p, e
		}
		if e = configRoot.Remove(rootCopyMarker); e != nil {
			return p, e
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		return p, e
	}
	return p, storage.SyncDir(configRoot)
}
func CancelMediaRoot(ctx context.Context, cfg config.Config, digest string) (RootMigration, error) {
	var p RootMigration
	close, e := lockRootMigration(cfg, true)
	if e != nil {
		return p, e
	}
	defer close()
	p, e = rootPlanRead(cfg.ConfigDir)
	if e != nil {
		return p, e
	}
	if p.Digest != digest || p.State == "completed" {
		return p, ErrRootMigration
	}
	if cfg.MediaDir != p.Source {
		return p, ErrRootMigration
	}
	root, e := rootDir(cfg.MediaDir)
	if e != nil {
		return p, e
	}
	identity, e := rootIdentity(root)
	if e != nil || identity != p.SourceIdentity {
		root.Close()
		return p, ErrRootMigration
	}
	root.Close()
	configRoot, e := os.OpenRoot(cfg.ConfigDir)
	if e != nil {
		return p, e
	}
	defer configRoot.Close()
	if _, e = configRoot.Lstat(rootCopyMarker); e == nil {
		if e = ownMigrationMarker(cfg, p, false); e != nil {
			return p, e
		}
		if e = configRoot.Remove(rootCopyMarker); e != nil {
			return p, e
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		return p, e
	}
	p.State = "cancelled"
	if e = saveRootPlan(cfg, p); e != nil {
		return p, e
	}
	return p, storage.SyncDir(configRoot)
}

func prepareRootChecksums(ctx context.Context, cfg config.Config, root *os.Root) error {
	db, err := database.OpenCurrent(ctx, filepath.Join(cfg.ConfigDir, "reelscarey.db"))
	if err != nil {
		return err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `SELECT id FROM media_files WHERE sha256='' ORDER BY id`)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		if _, err = filecheck.EnsureSHA(ctx, db, root, id); err != nil {
			return err
		}
	}
	return nil
}
