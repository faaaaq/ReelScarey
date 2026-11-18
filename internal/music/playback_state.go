package music

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

const (
	maxPlaybackBatch = 500
	maxNowPlaying    = 100
	nowPlayingTTL    = 10 * time.Minute
)

type PlaybackState struct{ db *sql.DB }

func (s *Service) PlaybackState() *PlaybackState { return &PlaybackState{db: s.store.db} }

type PlaybackMark struct {
	Kind      string `json:"kind"`
	ObjectID  int64  `json:"object_id"`
	StarredAt int64  `json:"starred_at"`
	Rating    int    `json:"rating"`
	UpdatedAt int64  `json:"updated_at"`
}

type PlaybackMarkTarget struct {
	Kind     string
	ObjectID int64
}

type PlaybackPlaylist struct {
	ID         int64   `json:"id"`
	OwnerID    int64   `json:"owner_id"`
	Owner      string  `json:"owner"`
	Name       string  `json:"name"`
	Comment    string  `json:"comment"`
	Public     bool    `json:"public"`
	CreatedAt  int64   `json:"created_at"`
	UpdatedAt  int64   `json:"updated_at"`
	DurationMS int64   `json:"duration_ms"`
	EntryIDs   []int64 `json:"entry_ids"`
}

type PlaybackQueue struct {
	CurrentFileID int64   `json:"current_file_id,omitempty"`
	CurrentIndex  int     `json:"current_index"`
	PositionMS    int64   `json:"position_ms"`
	EntryIDs      []int64 `json:"entry_ids"`
	ChangedAt     int64   `json:"changed_at"`
	ChangedBy     string  `json:"changed_by"`
}

type PlaybackScrobble struct {
	FileID   int64
	PlayedAt int64
}

type NowPlaying struct {
	UserID    int64  `json:"user_id"`
	Username  string `json:"username"`
	FileID    int64  `json:"file_id"`
	StartedAt int64  `json:"started_at"`
	Client    string `json:"client"`
}

// PlaybackSnapshot is the complete per-user decoration and album-list state.
// Event rows are grouped in SQL, so its size is bounded by the catalog rather
// than by the number of scrobbles.
type PlaybackSnapshot struct {
	Marks            []PlaybackMark
	SongPlayCounts   map[int64]int
	AlbumPlayCounts  map[int64]int
	ArtistPlayCounts map[int64]int
	SongLastPlayed   map[int64]int64
	AlbumLastPlayed  map[int64]int64
	ArtistLastPlayed map[int64]int64
}

type playbackQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func playbackUser(ctx context.Context, q playbackQuerier, userID int64) error {
	if userID < 1 {
		return ErrInvalidInput
	}
	var exists int
	if err := q.QueryRowContext(ctx, `SELECT 1 FROM music_server_users WHERE id=?`, userID).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrInvalidInput
		}
		return err
	}
	return nil
}

func playbackObject(ctx context.Context, q playbackQuerier, kind string, id int64) error {
	if id < 1 {
		return ErrInvalidInput
	}
	table := map[string]string{"song": "music_files", "album": "music_albums", "artist": "music_artists"}[kind]
	if table == "" {
		return ErrInvalidInput
	}
	var exists int
	if err := q.QueryRowContext(ctx, `SELECT 1 FROM `+table+` WHERE id=?`, id).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return sql.ErrNoRows
		}
		return err
	}
	return nil
}

func playbackFiles(ctx context.Context, q playbackQuerier, ids []int64) error {
	if len(ids) > maxPlaybackBatch {
		return ErrInvalidInput
	}
	for _, id := range ids {
		if err := playbackObject(ctx, q, "song", id); err != nil {
			return err
		}
	}
	return nil
}

func (p *PlaybackState) SetStarred(ctx context.Context, userID int64, kind string, id int64, starred bool) error {
	return p.SetStarredBatch(ctx, userID, []PlaybackMarkTarget{{Kind: kind, ObjectID: id}}, starred)
}

