// Package storage provides bounded read-only folder inspection. It never
// registers files or changes the administrator's filesystem.
package storage

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
)

// Regular opens a regular file under root, verifying it is a regular file
// within the size limit and that it does not change during open.
func Regular(root *os.Root, name string, max int64) (*os.File, error) {
	before, e := root.Lstat(name)
	if e != nil {
		return nil, e
	}
	if !before.Mode().IsRegular() || before.Size() > max {
		return nil, errors.New("file must be a supported regular file")
	}
	f, e := root.Open(name)
	if e != nil {
		return nil, e
	}
	info, e := f.Stat()
	if e != nil || !os.SameFile(before, info) {
		f.Close()
		return nil, errors.New("file changed during inspection")
	}
	return f, nil
}

// WriteJSON creates an exclusive file, encodes v as JSON, syncs, and closes.
func WriteJSON(root *os.Root, name string, v any) error {
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

// SyncDir syncs the directory itself. On Windows the call is a no-op.
func SyncDir(root *os.Root) error {
	f, e := root.Open(".")
	if e != nil {
		return e
	}
	defer f.Close()
	e = f.Sync()
	if runtime.GOOS == "windows" {
		return nil
	}
	return e
}

// NewID returns a 32-hex-char random identifier. crypto/rand.Read never
// returns an error as of Go 1.24, so the caller never sees a panic.
func NewID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// Overlap reports whether the two paths refer to overlapping content, meaning
// one is an ancestor of the other or they refer to the same path.
func Overlap(a, b string) bool {
	r, e := filepath.Rel(a, b)
	return e == nil && (r == "." || filepath.IsLocal(r))
}