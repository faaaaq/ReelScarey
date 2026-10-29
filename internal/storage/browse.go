// Package storage provides bounded read-only folder inspection. It never
// registers files or changes the administrator's filesystem.
package storage

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/faaaaq/ReelScarey/internal/config"
)

var ErrPath = errors.New("Choose an available mounted folder without links or protected configuration paths.")
var ErrLimit = errors.New("This folder exceeds the 10000-entry browsing limit; choose a smaller folder.")

type Root struct {
	ID   string `json:"id"`
	Path string `json:"path"`
}
type Entry struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Kind     string `json:"kind"`
	Size     int64  `json:"size_bytes"`
	Modified int64  `json:"modified_at"`
}
type Page struct {
	Roots []Root  `json:"roots"`
	Root  string  `json:"root"`
	Path  string  `json:"path"`
	Items []Entry `json:"items"`
	Page  int     `json:"page"`
	Total int     `json:"total"`
	Limit int     `json:"limit"`
}

func Overlap(a, b string) bool {
	r, e := filepath.Rel(a, b)
	return e == nil && (r == "." || filepath.IsLocal(r))
}

// Open refuses every symlink component, including the selected leaf. os.Root
// confines subsequent opens if a directory is concurrently renamed.
func Open(root *os.Root, relative string) (*os.File, error) {
	if relative == "" || relative == "." {
		return root.Open(".")
	}
	if !fs.ValidPath(relative) || strings.Contains(relative, "\\") || len(relative) > 2048 {
		return nil, ErrPath
	}
	current := ""
	for _, part := range strings.Split(relative, "/") {
		current = path.Join(current, part)
		info, e := root.Lstat(filepath.FromSlash(current))
		if e != nil || info.Mode()&os.ModeSymlink != 0 {
			return nil, ErrPath
		}
	}
	return root.Open(filepath.FromSlash(relative))
}
func canonical(dir string) (string, error) {
	if dir == "" {
		return "", ErrPath
	}
	absolute, e := filepath.Abs(dir)
	if e != nil {
		return "", ErrPath
	}
	resolved, e := filepath.EvalSymlinks(absolute)
	if e != nil || resolved != absolute || filepath.Dir(resolved) == resolved {
		return "", ErrPath
	}
	info, e := os.Lstat(resolved)
	if e != nil || !info.IsDir() {
		return "", ErrPath
	}
	return resolved, nil
}
func Browse(ctx context.Context, cfg config.Config, id, relative string, page int) (Page, error) {
	out := Page{Roots: []Root{}, Root: id, Path: relative, Page: page, Limit: 100, Items: []Entry{}}
	if page < 1 || page > 100 || relative == "." {
		return out, ErrPath
	}
	for _, part := range strings.Split(relative, "/") {
		if strings.HasPrefix(part, ".") {
			return out, ErrPath
		}
	}
	roots := map[string]string{"media": cfg.MediaDir, "downloads": cfg.DownloadsDir}
	for name, dir := range cfg.BrowseRoots {
		roots[name] = dir
	}
	for name, dir := range roots {
		out.Roots = append(out.Roots, Root{name, dir})
	}
	sort.Slice(out.Roots, func(i, j int) bool { return out.Roots[i].ID < out.Roots[j].ID })
	if id == "" {
		id = "media"
		out.Root = id
	}
	selected, ok := roots[id]
	if !ok {
		return out, ErrPath
	}
	selected, e := canonical(selected)
	if e != nil {
		return out, e
	}
	for _, private := range []string{cfg.ConfigDir, cfg.BackupsDir} {
		if private == "" {
			continue
		}
		absolute, e := filepath.Abs(private)
		if e != nil {
			return out, ErrPath
		}
		if p, e := filepath.EvalSymlinks(absolute); e == nil {
			absolute = p
		}
		if Overlap(selected, absolute) || Overlap(absolute, selected) {
			return out, ErrPath
		}
	}
	root, e := os.OpenRoot(selected)
	if e != nil {
		return out, ErrPath
	}
	defer root.Close()
	dir, e := Open(root, relative)
	if e != nil {
		return out, e
	}
	defer dir.Close()
	before, e := dir.Stat()
	if e != nil || !before.IsDir() {
		return out, ErrPath
	}
	entries := []Entry{}
	examined := 0
	for {
		if e = ctx.Err(); e != nil {
			return out, e
		}
		items, re := dir.ReadDir(128)
		for _, v := range items {
			examined++
			if examined > 10000 {
				return out, ErrLimit
			}
			if strings.HasPrefix(v.Name(), ".") {
				continue
			}
			info, e := root.Lstat(filepath.FromSlash(path.Join(relative, v.Name())))
			if e != nil {
				return out, ErrPath
			}
			kind := "blocked"
			if info.IsDir() {
				kind = "directory"
			} else if info.Mode().IsRegular() {
				kind = "file"
			}
			entries = append(entries, Entry{v.Name(), path.Join(relative, v.Name()), kind, info.Size(), info.ModTime().Unix()})
		}
		if re == io.EOF {
			break
		}
		if re != nil {
			return out, ErrPath
		}
	}
	after, e := dir.Stat()
	visible, ve := root.Lstat(filepath.FromSlash(path.Join(relative, ".")))
	if e != nil || ve != nil || !visible.IsDir() || !os.SameFile(before, visible) || before.ModTime() != after.ModTime() {
		return out, ErrPath
	}
	sort.Slice(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if (a.Kind == "directory") != (b.Kind == "directory") {
			return a.Kind == "directory"
		}
		if strings.ToLower(a.Name) != strings.ToLower(b.Name) {
			return strings.ToLower(a.Name) < strings.ToLower(b.Name)
		}
		return a.Name < b.Name
	})
	out.Total = len(entries)
	start := min((page-1)*out.Limit, len(entries))
	out.Items = entries[start:min(start+out.Limit, len(entries))]
	return out, nil
}