// SetStarredBatch validates every target before changing any row. A malformed
// or missing id in a multi-kind request therefore cannot leave a partial star.
func (p *PlaybackState) SetStarredBatch(ctx context.Context, userID int64, targets []PlaybackMarkTarget, starred bool) error {
	if len(targets) == 0 || len(targets) > maxPlaybackBatch {
		return ErrInvalidInput
	}
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = playbackUser(ctx, tx, userID); err != nil {
		return err
	}
	for _, target := range targets {
		if err = playbackObject(ctx, tx, target.Kind, target.ObjectID); err != nil {
			return err
		}
	}
	for _, target := range targets {
		if starred {
			_, err = tx.ExecContext(ctx, `INSERT INTO music_playback_marks(user_id,kind,object_id,starred_at,updated_at) VALUES(?,?,?,unixepoch(),unixepoch()) ON CONFLICT(user_id,kind,object_id) DO UPDATE SET starred_at=CASE WHEN music_playback_marks.starred_at=0 THEN unixepoch() ELSE music_playback_marks.starred_at END,updated_at=unixepoch()`, userID, target.Kind, target.ObjectID)
		} else {
			_, err = tx.ExecContext(ctx, `UPDATE music_playback_marks SET starred_at=0,updated_at=unixepoch() WHERE user_id=? AND kind=? AND object_id=?`, userID, target.Kind, target.ObjectID)
			if err == nil {
				_, err = tx.ExecContext(ctx, `DELETE FROM music_playback_marks WHERE user_id=? AND kind=? AND object_id=? AND starred_at=0 AND rating=0`, userID, target.Kind, target.ObjectID)
			}
		}
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (p *PlaybackState) SetRating(ctx context.Context, userID int64, kind string, id int64, rating int) error {
	if rating < 0 || rating > 5 {
		return ErrInvalidInput
	}
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = playbackUser(ctx, tx, userID); err != nil {
		return err
	}
	if err = playbackObject(ctx, tx, kind, id); err != nil {
		return err
	}
	if rating > 0 {
		if _, err = tx.ExecContext(ctx, `INSERT INTO music_playback_marks(user_id,kind,object_id,rating,updated_at) VALUES(?,?,?,?,unixepoch()) ON CONFLICT(user_id,kind,object_id) DO UPDATE SET rating=excluded.rating,updated_at=excluded.updated_at`, userID, kind, id, rating); err != nil {
			return err
		}
		return tx.Commit()
	}
	if _, err = tx.ExecContext(ctx, `UPDATE music_playback_marks SET rating=0,updated_at=unixepoch() WHERE user_id=? AND kind=? AND object_id=?`, userID, kind, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM music_playback_marks WHERE user_id=? AND kind=? AND object_id=? AND starred_at=0 AND rating=0`, userID, kind, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (p *PlaybackState) Marks(ctx context.Context, userID int64) ([]PlaybackMark, error) {
	if err := playbackUser(ctx, p.db, userID); err != nil {
		return nil, err
	}
	rows, err := p.db.QueryContext(ctx, `SELECT kind,object_id,starred_at,rating,updated_at FROM music_playback_marks WHERE user_id=? ORDER BY kind,object_id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PlaybackMark{}
	for rows.Next() {
		var mark PlaybackMark
		if err := rows.Scan(&mark.Kind, &mark.ObjectID, &mark.StarredAt, &mark.Rating, &mark.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, mark)
	}
	return out, rows.Err()
}

func (p *PlaybackState) Snapshot(ctx context.Context, userID int64) (PlaybackSnapshot, error) {
	out := PlaybackSnapshot{
		SongPlayCounts: map[int64]int{}, AlbumPlayCounts: map[int64]int{}, ArtistPlayCounts: map[int64]int{},
		SongLastPlayed: map[int64]int64{}, AlbumLastPlayed: map[int64]int64{}, ArtistLastPlayed: map[int64]int64{},
	}
	marks, err := p.Marks(ctx, userID)
	if err != nil {
		return out, err
	}
	out.Marks = marks
	rows, err := p.db.QueryContext(ctx, `SELECT f.id,f.album_id,f.artist_id,COUNT(*),MAX(e.played_at)
		FROM music_play_events e JOIN music_files f ON f.id=e.music_file_id
		WHERE e.user_id=? GROUP BY f.id,f.album_id,f.artist_id`, userID)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var fileID, albumID, artistID, last int64
		var count int
		if err := rows.Scan(&fileID, &albumID, &artistID, &count, &last); err != nil {
			return PlaybackSnapshot{}, err
		}
		out.SongPlayCounts[fileID] = count
		out.AlbumPlayCounts[albumID] += count
		out.ArtistPlayCounts[artistID] += count
		out.SongLastPlayed[fileID] = last
		if last > out.AlbumLastPlayed[albumID] {
			out.AlbumLastPlayed[albumID] = last
		}
		if last > out.ArtistLastPlayed[artistID] {
			out.ArtistLastPlayed[artistID] = last
		}
	}
	return out, rows.Err()
}

func validPlaylistText(name, comment string) bool {
	return strings.TrimSpace(name) != "" && len(name) <= 200 && len(comment) <= 2000 && !strings.ContainsAny(name+comment, "\x00")
}

func (p *PlaybackState) CreatePlaylist(ctx context.Context, userID int64, name, comment string, public bool, entryIDs []int64) (PlaybackPlaylist, error) {
	name = strings.TrimSpace(name)
	if !validPlaylistText(name, comment) || len(entryIDs) > maxPlaybackBatch {
		return PlaybackPlaylist{}, ErrInvalidInput
	}
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return PlaybackPlaylist{}, err
	}
	defer tx.Rollback()
	if err = playbackUser(ctx, tx, userID); err != nil {
		return PlaybackPlaylist{}, err
	}
	if err = playbackFiles(ctx, tx, entryIDs); err != nil {
		return PlaybackPlaylist{}, err
	}
	now := time.Now().Unix()
	result, err := tx.ExecContext(ctx, `INSERT INTO music_playlists(user_id,name,comment,public,created_at,updated_at) VALUES(?,?,?,?,?,?)`, userID, name, comment, public, now, now)
	if err != nil {
		return PlaybackPlaylist{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return PlaybackPlaylist{}, err
	}
	for i, fileID := range entryIDs {
		if _, err = tx.ExecContext(ctx, `INSERT INTO music_playlist_entries(playlist_id,position,music_file_id) VALUES(?,?,?)`, id, i+1, fileID); err != nil {
			return PlaybackPlaylist{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return PlaybackPlaylist{}, err
	}
	return p.Playlist(ctx, userID, id)
}

func (p *PlaybackState) playlistTx(ctx context.Context, q playbackQuerier, userID, playlistID int64, ownerOnly bool) (PlaybackPlaylist, error) {
	var out PlaybackPlaylist
	where := `p.id=? AND (p.user_id=? OR p.public=1)`
	if ownerOnly {
		where = `p.id=? AND p.user_id=?`
	}
	err := q.QueryRowContext(ctx, `SELECT p.id,p.user_id,u.username,p.name,p.comment,p.public,p.created_at,p.updated_at,
		(SELECT COALESCE(SUM(f.duration_ms),0) FROM music_playlist_entries e JOIN music_files f ON f.id=e.music_file_id WHERE e.playlist_id=p.id)
		FROM music_playlists p JOIN music_server_users u ON u.id=p.user_id WHERE `+where, playlistID, userID).
		Scan(&out.ID, &out.OwnerID, &out.Owner, &out.Name, &out.Comment, &out.Public, &out.CreatedAt, &out.UpdatedAt, &out.DurationMS)
	if err != nil {
		return out, err
	}
	rows, err := q.QueryContext(ctx, `SELECT music_file_id FROM music_playlist_entries WHERE playlist_id=? ORDER BY position`, playlistID)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	out.EntryIDs = []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return out, err
		}
		out.EntryIDs = append(out.EntryIDs, id)
	}
	return out, rows.Err()
}

// Playlist permits owner and public reads. Writes use playlistTx ownerOnly.
func (p *PlaybackState) Playlist(ctx context.Context, userID, playlistID int64) (PlaybackPlaylist, error) {
	if playlistID < 1 {
		return PlaybackPlaylist{}, ErrInvalidInput
	}
	return p.playlistTx(ctx, p.db, userID, playlistID, false)
}

func (p *PlaybackState) Playlists(ctx context.Context, userID int64) ([]PlaybackPlaylist, error) {
	rows, err := p.db.QueryContext(ctx, `SELECT id FROM music_playlists WHERE user_id=? OR public=1 ORDER BY updated_at DESC,id DESC LIMIT 100`, userID)
	if err != nil {
		return nil, err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	out := []PlaybackPlaylist{}
	for _, id := range ids {
		playlist, err := p.Playlist(ctx, userID, id)
		if err != nil {
			return nil, err
		}
		out = append(out, playlist)
	}
	return out, nil
}

type PlaylistChanges struct {
	Name          *string
	Comment       *string
	Public        *bool
	Replace       *[]int64
	Add           []int64
	Remove        []int64
	RemoveIndexes []int
}

func (p *PlaybackState) UpdatePlaylist(ctx context.Context, userID, playlistID int64, changes PlaylistChanges) (PlaybackPlaylist, error) {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return PlaybackPlaylist{}, err
	}
	defer tx.Rollback()
	if err = playbackUser(ctx, tx, userID); err != nil {
		return PlaybackPlaylist{}, err
	}
	playlist, err := p.playlistTx(ctx, tx, userID, playlistID, true)
	if err != nil {
		return PlaybackPlaylist{}, err
	}
	if changes.Name != nil {
		playlist.Name = strings.TrimSpace(*changes.Name)
	}
	if changes.Comment != nil {
		playlist.Comment = *changes.Comment
	}
	if changes.Public != nil {
		playlist.Public = *changes.Public
	}
	if !validPlaylistText(playlist.Name, playlist.Comment) {
		return PlaybackPlaylist{}, ErrInvalidInput
	}
	entries := append([]int64(nil), playlist.EntryIDs...)
	if changes.Replace != nil {
		entries = append([]int64(nil), (*changes.Replace)...)
		if err = playbackFiles(ctx, tx, entries); err != nil {
			return PlaybackPlaylist{}, err
		}
	}
	if err = playbackFiles(ctx, tx, changes.Add); err != nil {
		return PlaybackPlaylist{}, err
	}
	if err = playbackFiles(ctx, tx, changes.Remove); err != nil {
		return PlaybackPlaylist{}, err
	}
	remove := map[int64]bool{}
	for _, id := range changes.Remove {
		remove[id] = true
	}
	removeIndexes := map[int]bool{}
	for _, index := range changes.RemoveIndexes {
		if index < 0 || index >= len(entries) {
			return PlaybackPlaylist{}, ErrInvalidInput
		}
		removeIndexes[index] = true
	}
	if len(remove) > 0 || len(removeIndexes) > 0 {
		kept := []int64{}
		for i, id := range entries {
			if !remove[id] && !removeIndexes[i] {
				kept = append(kept, id)
			}
		}
		entries = kept
	}
	entries = append(entries, changes.Add...)
	if len(entries) > maxPlaybackBatch {
		return PlaybackPlaylist{}, ErrInvalidInput
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM music_playlist_entries WHERE playlist_id=?`, playlistID); err != nil {
		return PlaybackPlaylist{}, err
	}
	for i, id := range entries {
		if _, err = tx.ExecContext(ctx, `INSERT INTO music_playlist_entries(playlist_id,position,music_file_id) VALUES(?,?,?)`, playlistID, i+1, id); err != nil {
			return PlaybackPlaylist{}, err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE music_playlists SET name=?,comment=?,public=?,updated_at=unixepoch() WHERE id=? AND user_id=?`, playlist.Name, playlist.Comment, playlist.Public, playlistID, userID); err != nil {
		return PlaybackPlaylist{}, err
	}
	if err = tx.Commit(); err != nil {
		return PlaybackPlaylist{}, err
	}
	return p.Playlist(ctx, userID, playlistID)
}

func (p *PlaybackState) DeletePlaylist(ctx context.Context, userID, playlistID int64) error {
	if userID < 1 || playlistID < 1 {
		return ErrInvalidInput
	}
	result, err := p.db.ExecContext(ctx, `DELETE FROM music_playlists WHERE id=? AND user_id=?`, playlistID, userID)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return sql.ErrNoRows
	}
	return nil
}

// SaveQueue is the ID-based Subsonic wrapper. With duplicate song IDs it picks
// the first matching entry; index-based clients use SaveQueueByIndex instead.
func (p *PlaybackState) SaveQueue(ctx context.Context, userID, currentID, positionMS int64, entryIDs []int64, changedBy string) error {
	index := -1
	if currentID > 0 {
		for i, id := range entryIDs {
			if id == currentID {
				index = i
				break
			}
		}
		if index < 0 {
			return ErrInvalidInput
		}
	} else if currentID < 0 {
		return ErrInvalidInput
	}
	return p.saveQueue(ctx, userID, index, positionMS, entryIDs, changedBy)
}

func (p *PlaybackState) SaveQueueByIndex(ctx context.Context, userID int64, currentIndex int, positionMS int64, entryIDs []int64, changedBy string) error {
	if currentIndex < 0 || currentIndex >= len(entryIDs) {
		return ErrInvalidInput
	}
	return p.saveQueue(ctx, userID, currentIndex, positionMS, entryIDs, changedBy)
}

func (p *PlaybackState) saveQueue(ctx context.Context, userID int64, currentIndex int, positionMS int64, entryIDs []int64, changedBy string) error {
	if positionMS < 0 || positionMS > 31*24*60*60*1000 || len(changedBy) > 200 || len(entryIDs) > maxPlaybackBatch || currentIndex >= len(entryIDs) || currentIndex < -1 {
		return ErrInvalidInput
	}
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = playbackUser(ctx, tx, userID); err != nil {
		return err
	}
	if err = playbackFiles(ctx, tx, entryIDs); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM music_play_queue_entries WHERE user_id=?`, userID); err != nil {
		return err
	}
	for i, id := range entryIDs {
		if _, err = tx.ExecContext(ctx, `INSERT INTO music_play_queue_entries(user_id,position,music_file_id) VALUES(?,?,?)`, userID, i+1, id); err != nil {
			return err
		}
	}
	var current, index any
	if currentIndex >= 0 {
		current = entryIDs[currentIndex]
		index = currentIndex
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO music_play_queue(user_id,current_file_id,current_index,position_ms,changed_at,changed_by) VALUES(?,?,?,?,unixepoch(),?) ON CONFLICT(user_id) DO UPDATE SET current_file_id=excluded.current_file_id,current_index=excluded.current_index,position_ms=excluded.position_ms,changed_at=excluded.changed_at,changed_by=excluded.changed_by`, userID, current, index, positionMS, changedBy); err != nil {
		return err
	}
	return tx.Commit()
}

