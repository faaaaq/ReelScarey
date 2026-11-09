package httpapi

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/faaaaq/ReelScarey/internal/music"
)

const (
	subsonicVersion = "1.16.1"
	subsonicType    = "ReelScarey"
)

type subsonicError struct {
	Code    int    `xml:"code,attr" json:"code"`
	Message string `xml:"message,attr" json:"message"`
}

type subsonicLicense struct {
	Valid bool `xml:"valid,attr" json:"valid"`
}

type subsonicMusicFolder struct {
	ID   int    `xml:"id,attr" json:"id"`
	Name string `xml:"name,attr" json:"name"`
}

type subsonicMusicFolders struct {
	Folders []subsonicMusicFolder `xml:"musicFolder" json:"musicFolder"`
}

type subsonicArtist struct {
	ID         string `xml:"id,attr" json:"id"`
	Name       string `xml:"name,attr" json:"name"`
	AlbumCount int    `xml:"albumCount,attr" json:"albumCount"`
	CoverArt   string `xml:"coverArt,attr,omitempty" json:"coverArt,omitempty"`
}

type subsonicIndex struct {
	Name    string           `xml:"name,attr" json:"name"`
	Artists []subsonicArtist `xml:"artist" json:"artist"`
}

type subsonicArtists struct {
	Indexes []subsonicIndex `xml:"index" json:"index"`
}

type subsonicIndexes struct {
	Indexes []subsonicIndex `xml:"index" json:"index"`
}

type subsonicArtistWithAlbums struct {
	subsonicArtist
	Album []subsonicAlbum `xml:"album" json:"album"`
}

type subsonicAlbum struct {
	ID        string         `xml:"id,attr" json:"id"`
	Name      string         `xml:"name,attr" json:"name"`
	Artist    string         `xml:"artist,attr" json:"artist"`
	ArtistID  string         `xml:"artistId,attr" json:"artistId"`
	CoverArt  string         `xml:"coverArt,attr,omitempty" json:"coverArt,omitempty"`
	SongCount int            `xml:"songCount,attr" json:"songCount"`
	Duration  int            `xml:"duration,attr" json:"duration"`
	Year      *int           `xml:"year,attr,omitempty" json:"year,omitempty"`
	Created   string         `xml:"created,attr,omitempty" json:"created,omitempty"`
	Songs     []subsonicSong `xml:"song" json:"song,omitempty"`
	PlayCount int            `xml:"playCount,attr" json:"playCount"`
}

type subsonicSong struct {
	ID          string `xml:"id,attr" json:"id"`
	Parent      string `xml:"parent,attr" json:"parent"`
	Title       string `xml:"title,attr" json:"title"`
	Album       string `xml:"album,attr" json:"album"`
	Artist      string `xml:"artist,attr" json:"artist"`
	Track       int    `xml:"track,attr" json:"track"`
	DiscNumber  int    `xml:"discNumber,attr" json:"discNumber"`
	Year        *int   `xml:"year,attr,omitempty" json:"year,omitempty"`
	Duration    int    `xml:"duration,attr" json:"duration"`
	Suffix      string `xml:"suffix,attr" json:"suffix"`
	ContentType string `xml:"contentType,attr" json:"contentType"`
	Size        int64  `xml:"size,attr" json:"size"`
	CoverArt    string `xml:"coverArt,attr,omitempty" json:"coverArt,omitempty"`
	IsDir       bool   `xml:"isDir,attr" json:"isDir"`
}

type subsonicAlbumList struct {
	Albums []subsonicAlbum `xml:"album" json:"album"`
}

type subsonicSearch struct {
	Artists []subsonicArtist `xml:"artist" json:"artist,omitempty"`
	Albums  []subsonicAlbum  `xml:"album" json:"album,omitempty"`
	Songs   []subsonicSong   `xml:"song" json:"song,omitempty"`
}

type subsonicSongs struct {
	Songs []subsonicSong `xml:"song" json:"song"`
}

