package transfer

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/faaaaq/ReelScarey/internal/library"
	"github.com/faaaaq/ReelScarey/internal/quality"
)

// jsonUnmarshalProbe decodes a persisted probe_json column.
func jsonUnmarshalProbe(raw string, p *Probe) error {
	return json.Unmarshal([]byte(raw), p)
}

// ---------------------------------------------------------------------------
// Classifier unit tests (pure, run on every platform)
// ---------------------------------------------------------------------------

func TestNormalizeAudioLanguage(t *testing.T) {
	cases := []struct{ tag, want string }{
		{"eng", "en"}, {"en", "en"}, {"english", "en"}, {"EN", "en"},
		{" English ", "en"}, {"en-US", "en"}, {"en-us", "en"}, {"eng-GB", "en"},
		{"jpn", "jpn"}, {"ja", "ja"}, {"japanese", "japanese"},
		{"fre", "fre"}, {"fra", "fra"}, {"deu", "deu"}, {"ger", "ger"},
		{"spa", "spa"}, {"ita", "ita"}, {"kor", "kor"}, {"zho", "zho"},
		{"chi", "chi"}, {"rus", "rus"}, {"por", "por"},
		{"", ""}, {"   ", ""}, {"und", ""}, {"mis", ""}, {"zxx", ""},
		{"qaa", ""}, {"qaa-qtz", ""}, {"gibberish-tag", "gibberish-tag"},
	}
	for _, c := range cases {
		if got := normalizeAudioLanguage(c.tag); got != c.want {
			t.Fatalf("normalizeAudioLanguage(%q) = %q, want %q", c.tag, got, c.want)
		}
	}
}

func TestClassifyAudioStreams(t *testing.T) {
	eng := func(lang string) AudioStream { return AudioStream{Codec: "aac", Language: lang} }
	cases := []struct {
		name      string
		streams   []AudioStream
		state     string
		reason    string
		languages []string
	}{
		{"zero streams", nil, AudioUnverified, "no audio streams", nil},
		{"eng only", []AudioStream{eng("eng")}, AudioVerifiedEnglish, "", nil},
		{"en-us only", []AudioStream{eng("en-US")}, AudioVerifiedEnglish, "", nil},
		{"english word", []AudioStream{eng("English")}, AudioVerifiedEnglish, "", nil},
		{"jpn only", []AudioStream{eng("jpn")}, AudioVerifiedNoEnglish, "", []string{"jpn"}},
		{"ja only", []AudioStream{eng("ja")}, AudioVerifiedNoEnglish, "", []string{"ja"}},
		{"japanese word", []AudioStream{eng("Japanese")}, AudioVerifiedNoEnglish, "", []string{"japanese"}},
		{"eng plus jpn", []AudioStream{eng("eng"), eng("jpn")}, AudioVerifiedEnglish, "", []string{"jpn"}},
		{"jpn plus eng", []AudioStream{eng("jpn"), eng("eng"), eng("jpn")}, AudioVerifiedEnglish, "", []string{"jpn"}},
		{"jpn plus untagged", []AudioStream{eng("jpn"), eng("")}, AudioVerifiedNoEnglish, "", []string{"jpn"}},
		{"eng plus untagged", []AudioStream{eng("eng"), eng("")}, AudioVerifiedEnglish, "", nil},
		{"all undetermined", []AudioStream{eng("und"), eng("")}, AudioUnverified, "no language tags", nil},
		{"all empty", []AudioStream{eng(""), eng(" ")}, AudioUnverified, "no language tags", nil},
		{"three mixed tracks", []AudioStream{eng("eng"), eng("jpn"), eng("fre")}, AudioVerifiedEnglish, "", []string{"jpn", "fre"}},
		{"multiple non-english", []AudioStream{eng("kor"), eng("zho")}, AudioVerifiedNoEnglish, "", []string{"kor", "zho"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := classifyAudioStreams(c.streams)
			if got.State != c.state {
				t.Fatalf("state = %q, want %q (%+v)", got.State, c.state, got)
			}
			if got.Reason != c.reason {
				t.Fatalf("reason = %q, want %q", got.Reason, c.reason)
			}
			if strings.Join(got.Languages, ",") != strings.Join(c.languages, ",") {
				t.Fatalf("languages = %v, want %v", got.Languages, c.languages)
			}
		})
	}
	// Subtitle streams are never consulted: the classifier only ever receives
	// audio streams, and a nil audio list is UNVERIFIED even when the caller
	// has subtitle evidence.
	if got := classifyAudioStreams(nil); got.State != AudioUnverified {
		t.Fatal("subtitle evidence must not reach the classifier")
	}
}