func (p *PlaybackState) Queue(ctx context.Context, userID int64) (PlaybackQueue, error) {
	out := PlaybackQueue{CurrentIndex: -1, EntryIDs: []int64{}}
	if err := playbackUser(ctx, p.db, userID); err != nil {
		return out, err
	}
	var current, index sql.NullInt64
	err := p.db.QueryRowContext(ctx, `SELECT current_file_id,current_index,position_ms,changed_at,changed_by FROM music_play_queue WHERE user_id=?`, userID).Scan(&current, &index, &out.PositionMS, &out.ChangedAt, &out.ChangedBy)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	if current.Valid {
		out.CurrentFileID = current.Int64
	}
	rows, err := p.db.QueryContext(ctx, `SELECT music_file_id FROM music_play_queue_entries WHERE user_id=? ORDER BY position`, userID)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return out, err
		}
		out.EntryIDs = append(out.EntryIDs, id)
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	if index.Valid && index.Int64 >= 0 && index.Int64 < int64(len(out.EntryIDs)) {
		out.CurrentIndex = int(index.Int64)
		out.CurrentFileID = out.EntryIDs[out.CurrentIndex]
	} else if out.CurrentFileID > 0 {
		for i, id := range out.EntryIDs {
			if id == out.CurrentFileID {
				out.CurrentIndex = i
				break
			}
		}
	}
	return out, nil
}

