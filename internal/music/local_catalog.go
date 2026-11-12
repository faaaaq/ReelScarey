package music

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Local catalog identities are derived from embedded tags, never from a
// provider. The prefix is part of the stored identifier so a local row can
// never be mistaken for a MusicBrainz row and is never written into a
// MusicBrainz tag field.
const (
	localArtistIDPrefix = "local-artist:"
	localAlbumIDPrefix  = "local-album:"
	localTrackIDPrefix  = "local-track:"
)

// IsLocalMusicID reports whether an identifier was generated from file tags
// rather than supplied by MusicBrainz.
func IsLocalMusicID(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.HasPrefix(value, localArtistIDPrefix) ||
		strings.HasPrefix(value, localAlbumIDPrefix) ||
		strings.HasPrefix(value, localTrackIDPrefix)
}

// localCatalogID is deterministic for one normalized tag identity. Different
// files that carry the same tags therefore reuse one catalog row, while a
// fresh rerun recomputes the same identifier instead of copying rows.
func localCatalogID(prefix string, parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return prefix + hex.EncodeToString(sum[:16])
}

// tagIdentity is the identity that embedded tags can prove. It is deliberately
// evidence-only: names, titles, numbers and (when present) genuine
// MusicBrainz IDs. It never infers canonical ordering.
type tagIdentity struct {
	artistName    string
	albumTitle    string
	trackTitle    string
	artistKey     string
	albumKey      string
	trackKey      string
	disc          int
	number        int
	year          int
	artistMBID    string
	albumMBID     string
	trackMBID     string
	recordingMBID string
}

// canCreate reports whether the tags are complete enough to create or use a
// local catalog entry: album artist/artist, album, title and a track number.
func (ev tagIdentity) canCreate() bool {
	return ev.artistName != "" && ev.albumTitle != "" && ev.trackTitle != "" &&
		ev.artistKey != "" && ev.albumKey != "" && ev.trackKey != "" && ev.number > 0
}

// tagIdentityFor reads the evidence already stored on the scan row. A missing
// disc tag is not an error; the caller decides whether to default it or to use
// the album's existing track list.
func tagIdentityFor(file ScanFile) tagIdentity {
	artist, artistOK := cleanText(firstNonEmpty(file.AlbumArtist, file.Artist), 500)
	album, albumOK := cleanText(file.Album, 500)
	title, titleOK := cleanText(file.Title, 500)
	ev := tagIdentity{
		artistMBID:    trustedArtistMusicBrainzID(file),
		albumMBID:     strings.ToLower(strings.TrimSpace(file.MusicBrainzReleaseGroupID)),
		trackMBID:     strings.ToLower(strings.TrimSpace(file.MusicBrainzTrackID)),
		recordingMBID: strings.ToLower(strings.TrimSpace(file.MusicBrainzRecordingID)),
	}
	if !ValidMBID(ev.albumMBID) {
		ev.albumMBID = ""
	}
	if !ValidMBID(ev.trackMBID) {
		ev.trackMBID = ""
	}
	if !ValidMBID(ev.recordingMBID) {
		ev.recordingMBID = ""
	}
	if !artistOK || !albumOK || !titleOK {
		return ev
	}
	ev.artistName, ev.albumTitle, ev.trackTitle = artist, album, title
	ev.artistKey = localArtistKey(artist)
	ev.albumKey = compactMatchName(album)
	ev.trackKey = compactMatchName(title)
	if file.Track != nil && *file.Track > 0 && *file.Track <= 100000 {
		ev.number = *file.Track
	}
	if file.Disc != nil && *file.Disc > 0 && *file.Disc <= 100000 {
		ev.disc = *file.Disc
	}
	if file.Year != nil && *file.Year > 0 {
		ev.year = *file.Year
	}
	return ev
}

// localArtistKey folds case, punctuation, spacing and a leading English
// article. It is a tag comparison key, never a MusicBrainz identity.
func localArtistKey(name string) string {
	normalized := normalizeArtistName(name)
	if normalized == "" {
		return ""
	}
	if normalized == "various artists" {
		return normalized
	}
	return strings.ReplaceAll(trimArticle(normalized), " ", "")
}

