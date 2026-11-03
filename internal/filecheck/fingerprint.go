// Package filecheck records bounded observations of existing files. Observations
// are change detection, not full-content checksums. Mutating media workflows must
// promote them to a full SHA-256 before creating their durable operation plans.
package filecheck

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const sampleSize int64 = 64 << 10
const FullSize = 3 * sampleSize

var ErrChanged = errors.New("the registered file changed since it was catalogued")

type Snapshot struct {
	Identity string `json:"identity"`
	Change   string `json:"change"`
	Size     int64  `json:"size"`
	Mtime    int64  `json:"mtime"`
	Sample   string `json:"sample"`
}

type reader struct {
	ctx context.Context
	r   io.Reader
}

func (r reader) Read(b []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(b)
}
func Digest(ctx context.Context, f *os.File) (string, error) {
	h := sha256.New()
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	defer f.Seek(0, io.SeekStart)
	if _, err := io.Copy(h, reader{ctx, f}); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func Capture(ctx context.Context, f *os.File) (Snapshot, error) {
	var out Snapshot
	info, err := f.Stat()
	if err != nil {
		return out, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 {
		return out, ErrChanged
	}
	out.Identity, out.Change, err = stamp(f)
	if err != nil {
		return out, err
	}
	out.Size, out.Mtime = info.Size(), info.ModTime().UnixNano()
	h := sha256.New()
	// Small files get their actual full digest, preserving existing semantics.
	if out.Size <= FullSize {
		if _, err = io.Copy(h, reader{ctx, io.NewSectionReader(f, 0, out.Size)}); err != nil {
			return out, err
		}
	} else {
		fmt.Fprintf(h, "sample-v1:%d:", out.Size)
		for _, offset := range []int64{0, (out.Size - sampleSize) / 2, out.Size - sampleSize} {
			fmt.Fprintf(h, "%d:", offset)
			if _, err = io.CopyN(h, reader{ctx, io.NewSectionReader(f, offset, sampleSize)}, sampleSize); err != nil {
				return out, err
			}
		}
	}
	out.Sample = hex.EncodeToString(h.Sum(nil))
	after, err := f.Stat()
	if err != nil {
		return out, err
	}
	id, change, err := stamp(f)
	if err != nil {
		return out, err
	}
	if !os.SameFile(info, after) || after.Size() != out.Size || after.ModTime().UnixNano() != out.Mtime || id != out.Identity || change != out.Change {
		return out, ErrChanged
	}
	return out, nil
}
func Verify(ctx context.Context, f *os.File, raw string) error {
	var expected Snapshot
	if json.Unmarshal([]byte(raw), &expected) != nil || expected.Identity == "" || len(expected.Sample) != 64 {
		return ErrChanged
	}
	got, err := Capture(ctx, f)
	if err != nil {
		return err
	}
	if got != expected {
		return ErrChanged
	}
	return nil
}

// Open anchors every component and rejects symbolic links and special files.
func Open(root *os.Root, name string) (*os.File, error) {
	if !filepath.IsLocal(filepath.FromSlash(name)) || strings.ContainsAny(name, "\\:\x00\r\n") {
		return nil, ErrChanged
	}
	parts := strings.Split(name, "/")
	for i, part := range parts {
		if part == "" || part == "." || part == ".." {
			return nil, ErrChanged
		}
		info, err := root.Lstat(filepath.FromSlash(strings.Join(parts[:i+1], "/")))
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 || i < len(parts)-1 && !info.IsDir() || i == len(parts)-1 && !info.Mode().IsRegular() {
			return nil, ErrChanged
		}
	}
	f, err := open(root, filepath.FromSlash(name))
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	visible, e := root.Lstat(filepath.FromSlash(name))
	if err != nil || e != nil || !info.Mode().IsRegular() || !os.SameFile(info, visible) {
		f.Close()
		return nil, ErrChanged
	}
	return f, nil
}

// EnsureSHA is called only from explicit maintenance/organization operations,
// while their media gate is held. No database transaction spans the full read.
// Existing verified checksums are left untouched. Pending acquisition reservations
// keep their binding when this same file gains its first full-content checksum.
func EnsureSHA(ctx context.Context, db *sql.DB, root *os.Root, id int64) (string, error) {
	var name, sha, raw string
	var size int64
	if err := db.QueryRowContext(ctx, `SELECT relative_path,size_bytes,sha256,verification_json FROM media_files WHERE id=?`, id).Scan(&name, &size, &sha, &raw); err != nil {
		return "", err
	}
	if sha != "" {
		return sha, nil
	}
	f, err := Open(root, name)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if err = Verify(ctx, f, raw); err != nil {
		return "", err
	}
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	if info.Size() != size {
		return "", ErrChanged
	}
	sha, err = Digest(ctx, f)
	if err != nil {
		return "", err
	}
	if err = Verify(ctx, f, raw); err != nil {
		return "", err
	}
	visible, err := Open(root, name)
	if err != nil {
		return "", err
	}
	defer visible.Close()
	if err = Verify(ctx, visible, raw); err != nil {
		return "", err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if err = Verify(ctx, visible, raw); err != nil {
		return "", err
	}
	result, err := tx.ExecContext(ctx, `UPDATE media_files SET sha256=?,verification_json='' WHERE id=? AND relative_path=? AND size_bytes=? AND sha256='' AND verification_json=?`, sha, id, name, size, raw)
	if err != nil {
		return "", err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return "", err
	}
	if n != 1 {
		var current string
		if err = tx.QueryRowContext(ctx, `SELECT sha256 FROM media_files WHERE id=? AND relative_path=? AND size_bytes=?`, id, name, size).Scan(&current); err != nil {
			return "", err
		}
		if current != sha {
			return "", ErrChanged
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE acquisition_replacements SET target_sha=? WHERE media_file_id=? AND target_sha=''`, sha, id); err != nil {
		return "", err
	}
	return sha, tx.Commit()
}