type subsonicPlaylists struct {
	Playlists []any `xml:"playlist" json:"playlist,omitempty"`
}

type subsonicResponse struct {
	XMLName       xml.Name                  `xml:"subsonic-response" json:"-"`
	Xmlns         string                    `xml:"xmlns,attr,omitempty" json:"-"`
	Status        string                    `xml:"status,attr" json:"status"`
	Version       string                    `xml:"version,attr" json:"version"`
	Type          string                    `xml:"type,attr,omitempty" json:"type,omitempty"`
	ServerVersion string                    `xml:"serverVersion,attr,omitempty" json:"serverVersion,omitempty"`
	Error         *subsonicError            `xml:"error,omitempty" json:"error,omitempty"`
	License       *subsonicLicense          `xml:"license,omitempty" json:"license,omitempty"`
	MusicFolders  *subsonicMusicFolders     `xml:"musicFolders,omitempty" json:"musicFolders,omitempty"`
	Artists       *subsonicArtists          `xml:"artists,omitempty" json:"artists,omitempty"`
	Indexes       *subsonicIndexes          `xml:"indexes,omitempty" json:"indexes,omitempty"`
	Artist        *subsonicArtistWithAlbums `xml:"artist,omitempty" json:"artist,omitempty"`
	Album         *subsonicAlbum            `xml:"album,omitempty" json:"album,omitempty"`
	AlbumList2    *subsonicAlbumList        `xml:"albumList2,omitempty" json:"albumList2,omitempty"`
	Song          *subsonicSong             `xml:"song,omitempty" json:"song,omitempty"`
	SearchResult3 *subsonicSearch           `xml:"searchResult3,omitempty" json:"searchResult3,omitempty"`
	RandomSongs   *subsonicSongs            `xml:"randomSongs,omitempty" json:"randomSongs,omitempty"`
	Playlists     *subsonicPlaylists        `xml:"playlists,omitempty" json:"playlists,omitempty"`
}

func subsonicResponseFor(version string) subsonicResponse {
	return subsonicResponse{
		Xmlns:         "http://subsonic.org/restapi",
		Status:        "ok",
		Version:       subsonicVersion,
		Type:          subsonicType,
		ServerVersion: version,
	}
}

func subsonicFailure(version string, code int, message string) subsonicResponse {
	out := subsonicResponseFor(version)
	out.Status = "failed"
	out.Error = &subsonicError{Code: code, Message: message}
	return out
}

func writeSubsonic(w http.ResponseWriter, r *http.Request, response subsonicResponse) {
	if strings.EqualFold(r.URL.Query().Get("f"), "json") || strings.EqualFold(r.URL.Query().Get("format"), "json") {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(map[string]any{"subsonic-response": response})
		return
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(xml.Header))
	_ = xml.NewEncoder(w).Encode(response)
}

