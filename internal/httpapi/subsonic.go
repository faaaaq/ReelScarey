package httpapi

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/faaaaq/ReelScarey/internal/music"
)

const (
	subsonicVersion = "1.16.1"
	subsonicType    = "ReelScarey"

	// Subsonic identifiers are opaque strings that clients echo back verbatim.
	// Artists, albums and songs live in separate catalog tables but share one
	// integer space, so a bare number is ambiguous: artist 14 and album 14 are
	// different rows. Each namespace gets its own prefix so an id is never
	// interpreted as the wrong kind of object.
	subsonicArtistPrefix = "ar"
	subsonicAlbumPrefix  = "al"
	subsonicSongPrefix   = "tr"

	// The single music folder exposed to clients. ReelScarey has one library
	// root, so the Subsonic folder id is fixed.
	subsonicFolderID   = 1
	subsonicFolderName = "Music"

	// RS-0023: paged client syncs must not be able to scan past a bounded
	// offset. The getSongs offset is clamped against the catalog size, so the
	// 1,000,000 cap only ever materializes as a bounded scan; getAlbumList2
	// offsets feed SQL OFFSET directly and share the same bound.
	subsonicAllSongsOffsetCap = 1000000
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

// subsonicSong is used both for the <song> element and for the <child> element
// inside a <directory>. A directory child that is an album sets IsDir and only
// the fields that make sense for a folder, so the numeric and media attributes
// carry omitempty to keep those entries clean.
type subsonicSong struct {
	ID          string `xml:"id,attr" json:"id"`
	Parent      string `xml:"parent,attr,omitempty" json:"parent,omitempty"`
	Title       string `xml:"title,attr" json:"title"`
	Album       string `xml:"album,attr,omitempty" json:"album,omitempty"`
	Artist      string `xml:"artist,attr,omitempty" json:"artist,omitempty"`
	Track       int    `xml:"track,attr,omitempty" json:"track,omitempty"`
	DiscNumber  int    `xml:"discNumber,attr,omitempty" json:"discNumber,omitempty"`
	Year        *int   `xml:"year,attr,omitempty" json:"year,omitempty"`
	Duration    int    `xml:"duration,attr,omitempty" json:"duration,omitempty"`
	Suffix      string `xml:"suffix,attr,omitempty" json:"suffix,omitempty"`
	ContentType string `xml:"contentType,attr,omitempty" json:"contentType,omitempty"`
	Size        int64  `xml:"size,attr,omitempty" json:"size,omitempty"`
	CoverArt    string `xml:"coverArt,attr,omitempty" json:"coverArt,omitempty"`
	IsDir       bool   `xml:"isDir,attr" json:"isDir"`
	SongCount   int    `xml:"songCount,attr,omitempty" json:"songCount,omitempty"`
}

type subsonicDirectory struct {
	ID       string         `xml:"id,attr" json:"id"`
	Parent   string         `xml:"parent,attr,omitempty" json:"parent,omitempty"`
	Name     string         `xml:"name,attr" json:"name"`
	Children []subsonicSong `xml:"child" json:"child,omitempty"`
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

type subsonicPlaylist struct {
	ID        string         `xml:"id,attr" json:"id"`
	Name      string         `xml:"name,attr" json:"name"`
	SongCount int            `xml:"songCount,attr" json:"songCount"`
	Duration  int            `xml:"duration,attr" json:"duration"`
	Created   string         `xml:"created,attr,omitempty" json:"created,omitempty"`
	Entry     []subsonicSong `xml:"entry" json:"entry,omitempty"`
}

type subsonicPlaylists struct {
	Playlists []subsonicPlaylist `xml:"playlist" json:"playlist,omitempty"`
}

type subsonicPlayQueue struct {
	Current   string         `xml:"current,attr,omitempty" json:"current,omitempty"`
	Position  int64          `xml:"position,attr,omitempty" json:"position,omitempty"`
	Username  string         `xml:"username,attr,omitempty" json:"username,omitempty"`
	Changed   string         `xml:"changed,attr,omitempty" json:"changed,omitempty"`
	ChangedBy string         `xml:"changedBy,attr,omitempty" json:"changedBy,omitempty"`
	Entry     []subsonicSong `xml:"entry" json:"entry,omitempty"`
}

type subsonicNowPlaying struct {
	Entry []subsonicSong `xml:"entry" json:"entry,omitempty"`
}

type subsonicStarred struct {
	Artists []subsonicArtist `xml:"artist" json:"artist,omitempty"`
	Albums  []subsonicAlbum  `xml:"album" json:"album,omitempty"`
	Songs   []subsonicSong   `xml:"song" json:"song,omitempty"`
}

type subsonicScanStatus struct {
	Scanning bool `xml:"scanning,attr" json:"scanning"`
	Count    int  `xml:"count,attr" json:"count"`
}

type subsonicArtistInfo struct {
	Biography      string           `xml:"biography,omitempty" json:"biography,omitempty"`
	MusicBrainzID  string           `xml:"musicBrainzId,omitempty" json:"musicBrainzId,omitempty"`
	SmallImageURL  string           `xml:"smallImageUrl,omitempty" json:"smallImageUrl,omitempty"`
	MediumImageURL string           `xml:"mediumImageUrl,omitempty" json:"mediumImageUrl,omitempty"`
	LargeImageURL  string           `xml:"largeImageUrl,omitempty" json:"largeImageUrl,omitempty"`
	SimilarArtist  []subsonicArtist `xml:"similarArtist" json:"similarArtist,omitempty"`
}

type subsonicAlbumInfo struct {
	Notes         string `xml:"notes,omitempty" json:"notes,omitempty"`
	MusicBrainzID string `xml:"musicBrainzId,omitempty" json:"musicBrainzId,omitempty"`
	SmallImageURL string `xml:"smallImageUrl,omitempty" json:"smallImageUrl,omitempty"`
}

type subsonicGenre struct {
	Name       string `xml:"name,attr" json:"name"`
	SongCount  int    `xml:"songCount,attr" json:"songCount"`
	AlbumCount int    `xml:"albumCount,attr" json:"albumCount"`
}

type subsonicGenres struct {
	Genres []subsonicGenre `xml:"genre" json:"genre,omitempty"`
}

type subsonicUser struct {
	Username          string `xml:"username,attr" json:"username"`
	Email             string `xml:"email,attr,omitempty" json:"email,omitempty"`
	ScrobblingEnabled bool   `xml:"scrobblingEnabled,attr" json:"scrobblingEnabled"`
	AdminRole         bool   `xml:"adminRole,attr" json:"adminRole"`
	SettingsRole      bool   `xml:"settingsRole,attr" json:"settingsRole"`
	DownloadRole      bool   `xml:"downloadRole,attr" json:"downloadRole"`
	UploadRole        bool   `xml:"uploadRole,attr" json:"uploadRole"`
	PlaylistRole      bool   `xml:"playlistRole,attr" json:"playlistRole"`
	CoverArtRole      bool   `xml:"coverArtRole,attr" json:"coverArtRole"`
	ShareRole         bool   `xml:"shareRole,attr" json:"shareRole"`
}

type subsonicLyrics struct {
	Artist string `xml:"artist,attr,omitempty" json:"artist,omitempty"`
	Title  string `xml:"title,attr,omitempty" json:"title,omitempty"`
}

type subsonicBookmarks struct {
	Bookmarks []any `xml:"bookmark" json:"bookmark,omitempty"`
}

type subsonicShares struct {
	Shares []any `xml:"share" json:"share,omitempty"`
}

type subsonicPodcasts struct {
	Channels []any `xml:"channel" json:"channel,omitempty"`
}

type subsonicInternetRadioStations struct {
	Stations []any `xml:"internetRadioStation" json:"internetRadioStation,omitempty"`
}

type subsonicChatMessages struct {
	Messages []any `xml:"chatMessage" json:"chatMessage,omitempty"`
}

type subsonicVideos struct {
	Videos []any `xml:"video" json:"video,omitempty"`
}

type subsonicLyricsList struct {
	StructuredLyrics []any `xml:"structuredLyrics" json:"structuredLyrics,omitempty"`
}

type subsonicOpenSubsonicExtension struct {
	Name     string `xml:"name,attr" json:"name"`
	Versions []int  `xml:"versions" json:"versions"`
}

type subsonicOpenSubsonicExtensions struct {
	Extensions []subsonicOpenSubsonicExtension `xml:"openSubsonicExtension" json:"openSubsonicExtension,omitempty"`
}

// subsonicSupportedExtensions lists the OpenSubsonic extensions this server
// actually implements. Every entry must correspond to working behaviour, so
// the list stays deliberately short:
//
//   - transcodeOffset: the stream endpoint honours ?timeOffset= and seeks the
//     ffmpeg transcode (see music.transcodeArgs / OpenPlayback).
//   - topSongsByArtistId: getTopSongs accepts ?id= with an artist id and scopes
//     songs to that artist (see Server.subsonicTopSongs).
//
// Do not add extensions for actions that only return an empty or inert
// payload, such as indexBasedQueue, songLyrics, formPost or playbackReport.
func subsonicSupportedExtensions() []subsonicOpenSubsonicExtension {
	return []subsonicOpenSubsonicExtension{
		{Name: "transcodeOffset", Versions: []int{1}},
		{Name: "topSongsByArtistId", Versions: []int{1}},
	}
}

type subsonicResponse struct {
	XMLName       xml.Name `xml:"subsonic-response" json:"-"`
	Xmlns         string   `xml:"xmlns,attr,omitempty" json:"-"`
	Status        string   `xml:"status,attr" json:"status"`
	Version       string   `xml:"version,attr" json:"version"`
	Type          string   `xml:"type,attr,omitempty" json:"type,omitempty"`
	ServerVersion string   `xml:"serverVersion,attr,omitempty" json:"serverVersion,omitempty"`
	// OpenSubsonic must be true for any server implementing OpenSubsonic API v1.
	// Clients such as Arpeggi gate their whole sync/playback feature set on this
	// flag, so omitting it makes the server look like plain Subsonic 1.16.1.
	OpenSubsonic          bool                            `xml:"openSubsonic,attr" json:"openSubsonic"`
	Error                 *subsonicError                  `xml:"error,omitempty" json:"error,omitempty"`
	License               *subsonicLicense                `xml:"license,omitempty" json:"license,omitempty"`
	MusicFolders          *subsonicMusicFolders           `xml:"musicFolders,omitempty" json:"musicFolders,omitempty"`
	Artists               *subsonicArtists                `xml:"artists,omitempty" json:"artists,omitempty"`
	Indexes               *subsonicIndexes                `xml:"indexes,omitempty" json:"indexes,omitempty"`
	Artist                *subsonicArtistWithAlbums       `xml:"artist,omitempty" json:"artist,omitempty"`
	Album                 *subsonicAlbum                  `xml:"album,omitempty" json:"album,omitempty"`
	Directory             *subsonicDirectory              `xml:"directory,omitempty" json:"directory,omitempty"`
	AlbumList             *subsonicAlbumList              `xml:"albumList,omitempty" json:"albumList,omitempty"`
	AlbumList2            *subsonicAlbumList              `xml:"albumList2,omitempty" json:"albumList2,omitempty"`
	Song                  *subsonicSong                   `xml:"song,omitempty" json:"song,omitempty"`
	SearchResult2         *subsonicSearch                 `xml:"searchResult2,omitempty" json:"searchResult2,omitempty"`
	SearchResult3         *subsonicSearch                 `xml:"searchResult3,omitempty" json:"searchResult3,omitempty"`
	RandomSongs           *subsonicSongs                  `xml:"randomSongs,omitempty" json:"randomSongs,omitempty"`
	Songs                 *subsonicSongs                  `xml:"songs,omitempty" json:"songs,omitempty"`
	SongsByGenre          *subsonicSongs                  `xml:"songsByGenre,omitempty" json:"songsByGenre,omitempty"`
	TopSongs              *subsonicSongs                  `xml:"topSongs,omitempty" json:"topSongs,omitempty"`
	SimilarSongs          *subsonicSongs                  `xml:"similarSongs,omitempty" json:"similarSongs,omitempty"`
	SimilarSongs2         *subsonicSongs                  `xml:"similarSongs2,omitempty" json:"similarSongs2,omitempty"`
	Playlists             *subsonicPlaylists              `xml:"playlists,omitempty" json:"playlists,omitempty"`
	Playlist              *subsonicPlaylist               `xml:"playlist,omitempty" json:"playlist,omitempty"`
	PlayQueue             *subsonicPlayQueue              `xml:"playQueue,omitempty" json:"playQueue,omitempty"`
	NowPlaying            *subsonicNowPlaying             `xml:"nowPlaying,omitempty" json:"nowPlaying,omitempty"`
	Starred               *subsonicStarred                `xml:"starred,omitempty" json:"starred,omitempty"`
	Starred2              *subsonicStarred                `xml:"starred2,omitempty" json:"starred2,omitempty"`
	ScanStatus            *subsonicScanStatus             `xml:"scanStatus,omitempty" json:"scanStatus,omitempty"`
	ArtistInfo            *subsonicArtistInfo             `xml:"artistInfo,omitempty" json:"artistInfo,omitempty"`
	ArtistInfo2           *subsonicArtistInfo             `xml:"artistInfo2,omitempty" json:"artistInfo2,omitempty"`
	AlbumInfo             *subsonicAlbumInfo              `xml:"albumInfo,omitempty" json:"albumInfo,omitempty"`
	AlbumInfo2            *subsonicAlbumInfo              `xml:"albumInfo2,omitempty" json:"albumInfo2,omitempty"`
	Genres                *subsonicGenres                 `xml:"genres,omitempty" json:"genres,omitempty"`
	User                  *subsonicUser                   `xml:"user,omitempty" json:"user,omitempty"`
	Lyrics                *subsonicLyrics                 `xml:"lyrics,omitempty" json:"lyrics,omitempty"`
	LyricsList            *subsonicLyricsList             `xml:"lyricsList,omitempty" json:"lyricsList,omitempty"`
	Bookmarks             *subsonicBookmarks              `xml:"bookmarks,omitempty" json:"bookmarks,omitempty"`
	Shares                *subsonicShares                 `xml:"shares,omitempty" json:"shares,omitempty"`
	Podcasts              *subsonicPodcasts               `xml:"podcasts,omitempty" json:"podcasts,omitempty"`
	InternetRadioStations *subsonicInternetRadioStations  `xml:"internetRadioStations,omitempty" json:"internetRadioStations,omitempty"`
	ChatMessages          *subsonicChatMessages           `xml:"chatMessages,omitempty" json:"chatMessages,omitempty"`
	Videos                *subsonicVideos                 `xml:"videos,omitempty" json:"videos,omitempty"`
	OpenSubsonicExtension *subsonicOpenSubsonicExtensions `xml:"openSubsonicExtensions,omitempty" json:"openSubsonicExtensions,omitempty"`
}

func subsonicResponseFor(version string) subsonicResponse {
	return subsonicResponse{
		Xmlns:         "http://subsonic.org/restapi",
		Status:        "ok",
		Version:       subsonicVersion,
		Type:          subsonicType,
		ServerVersion: version,
		OpenSubsonic:  true,
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

func subsonicAction(r *http.Request) string {
	action := strings.TrimPrefix(r.URL.Path, "/rest/")
	return strings.ToLower(strings.TrimSuffix(action, ".view"))
}

// subsonicUserKey carries the account resolved during authentication into the
// individual Subsonic handlers, which only receive the request.
type subsonicUserKey struct{}

func withSubsonicUser(ctx context.Context, user music.MusicServerUser) context.Context {
	return context.WithValue(ctx, subsonicUserKey{}, user)
}

// subsonicRequestUser returns the account that authenticated this request. A
// missing context key yields the zero value, which is deliberately
// unprivileged (every Can*() is false), so a handler that somehow runs outside
// s.subsonic fails closed instead of silently acting with full rights. The
// legacy shared token resolves to music.LegacyMusicServerUser, an explicit
// sentinel with full roles, never to the zero value.
func subsonicRequestUser(r *http.Request) music.MusicServerUser {
	user, _ := r.Context().Value(subsonicUserKey{}).(music.MusicServerUser)
	return user
}

// subsonicDenied answers an action the account is not permitted to perform.
func (s *Server) subsonicDenied(w http.ResponseWriter, r *http.Request) {
	writeSubsonic(w, r, subsonicFailure(s.version, 50, "The user is not authorized for this operation."))
}

func (s *Server) subsonic(w http.ResponseWriter, r *http.Request) {
	if s.music == nil {
		writeSubsonic(w, r, subsonicFailure(s.version, 0, "The music catalog is unavailable."))
		return
	}
	user, ok, err := s.music.AuthenticateMusicServer(r.Context(), r.URL.Query().Get("u"), r.URL.Query().Get("p"), r.URL.Query().Get("t"), r.URL.Query().Get("s"))
	if err != nil {
		s.logger.Error("music server authentication failed", "error", err)
		writeSubsonic(w, r, subsonicFailure(s.version, 0, "The music server could not verify this request."))
		return
	}
	if !ok {
		// Password attempts over /rest/ arrive unthrottled from clients, so
		// failed authentications are limited per source address like the
		// compatibility API keys.
		ip, _, splitErr := net.SplitHostPort(r.RemoteAddr)
		if splitErr != nil {
			ip = r.RemoteAddr
		}
		if !s.compatLimitedIP(&s.subsonicAttempts, ip) {
			writeSubsonic(w, r, subsonicFailure(s.version, 50, "Too many failed sign-in attempts. Wait a minute and try again."))
			return
		}
		writeSubsonic(w, r, subsonicFailure(s.version, 40, "Wrong username or password."))
		return
	}
	r = r.WithContext(withSubsonicUser(r.Context(), user))
	action := subsonicAction(r)
	if action == "" || strings.Contains(action, "/") {
		writeSubsonic(w, r, subsonicFailure(s.version, 0, "Unknown Subsonic action."))
		return
	}
	switch action {
	case "ping":
		writeSubsonic(w, r, subsonicResponseFor(s.version))
	case "getlicense":
		out := subsonicResponseFor(s.version)
		out.License = &subsonicLicense{Valid: true}
		writeSubsonic(w, r, out)
	case "getmusicfolders":
		out := subsonicResponseFor(s.version)
		out.MusicFolders = &subsonicMusicFolders{Folders: []subsonicMusicFolder{{ID: subsonicFolderID, Name: subsonicFolderName}}}
		writeSubsonic(w, r, out)
	case "getopensubsonicextensions":
		out := subsonicResponseFor(s.version)
		out.OpenSubsonicExtension = &subsonicOpenSubsonicExtensions{Extensions: subsonicSupportedExtensions()}
		writeSubsonic(w, r, out)
	case "getartists":
		s.subsonicArtists(w, r)
	case "getindexes":
		s.subsonicIndexes(w, r)
	case "getartist":
		s.subsonicArtist(w, r)
	case "getartistinfo", "getartistinfo2":
		s.subsonicArtistInfo(w, r)
	case "getalbum":
		s.subsonicAlbum(w, r)
	case "getmusicdirectory":
		s.subsonicDirectory(w, r)
	case "getalbumlist":
		s.subsonicAlbumList(w, r, false)
	case "getalbumlist2":
		s.subsonicAlbumList(w, r, true)
	case "getalbuminfo", "getalbuminfo2":
		s.subsonicAlbumInfo(w, r)
	case "getsong":
		s.subsonicSong(w, r)
	case "search":
		s.subsonicSearch(w, r, false)
	case "search2":
		s.subsonicSearch(w, r, false)
	case "search3":
		s.subsonicSearch(w, r, true)
	case "getrandomsongs":
		s.subsonicRandomSongs(w, r)
	case "gettopsongs":
		s.subsonicTopSongs(w, r)
	case "getsimilarsongs", "getsimilarsongs2", "getsonicsimilartracks":
		s.subsonicSimilarSongs(w, r)
	case "getsongs":
		s.subsonicAllSongs(w, r)
	case "getsongsbygenre":
		s.subsonicSongsByGenre(w, r)
	case "stream", "download":
		if !user.CanDownload() {
			s.subsonicDenied(w, r)
			return
		}
		s.subsonicStream(w, r)
	case "getcoverart":
		if !user.CanCoverArt() {
			s.subsonicDenied(w, r)
			return
		}
		s.subsonicCoverArt(w, r)
	case "getavatar":
		if !user.CanCoverArt() {
			s.subsonicDenied(w, r)
			return
		}
		s.subsonicAvatar(w, r)
	case "getscanstatus":
		// A harmless read that Subsonic clients routinely poll during sync; any
		// authenticated account may see it.
		s.subsonicScanStatus(w, r)
	case "startscan":
		// Starting a scan is a settings-level operation (the equivalent
		// catalog-scan controls behind the session API are administrator-only),
		// so it requires the settings role like every other privileged case
		// above (RS-0018). The handler stays the honest no-op it already is:
		// it reports the current status and never starts a real scan.
		if !user.CanSettings() {
			s.subsonicDenied(w, r)
			return
		}
		s.subsonicScanStatus(w, r)
	case "getplaylists":
		if !user.CanPlaylist() {
			s.subsonicDenied(w, r)
			return
		}
		s.subsonicPlaylists(w, r)
	case "getplaylist":
		if !user.CanPlaylist() {
			s.subsonicDenied(w, r)
			return
		}
		s.subsonicPlaylist(w, r)
	case "getplayqueue", "getplayqueuebyindex":
		// OpenSubsonic marks username, changed and changedBy as required on
		// playQueue. ReelScarey does not persist a server-side queue yet, so the
		// entry list is empty and current is legitimately omitted (the spec only
		// requires current when entry is non-empty). Reporting the requesting
		// account and client keeps strict OpenSubsonic parsers happy.
		out := subsonicResponseFor(s.version)
		out.PlayQueue = &subsonicPlayQueue{
			Username:  subsonicUserParam(r),
			Changed:   time.Now().UTC().Format(time.RFC3339),
			ChangedBy: strings.TrimSpace(r.URL.Query().Get("c")),
			Entry:     []subsonicSong{},
		}
		writeSubsonic(w, r, out)
	case "getnowplaying":
		out := subsonicResponseFor(s.version)
		out.NowPlaying = &subsonicNowPlaying{Entry: []subsonicSong{}}
		writeSubsonic(w, r, out)
	case "getstarred":
		s.subsonicStarred(w, r, false)
	case "getstarred2":
		s.subsonicStarred(w, r, true)
	case "getgenres":
		out := subsonicResponseFor(s.version)
		out.Genres = &subsonicGenres{Genres: []subsonicGenre{}}
		writeSubsonic(w, r, out)
	case "getuser":
		s.subsonicUser(w, r)
	case "getlyrics":
		out := subsonicResponseFor(s.version)
		out.Lyrics = &subsonicLyrics{}
		writeSubsonic(w, r, out)
	case "getlyricsbysongid":
		out := subsonicResponseFor(s.version)
		out.LyricsList = &subsonicLyricsList{StructuredLyrics: []any{}}
		writeSubsonic(w, r, out)
	case "getbookmarks":
		out := subsonicResponseFor(s.version)
		out.Bookmarks = &subsonicBookmarks{Bookmarks: []any{}}
		writeSubsonic(w, r, out)
	case "getshares":
		if !user.CanShare() {
			s.subsonicDenied(w, r)
			return
		}
		out := subsonicResponseFor(s.version)
		out.Shares = &subsonicShares{Shares: []any{}}
		writeSubsonic(w, r, out)
	case "getpodcasts", "getnewestpodcasts":
		// ReelScarey has no podcast support; report an empty list so clients
		// that probe getNewestPodcasts during sync do not treat the server as
		// misconfigured.
		out := subsonicResponseFor(s.version)
		out.Podcasts = &subsonicPodcasts{Channels: []any{}}
		writeSubsonic(w, r, out)
	case "getinternetradiostations":
		out := subsonicResponseFor(s.version)
		out.InternetRadioStations = &subsonicInternetRadioStations{Stations: []any{}}
		writeSubsonic(w, r, out)
	case "getchatmessages":
		out := subsonicResponseFor(s.version)
		out.ChatMessages = &subsonicChatMessages{Messages: []any{}}
		writeSubsonic(w, r, out)
	case "getvideos":
		out := subsonicResponseFor(s.version)
		out.Videos = &subsonicVideos{Videos: []any{}}
		writeSubsonic(w, r, out)
	case "createplaylist", "updateplaylist", "deleteplaylist":
		if !user.CanPlaylist() {
			s.subsonicDenied(w, r)
			return
		}
		// Playlist writes are not implemented. A silent ok made clients believe
		// their data was saved while nothing was persisted (RS-0002), so the
		// OpenSubsonic "not authorized for this operation" error is returned
		// explicitly instead.
		writeSubsonic(w, r, subsonicFailure(s.version, 50, "Playlist changes are not implemented; this server stores no playlist data."))
	case "scrobble", "star", "unstar", "setrating", "saveplayqueue", "saveplayqueuebyindex":
		// Playback-state writes are not implemented and previously reported ok
		// while discarding the client's data (RS-0002); they now fail
		// explicitly with the OpenSubsonic not-authorized code.
		writeSubsonic(w, r, subsonicFailure(s.version, 50, "Playback-state changes are not implemented; this server stores no star, rating or play-queue data."))
	default:
		writeSubsonic(w, r, subsonicFailure(s.version, 0, "This Subsonic action is not implemented."))
	}
}

// --- identifier helpers -----------------------------------------------------

func subsonicPrefixedID(prefix string, id int64) string {
	return prefix + strconv.FormatInt(id, 10)
}

func subsonicArtistID(id int64) string { return subsonicPrefixedID(subsonicArtistPrefix, id) }
func subsonicAlbumID(id int64) string  { return subsonicPrefixedID(subsonicAlbumPrefix, id) }
func subsonicSongID(id int64) string   { return subsonicPrefixedID(subsonicSongPrefix, id) }

func subsonicParseID(raw, prefix string) (int64, bool) {
	raw = strings.TrimSpace(raw)
	if len(raw) <= len(prefix) || !strings.EqualFold(raw[:len(prefix)], prefix) {
		return 0, false
	}
	value, err := strconv.ParseInt(raw[len(prefix):], 10, 64)
	return value, err == nil && value > 0
}

// subsonicRequestID reads ?id= and accepts either the given namespace prefix or
// a bare integer. Every id this server emits is prefixed, but clients such as
// Amperfy keep the id form they were given and echo it back; older syncs and
// hand-written URLs also use bare integers. The action itself selects the
// namespace, so a bare integer is unambiguous here.
func subsonicRequestID(r *http.Request, prefix string) (int64, bool) {
	if id, ok := subsonicParseID(r.URL.Query().Get("id"), prefix); ok {
		return id, true
	}
	value, err := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("id")), 10, 64)
	return value, err == nil && value > 0
}

// --- element builders -------------------------------------------------------

func subsonicAlbumFrom(album music.Album) subsonicAlbum {
	return subsonicAlbum{
		ID:        subsonicAlbumID(album.ID),
		Name:      album.Title,
		Artist:    album.ArtistName,
		ArtistID:  subsonicArtistID(album.ArtistID),
		CoverArt:  subsonicAlbumID(album.ID),
		SongCount: album.TrackCount,
		Duration:  album.DurationMS / 1000,
		Year:      album.Year,
		PlayCount: 0,
	}
}

func subsonicSongFrom(song music.ServerSong) subsonicSong {
	return subsonicSong{
		ID:          subsonicSongID(song.FileID),
		Parent:      subsonicAlbumID(song.AlbumID),
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
		CoverArt:    subsonicAlbumID(song.AlbumID),
	}
}

func subsonicArtistFrom(artist music.Artist) subsonicArtist {
	return subsonicArtist{ID: subsonicArtistID(artist.ID), Name: artist.Name, AlbumCount: artist.AlbumCount}
}

// subsonicAlbumChild renders an album as a directory entry (isDir) so an artist
// can be browsed with getMusicDirectory.
func subsonicAlbumChild(album music.Album, parent string) subsonicSong {
	return subsonicSong{
		ID:        subsonicAlbumID(album.ID),
		Parent:    parent,
		Title:     album.Title,
		Album:     album.Title,
		Artist:    album.ArtistName,
		Year:      album.Year,
		Duration:  album.DurationMS / 1000,
		CoverArt:  subsonicAlbumID(album.ID),
		IsDir:     true,
		SongCount: album.TrackCount,
	}
}

func subsonicArtistChild(artist music.Artist) subsonicSong {
	return subsonicSong{
		ID:     subsonicArtistID(artist.ID),
		Parent: strconv.Itoa(subsonicFolderID),
		Title:  artist.Name,
		Artist: artist.Name,
		IsDir:  true,
	}
}

// --- handlers ---------------------------------------------------------------

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
	id, ok := subsonicRequestID(r, subsonicArtistPrefix)
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

func (s *Server) subsonicArtistInfo(w http.ResponseWriter, r *http.Request) {
	out := subsonicResponseFor(s.version)
	info := &subsonicArtistInfo{SimilarArtist: []subsonicArtist{}}
	if id, ok := subsonicRequestID(r, subsonicArtistPrefix); ok {
		if details, err := s.music.Artist(r.Context(), id); err == nil {
			info.MusicBrainzID = details.Artist.MusicBrainzID
			info.Biography = details.Artist.Overview
		}
	}
	if subsonicAction(r) == "getartistinfo" {
		out.ArtistInfo = info
	} else {
		out.ArtistInfo2 = info
	}
	writeSubsonic(w, r, out)
}

func (s *Server) subsonicAlbum(w http.ResponseWriter, r *http.Request) {
	id, ok := subsonicRequestID(r, subsonicAlbumPrefix)
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
	if songs, err := s.music.ServerAlbumSongs(r.Context(), id); err == nil {
		for _, song := range songs {
			entry.Songs = append(entry.Songs, subsonicSongFrom(song))
		}
	} else {
		// Fall back to the catalog tracks when the file-backed view fails.
		for _, track := range album.Tracks {
			if track.FileID == nil {
				continue
			}
			entry.Songs = append(entry.Songs, subsonicSong{
				ID:         subsonicSongID(*track.FileID),
				Parent:     subsonicAlbumID(album.Album.ID),
				Title:      track.Title,
				Album:      album.Album.Title,
				Artist:     album.Album.ArtistName,
				Track:      track.Number,
				DiscNumber: track.Disc,
				Year:       album.Album.Year,
				Duration:   track.DurationMS / 1000,
				CoverArt:   subsonicAlbumID(album.Album.ID),
			})
		}
	}
	entry.SongCount = len(entry.Songs)
	// Report the playable total rather than the catalog sum so the album
	// duration matches the songs actually returned to the client.
	entry.Duration = 0
	for _, song := range entry.Songs {
		entry.Duration += song.Duration
	}
	out := subsonicResponseFor(s.version)
	out.Album = &entry
	writeSubsonic(w, r, out)
}

// subsonicDirectory serves getMusicDirectory. An "ar" id browses an artist's
// albums; an "al" id browses an album's songs; the music-folder id browses all
// artists. Distinct prefixes keep those three cases from colliding.
func (s *Server) subsonicDirectory(w http.ResponseWriter, r *http.Request) {
	raw := strings.TrimSpace(r.URL.Query().Get("id"))
	// A bare integer is ambiguous here, so probe the catalog and adopt the
	// namespace that actually exists before dispatching on the prefix below.
	// The music-folder id and "root" are reserved and never probed.
	if raw != strconv.Itoa(subsonicFolderID) && !strings.EqualFold(raw, "root") {
		_, artist := subsonicParseID(raw, subsonicArtistPrefix)
		_, album := subsonicParseID(raw, subsonicAlbumPrefix)
		if !artist && !album {
			if value, err := strconv.ParseInt(raw, 10, 64); err == nil && value > 0 {
				if _, err := s.music.Artist(r.Context(), value); err == nil {
					raw = subsonicArtistID(value)
				} else if _, err := s.music.ServerAlbum(r.Context(), value); err == nil {
					raw = subsonicAlbumID(value)
				}
			}
		}
	}
	if id, ok := subsonicParseID(raw, subsonicArtistPrefix); ok {
		details, err := s.music.Artist(r.Context(), id)
		if err != nil {
			writeSubsonic(w, r, subsonicFailure(s.version, 70, "Directory not found."))
			return
		}
		dir := &subsonicDirectory{ID: raw, Parent: strconv.Itoa(subsonicFolderID), Name: details.Artist.Name, Children: []subsonicSong{}}
		for _, album := range details.Albums {
			dir.Children = append(dir.Children, subsonicAlbumChild(album, subsonicArtistID(details.Artist.ID)))
		}
		out := subsonicResponseFor(s.version)
		out.Directory = dir
		writeSubsonic(w, r, out)
		return
	}
	if id, ok := subsonicParseID(raw, subsonicAlbumPrefix); ok {
		album, err := s.music.ServerAlbum(r.Context(), id)
		if err != nil {
			writeSubsonic(w, r, subsonicFailure(s.version, 70, "Directory not found."))
			return
		}
		dir := &subsonicDirectory{ID: raw, Parent: subsonicArtistID(album.Album.ArtistID), Name: album.Album.Title, Children: []subsonicSong{}}
		if songs, err := s.music.ServerAlbumSongs(r.Context(), id); err == nil {
			for _, song := range songs {
				dir.Children = append(dir.Children, subsonicSongFrom(song))
			}
		}
		out := subsonicResponseFor(s.version)
		out.Directory = dir
		writeSubsonic(w, r, out)
		return
	}
	if raw == strconv.Itoa(subsonicFolderID) || strings.EqualFold(raw, "root") {
		dir := &subsonicDirectory{ID: strconv.Itoa(subsonicFolderID), Name: subsonicFolderName, Children: []subsonicSong{}}
		if indexes, err := s.subsonicArtistGroups(r.Context()); err == nil {
			for _, index := range indexes {
				for _, artist := range index.Artists {
					dir.Children = append(dir.Children, subsonicSong{ID: artist.ID, Parent: dir.ID, Title: artist.Name, Artist: artist.Name, IsDir: true})
				}
			}
		}
		out := subsonicResponseFor(s.version)
		out.Directory = dir
		writeSubsonic(w, r, out)
		return
	}
	writeSubsonic(w, r, subsonicFailure(s.version, 0, "A valid directory ID is required."))
}

func (s *Server) subsonicAlbumInfo(w http.ResponseWriter, r *http.Request) {
	out := subsonicResponseFor(s.version)
	info := &subsonicAlbumInfo{}
	if id, ok := subsonicRequestID(r, subsonicAlbumPrefix); ok {
		if album, err := s.music.ServerAlbum(r.Context(), id); err == nil {
			info.MusicBrainzID = album.Album.MusicBrainzID
		}
	}
	if subsonicAction(r) == "getalbuminfo" {
		out.AlbumInfo = info
	} else {
		out.AlbumInfo2 = info
	}
	writeSubsonic(w, r, out)
}

func (s *Server) subsonicAlbumList(w http.ResponseWriter, r *http.Request, second bool) {
	size := subsonicCount(r, "size", 50, 500)
	offset := 0
	if value, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && value > 0 {
		offset = value
	}
	if offset > subsonicAllSongsOffsetCap {
		offset = subsonicAllSongsOffsetCap
	}
	order := "alphabeticalByName"
	switch r.URL.Query().Get("type") {
	case "newest":
		order = "newest"
	case "recent":
		order = "recent"
	case "frequent":
		order = "frequent"
	case "highest", "starred":
		order = "starred"
	case "byYear":
		order = "byYear"
	case "alphabeticalByArtist":
		order = "alphabeticalByArtist"
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
	if second {
		out.AlbumList2 = list
	} else {
		out.AlbumList = list
	}
	writeSubsonic(w, r, out)
}

func (s *Server) subsonicSong(w http.ResponseWriter, r *http.Request) {
	id, ok := subsonicRequestID(r, subsonicSongPrefix)
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

func (s *Server) subsonicSearch(w http.ResponseWriter, r *http.Request, third bool) {
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
	if third {
		out.SearchResult3 = result
	} else {
		out.SearchResult2 = result
	}
	writeSubsonic(w, r, out)
}

// subsonicAllSongs serves getSongs: an alphabetical listing of the whole
// catalog with size/offset paging. Some clients use it instead of
// getAlbumList2 for the initial library sync.
func (s *Server) subsonicAllSongs(w http.ResponseWriter, r *http.Request) {
	size := subsonicCount(r, "size", 500, 500)
	offset := subsonicCount(r, "offset", 0, 1000000)
	// An empty query turns into a match-all LIKE pattern, so the existing
	// catalog search doubles as the full listing. The fetch bound is clamped
	// against the catalog size so a paged client can never materialize
	// size+offset rows beyond the catalog; an offset past the catalog end is
	// an honest empty page for paged clients, not an error.
	count, err := s.music.ServerSongCount(r.Context())
	if err != nil {
		writeSubsonic(w, r, subsonicFailure(s.version, 0, "Could not read songs."))
		return
	}
	if offset >= count {
		out := subsonicResponseFor(s.version)
		out.Songs = &subsonicSongs{Songs: []subsonicSong{}}
		writeSubsonic(w, r, out)
		return
	}
	limit := size + offset
	if limit > count {
		limit = count
	}
	songs, err := s.music.ServerSongs(r.Context(), "", limit)
	if err != nil {
		writeSubsonic(w, r, subsonicFailure(s.version, 0, "Could not read songs."))
		return
	}
	songs = songs[offset:]
	if len(songs) > size {
		songs = songs[:size]
	}
	out := subsonicResponseFor(s.version)
	list := &subsonicSongs{Songs: []subsonicSong{}}
	for _, song := range songs {
		list.Songs = append(list.Songs, subsonicSongFrom(song))
	}
	out.Songs = list
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

func (s *Server) subsonicTopSongs(w http.ResponseWriter, r *http.Request) {
	count := subsonicCount(r, "count", 50, 500)
	out := subsonicResponseFor(s.version)
	list := &subsonicSongs{Songs: []subsonicSong{}}
	// The topSongsByArtistId extension lets clients ask by artist id instead of
	// by artist name. Prefer the id when it is present and well formed: it is
	// exact, while the name path is a substring match.
	if strings.TrimSpace(r.URL.Query().Get("id")) != "" {
		if id, ok := subsonicRequestID(r, subsonicArtistPrefix); ok {
			if songs, err := s.music.ServerArtistSongs(r.Context(), id, count); err == nil {
				for _, song := range songs {
					list.Songs = append(list.Songs, subsonicSongFrom(song))
				}
			}
		}
	} else if artist := strings.TrimSpace(r.URL.Query().Get("artist")); artist != "" {
		if songs, err := s.music.ServerSongs(r.Context(), artist, count); err == nil {
			for _, song := range songs {
				list.Songs = append(list.Songs, subsonicSongFrom(song))
			}
		}
	}
	out.TopSongs = list
	writeSubsonic(w, r, out)
}

func (s *Server) subsonicSimilarSongs(w http.ResponseWriter, r *http.Request) {
	out := subsonicResponseFor(s.version)
	list := &subsonicSongs{Songs: []subsonicSong{}}
	if subsonicAction(r) == "getsimilarsongs" {
		out.SimilarSongs = list
	} else {
		out.SimilarSongs2 = list
	}
	writeSubsonic(w, r, out)
}

func (s *Server) subsonicSongsByGenre(w http.ResponseWriter, r *http.Request) {
	out := subsonicResponseFor(s.version)
	out.SongsByGenre = &subsonicSongs{Songs: []subsonicSong{}}
	writeSubsonic(w, r, out)
}

func (s *Server) subsonicScanStatus(w http.ResponseWriter, r *http.Request) {
	count := 0
	if value, err := s.music.ServerSongCount(r.Context()); err == nil {
		count = value
	}
	out := subsonicResponseFor(s.version)
	out.ScanStatus = &subsonicScanStatus{Scanning: false, Count: count}
	writeSubsonic(w, r, out)
}

func (s *Server) subsonicPlaylists(w http.ResponseWriter, r *http.Request) {
	out := subsonicResponseFor(s.version)
	out.Playlists = &subsonicPlaylists{Playlists: []subsonicPlaylist{}}
	writeSubsonic(w, r, out)
}

func (s *Server) subsonicPlaylist(w http.ResponseWriter, r *http.Request) {
	raw := strings.TrimSpace(r.URL.Query().Get("id"))
	out := subsonicResponseFor(s.version)
	out.Playlist = &subsonicPlaylist{ID: raw, Name: "Playlist", Entry: []subsonicSong{}}
	writeSubsonic(w, r, out)
}

func (s *Server) subsonicStarred(w http.ResponseWriter, r *http.Request, second bool) {
	out := subsonicResponseFor(s.version)
	starred := &subsonicStarred{Artists: []subsonicArtist{}, Albums: []subsonicAlbum{}, Songs: []subsonicSong{}}
	if second {
		out.Starred2 = starred
	} else {
		out.Starred = starred
	}
	writeSubsonic(w, r, out)
}

func (s *Server) subsonicUser(w http.ResponseWriter, r *http.Request) {
	username := subsonicUserParam(r)
	out := subsonicResponseFor(s.version)
	account := subsonicRequestUser(r)
	if !account.Anonymous() && account.Username != "" {
		username = account.Username
	}
	// Report the roles that actually govern this account. Scrobble requests
	// are rejected explicitly (see the scrobble case: writes are not
	// implemented), and the legacy shared token resolves to the sentinel
	// account whose explicit full roles — including AdminRole — are reported
	// honestly (NEW-E4), so existing clients never lose access and a client
	// that gates admin UI on AdminRole sees the real state.
	out.User = &subsonicUser{
		Username:          username,
		Email:             account.Email,
		ScrobblingEnabled: account.ScrobblingEnabled,
		AdminRole:         account.AdminRole,
		SettingsRole:      account.CanSettings(),
		DownloadRole:      account.CanDownload(),
		UploadRole:        account.CanUpload(),
		PlaylistRole:      account.CanPlaylist(),
		CoverArtRole:      account.CanCoverArt(),
		ShareRole:         account.CanShare(),
	}
	writeSubsonic(w, r, out)
}

// subsonicUserParam resolves the acting account name. Subsonic requires ?u=
// for authentication, while getUser is specified to take ?username=. Accept
// both so the value always reflects the authenticated caller.
func subsonicUserParam(r *http.Request) string {
	if username := strings.TrimSpace(r.URL.Query().Get("username")); username != "" {
		return username
	}
	return strings.TrimSpace(r.URL.Query().Get("u"))
}

// subsonicAvatarPNG is a 1x1 transparent PNG. ReelScarey has no user avatars,
// but several clients request one and treat a missing image as an error.
var subsonicAvatarPNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
	0x89, 0x00, 0x00, 0x00, 0x0a, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae,
	0x42, 0x60, 0x82,
}

func (s *Server) subsonicAvatar(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(subsonicAvatarPNG)
}

func (s *Server) subsonicStream(w http.ResponseWriter, r *http.Request) {
	id, ok := subsonicRequestID(r, subsonicSongPrefix)
	if !ok {
		writeSubsonic(w, r, subsonicFailure(s.version, 0, "A valid song ID is required."))
		return
	}
	// Convert when the client asked for another format or when the native
	// container is one the client cannot decode; the pristine file is streamed
	// byte-for-byte otherwise.
	playback, err := s.music.OpenPlayback(r.Context(), id, r.URL.Query().Get("format"),
		subsonicQueryInt(r, "maxBitRate"), subsonicQueryInt(r, "timeOffset"))
	if err != nil {
		writeSubsonic(w, r, subsonicFailure(s.version, 70, "Song file not found."))
		return
	}
	defer playback.Close()
	w.Header().Set("Content-Type", playback.ContentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if playback.Body != nil {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Accept-Ranges", "none")
		if subsonicAction(r) == "download" {
			w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", playback.Name))
		} else {
			w.Header().Set("Content-Disposition", "inline")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.Copy(w, playback.Body)
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=3600")
	if subsonicAction(r) == "download" {
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", playback.Name))
	} else {
		w.Header().Set("Content-Disposition", "inline")
	}
	http.ServeContent(w, r, playback.Name, playback.ModTime, playback.File)
}

// subsonicQueryInt parses a bounded non-negative integer query parameter.
func subsonicQueryInt(r *http.Request, key string) int {
	value, err := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get(key)))
	if err != nil || value < 0 {
		return 0
	}
	return value
}

func (s *Server) subsonicCoverArt(w http.ResponseWriter, r *http.Request) {
	id, ok := subsonicRequestID(r, subsonicAlbumPrefix)
	if !ok {
		writeSubsonic(w, r, subsonicFailure(s.version, 0, "A valid cover ID is required."))
		return
	}
	art, err := s.music.AlbumImage(r.Context(), id)
	if err != nil {
		writeSubsonic(w, r, subsonicFailure(s.version, 70, "Cover art not found."))
		return
	}
	defer art.File.Close()
	w.Header().Set("Content-Type", art.ContentType)
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, art.Name, art.ModTime, art.File)
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
	sort.Slice(keys, func(i, j int) bool {
		if keys[i] == "#" {
			return false
		}
		if keys[j] == "#" {
			return true
		}
		return keys[i] < keys[j]
	})
	return keys
}