func (p *PlaybackState) Scrobble(ctx context.Context, userID, fileID, playedAt int64, submission bool, client string) error {
	if playedAt <= 0 {
		playedAt = time.Now().Unix()
	}
	return p.ScrobbleBatch(ctx, userID, []PlaybackScrobble{{FileID: fileID, PlayedAt: playedAt}}, submission, client)
}

// ScrobbleBatch commits all repeated id/time pairs together. Out-of-order
// notifications cannot move a user's now-playing marker backwards, and a late
// submission only clears the matching marker it actually completes.
func (p *PlaybackState) ScrobbleBatch(ctx context.Context, userID int64, items []PlaybackScrobble, submission bool, client string) error {
	if len(items) == 0 || len(items) > maxPlaybackBatch || len(client) > 200 {
		return ErrInvalidInput
	}
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = playbackUser(ctx, tx, userID); err != nil {
		return err
	}
	for _, item := range items {
		if item.PlayedAt <= 0 {
			return ErrInvalidInput
		}
		if err = playbackObject(ctx, tx, "song", item.FileID); err != nil {
			return err
		}
	}
	var current NowPlaying
	var currentFile sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT music_file_id,started_at,client FROM music_now_playing WHERE user_id=?`, userID).Scan(&currentFile, &current.StartedAt, &current.Client)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if currentFile.Valid {
		current.FileID = currentFile.Int64
	}
	if submission {
		for _, item := range items {
			if _, err = tx.ExecContext(ctx, `INSERT INTO music_play_events(user_id,music_file_id,played_at,client) VALUES(?,?,?,?)`, userID, item.FileID, item.PlayedAt, client); err != nil {
				return err
			}
		}
		if current.FileID > 0 {
			for _, item := range items {
				if item.FileID == current.FileID && item.PlayedAt >= current.StartedAt {
					if _, err = tx.ExecContext(ctx, `DELETE FROM music_now_playing WHERE user_id=?`, userID); err != nil {
						return err
					}
					break
				}
			}
		}
		return tx.Commit()
	}
	latest := items[0]
	for _, item := range items[1:] {
		if item.PlayedAt > latest.PlayedAt {
			latest = item
		}
	}
	if current.FileID == 0 || latest.PlayedAt > current.StartedAt {
		if _, err = tx.ExecContext(ctx, `INSERT INTO music_now_playing(user_id,music_file_id,started_at,client) VALUES(?,?,?,?) ON CONFLICT(user_id) DO UPDATE SET music_file_id=excluded.music_file_id,started_at=excluded.started_at,client=excluded.client`, userID, latest.FileID, latest.PlayedAt, client); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (p *PlaybackState) PlayCount(ctx context.Context, userID, fileID int64) (int, error) {
	if err := playbackUser(ctx, p.db, userID); err != nil {
		return 0, err
	}
	var count int
	if err := p.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM music_play_events WHERE user_id=? AND music_file_id=?`, userID, fileID).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func (p *PlaybackState) nowPlaying(ctx context.Context, userID int64, all bool, limit int) ([]NowPlaying, error) {
	where := `n.started_at>=?`
	args := []any{time.Now().Add(-nowPlayingTTL).Unix()}
	if !all {
		if err := playbackUser(ctx, p.db, userID); err != nil {
			return nil, err
		}
		where += ` AND n.user_id=?`
		args = append(args, userID)
	}
	if limit < 1 || limit > maxNowPlaying {
		limit = maxNowPlaying
	}
	args = append(args, limit)
	rows, err := p.db.QueryContext(ctx, `SELECT n.user_id,u.username,n.music_file_id,n.started_at,n.client FROM music_now_playing n JOIN music_server_users u ON u.id=n.user_id WHERE `+where+` ORDER BY n.started_at DESC,n.user_id LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []NowPlaying{}
	for rows.Next() {
		var item NowPlaying
		if err := rows.Scan(&item.UserID, &item.Username, &item.FileID, &item.StartedAt, &item.Client); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (p *PlaybackState) NowPlaying(ctx context.Context, userID int64) ([]NowPlaying, error) {
	return p.nowPlaying(ctx, userID, false, 1)
}

func (p *PlaybackState) AllNowPlaying(ctx context.Context, limit int) ([]NowPlaying, error) {
	return p.nowPlaying(ctx, 0, true, limit)
}