func (s *Server) subsonic(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		writeSubsonic(w, r, subsonicFailure(s.version, 0, "The music catalog is unavailable."))
		return
	}
	ok, err := s.music.VerifyMusicServerToken(r.Context(), r.URL.Query().Get("u"), r.URL.Query().Get("p"), r.URL.Query().Get("t"), r.URL.Query().Get("s"))
	if err != nil {
		s.logger.Error("music server authentication failed", "error", err)
		writeSubsonic(w, r, subsonicFailure(s.version, 0, "The music server could not verify this request."))
		return
	}
	if !ok {
		writeSubsonic(w, r, subsonicFailure(s.version, 40, "Wrong username or password."))
		return
	}
	action := strings.TrimPrefix(r.URL.Path, "/rest/")
	action = strings.TrimSuffix(action, ".view")
	if action == "" || strings.Contains(action, "/") {
		writeSubsonic(w, r, subsonicFailure(s.version, 0, "Unknown Subsonic action."))
		return
	}
	switch strings.ToLower(action) {
	case "ping":
		writeSubsonic(w, r, subsonicResponseFor(s.version))
	case "getlicense":
		out := subsonicResponseFor(s.version)
		out.License = &subsonicLicense{Valid: true}
		writeSubsonic(w, r, out)
	case "getmusicfolders":
		out := subsonicResponseFor(s.version)
		out.MusicFolders = &subsonicMusicFolders{Folders: []subsonicMusicFolder{{ID: 1, Name: "Music"}}}
		writeSubsonic(w, r, out)
	case "getartists":
		s.subsonicArtists(w, r)
	case "getindexes":
		s.subsonicIndexes(w, r)
	case "getartist":
		s.subsonicArtist(w, r)
	case "getalbum", "getmusicdirectory":
		s.subsonicAlbum(w, r)
	case "getalbumlist2", "getalbumlist":
		s.subsonicAlbumList(w, r)
	case "getsong":
		s.subsonicSong(w, r)
	case "search3", "search2", "search":
		s.subsonicSearch(w, r)
	case "getrandomsongs":
		s.subsonicRandomSongs(w, r)
	case "stream", "download":
		s.subsonicStream(w, r)
	case "scrobble", "star", "unstar", "setrating":
		writeSubsonic(w, r, subsonicResponseFor(s.version))
	case "getplaylists":
		out := subsonicResponseFor(s.version)
		out.Playlists = &subsonicPlaylists{Playlists: []any{}}
		writeSubsonic(w, r, out)
	case "getstarred2", "getstarred":
		writeSubsonic(w, r, subsonicResponseFor(s.version))
	default:
		writeSubsonic(w, r, subsonicFailure(s.version, 0, "This Subsonic action is not implemented."))
	}
}

func subsonicID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
	return id, err == nil && id > 0
}

func subsonicAlbumFrom(album music.Album) subsonicAlbum {
	return subsonicAlbum{
		ID:        strconv.FormatInt(album.ID, 10),
		Name:      album.Title,
		Artist:    album.ArtistName,
		ArtistID:  strconv.FormatInt(album.ArtistID, 10),
		CoverArt:  strconv.FormatInt(album.ID, 10),
		SongCount: album.TrackCount,
		Year:      album.Year,
		PlayCount: 0,
	}
}

func subsonicSongFrom(song music.ServerSong) subsonicSong {
	return subsonicSong{
		ID:          strconv.FormatInt(song.FileID, 10),
		Parent:      strconv.FormatInt(song.AlbumID, 10),
		Title:       song.Title,
		Album:       song.Album,
		Artist:      song.Artist,
		Track:       song.Track,
		DiscNumber:  song.Disc,
		Year:        song.Year,
		Duration:    song.DurationMS / 1000,
		Suffix:      strings.TrimPrefix(strings.ToLower(song.Suffix), "."),
		ContentType: song.ContentType,
		Size:        song.SizeBytes,
		CoverArt:    strconv.FormatInt(song.AlbumID, 10),
	}
}

func subsonicArtistFrom(artist music.Artist) subsonicArtist {
	return subsonicArtist{ID: strconv.FormatInt(artist.ID, 10), Name: artist.Name, AlbumCount: artist.AlbumCount}
}

func (s *Server) subsonicArtistGroups(ctx context.Context) ([]subsonicIndex, error) {
	groups := map[string][]subsonicArtist{}
	for page := 1; page <= 200; page++ {
		list, err := s.music.ListArtists(ctx, "", page)
		if err != nil {
			return nil, err
		}
		for _, artist := range list.Items {
			key := subsonicIndexName(artist.Name)
			groups[key] = append(groups[key], subsonicArtistFrom(artist))
		}
		if len(list.Items) < list.Limit || len(list.Items) == 0 || page*list.Limit >= list.Total {
			break
		}
	}
	indexes := []subsonicIndex{}
	for _, name := range sortedKeys(groups) {
		indexes = append(indexes, subsonicIndex{Name: name, Artists: groups[name]})
	}
	return indexes, nil
}

