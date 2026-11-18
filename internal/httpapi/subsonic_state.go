package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/faaaaq/ReelScarey/internal/music"
)

type musicPlaybackStateService interface {
	PlaybackState() *music.PlaybackState
}

func (s *Server) playbackState(r *http.Request) (*music.PlaybackState, bool) {
	service, ok := s.music.(musicPlaybackStateService)
	if !ok {
		return nil, false
	}
	return service.PlaybackState(), true
}

func subsonicStateWriteFailure(w http.ResponseWriter, r *http.Request) {
	writeSubsonic(w, r, subsonicFailure(subsonicVersion, 50, "Playback state requires a named music-server account; the legacy shared token has no per-user state."))
}

func (s *Server) playbackStateForWrite(w http.ResponseWriter, r *http.Request) (*music.PlaybackState, bool) {
	state, ok := s.playbackState(r)
	if !ok || subsonicRequestUser(r).Anonymous() {
		subsonicStateWriteFailure(w, r)
		return nil, false
	}
	return state, true
}

func subsonicStateError(w http.ResponseWriter, r *http.Request, err error) {
	code := 0
	message := "The playback-state operation could not be completed."
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, music.ErrNotFound) {
		code = 70
		message = "The requested playback-state object was not found."
	} else if errors.Is(err, music.ErrInvalidInput) {
		code = 10
		message = "A required playback-state parameter is missing or malformed."
	}
	writeSubsonic(w, r, subsonicFailure(subsonicVersion, code, message))
}

func subsonicBool(r *http.Request, key string, fallback bool) (bool, error) {
	values, exists := r.URL.Query()[key]
	if !exists {
		return fallback, nil
	}
	if len(values) != 1 || strings.TrimSpace(values[0]) == "" {
		return false, music.ErrInvalidInput
	}
	value, err := strconv.ParseBool(strings.TrimSpace(values[0]))
	if err != nil {
		return false, music.ErrInvalidInput
	}
	return value, nil
}

func subsonicIDs(values []string, prefix string) ([]int64, error) {
	out := []int64{}
	for _, raw := range values {
		for _, part := range strings.Split(raw, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			id, ok := subsonicParseID(part, prefix)
			if !ok {
				value, err := strconv.ParseInt(part, 10, 64)
				if err != nil || value < 1 {
					return nil, music.ErrInvalidInput
				}
				id = value
			}
			out = append(out, id)
			if len(out) > 500 {
				return nil, music.ErrInvalidInput
			}
		}
	}
	return out, nil
}

func subsonicInts(values []string) ([]int, error) {
	out := []int{}
	for _, raw := range values {
		for _, part := range strings.Split(raw, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				return nil, music.ErrInvalidInput
			}
			value, err := strconv.Atoi(part)
			if err != nil || value < 0 {
				return nil, music.ErrInvalidInput
			}
			out = append(out, value)
			if len(out) > 500 {
				return nil, music.ErrInvalidInput
			}
		}
	}
	return out, nil
}

// subsonicPlaybackDecoration snapshots one named user's state for a complete
// response, avoiding a database query per song while keeping users isolated.
type subsonicPlaybackDecoration struct {
	snapshot music.PlaybackSnapshot
	marks    map[string]music.PlaybackMark
}

func playbackMarkKey(kind string, id int64) string {
	return kind + ":" + strconv.FormatInt(id, 10)
}

func decorationForSnapshot(snapshot music.PlaybackSnapshot) *subsonicPlaybackDecoration {
	d := &subsonicPlaybackDecoration{snapshot: snapshot, marks: map[string]music.PlaybackMark{}}
	for _, mark := range snapshot.Marks {
		d.marks[playbackMarkKey(mark.Kind, mark.ObjectID)] = mark
	}
	return d
}

func (s *Server) playbackDecoration(r *http.Request) *subsonicPlaybackDecoration {
	account := subsonicRequestUser(r)
	if account.Anonymous() {
		return decorationForSnapshot(music.PlaybackSnapshot{})
	}
	state, ok := s.playbackState(r)
	if !ok {
		return decorationForSnapshot(music.PlaybackSnapshot{})
	}
	snapshot, err := state.Snapshot(r.Context(), account.ID)
	if err != nil {
		return decorationForSnapshot(music.PlaybackSnapshot{})
	}
	return decorationForSnapshot(snapshot)
}

