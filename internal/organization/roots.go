package organization

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Scan roots are read-only discovery sources. They never change the library
// destination folder or naming templates used by organization plans.
type ScanRoot struct {
	Path      string `json:"path"`
	Available bool   `json:"available"`
	Default   bool   `json:"default"`
	Message   string `json:"message,omitempty"`
}

type LibraryRoots struct {
	Key        string     `json:"key"`
	Name       string     `json:"name"`
	Kind       string     `json:"kind"`
	Configured bool       `json:"configured"`
	Roots      []ScanRoot `json:"roots"`
	Message    string     `json:"message,omitempty"`
}

type RootsConfig struct {
	Revision  int            `json:"revision"`
	MediaRoot string         `json:"media_root"`
	Libraries []LibraryRoots `json:"libraries"`
}

type RootUpdate struct {
	Key   string   `json:"key"`
	Roots []string `json:"roots"`
}

type RootsUpdate struct {
	Revision  int          `json:"revision"`
	Libraries []RootUpdate `json:"libraries"`
}

var rootKeys = []string{"movies", "tv", "anime", "music"}

func rootKeySet() map[string]bool {
	out := map[string]bool{}
	for _, key := range rootKeys {
		out[key] = true
	}
	return out
}

func scanKind(key string, library Library) string {
	switch key {
	case "movies":
		return "movie"
	case "anime", "tv":
		return "series"
	case "music":
		return "music"
	}
	return library.Content
}

// DefaultScanRoot keeps the existing single-folder discovery behavior.
func DefaultScanRoot(l Library, kind string) string {
	if l.Key == "anime" {
		return l.Folder
	}
	if l.Content == kind {
		return l.Folder
	}
	if l.Content == "mixed" {
		if kind == "movie" {
			return filepath.ToSlash(filepath.Join(l.Folder, "Movies"))
		}
		if kind == "series" {
			return filepath.ToSlash(filepath.Join(l.Folder, "TV Shows"))
		}
	}
	return ""
}

func (s *Service) rootsState(ctx context.Context) (int, map[string][]string, error) {
	revision, raw := 1, "{}"
	err := s.db.QueryRowContext(ctx, `SELECT revision,roots FROM library_root_config WHERE id=1`).Scan(&revision, &raw)
	if err != nil && err != sql.ErrNoRows {
		return 0, nil, err
	}
	roots := map[string][]string{}
	if raw != "" && raw != "{}" {
		if err = json.Unmarshal([]byte(raw), &roots); err != nil {
			return 0, nil, err
		}
	}
	return revision, roots, nil
}

func (s *Service) rootEntry(path string, def bool) ScanRoot {
	available, message := s.rootAvailability(path)
	return ScanRoot{Path: path, Available: available, Default: def, Message: message}
}

func (s *Service) rootComponentsSafe(relative string) (bool, string) {
	parts := strings.Split(relative, "/")
	for i := range parts {
		info, err := os.Lstat(filepath.Join(s.cfg.MediaDir, filepath.FromSlash(strings.Join(parts[:i+1], "/"))))
		if err != nil {
			if os.IsNotExist(err) {
				return false, "This folder does not exist below the media root."
			}
			return false, "This folder cannot be inspected."
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return false, "Symbolic links cannot be used as library roots."
		}
		if i < len(parts)-1 && !info.IsDir() {
			return false, "A library root contains a non-folder path component."
		}
	}
	return true, ""
}

func (s *Service) rootAvailability(relative string) (bool, string) {
	if relative == "" || s.cfg.MediaDir == "" {
		return false, "No folder is configured."
	}
	if ok, message := s.rootComponentsSafe(relative); !ok {
		return false, message
	}
	abs := filepath.Join(s.cfg.MediaDir, filepath.FromSlash(relative))
	info, err := os.Lstat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return false, "This folder does not exist below the media root."
		}
		return false, "This folder cannot be inspected."
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return false, "Symbolic links cannot be used as library roots."
	}
	if !info.IsDir() {
		return false, "This path is not a folder."
	}
	resolvedMedia, err := filepath.EvalSymlinks(s.cfg.MediaDir)
	if err != nil {
		return false, "The configured media root is unavailable."
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil || !withinDir(resolvedMedia, resolved) {
		return false, "This folder resolves outside the media root."
	}
	return true, ""
}

