// Package instance prevents two servers or offline maintenance commands from
// owning one config directory. Locks are released by the OS after a crash.
package instance

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type Lock struct{ file *os.File }

func Acquire(dir string) (*Lock, error) {
	return acquire(dir, ".instance.lock", false, "another ReelScarey server is already using this config directory; stop that server or container before starting this one")
}

// Guard shares access for live administration, excluding offline maintenance.
func Guard(dir string, shared bool) (*Lock, error) {
	return acquire(dir, ".maintenance.lock", shared, "this config directory is in use; stop its ReelScarey server before offline maintenance")
}
func acquire(dir, name string, shared bool, busyMessage string) (*Lock, error) {
	if e := os.MkdirAll(dir, 0700); e != nil {
		return nil, fmt.Errorf("config directory is unavailable")
	}
	root, e := os.OpenRoot(dir)
	if e != nil {
		return nil, e
	}
	defer root.Close()
	if info, e := root.Lstat(name); e == nil && !info.Mode().IsRegular() {
		return nil, errors.New("config lock must be a regular file")
	} else if e != nil && !errors.Is(e, os.ErrNotExist) {
		return nil, errors.New("config lock is unavailable")
	}
	f, e := root.OpenFile(name, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, errors.New("config lock is unavailable")
	}
	info, e := f.Stat()
	visible, ve := root.Lstat(name)
	if e != nil || ve != nil || !visible.Mode().IsRegular() || !os.SameFile(info, visible) {
		f.Close()
		return nil, errors.New("config lock changed during access")
	}
	if e = lock(f, shared); e != nil {
		f.Close()
		return nil, errors.New(busyMessage)
	}
	return &Lock{f}, nil
}
func (l *Lock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	e := unlock(l.file)
	ce := l.file.Close()
	l.file = nil
	return errors.Join(e, ce)
}
func CheckStart(dir string) error {
	if e := CheckMaintenanceStart(dir); e != nil {
		return e
	}
	if _, e := os.Lstat(filepath.Join(dir, ".media-root-copy-in-progress")); !errors.Is(e, os.ErrNotExist) {
		return errors.New("a media-root migration is pending; finish media-root-activate or media-root-cancel offline before starting")
	}
	return nil
}

// CheckMaintenanceStart excludes restore/backup switches while allowing the
// migration command that owns the media-root recovery marker to resume.
func CheckMaintenanceStart(dir string) error {
	for _, name := range []string{".restore-in-progress", ".restore-request.json", ".backup-in-progress", "backup-manifest.json"} {
		if _, e := os.Lstat(filepath.Join(dir, name)); e == nil {
			return errors.New("config directory contains a backup or pending restore; finish the restore through the server or use verified offline recovery")
		} else if !errors.Is(e, os.ErrNotExist) {
			return errors.New("config directory could not be checked")
		}
	}
	return nil
}

// Any inspection failure keeps workers paused. This file is created only when
// restoring into a new config, never by the normal running server.
func RecoveryPending(dir string) bool {
	_, e := os.Lstat(filepath.Join(dir, "recovery-mode"))
	return !errors.Is(e, os.ErrNotExist)
}
