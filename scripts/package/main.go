// Builds private installation packages from the already-built embedded UI.
package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func run() error {
	if len(os.Args) != 3 || !regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:-[a-zA-Z0-9.]+)?$`).MatchString(os.Args[1]) {
		return fmt.Errorf("usage: go run ./scripts/package VERSION NEW_OUTPUT_FOLDER")
	}
	version, out := os.Args[1], os.Args[2]
	docs := []string{"README.md", "ARCHITECTURE.md", "ROADMAP.md", "AGENTS.md", "docs/OPERATIONS.md", "docs/SECURITY.md", "docs/API.md", "docs/AUTHENTICATION.md", "docs/MOVIE_WORKFLOW.md", "docs/TELEVISION.md", "docs/TV_DOWNLOADS.md", "docs/AUTOMATION.md", "docs/SUBTITLES.md", "docs/VALIDATION.md", "docs/INDEXERS_DETAILS.md", "docs/EXISTING_LIBRARY.md", "docs/USENET.md", "docs/PLANNING.md", "docs/LIBRARY_BROWSING.md", "docs/BULK_MONITORING.md", "go.mod", "go.sum", "web/package.json", "web/package-lock.json"}
	if _, e := os.Stat("web/dist/index.html"); e != nil {
		return fmt.Errorf("build the frontend before packaging")
	}
	for _, name := range docs {
		if _, e := os.Stat(name); e != nil {
			return e
		}
	}
	if e := os.MkdirAll(filepath.Dir(out), 0700); e != nil {
		return e
	}
	if e := os.Mkdir(out, 0700); e != nil {
		return fmt.Errorf("output folder must be new: %w", e)
	}
	stage, e := os.MkdirTemp("", "reelscarey-package-")
	if e != nil {
		return e
	}
	defer func() {
		absolute, e := filepath.Abs(stage)
		parent, pe := filepath.EvalSymlinks(filepath.Dir(absolute))
		temp, te := filepath.EvalSymlinks(os.TempDir())
		info, ie := os.Lstat(absolute)
		if e == nil && pe == nil && te == nil && ie == nil && parent == temp && info.IsDir() && info.Mode()&os.ModeSymlink == 0 && strings.HasPrefix(filepath.Base(absolute), "reelscarey-package-") {
			_ = os.RemoveAll(absolute)
		}
	}()
	sums := []string{}
	for _, target := range []struct{ goos, arch string }{{"linux", "amd64"}, {"linux", "arm64"}, {"windows", "amd64"}} {
		binary := "reelscarey"
		if target.goos == "windows" {
			binary += ".exe"
		}
		destination := filepath.Join(stage, binary)
		cmd := exec.Command("go", "build", "-trimpath", "-ldflags", "-s -w -X main.version="+version, "-o", destination, "./cmd/reelscarey")
		for _, v := range os.Environ() {
			if !strings.HasPrefix(v, "GOOS=") && !strings.HasPrefix(v, "GOARCH=") && !strings.HasPrefix(v, "CGO_ENABLED=") {
				cmd.Env = append(cmd.Env, v)
			}
		}
		cmd.Env = append(cmd.Env, "GOOS="+target.goos, "GOARCH="+target.arch, "CGO_ENABLED=0")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if e = cmd.Run(); e != nil {
			return e
		}
		name := "reelscarey-" + version + "-" + target.goos + "-" + target.arch
		files := []entry{{destination, binary, 0755}}
		for _, doc := range docs {
			files = append(files, entry{doc, filepath.ToSlash(doc), 0644})
		}
		if target.goos == "windows" {
			name += ".zip"
			e = writeZip(filepath.Join(out, name), files)
		} else {
			name += ".tar.gz"
			e = writeTar(filepath.Join(out, name), files)
		}
		if e != nil {
			return e
		}
		f, e := os.Open(filepath.Join(out, name))
		if e != nil {
			return e
		}
		h := sha256.New()
		_, e = io.Copy(h, f)
		f.Close()
		if e != nil {
			return e
		}
		sums = append(sums, fmt.Sprintf("%x  %s", h.Sum(nil), name))
		fmt.Println("Packaged", name)
	}
	return os.WriteFile(filepath.Join(out, "SHA256SUMS"), []byte(strings.Join(sums, "\n")+"\n"), 0600)
}

type entry struct {
	source, name string
	mode         int64
}

func writeZip(dest string, files []entry) (err error) {
	f, e := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	defer func() { err = join(err, f.Close()) }()
	w := zip.NewWriter(f)
	defer func() { err = join(err, w.Close()) }()
	for _, v := range files {
		h := &zip.FileHeader{Name: v.name, Method: zip.Deflate}
		h.SetMode(os.FileMode(v.mode))
		out, e := w.CreateHeader(h)
		if e != nil {
			return e
		}
		if e = copyEntry(out, v.source); e != nil {
			return e
		}
	}
	return nil
}
func writeTar(dest string, files []entry) (err error) {
	f, e := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	defer func() { err = join(err, f.Close()) }()
	gz := gzip.NewWriter(f)
	defer func() { err = join(err, gz.Close()) }()
	w := tar.NewWriter(gz)
	defer func() { err = join(err, w.Close()) }()
	for _, v := range files {
		info, e := os.Stat(v.source)
		if e != nil {
			return e
		}
		if e = w.WriteHeader(&tar.Header{Name: v.name, Mode: v.mode, Size: info.Size(), Typeflag: tar.TypeReg}); e != nil {
			return e
		}
		if e = copyEntry(w, v.source); e != nil {
			return e
		}
	}
	return nil
}
func copyEntry(w io.Writer, name string) error {
	f, e := os.Open(name)
	if e != nil {
		return e
	}
	defer f.Close()
	_, e = io.Copy(w, f)
	return e
}
func join(a, b error) error {
	if a != nil {
		return a
	}
	return b
}