func (s *Server) subsonicArtists(w http.ResponseWriter, r *http.Request) {
	indexes, err := s.subsonicArtistGroups(r.Context())
	if err != nil {
		writeSubsonic(w, r, subsonicFailure(s.version, 0, "Could not read artists."))
		return
	}
	out := subsonicResponseFor(s.version)
	out.Artists = &subsonicArtists{Indexes: indexes}
	writeSubsonic(w, r, out)
}

func (s *Server) subsonicIndexes(w http.ResponseWriter, r *http.Request) {
	indexes, err := s.subsonicArtistGroups(r.Context())
	if err != nil {
		writeSubsonic(w, r, subsonicFailure(s.version, 0, "Could not read artists."))
		return
	}
	out := subsonicResponseFor(s.version)
	out.Indexes = &subsonicIndexes{Indexes: indexes}
	writeSubsonic(w, r, out)
}

func (s *Server) subsonicArtist(w http.ResponseWriter, r *http.Request) {
	id, ok := subsonicID(r)
	if !ok {
		writeSubsonic(w, r, subsonicFailure(s.version, 0, "A valid artist ID is required."))
		return
	}
	details, err := s.music.Artist(r.Context(), id)
	if err != nil {
		writeSubsonic(w, r, subsonicFailure(s.version, 70, "Artist not found."))
		return
	}
	out := subsonicResponseFor(s.version)
	artist := subsonicArtistFrom(details.Artist)
	withAlbums := &subsonicArtistWithAlbums{subsonicArtist: artist, Album: []subsonicAlbum{}}
	for _, album := range details.Albums {
		withAlbums.Album = append(withAlbums.Album, subsonicAlbumFrom(album))
	}
	out.Artist = withAlbums
	writeSubsonic(w, r, out)
}

func (s *Server) subsonicAlbum(w http.ResponseWriter, r *http.Request) {
	id, ok := subsonicID(r)
	if !ok {
		writeSubsonic(w, r, subsonicFailure(s.version, 0, "A valid album ID is required."))
		return
	}
	album, err := s.music.ServerAlbum(r.Context(), id)
	if err != nil {
		writeSubsonic(w, r, subsonicFailure(s.version, 70, "Album not found."))
		return
	}
	entry := subsonicAlbumFrom(album.Album)
	entry.Songs = []subsonicSong{}
	for _, track := range album.Tracks {
		if track.FileID == nil {
			continue
		}
		entry.Songs = append(entry.Songs, subsonicSong{
			ID:          strconv.FormatInt(*track.FileID, 10),
			Parent:      strconv.FormatInt(album.Album.ID, 10),
			Title:       track.Title,
			Album:       album.Album.Title,
			Artist:      album.Album.ArtistName,
			Track:       track.Number,
			DiscNumber:  track.Disc,
			Year:        album.Album.Year,
			Duration:    track.DurationMS / 1000,
			Suffix:      "",
			ContentType: "",
			CoverArt:    strconv.FormatInt(album.Album.ID, 10),
		})
	}
	entry.SongCount = len(entry.Songs)
	out := subsonicResponseFor(s.version)
	out.Album = &entry
	writeSubsonic(w, r, out)
}

func (s *Server) subsonicAlbumList(w http.ResponseWriter, r *http.Request) {
	size := subsonicCount(r, "size", 50, 500)
	offset := 0
	if value, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && value > 0 {
		offset = value
	}
	order := "alphabeticalByName"
	switch r.URL.Query().Get("type") {
	case "newest", "recent", "frequent", "highest", "starred":
		order = "newest"
	case "random":
		order = "random"
	}
	albums, err := s.music.ServerAlbums(r.Context(), order, size, offset)
	if err != nil {
		writeSubsonic(w, r, subsonicFailure(s.version, 0, "Could not read albums."))
		return
	}
	out := subsonicResponseFor(s.version)
	list := &subsonicAlbumList{Albums: []subsonicAlbum{}}
	for _, album := range albums {
		list.Albums = append(list.Albums, subsonicAlbumFrom(album))
	}
	out.AlbumList2 = list
	writeSubsonic(w, r, out)
}