func subsonicStarredAt(seconds int64) string {
	if seconds <= 0 {
		return ""
	}
	return time.Unix(seconds, 0).UTC().Format(time.RFC3339)
}

func (d *subsonicPlaybackDecoration) song(entry subsonicSong) subsonicSong {
	id, ok := subsonicParseID(entry.ID, subsonicSongPrefix)
	if !ok {
		return entry
	}
	mark := d.marks[playbackMarkKey("song", id)]
	entry.Starred = subsonicStarredAt(mark.StarredAt)
	entry.UserRating = mark.Rating
	entry.PlayCount = d.snapshot.SongPlayCounts[id]
	entry.Played = subsonicStarredAt(d.snapshot.SongLastPlayed[id])
	return entry
}

func (d *subsonicPlaybackDecoration) album(entry subsonicAlbum) subsonicAlbum {
	id, ok := subsonicParseID(entry.ID, subsonicAlbumPrefix)
	if !ok {
		return entry
	}
	mark := d.marks[playbackMarkKey("album", id)]
	entry.Starred = subsonicStarredAt(mark.StarredAt)
	entry.UserRating = mark.Rating
	entry.PlayCount = d.snapshot.AlbumPlayCounts[id]
	entry.Played = subsonicStarredAt(d.snapshot.AlbumLastPlayed[id])
	return entry
}

func (d *subsonicPlaybackDecoration) artist(entry subsonicArtist) subsonicArtist {
	id, ok := subsonicParseID(entry.ID, subsonicArtistPrefix)
	if !ok {
		return entry
	}
	mark := d.marks[playbackMarkKey("artist", id)]
	entry.Starred = subsonicStarredAt(mark.StarredAt)
	entry.UserRating = mark.Rating
	return entry
}

func (d *subsonicPlaybackDecoration) child(entry subsonicSong) subsonicSong {
	if id, ok := subsonicParseID(entry.ID, subsonicAlbumPrefix); ok && entry.IsDir {
		mark := d.marks[playbackMarkKey("album", id)]
		entry.Starred = subsonicStarredAt(mark.StarredAt)
		entry.UserRating = mark.Rating
		entry.PlayCount = d.snapshot.AlbumPlayCounts[id]
		entry.Played = subsonicStarredAt(d.snapshot.AlbumLastPlayed[id])
		return entry
	}
	if id, ok := subsonicParseID(entry.ID, subsonicArtistPrefix); ok && entry.IsDir {
		mark := d.marks[playbackMarkKey("artist", id)]
		entry.Starred = subsonicStarredAt(mark.StarredAt)
		entry.UserRating = mark.Rating
		entry.PlayCount = d.snapshot.ArtistPlayCounts[id]
		return entry
	}
	return d.song(entry)
}

func (s *Server) decorateSubsonicSongs(r *http.Request, entries []subsonicSong) {
	d := s.playbackDecoration(r)
	for i := range entries {
		entries[i] = d.song(entries[i])
	}
}

func (s *Server) decorateSubsonicAlbums(r *http.Request, entries []subsonicAlbum) {
	d := s.playbackDecoration(r)
	for i := range entries {
		entries[i] = d.album(entries[i])
	}
}

func (s *Server) decorateSubsonicArtists(r *http.Request, entries []subsonicArtist) {
	d := s.playbackDecoration(r)
	for i := range entries {
		entries[i] = d.artist(entries[i])
	}
}

func subsonicStateSongs(r *http.Request, s *Server, ids []int64, d *subsonicPlaybackDecoration) ([]subsonicSong, error) {
	out := []subsonicSong{}
	for _, id := range ids {
		song, err := s.music.ServerSong(r.Context(), id)
		if err != nil {
			return nil, err
		}
		out = append(out, d.song(subsonicSongFrom(song)))
	}
	return out, nil
}

func subsonicPlaylistResponse(r *http.Request, s *Server, playlist music.PlaybackPlaylist, entries bool, d *subsonicPlaybackDecoration) (subsonicPlaylist, error) {
	out := subsonicPlaylist{
		ID: strconv.FormatInt(playlist.ID, 10), Owner: playlist.Owner, Name: playlist.Name,
		Comment: playlist.Comment, Public: playlist.Public, SongCount: len(playlist.EntryIDs),
		Duration: int(playlist.DurationMS / 1000),
		Created:  time.Unix(playlist.CreatedAt, 0).UTC().Format(time.RFC3339),
		Changed:  time.Unix(playlist.UpdatedAt, 0).UTC().Format(time.RFC3339),
	}
	if entries {
		songs, err := subsonicStateSongs(r, s, playlist.EntryIDs, d)
		if err != nil {
			return out, err
		}
		out.Entry = songs
	}
	return out, nil
}

