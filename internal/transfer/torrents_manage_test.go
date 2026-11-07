package transfer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

const otherHash = "fedcba9876543210fedcba9876543210fedcba98"

func problemCode(e error) string {
	var p *Error
	if errors.As(e, &p) {
		return p.Code
	}
	return ""
}

func TestRedactTrackerURL(t *testing.T) {
	for raw, want := range map[string]string{
		"":            "",
		"** [DHT] **": "** [DHT] **",
		"** [PeX] **": "** [PeX] **",
		"https://tracker.example/announce/PASSKEY":                    "https://tracker.example",
		"udp://Tracker.Example:6969/announce?passkey=PASSKEY#PASSKEY": "udp://tracker.example:6969",
		"http://user:PASSKEY@tracker.example:8080/a":                  "http://tracker.example:8080",
		"https://[2001:db8::1]:443/PASSKEY":                           "https://[2001:db8::1]:443",
		"tracker.example:80/announce/PASSKEY":                         "(hidden)",
		"ftp://tracker.example/PASSKEY":                               "(hidden)",
		"https:///PASSKEY":                                            "(hidden)",
		"https://tra%cker/PASSKEY":                                    "(hidden)",
		"not a url PASSKEY":                                           "(hidden)",
	} {
		if got := RedactTrackerURL(raw); got != want || strings.Contains(got, "PASSKEY") {
			t.Errorf("RedactTrackerURL(%q) = %q, want %q", raw, got, want)
		}
	}
	msg := redactURLsInText(`announce to "https://t.example/a/PASSKEY?x=PASSKEY" failed; retry url=udp://u.example:1/PASSKEY`)
	if strings.Contains(msg, "PASSKEY") || !strings.Contains(msg, "https://t.example") || !strings.Contains(msg, "url=udp://u.example:1") {
		t.Fatal(msg)
	}
}

func TestTorrentOriginClassification(t *testing.T) {
	for tags, want := range map[string][2]string{
		"":                         {"external", ""},
		"reelscarey":               {"external", ""},
		"reelscarey-manual":        {"manual", ""},
		"other, reelscarey-manual": {"manual", ""},
		"reelscarey-0123456789abcdef0123456789abcdef": {"job", "0123456789abcdef0123456789abcdef"},
		"mine, reelscarey-job":                        {"job", "job"},
		"reelscarey-bad/../tag":                       {"external", ""},
	} {
		v := Torrent{Tags: tags, Origin: "job", JobID: "forged"}
		classifyTorrent(&v)
		if v.Origin != want[0] || v.JobID != want[1] {
			t.Errorf("%q: %q %q", tags, v.Origin, v.JobID)
		}
	}
	var decoded Torrent
	if e := json.Unmarshal([]byte(`{"hash":"x","origin":"job","job_id":"forged","tracker":"https://t.example/PASSKEY","magnet_uri":"magnet:?tr=PASSKEY"}`), &decoded); e != nil || decoded.Origin != "" || decoded.JobID != "" || decoded.Tracker != "https://t.example" {
		t.Fatal(decoded, e)
	}
}

func TestDownsamplePieces(t *testing.T) {
	if got := downsamplePieces(nil, 512); len(got) != 0 || got == nil {
		t.Fatal(got)
	}
	if got := downsamplePieces([]int{0, 1, 2}, 512); !reflect.DeepEqual(got, []int{0, 1, 2}) {
		t.Fatal(got)
	}
	// Four buckets of three pieces: all missing, mixed missing/complete,
	// one downloading, all complete.
	in := []int{0, 0, 0, 0, 2, 2, 2, 1, 2, 2, 2, 2}
	if got := downsamplePieces(in, 4); !reflect.DeepEqual(got, []int{0, 1, 1, 2}) {
		t.Fatal(got)
	}
	large := make([]int, 1000003)
	for n := range large {
		large[n] = 2
	}
	large[len(large)-1] = 0
	got := downsamplePieces(large, 512)
	if len(got) != 512 || got[0] != 2 || got[511] != 1 {
		t.Fatal(len(got), got[0], got[511])
	}
}

