package organization

import (
	"context"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// folderInventory is a bounded, read-only snapshot. It is not itself authority
// to move anything: a future reviewed move must persist and revalidate it.
type folderInventory struct {
	Directories []folderDirectory `json:"directories"`
	Files       []Companion       `json:"files"`
}

type folderDirectory struct {
	Path     string `json:"path"`
	Identity string `json:"identity"`
}

// titleFolder identifies a single title directory below one configured
// collection's content leaf. It never interprets a collection root itself as
// a title folder, and refuses titles whose registered files span directories.
func titleFolder(c Config, kind string, files []string) (string, error) {
	if len(files) == 0 {
		return "", problem("invalid_selection", "A title needs registered files before its folder can be reviewed.")
	}
	folder := ""
	for _, name := range files {
		found := ""
		for _, library := range c.Libraries {
			if library.Content != "mixed" && library.Content != kind {
				continue
			}
			prefix := leaf(library, kind) + "/"
			if !strings.HasPrefix(name, prefix) {
				continue
			}
			rest := strings.TrimPrefix(name, prefix)
			parts := strings.Split(rest, "/")
			if len(parts) < 2 || parts[0] == "" {
				continue
			}
			candidate := path.Join(strings.TrimSuffix(prefix, "/"), parts[0])
			if found != "" && found != candidate {
				return "", problem("unsafe_path", "The title folder is ambiguous.")
			}
			found = candidate
		}
		if found == "" || !validRelative(found) || folder != "" && folder != found {
			return "", problem("invalid_selection", "Registered files must share one title folder inside a configured collection.")
		}
		folder = found
	}
	return folder, nil
}

func inventoryFolder(ctx context.Context, root *os.Root, q query, folder string, mediaID int64) (folderInventory, error) {
	var out folderInventory
	if mediaID < 1 || !validManagedRelative(folder) {
		return out, problem("unsafe_path", "Choose a safe title folder inside a configured collection.")
	}
	if err := segments(root, folder, false); err != nil {
		return out, err
	}
	// Compare paths as path components, not SQL LIKE patterns, so %, _ and
	// case differences cannot hide another registered title.
	rows, err := q.QueryContext(ctx, `SELECT relative_path,COALESCE(movie_id,series_id) FROM media_files`)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var name string
		var owner int64
		if err = rows.Scan(&name, &owner); err != nil {
			rows.Close()
			return out, err
		}
		if strings.HasPrefix(strings.ToLower(name), strings.ToLower(folder)+"/") && owner != mediaID {
			rows.Close()
			return out, problem("shared_folder", "This folder contains files registered to another title.")
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	rows, err = q.QueryContext(ctx, `SELECT yf.relative_path,COALESCE(m.movie_id,m.series_id,0) FROM youtube_files yf LEFT JOIN media_files m ON m.id=yf.media_file_id UNION ALL SELECT sf.relative_path,COALESCE(m.movie_id,m.series_id,0) FROM youtube_subtitle_files sf JOIN youtube_files yf ON yf.job_id=sf.job_id LEFT JOIN media_files m ON m.id=yf.media_file_id UNION ALL SELECT s.relative_path,COALESCE(m.movie_id,m.series_id,0) FROM subtitles s JOIN media_files m ON m.id=s.media_file_id WHERE s.relative_path<>'' UNION ALL SELECT i.destination,COALESCE(m.movie_id,m.series_id,0) FROM subtitle_imports i JOIN media_files m ON m.id=i.media_file_id WHERE i.phase='committed'`)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var name string
		var owner int64
		if err = rows.Scan(&name, &owner); err != nil {
			rows.Close()
			return out, err
		}
		if strings.HasPrefix(strings.ToLower(name), strings.ToLower(folder)+"/") && owner != mediaID {
			rows.Close()
			return out, problem("shared_folder", "This folder contains YouTube media belonging to another collection or title.")
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	var walk func(string, int) error
	walk = func(dir string, depth int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if depth > 20 || len(out.Directories)+len(out.Files) >= 2000 {
			return problem("folder_too_large", "The folder exceeds the review limit.")
		}
		if err := segments(root, dir, false); err != nil {
			return err
		}
		f, err := root.Open(filepath.FromSlash(dir))
		if err != nil {
			return err
		}
		info, err := f.Stat()
		if err != nil {
			f.Close()
			return err
		}
		if !info.IsDir() {
			f.Close()
			return problem("unsafe_path", "The reviewed title folder must be a directory.")
		}
		directoryID, err := identity(f)
		if err != nil {
			f.Close()
			return err
		}
		before, err := root.Lstat(filepath.FromSlash(dir))
		if err != nil || !before.IsDir() || !os.SameFile(info, before) {
			f.Close()
			return problem("source_changed", "A reviewed directory was replaced.")
		}
		names, err := f.Readdirnames(2001)
		f.Close()
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		if len(names) > 2000 {
			return problem("folder_too_large", "The folder exceeds the review limit.")
		}
		sort.Strings(names)
		out.Directories = append(out.Directories, folderDirectory{Path: dir, Identity: directoryID})
		for _, name := range names {
			if err := ctx.Err(); err != nil {
				return err
			}
			child := path.Join(dir, name)
			if !validManagedRelative(child) || internalStageRE.MatchString(name) || internalFolderStageRE.MatchString(name) {
				return problem("unsafe_path", "The folder contains an unsafe or pending staging path.")
			}
			if len(out.Directories)+len(out.Files) >= 2000 {
				return problem("folder_too_large", "The folder exceeds the review limit.")
			}
			info, err := root.Lstat(filepath.FromSlash(child))
			if err != nil {
				return err
			}
			if info.IsDir() {
				if err = walk(child, depth+1); err != nil {
					return err
				}
				continue
			}
			if !info.Mode().IsRegular() {
				return problem("unsafe_path", "The folder contains a symbolic link or non-regular file.")
			}
			v, err := inspect(root, child)
			if err != nil {
				return err
			}
			file, err := root.Open(filepath.FromSlash(child))
			if err != nil {
				return err
			}
			actual, err := identity(file)
			if err == nil && actual != v.Identity {
				err = problem("source_changed", "A reviewed file was replaced.")
			}
			if err == nil {
				v.SHA, err = hash(ctx, file)
			}
			file.Close()
			if err != nil {
				return err
			}
			if err = checkSource(ctx, root, v, false); err != nil {
				return err
			}
			out.Files = append(out.Files, v)
		}
		return nil
	}
	if err = walk(folder, 0); err != nil {
		return folderInventory{}, err
	}
	return out, nil
}