func (s *Server) subsonicStatePlaylists(w http.ResponseWriter, r *http.Request) {
	out := subsonicResponseFor(s.version)
	out.Playlists = &subsonicPlaylists{Playlists: []subsonicPlaylist{}}
	state, ok := s.playbackState(r)
	if !ok {
		writeSubsonic(w, r, out)
		return
	}
	playlists, err := state.Playlists(r.Context(), subsonicRequestUser(r).ID)
	if err != nil {
		subsonicStateError(w, r, err)
		return
	}
	d := s.playbackDecoration(r)
	for _, playlist := range playlists {
		entry, err := subsonicPlaylistResponse(r, s, playlist, false, d)
		if err != nil {
			subsonicStateError(w, r, err)
			return
		}
		out.Playlists.Playlists = append(out.Playlists.Playlists, entry)
	}
	writeSubsonic(w, r, out)
}

func (s *Server) subsonicStatePlaylist(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("id")), 10, 64)
	if err != nil || id < 1 {
		subsonicStateError(w, r, music.ErrInvalidInput)
		return
	}
	state, ok := s.playbackState(r)
	if !ok {
		subsonicStateError(w, r, sql.ErrNoRows)
		return
	}
	playlist, err := state.Playlist(r.Context(), subsonicRequestUser(r).ID, id)
	if err != nil {
		subsonicStateError(w, r, err)
		return
	}
	entry, err := subsonicPlaylistResponse(r, s, playlist, true, s.playbackDecoration(r))
	if err != nil {
		subsonicStateError(w, r, err)
		return
	}
	out := subsonicResponseFor(s.version)
	out.Playlist = &entry
	writeSubsonic(w, r, out)
}

func subsonicPlaylistChanges(r *http.Request) (music.PlaylistChanges, error) {
	changes := music.PlaylistChanges{}
	var err error
	if values, exists := r.URL.Query()["name"]; exists {
		if len(values) != 1 {
			return changes, music.ErrInvalidInput
		}
		value := values[0]
		changes.Name = &value
	}
	if values, exists := r.URL.Query()["comment"]; exists {
		if len(values) != 1 {
			return changes, music.ErrInvalidInput
		}
		value := values[0]
		changes.Comment = &value
	}
	if _, exists := r.URL.Query()["public"]; exists {
		value, boolErr := subsonicBool(r, "public", false)
		if boolErr != nil {
			return changes, boolErr
		}
		changes.Public = &value
	}
	if changes.Add, err = subsonicIDs(r.URL.Query()["songIdToAdd"], subsonicSongPrefix); err != nil {
		return changes, err
	}
	if changes.Remove, err = subsonicIDs(r.URL.Query()["songIdToRemove"], subsonicSongPrefix); err != nil {
		return changes, err
	}
	if changes.RemoveIndexes, err = subsonicInts(r.URL.Query()["songIndexToRemove"]); err != nil {
		return changes, err
	}
	return changes, nil
}