// withAcoustID keeps the AcoustID evidence together with the tags. The
// release-group and recording identifiers are genuine MusicBrainz IDs even
// though no MusicBrainz request was made.
func (ev tagIdentity) withAcoustID(releaseGroupID, recordingID string) tagIdentity {
	if ValidMBID(releaseGroupID) {
		ev.albumMBID = strings.ToLower(strings.TrimSpace(releaseGroupID))
	}
	if ValidMBID(recordingID) {
		ev.recordingMBID = strings.ToLower(strings.TrimSpace(recordingID))
	}
	return ev
}

type albumRef struct {
	id       int64
	artistID int64
	mbid     string
	title    string
}

// localCatalog caches the artist, album and track identities used by one
// matching run. Artists are loaded once; albums and tracks are loaded once per
// artist/album, so a large well-tagged batch does not run several catalog
// queries per file.
type localCatalog struct {
	service *Service

	artistsLoaded bool
	artistsByMBID map[string]int64
	artistsByKey  map[string][]int64

	albumsLoaded   map[int64]bool
	albumsByMBID   map[string]albumRef
	albumsByArtist map[int64]map[string][]albumRef

	tracksLoaded  map[int64]bool
	tracksByAlbum map[int64][]Track
}

func newLocalCatalog(service *Service) *localCatalog {
	return &localCatalog{
		service:        service,
		artistsByMBID:  map[string]int64{},
		artistsByKey:   map[string][]int64{},
		albumsLoaded:   map[int64]bool{},
		albumsByMBID:   map[string]albumRef{},
		albumsByArtist: map[int64]map[string][]albumRef{},
		tracksLoaded:   map[int64]bool{},
		tracksByAlbum:  map[int64][]Track{},
	}
}

