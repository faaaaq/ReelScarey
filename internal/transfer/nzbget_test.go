package transfer

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestNZBGetLostAppendResponseReconcilesWithoutResubmitting(t *testing.T) {
	f := setupWorkflow(t)
	installUsenet(t, f)
	ctx := context.Background()
	var mu sync.Mutex
	tag := ""
	adds := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		var in struct {
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if json.NewDecoder(r.Body).Decode(&in) != nil {
			w.WriteHeader(400)
			return
		}
		var out any
		switch in.Method {
		case "version":
			out = "26.3"
		case "config":
			out = []nzbgetOption{{"Category1.Name", "reelscarey"}, {"Category1.DestDir", "/completed/ReelScarey"}}
		case "append":
			adds++
			json.Unmarshal(in.Params[6], &tag)
			connection, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			connection.Close()
			return
		case "listgroups":
			groups := []nzbgetGroup{}
			if tag != "" {
				groups = append(groups, nzbgetGroup{ID: 47, Kind: "NZB", Name: tag, DupeKey: tag, Category: "reelscarey", Status: "DOWNLOADING", Destination: "/completed/ReelScarey/" + tag, TotalLow: 100, RemainingLow: 100})
			}
			out = groups
		case "history":
			out = []nzbgetGroup{}
		default:
			t.Error("unexpected RPC", in.Method)
		}
		json.NewEncoder(w).Encode(map[string]any{"id": 1, "result": out})
	}))
	defer server.Close()
	password := "fixture-private-password"
	saved, e := f.s.SaveDownloadClient(ctx, DownloadClientUpdate{Name: "Lost reply NZBGet", Kind: "nzbget", URL: server.URL, Username: "fixture", Password: &password, DownloadPath: "/completed"})
	if e != nil {
		t.Fatal(e)
	}
	saved, e = f.s.SaveDownloadClient(ctx, DownloadClientUpdate{ID: saved.ID, Name: saved.Name, Kind: saved.Kind, Revision: saved.Revision, Enabled: true, URL: server.URL, Username: "fixture", DownloadPath: "/completed"})
	if e != nil {
		t.Fatal(e)
	}
	d, e := f.s.ClientDefaults(ctx)
	if e != nil {
		t.Fatal(e)
	}
	d.Usenet = saved.ID
	if _, e = f.s.SaveClientDefaults(ctx, d); e != nil {
		t.Fatal(e)
	}
	j := queueUsenet(t, f, f.movie.ID, false)
	f.tick(t)
	current, e := f.s.Job(ctx, j.ID)
	if e != nil || current.attempted != 1 || current.ClientID != "" {
		t.Fatal("uncertain append was not fenced", e)
	}
	f.reopen(t)
	f.tick(t)
	f.tick(t)
	current, e = f.s.Job(ctx, j.ID)
	if e != nil || current.ClientID != "47" || current.State != "downloading" {
		t.Fatal("lost append did not reconcile", current, e)
	}
	mu.Lock()
	defer mu.Unlock()
	if adds != 1 {
		t.Fatal("uncertain append was repeated", adds)
	}
}