func (s *Server) subsonicStatePlaylistWrite(w http.ResponseWriter, r *http.Request) {
	state, ok := s.playbackStateForWrite(w, r)
	if !ok {
		return
	}
	userID := subsonicRequestUser(r).ID
	action := subsonicAction(r)
	if action == "deleteplaylist" {
		id, err := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("id")), 10, 64)
		if err != nil || id < 1 {
			subsonicStateError(w, r, music.ErrInvalidInput)
			return
		}
		if err = state.DeletePlaylist(r.Context(), userID, id); err != nil {
			subsonicStateError(w, r, err)
			return
		}
		writeSubsonic(w, r, subsonicResponseFor(s.version))
		return
	}

	if action == "createplaylist" {
		entries, err := subsonicIDs(r.URL.Query()["songId"], subsonicSongPrefix)
		if err != nil {
			subsonicStateError(w, r, err)
			return
		}
		changes, err := subsonicPlaylistChanges(r)
		if err != nil {
			subsonicStateError(w, r, err)
			return
		}
		var playlist music.PlaybackPlaylist
		rawID := strings.TrimSpace(r.URL.Query().Get("playlistId"))
		if rawID != "" {
			id, parseErr := strconv.ParseInt(rawID, 10, 64)
			if parseErr != nil || id < 1 {
				subsonicStateError(w, r, music.ErrInvalidInput)
				return
			}
			if _, exists := r.URL.Query()["songId"]; exists {
				changes.Replace = &entries
			}
			playlist, err = state.UpdatePlaylist(r.Context(), userID, id, changes)
		} else {
			name, comment, public := "", "", false
			if changes.Name != nil {
				name = *changes.Name
			}
			if changes.Comment != nil {
				comment = *changes.Comment
			}
			if changes.Public != nil {
				public = *changes.Public
			}
			// Add/remove/index parameters belong to updatePlaylist, not creation.
			if len(changes.Add) > 0 || len(changes.Remove) > 0 || len(changes.RemoveIndexes) > 0 {
				subsonicStateError(w, r, music.ErrInvalidInput)
				return
			}
			playlist, err = state.CreatePlaylist(r.Context(), userID, name, comment, public, entries)
		}
		if err != nil {
			subsonicStateError(w, r, err)
			return
		}
		entry, err := subsonicPlaylistResponse(r, s, playlist, true, s.playbackDecoration(r))
		if err != nil {
			subsonicStateError(w, r, err)
			return
		}
		out := subsonicResponseFor(s.version)
		out.Playlist = &entry
		writeSubsonic(w, r, out)
		return
	}

	id, err := strconv.ParseInt(strings.TrimSpace(firstNonempty(r.URL.Query().Get("playlistId"), r.URL.Query().Get("id"))), 10, 64)
	if err != nil || id < 1 {
		subsonicStateError(w, r, music.ErrInvalidInput)
		return
	}
	changes, err := subsonicPlaylistChanges(r)
	if err != nil {
		subsonicStateError(w, r, err)
		return
	}
	playlist, err := state.UpdatePlaylist(r.Context(), userID, id, changes)
	if err != nil {
		subsonicStateError(w, r, err)
		return
	}
	entry, err := subsonicPlaylistResponse(r, s, playlist, true, s.playbackDecoration(r))
	if err != nil {
		subsonicStateError(w, r, err)
		return
	}
	out := subsonicResponseFor(s.version)
	out.Playlist = &entry
	writeSubsonic(w, r, out)
}

func firstNonempty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func (s *Server) subsonicStateStar(w http.ResponseWriter, r *http.Request, starred bool) {
	state, ok := s.playbackStateForWrite(w, r)
	if !ok {
		return
	}
	targets := []music.PlaybackMarkTarget{}
	for _, group := range []struct {
		kind   string
		keys   []string
		prefix string
	}{
		{"song", []string{"id", "songId"}, subsonicSongPrefix},
		{"album", []string{"albumId"}, subsonicAlbumPrefix},
		{"artist", []string{"artistId"}, subsonicArtistPrefix},
	} {
		values := []string{}
		for _, key := range group.keys {
			values = append(values, r.URL.Query()[key]...)
		}
		ids, err := subsonicIDs(values, group.prefix)
		if err != nil {
			subsonicStateError(w, r, err)
			return
		}
		for _, id := range ids {
			targets = append(targets, music.PlaybackMarkTarget{Kind: group.kind, ObjectID: id})
		}
	}
	if len(targets) == 0 || len(targets) > 500 {
		subsonicStateError(w, r, music.ErrInvalidInput)
		return
	}
	if err := state.SetStarredBatch(r.Context(), subsonicRequestUser(r).ID, targets, starred); err != nil {
		subsonicStateError(w, r, err)
		return
	}
	writeSubsonic(w, r, subsonicResponseFor(s.version))
}