// fakeQBittorrent serves the read endpoints of the built-in client with
// passkeys in every tracker-bearing field.
func fakeQBittorrent(t *testing.T, posts *[]string, status map[string]int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/v2/")
		if path == "auth/login" {
			http.SetCookie(w, &http.Cookie{Name: "SID", Value: "fixture", Path: "/"})
			fmt.Fprint(w, "Ok.")
			return
		}
		if code := status[path]; code != 0 {
			w.WriteHeader(code)
			fmt.Fprint(w, "Torrent queueing must be enabled")
			return
		}
		if r.Method == http.MethodPost {
			_ = r.ParseForm()
			*posts = append(*posts, path+"?"+r.PostForm.Encode())
			return
		}
		switch path {
		case "transfer/info":
			fmt.Fprint(w, `{"dl_info_speed":10,"up_info_speed":20,"dl_info_data":30,"up_info_data":40,"connection_status":"connected","dht_nodes":321,"dl_rate_limit":1024,"up_rate_limit":-1}`)
		case "transfer/speedLimitsMode":
			fmt.Fprint(w, "1")
		case "app/preferences":
			fmt.Fprint(w, `{"queueing_enabled":true,"alt_dl_limit":102400,"alt_up_limit":51200,"autorun_program":"PRIVATE-AUTORUN","web_ui_password":"PRIVATE-PREF","save_path":"/PRIVATE-PREF"}`)
		case "torrents/info":
			one := fmt.Sprintf(`{"hash":%q,"name":"Job","state":"downloading","tags":"reelscarey-0123456789abcdef0123456789abcdef","tracker":"https://user:PASSKEY@tracker.example/announce/PASSKEY?passkey=PASSKEY","magnet_uri":"magnet:?xt=urn:btih:%s&tr=PASSKEY","seq_dl":true,"f_l_piece_prio":false,"priority":3,"dl_limit":-1,"ratio_limit":-2,"seeding_time_limit":-2,"availability":1.5,"num_complete":7,"num_incomplete":2}`, testHash, testHash)
			two := fmt.Sprintf(`{"hash":%q,"name":"Manual","state":"uploading","tags":"reelscarey-manual","tracker":"udp://tracker.example:6969/PASSKEY","magnet_uri":"magnet:?tr=PASSKEY"}`, otherHash)
			if r.URL.Query().Get("hashes") == testHash {
				fmt.Fprint(w, "["+one+"]")
			} else if r.URL.Query().Get("hashes") == otherHash {
				fmt.Fprint(w, "["+two+"]")
			} else {
				fmt.Fprint(w, "["+one+","+two+"]")
			}
		case "torrents/files":
			fmt.Fprint(w, `[{"index":0,"name":"a.mkv","size":10,"progress":0.5,"priority":1,"availability":0.75,"is_seed":false},{"index":1,"name":"b.nfo","size":1,"progress":0,"priority":0}]`)
		case "torrents/properties":
			comment, _ := json.Marshal("\x00" + strings.Repeat("c", 3000))
			fmt.Fprintf(w, `{"save_path":"/downloads/x","creation_date":-1,"piece_size":16384,"comment":%s,"eta":8640000.0,"share_ratio":0.5,"pieces_have":3,"pieces_num":12,"created_by":"mktorrent","total_size":11}`, comment)
		case "torrents/trackers":
			fmt.Fprint(w, `[{"url":"** [DHT] **","status":2,"tier":-1,"num_peers":5,"num_seeds":0,"num_leeches":0,"num_downloaded":-1,"msg":""},{"url":"https://tracker.example/announce/PASSKEY?passkey=PASSKEY","status":4,"tier":"","num_peers":-1,"num_seeds":-1,"num_leeches":-1,"num_downloaded":-1,"msg":"error for https://tracker.example/announce/PASSKEY?passkey=PASSKEY: unregistered"}]`)
		case "sync/torrentPeers":
			if r.URL.Query().Get("rid") != "0" {
				t.Error("peer snapshot is not full")
			}
			fmt.Fprint(w, `{"full_update":true,"rid":1,"peers":{"10.0.0.2:1":{"ip":"10.0.0.2","port":1,"client":"slow","dl_speed":1,"up_speed":0,"progress":0.1,"relevance":1,"flags":"D","connection":"BT","country_code":"nl"},"10.0.0.1:2":{"ip":"10.0.0.1","port":2,"client":"fast","dl_speed":100,"up_speed":5,"progress":1,"relevance":0,"flags":"U","connection":"uTP"}}}`)
		case "torrents/webseeds":
			fmt.Fprint(w, `[{"url":"https://seed.example/files/PASSKEY/movie.mkv"}]`)
		case "torrents/pieceStates":
			fmt.Fprint(w, `[2,2,2,1,0,0,2,2,2,2,2,2]`)
		default:
			t.Error("unexpected endpoint", r.Method, path)
			w.WriteHeader(404)
		}
	}))
}

