package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/faaaaq/ReelScarey/internal/organization"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

func (s *Server) organizationError(w http.ResponseWriter, e error) {
	var typed *organization.Error
	if errors.As(e, &typed) {
		status := 400
		switch typed.Code {
		case "revision_conflict", "plan_stale", "media_busy", "organization_busy", "file_changed", "destination_exists", "unsafe_path", "storage_unavailable", "work_pending":
			status = 409
		}
		fail(w, status, typed.Code, typed.Message)
		return
	}
	if errors.Is(e, sql.ErrNoRows) {
		fail(w, 404, "not_found", "This saved title, file or organization review was not found.")
		return
	}
	s.internal(w, "organization", e)
}
func (s *Server) createTitleFolder(w http.ResponseWriter, r *http.Request) {
	id, ok := tvID(w, r)
	if !ok || !decodeJSON(w, r, &struct{}{}) {
		return
	}
	name, err := s.organization.CreateTitleFolder(r.Context(), id)
	if err != nil {
		s.organizationError(w, err)
		return
	}
	send(w, 200, map[string]string{"path": name})
}
func (s *Server) organizationSettings(w http.ResponseWriter, r *http.Request) {
	var out organization.Config
	var e error
	switch r.Method {
	case http.MethodGet:
		out, e = s.organization.Get(r.Context())
	case http.MethodPut:
		var in organization.Config
		if !decodeOrganizationConfig(w, r, &in) {
			return
		}
		out, e = s.organization.Update(r.Context(), in)
	default:
		w.Header().Set("Allow", "GET, PUT")
		fail(w, 405, "method_not_allowed", "Use GET or PUT.")
		return
	}
	if e != nil {
		s.organizationError(w, e)
		return
	}
	send(w, 200, out)
}
func decodeOrganizationConfig(w http.ResponseWriter, r *http.Request, out *organization.Config) bool {
	media, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || media != "application/json" {
		fail(w, 415, "unsupported_media_type", "Use application/json.")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	invalid := func(e error) {
		var large *http.MaxBytesError
		if errors.As(e, &large) {
			fail(w, 413, "body_too_large", "Organization settings must be at most 64 KiB.")
		} else {
			fail(w, 400, "invalid_json", "Send one valid organization settings object with documented fields.")
		}
	}
	if e = d.Decode(out); e != nil {
		invalid(e)
		return false
	}
	var extra any
	if e = d.Decode(&extra); e != io.EOF {
		invalid(e)
		return false
	}
	return true
}
func (s *Server) organizationAssignment(w http.ResponseWriter, r *http.Request) {
	id, ok := tvID(w, r)
	if !ok {
		return
	}
	var out organization.Assignment
	var e error
	switch r.Method {
	case http.MethodGet:
		out, e = s.organization.GetAssignment(r.Context(), id)
	case http.MethodPut:
		var in struct {
			LibraryKey string `json:"library_key"`
			Revision   int    `json:"revision"`
		}
		if !decodeJSON(w, r, &in) {
			return
		}
		out, e = s.organization.PutAssignment(r.Context(), id, in.LibraryKey, in.Revision)
	default:
		w.Header().Set("Allow", "GET, PUT")
		fail(w, 405, "method_not_allowed", "Use GET or PUT.")
		return
	}
	if e != nil {
		s.organizationError(w, e)
		return
	}
	send(w, 200, out)
}
func (s *Server) organizationPlans(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		out, e := s.organization.Plans(r.Context())
		if e != nil {
			s.organizationError(w, e)
			return
		}
		send(w, 200, map[string]any{"items": out})
	case http.MethodPost:
		var in struct {
			FileIDs       []int64 `json:"file_ids"`
			MediaID       int64   `json:"media_id"`
			TargetLibrary string  `json:"target_library"`
			WholeFolder   bool    `json:"whole_folder"`
		}
		if !decodeJSON(w, r, &in) {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		var out organization.Plan
		var e error
		if in.WholeFolder && (in.MediaID == 0 || len(in.FileIDs) != 0) {
			fail(w, 400, "invalid_selection", "Choose one title for a whole-folder review.")
			return
		} else if in.WholeFolder {
			out, e = s.organization.PreviewFolder(ctx, in.MediaID, in.TargetLibrary)
		} else if in.MediaID != 0 && len(in.FileIDs) == 0 {
			out, e = s.organization.PreviewTitle(ctx, in.MediaID, in.TargetLibrary)
		} else if in.MediaID != 0 {
			fail(w, 400, "invalid_selection", "Choose a title or individual files, not both.")
			return
		} else {
			out, e = s.organization.Preview(ctx, in.FileIDs, in.TargetLibrary)
		}
		if e != nil {
			s.organizationError(w, e)
			return
		}
		send(w, 201, out)
	default:
		w.Header().Set("Allow", "GET, POST")
		fail(w, 405, "method_not_allowed", "Use GET or POST.")
	}
}
func (s *Server) organizationPlan(w http.ResponseWriter, r *http.Request) {
	out, e := s.organization.Plan(r.Context(), r.PathValue("id"))
	if e != nil {
		s.organizationError(w, e)
		return
	}
	send(w, 200, out)
}
func (s *Server) organizationApply(w http.ResponseWriter, r *http.Request) {
	var in struct{}
	if !decodeJSON(w, r, &in) {
		return
	}
	out, e := s.organization.Apply(r.Context(), r.PathValue("id"))
	if e != nil {
		s.organizationError(w, e)
		return
	}
	send(w, 202, out)
}

type organizationFile struct {
	ID       int64  `json:"id"`
	MediaID  int64  `json:"media_id"`
	Title    string `json:"title"`
	Kind     string `json:"kind"`
	Category string `json:"category"`
	Path     string `json:"path"`
	Size     int64  `json:"size_bytes"`
}

func (s *Server) organizationFiles(w http.ResponseWriter, r *http.Request) {
	q, e := url.ParseQuery(r.URL.RawQuery)
	valid := e == nil
	for k, v := range q {
		if len(v) != 1 || (k != "page" && k != "query" && k != "media_id" && k != "kind") {
			valid = false
		}
	}
	page, ok := pageNumber(r, 100000)
	query := q.Get("query")
	kind := q.Get("kind")
	if !valid || !ok || len(query) > 200 || !utf8.ValidString(query) || strings.ContainsFunc(query, unicode.IsControl) || (kind != "" && kind != "movie" && kind != "series" && kind != "anime") {
		fail(w, 400, "invalid_files", "Choose valid file filters and a page.")
		return
	}
	var mediaID int64
	if q.Get("media_id") != "" {
		mediaID, e = strconv.ParseInt(q.Get("media_id"), 10, 64)
		if e != nil || mediaID < 1 {
			fail(w, 400, "invalid_files", "Choose a saved title.")
			return
		}
	}
	where := ` FROM media_files f JOIN media_items m ON m.id=COALESCE(f.movie_id,f.series_id) WHERE (?=0 OR m.id=?) AND (?='' OR (?='anime' AND m.category='anime') OR (m.kind=? AND m.category='standard')) AND m.title LIKE ? ESCAPE '\'`
	query = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(strings.TrimSpace(query))
	args := []any{mediaID, mediaID, kind, kind, kind, "%" + query + "%"}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		s.organizationError(w, e)
		return
	}
	defer tx.Rollback()
	var total int
	if e = tx.QueryRowContext(ctx, `SELECT COUNT(*)`+where, args...).Scan(&total); e != nil {
		s.organizationError(w, e)
		return
	}
	rows, e := tx.QueryContext(ctx, `SELECT f.id,m.id,m.title,m.kind,m.category,f.relative_path,f.size_bytes`+where+` ORDER BY m.title COLLATE NOCASE,f.id LIMIT 50 OFFSET ?`, append(args, (page-1)*50)...)
	if e != nil {
		s.organizationError(w, e)
		return
	}
	items := []organizationFile{}
	for rows.Next() {
		var f organizationFile
		if e = rows.Scan(&f.ID, &f.MediaID, &f.Title, &f.Kind, &f.Category, &f.Path, &f.Size); e != nil {
			rows.Close()
			s.organizationError(w, e)
			return
		}
		items = append(items, f)
	}
	e = rows.Err()
	rows.Close()
	if e == nil {
		e = tx.Commit()
	}
	if e != nil {
		s.organizationError(w, e)
		return
	}
	send(w, 200, map[string]any{"items": items, "total": total, "page": page, "limit": 50})
}

