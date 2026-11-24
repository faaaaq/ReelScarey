package httpapi

import (
	"encoding/hex"
	"net/http"
	"sync"
)

type browserStreamKey struct {
	fileID int64
	id     [16]byte
}

type browserMusicStreamInfo struct {
	Codec         string `json:"codec"`
	BitrateKbps   int    `json:"bitrate_kbps,omitempty"`
	Mode          string `json:"mode"`
	RangeSeekable bool   `json:"range_seekable"`
}

// Keep only recent observations. The browser reads the entry after it has
// decoded the media headers, without opening another encoder or audio file.
type browserMusicStreams struct {
	mu      sync.Mutex
	entries map[browserStreamKey]browserMusicStreamInfo
	order   [128]browserStreamKey
	next    int
}

func (streams *browserMusicStreams) put(key browserStreamKey, info browserMusicStreamInfo) {
	streams.mu.Lock()
	defer streams.mu.Unlock()
	if streams.entries == nil {
		streams.entries = make(map[browserStreamKey]browserMusicStreamInfo)
	}
	if _, exists := streams.entries[key]; !exists {
		delete(streams.entries, streams.order[streams.next])
		streams.order[streams.next] = key
		streams.next = (streams.next + 1) % len(streams.order)
	}
	streams.entries[key] = info
}

func (streams *browserMusicStreams) get(key browserStreamKey) (browserMusicStreamInfo, bool) {
	streams.mu.Lock()
	defer streams.mu.Unlock()
	info, ok := streams.entries[key]
	return info, ok
}

func browserMusicStreamKey(r *http.Request, fileID int64) (browserStreamKey, bool) {
	key := browserStreamKey{fileID: fileID}
	token := r.URL.Query().Get("stream_id")
	if len(token) != 2*len(key.id) {
		return key, false
	}
	_, err := hex.Decode(key.id[:], []byte(token))
	return key, err == nil
}

func (s *Server) musicFileStreamInfo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	id, ok := musicID(w, r)
	if !ok {
		return
	}
	key, ok := browserMusicStreamKey(r, id)
	if !ok {
		fail(w, http.StatusBadRequest, "music_stream_id_invalid", "The stream ID is invalid.")
		return
	}
	info, ok := s.musicStreams.get(key)
	if !ok {
		fail(w, http.StatusNotFound, "music_stream_info_unavailable", "Stream details are unavailable.")
		return
	}
	send(w, http.StatusOK, info)
}
