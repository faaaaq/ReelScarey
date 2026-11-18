package httpapi

import (
	"context"
	"fmt"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/music"
	"github.com/faaaaq/ReelScarey/internal/securestore"
)

func TestSubsonicNamedUserPlaybackStateEndToEnd(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := database.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err = db.Exec(`INSERT INTO music_artists(id,musicbrainz_id,name,monitored,status,created_at,updated_at) VALUES(1,'artist-local','Artist',1,'available',1,1);
		INSERT INTO music_albums(id,artist_id,musicbrainz_id,title,monitored,status,created_at,updated_at) VALUES(2,1,'album-local','Album',1,'available',1,1);
		INSERT INTO music_tracks(id,album_id,musicbrainz_id,recording_id,disc,number,title,duration_ms,created_at,updated_at) VALUES(3,2,'track-local','recording-local',1,1,'Song',1000,1,1);
		INSERT INTO music_files(id,artist_id,album_id,track_id,root,relative_path,size_bytes,sha256,format,duration_ms,tag_json,imported_at,origin,source_name) VALUES(1,1,2,3,'Music','Music/Artist/Album/01 Song.mp3',7,'sha','mp3',1000,'{}',1,'existing','file.mp3')`); err != nil {
		t.Fatal(err)
	}
	newService := func() *music.Service {
		service := music.NewService(db, t.TempDir())
		secrets, secretErr := securestore.New(ctx, db, dir)
		if secretErr != nil {
			t.Fatal(secretErr)
		}
		service.SetSecrets(secrets)
		return service
	}
	service := newService()
	if _, err = service.SaveMusicServer(ctx, true, 1); err != nil {
		t.Fatal(err)
	}
	for _, account := range []struct {
		name, password string
		scrobbling     *bool
		admin          *bool
	}{{"alice", "alice-pass", nil, nil}, {"bob", "bob-pass", nil, nil}, {"carol", "carol-pass", func() *bool { value := false; return &value }(), nil}, {"dave", "dave-pass", func() *bool { value := false; return &value }(), func() *bool { value := true; return &value }()}} {
		if _, _, err = service.CreateMusicServerUser(ctx, music.MusicServerUserInput{Username: account.name, Password: account.password, ScrobblingEnabled: account.scrobbling, AdminRole: account.admin}); err != nil {
			t.Fatal(err)
		}
	}
	handler, _ := musicFixture(t, service)
	request := func(rawURL string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", rawURL, nil)
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, r)
		return out
	}
	auth := func(user, password string) string {
		return "u=" + url.QueryEscape(user) + "&p=" + url.QueryEscape(password) + "&v=1.16.1&c=state-test"
	}
	alice, bob, carol := auth("alice", "alice-pass"), auth("bob", "bob-pass"), auth("carol", "carol-pass")
	dave := auth("dave", "dave-pass")
	requireOK := func(action, authQuery, query string) string {
		t.Helper()
		out := request("/rest/" + action + ".view?" + authQuery + query)
		if out.Code != 200 || !strings.Contains(out.Body.String(), `status="ok"`) {
			t.Fatalf("%s: %d %s", action, out.Code, out.Body.String())
		}
		return out.Body.String()
	}
	requireFailure := func(action, authQuery, query, code string) string {
		t.Helper()
		out := request("/rest/" + action + ".view?" + authQuery + query)
		body := out.Body.String()
		if out.Code != 200 || !strings.Contains(body, `status="failed"`) || !strings.Contains(body, `code="`+code+`"`) {
			t.Fatalf("%s: expected code %s: %d %s", action, code, out.Code, body)
		}
		return body
	}

	requireOK("star", alice, "&id=tr1&albumId=al2&artistId=ar1")
	for _, id := range []string{"tr1", "al2", "ar1"} {
		requireOK("setRating", alice, "&id="+id+"&rating=5")
	}
	for action, query := range map[string]string{"getSong": "&id=tr1", "getAlbum": "&id=al2", "getArtist": "&id=ar1"} {
		body := requireOK(action, alice, query)
		if !strings.Contains(body, `userRating="5"`) || !strings.Contains(body, `starred="`) {
			t.Fatalf("%s lacks per-user mark decoration: %s", action, body)
		}
		body = requireOK(action, bob, query)
		if strings.Contains(body, `userRating=`) || strings.Contains(body, `starred=`) {
			t.Fatalf("%s leaked alice's mark to bob: %s", action, body)
		}
	}

	requireOK("scrobble", alice, "&id=tr1&submission=false")
	requireOK("scrobble", bob, "&id=tr1&submission=false")
	body := requireOK("getNowPlaying", bob, "")
	for _, expected := range []string{`username="alice"`, `username="bob"`, `minutesAgo="0"`} {
		if !strings.Contains(body, expected) {
			t.Fatalf("all-user now playing lacks %s: %s", expected, body)
		}
	}
	// A repeated id/time batch is all-or-nothing, and a time-count mismatch is
	// malformed rather than a partially accepted submission.
	nowMS := time.Now().UnixMilli()
	requireFailure("scrobble", alice, fmt.Sprintf("&id=tr1&id=tr1&time=%d&submission=true", nowMS), "10")
	body = requireOK("getSong", alice, "&id=tr1")
	if strings.Contains(body, `playCount="`) {
		t.Fatalf("malformed batch wrote a play event: %s", body)
	}
	requireOK("scrobble", alice, fmt.Sprintf("&id=tr1&id=tr1&time=%d&time=%d&submission=true", nowMS-2000, nowMS-1000))
	body = requireOK("getSong", alice, "&id=tr1")
	if !strings.Contains(body, `playCount="2"`) || !strings.Contains(body, `userRating="5"`) || !strings.Contains(body, `played="`) {
		t.Fatalf("song state missing after batch: %s", body)
	}
	body = requireOK("getSong", bob, "&id=tr1")
	if strings.Contains(body, `playCount="2"`) {
		t.Fatalf("bob inherited alice's play count: %s", body)
	}
	requireFailure("scrobble", carol, "&id=tr1&submission=true", "50")
	// Administrator role does not contradict an explicitly disabled scrobbling
	// capability advertised by getUser.
	requireFailure("scrobble", dave, "&id=tr1&submission=true", "50")
	body = requireOK("getSong", carol, "&id=tr1")
	if strings.Contains(body, `playCount="`) {
		t.Fatalf("scrobbling-disabled account wrote state: %s", body)
	}

	requireOK("savePlayQueueByIndex", alice, "&id=tr1&id=tr1&currentIndex=1&position=23456")
	requireFailure("savePlayQueueByIndex", alice, "&id=tr1&id=tr1&currentIndex=0&position=bad", "10")
	body = requireOK("getPlayQueueByIndex", alice, "")
	for _, expected := range []string{`<playQueueByIndex`, `currentIndex="1"`, `position="23456"`, `username="alice"`, `changedBy="state-test"`} {
		if !strings.Contains(body, expected) {
			t.Fatalf("index queue lacks %s: %s", expected, body)
		}
	}
	if strings.Contains(body, `<playQueue `) || strings.Count(body, `<entry `) != 2 {
		t.Fatalf("index queue used the wrong wrapper or lost duplicates: %s", body)
	}
	body = requireOK("getPlayQueue", bob, "")
	if strings.Contains(body, `<entry `) || strings.Contains(body, `currentIndex=`) {
		t.Fatalf("bob inherited alice's queue: %s", body)
	}

	body = requireOK("createPlaylist", alice, "&name=Mix&comment=Shared+mix&public=true&songId=tr1&songId=tr1")
	for _, expected := range []string{`owner="alice"`, `name="Mix"`, `comment="Shared mix"`, `public="true"`, `songCount="2"`, `duration="2"`, `created="`, `changed="`} {
		if !strings.Contains(body, expected) {
			t.Fatalf("playlist metadata lacks %s: %s", expected, body)
		}
	}
	body = requireOK("getPlaylists", bob, "")
	if !strings.Contains(body, `name="Mix"`) || !strings.Contains(body, `owner="alice"`) {
		t.Fatalf("bob could not read alice's public playlist: %s", body)
	}
	body = requireOK("getPlaylist", bob, "&id=1")
	if strings.Count(body, `<entry `) != 2 || !strings.Contains(body, `duration="2"`) {
		t.Fatalf("cross-user playlist read: %s", body)
	}
	requireFailure("updatePlaylist", bob, "&playlistId=1&name=Stolen", "70")
	body = requireOK("createPlaylist", alice, "&playlistId=1&name=Mix2&songId=tr1&songId=tr1&songId=tr1")
	if !strings.Contains(body, `name="Mix2"`) || !strings.Contains(body, `songCount="3"`) {
		t.Fatalf("createPlaylist playlistId update: %s", body)
	}
	requireFailure("updatePlaylist", alice, "&playlistId=1&name=Bad&songIndexToRemove=bad", "10")
	body = requireOK("updatePlaylist", alice, "&playlistId=1&songIndexToRemove=1")
	if !strings.Contains(body, `name="Mix2"`) || !strings.Contains(body, `songCount="2"`) {
		t.Fatalf("songIndexToRemove did not remove exactly one position: %s", body)
	}
	requireOK("createPlaylist", bob, "&name=Secret&songId=tr1")
	body = requireOK("getPlaylists", alice, "")
	if strings.Contains(body, "Secret") {
		t.Fatalf("alice could read bob's private playlist: %s", body)
	}
	requireFailure("getPlaylist", alice, "&id=2", "70")

	for _, listType := range []string{"starred", "highest", "frequent", "recent"} {
		body = requireOK("getAlbumList2", alice, "&type="+listType)
		if !strings.Contains(body, `id="al2"`) {
			t.Fatalf("%s album list ignored alice's state: %s", listType, body)
		}
		body = requireOK("getAlbumList2", bob, "&type="+listType)
		if strings.Contains(body, `id="al2"`) {
			t.Fatalf("%s album list leaked alice's state to bob: %s", listType, body)
		}
	}
	// Validate all targets before a multi-kind star writes anything.
	requireOK("unstar", alice, "&id=tr1")
	requireFailure("star", alice, "&id=tr1&albumId=bad", "10")
	body = requireOK("getStarred", alice, "")
	if strings.Contains(body, `id="tr1"`) {
		t.Fatalf("malformed multi-kind star partially wrote state: %s", body)
	}
	requireOK("star", alice, "&id=tr1")

	// Reopen the real music database and rebuild only the HTTP fixture around
	// it. Queue index, playlist metadata, marks and events must all survive.
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = database.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	service = newService()
	handler, _ = musicFixture(t, service)
	body = requireOK("getPlayQueueByIndex", alice, "")
	if !strings.Contains(body, `currentIndex="1"`) || !strings.Contains(body, `position="23456"`) {
		t.Fatalf("queue did not survive real reopen: %s", body)
	}
	body = requireOK("getPlaylist", bob, "&id=1")
	if !strings.Contains(body, `owner="alice"`) || !strings.Contains(body, `name="Mix2"`) || !strings.Contains(body, `songCount="2"`) {
		t.Fatalf("playlist did not survive real reopen: %s", body)
	}
	body = requireOK("getSong", alice, "&id=tr1")
	if !strings.Contains(body, `userRating="5"`) || !strings.Contains(body, `playCount="2"`) {
		t.Fatalf("song state did not survive real reopen: %s", body)
	}
	out := request("/rest/getStarred.view?" + alice + "&f=json")
	if !strings.Contains(out.Body.String(), `"status":"ok"`) || !strings.Contains(out.Body.String(), `"userRating":5`) {
		t.Fatal("JSON starred contract", out.Body.String())
	}
}
