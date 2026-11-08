package httpapi

import (
	"net/http"
	"path/filepath"

	"github.com/faaaaq/ReelScarey/internal/organization"
	"github.com/faaaaq/ReelScarey/internal/transfer"
)

func (s *Server) existingCollection(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, e := s.transfer.LibraryScans(r.Context())
		if e != nil {
			s.transferError(w, e)
			return
		}
		prefs, e := s.organization.Get(r.Context())
		if e != nil {
			s.internal(w, "library_scan_settings", e)
			return
		}
		rootConfig, e := s.organization.GetRoots(r.Context())
		if e != nil {
			s.internal(w, "library_scan_roots", e)
			return
		}
		byKey := map[string]organization.LibraryRoots{}
		for _, item := range rootConfig.Libraries {
			byKey[item.Key] = item
		}
		rootPaths := func(key string) []string {
			paths := []string{}
			for _, root := range byKey[key].Roots {
				paths = append(paths, root.Path)
			}
			return paths
		}
		collections := []map[string]any{}
		movieRoot, seriesRoot := "", ""
		for _, l := range prefs.Libraries {
			m, t := transfer.DiscoveryFolder(l, "movie"), transfer.DiscoveryFolder(l, "series")
			if m != "" {
				m = filepath.Join(s.config.MediaDir, m)
			}
			if t != "" {
				t = filepath.Join(s.config.MediaDir, t)
			}
			content := l.Content
			if l.Key == "anime" {
				content = "series"
			}
			if l.Key == "movies" {
				movieRoot = m
			}
			if l.Key == "tv" {
				seriesRoot = t
			}
			paths := rootPaths(l.Key)
			if len(paths) > 0 && paths[0] != "" {
				switch l.Key {
				case "movies":
					movieRoot = filepath.Join(s.config.MediaDir, paths[0])
				case "tv", "anime":
					seriesRoot = filepath.Join(s.config.MediaDir, paths[0])
				}
			}
			collections = append(collections, map[string]any{"key": l.Key, "name": l.Name, "content": content, "movie_root": m, "series_root": t, "roots": byKey[l.Key].Roots})
		}
		mediaRoots := []organization.LibraryRoots{}
		for _, key := range []string{"movies", "tv", "anime", "music"} {
			if item, ok := byKey[key]; ok {
				mediaRoots = append(mediaRoots, item)
			}
		}
		send(w, 200, map[string]any{"items": items, "media_root": s.config.MediaDir, "movie_root": movieRoot, "series_root": seriesRoot, "collections": collections, "library_roots": mediaRoots})
	case http.MethodPost:
		var in struct {
			Kind              string `json:"kind"`
			LibraryKey        string `json:"library_key"`
			Root              string `json:"root"`
			AutomaticMatching bool   `json:"automatic_matching"`
		}
		if !decodeJSON(w, r, &in) {
			return
		}
		out, e := s.transfer.StartLibraryScanRoots(r.Context(), in.Kind, in.LibraryKey, in.Root, in.AutomaticMatching)
		if e != nil {
			s.transferError(w, e)
			return
		}
		send(w, 202, out)
	default:
		w.Header().Set("Allow", "GET, POST")
		fail(w, 405, "method_not_allowed", "Use GET or POST.")
	}
}
func (s *Server) existingScan(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	id := r.PathValue("id")
	if len(id) != 32 {
		fail(w, 400, "invalid_scan", "Choose a saved library scan.")
		return
	}
	switch r.Method {
	case http.MethodGet:
		page, ok := pageNumber(r, 100000)
		if !ok {
			fail(w, 400, "invalid_page", "Choose a valid page.")
			return
		}
		out, e := s.transfer.DiscoveredFiles(r.Context(), id, page, r.URL.Query().Get("state"))
		if e != nil {
			s.transferError(w, e)
			return
		}
		send(w, 200, out)
	case http.MethodDelete:
		if !decodeJSON(w, r, &struct{}{}) {
			return
		}
		if e := s.transfer.RemoveLibraryScan(r.Context(), id); e != nil {
			s.transferError(w, e)
			return
		}
		send(w, 200, map[string]bool{"removed": true})
	default:
		w.Header().Set("Allow", "GET, DELETE")
		fail(w, 405, "method_not_allowed", "Use GET or DELETE.")
	}
}
func (s *Server) existingCancel(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) || !decodeJSON(w, r, &struct{}{}) {
		return
	}
	if e := s.transfer.CancelLibraryScan(r.Context(), r.PathValue("id")); e != nil {
		s.transferError(w, e)
		return
	}
	send(w, 200, map[string]bool{"cancelled": true})
}
func (s *Server) existingRegister(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	var in transfer.ExistingRegistration
	if !decodeJSON(w, r, &in) {
		return
	}
	if e := s.transfer.RegisterDiscovered(r.Context(), r.PathValue("id"), in); e != nil {
		s.transferError(w, e)
		return
	}
	send(w, 202, map[string]bool{"queued": true})
}
func (s *Server) existingSkip(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) || !decodeJSON(w, r, &struct{}{}) {
		return
	}
	if e := s.transfer.SkipDiscovered(r.Context(), r.PathValue("id")); e != nil {
		s.transferError(w, e)
		return
	}
	send(w, 200, map[string]bool{"skipped": true})
}
func (s *Server) existingDuplicates(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	id := r.PathValue("id")
	if len(id) != 32 {
		fail(w, 400, "invalid_scan", "Choose a saved library scan.")
		return
	}
	out, e := s.transfer.DuplicateGroups(r.Context(), id)
	if e != nil {
		s.transferError(w, e)
		return
	}
	send(w, 200, out)
}
func (s *Server) existingDuplicateResolve(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	id := r.PathValue("id")
	if len(id) != 32 {
		fail(w, 400, "invalid_scan", "Choose a saved library scan.")
		return
	}
	var in transfer.DuplicateResolution
	if !decodeJSON(w, r, &in) {
		return
	}
	skipped, e := s.transfer.ResolveDuplicate(r.Context(), id, in)
	if e != nil {
		s.transferError(w, e)
		return
	}
	send(w, 202, map[string]any{"queued": true, "skipped": skipped})
}
func (s *Server) existingCatalog(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	items, e := s.transfer.ImportCatalog(r.Context(), r.URL.Query().Get("kind"), r.URL.Query().Get("query"))
	if e != nil {
		s.transferError(w, e)
		return
	}
	send(w, 200, map[string]any{"items": items})
}

func (s *Server) existingAutomaticMatching(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	var in struct {
		Action string `json:"action"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	var err error
	switch in.Action {
	case "start":
		err = s.transfer.StartAutomaticMatching(r.Context(), r.PathValue("id"))
	case "stop":
		err = s.transfer.CancelAutomaticMatching(r.Context(), r.PathValue("id"))
	default:
		fail(w, 400, "invalid_action", "Choose start or stop for automatic matching.")
		return
	}
	if err != nil {
		s.transferError(w, err)
		return
	}
	status, err := s.transfer.AutomaticMatchStatus(r.Context(), r.PathValue("id"))
	if err != nil {
		s.transferError(w, err)
		return
	}
	send(w, 202, status)
}