func TestTorrentListAndDetailNeverForwardPasskeysOrPreferences(t *testing.T) {
	ctx := context.Background()
	var posts []string
	server := fakeQBittorrent(t, &posts, nil)
	defer server.Close()
	f := setupWorkflow(t)
	f.s.client = NewQBittorrent()
	enableFixtureBuiltinAt(t, f, server.URL)
	overview, e := f.s.Torrents(ctx)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(overview)
	body := string(raw)
	for _, secret := range []string{"PASSKEY", "PRIVATE", "magnet_uri", "autorun"} {
		if strings.Contains(body, secret) {
			t.Fatalf("overview leaks %s: %s", secret, body)
		}
	}
	if overview.Server == nil || !overview.Server.AltSpeedEnabled || !overview.Server.QueueingEnabled || overview.Server.AltDLLimit != 100*1024 || overview.Server.AltUPLimit != 50*1024 {
		t.Fatal(overview.Server)
	}
	if overview.Transfer.DHTNodes != 321 || overview.Transfer.DLRateLimit != 1024 || overview.Transfer.UPRateLimit != 0 {
		t.Fatal(overview.Transfer)
	}
	if len(overview.Items) != 2 || overview.Items[0].Origin != "job" || overview.Items[0].JobID != "0123456789abcdef0123456789abcdef" || overview.Items[1].Origin != "manual" || overview.Items[1].JobID != "" {
		t.Fatal(overview.Items)
	}
	if v := overview.Items[0]; v.Tracker != "https://tracker.example" || !v.SequentialDL || v.Priority != 3 || v.RatioLimit != -2 || v.NumComplete != 7 || v.Availability != 1.5 {
		t.Fatal(v)
	}
	if !strings.Contains(body, `"tracker":"udp://tracker.example:6969"`) {
		t.Fatal(body)
	}

	detail, e := f.s.Torrent(ctx, strings.ToUpper(testHash))
	if e != nil {
		t.Fatal(e)
	}
	raw, _ = json.Marshal(detail)
	body = string(raw)
	if strings.Contains(body, "PASSKEY") || strings.Contains(body, "magnet_uri") || strings.Contains(body, `\u0000`) {
		t.Fatal(body)
	}
	if len(detail.Trackers) != 2 || detail.Trackers[0].URL != "** [DHT] **" || detail.Trackers[1].URL != "https://tracker.example" || detail.Trackers[1].Tier != -1 || !strings.Contains(detail.Trackers[1].Message, "unregistered") {
		t.Fatal(detail.Trackers)
	}
	if len(detail.WebSeeds) != 1 || detail.WebSeeds[0].URL != "https://seed.example" {
		t.Fatal(detail.WebSeeds)
	}
	if len(detail.Peers) != 2 || detail.Peers[0].Client != "fast" || detail.Peers[1].CountryCode != "nl" {
		t.Fatal(detail.Peers)
	}
	if len([]rune(detail.Properties.Comment)) != maxTorrentComment || detail.Properties.ETA != 8640000 || detail.Properties.CreationDate != -1 || detail.Properties.PiecesNum != 12 {
		t.Fatal(len(detail.Properties.Comment), detail.Properties)
	}
	if !reflect.DeepEqual(detail.Pieces, []int{2, 2, 2, 1, 0, 0, 2, 2, 2, 2, 2, 2}) {
		t.Fatal(detail.Pieces)
	}
	if len(detail.Files) != 2 || detail.Files[0].Availability != 0.75 {
		t.Fatal(detail.Files)
	}
	if len(posts) != 0 {
		t.Fatal("read requests changed the client", posts)
	}
}