func subsonicRatingID(r *http.Request) (string, int64, error) {
	if values := r.URL.Query()["id"]; len(values) != 1 {
		return "", 0, music.ErrInvalidInput
	}
	raw := strings.TrimSpace(r.URL.Query().Get("id"))
	for _, candidate := range []struct{ kind, prefix string }{{"song", subsonicSongPrefix}, {"album", subsonicAlbumPrefix}, {"artist", subsonicArtistPrefix}} {
		if id, ok := subsonicParseID(raw, candidate.prefix); ok {
			return candidate.kind, id, nil
		}
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id < 1 {
		return "", 0, music.ErrInvalidInput
	}
	// Bare ids retain the pre-namespaced client behaviour: they mean a song.
	return "song", id, nil
}

func (s *Server) subsonicStateRating(w http.ResponseWriter, r *http.Request) {
	state, ok := s.playbackStateForWrite(w, r)
	if !ok {
		return
	}
	kind, id, err := subsonicRatingID(r)
	if err != nil {
		subsonicStateError(w, r, err)
		return
	}
	if values := r.URL.Query()["rating"]; len(values) != 1 {
		subsonicStateError(w, r, music.ErrInvalidInput)
		return
	}
	rating, err := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("rating")))
	if err != nil || rating < 0 || rating > 5 {
		subsonicStateError(w, r, music.ErrInvalidInput)
		return
	}
	if err = state.SetRating(r.Context(), subsonicRequestUser(r).ID, kind, id, rating); err != nil {
		subsonicStateError(w, r, err)
		return
	}
	writeSubsonic(w, r, subsonicResponseFor(s.version))
}

func subsonicScrobbleTimes(values []string, count int) ([]int64, error) {
	if len(values) == 0 {
		now := time.Now().Unix()
		out := make([]int64, count)
		for i := range out {
			out[i] = now
		}
		return out, nil
	}
	raw := []string{}
	for _, value := range values {
		raw = append(raw, strings.Split(value, ",")...)
	}
	if len(raw) != count {
		return nil, music.ErrInvalidInput
	}
	out := make([]int64, 0, count)
	for _, value := range raw {
		playedAt, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		if err != nil || playedAt <= 0 {
			return nil, music.ErrInvalidInput
		}
		// Subsonic specifies milliseconds; accept legacy seconds as well.
		if playedAt > 100000000000 {
			playedAt /= 1000
		}
		out = append(out, playedAt)
	}
	return out, nil
}

func (s *Server) subsonicStateScrobble(w http.ResponseWriter, r *http.Request) {
	state, ok := s.playbackStateForWrite(w, r)
	if !ok {
		return
	}
	account := subsonicRequestUser(r)
	if !account.ScrobblingEnabled {
		s.subsonicDenied(w, r)
		return
	}
	ids, err := subsonicIDs(r.URL.Query()["id"], subsonicSongPrefix)
	if err != nil || len(ids) == 0 {
		subsonicStateError(w, r, music.ErrInvalidInput)
		return
	}
	times, err := subsonicScrobbleTimes(r.URL.Query()["time"], len(ids))
	if err != nil {
		subsonicStateError(w, r, err)
		return
	}
	submission, err := subsonicBool(r, "submission", true)
	if err != nil {
		subsonicStateError(w, r, err)
		return
	}
	items := make([]music.PlaybackScrobble, len(ids))
	for i := range ids {
		items[i] = music.PlaybackScrobble{FileID: ids[i], PlayedAt: times[i]}
	}
	if err = state.ScrobbleBatch(r.Context(), account.ID, items, submission, strings.TrimSpace(r.URL.Query().Get("c"))); err != nil {
		subsonicStateError(w, r, err)
		return
	}
	writeSubsonic(w, r, subsonicResponseFor(s.version))
}

func (s *Server) subsonicStateSaveQueue(w http.ResponseWriter, r *http.Request) {
	state, ok := s.playbackStateForWrite(w, r)
	if !ok {
		return
	}
	entries, err := subsonicIDs(r.URL.Query()["id"], subsonicSongPrefix)
	if err != nil || len(entries) == 0 {
		subsonicStateError(w, r, music.ErrInvalidInput)
		return
	}
	position := int64(0)
	if raw := strings.TrimSpace(r.URL.Query().Get("position")); raw != "" {
		position, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || position < 0 {
			subsonicStateError(w, r, music.ErrInvalidInput)
			return
		}
	}
	userID := subsonicRequestUser(r).ID
	client := strings.TrimSpace(r.URL.Query().Get("c"))
	if subsonicAction(r) == "saveplayqueuebyindex" {
		raw := strings.TrimSpace(r.URL.Query().Get("currentIndex"))
		if raw == "" {
			// Accept the draft's old spelling while emitting the standard one.
			raw = strings.TrimSpace(r.URL.Query().Get("index"))
		}
		index, parseErr := strconv.Atoi(raw)
		if parseErr != nil || index < 0 || index >= len(entries) {
			subsonicStateError(w, r, music.ErrInvalidInput)
			return
		}
		err = state.SaveQueueByIndex(r.Context(), userID, index, position, entries, client)
	} else {
		current, parseErr := subsonicIDs(r.URL.Query()["current"], subsonicSongPrefix)
		if parseErr != nil || len(current) > 1 {
			subsonicStateError(w, r, music.ErrInvalidInput)
			return
		}
		var currentID int64
		if len(current) == 1 {
			currentID = current[0]
		}
		err = state.SaveQueue(r.Context(), userID, currentID, position, entries, client)
	}
	if err != nil {
		subsonicStateError(w, r, err)
		return
	}
	writeSubsonic(w, r, subsonicResponseFor(s.version))
}

