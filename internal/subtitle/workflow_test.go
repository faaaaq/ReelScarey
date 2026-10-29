package subtitle

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/faaaaq/ReelScarey/internal/config"
	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/securestore"
	"github.com/faaaaq/ReelScarey/internal/transfer"
)

const validSRT = "1\n00:00:01,000 --> 00:00:03,000\nGenerated subtitle fixture.\n"

type fixtureProvider struct {
	content             []byte
	searches, downloads int
	searchErr, fetchErr error
	matches             []Candidate
	onFetch             func()
	remaining           *int
	target              Target
}

func (*fixtureProvider) Test(context.Context, Credentials) error { return nil }
func (p *fixtureProvider) Search(_ context.Context, _ Credentials, target Target, _ []string) ([]Candidate, error) {
	p.searches++
	p.target = target
	return slices.Clone(p.matches), p.searchErr
}

func TestRegisteredMultiEpisodeFileUsesItsAssociationsAndThenAutomaticChecks(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	for _, query := range []string{
		`UPDATE media_items SET kind='series',title='Test Show' WHERE id=1`,
		`INSERT INTO series_details(media_id) VALUES(1)`,
		`INSERT INTO seasons(id,series_id,tmdb_id,number,title,overview,air_date,episode_count) VALUES(1,1,10,2,'Season 2','','',2)`,
		`INSERT INTO episodes(id,season_id,tmdb_id,number,title,overview,air_date) VALUES(1,1,101,3,'First','',''),(2,1,102,4,'Second','','')`,
		`UPDATE media_files SET movie_id=NULL,series_id=1 WHERE id=1`,
		`INSERT INTO episode_files(episode_id,media_file_id) VALUES(1,1),(2,1)`,
		`UPDATE downloads SET title='Test.Show.S02E03E04.WEB' WHERE id='d'`,
	} {
		if _, e := f.db.Exec(query); e != nil {
			t.Fatal(e)
		}
	}
	enable(t, f, true, Track{"en", "forced"})
	if e := f.s.Search(ctx, 1); e != nil {
		t.Fatal(e)
	}
	process(t, f.s)
	if f.p.target.Kind != "series" || f.p.target.TMDbID != 123 || len(f.p.target.Episodes) != 2 || f.p.target.Episodes[0].TMDbID != 101 || f.p.target.MovieHash == "" {
		t.Fatal(f.p.target)
	}
	v, e := f.s.File(ctx, 1, true)
	if e != nil || v.Episodes != "S02E03–E04" {
		t.Fatal(v, e)
	}
	var choice Candidate
	for _, c := range v.Candidates {
		if c.Variant == "forced" {
			choice = c
		}
	}
	if !choice.Automatic {
		t.Fatal(choice)
	}
	if e = f.s.Select(ctx, choice.ID, true); e != nil {
		t.Fatal(e)
	}
	process(t, f.s)
	// Recurring checks following manual work must not reuse an expired selection.
	if _, e = f.db.Exec(`UPDATE subtitle_jobs SET next_run=0; UPDATE subtitle_candidates SET expires_at=0`); e != nil {
		t.Fatal(e)
	}
	process(t, f.s)
	v, e = f.s.File(ctx, 1, false)
	if e != nil || v.Job.State != "completed" || v.Job.Mode != "automatic" || f.p.downloads != 1 {
		t.Fatal(v, v.Job, e)
	}
}
func (p *fixtureProvider) Fetch(context.Context, Credentials, Candidate) (Download, error) {
	p.downloads++
	if p.onFetch != nil {
		p.onFetch()
	}
	content := p.content
	if content == nil {
		content = []byte(validSRT)
	}
	return Download{Content: content, Remaining: p.remaining, ResetAt: time.Now().Add(24 * time.Hour).Unix()}, p.fetchErr
}

type fixtureProbe struct{ streams []transfer.SubtitleStream }

func (p fixtureProbe) Probe(context.Context, *os.File) (transfer.Probe, error) {
	streams := p.streams
	if streams == nil {
		streams = []transfer.SubtitleStream{}
	}
	return transfer.Probe{Codec: "h264", Width: 1280, Height: 720, Duration: 90, Subtitles: streams}, nil
}

type fixture struct {
	db         *sql.DB
	s          *Service
	p          *fixtureProvider
	cfg        config.Config
	dir, video string
	body       []byte
}