// ---------------------------------------------------------------------------
// Orchestration tests (stub Prober, run on every platform)
// ---------------------------------------------------------------------------

// audioProbe is a stub Prober returning declared audio streams without
// invoking ffprobe.
type audioProbe struct {
	calls   int
	streams []AudioStream
	err     error
}

func (p *audioProbe) Probe(context.Context, *os.File) (Probe, error) {
	p.calls++
	if p.err != nil {
		return Probe{}, p.err
	}
	return Probe{Codec: "h264", Width: 1920, Height: 1080, Duration: 90, Audio: p.streams}, nil
}

// animeAudioIndexer returns an English-dub-labeled release so the strict
// show's queue-time gate accepts it; the downloaded file's actual audio is
// whatever the stub prober reports.
type animeAudioIndexer struct{ query string }

func (*animeAudioIndexer) Test(context.Context, IndexerConfig) error { return nil }
func (f *animeAudioIndexer) Search(_ context.Context, c IndexerConfig, q string) ([]Candidate, error) {
	f.query = q
	return []Candidate{{Release: Release{Title: "Test.Show.S01E02 [English Dub] [1080p]", Size: 50000, Published: "2026-10-22T10:00:00Z"}, Magnet: testMagnet, Generation: c.Generation}}, nil
}
func (f *animeAudioIndexer) Recent(ctx context.Context, c IndexerConfig) ([]Candidate, error) {
	return f.Search(ctx, c, "")
}
func (f *animeAudioIndexer) Fetch(_ context.Context, _ IndexerConfig, v Candidate) (Payload, string, error) {
	return Payload{Magnet: v.Magnet}, testHash, nil
}

// animeAudioStep advances the worker one claim/process cycle. The returned job
// error is deliberately ignored: job problems (including the expected
// anime_audio_mismatch block) are recorded on the download itself.
func animeAudioStep(t *testing.T, f *workflowFixture) {
	t.Helper()
	if _, e := f.db.Exec(`UPDATE downloads SET next_run=0`); e != nil {
		t.Fatal(e)
	}
	_, _ = f.s.ProcessNext(context.Background())
}

func animeAudioRun(t *testing.T, f *workflowFixture, j Job, want string) Job {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	current, e := f.s.Job(context.Background(), j.ID)
	if e != nil {
		t.Fatal(e)
	}
	for current.State != want {
		if time.Now().After(deadline) {
			t.Fatalf("download never reached %q: %+v", want, current)
		}
		animeAudioStep(t, f)
		time.Sleep(20 * time.Millisecond)
		current, e = f.s.Job(context.Background(), j.ID)
		if e != nil {
			t.Fatal(e)
		}
	}
	return current
}

// strictAnimeEpisode queues an English-dub-labeled release for a strict anime
// show, submits it and marks a single completed video file, leaving the
// automatic import one worker cycle away from planning.
func strictAnimeEpisode(t *testing.T, prober Prober) (*workflowFixture, int64, Job) {
	t.Helper()
	f, id, _ := setupTV(t)
	enableAnimeDubOnly(t, f, id)
	f.s = New(f.db, f.s.secrets, f.cfg, f.client, &animeAudioIndexer{})
	f.s.importer.Prober = prober
	ctx := context.Background()
	out, e := f.s.SearchTV(ctx, id, 1, 2, "")
	if e != nil || len(out) != 1 {
		t.Fatal(out, e)
	}
	j, created, e := f.s.Queue(ctx, out[0].ID)
	if e != nil || !created || j.Kind != "series" {
		t.Fatal("the strict show's English-dub release was not queued", j, e)
	}
	animeAudioStep(t, f)
	const data = "owned fixture video"
	f.source(t, j, "Test.Show.S01E02.mkv", []byte(data))
	f.client.torrent.Progress = 1
	f.client.torrent.AmountLeft = 0
	f.client.torrent.State = "uploading"
	f.client.files = []TorrentFile{{Index: 0, Name: "Test.Show.S01E02.mkv", Size: int64(len(data)), Progress: 1, Priority: 1}}
	return f, id, j
}