func TestTorrentClientRejectsInvalidDetailLists(t *testing.T) {
	ctx := context.Background()
	for path, body := range map[string]string{
		"torrents/trackers":    `{"not":"a list"}`,
		"torrents/pieceStates": `[0,3]`,
		"torrents/webseeds":    `[` + strings.TrimSuffix(strings.Repeat(`{"url":""},`, maxTorrentList+1), ",") + `]`,
		"sync/torrentPeers":    `{"peers":[]}`,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/v2/auth/login" {
				http.SetCookie(w, &http.Cookie{Name: "SID", Value: "fixture", Path: "/"})
				fmt.Fprint(w, "Ok.")
				return
			}
			fmt.Fprint(w, body)
		}))
		q, c := NewQBittorrent(), ClientConfig{URL: server.URL, Username: "u", Password: "p"}
		var e error
		switch path {
		case "torrents/trackers":
			_, e = q.Trackers(ctx, c, testHash)
		case "torrents/pieceStates":
			_, e = q.PieceStates(ctx, c, testHash)
		case "torrents/webseeds":
			_, e = q.WebSeeds(ctx, c, testHash)
		case "sync/torrentPeers":
			_, e = q.Peers(ctx, c, testHash)
		}
		server.Close()
		if problemCode(e) != "client_response" {
			t.Fatal(path, e)
		}
	}
}

func TestTorrentCommandsMapQueueingConflictAndForms(t *testing.T) {
	ctx := context.Background()
	var posts []string
	server := fakeQBittorrent(t, &posts, map[string]int{"torrents/topPrio": 409, "torrents/filePrio": 409})
	defer server.Close()
	q, c := NewQBittorrent(), ClientConfig{URL: server.URL, Username: "u", Password: "p"}
	if e := q.Command(ctx, c, "queue_top", []string{testHash}, ""); problemCode(e) != "queueing_disabled" {
		t.Fatal(e)
	}
	if e := q.SetFilePriority(ctx, c, testHash, []int{0}, 0); problemCode(e) != "file_priority_rejected" {
		t.Fatal(e)
	}
	if e := q.Command(ctx, c, "set_location", []string{testHash}, "/"); problemCode(e) != "invalid_selection" {
		t.Fatal(e)
	}
	if e := q.Command(ctx, c, "force_start", []string{testHash, otherHash}, "true"); e != nil {
		t.Fatal(e)
	}
	if e := q.SetShareLimits(ctx, c, []string{testHash}, 1.5, -1); e != nil {
		t.Fatal(e)
	}
	if e := q.Command(ctx, c, "queue_down", []string{testHash}, ""); e != nil {
		t.Fatal(e)
	}
	want := []string{
		"torrents/setForceStart?hashes=" + testHash + "%7C" + otherHash + "&value=true",
		"torrents/setShareLimits?hashes=" + testHash + "&inactiveSeedingTimeLimit=-2&ratioLimit=1.5&seedingTimeLimit=-1",
		"torrents/decreasePrio?hashes=" + testHash,
	}
	if !reflect.DeepEqual(posts, want) {
		t.Fatal(posts)
	}
}

