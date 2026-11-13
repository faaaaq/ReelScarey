package transfer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const archiveMarker = ".reelscarey-extract.json"
const archivePrefix = ".reelscarey-extracted-"

var archivePartRE = regexp.MustCompile(`(?i)^(.+)\.part(\d+)\.rar$`)
var archiveVolRE = regexp.MustCompile(`(?i)^(.+)\.r(\d{2,3})$`)
var archiveSplitRE = regexp.MustCompile(`(?i)^(.+)\.(\d{3})$`)

type archiveCandidate struct {
	Name string
	Kind string
	Base string
	Vol  int
	Size int64
}

type archiveSet struct {
	First string
	Kind  string
	Total int64
}

type archiveEntry struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

type archiveManifest struct {
	Files []archiveEntry `json:"files"`
}

func archiveCandidateOf(name string, size int64) (archiveCandidate, bool) {
	lower := strings.ToLower(name)
	if m := archivePartRE.FindStringSubmatch(lower); len(m) == 3 {
		n, _ := strconv.Atoi(m[2])
		return archiveCandidate{Name: name, Kind: "rar", Base: m[1], Vol: n, Size: size}, true
	}
	if m := archiveVolRE.FindStringSubmatch(lower); len(m) == 3 {
		n, _ := strconv.Atoi(m[2])
		return archiveCandidate{Name: name, Kind: "rar", Base: m[1], Vol: n + 1, Size: size}, true
	}
	if m := archiveSplitRE.FindStringSubmatch(lower); len(m) == 3 && m[2] == "001" {
		return archiveCandidate{Name: name, Kind: "split", Base: m[1], Vol: 1, Size: size}, true
	}
	switch strings.ToLower(path.Ext(name)) {
	case ".rar":
		return archiveCandidate{Name: name, Kind: "rar", Base: strings.TrimSuffix(lower, ".rar"), Vol: 1, Size: size}, true
	case ".zip":
		return archiveCandidate{Name: name, Kind: "zip", Base: lower, Vol: 1, Size: size}, true
	case ".7z":
		return archiveCandidate{Name: name, Kind: "7z", Base: lower, Vol: 1, Size: size}, true
	}
	return archiveCandidate{}, false
}

func findArchiveSets(files []TorrentFile) ([]archiveSet, error) {
	groups := map[string][]archiveCandidate{}
	for _, file := range files {
		if file.Priority <= 0 || file.Size <= 0 || file.Progress != 1 {
			continue
		}
		if _, e := safeRelative(file.Name); e != nil {
			return nil, e
		}
		candidate, ok := archiveCandidateOf(file.Name, file.Size)
		if !ok {
			continue
		}
		key := candidate.Kind + "\x00" + candidate.Base
		groups[key] = append(groups[key], candidate)
	}
	sets := []archiveSet{}
	for _, entries := range groups {
		sort.SliceStable(entries, func(i, j int) bool {
			if entries[i].Vol != entries[j].Vol {
				return entries[i].Vol < entries[j].Vol
			}
			aRar := strings.HasSuffix(strings.ToLower(entries[i].Name), ".rar")
			bRar := strings.HasSuffix(strings.ToLower(entries[j].Name), ".rar")
			if aRar != bRar {
				return aRar
			}
			return entries[i].Name < entries[j].Name
		})
		first := entries[0]
		if first.Vol > 1 {
			return nil, problem("archive_incomplete", "The first volume of a split archive is missing. Restore it and retry.")
		}
		if first.Kind == "rar" && first.Vol == 1 && strings.HasSuffix(strings.ToLower(first.Name), ".r00") && len(entries) > 0 {
			// The .rar first volume is missing when only .rNN volumes exist.
			return nil, problem("archive_incomplete", "The .rar first volume is missing from this archive set.")
		}
		total := int64(0)
		for _, entry := range entries {
			total += entry.Size
		}
		sets = append(sets, archiveSet{First: first.Name, Kind: first.Kind, Total: total})
	}
	sort.SliceStable(sets, func(i, j int) bool { return sets[i].First < sets[j].First })
	if len(sets) > 20 {
		return nil, problem("archive_invalid", "This download contains more than 20 archive sets. Extract them manually.")
	}
	return sets, nil
}

func (s *Service) jobSaveFolder(j Job, savePath, remote string) (string, error) {
	mapped, err := MapSource(savePath, "x", remote)
	if err != nil {
		return "", err
	}
	rel := strings.TrimSuffix(mapped, "/x")
	if rel == "x" {
		rel = ""
	}
	if j.clientLocal != "" {
		rel = path.Join(j.clientLocal, rel)
	}
	if rel == "." {
		rel = ""
	}
	if rel != "" {
		if _, err = safeRelative(rel); err != nil {
			return "", err
		}
	}
	return rel, nil
}

