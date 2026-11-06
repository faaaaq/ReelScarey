package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// The uploaded metainfo is the release selection: the request body is the raw
// .torrent file, bounded like every other external input. The queueing rules,
// duplicate prevention and verified import are the standard ones.
func (s *Server) manualMovieTorrent(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	id, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if e != nil || id < 1 {
		fail(w, 400, "invalid_selection", "Choose a movie from the catalog.")
		return
	}
	version := r.URL.Query().Get("movie_version")
	if version != "" && version != "hd" && version != "uhd" && version != "additional" {
		fail(w, 400, "invalid_version", "Choose a managed version, an additional edition, or leave the version empty.")
		return
	}
	data, ok := readTorrentBody(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	job, created, err := s.transfer.QueueManualTorrent(ctx, id, version, data)
	if err != nil {
		s.transferError(w, err)
		return
	}
	status := 200
	if created {
		status = 202
	}
	send(w, status, map[string]any{"item": job, "created": created})
}

func (s *Server) manualSeriesTorrent(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	id, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if e != nil || id < 1 {
		fail(w, 400, "invalid_selection", "Choose a series from the catalog.")
		return
	}
	season, e := strconv.Atoi(r.URL.Query().Get("season"))
	if e != nil || season < 0 {
		fail(w, 400, "invalid_season", "Choose the season this torrent covers.")
		return
	}
	episodes := []int{}
	if raw := strings.TrimSpace(r.URL.Query().Get("episodes")); raw != "" {
		for _, part := range strings.Split(raw, ",") {
			n, e := strconv.Atoi(strings.TrimSpace(part))
			if e != nil || n < 0 {
				fail(w, 400, "invalid_season", "Choose valid episode numbers.")
				return
			}
			episodes = append(episodes, n)
		}
	}
	data, ok := readTorrentBody(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	job, created, err := s.transfer.QueueManualSeriesTorrent(ctx, id, season, episodes, data)
	if err != nil {
		s.transferError(w, err)
		return
	}
	status := 200
	if created {
		status = 202
	}
	send(w, status, map[string]any{"item": job, "created": created})
}

// readTorrentBody reads the bounded raw metainfo upload. An empty or oversized
// body fails with the same honest problem the parser would report.
func readTorrentBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	body := http.MaxBytesReader(w, r.Body, 8<<20)
	data, e := io.ReadAll(body)
	if e != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(e, &tooLarge) {
			fail(w, 413, "payload_too_large", "Upload a .torrent file of at most 8 MiB.")
		} else {
			fail(w, 400, "invalid_torrent", "The uploaded file could not be read.")
		}
		return nil, false
	}
	if len(data) == 0 {
		fail(w, 400, "invalid_torrent", "Choose a .torrent file to upload.")
		return nil, false
	}
	return data, true
}