func (s *Server) subsonicStateQueue(w http.ResponseWriter, r *http.Request) {
	byIndex := subsonicAction(r) == "getplayqueuebyindex"
	out := subsonicResponseFor(s.version)
	queueResponse := &subsonicPlayQueue{Entry: []subsonicSong{}}
	indexResponse := &subsonicPlayQueueByIndex{Entry: []subsonicSong{}}
	account := subsonicRequestUser(r)
	state, ok := s.playbackState(r)
	if !ok || account.Anonymous() {
		queueResponse.Username = subsonicUserParam(r)
		queueResponse.Changed = time.Unix(0, 0).UTC().Format(time.RFC3339)
		queueResponse.ChangedBy = strings.TrimSpace(r.URL.Query().Get("c"))
		indexResponse.Username = queueResponse.Username
		indexResponse.Changed = queueResponse.Changed
		indexResponse.ChangedBy = queueResponse.ChangedBy
	} else {
		queue, err := state.Queue(r.Context(), account.ID)
		if err != nil {
			subsonicStateError(w, r, err)
			return
		}
		entries, err := subsonicStateSongs(r, s, queue.EntryIDs, s.playbackDecoration(r))
		if err != nil {
			subsonicStateError(w, r, err)
			return
		}
		queueResponse.Entry = entries
		queueResponse.Username = account.Username
		queueResponse.Position = queue.PositionMS
		queueResponse.Changed = time.Unix(queue.ChangedAt, 0).UTC().Format(time.RFC3339)
		queueResponse.ChangedBy = queue.ChangedBy
		if queue.CurrentFileID > 0 {
			queueResponse.Current = subsonicSongID(queue.CurrentFileID)
		}
		indexResponse.Entry = entries
		indexResponse.Username = account.Username
		indexResponse.Position = queue.PositionMS
		indexResponse.Changed = queueResponse.Changed
		indexResponse.ChangedBy = queue.ChangedBy
		if queue.CurrentIndex >= 0 {
			index := queue.CurrentIndex
			indexResponse.CurrentIndex = &index
		}
	}
	if byIndex {
		out.PlayQueueByIndex = indexResponse
	} else {
		out.PlayQueue = queueResponse
	}
	writeSubsonic(w, r, out)
}

func (s *Server) subsonicStateNowPlaying(w http.ResponseWriter, r *http.Request) {
	out := subsonicResponseFor(s.version)
	out.NowPlaying = &subsonicNowPlaying{Entry: []subsonicSong{}}
	state, ok := s.playbackState(r)
	if !ok {
		writeSubsonic(w, r, out)
		return
	}
	items, err := state.AllNowPlaying(r.Context(), 100)
	if err != nil {
		subsonicStateError(w, r, err)
		return
	}
	d := s.playbackDecoration(r)
	now := time.Now()
	for _, item := range items {
		song, err := s.music.ServerSong(r.Context(), item.FileID)
		if err != nil {
			continue
		}
		entry := d.song(subsonicSongFrom(song))
		entry.Username = item.Username
		minutesAgo := int(now.Sub(time.Unix(item.StartedAt, 0)) / time.Minute)
		if minutesAgo < 0 {
			minutesAgo = 0
		}
		entry.MinutesAgo = &minutesAgo
		out.NowPlaying.Entry = append(out.NowPlaying.Entry, entry)
	}
	writeSubsonic(w, r, out)
}

