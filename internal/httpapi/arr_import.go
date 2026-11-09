package httpapi

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/faaaaq/ReelScarey/internal/arrimport"
	"github.com/faaaaq/ReelScarey/internal/library"
	"github.com/faaaaq/ReelScarey/internal/metadata"
	"github.com/faaaaq/ReelScarey/internal/quality"
)

const maxArrBackup = 256 << 20

type arrPreviewItem struct {
	Kind        string `json:"kind"`
	TMDbID      int64  `json:"tmdb_id,omitempty"`
	TVDBID      int64  `json:"tvdb_id,omitempty"`
	Title       string `json:"title"`
	Year        *int   `json:"year,omitempty"`
	Monitored   bool   `json:"monitored"`
	Date        string `json:"date,omitempty"`
	ReleaseKind string `json:"release_kind,omitempty"`
	InLibrary   bool   `json:"in_library"`
	Error       string `json:"error,omitempty"`
}

type arrApplyItem struct {
	Kind      string `json:"kind"`
	TMDbID    int64  `json:"tmdb_id"`
	TVDBID    int64  `json:"tvdb_id"`
	Monitored bool   `json:"monitored"`
}

// arrBackupPreview reads only the title list from a Radarr or Sonarr backup.
// Indexers, clients, paths, quality profiles and credentials are never read.
func (s *Server) arrBackupPreview(w http.ResponseWriter, r *http.Request) {
	body := http.MaxBytesReader(w, r.Body, maxArrBackup)
	tmp, err := os.CreateTemp("", "reelscarey-arr-*.zip")
	if err != nil {
		s.internal(w, "arr_preview_temp", err)
		return
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err = io.Copy(tmp, body); err != nil {
		tmp.Close()
		fail(w, 400, "invalid_backup", "The backup could not be read.")
		return
	}
	if err = tmp.Close(); err != nil {
		s.internal(w, "arr_preview_temp", err)
		return
	}
	backup, err := arrimport.ParseAny(r.Context(), name)
	if err != nil {
		fail(w, 400, "invalid_backup", err.Error())
		return
	}
	items := make([]arrPreviewItem, 0, len(backup.Items))
	for _, item := range backup.Items {
		preview := arrPreviewItem{Kind: item.Kind, TMDbID: item.TMDbID, TVDBID: item.TVDBID, Title: item.Title, Year: item.Year, Monitored: item.Monitored, Date: item.Date, ReleaseKind: item.ReleaseKind}
		if item.Kind == "series" {
			preview.TMDbID = 0
			if item.TVDBID > 0 {
				resolved, err := s.metadata.FindTVDB(r.Context(), strconv.FormatInt(item.TVDBID, 10))
				if err != nil {
					preview.Error = err.Error()
				} else {
					preview.TMDbID = resolved.TMDbID
					if strings.TrimSpace(preview.Title) == "" {
						preview.Title = resolved.Title
					}
				}
			} else if preview.TMDbID == 0 {
				tmdb, title, err := s.resolveCalendarSeries(r.Context(), item)
				if err != nil {
					preview.Error = err.Error()
				} else {
					preview.TMDbID, preview.Title = tmdb, title
				}
			}
		} else if preview.TMDbID == 0 {
			tmdb, title, err := s.resolveCalendarMovie(r.Context(), item)
			if err != nil {
				preview.Error = err.Error()
			} else {
				preview.TMDbID, preview.Title = tmdb, title
			}
		}
		if preview.TMDbID > 0 {
			var existing library.Item
			if item.Kind == "movie" {
				existing, err = s.library.Movie(r.Context(), preview.TMDbID)
			} else {
				existing, err = s.library.Series(r.Context(), preview.TMDbID)
			}
			preview.InLibrary = err == nil && existing.ID > 0
		}
		items = append(items, preview)
	}
	send(w, 200, map[string]any{"source": backup.Source, "items": items})
}

func sameCalendarTitle(a, b string) bool {
	return quality.Normalize(a) == quality.Normalize(b)
}

func (s *Server) resolveCalendarMovie(ctx context.Context, item arrimport.Candidate) (int64, string, error) {
	results, err := s.metadata.Search(ctx, item.Title, 1)
	if err != nil {
		return 0, "", err
	}
	matches := []metadata.Movie{}
	for _, movie := range results.Items {
		if sameCalendarTitle(movie.Title, item.Title) {
			matches = append(matches, movie)
		}
	}
	if len(matches) > 1 && item.Year != nil {
		filtered := matches[:0]
		for _, movie := range matches {
			if movie.Year != nil && *movie.Year == *item.Year {
				filtered = append(filtered, movie)
			}
		}
		matches = filtered
	}
	if len(matches) == 1 && matches[0].TMDbID > 0 {
		return matches[0].TMDbID, matches[0].Title, nil
	}
	if len(matches) > 1 {
		return 0, "", fmt.Errorf("multiple TMDb movies match this title; add it manually")
	}
	return 0, "", fmt.Errorf("no exact TMDb movie match; add it manually")
}

func (s *Server) resolveCalendarSeries(ctx context.Context, item arrimport.Candidate) (int64, string, error) {
	results, err := s.metadata.SearchSeries(ctx, item.Title, 1)
	if err != nil {
		return 0, "", err
	}
	matches := []metadata.Series{}
	for _, show := range results.Items {
		if sameCalendarTitle(show.Title, item.Title) {
			matches = append(matches, show)
		}
	}
	if len(matches) == 1 && matches[0].TMDbID > 0 {
		return matches[0].TMDbID, matches[0].Title, nil
	}
	if len(matches) > 1 {
		return 0, "", fmt.Errorf("multiple TMDb shows match this title; add it manually")
	}
	return 0, "", fmt.Errorf("no exact TMDb show match; add it manually")
}

// arrBackupApply adds the selected reviewed titles. It never imports paths,
// files, clients, indexers or provider credentials.
func (s *Server) arrBackupApply(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Items []arrApplyItem `json:"items"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if len(in.Items) < 1 || len(in.Items) > 5000 {
		fail(w, 400, "invalid_selection", "Choose between 1 and 5000 titles from the backup review.")
		return
	}
	added, skipped := 0, 0
	errorsOut := []map[string]string{}
	for _, item := range in.Items {
		kind := strings.TrimSpace(item.Kind)
		if kind != "movie" && kind != "series" {
			errorsOut = append(errorsOut, map[string]string{"title": "Unknown title", "message": "Choose a movie or series."})
			continue
		}
		tmdb := item.TMDbID
		title := ""
		if kind == "series" && tmdb < 1 && item.TVDBID > 0 {
			resolved, err := s.metadata.FindTVDB(r.Context(), strconv.FormatInt(item.TVDBID, 10))
			if err != nil {
				errorsOut = append(errorsOut, map[string]string{"title": "TVDB " + strconv.FormatInt(item.TVDBID, 10), "message": err.Error()})
				continue
			}
			tmdb = resolved.TMDbID
			title = resolved.Title
		}
		if tmdb < 1 {
			errorsOut = append(errorsOut, map[string]string{"title": item.Kind, "message": "This title has no TMDb ID."})
			continue
		}
		if kind == "movie" {
			if _, err := s.library.Movie(r.Context(), tmdb); err == nil {
				skipped++
				continue
			}
			movie, err := s.metadata.Movie(r.Context(), tmdb)
			if err != nil {
				errorsOut = append(errorsOut, map[string]string{"title": titleFor(tmdb, title), "message": err.Error()})
				continue
			}
			if _, created, err := s.library.AddMovie(r.Context(), movie, item.Monitored, ""); err != nil {
				errorsOut = append(errorsOut, map[string]string{"title": movie.Title, "message": err.Error()})
			} else if created {
				added++
			} else {
				skipped++
			}
			continue
		}
		if _, err := s.library.Series(r.Context(), tmdb); err == nil {
			skipped++
			continue
		}
		show, err := s.metadata.Series(r.Context(), tmdb)
		if err != nil {
			errorsOut = append(errorsOut, map[string]string{"title": titleFor(tmdb, title), "message": err.Error()})
			continue
		}
		item2, created, err := s.library.AddSeries(r.Context(), show, item.Monitored, "")
		if err != nil {
			errorsOut = append(errorsOut, map[string]string{"title": show.Title, "message": err.Error()})
			continue
		}
		if created {
			added++
			if item.Monitored {
				// Sonarr's per-season choices are not imported; future-only
				// monitoring matches the new-title default and avoids pulling
				// the entire back catalog without an explicit decision.
				if err := s.library.SetSeriesMonitorMode(context.WithoutCancel(r.Context()), item2.ID, "future"); err != nil {
					errorsOut = append(errorsOut, map[string]string{
						"title":   show.Title,
						"message": "The show was added, but future-only monitoring could not be applied: " + err.Error(),
					})
				}
			}
		} else {
			skipped++
		}
	}
	send(w, 200, map[string]any{"added": added, "skipped": skipped, "errors": errorsOut})
}

func titleFor(tmdb int64, title string) string {
	if strings.TrimSpace(title) != "" {
		return title
	}
	return "TMDb " + strconv.FormatInt(tmdb, 10)
}