// defaultMusicRoot resolves the actual on-disk folder name rather than guessing
// a case variant. On Linux Music and music are different directories.
func (s *Service) defaultMusicRoot() (string, string) {
	if s.cfg.MediaDir == "" {
		return "Music", "The configured media root is unavailable."
	}
	entries, err := os.ReadDir(s.cfg.MediaDir)
	if err != nil {
		return "Music", "The configured media root is unavailable."
	}
	for _, preferred := range []string{"Music", "music"} {
		for _, entry := range entries {
			if entry.Name() == preferred && entry.IsDir() && entry.Type()&os.ModeSymlink == 0 {
				return preferred, ""
			}
		}
	}
	matches := []string{}
	for _, entry := range entries {
		if strings.EqualFold(entry.Name(), "music") && entry.IsDir() && entry.Type()&os.ModeSymlink == 0 {
			matches = append(matches, entry.Name())
		}
	}
	if len(matches) == 1 {
		return matches[0], ""
	}
	return "Music", "No Music folder was found below the media root. Create one or add an existing music folder in library roots."
}

func (s *Service) GetRoots(ctx context.Context) (RootsConfig, error) {
	prefs, err := s.Get(ctx)
	if err != nil {
		return RootsConfig{}, err
	}
	revision, configured, err := s.rootsState(ctx)
	if err != nil {
		return RootsConfig{}, err
	}
	out := RootsConfig{Revision: revision, MediaRoot: s.cfg.MediaDir, Libraries: []LibraryRoots{}}
	for _, key := range rootKeys {
		var library Library
		found := false
		for _, candidate := range prefs.Libraries {
			if candidate.Key == key {
				library, found = candidate, true
				break
			}
		}
		if key != "music" && !found {
			continue
		}
		kind := scanKind(key, library)
		name := library.Name
		if key == "music" {
			name = "Music"
		}
		entry := LibraryRoots{Key: key, Name: name, Kind: kind}
		paths, explicit := configured[key]
		if explicit {
			entry.Configured = true
			entry.Roots = make([]ScanRoot, 0, len(paths))
			for _, p := range paths {
				entry.Roots = append(entry.Roots, s.rootEntry(p, false))
			}
		} else if key == "music" {
			path, message := s.defaultMusicRoot()
			entry.Roots = []ScanRoot{s.rootEntry(path, true)}
			entry.Message = message
		} else {
			path := DefaultScanRoot(library, kind)
			entry.Roots = []ScanRoot{s.rootEntry(path, true)}
		}
		if len(entry.Roots) == 0 {
			entry.Message = "No scan root is configured."
		}
		out.Libraries = append(out.Libraries, entry)
	}
	return out, nil
}

// EffectiveRoots returns the roots a scan should walk for a library. Unknown
// custom libraries retain their original single discovery folder.
func (s *Service) EffectiveRoots(ctx context.Context, key, kind string) ([]string, error) {
	prefs, err := s.Get(ctx)
	if err != nil {
		return nil, err
	}
	_, configured, err := s.rootsState(ctx)
	if err != nil {
		return nil, err
	}
	if key == "music" {
		if roots := configured[key]; len(roots) > 0 {
			return roots, nil
		}
		path, _ := s.defaultMusicRoot()
		return []string{path}, nil
	}
	var library Library
	found := false
	for _, candidate := range prefs.Libraries {
		if candidate.Key == key {
			library, found = candidate, true
			break
		}
	}
	if !found {
		return nil, problem("invalid_scan", "Choose a configured library.")
	}
	if roots := configured[key]; len(roots) > 0 {
		return roots, nil
	}
	path := DefaultScanRoot(library, kind)
	if path == "" {
		return nil, problem("invalid_scan", "Choose a configured collection compatible with movies or shows.")
	}
	return []string{path}, nil
}

func normalizeRoot(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	for len(value) > 1 && strings.HasSuffix(value, "/") {
		value = strings.TrimSuffix(value, "/")
	}
	return value
}

