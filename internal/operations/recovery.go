package operations

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"

	"github.com/faaaaq/ReelScarey/internal/instance"
)

func (s *Service) Resume(ctx context.Context, reviewed bool) error {
	if !reviewed {
		return errors.New("review the restored settings, download activity and storage paths before resuming workers")
	}
	if !instance.RecoveryPending(s.cfg.ConfigDir) {
		return nil
	}
	report, e := s.Run(ctx)
	if e != nil {
		return e
	}
	for _, check := range report.Checks {
		if (check.ID == "database" || check.ID == "secrets" || check.ID == "paths") && check.State != "ready" {
			return errors.New("database, credentials and separate storage roots must pass their checks before workers resume")
		}
	}
	root, e := os.OpenRoot(s.cfg.ConfigDir)
	if e != nil {
		return e
	}
	defer root.Close()
	before, e := root.Lstat("recovery-mode")
	if e != nil {
		return e
	}
	if !before.Mode().IsRegular() || before.Size() > 128 {
		return errors.New("recovery marker is invalid; use a verified configuration restore")
	}
	f, e := root.Open("recovery-mode")
	if e != nil {
		return e
	}
	info, e := f.Stat()
	if e != nil || !os.SameFile(before, info) {
		f.Close()
		return errors.New("recovery marker changed")
	}
	var marker struct {
		Format int `json:"format"`
	}
	decoder := json.NewDecoder(io.LimitReader(f, 129))
	decoder.DisallowUnknownFields()
	e = decoder.Decode(&marker)
	extra := decoder.Decode(&struct{}{})
	f.Close()
	if e != nil || extra != io.EOF || marker.Format != 1 {
		return errors.New("recovery marker is invalid; use a verified configuration restore")
	}
	visible, e := root.Lstat("recovery-mode")
	if e != nil || !os.SameFile(visible, info) {
		return errors.New("recovery marker changed")
	}
	return root.Remove("recovery-mode")
}