func setup(t *testing.T) fixture {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	cfg := config.Config{ConfigDir: filepath.Join(dir, "config"), MediaDir: filepath.Join(dir, "media"), DownloadsDir: filepath.Join(dir, "downloads")}
	for _, p := range []string{cfg.MediaDir, cfg.DownloadsDir} {
		if e := os.MkdirAll(p, 0755); e != nil {
			t.Fatal(e)
		}
	}
	db, e := database.Open(ctx, cfg.ConfigDir)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	secrets, e := securestore.New(ctx, db, cfg.ConfigDir)
	if e != nil {
		t.Fatal(e)
	}
	p := &fixtureProvider{}
	for n, variant := range []string{"full", "signs", "forced"} {
		p.matches = append(p.matches, Candidate{FileID: int64(100 + n), SubtitleID: fmt.Sprint(n), Track: Track{Language: "en", Variant: variant}, Release: "Test.Movie.2020.1080p.WEB", FileName: "Test.en.srt", Identity: true, HashMatch: true, Trusted: true})
	}
	s := New(db, secrets, cfg, p)
	s.Prober = fixtureProbe{}
	key, password := "fixture-key", "fixture-password"
	if e = s.Configure(ctx, CredentialUpdate{APIKey: &key, Username: "fixture", Password: &password, UserAgent: "ReelScarey v0.7.0"}); e != nil {
		t.Fatal(e)
	}
	body := bytes.Repeat([]byte("Generated media fixture\n"), 8192)
	sum := sha256.Sum256(body)
	relative := "Movies/Test/Test.mkv"
	video := filepath.Join(cfg.MediaDir, filepath.FromSlash(relative))
	if e = os.MkdirAll(filepath.Dir(video), 0755); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(video, body, 0644); e != nil {
		t.Fatal(e)
	}
	release, e := secrets.Seal("release", []byte(`{}`))
	if e != nil {
		t.Fatal(e)
	}
	payload, e := secrets.Seal("download", []byte(`{}`))
	if e != nil {
		t.Fatal(e)
	}
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO media_items(id,kind,tmdb_id,title,year,monitored,status) VALUES(1,'movie',123,'Test Movie',2020,1,'available')`, nil},
		{`INSERT INTO release_candidates(id,movie_id,title,size_bytes,published_at,payload,expires_at) VALUES('r',1,'release',1,'',?,1)`, []any{release}},
		{`INSERT INTO downloads(id,movie_id,release_id,title,info_hash,payload,client_url,save_path,state,created_at,updated_at) VALUES('d',1,'r','Test.Movie.2020.1080p.WEB','fixturehash',?,'http://unused.invalid','/unused','completed',1,1)`, []any{payload}},
		{`INSERT INTO media_files(id,movie_id,download_id,relative_path,size_bytes,sha256,probe_json,imported_at) VALUES(1,1,'d',?,?,?,'{}',1)`, []any{relative, len(body), hex.EncodeToString(sum[:])}},
	} {
		if _, e = db.Exec(q.sql, q.args...); e != nil {
			t.Fatal(e)
		}
	}
	return fixture{db: db, s: s, p: p, cfg: cfg, dir: dir, video: video, body: body}
}
func enable(t *testing.T, f fixture, enabled bool, tracks ...Track) {
	t.Helper()
	p, e := f.s.Preferences(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	p.Enabled = enabled
	if tracks != nil {
		p.Tracks = tracks
	}
	if _, e = f.s.SavePreferences(context.Background(), p); e != nil {
		t.Fatal(e)
	}
}
func process(t *testing.T, s *Service) {
	t.Helper()
	ok, e := s.ProcessNext(context.Background())
	if e != nil || !ok {
		t.Fatal("subtitle job did not process", ok, e)
	}
}
func TestAutomaticSubtitlesAndNoDuplicateQuotaAfterRestart(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	page, err := f.s.List(ctx, 1)
	if err != nil || len(page.Items) != 1 || page.Items[0].Job != nil {
		t.Fatal("new imports must be visible while automatic work is paused", page, err)
	}
	if ok, e := f.s.ProcessNext(ctx); ok || e != nil {
		t.Fatal("paused workflow ran", ok, e)
	}
	enable(t, f, true)
	process(t, f.s)
	view, e := f.s.File(ctx, 1, true)
	if e != nil || view.Job.State != "completed" || len(view.Tracks) != 3 {
		t.Fatal(view, view.Job, e)
	}
	for _, suffix := range []string{"en.srt", "en.signs.srt", "en.forced.srt"} {
		b, e := os.ReadFile(strings.TrimSuffix(f.video, ".mkv") + "." + suffix)
		if e != nil || string(b) != validSRT {
			t.Fatal(suffix, e)
		}
	}
	video, e := os.ReadFile(f.video)
	if e != nil || !bytes.Equal(video, f.body) {
		t.Fatal("media changed", e)
	}
	if f.p.downloads != 3 || f.p.searches != 1 {
		t.Fatal(f.p.downloads, f.p.searches)
	}
	f.db.Close()
	db, e := database.Open(ctx, f.cfg.ConfigDir)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	secrets, e := securestore.New(ctx, db, f.cfg.ConfigDir)
	if e != nil {
		t.Fatal(e)
	}
	s := New(db, secrets, f.cfg, f.p)
	s.Prober = fixtureProbe{}
	s.now = func() time.Time { return time.Now().Add(7 * time.Hour) }
	process(t, s)
	if f.p.downloads != 3 || f.p.searches != 1 {
		t.Fatal("existing tracks consumed quota", f.p.downloads, f.p.searches)
	}
}
func TestManualReviewRequiresExplicitChoiceAndPermitsUncertainTiming(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	for i := range f.p.matches {
		f.p.matches[i].HashMatch = false
		f.p.matches[i].Release = "Different cut"
	}
	enable(t, f, false, Track{"en", "full"})
	if e := f.s.Search(ctx, 1); e != nil {
		t.Fatal(e)
	}
	process(t, f.s)
	v, e := f.s.File(ctx, 1, true)
	if e != nil || len(v.Candidates) != 3 || v.Job.State != "needs_review" || f.p.downloads != 0 {
		t.Fatal(v, e)
	}
	choice := v.Candidates[0]
	for _, c := range v.Candidates {
		if c.Variant == "full" {
			choice = c
		}
	}
	if choice.Automatic || !choice.CanDownload {
		t.Fatal(choice)
	}
	if e = f.s.Select(ctx, choice.ID, false); e == nil {
		t.Fatal("review was optional")
	}
	if e = f.s.Select(ctx, choice.ID, true); e != nil {
		t.Fatal(e)
	}
	process(t, f.s)
	if f.p.downloads != 1 {
		t.Fatal(f.p.downloads)
	}
	view, e := f.s.File(ctx, 1, false)
	if e != nil || view.Job.State != "completed" || len(view.Tracks) != 1 || view.Tracks[0].State != "available" {
		t.Fatal(view, view.Job, e)
	}
	if b, e := os.ReadFile(strings.TrimSuffix(f.video, ".mkv") + ".en.srt"); e != nil || string(b) != validSRT {
		t.Fatal("manual subtitle was not published", e)
	}
}
func TestExistingEmbeddedAndSidecarsDoNotSpendQuota(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	f.s.Prober = fixtureProbe{streams: []transfer.SubtitleStream{{Language: "eng"}, {Language: "eng", Forced: true}}}
	if e := os.WriteFile(strings.TrimSuffix(f.video, ".mkv")+".eng.signs.ass", []byte("[Script Info]\n[Events]\nDialogue: 0,0:00:01.00,0:00:03.00,Default,,0,0,0,,Generated signs\n"), 0644); e != nil {
		t.Fatal(e)
	}
	if e := f.s.Disconnect(ctx); e != nil {
		t.Fatal(e)
	}
	enable(t, f, true)
	process(t, f.s)
	view, e := f.s.File(ctx, 1, false)
	if e != nil || view.Job.State != "completed" || len(view.Tracks) != 3 || f.p.searches != 0 || f.p.downloads != 0 {
		t.Fatal(view, view.Job, e)
	}
}
func TestUnknownAndInvalidSidecarsArePreserved(t *testing.T) {
	for _, suffix := range []string{".srt", ".en.srt"} {
		t.Run(suffix, func(t *testing.T) {
			f := setup(t)
			enable(t, f, true, Track{"en", "full"})
			existing := strings.TrimSuffix(f.video, ".mkv") + suffix
			body := []byte("Do not replace this existing file")
			if e := os.WriteFile(existing, body, 0644); e != nil {
				t.Fatal(e)
			}
			process(t, f.s)
			after, e := os.ReadFile(existing)
			if e != nil || !bytes.Equal(after, body) || f.p.downloads != 0 {
				t.Fatal("existing subtitle changed or consumed quota", e, f.p.downloads)
			}
			v, e := f.s.File(context.Background(), 1, false)
			if e != nil {
				t.Fatal(e)
			}
			if v.Job.State != "needs_review" {
				t.Fatal(v.Job)
			}
		})
	}
}
func TestPauseAndConnectionChangesFenceAfterProviderFetch(t *testing.T) {
	for _, change := range []string{"pause", "disconnect", "monitor"} {
		t.Run(change, func(t *testing.T) {
			f := setup(t)
			ctx := context.Background()
			enable(t, f, true, Track{"en", "full"})
			f.p.onFetch = func() {
				switch change {
				case "pause":
					enable(t, f, false)
				case "disconnect":
					if e := f.s.Disconnect(ctx); e != nil {
						t.Fatal(e)
					}
				case "monitor":
					if _, e := f.db.Exec(`UPDATE media_items SET monitored=0 WHERE id=1`); e != nil {
						t.Fatal(e)
					}
				}
			}
			process(t, f.s)
			if _, e := os.Stat(strings.TrimSuffix(f.video, ".mkv") + ".en.srt"); !errors.Is(e, os.ErrNotExist) {
				t.Fatal("subtitle published after intent changed", e)
			}
			body, _ := os.ReadFile(f.video)
			if !bytes.Equal(body, f.body) {
				t.Fatal("video changed")
			}
		})
	}
}
func TestQuotaAndAuthenticationBlockFurtherProviderRequests(t *testing.T) {
	for _, code := range []string{"provider_quota", "provider_auth"} {
		t.Run(code, func(t *testing.T) {
			f := setup(t)
			ctx := context.Background()
			enable(t, f, true, Track{"en", "full"})
			f.p.fetchErr = &Error{Code: code, Message: "Fixture provider must wait", Temporary: code == "provider_quota", RetryAt: time.Now().Add(12 * time.Hour).Unix()}
			process(t, f.s)
			if _, e := f.db.Exec(`UPDATE subtitle_jobs SET next_run=0`); e != nil {
				t.Fatal(e)
			}
			process(t, f.s)
			if f.p.searches != 1 || f.p.downloads != 1 {
				t.Fatal("provider was repeatedly called during cooldown", f.p.searches, f.p.downloads)
			}
			status, e := f.s.Status(ctx)
			if e != nil || status.State != code {
				t.Fatal(status, e)
			}
		})
	}
}
func TestMatchingRejectsWrongIdentityAndRequiresHashForSharedEpisodeFiles(t *testing.T) {
	p := Preferences{Tracks: []Track{{"en", "full"}}, HearingImpaired: "any", MatchPolicy: "hash_or_release"}
	target := Target{Release: "A.2020.WEB", Episodes: []Episode{{TMDbID: 1}, {TMDbID: 2}}}
	r := Candidate{FileID: 1, Track: Track{"en", "full"}, Release: target.Release, Identity: true}
	out := rank(p, target, []Candidate{r})
	if out[0].Automatic || !out[0].CanDownload {
		t.Fatal(out)
	}
	r.HashMatch = true
	out = rank(p, target, []Candidate{r})
	if !out[0].Automatic {
		t.Fatal(out)
	}
	r.Identity = false
	out = rank(p, target, []Candidate{r})
	if out[0].CanDownload {
		t.Fatal("hash bypassed catalog identity")
	}
	r.Identity = true
	r.Translated = true
	if rank(p, target, []Candidate{r})[0].CanDownload {
		t.Fatal("translated subtitle accepted")
	}
}
func TestPreferencesRevisionsAndCredentialEncryptionSurviveReopen(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	p, e := f.s.Preferences(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if !slices.Contains(p.Tracks, Track{"en", "signs"}) || !slices.Contains(p.Tracks, Track{"en", "forced"}) {
		t.Fatal(p)
	}
	old := p
	p.Enabled = true
	if _, e = f.s.SavePreferences(ctx, p); e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.SavePreferences(ctx, old); e == nil {
		t.Fatal("stale preference accepted")
	}
	var ciphertext []byte
	if e = f.db.QueryRow(`SELECT ciphertext FROM integration_configs WHERE name='opensubtitles'`).Scan(&ciphertext); e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(ciphertext, []byte("fixture-password")) || bytes.Contains(ciphertext, []byte("fixture-key")) {
		t.Fatal("plaintext credentials")
	}
	status, e := f.s.Status(ctx)
	b, _ := json.Marshal(status)
	if e != nil || bytes.Contains(b, []byte("fixture-password")) || bytes.Contains(b, []byte("fixture-key")) {
		t.Fatal("credential status disclosure", e)
	}
	p.Revision++
	p.Tracks = []Track{{"en", "full"}, {"en", "full"}}
	if _, e = f.s.SavePreferences(ctx, p); e == nil {
		t.Fatal("duplicate tracks accepted")
	}
}