func withinDir(base, target string) bool {
	rel, err := filepath.Rel(base, target)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func (s *Service) validateRoot(value string) (string, error) {
	value = normalizeRoot(value)
	if !validRelative(value) {
		return "", problem("invalid_roots", "Use safe relative folders below the media root. Absolute paths, .. and special characters are not allowed.")
	}
	abs := filepath.Join(s.cfg.MediaDir, filepath.FromSlash(value))
	if !withinDir(s.cfg.MediaDir, abs) {
		return "", problem("invalid_roots", "Every scan root must stay below the media root.")
	}
	if ok, message := s.rootComponentsSafe(value); !ok {
		return "", problem("invalid_roots", message)
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return "", problem("invalid_roots", "Scan roots must be existing folders below the media root.")
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", problem("invalid_roots", "Scan roots cannot be symbolic links.")
	}
	if !info.IsDir() {
		return "", problem("invalid_roots", "Every scan root must be a folder.")
	}
	resolvedMedia, err := filepath.EvalSymlinks(s.cfg.MediaDir)
	if err != nil {
		return "", problem("storage_unavailable", "The configured media root is unavailable.")
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil || !withinDir(resolvedMedia, resolved) {
		return "", problem("invalid_roots", "A scan root resolves outside the media root or contains a symbolic link.")
	}
	return value, nil
}

func rootsOverlap(a, b string) bool {
	a, b = strings.ToLower(a), strings.ToLower(b)
	return a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/")
}

func (s *Service) validateRootSet(paths []string) ([]string, error) {
	if len(paths) == 0 || len(paths) > 20 {
		return nil, problem("invalid_roots", "Each library needs between 1 and 20 scan roots.")
	}
	out := make([]string, 0, len(paths))
	for _, value := range paths {
		clean, err := s.validateRoot(value)
		if err != nil {
			return nil, err
		}
		for _, existing := range out {
			if rootsOverlap(clean, existing) {
				return nil, problem("invalid_roots", "Scan roots in one library must be separate, non-overlapping folders.")
			}
		}
		out = append(out, clean)
	}
	return out, nil
}

func (s *Service) UpdateRoots(ctx context.Context, in RootsUpdate) (RootsConfig, error) {
	if in.Revision < 1 || len(in.Libraries) == 0 || len(in.Libraries) > 20 {
		return RootsConfig{}, problem("invalid_roots", "Use the current library-root revision and at least one library.")
	}
	prefs, err := s.Get(ctx)
	if err != nil {
		return RootsConfig{}, err
	}
	allowed := rootKeySet()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return RootsConfig{}, err
	}
	defer tx.Rollback()
	var revision int
	var raw string
	if err = tx.QueryRowContext(ctx, `SELECT revision,roots FROM library_root_config WHERE id=1`).Scan(&revision, &raw); err != nil {
		return RootsConfig{}, err
	}
	if revision != in.Revision {
		return RootsConfig{}, problem("revision_conflict", "Library roots changed. Reload before saving.")
	}
	merged := map[string][]string{}
	if raw != "" && raw != "{}" {
		if err = json.Unmarshal([]byte(raw), &merged); err != nil {
			return RootsConfig{}, err
		}
	}
	seen := map[string]bool{}
	for _, set := range in.Libraries {
		key := strings.TrimSpace(set.Key)
		if !allowed[key] || seen[key] {
			return RootsConfig{}, problem("invalid_roots", "Choose each built-in library or Music once.")
		}
		seen[key] = true
		paths, err := s.validateRootSet(set.Roots)
		if err != nil {
			return RootsConfig{}, err
		}
		merged[key] = paths
	}
	// Compare every effective root so two libraries cannot scan the same tree.
	effective := map[string][]string{}
	for _, key := range rootKeys {
		paths := merged[key]
		if len(paths) == 0 {
			if key == "music" {
				path, _ := s.defaultMusicRoot()
				paths = []string{path}
			} else {
				var library Library
				for _, candidate := range prefs.Libraries {
					if candidate.Key == key {
						library = candidate
						break
					}
				}
				if library.Key != "" {
					paths = []string{DefaultScanRoot(library, scanKind(key, library))}
				}
			}
		}
		effective[key] = paths
	}
	for i, a := range rootKeys {
		for _, pathA := range effective[a] {
			for _, b := range rootKeys[i+1:] {
				for _, pathB := range effective[b] {
					if rootsOverlap(pathA, pathB) {
						return RootsConfig{}, problem("invalid_roots", "Library roots must not overlap between libraries.")
					}
				}
			}
		}
	}
	encoded, err := json.Marshal(merged)
	if err != nil {
		return RootsConfig{}, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE library_root_config SET revision=revision+1,roots=? WHERE id=1 AND revision=?`, string(encoded), in.Revision)
	if err != nil {
		return RootsConfig{}, err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return RootsConfig{}, problem("revision_conflict", "Library roots changed. Reload before saving.")
	}
	if err = tx.Commit(); err != nil {
		return RootsConfig{}, err
	}
	return s.GetRoots(ctx)
}