func (s *Server) subsonicStateStarred(w http.ResponseWriter, r *http.Request, second bool) {
	out := subsonicResponseFor(s.version)
	starred := &subsonicStarred{Artists: []subsonicArtist{}, Albums: []subsonicAlbum{}, Songs: []subsonicSong{}}
	account := subsonicRequestUser(r)
	state, ok := s.playbackState(r)
	if ok && !account.Anonymous() {
		snapshot, err := state.Snapshot(r.Context(), account.ID)
		if err != nil {
			subsonicStateError(w, r, err)
			return
		}
		d := decorationForSnapshot(snapshot)
		for _, mark := range snapshot.Marks {
			if mark.StarredAt == 0 {
				continue
			}
			switch mark.Kind {
			case "song":
				song, err := s.music.ServerSong(r.Context(), mark.ObjectID)
				if err == nil {
					starred.Songs = append(starred.Songs, d.song(subsonicSongFrom(song)))
				}
			case "album":
				album, err := s.music.ServerAlbum(r.Context(), mark.ObjectID)
				if err == nil {
					starred.Albums = append(starred.Albums, d.album(subsonicAlbumFrom(album.Album)))
				}
			case "artist":
				artist, err := s.music.Artist(r.Context(), mark.ObjectID)
				if err == nil {
					starred.Artists = append(starred.Artists, d.artist(subsonicArtistFrom(artist.Artist)))
				}
			}
		}
	}
	if second {
		out.Starred2 = starred
	} else {
		out.Starred = starred
	}
	writeSubsonic(w, r, out)
}

type subsonicAlbumCandidate struct {
	id      int64
	rating  int
	starred int64
	updated int64
	count   int
	played  int64
}

// subsonicStatefulAlbums implements starred/highest/frequent/recent from the
// caller's marks and events, never from unrelated global album timestamps.
func (s *Server) subsonicStatefulAlbums(r *http.Request, size, offset int) ([]subsonicAlbum, bool, error) {
	typeName := r.URL.Query().Get("type")
	if typeName != "starred" && typeName != "highest" && typeName != "frequent" && typeName != "recent" {
		return nil, false, nil
	}
	empty := []subsonicAlbum{}
	account := subsonicRequestUser(r)
	state, ok := s.playbackState(r)
	if !ok || account.Anonymous() {
		return empty, true, nil
	}
	snapshot, err := state.Snapshot(r.Context(), account.ID)
	if err != nil {
		return nil, true, err
	}
	candidates := []subsonicAlbumCandidate{}
	switch typeName {
	case "starred", "highest":
		for _, mark := range snapshot.Marks {
			if mark.Kind != "album" || (typeName == "starred" && mark.StarredAt == 0) || (typeName == "highest" && mark.Rating == 0) {
				continue
			}
			candidates = append(candidates, subsonicAlbumCandidate{id: mark.ObjectID, rating: mark.Rating, starred: mark.StarredAt, updated: mark.UpdatedAt})
		}
	case "frequent":
		for id, count := range snapshot.AlbumPlayCounts {
			if count > 0 {
				candidates = append(candidates, subsonicAlbumCandidate{id: id, count: count, played: snapshot.AlbumLastPlayed[id]})
			}
		}
	case "recent":
		for id, played := range snapshot.AlbumLastPlayed {
			if played > 0 {
				candidates = append(candidates, subsonicAlbumCandidate{id: id, played: played, count: snapshot.AlbumPlayCounts[id]})
			}
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		switch typeName {
		case "starred":
			if a.starred != b.starred {
				return a.starred > b.starred
			}
		case "highest":
			if a.rating != b.rating {
				return a.rating > b.rating
			}
			if a.updated != b.updated {
				return a.updated > b.updated
			}
		case "frequent":
			if a.count != b.count {
				return a.count > b.count
			}
			if a.played != b.played {
				return a.played > b.played
			}
		case "recent":
			if a.played != b.played {
				return a.played > b.played
			}
		}
		return a.id < b.id
	})
	if offset > len(candidates) {
		offset = len(candidates)
	}
	end := offset + size
	if end > len(candidates) {
		end = len(candidates)
	}
	d := decorationForSnapshot(snapshot)
	for _, candidate := range candidates[offset:end] {
		album, err := s.music.ServerAlbum(r.Context(), candidate.id)
		if err != nil {
			continue
		}
		empty = append(empty, d.album(subsonicAlbumFrom(album.Album)))
	}
	return empty, true, nil
}
