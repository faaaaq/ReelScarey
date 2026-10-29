package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/faaaaq/ReelScarey/internal/backups"
)

func (s *Server) backupAvailable(w http.ResponseWriter) bool {
	w.Header().Set("Cache-Control", "no-store")
	if s.backups == nil {
		fail(w, 503, "backups_unavailable", "Backup storage is unavailable in this installation.")
		return false
	}
	return true
}
func (s *Server) backupError(w http.ResponseWriter, err error) {
	if errors.Is(err, backups.ErrBusy) {
		fail(w, 409, "backup_busy", "Another backup operation is running. Wait for it to finish.")
		return
	}
	if errors.Is(err, backups.ErrStale) || errors.Is(err, backups.ErrChanged) {
		fail(w, 409, "backup_changed", err.Error())
		return
	}
	if errors.Is(err, backups.ErrConfirmation) {
		fail(w, 400, "review_required", err.Error())
		return
	}
	s.logger.Warn("backup operation refused", "error", err)
	fail(w, 400, "backup_refused", "The backup operation was refused. Check the archive, available storage and permissions. Uploaded files must be verified ReelScarey ZIP backups of at most 512 MiB.")
}
func (s *Server) backupCollection(w http.ResponseWriter, r *http.Request) {
	if !s.backupAvailable(w) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		v, e := s.backups.Status(r.Context())
		if e != nil {
			s.backupError(w, e)
			return
		}
		send(w, 200, v)
	case http.MethodPost:
		if !decodeJSON(w, r, &struct{}{}) {
			return
		}
		if e := s.backups.Start(r.Context()); e != nil {
			s.backupError(w, e)
			return
		}
		send(w, 202, map[string]bool{"started": true})
	default:
		w.Header().Set("Allow", "GET, POST")
		fail(w, 405, "method_not_allowed", "This method is not supported.")
	}
}
func (s *Server) backupSchedule(w http.ResponseWriter, r *http.Request) {
	if !s.backupAvailable(w) {
		return
	}
	var p struct {
		Enabled      bool `json:"enabled"`
		IntervalDays int  `json:"interval_days"`
		KeepCount    int  `json:"keep_count"`
		KeepDays     int  `json:"keep_days"`
		Revision     int  `json:"revision"`
	}
	if !decodeJSON(w, r, &p) {
		return
	}
	if e := s.backups.Save(r.Context(), backups.Schedule{Enabled: p.Enabled, IntervalDays: p.IntervalDays, KeepCount: p.KeepCount, KeepDays: p.KeepDays, Revision: p.Revision}); e != nil {
		s.backupError(w, e)
		return
	}
	v, e := s.backups.Status(r.Context())
	if e != nil {
		s.backupError(w, e)
		return
	}
	send(w, 200, v)
}
func longBackupRequest(w http.ResponseWriter, r *http.Request) {
	c := http.NewResponseController(w)
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Minute))
	_ = c.SetWriteDeadline(time.Now().Add(10 * time.Minute))
}
func (s *Server) backupUpload(w http.ResponseWriter, r *http.Request) {
	if !s.backupAvailable(w) {
		return
	}
	longBackupRequest(w, r)
	if r.Header.Get("Content-Type") != "application/zip" {
		fail(w, 415, "zip_required", "Choose a ReelScarey ZIP backup.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, backups.MaxArchive+1)
	v, e := s.backups.Upload(r.Context(), r.Body)
	if e != nil {
		s.backupError(w, e)
		return
	}
	send(w, 201, v)
}
func (s *Server) backupDownload(w http.ResponseWriter, r *http.Request) {
	if !s.backupAvailable(w) {
		return
	}
	longBackupRequest(w, r)
	f, a, e := s.backups.Open(r.PathValue("id"))
	if e != nil {
		fail(w, 404, "backup_missing", "The backup could not be opened.")
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="reelscarey-`+time.Unix(a.Created, 0).UTC().Format("20060102-150405")+`-`+a.ID[:8]+`.zip"`)
	http.ServeContent(w, r, "backup.zip", time.Unix(a.Created, 0), f)
}
func (s *Server) backupDelete(w http.ResponseWriter, r *http.Request) {
	if !s.backupAvailable(w) {
		return
	}
	if e := s.backups.Delete(r.PathValue("id")); e != nil {
		s.backupError(w, e)
		return
	}
	send(w, 200, map[string]bool{"deleted": true})
}
func (s *Server) backupPreview(w http.ResponseWriter, r *http.Request) {
	if !s.backupAvailable(w) || !decodeJSON(w, r, &struct{}{}) {
		return
	}
	longBackupRequest(w, r)
	v, e := s.backups.Preview(r.Context(), r.PathValue("id"))
	if e != nil {
		s.backupError(w, e)
		return
	}
	send(w, 200, v)
}
func (s *Server) backupRestore(w http.ResponseWriter, r *http.Request) {
	if !s.backupAvailable(w) {
		return
	}
	var p struct {
		SHA          string `json:"sha256"`
		Confirmation string `json:"confirmation"`
	}
	if !decodeJSON(w, r, &p) {
		return
	}
	longBackupRequest(w, r)
	if e := s.backups.PrepareRestore(r.Context(), r.PathValue("id"), p.SHA, p.Confirmation); e != nil {
		s.backupError(w, e)
		return
	}
	send(w, 202, map[string]bool{"restarting": true})
}
