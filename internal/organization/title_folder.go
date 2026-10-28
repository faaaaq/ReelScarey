package organization

import (
	"context"
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// CreateTitleFolder creates only the configured destination directory; it does
// not move, register, overwrite or remove media.
func (s *Service) CreateTitleFolder(ctx context.Context, id int64) (string, error) {
	defer ReadMedia()()
	if err := EnsureMediaAvailable(ctx, s.db); err != nil {
		return "", err
	}
	snapshot, err := s.Capture(ctx, id)
	if err != nil {
		return "", err
	}
	var dir string
	if snapshot.Content.Kind == "movie" {
		destination, e := Destination(snapshot, nil, "placeholder.mkv")
		if e != nil {
			return "", e
		}
		dir = path.Dir(destination)
	} else if snapshot.Library.Preset == "legacy" {
		dir = path.Join(leaf(snapshot.Library, "series"), legacyName(snapshot.Content))
	} else {
		name, e := render(snapshot.Library.SeriesTemplate, snapshot.Content, nil)
		if e != nil {
			return "", e
		}
		dir = path.Join(leaf(snapshot.Library, "series"), name)
	}
	base := leaf(snapshot.Library, snapshot.Content.Kind)
	if !validRelative(dir) || dir == base || !strings.HasPrefix(dir, base+"/") {
		return "", problem("invalid_template", "This naming template does not provide a separate title folder. Update the library naming settings first.")
	}
	root, resolved, err := s.openRoot("")
	if err != nil {
		return "", err
	}
	defer root.Close()
	// Check each existing component and create one directory at a time.
	parts := strings.Split(dir, "/")
	for i := range parts {
		name := strings.Join(parts[:i+1], "/")
		if err = ctx.Err(); err != nil {
			return "", err
		}
		info, e := root.Lstat(filepath.FromSlash(name))
		if errors.Is(e, os.ErrNotExist) {
			if exists, checkErr := existsFolded(root, name); checkErr != nil {
				return "", checkErr
			} else if exists {
				return "", problem("destination_exists", "A folder with the same name but different capitalization already exists. Review the library before creating this folder.")
			}
			if e = root.Mkdir(filepath.FromSlash(name), 0755); e != nil && !errors.Is(e, os.ErrExist) {
				return "", problem("storage_unavailable", "The destination folder could not be created. Check media mount permissions.")
			}
			info, e = root.Lstat(filepath.FromSlash(name))
		}
		if e != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", problem("unsafe_path", "The destination contains a symbolic link or is not a directory.")
		}
	}
	return filepath.Join(resolved, filepath.FromSlash(dir)), nil
}
