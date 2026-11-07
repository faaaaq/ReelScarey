package transfer

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"unicode"

	"github.com/faaaaq/ReelScarey/internal/organization"
	"github.com/faaaaq/ReelScarey/internal/quality"
	"github.com/faaaaq/ReelScarey/internal/settings"
)

type Importer struct {
	service *Service
	Prober  Prober
	link    func(*os.File, *os.File, string) error
}

func NewImporter(s *Service) *Importer {
	return &Importer{service: s, Prober: FFprobe{}, link: linkSource}
}

type importPlan struct {
	Source, Destination, Stage, Hash, Probe, Mode, Phase string
	Size, Mtime                                          int64
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
func fileHash(ctx context.Context, f *os.File) (string, error) {
	if _, e := f.Seek(0, 0); e != nil {
		return "", e
	}
	h := sha256.New()
	_, e := io.Copy(h, contextReader{ctx, f})
	if e != nil {
		return "", e
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func safeRelative(name string) (string, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	if !textOK(name, 4096) || strings.HasPrefix(name, "/") || strings.Contains(name, ":") {
		return "", problem("unsafe_path", "A download path is outside the configured root or contains unsafe components.")
	}
	for _, p := range strings.Split(name, "/") {
		if p == "" || p == "." || p == ".." {
			return "", problem("unsafe_path", "Download paths cannot contain empty or dot components.")
		}
	}
	if !filepath.IsLocal(filepath.FromSlash(name)) {
		return "", problem("unsafe_path", "The download path is not a local relative path.")
	}
	return name, nil
}
func mapJobSource(j Job, save, file, remote string) (string, error) {
	relative, e := MapSource(save, file, remote)
	if e != nil {
		return "", e
	}
	if j.clientLocal != "" {
		relative = j.clientLocal + "/" + relative
	}
	return safeRelative(relative)
}
func MapSource(save, file, remote string) (string, error) {
	base, e := remoteRoot(remote)
	if e != nil {
		return "", e
	}
	save = strings.ReplaceAll(save, "\\", "/")
	if _, e = remoteRoot(save); e != nil {
		return "", problem("path_mapping", "The download client returned an unsafe save path.")
	}
	compareBase, compareSave := base, save
	if regexp.MustCompile(`^[A-Za-z]:/`).MatchString(base) {
		compareBase = strings.ToLower(base)
		compareSave = strings.ToLower(save)
	}
	if compareSave != compareBase && !strings.HasPrefix(compareSave, compareBase+"/") {
		return "", problem("path_mapping", "The download is outside the configured client download root. Check the shared download mount and remote path.")
	}
	relative := strings.TrimPrefix(save[len(base):], "/")
	file, e = safeRelative(file)
	if e != nil {
		return "", e
	}
	if relative != "" {
		file = relative + "/" + file
	}
	return safeRelative(file)
}
func segments(root *os.Root, name string, allowMissing bool) error {
	parts := strings.Split(filepath.ToSlash(name), "/")
	for i := range parts {
		info, e := root.Lstat(filepath.FromSlash(strings.Join(parts[:i+1], "/")))
		if errors.Is(e, os.ErrNotExist) && allowMissing {
			return nil
		}
		if e != nil {
			return e
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return problem("unsafe_path", "Symbolic links are not imported or used as destination folders.")
		}
		if i < len(parts)-1 && !info.IsDir() {
			return problem("unsafe_path", "An import path contains a non-directory component.")
		}
	}
	return nil
}
func overlap(a, b string) bool {
	rel, e := filepath.Rel(a, b)
	return e == nil && (rel == "." || filepath.IsLocal(rel))
}
func roots(downloads, media, configDir string) (*os.Root, *os.Root, error) {
	paths := []string{downloads, media, configDir}
	for i, p := range paths {
		resolved, e := filepath.EvalSymlinks(p)
		if e != nil {
			return nil, nil, problem("storage_unavailable", "The download, media, and config roots must exist and be accessible on the ReelScarey server.")
		}
		paths[i] = resolved
	}
	for i := range paths {
		for j := i + 1; j < len(paths); j++ {
			if overlap(paths[i], paths[j]) || overlap(paths[j], paths[i]) {
				return nil, nil, problem("unsafe_roots", "Config, downloads, and media must use separate, non-overlapping directories.")
			}
		}
	}
	src, e := os.OpenRoot(downloads)
	if e != nil {
		return nil, nil, e
	}
	dst, e := os.OpenRoot(media)
	if e != nil {
		src.Close()
		return nil, nil, e
	}
	return src, dst, nil
}
func movieName(title string, year *int, id int64) string {
	r := []rune(strings.Map(func(c rune) rune {
		if unicode.IsControl(c) || strings.ContainsRune(`<>:"/\|?*`, c) {
			return '_'
		}
		return c
	}, title))
	if len(r) > 100 {
		r = r[:100]
	}
	name := strings.Trim(string(r), " .")
	if name == "" {
		name = "Movie"
	}
	if year != nil {
		b, _ := json.Marshal(*year)
		name += " (" + string(b) + ")"
	}
	b, _ := json.Marshal(id)
	return name + " [tmdb-" + string(b) + "]"
}
func video(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".mkv", ".mp4", ".m4v", ".avi", ".mov", ".webm", ".ts", ".m2ts":
		return true
	}
	return false
}

var extraVideo = regexp.MustCompile(`(?i)(^|[\s._\-/])(sample|trailer)([\s._\-/]|$)`)

func (i *Importer) plan(ctx context.Context, j Job, source string, size int64, src *os.File) (importPlan, error) {
	var p importPlan
	s := i.service
	e := s.db.QueryRowContext(ctx, `SELECT source_path,destination_path,stage_name,size_bytes,source_mtime,sha256,probe_json,mode,phase FROM import_operations WHERE download_id=? AND file_index=?`, j.ID, j.importIndex()).Scan(&p.Source, &p.Destination, &p.Stage, &p.Size, &p.Mtime, &p.Hash, &p.Probe, &p.Mode, &p.Phase)
	if e == nil {
		if p.Source != source || p.Size != size {
			return p, problem("source_changed", "The completed torrent's file changed after its import was planned.")
		}
		return p, nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return p, e
	}
	before, e := src.Stat()
	if e != nil {
		return p, e
	}
	if !before.Mode().IsRegular() || before.Size() != size || size <= 0 {
		return p, problem("source_changed", "The source is not a complete regular movie file of the expected size.")
	}
	probe, e := i.Prober.Probe(ctx, src)
	if e != nil {
		return p, e
	}
	if j.Kind != "series" && (j.MovieVersion == "hd" || j.MovieVersion == "uhd") && !quality.MovieVersionMatches(j.MovieVersion, quality.VerifiedQuality(probe.Width, probe.Height)) {
		return p, problem("version_mismatch", "The downloaded video's inspected resolution does not match the selected movie version. The source and existing versions were left unchanged.")
	}
	hash, e := fileHash(ctx, src)
	if e != nil {
		return p, e
	}
	after, e := src.Stat()
	if e != nil {
		return p, e
	}
	if after.Size() != before.Size() || after.ModTime() != before.ModTime() {
		return p, problem("source_changed", "The source changed while it was being verified. Wait for the client and retry.")
	}
	var replacement int
	if e = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM acquisition_replacements WHERE source_kind='download' AND job_id=?`, j.ID).Scan(&replacement); e != nil {
		return p, e
	}
	// The verified probe drives the naming quality tag: resolution plus an
	// explicit HDR or SDR transfer label. Every import is named from its own
	// inspected facts, and HD/4K downloads no longer need a separate label —
	// the inspected resolution is stronger evidence than the requested target.
	tag := organization.QualityTag(probe.Width, probe.Height, probe.HDR)
	// Snapshot-planned movies render the tag inside Destination from
	// snapshot.Quality; series and plain-folder movies append it explicitly.
	var destination string
	var tagged bool
	if j.Kind == "series" {
		destination, e = s.tvDestination(ctx, j, source)
	} else if j.organizationSnapshot != "" {
		var snapshot organization.Snapshot
		if e = json.Unmarshal([]byte(j.organizationSnapshot), &snapshot); e == nil {
			snapshot.Quality = tag
			if j.MovieVersion == "additional" {
				snapshot.MovieVersion = "Version " + j.ID
			}
			destination, e = organization.Destination(snapshot, nil, source)
			tagged = true
		}
	} else {
		var name string
		movie, err := s.MovieByID(ctx, j.MovieID)
		if err != nil {
			return p, err
		}
		prefs, err := settings.New(s.db).Get(ctx)
		if err != nil {
			return p, err
		}
		name = movieName(movie.Title, movie.Year, movie.TMDbID)
		destination = path.Join(prefs.MovieFolder, name, name+strings.ToLower(path.Ext(source)))
	}
	if e != nil {
		var namingError *organization.Error
		if errors.As(e, &namingError) {
			return p, problem(namingError.Code, namingError.Message)
		}
		return p, e
	}
	if tag != "" && !tagged {
		ext := path.Ext(destination)
		destination = strings.TrimSuffix(destination, ext) + " [" + tag + "]" + ext
	}
	if _, e = safeRelative(destination); e != nil {
		return p, e
	}
	b, e := json.Marshal(probe)
	if e != nil {
		return p, e
	}
	if replacement > 0 {
		destination = path.Join(".reelscarey-versions", "acquired-"+j.stageID()+strings.ToLower(path.Ext(source)))
	}
	p = importPlan{Source: source, Destination: destination, Stage: ".reelscarey-" + j.stageID() + ".part", Size: size, Mtime: before.ModTime().UnixNano(), Hash: hash, Probe: string(b), Mode: "pending", Phase: "planned"}
	result, e := s.db.ExecContext(ctx, `INSERT INTO import_operations(download_id,file_index,source_path,destination_path,stage_name,size_bytes,source_mtime,sha256,probe_json,phase,updated_at) SELECT ?,?,?,?,?,?,?,?,?,'planned',unixepoch() WHERE EXISTS(SELECT 1 FROM downloads WHERE id=? AND lease_owner=? AND lease_until>=unixepoch())`, j.ID, j.importIndex(), p.Source, p.Destination, p.Stage, p.Size, p.Mtime, p.Hash, p.Probe, j.ID, j.owner)
	if e != nil {
		return p, e
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return p, ErrLease
	}
	return p, nil
}
func (i *Importer) phase(ctx context.Context, j Job, p *importPlan, phase, mode string) error {
	result, e := i.service.db.ExecContext(ctx, `UPDATE import_operations SET phase=?,mode=?,updated_at=unixepoch() WHERE download_id=? AND file_index=? AND EXISTS(SELECT 1 FROM downloads WHERE id=? AND lease_owner=? AND lease_until>=unixepoch())`, phase, mode, j.ID, j.importIndex(), j.ID, j.owner)
	if e != nil {
		return e
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return ErrLease
	}
	p.Phase = phase
	p.Mode = mode
	return nil
}
func syncParent(parent *os.File) error {
	e := parent.Sync()
	if runtime.GOOS == "windows" {
		return nil
	}
	return e
}
func (i *Importer) Execute(ctx context.Context, j Job, source string, size int64) error {
	defer organization.ReadMedia()()
	s := i.service
	if e := organization.EnsureMediaAvailable(ctx, s.db); e != nil {
		return e
	}
	srcRoot, dstRoot, e := roots(s.cfg.DownloadsDir, s.cfg.MediaDir, s.cfg.ConfigDir)
	if e != nil {
		return e
	}
	defer srcRoot.Close()
	defer dstRoot.Close()
	if _, e = safeRelative(source); e != nil {
		return e
	}
	if e = segments(srcRoot, source, false); e != nil {
		return problem("source_unavailable", "The completed movie file is missing, inaccessible, or uses a symbolic link. Check the download mount.")
	}
	src, e := srcRoot.Open(filepath.FromSlash(source))
	if e != nil {
		return problem("source_unavailable", "The completed movie file could not be opened under the download root.")
	}
	defer src.Close()
	p, e := i.plan(ctx, j, source, size, src)
	if e != nil {
		return e
	}
	info, e := src.Stat()
	if e != nil {
		return e
	}
	if !info.Mode().IsRegular() || info.Size() != p.Size || info.ModTime().UnixNano() != p.Mtime {
		return problem("source_changed", "The source no longer matches its import journal. Restore the original completed file before retrying.")
	}
	hash, e := fileHash(ctx, src)
	if e != nil {
		return e
	}
	if hash != p.Hash {
		return problem("source_changed", "The source content changed after import planning.")
	}
	dir := path.Dir(p.Destination)
	if e = segments(dstRoot, dir, true); e != nil {
		return problem("unsafe_path", "The destination folder is unsafe or inaccessible.")
	}
	if e = dstRoot.MkdirAll(filepath.FromSlash(dir), 0755); e != nil {
		return problem("storage_unavailable", "The media root is not writable. Check the mount and permissions.")
	}
	folder, e := dstRoot.OpenRoot(filepath.FromSlash(dir))
	if e != nil {
		return e
	}
	defer folder.Close()
	parent, e := folder.Open(".")
	if e != nil {
		return e
	}
	defer parent.Close()
	name := path.Base(p.Destination)
	if e = ctx.Err(); e != nil {
		return e
	}
	stageInfo, stageErr := folder.Lstat(p.Stage)
	if errors.Is(stageErr, os.ErrNotExist) {
		if p.Phase == "published" || p.Phase == "committed" {
			return i.commitPublished(ctx, j, p, folder, name, dstRoot)
		}
		if _, e = folder.Lstat(name); e == nil {
			return problem("destination_exists", "A library file already exists at this destination. It was not overwritten.")
		} else if !errors.Is(e, os.ErrNotExist) {
			return e
		}
		mode := "hardlink"
		if e = i.link(src, parent, p.Stage); e != nil {
			if errors.Is(e, os.ErrExist) {
				return problem("staging_conflict", "An unexpected staging file appeared. Retry after reviewing it.")
			}
			mode = "copy"
			stage, e := folder.OpenFile(p.Stage, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
			if e != nil {
				return problem("storage_unavailable", "The library staging file could not be created safely.")
			}
			if e = i.phase(ctx, j, &p, "planned", mode); e != nil {
				stage.Close()
				return e
			}
			e = i.copy(ctx, src, stage, p)
			stage.Close()
			if e != nil {
				return e
			}
		}
		if mode == "hardlink" {
			if e = i.phase(ctx, j, &p, "planned", mode); e != nil {
				return e
			}
		}
		if e = syncParent(parent); e != nil {
			return e
		}
	} else if stageErr != nil {
		return stageErr
	} else if !stageInfo.Mode().IsRegular() {
		return problem("staging_conflict", "The staging path is not a regular file. It was left unchanged.")
	}
	stageInfo, e = folder.Stat(p.Stage)
	if e != nil {
		return e
	}
	sourceInfo, e := src.Stat()
	if e != nil {
		return e
	}
	if os.SameFile(stageInfo, sourceInfo) && p.Mode != "hardlink" {
		if e = i.phase(ctx, j, &p, "planned", "hardlink"); e != nil {
			return e
		}
	}
	flags := os.O_RDWR
	if p.Mode == "hardlink" {
		flags = os.O_RDONLY
	}
	if e = segments(folder, p.Stage, false); e != nil {
		return problem("staging_conflict", "The staging path changed or became a symbolic link.")
	}
	expectedStageInfo := stageInfo
	stage, e := folder.OpenFile(p.Stage, flags, 0)
	if e != nil {
		return e
	}
	defer stage.Close()
	stageInfo, e = stage.Stat()
	if e != nil {
		return e
	}
	if !os.SameFile(expectedStageInfo, stageInfo) || !stageInfo.Mode().IsRegular() {
		return problem("staging_conflict", "The staging file changed while it was being opened. It was left unchanged.")
	}
	if stageInfo.Size() < p.Size {
		if p.Mode == "hardlink" {
			return problem("source_changed", "A hardlinked source changed during import.")
		}
		if e = i.copy(ctx, src, stage, p); e != nil {
			return e
		}
		if e = i.phase(ctx, j, &p, "planned", "copy"); e != nil {
			return e
		}
	}
	stageHash, e := fileHash(ctx, stage)
	if e != nil {
		return e
	}
	stageInfo, e = stage.Stat()
	if e != nil {
		return e
	}
	if stageInfo.Size() != p.Size || stageHash != p.Hash {
		return problem("staging_conflict", "The staged file does not match the verified source. It was left for review.")
	}
	if p.Mode != "hardlink" {
		if e = stage.Chmod(0644); e != nil {
			return e
		}
	}
	if e = stage.Sync(); e != nil && p.Mode != "hardlink" {
		return e
	}
	// Windows link publication needs the staged file's write handle closed.
	if e = stage.Close(); e != nil {
		return e
	}
	if e = i.phase(ctx, j, &p, "staged", p.Mode); e != nil {
		return e
	}
	if e = ctx.Err(); e != nil {
		return e
	}
	// Link is an atomic no-replace publication. Rename would overwrite on Unix.
	if e = folder.Link(p.Stage, name); e != nil {
		if !errors.Is(e, os.ErrExist) {
			return problem("publish_failed", "This media filesystem could not publish the staged file without overwriting. The verified staging file remains for retry.")
		}
		existing, e := folder.Open(name)
		if e != nil {
			return problem("destination_exists", "The destination already exists and was left unchanged.")
		}
		existingInfo, statErr := existing.Stat()
		existing.Close()
		if statErr != nil || !os.SameFile(stageInfo, existingInfo) {
			return problem("destination_exists", "A different library file already exists at the destination. It was not overwritten.")
		}
	}
	if e = syncParent(parent); e != nil {
		return e
	}
	if e = i.phase(ctx, j, &p, "published", p.Mode); e != nil {
		return e
	}
	return i.commitPublished(ctx, j, p, folder, name, dstRoot)
}
func (i *Importer) copy(ctx context.Context, src, stage *os.File, p importPlan) error {
	info, e := stage.Stat()
	if e != nil {
		return e
	}
	if !info.Mode().IsRegular() || info.Size() > p.Size {
		return problem("staging_conflict", "The app's staging file has unexpected contents.")
	}
	offset := info.Size()
	if _, e = stage.Seek(0, 0); e != nil {
		return e
	}
	if _, e = src.Seek(0, 0); e != nil {
		return e
	}
	a, b := sha256.New(), sha256.New()
	if _, e = io.CopyN(a, contextReader{ctx, stage}, offset); e != nil {
		return e
	}
	if _, e = io.CopyN(b, contextReader{ctx, src}, offset); e != nil {
		return e
	}
	if !strings.EqualFold(hex.EncodeToString(a.Sum(nil)), hex.EncodeToString(b.Sum(nil))) {
		return problem("staging_conflict", "The partial staging file does not match the source. It was left unchanged.")
	}
	if _, e = stage.Seek(offset, 0); e != nil {
		return e
	}
	if _, e = io.Copy(stage, contextReader{ctx, io.LimitReader(src, p.Size-offset)}); e != nil {
		return e
	}
	return stage.Sync()
}
func (i *Importer) commitPublished(ctx context.Context, j Job, p importPlan, folder *os.Root, name string, mediaRoot *os.Root) error {
	if e := segments(folder, name, false); e != nil {
		return e
	}
	file, e := folder.Open(name)
	if e != nil {
		return e
	}
	info, e := file.Stat()
	if e != nil {
		file.Close()
		return e
	}
	hash, e := fileHash(ctx, file)
	file.Close()
	if e != nil {
		return e
	}
	if !info.Mode().IsRegular() || info.Size() != p.Size || hash != p.Hash {
		return problem("destination_changed", "The published movie no longer matches its import journal.")
	}
	if e = segments(mediaRoot, p.Destination, false); e != nil {
		return problem("destination_changed", "The published library path changed before registration.")
	}
	visible, e := mediaRoot.Stat(filepath.FromSlash(p.Destination))
	if e != nil || !os.SameFile(info, visible) {
		return problem("destination_changed", "The destination folder moved before the movie could be registered.")
	}
	s := i.service
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var owner string
	if e = tx.QueryRowContext(ctx, `SELECT lease_owner FROM downloads WHERE id=? AND lease_until>=unixepoch()`, j.ID).Scan(&owner); e != nil || owner != j.owner {
		return ErrLease
	}
	var replacement int
	if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM acquisition_replacements WHERE source_kind='download' AND job_id=?`, j.ID).Scan(&replacement); e != nil {
		return e
	}
	if replacement > 0 {
		if _, e = tx.ExecContext(ctx, `UPDATE acquisition_replacements SET source_path=?,size_bytes=?,sha256=?,probe_json=?,file_index=? WHERE source_kind='download' AND job_id=?`, p.Destination, p.Size, p.Hash, p.Probe, j.importIndex(), j.ID); e != nil {
			return e
		}
		if e = tx.Commit(); e != nil {
			return e
		}
		return problem("replacement_review", "Acquired video is verified and retained. Review its replacement in Organize library.")
	}
	if e = s.registerImport(ctx, tx, j, p); e != nil {
		return e
	}
	if j.Kind != "series" {
		if _, e = tx.ExecContext(ctx, `UPDATE media_items SET status='available' WHERE id=?`, j.MovieID); e != nil {
			return e
		}
	}
	if _, e = tx.ExecContext(ctx, `UPDATE import_operations SET phase='committed',updated_at=unixepoch() WHERE download_id=? AND file_index=?`, j.ID, j.importIndex()); e != nil {
		return e
	}
	if j.Kind != "series" {
		if _, e = tx.ExecContext(ctx, `UPDATE downloads SET state='completed',progress=1,error_code='',error_message='',updated_at=unixepoch() WHERE id=?`, j.ID); e != nil {
			return e
		}
		if e = recordDownloadCompleted(ctx, tx, j.ID, j.Title); e != nil {
			return e
		}
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	// Removing this app-owned extra link never removes or changes the torrent's source.
	if staged, e := folder.Lstat(p.Stage); e == nil && staged.Mode().IsRegular() && os.SameFile(staged, info) {
		_ = folder.Remove(p.Stage)
	}
	return nil
}

// Remove only redundant app-named links to the very same committed file.
// Unknown files and all torrent sources are left untouched.
func (s *Service) cleanupCommitted(ctx context.Context) error {
	defer organization.ReadMedia()()
	if e := organization.EnsureMediaAvailable(ctx, s.db); e != nil {
		return e
	}
	rows, e := s.db.QueryContext(ctx, `SELECT destination_path,stage_name FROM import_operations WHERE phase='committed'`)
	if e != nil {
		return e
	}
	type entry struct{ destination, stage string }
	var entries []entry
	for rows.Next() {
		var v entry
		if e = rows.Scan(&v.destination, &v.stage); e != nil {
			rows.Close()
			return e
		}
		entries = append(entries, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	if len(entries) == 0 {
		return nil
	}
	root, e := os.OpenRoot(s.cfg.MediaDir)
	if e != nil {
		return e
	}
	defer root.Close()
	for _, v := range entries {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if _, e = safeRelative(v.destination); e != nil {
			continue
		}
		if !strings.HasPrefix(v.stage, ".reelscarey-") || path.Base(v.stage) != v.stage {
			continue
		}
		if e = segments(root, path.Dir(v.destination), false); e != nil {
			continue
		}
		folder, e := root.OpenRoot(filepath.FromSlash(path.Dir(v.destination)))
		if e != nil {
			continue
		}
		a, ea := folder.Lstat(v.stage)
		b, eb := folder.Lstat(path.Base(v.destination))
		if ea == nil && eb == nil && a.Mode().IsRegular() && b.Mode().IsRegular() && os.SameFile(a, b) {
			_ = folder.Remove(v.stage)
		}
		folder.Close()
	}
	return nil
}