func TestTorrentBulkValidatesBeforeContactingClient(t *testing.T) {
	ctx := context.Background()
	f := setupWorkflow(t)
	enableFixtureBuiltin(t, f)
	on, off := true, false
	limit, negative, huge := int64(1024), int64(-1), int64(maxRateLimit+1)
	ratio, badRatio, minutes, badMinutes := 2.0, -3.0, int64(-1), int64(maxSeedingMinutes+1)
	many := make([]string, maxBulkTorrents+1)
	for n := range many {
		many[n] = testHash
	}
	for name, in := range map[string]TorrentBulkAction{
		"empty":           {Action: "pause"},
		"too many":        {Hashes: many, Action: "pause"},
		"bad hash":        {Hashes: []string{testHash, "not-a-hash"}, Action: "pause"},
		"unknown":         {Hashes: []string{testHash}, Action: "set_location"},
		"no enabled":      {Hashes: []string{testHash}, Action: "force_start"},
		"no limit":        {Hashes: []string{testHash}, Action: "set_download_limit"},
		"negative limit":  {Hashes: []string{testHash}, Action: "set_upload_limit", Limit: &negative},
		"huge limit":      {Hashes: []string{testHash}, Action: "set_upload_limit", Limit: &huge},
		"bad ratio":       {Hashes: []string{testHash}, Action: "set_share_limits", RatioLimit: &badRatio, SeedingTimeLimit: &minutes},
		"bad minutes":     {Hashes: []string{testHash}, Action: "set_share_limits", RatioLimit: &ratio, SeedingTimeLimit: &badMinutes},
		"missing minutes": {Hashes: []string{testHash}, Action: "set_share_limits", RatioLimit: &ratio},
		"unreviewed":      {Hashes: []string{testHash}, Action: "remove", DeleteFiles: true},
	} {
		f.client.action = ""
		e := f.s.TorrentBulk(ctx, in)
		want := "invalid_selection"
		if name == "unreviewed" {
			want = "review_required"
		}
		if problemCode(e) != want || f.client.action != "" {
			t.Fatal(name, e, f.client.action)
		}
	}
	if e := f.s.TorrentBulk(ctx, TorrentBulkAction{Hashes: []string{strings.ToUpper(testHash), testHash, otherHash}, Action: "force_start", Enabled: &off}); e != nil || f.client.action != "force_start" || f.client.lastValue != "false" || !reflect.DeepEqual(f.client.lastHashes, []string{testHash, otherHash}) {
		t.Fatal(e, f.client.action, f.client.lastHashes)
	}
	if e := f.s.TorrentBulk(ctx, TorrentBulkAction{Hashes: []string{testHash}, Action: "set_download_limit", Limit: &limit}); e != nil || f.client.action != "download_limit" || f.client.lastValue != "1024" {
		t.Fatal(e, f.client.action)
	}
	if e := f.s.TorrentBulk(ctx, TorrentBulkAction{Hashes: []string{testHash}, Action: "set_share_limits", RatioLimit: &ratio, SeedingTimeLimit: &minutes}); e != nil || f.client.action != "share_limits" {
		t.Fatal(e)
	}
	if e := f.s.TorrentBulk(ctx, TorrentBulkAction{Hashes: []string{testHash}, Action: "remove", DeleteFiles: true, Reviewed: true}); e != nil || f.client.action != "remove" || !f.client.deleteFiles {
		t.Fatal(e)
	}
	f.client.commandErr = problem("queueing_disabled", "off")
	if e := f.s.TorrentBulk(ctx, TorrentBulkAction{Hashes: []string{testHash}, Action: "queue_top"}); problemCode(e) != "queueing_disabled" {
		t.Fatal(e)
	}
	f.client.commandErr = nil
	if e := f.s.TorrentBulk(ctx, TorrentBulkAction{Hashes: []string{testHash}, Action: "sequential", Enabled: &on}); problemCode(e) != "not_found" {
		t.Fatal("missing torrent toggled", e)
	}
}

func TestTorrentBulkTogglesOnlyDifferingTorrents(t *testing.T) {
	ctx := context.Background()
	f := setupWorkflow(t)
	enableFixtureBuiltin(t, f)
	on, off := true, false
	f.client.list = []Torrent{{Hash: testHash, SequentialDL: true, FirstLastPiece: false}, {Hash: strings.ToUpper(otherHash), SequentialDL: false, FirstLastPiece: false}}
	if e := f.s.TorrentBulk(ctx, TorrentBulkAction{Hashes: []string{testHash, otherHash}, Action: "sequential", Enabled: &on}); e != nil || f.client.action != "toggle_sequential" || !reflect.DeepEqual(f.client.lastHashes, []string{otherHash}) {
		t.Fatal(e, f.client.action, f.client.lastHashes)
	}
	f.client.commands = 0
	if e := f.s.TorrentBulk(ctx, TorrentBulkAction{Hashes: []string{testHash, otherHash}, Action: "first_last_piece", Enabled: &off}); e != nil || f.client.commands != 0 {
		t.Fatal("unchanged torrents were toggled", e, f.client.commands)
	}
	if e := f.s.TorrentBulk(ctx, TorrentBulkAction{Hashes: []string{testHash}, Action: "first_last_piece", Enabled: &on}); e != nil || f.client.action != "toggle_first_last" || !reflect.DeepEqual(f.client.lastHashes, []string{testHash}) {
		t.Fatal(e, f.client.lastHashes)
	}
}