func (c *localCatalog) loadArtists(ctx context.Context) error {
	if c.artistsLoaded {
		return nil
	}
	rows, err := c.service.store.db.QueryContext(ctx, `SELECT id,musicbrainz_id,name FROM music_artists`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var mbid, name string
		if err := rows.Scan(&id, &mbid, &name); err != nil {
			return err
		}
		mbid = strings.ToLower(strings.TrimSpace(mbid))
		if mbid != "" {
			c.artistsByMBID[mbid] = id
		}
		if key := localArtistKey(name); key != "" {
			c.artistsByKey[key] = append(c.artistsByKey[key], id)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	c.artistsLoaded = true
	return nil
}

func (c *localCatalog) loadAlbums(ctx context.Context, artistID int64) error {
	if c.albumsLoaded[artistID] {
		return nil
	}
	rows, err := c.service.store.db.QueryContext(ctx, `SELECT id,artist_id,musicbrainz_id,title FROM music_albums WHERE artist_id=? ORDER BY id`, artistID)
	if err != nil {
		return err
	}
	defer rows.Close()
	byKey := map[string][]albumRef{}
	for rows.Next() {
		var ref albumRef
		if err := rows.Scan(&ref.id, &ref.artistID, &ref.mbid, &ref.title); err != nil {
			return err
		}
		ref.mbid = strings.ToLower(strings.TrimSpace(ref.mbid))
		c.albumsByMBID[ref.mbid] = ref
		if key := compactMatchName(ref.title); key != "" {
			byKey[key] = append(byKey[key], ref)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	c.albumsByArtist[artistID] = byKey
	c.albumsLoaded[artistID] = true
	return nil
}

func (c *localCatalog) findAlbumByMBID(ctx context.Context, mbid string) (albumRef, bool, error) {
	if mbid == "" {
		return albumRef{}, false, nil
	}
	if ref, ok := c.albumsByMBID[mbid]; ok {
		return ref, true, nil
	}
	var ref albumRef
	err := c.service.store.db.QueryRowContext(ctx, `SELECT id,artist_id,musicbrainz_id,title FROM music_albums WHERE musicbrainz_id=? ORDER BY id LIMIT 1`, mbid).
		Scan(&ref.id, &ref.artistID, &ref.mbid, &ref.title)
	if errors.Is(err, sql.ErrNoRows) {
		return albumRef{}, false, nil
	}
	if err != nil {
		return albumRef{}, false, err
	}
	ref.mbid = strings.ToLower(strings.TrimSpace(ref.mbid))
	c.albumsByMBID[ref.mbid] = ref
	if c.albumsLoaded[ref.artistID] {
		key := compactMatchName(ref.title)
		if key != "" {
			c.albumsByArtist[ref.artistID][key] = append(c.albumsByArtist[ref.artistID][key], ref)
		}
	}
	return ref, true, nil
}

func (c *localCatalog) albumsByTitle(ctx context.Context, artistID int64, title string) ([]albumRef, error) {
	if err := c.loadAlbums(ctx, artistID); err != nil {
		return nil, err
	}
	return c.albumsByArtist[artistID][compactMatchName(title)], nil
}

func (c *localCatalog) resolveArtist(ctx context.Context, ev tagIdentity, create bool) (int64, bool, error) {
	if err := c.loadArtists(ctx); err != nil {
		return 0, false, err
	}
	if ev.artistMBID != "" {
		if id, ok := c.artistsByMBID[ev.artistMBID]; ok {
			return id, true, nil
		}
	}
	ids := c.artistsByKey[ev.artistKey]
	if len(ids) == 1 {
		// Reuse a saved artist with the same normalized name before creating a
		// second row for the same name, even when one file also carries an MBID.
		c.enrichLocalArtistMBID(ctx, ids[0], ev.artistMBID)
		return ids[0], true, nil
	}
	if len(ids) > 1 && ev.artistMBID == "" {
		if !create {
			return 0, false, nil
		}
		return 0, false, ambiguousLocalArtistError(ev.artistName)
	}
	if !create || ev.artistName == "" {
		return 0, false, nil
	}
	id := ev.artistMBID
	if id == "" {
		id = localCatalogID(localArtistIDPrefix, ev.artistKey)
	}
	return c.createArtist(ctx, id, ev.artistName)
}

// enrichLocalArtistMBID upgrades a tag-derived artist row to a genuine
// MusicBrainz identity when one is available and no other row uses it. It is
// best effort: a conflict keeps the local identity instead of duplicating.
func (c *localCatalog) enrichLocalArtistMBID(ctx context.Context, id int64, mbid string) {
	if id < 1 || mbid == "" {
		return
	}
	var current string
	if err := c.service.store.db.QueryRowContext(ctx, `SELECT musicbrainz_id FROM music_artists WHERE id=?`, id).Scan(&current); err != nil {
		return
	}
	if !IsLocalMusicID(current) {
		return
	}
	if _, err := c.service.store.db.ExecContext(ctx, `UPDATE music_artists SET musicbrainz_id=?,updated_at=unixepoch() WHERE id=? AND musicbrainz_id=?`, mbid, id, current); err != nil {
		return
	}
	delete(c.artistsByMBID, strings.ToLower(current))
	c.artistsByMBID[mbid] = id
}

// enrichLocalAlbumMBID is the album equivalent of enrichLocalArtistMBID. It
// keeps the row, its tracks and every file reference while replacing only the
// local key with the genuine release-group ID.
func (c *localCatalog) enrichLocalAlbumMBID(ctx context.Context, ref albumRef, mbid string) albumRef {
	if ref.id < 1 || mbid == "" || !IsLocalMusicID(ref.mbid) {
		return ref
	}
	if _, err := c.service.store.db.ExecContext(ctx, `UPDATE music_albums SET musicbrainz_id=?,updated_at=unixepoch() WHERE id=? AND musicbrainz_id=?`, mbid, ref.id, ref.mbid); err != nil {
		return ref
	}
	delete(c.albumsByMBID, ref.mbid)
	ref.mbid = mbid
	c.albumsByMBID[mbid] = ref
	if refs := c.albumsByArtist[ref.artistID][compactMatchName(ref.title)]; len(refs) > 0 {
		for i := range refs {
			if refs[i].id == ref.id {
				refs[i].mbid = mbid
			}
		}
	}
	return ref
}

func (c *localCatalog) createArtist(ctx context.Context, mbid, name string) (int64, bool, error) {
	now := time.Now().Unix()
	result, err := c.service.store.db.ExecContext(ctx, `INSERT INTO music_artists(musicbrainz_id,name,sort_name,disambiguation,country,artist_type,overview,image_url,monitored,status,created_at,updated_at)
		VALUES(?,?,?,'','','','','',1,'missing',?,?)
		ON CONFLICT(musicbrainz_id) DO NOTHING`, mbid, name, name, now, now)
	if err != nil {
		return 0, false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, false, err
	}
	if affected == 1 {
		id, err := result.LastInsertId()
		if err != nil {
			return 0, false, err
		}
		c.artistsByMBID[strings.ToLower(mbid)] = id
		if key := localArtistKey(name); key != "" {
			c.artistsByKey[key] = append(c.artistsByKey[key], id)
		}
		return id, true, nil
	}
	var id int64
	if err := c.service.store.db.QueryRowContext(ctx, `SELECT id FROM music_artists WHERE musicbrainz_id=?`, mbid).Scan(&id); err != nil {
		return 0, false, err
	}
	c.artistsByMBID[strings.ToLower(mbid)] = id
	return id, false, nil
}

// resolveAlbum finds an existing album first and only creates one when the
// tags prove an identity that is not already in the catalog. A genuine tagged
// release-group ID is stored as the album identifier; otherwise the identifier
// is deterministic from the tags. An existing canonical row is never rewritten
// as a local row.
func (c *localCatalog) resolveAlbum(ctx context.Context, ev tagIdentity, create bool) (albumRef, bool, error) {
	if ev.albumMBID != "" {
		if ref, ok, err := c.findAlbumByMBID(ctx, ev.albumMBID); err != nil {
			return albumRef{}, false, err
		} else if ok {
			return ref, true, nil
		}
	}
	if ev.albumTitle == "" || ev.artistKey == "" {
		return albumRef{}, false, nil
	}
	artistID, found, err := c.resolveArtist(ctx, ev, create)
	if err != nil || !found {
		return albumRef{}, false, err
	}
	refs, err := c.albumsByTitle(ctx, artistID, ev.albumTitle)
	if err != nil {
		return albumRef{}, false, err
	}
	if len(refs) == 1 {
		ref := refs[0]
		if ev.albumMBID != "" {
			ref = c.enrichLocalAlbumMBID(ctx, ref, ev.albumMBID)
		}
		return ref, true, nil
	}
	if len(refs) > 1 {
		if !create {
			return albumRef{}, false, nil
		}
		return albumRef{}, false, &Error{Code: "music_ambiguous_album", Message: fmt.Sprintf("More than one saved album matches the tagged title %q. Correct the tags or merge the catalog entries first.", ev.albumTitle)}
	}
	if !create {
		return albumRef{}, false, nil
	}
	mbid := ev.albumMBID
	if mbid == "" {
		mbid = localCatalogID(localAlbumIDPrefix, ev.artistKey, ev.albumKey)
	}
	var year any
	if ev.year > 0 {
		year = ev.year
	}
	now := time.Now().Unix()
	result, err := c.service.store.db.ExecContext(ctx, `INSERT INTO music_albums(artist_id,musicbrainz_id,title,primary_type,secondary_types,year,release_date,cover_url,monitored,status,created_at,updated_at)
		VALUES(?,?,?,'','',?,'','',1,'missing',?,?)
		ON CONFLICT(artist_id,musicbrainz_id) DO NOTHING`, artistID, mbid, ev.albumTitle, year, now, now)
	if err != nil {
		return albumRef{}, false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return albumRef{}, false, err
	}
	ref := albumRef{artistID: artistID, mbid: strings.ToLower(mbid), title: ev.albumTitle}
	if affected == 1 {
		ref.id, err = result.LastInsertId()
		if err != nil {
			return albumRef{}, false, err
		}
	} else if err := c.service.store.db.QueryRowContext(ctx, `SELECT id,musicbrainz_id,title FROM music_albums WHERE artist_id=? AND musicbrainz_id=?`, artistID, mbid).
		Scan(&ref.id, &ref.mbid, &ref.title); err != nil {
		return albumRef{}, false, err
	}
	c.albumsByMBID[ref.mbid] = ref
	if c.albumsLoaded[artistID] {
		key := compactMatchName(ref.title)
		if key != "" {
			c.albumsByArtist[artistID][key] = append(c.albumsByArtist[artistID][key], ref)
		}
	}
	return ref, true, nil
}

func (c *localCatalog) loadTracks(ctx context.Context, albumID int64) ([]Track, error) {
	if c.tracksLoaded[albumID] {
		return c.tracksByAlbum[albumID], nil
	}
	tracks, err := c.service.store.Tracks(ctx, albumID)
	if err != nil {
		return nil, err
	}
	c.tracksByAlbum[albumID] = tracks
	c.tracksLoaded[albumID] = true
	return tracks, nil
}

// findExisting matches one file against already saved catalog rows without
// creating anything and without calling a provider.
func (c *localCatalog) findExisting(ctx context.Context, file ScanFile) (int64, int64, int64, bool, error) {
	ev := tagIdentityFor(file)
	ref, found, err := c.resolveAlbum(ctx, ev, false)
	if err != nil || !found {
		return 0, 0, 0, false, err
	}
	tracks, err := c.loadTracks(ctx, ref.id)
	if err != nil {
		return 0, 0, 0, false, err
	}
	if len(tracks) == 0 {
		return ref.artistID, ref.id, 0, false, nil
	}
	trackID, err := matchAlbumTrack(file, tracks)
	if err != nil {
		return ref.artistID, ref.id, 0, false, nil
	}
	if track := trackByID(tracks, trackID); track != nil && trackMatchContradicts(file, *track) {
		return ref.artistID, ref.id, 0, false, nil
	}
	c.enrichLocalTrackIDs(ctx, ref, trackID, ev)
	return ref.artistID, ref.id, trackID, true, nil
}

// matchTagCreate creates or reuses the artist, album and track the tags prove
// and returns the matched catalog IDs. It never contacts a provider.
func (c *localCatalog) matchTagCreate(ctx context.Context, file ScanFile) (int64, int64, int64, error) {
	return c.matchIdentity(ctx, file, tagIdentityFor(file))
}

func (c *localCatalog) matchTagCreateWithIDs(ctx context.Context, file ScanFile, releaseGroupID, recordingID string) (int64, int64, int64, error) {
	return c.matchIdentity(ctx, file, tagIdentityFor(file).withAcoustID(releaseGroupID, recordingID))
}

func (c *localCatalog) matchIdentity(ctx context.Context, file ScanFile, ev tagIdentity) (int64, int64, int64, error) {
	if !ev.canCreate() {
		return 0, 0, 0, &Error{Code: "music_local_tags", Message: "The file needs artist, album, title and a track number to match from its tags. Add the missing tags or use MusicBrainz discovery."}
	}
	ref, found, err := c.resolveAlbum(ctx, ev, true)
	if err != nil {
		return 0, 0, 0, err
	}
	if !found {
		return 0, 0, 0, &Error{Code: "music_local_tags", Message: "The file's tags did not identify an album. Add artist, album and title tags and try again."}
	}
	trackID, err := c.findOrCreateTrack(ctx, file, ev, ref)
	if err != nil {
		return ref.artistID, ref.id, 0, err
	}
	return ref.artistID, ref.id, trackID, nil
}

// findOrCreateTrack uses an existing catalog track when the provider IDs,
// disc/number or exact title prove it. Otherwise it inserts one tag-derived
// track at the tagged disc and number. A position occupied by a different
// title is never overwritten or renumbered.
func (c *localCatalog) findOrCreateTrack(ctx context.Context, file ScanFile, ev tagIdentity, album albumRef) (int64, error) {
	tracks, err := c.loadTracks(ctx, album.id)
	if err != nil {
		return 0, err
	}
	if len(tracks) > 0 {
		if id, err := matchAlbumTrack(file, tracks); err == nil {
			// A match that rests on the disc/number while the titles disagree is
			// not evidence of the same song; treat the position as occupied so
			// the caller can use the next free number instead of overwriting.
			if track := trackByID(tracks, id); track == nil || !trackMatchContradicts(file, *track) {
				c.enrichLocalTrackIDs(ctx, album, id, ev)
				return id, nil
			}
		}
	}
	disc := ev.disc
	if disc < 1 {
		disc = 1
	}
	for _, track := range tracks {
		if track.Disc != disc || track.Number != ev.number {
			continue
		}
		if trackIdentityMatches(track, ev) {
			c.enrichLocalTrackIDs(ctx, album, track.ID, ev)
			return track.ID, nil
		}
		return 0, &Error{Code: "music_local_track", Message: fmt.Sprintf("Another file in this album already uses disc %d track %d with a different title. Correct the tags and try again.", disc, ev.number)}
	}
	mbid := ev.trackMBID
	if mbid == "" {
		mbid = localCatalogID(localTrackIDPrefix, ev.artistKey, ev.albumKey, strconv.Itoa(disc), strconv.Itoa(ev.number), ev.trackKey)
	}
	now := time.Now().Unix()
	result, err := c.service.store.db.ExecContext(ctx, `INSERT INTO music_tracks(album_id,musicbrainz_id,recording_id,disc,number,title,duration_ms,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?)
		ON CONFLICT(album_id,disc,number) DO NOTHING`, album.id, mbid, ev.recordingMBID, disc, ev.number, ev.trackTitle, file.DurationMS, now, now)
	if err != nil {
		return 0, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	var track Track
	if affected == 1 {
		track.ID, err = result.LastInsertId()
		if err != nil {
			return 0, err
		}
		track.AlbumID, track.MusicBrainzID, track.RecordingID = album.id, mbid, ev.recordingMBID
		track.Disc, track.Number, track.Title, track.DurationMS = disc, ev.number, ev.trackTitle, file.DurationMS
	} else if err := c.service.store.db.QueryRowContext(ctx, `SELECT id,album_id,musicbrainz_id,recording_id,disc,number,title,duration_ms FROM music_tracks WHERE album_id=? AND disc=? AND number=?`, album.id, disc, ev.number).
		Scan(&track.ID, &track.AlbumID, &track.MusicBrainzID, &track.RecordingID, &track.Disc, &track.Number, &track.Title, &track.DurationMS); err != nil {
		return 0, err
	}
	if !trackIdentityMatches(track, ev) {
		return 0, &Error{Code: "music_local_track", Message: fmt.Sprintf("Another file in this album already uses disc %d track %d with a different title. Correct the tags and try again.", disc, ev.number)}
	}
	if affected == 1 {
		c.tracksByAlbum[album.id] = append(c.tracksByAlbum[album.id], track)
	}
	c.enrichLocalTrackIDs(ctx, album, track.ID, ev)
	return track.ID, nil
}

// enrichLocalTrackIDs upgrades a tag-derived track row to the genuine track and
// recording IDs the file carries. It never rewrites a different genuine ID.
func (c *localCatalog) enrichLocalTrackIDs(ctx context.Context, album albumRef, trackID int64, ev tagIdentity) {
	if trackID < 1 || (ev.trackMBID == "" && ev.recordingMBID == "") {
		return
	}
	tracks := c.tracksByAlbum[album.id]
	for i := range tracks {
		if tracks[i].ID != trackID {
			continue
		}
		mbid, recording := tracks[i].MusicBrainzID, tracks[i].RecordingID
		changed := false
		if ev.trackMBID != "" && (mbid == "" || IsLocalMusicID(mbid)) && !strings.EqualFold(mbid, ev.trackMBID) {
			mbid, changed = ev.trackMBID, true
		}
		if ev.recordingMBID != "" && recording == "" && !strings.EqualFold(recording, ev.recordingMBID) {
			recording, changed = ev.recordingMBID, true
		}
		if !changed {
			return
		}
		if _, err := c.service.store.db.ExecContext(ctx, `UPDATE music_tracks SET musicbrainz_id=?,recording_id=?,updated_at=unixepoch() WHERE id=? AND album_id=?`, mbid, recording, trackID, album.id); err != nil {
			return
		}
		tracks[i].MusicBrainzID, tracks[i].RecordingID = mbid, recording
		return
	}
}

func trackIdentityMatches(track Track, ev tagIdentity) bool {
	if ev.trackMBID != "" && strings.EqualFold(track.MusicBrainzID, ev.trackMBID) {
		return true
	}
	if ev.recordingMBID != "" && strings.EqualFold(track.RecordingID, ev.recordingMBID) {
		return true
	}
	return compactMatchName(track.Title) != "" && compactMatchName(track.Title) == ev.trackKey
}

func ambiguousLocalArtistError(name string) error {
	return &Error{Code: "music_ambiguous_artist", Message: fmt.Sprintf("More than one saved artist matches the tagged name %q. Correct the tags or merge the catalog entries first.", name)}
}

// genuineMBID drops local identities before they can reach a MusicBrainz tag
// field. Only a value that is still a valid MusicBrainz ID is preserved.
func genuineMBID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if !ValidMBID(value) {
		return ""
	}
	return value
}