func animeAudioCounts(t *testing.T, f *workflowFixture, j Job) (importOps, media, episodes int) {
	t.Helper()
	if e := f.db.QueryRow(`SELECT COUNT(*) FROM import_operations WHERE download_id=?`, j.ID).Scan(&importOps); e != nil {
		t.Fatal(e)
	}
	if e := f.db.QueryRow(`SELECT COUNT(*) FROM media_files WHERE download_id=?`, j.ID).Scan(&media); e != nil {
		t.Fatal(e)
	}
	if e := f.db.QueryRow(`SELECT COUNT(*) FROM episode_files ef JOIN download_mappings m ON m.episode_id=ef.episode_id WHERE m.download_id=?`, j.ID).Scan(&episodes); e != nil {
		t.Fatal(e)
	}
	return
}

func TestAnimeAudioMismatchBlocksStrictImport(t *testing.T) {
	prober := &audioProbe{streams: []AudioStream{{Codec: "aac", Language: "jpn"}}}
	f, id, j := strictAnimeEpisode(t, prober)
	current := animeAudioRun(t, f, j, "blocked")
	if current.ErrorCode != "anime_audio_mismatch" {
		t.Fatal("unexpected block", current)
	}
	if !strings.Contains(current.ErrorMessage, "requires an English dub") || !strings.Contains(current.ErrorMessage, "Test.Show.S01E02.mkv") {
		t.Fatal("the block message is not actionable", current.ErrorMessage)
	}
	if prober.calls < 1 {
		t.Fatal("the source file was never probed")
	}
	importOps, media, episodes := animeAudioCounts(t, f, j)
	if importOps != 0 || media != 0 || episodes != 0 {
		t.Fatal("a mismatched download journaled or registered a file", importOps, media, episodes)
	}
	// The source stays in the download folder; nothing was ever deleted.
	data, e := os.ReadFile(filepath.Join(f.cfg.DownloadsDir, "ReelScarey", j.ID, "Test.Show.S01E02.mkv"))
	if e != nil || string(data) != "owned fixture video" {
		t.Fatal("the source file was not retained", e)
	}
	// The reservation was not satisfied: the episode stays wanted.
	season, e := library.New(f.db).TVSeason(context.Background(), id, 1)
	if e != nil || season.Episodes[1].Available {
		t.Fatal("the episode was satisfied by the blocked import", season, e)
	}
	// The rejected release is blacklisted so the next automatic search skips it.
	var blocked bool
	if e := f.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM release_blacklist WHERE reason LIKE 'anime_audio_mismatch%')`).Scan(&blocked); e != nil || !blocked {
		t.Fatal("the mismatched release was not blacklisted", blocked, e)
	}
	out, e := f.s.SearchTV(context.Background(), id, 1, 2, "")
	if e != nil || len(out) != 1 {
		t.Fatal(out, e)
	}
	if _, _, e := f.s.Queue(context.Background(), out[0].ID); e == nil || !strings.Contains(e.Error(), "blocked after an earlier failure") {
		t.Fatal("the blacklisted release was queueable again", e)
	}
}

func TestAnimeAudioMismatchRetryReblocks(t *testing.T) {
	prober := &audioProbe{streams: []AudioStream{{Codec: "aac", Language: "jpn"}}}
	f, _, j := strictAnimeEpisode(t, prober)
	blocked := animeAudioRun(t, f, j, "blocked")
	if e := f.s.Retry(context.Background(), j.ID); e != nil {
		t.Fatal(e)
	}
	reblocked := animeAudioRun(t, f, j, "blocked")
	if reblocked.ErrorCode != "anime_audio_mismatch" || reblocked.Updated < blocked.Updated {
		t.Fatal("retry did not re-verify and re-block", blocked, reblocked)
	}
	if prober.calls < 2 {
		t.Fatal("retry replayed a stored plan instead of re-probing", prober.calls)
	}
	if _, media, episodes := animeAudioCounts(t, f, j); media != 0 || episodes != 0 {
		t.Fatal("retry imported the mismatched file", media, episodes)
	}
}

func TestAnimeAudioVerifiedEnglishImports(t *testing.T) {
	prober := &audioProbe{streams: []AudioStream{{Codec: "aac", Language: "eng"}}}
	f, id, j := strictAnimeEpisode(t, prober)
	current := animeAudioRun(t, f, j, "completed")
	if current.ErrorCode != "" {
		t.Fatal("verified English audio was blocked", current)
	}
	importOps, media, episodes := animeAudioCounts(t, f, j)
	if importOps != 1 || media != 1 || episodes != 1 {
		t.Fatal("the verified import was not registered", importOps, media, episodes)
	}
	verify := func(raw string) {
		t.Helper()
		var p Probe
		if e := jsonUnmarshalProbe(raw, &p); e != nil {
			t.Fatal(e)
		}
		if p.AudioVerification == nil || p.AudioVerification.State != AudioVerifiedEnglish {
			t.Fatal("classification missing from probe JSON", raw)
		}
	}
	var opsJSON, fileJSON string
	if e := f.db.QueryRow(`SELECT probe_json FROM import_operations WHERE download_id=?`, j.ID).Scan(&opsJSON); e != nil {
		t.Fatal(e)
	}
	if e := f.db.QueryRow(`SELECT probe_json FROM media_files WHERE download_id=?`, j.ID).Scan(&fileJSON); e != nil {
		t.Fatal(e)
	}
	verify(opsJSON)
	verify(fileJSON)
	season, e := library.New(f.db).TVSeason(context.Background(), id, 1)
	if e != nil || !season.Episodes[1].Available {
		t.Fatal("the episode was not registered", season, e)
	}
}

func TestAnimeAudioUnverifiedImportsFailOpen(t *testing.T) {
	prober := &audioProbe{streams: []AudioStream{{Codec: "aac", Language: "und"}, {Codec: "aac", Language: ""}}}
	f, id, j := strictAnimeEpisode(t, prober)
	current := animeAudioRun(t, f, j, "completed")
	if current.ErrorCode != "" {
		t.Fatal("an untagged dub was blocked", current)
	}
	importOps, media, episodes := animeAudioCounts(t, f, j)
	if importOps != 1 || media != 1 || episodes != 1 {
		t.Fatal("the fail-open import was not registered", importOps, media, episodes)
	}
	verify := func(raw string) {
		t.Helper()
		var p Probe
		if e := jsonUnmarshalProbe(raw, &p); e != nil {
			t.Fatal(e)
		}
		if p.AudioVerification == nil || p.AudioVerification.State != AudioUnverified || p.AudioVerification.Reason != "no language tags" {
			t.Fatal("unverified classification missing from probe JSON", raw)
		}
	}
	var opsJSON, fileJSON string
	if e := f.db.QueryRow(`SELECT probe_json FROM import_operations WHERE download_id=?`, j.ID).Scan(&opsJSON); e != nil {
		t.Fatal(e)
	}
	if e := f.db.QueryRow(`SELECT probe_json FROM media_files WHERE download_id=?`, j.ID).Scan(&fileJSON); e != nil {
		t.Fatal(e)
	}
	verify(opsJSON)
	verify(fileJSON)
	var notes int
	if e := f.db.QueryRow(`SELECT COUNT(*) FROM activity_events WHERE kind='download' AND object_id=? AND state LIKE 'English audio could not be verified%'`, j.ID).Scan(&notes); e != nil || notes != 1 {
		t.Fatal("no honest unverified note was recorded", notes, e)
	}
	season, e := library.New(f.db).TVSeason(context.Background(), id, 1)
	if e != nil || !season.Episodes[1].Available {
		t.Fatal("the episode was not registered", season, e)
	}
}

func TestAnimeAudioManualOverrideImportsAfterDubRequirementDisabled(t *testing.T) {
	prober := &audioProbe{streams: []AudioStream{{Codec: "aac", Language: "jpn"}}}
	f, id, j := strictAnimeEpisode(t, prober)
	animeAudioRun(t, f, j, "blocked")
	// Owner override: turn the strict requirement off, then retry.
	ctx := context.Background()
	catalog := library.New(f.db)
	policy, e := catalog.AnimeSearch(ctx, id)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = catalog.SaveAnimeSearch(ctx, id, library.AnimeSearchUpdate{Revision: policy.Revision, CategoryRevision: policy.CategoryRevision, NumberingRevision: policy.NumberingRevision, Mode: policy.Mode, SearchTitle: policy.SearchTitle, AlternateTitles: policy.AlternateTitles, EnglishDubOnly: false}); e != nil {
		t.Fatal(e)
	}
	if e := f.s.Retry(ctx, j.ID); e != nil {
		t.Fatal(e)
	}
	current := animeAudioRun(t, f, j, "completed")
	if current.ErrorCode != "" {
		t.Fatal("the override retry did not import", current)
	}
	if _, media, episodes := animeAudioCounts(t, f, j); media != 1 || episodes != 1 {
		t.Fatal("the override import was not registered", media, episodes)
	}
}

func TestAnimeAudioOrdinaryTVImportsJapaneseAudio(t *testing.T) {
	prober := &audioProbe{streams: []AudioStream{{Codec: "aac", Language: "jpn"}}}
	f, id, _ := setupTV(t)
	f.s.importer.Prober = prober
	ctx := context.Background()
	out, e := f.s.SearchTV(ctx, id, 1, 2, "")
	if e != nil || len(out) != 1 {
		t.Fatal(out, e)
	}
	j, created, e := f.s.Queue(ctx, out[0].ID)
	if e != nil || !created || j.Kind != "series" {
		t.Fatal(j, e)
	}
	animeAudioStep(t, f)
	f.source(t, j, "Test.Show.S01E02.mkv", []byte("owned fixture video"))
	f.client.torrent.Progress = 1
	f.client.torrent.AmountLeft = 0
	f.client.torrent.State = "uploading"
	f.client.files = []TorrentFile{{Index: 0, Name: "Test.Show.S01E02.mkv", Size: int64(len("owned fixture video")), Progress: 1, Priority: 1}}
	current := animeAudioRun(t, f, j, "completed")
	if current.ErrorCode != "" {
		t.Fatal("ordinary TV was blocked by the anime audio check", current)
	}
	if _, media, episodes := animeAudioCounts(t, f, j); media != 1 || episodes != 1 {
		t.Fatal("the ordinary TV import was not registered", media, episodes)
	}
	var raw string
	if e := f.db.QueryRow(`SELECT probe_json FROM import_operations WHERE download_id=?`, j.ID).Scan(&raw); e != nil {
		t.Fatal(e)
	}
	// Without the strict flag the verification is never computed or recorded.
	if strings.Contains(raw, "audio_verification") {
		t.Fatal("a non-strict show's probe must not carry an audio verification", raw)
	}
}

func TestAnimeAudioMoviesIgnoreVerification(t *testing.T) {
	prober := &audioProbe{streams: []AudioStream{{Codec: "aac", Language: "jpn"}}}
	f := setupWorkflow(t)
	f.s.importer.Prober = prober
	j := f.queue(t)
	animeAudioStep(t, f)
	data := []byte(strings.Repeat("owned movie bytes ", 1000))
	f.complete(t, j, data)
	current := animeAudioRun(t, f, j, "completed")
	if current.ErrorCode != "" {
		t.Fatal("a movie was blocked by the anime audio check", current)
	}
	var n int
	if e := f.db.QueryRow(`SELECT COUNT(*) FROM media_files WHERE download_id=?`, j.ID).Scan(&n); e != nil || n != 1 {
		t.Fatal("the movie was not registered", n, e)
	}
	var raw string
	if e := f.db.QueryRow(`SELECT probe_json FROM media_files WHERE download_id=?`, j.ID).Scan(&raw); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(raw, "audio_verification") {
		t.Fatal("a movie probe must not carry an audio verification", raw)
	}
}

func TestAnimeAudioUnreadableMediaBlocksStrictImport(t *testing.T) {
	prober := &audioProbe{err: problem("invalid_media", "FFprobe could not verify a playable video. The source was left unchanged.")}
	f, _, j := strictAnimeEpisode(t, prober)
	current := animeAudioRun(t, f, j, "blocked")
	if current.ErrorCode != "invalid_media" {
		t.Fatal("unreadable media did not fail closed", current)
	}
	if _, media, _ := animeAudioCounts(t, f, j); media != 0 {
		t.Fatal("unreadable media was registered", media)
	}
}

// ---------------------------------------------------------------------------
// Real-prober fixture tests (Linux + FFmpeg-gated; skipped elsewhere,
// including this Windows development host — that is expected)
// ---------------------------------------------------------------------------

func skipAnimeAudioFixtures(t *testing.T) {
	t.Helper()
	skipOutsideLinuxRewrite(t)
	if _, e := exec.LookPath("ffmpeg"); e != nil {
		t.Skip("FFmpeg unavailable")
	}
	if _, e := exec.LookPath("ffprobe"); e != nil {
		t.Skip("FFprobe unavailable")
	}
}

func TestRealFFprobeAnimeAudioClassification(t *testing.T) {
	skipAnimeAudioFixtures(t)
	dir := t.TempDir()
	subtitle := filepath.Join(dir, "subtitle.srt")
	if e := os.WriteFile(subtitle, []byte("1\n00:00:00,000 --> 00:00:01,000\nHello\n"), 0600); e != nil {
		t.Fatal(e)
	}
	// Inputs come first; every option after them is an output option. FFmpeg
	// scopes per-file options to the NEXT file, so an input appended after
	// output options would consume the codecs and fail to open.
	inputs := []string{"-v", "error", "-f", "lavfi", "-i", "color=c=black:s=64x64:r=10", "-f", "lavfi", "-i", "sine=frequency=1000"}
	outputs := []string{"-t", "0.5", "-map", "0:v", "-map", "1:a", "-c:v", "mpeg4", "-c:a", "aac"}
	cases := []struct {
		name      string
		extraIn   []string
		extraOut  []string
		want      string
		languages []string
	}{
		{"english-only", nil, []string{"-metadata:s:a:0", "language=eng"}, AudioVerifiedEnglish, nil},
		{"dual-english-japanese", []string{"-f", "lavfi", "-i", "sine=frequency=2000"}, []string{"-map", "2:a", "-c:a:1", "aac", "-metadata:s:a:0", "language=eng", "-metadata:s:a:1", "language=jpn"}, AudioVerifiedEnglish, []string{"jpn"}},
		{"japanese-only", nil, []string{"-metadata:s:a:0", "language=jpn"}, AudioVerifiedNoEnglish, []string{"jpn"}},
		{"english-subs-japanese-audio", []string{"-i", subtitle}, []string{"-map", "2:s", "-c:s", "srt", "-metadata:s:a:0", "language=jpn", "-metadata:s:s:0", "language=eng"}, AudioVerifiedNoEnglish, []string{"jpn"}},
		{"untagged", nil, nil, AudioUnverified, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			name := filepath.Join(t.TempDir(), "audio.mkv")
			args := append(append(append(append([]string{}, inputs...), c.extraIn...), outputs...), c.extraOut...)
			args = append(args, name)
			cmd := exec.Command("ffmpeg", args...)
			hideProbeWindow(cmd)
			if out, e := cmd.CombinedOutput(); e != nil {
				t.Fatalf("generate: %v %s", e, out)
			}
			f, e := os.Open(name)
			if e != nil {
				t.Fatal(e)
			}
			defer f.Close()
			p, e := (FFprobe{}).Probe(context.Background(), f)
			if e != nil {
				t.Fatal(e)
			}
			if p.AudioVerification != nil {
				t.Fatal("the prober must not classify by itself")
			}
			got := classifyAudioStreams(p.Audio)
			if got.State != c.want {
				t.Fatalf("state = %q, want %q (%+v)", got.State, c.want, got)
			}
			if strings.Join(got.Languages, ",") != strings.Join(c.languages, ",") {
				t.Fatalf("languages = %v, want %v", got.Languages, c.languages)
			}
			// The English-subs case proves subtitle tags are never evidence of
			// spoken English audio.
			if c.name == "english-subs-japanese-audio" && got.State != AudioVerifiedNoEnglish {
				t.Fatal("subtitle language was consulted")
			}
		})
	}
}

// TestAnimeAudioDefaultDubRuleStaysStructural keeps the structural rule honest:
// the default dub rule accepts the explicit "[English Dub]" label the fixture
// uses, so orchestration failures are never caused by the rule text.
func TestAnimeAudioDefaultDubRuleStaysStructural(t *testing.T) {
	rule, e := quality.ParseDubRule(quality.DefaultDubRule)
	if e != nil {
		t.Fatal(e)
	}
	if !rule.Qualifies("Test.Show.S01E02 [English Dub] [1080p]") {
		t.Fatal("the fixture's English-dub label no longer qualifies under the default rule")
	}
	if rule.Qualifies("Test.Show.S01E02 [1080p]") {
		t.Fatal("a bare release title must not qualify")
	}
}