func (s *Service) extractArchiveMedia(ctx context.Context, j Job, files []TorrentFile, savePath, remoteRoot string) ([]TorrentFile, error) {
	sets, err := findArchiveSets(files)
	if err != nil || len(sets) == 0 {
		return nil, err
	}
	saveRel, err := s.jobSaveFolder(j, savePath, remoteRoot)
	if err != nil {
		return nil, err
	}
	rootRel := path.Join(archivePrefix + j.ID)
	root := filepath.Join(s.cfg.DownloadsDir, filepath.FromSlash(saveRel), filepath.FromSlash(rootRel))
	if err = inside(s.cfg.DownloadsDir, root); err != nil {
		return nil, err
	}
	// Reuse a previously verified extraction after an import retry.
	if manifest, ok := readArchiveManifest(root); ok {
		if entries, valid := validateArchiveEntries(root, manifest); valid {
			if err = s.setExtractPath(ctx, j, path.Join(saveRel, rootRel)); err != nil {
				return nil, err
			}
			return archiveTorrentFiles(files, rootRel, entries), nil
		}
		_ = os.RemoveAll(root)
	}
	if err = os.MkdirAll(root, 0o700); err != nil {
		return nil, problem("archive_extract", "The extraction folder could not be created. Check the download mount.")
	}
	limit := archiveExpansionLimit(sets)
	entries := []archiveEntry{}
	for n, set := range sets {
		// Only a first volume triggers an extraction. Other volumes are present
		// for the extractor to discover beside it.
		archiveRel := path.Join(saveRel, set.First)
		archive := filepath.Join(s.cfg.DownloadsDir, filepath.FromSlash(archiveRel))
		if err = inside(s.cfg.DownloadsDir, archive); err != nil {
			return nil, err
		}
		if _, statErr := os.Lstat(archive); statErr != nil {
			return nil, problem("archive_incomplete", "An archive volume is missing. Restore it and retry.")
		}
		out := filepath.Join(root, fmt.Sprintf("set-%d", n+1))
		if err = os.MkdirAll(out, 0o700); err != nil {
			return nil, problem("archive_extract", "The extraction folder could not be created.")
		}
		if err = runArchiveExtractor(ctx, archive, set.Kind, out); err != nil {
			return nil, err
		}
		setEntries, e := scanArchiveOut(out, limit, archiveMarker)
		if e != nil {
			return nil, e
		}
		for _, entry := range setEntries {
			entry.Name = path.Join(fmt.Sprintf("set-%d", n+1), entry.Name)
			entries = append(entries, entry)
		}
	}
	if len(entries) == 0 {
		_ = os.RemoveAll(root)
		return nil, problem("archive_invalid", "The archive did not contain a supported video file.")
	}
	raw, _ := json.Marshal(archiveManifest{Files: entries})
	temp := filepath.Join(root, archiveMarker+".tmp")
	if err = os.WriteFile(temp, raw, 0o600); err != nil {
		return nil, problem("archive_extract", "The extraction manifest could not be saved.")
	}
	if err = os.Rename(temp, filepath.Join(root, archiveMarker)); err != nil {
		return nil, problem("archive_extract", "The extraction manifest could not be committed.")
	}
	if err = s.setExtractPath(ctx, j, path.Join(saveRel, rootRel)); err != nil {
		return nil, err
	}
	return archiveTorrentFiles(files, rootRel, entries), nil
}

func archiveExpansionLimit(sets []archiveSet) int64 {
	total := int64(0)
	for _, set := range sets {
		total += set.Total
	}
	limit := total * 20
	if limit < 10<<30 {
		limit = 10 << 30
	}
	if limit > 200<<30 {
		limit = 200 << 30
	}
	return limit
}

func runArchiveExtractor(ctx context.Context, archive, kind, out string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	var command string
	var args []string
	switch kind {
	case "rar":
		command = "unrar"
		args = []string{"x", "-o+", "-p-", "-y", "-idq", archive, out + string(os.PathSeparator)}
	case "zip", "7z", "split":
		command = "7z"
		args = []string{"x", "-y", "-p-", "-bso0", "-bsp0", "-o" + out, archive}
	default:
		return problem("archive_invalid", "This archive type is not supported.")
	}
	path, err := exec.LookPath(command)
	if err != nil {
		return problem("archive_tool_missing", "Archive extraction requires "+command+" in the container image. Update the image and retry.")
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Stdin = nil
	var output limitedBuffer
	output.max = 256 << 10
	cmd.Stdout = &output
	cmd.Stderr = &output
	err = cmd.Run()
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return problem("archive_extract", "Archive extraction timed out. Retry after checking storage.")
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		return problem("archive_extract", "The archive extractor could not run.")
	}
	return archiveFailure(output.String())
}

