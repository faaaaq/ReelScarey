package httpapi

import (
	"context"
	"errors"
	"github.com/faaaaq/ReelScarey/internal/maintenance"
	"github.com/faaaaq/ReelScarey/internal/storage"
	"net/http"
	"strconv"
	"time"
)

func (s *Server) storageBrowser(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	for key, v := range q {
		if (key != "root" && key != "path" && key != "page") || len(v) != 1 {
			fail(w, 400, "invalid_folder", "Choose a mounted folder and page.")
			return
		}
	}
	page := 1
	var e error
	if q.Get("page") != "" {
		page, e = strconv.Atoi(q.Get("page"))
		if e != nil {
			fail(w, 400, "invalid_folder", "Choose a valid page.")
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	out, e := storage.Browse(ctx, s.config, q.Get("root"), q.Get("path"), page)
	if e != nil {
		code := 400
		message := storage.ErrPath.Error()
		if errors.Is(e, storage.ErrLimit) {
			code = 409
			message = storage.ErrLimit.Error()
		}
		fail(w, code, "folder_unavailable", message)
		return
	}
	send(w, 200, out)
}
func (s *Server) storageMigration(w http.ResponseWriter, r *http.Request) {
	out, e := maintenance.RootMigrationInfo(s.config.ConfigDir)
	if e != nil {
		fail(w, 409, "storage_review_required", "The saved storage migration needs offline review.")
		return
	}
	send(w, 200, out)
}