func TestTorrentFilePriorityProtectsJobsAndValidatesIndexes(t *testing.T) {
	ctx := context.Background()
	f := setupWorkflow(t)
	enableFixtureBuiltin(t, f)
	f.client.files = []TorrentFile{{Index: 0, Name: "a.mkv"}, {Index: 1, Name: "b.nfo"}}
	f.client.torrent = &Torrent{Hash: testHash, Tags: "reelscarey-0123456789abcdef0123456789abcdef"}
	if e := f.s.SetTorrentFilePriority(ctx, testHash, []int{1}, 0); problemCode(e) != "managed_torrent" || f.client.action != "" {
		t.Fatal(e)
	}
	f.client.torrent = &Torrent{Hash: testHash, Tags: "reelscarey-manual"}
	for name, v := range map[string]struct {
		hash     string
		indexes  []int
		priority int
	}{
		"bad hash":     {"nope", []int{0}, 1},
		"bad priority": {testHash, []int{0}, 2},
		"empty":        {testHash, nil, 1},
		"duplicate":    {testHash, []int{0, 0}, 1},
		"negative":     {testHash, []int{-1}, 1},
		"missing":      {testHash, []int{0, 2}, 1},
		"too many":     {testHash, make([]int, maxFileIndexes+1), 1},
	} {
		if e := f.s.SetTorrentFilePriority(ctx, v.hash, v.indexes, v.priority); problemCode(e) != "invalid_selection" || f.client.action != "" {
			t.Fatal(name, e)
		}
	}
	if e := f.s.SetTorrentFilePriority(ctx, testHash, []int{1, 0}, 7); e != nil || f.client.action != "file_priority" || f.client.lastValue != testHash+"[1 0] 7" {
		t.Fatal(e, f.client.lastValue)
	}
	f.client.torrent = nil
	if e := f.s.SetTorrentFilePriority(ctx, testHash, []int{0}, 1); problemCode(e) != "not_found" {
		t.Fatal(e)
	}
}

func TestTorrentSpeedLimitsToggleOnlyWhenDifferent(t *testing.T) {
	ctx := context.Background()
	f := setupWorkflow(t)
	enableFixtureBuiltin(t, f)
	on, off := true, false
	limit, bad := int64(2048), int64(-5)
	if e := f.s.SetTorrentSpeedLimits(ctx, TorrentSpeedLimits{}); problemCode(e) != "invalid_selection" {
		t.Fatal(e)
	}
	if e := f.s.SetTorrentSpeedLimits(ctx, TorrentSpeedLimits{DownloadLimit: &bad}); problemCode(e) != "invalid_selection" || f.client.globalLimits != nil {
		t.Fatal(e)
	}
	if e := f.s.SetTorrentSpeedLimits(ctx, TorrentSpeedLimits{AltEnabled: &off}); e != nil || f.client.toggles != 0 {
		t.Fatal("unchanged mode toggled", e)
	}
	if e := f.s.SetTorrentSpeedLimits(ctx, TorrentSpeedLimits{AltEnabled: &on, UploadLimit: &limit}); e != nil || f.client.toggles != 1 || !f.client.altSpeed || f.client.globalLimits[true] != 2048 {
		t.Fatal(e, f.client.toggles, f.client.globalLimits)
	}
	if e := f.s.SetTorrentSpeedLimits(ctx, TorrentSpeedLimits{AltEnabled: &on}); e != nil || f.client.toggles != 1 {
		t.Fatal("unchanged mode toggled", e)
	}
	zero := int64(0)
	if e := f.s.SetTorrentSpeedLimits(ctx, TorrentSpeedLimits{DownloadLimit: &zero}); e != nil || f.client.globalLimits[false] != 0 {
		t.Fatal(e)
	}
}

func TestTorrentManagementRequiresBuiltinClient(t *testing.T) {
	ctx := context.Background()
	f := setupWorkflow(t)
	on := true
	if e := f.s.TorrentBulk(ctx, TorrentBulkAction{Hashes: []string{testHash}, Action: "pause"}); problemCode(e) != "builtin_unavailable" {
		t.Fatal(e)
	}
	if e := f.s.SetTorrentFilePriority(ctx, testHash, []int{0}, 1); problemCode(e) != "builtin_unavailable" {
		t.Fatal(e)
	}
	if e := f.s.SetTorrentSpeedLimits(ctx, TorrentSpeedLimits{AltEnabled: &on}); problemCode(e) != "builtin_unavailable" {
		t.Fatal(e)
	}
}