func archiveFailure(output string) error {
	lower := strings.ToLower(output)
	switch {
	case strings.Contains(lower, "wrong password"), strings.Contains(lower, "password"), strings.Contains(lower, "checksum error"), strings.Contains(lower, "encrypted"), strings.Contains(lower, "unsupported method"):
		return problem("archive_password", "This archive is password-protected or encrypted. Extract it manually and retry with the media files.")
	case strings.Contains(lower, "missing volume"), strings.Contains(lower, "unexpected end"), strings.Contains(lower, "cannot open the file as archive"), strings.Contains(lower, "no files to extract"):
		return problem("archive_incomplete", "The archive set is incomplete or damaged. Restore every volume and retry.")
	case strings.Contains(lower, "crc failed"), strings.Contains(lower, "corrupt"):
		return problem("archive_invalid", "The archive failed its integrity check. Re-download it and retry.")
	default:
		return problem("archive_extract", "The archive could not be extracted. The original files were left in place; extract it manually and retry.")
	}
}

func scanArchiveOut(root string, limit int64, marker string) ([]archiveEntry, error) {
	entries := []archiveEntry{}
	total := int64(0)
	count := 0
	err := filepath.WalkDir(root, func(name string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, name)
		if err != nil || rel == "." {
			return err
		}
		if rel == marker || strings.HasPrefix(rel, marker+".") {
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return problem("archive_invalid", "The archive contains a link or special file. Extract it manually.")
		}
		count++
		if count > 5000 {
			return problem("archive_too_large", "The archive contains too many files. Extract it manually.")
		}
		total += info.Size()
		if info.Size() > limit || total > limit {
			return problem("archive_too_large", "The extracted archive exceeds the safe size limit. Extract it manually.")
		}
		slashed := filepath.ToSlash(rel)
		if video(slashed) && !extraVideo.MatchString(slashed) {
			entries = append(entries, archiveEntry{Name: slashed, Size: info.Size()})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return entries, nil
}

func readArchiveManifest(root string) (archiveManifest, bool) {
	var manifest archiveManifest
	raw, err := os.ReadFile(filepath.Join(root, archiveMarker))
	if err != nil || json.Unmarshal(raw, &manifest) != nil || len(manifest.Files) == 0 {
		return archiveManifest{}, false
	}
	return manifest, true
}

func validateArchiveEntries(root string, manifest archiveManifest) ([]archiveEntry, bool) {
	out := []archiveEntry{}
	for _, entry := range manifest.Files {
		rel, err := safeRelative(entry.Name)
		if err != nil {
			return nil, false
		}
		name := filepath.Join(root, filepath.FromSlash(rel))
		if inside(root, name) != nil {
			return nil, false
		}
		info, err := os.Lstat(name)
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() != entry.Size || entry.Size <= 0 {
			return nil, false
		}
		out = append(out, archiveEntry{Name: rel, Size: entry.Size})
	}
	return out, true
}

func archiveTorrentFiles(files []TorrentFile, rootRel string, entries []archiveEntry) []TorrentFile {
	maxIndex := -1
	for _, file := range files {
		if file.Index > maxIndex {
			maxIndex = file.Index
		}
	}
	out := make([]TorrentFile, 0, len(entries))
	for n, entry := range entries {
		out = append(out, TorrentFile{
			Index:    maxIndex + 1 + n,
			Name:     path.Join(rootRel, entry.Name),
			Size:     entry.Size,
			Progress: 1,
			Priority: 1,
		})
	}
	return out
}

func (s *Service) setExtractPath(ctx context.Context, j Job, relative string) error {
	if err := s.owned(ctx, j, `UPDATE downloads SET extract_path=?,updated_at=unixepoch()`, relative); err != nil {
		return err
	}
	return nil
}

// cleanupExtract removes only the verified app-owned extraction folder. The
// original archives remain in place for seeding.
func (s *Service) cleanupExtract(ctx context.Context, j Job) error {
	if j.ExtractPath == "" {
		// The in-memory job may predate extraction in this process; trust the
		// durable value before deciding there is nothing to clean up.
		_ = s.db.QueryRowContext(ctx, `SELECT extract_path FROM downloads WHERE id=?`, j.ID).Scan(&j.ExtractPath)
	}
	if j.ExtractPath == "" {
		return nil
	}
	rel, err := safeRelative(j.ExtractPath)
	if err != nil {
		return nil
	}
	base := path.Base(rel)
	if !strings.HasPrefix(base, archivePrefix) || !strings.HasSuffix(rel, base) {
		return nil
	}
	root := filepath.Join(s.cfg.DownloadsDir, filepath.FromSlash(rel))
	if inside(s.cfg.DownloadsDir, root) != nil {
		return nil
	}
	info, err := os.Lstat(root)
	if err != nil {
		return nil
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil
	}
	if _, ok := readArchiveManifest(root); !ok {
		return nil
	}
	if err = os.RemoveAll(root); err != nil {
		return err
	}
	// Clear the durable pointer through the lease fence so a stale claimant
	// cannot clear another worker's extraction state. owned appends its own
	// ownership/lease predicate.
	return s.owned(ctx, j, `UPDATE downloads SET extract_path=''`)
}

func inside(root, name string) error {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	nameAbs, err := filepath.Abs(name)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(rootAbs, nameAbs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return problem("unsafe_path", "An archive path is outside the download root.")
	}
	return nil
}