func (s *Server) subsonicSong(w http.ResponseWriter, r *http.Request) {
	id, ok := subsonicID(r)
	if !ok {
		writeSubsonic(w, r, subsonicFailure(s.version, 0, "A valid song ID is required."))
		return
	}
	song, err := s.music.ServerSong(r.Context(), id)
	if err != nil {
		writeSubsonic(w, r, subsonicFailure(s.version, 70, "Song not found."))
		return
	}
	out := subsonicResponseFor(s.version)
	entry := subsonicSongFrom(song)
	out.Song = &entry
	writeSubsonic(w, r, out)
}

func (s *Server) subsonicSearch(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("query"))
	if query == "" {
		writeSubsonic(w, r, subsonicFailure(s.version, 0, "A search query is required."))
		return
	}
	songCount := subsonicCount(r, "songCount", 50, 500)
	albumCount := subsonicCount(r, "albumCount", 20, 200)
	out := subsonicResponseFor(s.version)
	result := &subsonicSearch{Artists: []subsonicArtist{}, Albums: []subsonicAlbum{}, Songs: []subsonicSong{}}
	if artists, err := s.music.ListArtists(r.Context(), query, 1); err == nil {
		for _, artist := range artists.Items {
			result.Artists = append(result.Artists, subsonicArtistFrom(artist))
		}
	}
	if albums, err := s.music.ServerSearchAlbums(r.Context(), query, albumCount); err == nil {
		for _, album := range albums {
			result.Albums = append(result.Albums, subsonicAlbumFrom(album))
		}
	}
	if songs, err := s.music.ServerSongs(r.Context(), query, songCount); err == nil {
		for _, song := range songs {
			result.Songs = append(result.Songs, subsonicSongFrom(song))
		}
	}
	out.SearchResult3 = result
	writeSubsonic(w, r, out)
}

func (s *Server) subsonicRandomSongs(w http.ResponseWriter, r *http.Request) {
	size := subsonicCount(r, "size", 50, 500)
	songs, err := s.music.ServerRandomSongs(r.Context(), size)
	if err != nil {
		writeSubsonic(w, r, subsonicFailure(s.version, 0, "Could not read songs."))
		return
	}
	out := subsonicResponseFor(s.version)
	list := &subsonicSongs{Songs: []subsonicSong{}}
	for _, song := range songs {
		list.Songs = append(list.Songs, subsonicSongFrom(song))
	}
	out.RandomSongs = list
	writeSubsonic(w, r, out)
}

func (s *Server) subsonicStream(w http.ResponseWriter, r *http.Request) {
	id, ok := subsonicID(r)
	if !ok {
		writeSubsonic(w, r, subsonicFailure(s.version, 0, "A valid song ID is required."))
		return
	}
	stream, err := s.music.OpenAudioFile(r.Context(), id)
	if err != nil {
		writeSubsonic(w, r, subsonicFailure(s.version, 70, "Song file not found."))
		return
	}
	defer stream.File.Close()
	w.Header().Set("Content-Type", stream.ContentType)
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if strings.EqualFold(strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/rest/"), ".view"), "download") {
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", stream.Name))
	} else {
		w.Header().Set("Content-Disposition", "inline")
	}
	http.ServeContent(w, r, stream.Name, stream.ModTime, stream.File)
}

func subsonicCount(r *http.Request, key string, fallback, maximum int) int {
	value, err := strconv.Atoi(r.URL.Query().Get(key))
	if err != nil || value < 1 {
		return fallback
	}
	if value > maximum {
		return maximum
	}
	return value
}

func subsonicIndexName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "#"
	}
	first := []rune(strings.ToUpper(name))[0]
	if first >= 'A' && first <= 'Z' {
		return string(first)
	}
	return "#"
}

func sortedKeys(groups map[string][]subsonicArtist) []string {
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0; j-- {
			left, right := keys[j-1], keys[j]
			if left == "#" || (right != "#" && left > right) {
				keys[j-1], keys[j] = keys[j], keys[j-1]
			} else {
				break
			}
		}
	}
	return keys
}