func (s *Server) organizationFileHealth(w http.ResponseWriter, r *http.Request) {
	id, ok := tvID(w, r)
	if !ok || !decodeJSON(w, r, &struct{}{}) {
		return
	}
	// A checksum reads the entire file and can take longer than ordinary requests.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(30 * time.Minute))
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Minute)
	defer cancel()
	out, err := s.organization.CheckFileHealth(ctx, id)
	if err != nil {
		s.organizationError(w, err)
		return
	}
	send(w, 200, out)
}

func (s *Server) organizationReplacement(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		id, err := strconv.ParseInt(r.URL.Query().Get("media_file_id"), 10, 64)
		if err != nil || id < 1 {
			fail(w, 400, "invalid_selection", "Choose a registered file.")
			return
		}
		out, err := s.organization.ReplacementVersions(r.Context(), id)
		if err != nil {
			s.organizationError(w, err)
			return
		}
		send(w, 200, map[string]any{"items": out})
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		fail(w, 405, "method_not_allowed", "Use GET or POST.")
		return
	}
	var in struct {
		FileID     int64  `json:"media_file_id"`
		ScanFileID string `json:"scan_file_id"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.FileID < 1 || in.ScanFileID == "" {
		fail(w, 400, "invalid_selection", "Select one registered file and one discovered source.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	out, err := s.organization.PreviewReplacement(ctx, in.FileID, in.ScanFileID)
	if err != nil {
		s.organizationError(w, err)
		return
	}
	send(w, 201, out)
}

func (s *Server) organizationRollback(w http.ResponseWriter, r *http.Request) {
	if !decodeJSON(w, r, &struct{}{}) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	out, err := s.organization.PreviewRollback(ctx, r.PathValue("id"))
	if err != nil {
		s.organizationError(w, err)
		return
	}
	send(w, 201, out)
}

func (s *Server) organizationCancel(w http.ResponseWriter, r *http.Request) {
	if !decodeJSON(w, r, &struct{}{}) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	out, err := s.organization.CancelQueued(ctx, r.PathValue("id"))
	if err != nil {
		s.organizationError(w, err)
		return
	}
	send(w, 200, out)
}

func (s *Server) organizationRecoverReplacement(w http.ResponseWriter, r *http.Request) {
	if !decodeJSON(w, r, &struct{}{}) {
		return
	}
	out, err := s.organization.RecoverReplacement(r.Context(), r.PathValue("id"))
	if err != nil {
		s.organizationError(w, err)
		return
	}
	send(w, 200, out)
}