func TestNZBGetRPCSubmissionIdentityHistoryAndFailureFences(t *testing.T) {
	ctx := context.Background()
	state := "DOWNLOADING"
	duplicate := false
	name := "reelscarey-fixture"
	adds := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		if !ok || user != "fixture" || password != "fixture-password" || r.URL.Path != "/jsonrpc" || r.Method != "POST" {
			w.WriteHeader(403)
			return
		}
		var in struct {
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if json.NewDecoder(r.Body).Decode(&in) != nil {
			t.Error("invalid JSON")
			w.WriteHeader(400)
			return
		}
		var out any
		switch in.Method {
		case "version":
			out = "26.3"
		case "config":
			out = []nzbgetOption{{"Category1.Name", "reelscarey"}, {"Category1.DestDir", "/completed/ReelScarey"}}
		case "append":
			if len(in.Params) != 9 {
				t.Error("append signature changed")
			}
			var file, body, category, key string
			json.Unmarshal(in.Params[0], &file)
			json.Unmarshal(in.Params[1], &body)
			json.Unmarshal(in.Params[2], &category)
			json.Unmarshal(in.Params[6], &key)
			decoded, e := base64.StdEncoding.DecodeString(body)
			if e != nil || string(decoded) != ownedNZB || file != name+".nzb" || category != "reelscarey" || key != name {
				t.Error("append lost identity/content")
			}
			adds++
			out = 41
		case "listgroups", "history":
			groups := []nzbgetGroup{}
			if adds > 0 && ((state == "DOWNLOADING" && in.Method == "listgroups") || (state != "DOWNLOADING" && in.Method == "history")) {
				g := nzbgetGroup{ID: 41, Kind: "NZB", Name: name, DupeKey: "reelscarey-fixture", Category: "reelscarey", Status: state, Destination: "/completed/ReelScarey/" + name, TotalLow: 100, RemainingLow: 20, Unpack: "NONE", Par: "NONE", Delete: "NONE"}
				groups = append(groups, g)
				if duplicate {
					groups = append(groups, g)
				}
			}
			out = groups
		default:
			t.Error("unexpected RPC method", in.Method)
		}
		json.NewEncoder(w).Encode(map[string]any{"id": 1, "result": out})
	}))
	defer server.Close()
	n := NewNZBGet()
	c := ClientConfig{URL: server.URL, Username: "fixture", Password: "fixture-password", DownloadPath: "/completed"}
	if e := n.Test(ctx, c); e != nil {
		t.Fatal(e)
	}
	id, e := n.Add(ctx, c, name, []byte(ownedNZB))
	if e != nil || id != "41" {
		t.Fatal(id, e)
	}
	job, e := n.Find(ctx, c, id, name)
	if e != nil || job.Complete || job.Progress != 0.8 {
		t.Fatal(job, e)
	}
	state = "SUCCESS/HEALTH"
	job, e = n.Find(ctx, c, id, name)
	if e != nil || !job.Complete || job.Progress != 1 {
		t.Fatal(job, e)
	}
	state = "SUCCESS/MARK"
	job, e = n.Find(ctx, c, id, name)
	if e != nil || !job.Failed || job.Complete {
		t.Fatal("manual success trusted", job, e)
	}
	state = "SUCCESS/HEALTH"
	duplicate = true
	if _, e = n.Find(ctx, c, id, name); e == nil {
		t.Fatal("duplicate job accepted")
	}
	duplicate = false
	name = "renamed"
	if _, e = n.Find(ctx, c, id, "reelscarey-fixture"); e == nil {
		t.Fatal("renamed job accepted")
	}
	if _, e = n.Find(ctx, c, "-1", name); e == nil {
		t.Fatal("invalid job ID accepted")
	}
	c.Password = "wrong"
	if e = n.Test(ctx, c); e == nil {
		t.Fatal("authentication failure accepted")
	}
}
func TestNZBGetRPCRejectsUnsafeConfigurationAndRedirects(t *testing.T) {
	for _, script := range []string{"Renamer.py", ""} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var in struct {
				Method string `json:"method"`
			}
			json.NewDecoder(r.Body).Decode(&in)
			var out any = "26.3"
			if in.Method == "config" {
				out = []nzbgetOption{{"Category1.Name", "reelscarey"}, {"Category1.DestDir", "/wrong"}, {"Extensions", script}}
			}
			json.NewEncoder(w).Encode(map[string]any{"id": 1, "result": out})
		}))
		if e := NewNZBGet().Test(context.Background(), ClientConfig{URL: server.URL, DownloadPath: "/completed"}); e == nil {
			t.Fatal("unsafe output accepted", script)
		}
		server.Close()
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://example.invalid/credential-trap", 302)
	}))
	defer server.Close()
	if e := NewNZBGet().Test(context.Background(), ClientConfig{URL: server.URL, Username: "fixture", Password: "private-password"}); e == nil || strings.Contains(e.Error(), "private-password") {
		t.Fatal(e)
	}
}
