package organization

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/config"
	"github.com/faaaaq/ReelScarey/internal/database"
)

type reviewFixture struct {
	db  *sql.DB
	cfg config.Config
	s   *Service
}

func newReviewFixture(t *testing.T) reviewFixture {
	t.Helper()
	root := t.TempDir()
	cfg := config.Config{ConfigDir: filepath.Join(root, "config"), MediaDir: filepath.Join(root, "media"), DownloadsDir: filepath.Join(root, "downloads")}
	for _, dir := range []string{cfg.MediaDir, cfg.DownloadsDir} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	db, err := database.Open(context.Background(), cfg.ConfigDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return reviewFixture{db: db, cfg: cfg, s: New(db, cfg)}
}

func reviewClone(t *testing.T, in Config) Config {
	t.Helper()
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out Config
	if err = json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func (f reviewFixture) movie(t *testing.T, id int, rel string, body []byte) {
	t.Helper()
	if _, err := f.db.Exec(`INSERT INTO media_items(id,kind,tmdb_id,title,year,status) VALUES(?,'movie',?,'Review movie',2020,'available')`, id, id); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(f.cfg.MediaDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, body, 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	if _, err := f.db.Exec(`INSERT INTO media_files(id,movie_id,relative_path,size_bytes,sha256,probe_json,imported_at,origin,source_name) VALUES(?,?,?,?,?,'{}',1,'existing','review-original.mkv')`, id, id, rel, len(body), hex.EncodeToString(sum[:])); err != nil {
		t.Fatal(err)
	}
}

func TestWholeTitleMoveRejectsChangedFileSet(t *testing.T) {
	ctx := context.Background()
	f := newReviewFixture(t)
	f.movie(t, 1, "Movies/Original/movie.mkv", []byte("original video"))
	plan, err := f.s.PreviewTitle(ctx, 1, "anime")
	if err != nil || plan.EntireMediaID != 1 || len(plan.Files) != 1 {
		t.Fatalf("whole-title review: %+v, %v", plan, err)
	}
	if _, err = f.db.Exec(`DELETE FROM media_files WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.Apply(ctx, plan.ID); err == nil {
		t.Fatal("a review must become stale when a file is unregistered")
	}
	if _, err = os.Stat(filepath.Join(f.cfg.MediaDir, "Movies", "Original", "movie.mkv")); err != nil {
		t.Fatal("stale review changed the original", err)
	}
	if _, err = f.s.PreviewTitle(ctx, 1, ""); err == nil {
		t.Fatal("missing destination accepted")
	}
}

func TestReviewOrganizationRejectsUnsafeOrOverlappingFoldersAtomically(t *testing.T) {
	ctx := context.Background()
	f := newReviewFixture(t)
	initial, err := f.s.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"../outside", "Movies/../outside", "/outside", `C:/outside`, `Movies\\outside`, "Movies//other", "Movies/CON", "Movies/COM1.mkv", "Movies/trailing.", "Movies/ trailing", "Movies/\nother", "Movies/subfolder", "movies"} {
		t.Run(bad, func(t *testing.T) {
			in := reviewClone(t, initial)
			in.Libraries[1].Folder = bad
			if _, err = f.s.Update(ctx, in); err == nil {
				t.Fatal("unsafe or overlapping folder accepted", bad)
			}
			got, err := f.s.Get(ctx)
			if err != nil || got.Revision != initial.Revision || got.Libraries[1].Folder != initial.Libraries[1].Folder {
				t.Fatal("rejected settings changed saved configuration", got, err)
			}
		})
	}
	entries, err := os.ReadDir(f.cfg.MediaDir)
	if err != nil || len(entries) != 0 {
		t.Fatal("settings validation created media folders", entries, err)
	}
}

func TestReviewLibraryAssignmentsPreserveFilesAndRejectStaleEdits(t *testing.T) {
	ctx := context.Background()
	f := newReviewFixture(t)
	body := []byte("registered media must remain in place until a separate reviewed reorganization")
	rel := "Movies/Original/Review movie.mkv"
	f.movie(t, 1, rel, body)
	c, err := f.s.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	c.Libraries = append(c.Libraries, Library{Key: "family", Name: "Family", Folder: "Family", Content: "mixed", Preset: "legacy"})
	c, err = f.s.Update(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := f.s.GetAssignment(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	assigned, err := f.s.PutAssignment(ctx, 1, "family", initial.Revision)
	if err != nil || assigned.LibraryKey != "family" {
		t.Fatal("compatible assignment failed", assigned, err)
	}
	if _, err = f.s.PutAssignment(ctx, 1, "movies", initial.Revision); err == nil {
		t.Fatal("stale assignment overwrote newer choice")
	}
	if _, err = f.s.PutAssignment(ctx, 1, "tv", assigned.Revision); err == nil {
		t.Fatal("movie accepted in series-only library")
	}
	var actual string
	if err = f.db.QueryRow(`SELECT relative_path FROM media_files WHERE id=1`).Scan(&actual); err != nil || actual != rel {
		t.Fatal("assignment changed file registration", actual, err)
	}
	got, err := os.ReadFile(filepath.Join(f.cfg.MediaDir, filepath.FromSlash(rel)))
	if err != nil || string(got) != string(body) {
		t.Fatal("assignment changed media bytes", err)
	}
	c.Libraries = c.Libraries[:3]
	if _, err = f.s.Update(ctx, c); err == nil {
		t.Fatal("assigned library was removed")
	}
}

func TestReviewConfigurationRevisionAndServerRootAreAuthoritative(t *testing.T) {
	ctx := context.Background()
	f := newReviewFixture(t)
	initial, err := f.s.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	in := reviewClone(t, initial)
	in.MainRoot = filepath.Join(t.TempDir(), "unconfigured-root")
	in.Libraries[0].Name = "Film collection"
	got, err := f.s.Update(ctx, in)
	if err != nil || got.MainRoot != f.cfg.MediaDir || got.Revision != initial.Revision+1 {
		t.Fatal("update changed configured root or lost revision fence", got, err)
	}
	if _, err = f.s.Update(ctx, initial); err == nil {
		t.Fatal("stale settings overwrote newer configuration")
	}
}

func TestReviewAbsoluteTemplatesRequireExplicitConsecutiveCanonicalMappings(t *testing.T) {
	l := Library{Key: "anime", Name: "Anime", Folder: "Anime", Content: "mixed", Preset: "custom", MovieTemplate: "{title}/{title}", SeriesTemplate: "{title}", EpisodeTemplate: "{title} - {absolute:03}{absolute_range}"}
	snapshot := Snapshot{Library: l, Content: Content{Kind: "series", Category: "anime", Title: "Review show", TMDbID: 10}}
	abs := func(n int) *int { return &n }
	for _, episodes := range [][]Episode{
		{{ID: 1, Season: 1, Number: 1}},
		{{ID: 1, Season: 1, Number: 1, Absolute: abs(25)}, {ID: 2, Season: 1, Number: 2}},
		{{ID: 1, Season: 1, Number: 1, Absolute: abs(25)}, {ID: 2, Season: 1, Number: 2, Absolute: abs(27)}},
		{{ID: 1, Season: 1, Number: 1, Absolute: abs(25)}, {ID: 2, Season: 1, Number: 3, Absolute: abs(26)}},
		{{ID: 1, Season: 0, Number: 1, Absolute: abs(25)}, {ID: 2, Season: 1, Number: 2, Absolute: abs(26)}},
	} {
		if path, err := Destination(snapshot, episodes, "original.mkv"); err == nil {
			t.Fatal("unsupported mapping guessed an organization path", episodes, path)
		}
	}
	got, err := Destination(snapshot, []Episode{{ID: 1, Season: 1, Number: 1, Absolute: abs(25)}, {ID: 2, Season: 1, Number: 2, Absolute: abs(26)}}, "original.MKV")
	if err != nil || got != "Anime/TV Shows/Review show/Review show - 025-026.mkv" {
		t.Fatal("explicit anime range not retained", got, err)
	}
}

func TestReviewCustomTemplatesCannotEscapeOrOmitEpisodeIdentity(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	initial, err := f.s.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ movie, show, episode string }{
		{"../{title}", "{title}", "{episode}"},
		{"{title}", "/{title}", "{episode}"},
		{"{title}", "{title}", "{episode_title}"},
		{"{title}/{episode}", "{title}", "{episode}"},
		{"{title}", "{title}", "{unknown}"},
		{"{title}", "{title}", "{episode}/../../outside"},
		{"{title}", "{title}", "CON/{episode}"},
	} {
		in := reviewClone(t, initial)
		in.Libraries[2].Preset = "custom"
		in.Libraries[2].MovieTemplate, in.Libraries[2].SeriesTemplate, in.Libraries[2].EpisodeTemplate = tc.movie, tc.show, tc.episode
		if _, err := f.s.Update(ctx, in); err == nil {
			t.Fatal("unsafe or ambiguous custom template accepted", tc)
		}
	}
}

func TestReviewCompanionAddedAfterPreviewRejectsWholeBatch(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	source := "Movies/Original/film.mkv"
	body := []byte("an unchanged registered movie")
	f.movie(t, 1, source, body)
	plan, err := f.s.Preview(ctx, []int64{1}, "")
	if err != nil || len(plan.Files) != 1 {
		t.Fatal(plan, err)
	}
	sidecar := filepath.Join(f.cfg.MediaDir, "Movies", "Original", "film.eng.signs.ass")
	if err = os.WriteFile(sidecar, []byte("[Events]\nDialogue: generated review subtitle"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.Apply(ctx, plan.ID); err == nil {
		t.Fatal("new companion was silently excluded from reviewed batch")
	}
	got, err := os.ReadFile(filepath.Join(f.cfg.MediaDir, filepath.FromSlash(source)))
	if err != nil || string(got) != string(body) {
		t.Fatal("rejected stale review changed source bytes", err)
	}
	if _, err = os.Stat(filepath.Join(f.cfg.MediaDir, filepath.FromSlash(plan.Files[0].Destination))); !os.IsNotExist(err) {
		t.Fatal("stale review published an output", err)
	}
}

func TestReviewQueuedPlanCannotUseChangedNamingRules(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	source := "Movies/Original/film.mkv"
	f.movie(t, 1, source, []byte("registered movie must stay put when its queued rules change"))
	plan, err := f.s.Preview(ctx, []int64{1}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.Apply(ctx, plan.ID); err != nil {
		t.Fatal(err)
	}
	c, err := f.s.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	c.Libraries[0].Folder = "Renamed Movies"
	if _, err = f.s.Update(ctx, c); err != nil {
		t.Fatal(err)
	}
	if err = f.s.Tick(ctx); err == nil {
		t.Fatal("queued plan used changed rules without a fresh review")
	}
	saved, err := f.s.Plan(ctx, plan.ID)
	if err != nil || saved.State != "failed" || !strings.Contains(saved.Message, "changed") {
		t.Fatal("stale queued plan not left for review", saved, err)
	}
	if _, err = os.Stat(filepath.Join(f.cfg.MediaDir, filepath.FromSlash(source))); err != nil {
		t.Fatal("queued-plan conflict removed source", err)
	}
	if _, err = os.Stat(filepath.Join(f.cfg.MediaDir, filepath.FromSlash(plan.Files[0].Destination))); !os.IsNotExist(err) {
		t.Fatal("queued-plan conflict published old destination", err)
	}
}

func TestReviewReorganizationPreservesHardlinkedDownloadAndSubtitleReferences(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	source := "Movies/Original/film.mkv"
	body := []byte("registered movie shared with an independently seeding download source")
	f.movie(t, 1, source, body)
	old := filepath.Join(f.cfg.MediaDir, filepath.FromSlash(source))
	seeding := filepath.Join(f.cfg.DownloadsDir, "independent-seeding-source.mkv")
	if err := os.Link(old, seeding); err != nil {
		t.Skip("hardlinks unsupported on disposable test filesystem", err)
	}
	sidecarSource := "Movies/Original/film.eng.signs.ass"
	sidecarBytes := []byte("[Events]\nDialogue: original styled signs subtitle")
	if err := os.WriteFile(filepath.Join(f.cfg.MediaDir, filepath.FromSlash(sidecarSource)), sidecarBytes, 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(sidecarBytes)
	if _, err := f.db.Exec(`INSERT INTO subtitles(media_file_id,language,variant,source,relative_path,sha256,provider_file_id,updated_at) VALUES(1,'en','signs','downloaded',?,?,99,1)`, sidecarSource, hex.EncodeToString(sum[:])); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`INSERT INTO subtitle_imports(id,media_file_id,language,variant,provider_file_id,destination,stage_name,sha256,size_bytes,phase,updated_at) VALUES('review-sub',1,'en','signs',99,?,'old-committed-stage',?,?,'committed',1)`, sidecarSource, hex.EncodeToString(sum[:]), len(sidecarBytes)); err != nil {
		t.Fatal(err)
	}
	plan, err := f.s.PreviewTitle(ctx, 1, "anime")
	if err != nil || len(plan.Files) != 1 || len(plan.Files[0].Companions) != 1 {
		t.Fatal("review omitted subtitle companion", plan, err)
	}
	if _, err = os.Stat(old); err != nil {
		t.Fatal("preview moved source", err)
	}
	if _, err = f.s.Apply(ctx, plan.ID); err != nil {
		t.Fatal(err)
	}
	if err = f.s.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	finished, err := f.s.Plan(ctx, plan.ID)
	if err != nil || finished.State != "completed" {
		t.Fatal("batch not completed", finished, err)
	}
	var actualPath, subtitlePath, journalPath string
	if err = f.db.QueryRow(`SELECT relative_path FROM media_files WHERE id=1`).Scan(&actualPath); err != nil || actualPath != plan.Files[0].Destination {
		t.Fatal("file registration not switched", actualPath, err)
	}
	if err = f.db.QueryRow(`SELECT relative_path FROM subtitles WHERE media_file_id=1`).Scan(&subtitlePath); err != nil || subtitlePath != plan.Files[0].Companions[0].Destination {
		t.Fatal("subtitle registration left behind", subtitlePath, err)
	}
	if err = f.db.QueryRow(`SELECT destination FROM subtitle_imports WHERE id='review-sub'`).Scan(&journalPath); err != nil || journalPath != subtitlePath {
		t.Fatal("committed subtitle journal left behind", journalPath, err)
	}
	for _, tc := range []struct {
		path string
		body []byte
	}{{filepath.Join(f.cfg.MediaDir, filepath.FromSlash(actualPath)), body}, {seeding, body}, {filepath.Join(f.cfg.MediaDir, filepath.FromSlash(subtitlePath)), sidecarBytes}} {
		got, err := os.ReadFile(tc.path)
		if err != nil || string(got) != string(tc.body) {
			t.Fatal("reorganization changed or removed preserved bytes", tc.path, err)
		}
	}
	if _, err = os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("verified old library link was not cleaned up", err)
	}
	if _, err = os.Stat(filepath.Join(f.cfg.MediaDir, filepath.FromSlash(sidecarSource))); !os.IsNotExist(err) {
		t.Fatal("old subtitle link was not cleaned up", err)
	}
	var origin, sourceName string
	var downloadID sql.NullString
	if err = f.db.QueryRow(`SELECT origin,source_name,download_id FROM media_files WHERE id=1`).Scan(&origin, &sourceName, &downloadID); err != nil || origin != "existing" || sourceName != "review-original.mkv" || downloadID.Valid {
		t.Fatal("reorganization fabricated provenance/history", origin, sourceName, downloadID, err)
	}
	// A repeat tick/new service has nothing to repeat and retains the IDs/history.
	if err = New(f.db, f.cfg).Tick(ctx); err != nil {
		t.Fatal("completed batch not idempotent after service recreation", err)
	}
}

func TestReorganizationUpdatesYouTubeVideoAndSubtitleProvenance(t *testing.T) {
	ctx := context.Background()
	f := newReviewFixture(t)
	source := "Movies/Original/video.mkv"
	body := []byte("owned video")
	f.movie(t, 1, source, body)
	subSource := "Movies/Original/video.en.vtt"
	subBytes := []byte("WEBVTT\n\n00:00:00.000 --> 00:00:01.000\nOwned\n")
	if err := os.WriteFile(filepath.Join(f.cfg.MediaDir, filepath.FromSlash(subSource)), subBytes, 0600); err != nil {
		t.Fatal(err)
	}
	videoHash := sha256.Sum256(body)
	subHash := sha256.Sum256(subBytes)
	videoSHA := hex.EncodeToString(videoHash[:])
	subSHA := hex.EncodeToString(subHash[:])
	queries := []struct {
		q    string
		args []any
	}{
		{`INSERT INTO youtube_reviews(id,video_id,video_title,channel,target_kind,media_id,target_snapshot,expires_at) VALUES('r','abcdefghijk','Video','','movie',1,'{}',9999999999)`, nil},
		{`INSERT INTO youtube_jobs(id,review_id,video_id,video_title,channel,target_kind,media_id,target_snapshot,state,created_at,updated_at) VALUES('j','r','abcdefghijk','Video','','movie',1,'{}','completed',1,1)`, nil},
		{`INSERT INTO youtube_files(job_id,media_file_id,video_id,title,relative_path,size_bytes,sha256,probe_json,imported_at) VALUES('j',1,'abcdefghijk','Video',?,?,?,'{}',1)`, []any{source, len(body), videoSHA}},
		{`INSERT INTO youtube_imports(job_id,source_path,destination_path,stage_name,size_bytes,source_mtime,sha256,probe_json,phase,updated_at) VALUES('j','source',?,'stage',?,1,?,'{}','committed',1)`, []any{source, len(body), videoSHA}},
		{`INSERT INTO subtitles(media_file_id,language,variant,source,relative_path,sha256,updated_at) VALUES(1,'en','full','downloaded',?,?,1)`, []any{subSource, subSHA}},
		{`INSERT INTO youtube_subtitle_files(job_id,language,relative_path,sha256,size_bytes) VALUES('j','en',?,?,?)`, []any{subSource, subSHA, len(subBytes)}},
		{`INSERT INTO youtube_subtitle_imports(job_id,language,source_path,destination_path,stage_name,size_bytes,source_mtime,sha256,phase) VALUES('j','en','source',?,'stage',?,1,?,'committed')`, []any{subSource, len(subBytes), subSHA}},
	}
	for _, v := range queries {
		if _, err := f.db.Exec(v.q, v.args...); err != nil {
			t.Fatal(err)
		}
	}
	plan, err := f.s.Preview(ctx, []int64{1}, "anime")
	if err != nil || len(plan.Files) != 1 || len(plan.Files[0].Companions) != 1 {
		t.Fatalf("preview: %+v %v", plan, err)
	}
	if _, err = f.s.Apply(ctx, plan.ID); err != nil {
		t.Fatal(err)
	}
	if err = f.s.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	var videoPath, videoJournal, subPath, subJournal string
	if err = f.db.QueryRow(`SELECT relative_path FROM youtube_files WHERE job_id='j'`).Scan(&videoPath); err != nil {
		t.Fatal(err)
	}
	if err = f.db.QueryRow(`SELECT destination_path FROM youtube_imports WHERE job_id='j'`).Scan(&videoJournal); err != nil {
		t.Fatal(err)
	}
	if err = f.db.QueryRow(`SELECT relative_path FROM youtube_subtitle_files WHERE job_id='j' AND language='en'`).Scan(&subPath); err != nil {
		t.Fatal(err)
	}
	if err = f.db.QueryRow(`SELECT destination_path FROM youtube_subtitle_imports WHERE job_id='j' AND language='en'`).Scan(&subJournal); err != nil {
		t.Fatal(err)
	}
	if videoPath != plan.Files[0].Destination || videoJournal != videoPath || subPath != plan.Files[0].Companions[0].Destination || subJournal != subPath {
		t.Fatalf("provenance mismatch: %q %q %q %q", videoPath, videoJournal, subPath, subJournal)
	}
}

func TestReviewInterruptedPublishedBatchRecoversWithoutAdoptingForeignOutput(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	source := "Movies/Original/film.mkv"
	body := []byte("known registered video for interrupted publication review")
	f.movie(t, 1, source, body)
	plan, err := f.s.Preview(ctx, []int64{1}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.Apply(ctx, plan.ID); err != nil {
		t.Fatal(err)
	}
	root, _, err := f.s.openRoot(plan.Root)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	entry := allFiles(plan)[0]
	if err = f.s.publish(ctx, root, plan, entry, 0); err != nil {
		t.Fatal(err)
	}
	if err = f.s.savePhase(ctx, &plan, "published"); err != nil {
		t.Fatal(err)
	}
	// Simulate a crash after publication followed by a foreign replacement with
	// identical bytes. Hash equality alone does not prove ownership of that path.
	dest := filepath.Join(f.cfg.MediaDir, filepath.FromSlash(entry.Destination))
	stage := filepath.Join(f.cfg.MediaDir, filepath.FromSlash(path.Join(path.Dir(entry.Destination), ".reelscarey-organize-"+plan.ID+"-0.part")))
	if err = os.Remove(dest); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(stage); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(dest, body, 0600); err != nil {
		t.Fatal(err)
	}
	if err = New(f.db, f.cfg).Tick(ctx); err == nil {
		t.Fatal("recovery adopted a foreign output with equal bytes")
	}
	var registered string
	if err = f.db.QueryRow(`SELECT relative_path FROM media_files WHERE id=1`).Scan(&registered); err != nil || registered != source {
		t.Fatal("foreign output changed canonical registration", registered, err)
	}
	got, err := os.ReadFile(filepath.Join(f.cfg.MediaDir, filepath.FromSlash(source)))
	if err != nil || string(got) != string(body) {
		t.Fatal("foreign-output recovery removed canonical source", err)
	}
}

func TestReviewSameDirectoryRenameDoesNotMistakeOwnOutputForCompanion(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	source := "Movies/Original/film.mkv"
	f.movie(t, 1, source, []byte("same-directory rename must not conflict with its own output"))
	if _, err := f.db.Exec(`UPDATE media_items SET title='film' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	c, err := f.s.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	c.Libraries[0].Preset = "custom"
	c.Libraries[0].MovieTemplate = "Original/{title}.new"
	c.Libraries[0].SeriesTemplate = "{title}"
	c.Libraries[0].EpisodeTemplate = "{episode}"
	if _, err = f.s.Update(ctx, c); err != nil {
		t.Fatal(err)
	}
	plan, err := f.s.Preview(ctx, []int64{1}, "")
	if err != nil || plan.Files[0].Destination != "Movies/Original/film.new.mkv" {
		t.Fatal(plan, err)
	}
	if _, err = f.s.Apply(ctx, plan.ID); err != nil {
		t.Fatal(err)
	}
	if err = f.s.Tick(ctx); err != nil {
		t.Fatal("own newly published video was treated as an unrelated companion", err)
	}
	finished, err := f.s.Plan(ctx, plan.ID)
	if err != nil || finished.State != "completed" {
		t.Fatal(finished, err)
	}
}

func TestReviewPublishedBatchRecoversAndCleansOnlyJournaledLinks(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	source := "Movies/Original/film.mkv"
	body := []byte("recoverable known video")
	f.movie(t, 1, source, body)
	plan, err := f.s.Preview(ctx, []int64{1}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.Apply(ctx, plan.ID); err != nil {
		t.Fatal(err)
	}
	root, _, err := f.s.openRoot(plan.Root)
	if err != nil {
		t.Fatal(err)
	}
	entry := allFiles(plan)[0]
	if err = f.s.publish(ctx, root, plan, entry, 0); err != nil {
		root.Close()
		t.Fatal(err)
	}
	root.Close()
	if err = f.s.savePhase(ctx, &plan, "published"); err != nil {
		t.Fatal(err)
	}
	if err = EnsureMediaAvailable(ctx, f.db); err == nil {
		t.Fatal("pending durable publication allowed ordinary media workers")
	}
	if err = New(f.db, f.cfg).Tick(ctx); err != nil {
		t.Fatal("published batch did not reconcile after restart", err)
	}
	if err = EnsureMediaAvailable(ctx, f.db); err != nil {
		t.Fatal("completed recovery did not release ordinary media work", err)
	}
	var dest string
	if err = f.db.QueryRow(`SELECT relative_path FROM media_files WHERE id=1`).Scan(&dest); err != nil || dest != entry.Destination {
		t.Fatal("recovered registration not switched exactly once", dest, err)
	}
	got, err := os.ReadFile(filepath.Join(f.cfg.MediaDir, filepath.FromSlash(dest)))
	if err != nil || string(got) != string(body) {
		t.Fatal("recovery lost verified bytes", err)
	}
	if _, err = os.Stat(filepath.Join(f.cfg.MediaDir, filepath.FromSlash(source))); !os.IsNotExist(err) {
		t.Fatal("recovery left old canonical link", err)
	}
}

func TestReviewJournaledPartialCopyRecoversAfterInterruption(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	source := "Movies/Original/film.mkv"
	body := []byte(strings.Repeat("known owned copy bytes ", 5000))
	f.movie(t, 1, source, body)
	plan, err := f.s.Preview(ctx, []int64{1}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.Apply(ctx, plan.ID); err != nil {
		t.Fatal(err)
	}
	entry := allFiles(plan)[0]
	stage := path.Join(path.Dir(entry.Destination), ".reelscarey-organize-"+plan.ID+"-0.part")
	absoluteStage := filepath.Join(f.cfg.MediaDir, filepath.FromSlash(stage))
	if err = os.MkdirAll(filepath.Dir(absoluteStage), 0700); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(absoluteStage, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.Write(body[:len(body)/3]); err != nil {
		file.Close()
		t.Fatal(err)
	}
	stageIdentity, err := identity(file)
	file.Close()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(`INSERT INTO organization_outputs(plan_id,destination,stage,identity,phase) VALUES(?,?,?,?,'copying')`, plan.ID, entry.Destination, stage, stageIdentity); err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(`UPDATE organization_plans SET state='running' WHERE id=?`, plan.ID); err != nil {
		t.Fatal(err)
	}
	if err = New(f.db, f.cfg).Tick(ctx); err != nil {
		t.Fatal("owned partial copying stage did not resume", err)
	}
	got, err := os.ReadFile(filepath.Join(f.cfg.MediaDir, filepath.FromSlash(entry.Destination)))
	if err != nil || !bytes.Equal(got, body) {
		t.Fatal("recovered copy differs from registered media", err)
	}
	if _, err = os.Stat(absoluteStage); !os.IsNotExist(err) {
		t.Fatal("completed partial-copy stage not cleaned", err)
	}
}

func TestReviewCancelledProcessingRetainsQueuedPlanForRestart(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	source := "Movies/Original/film.mkv"
	body := []byte("a cancelled worker cannot remove the reviewed source")
	f.movie(t, 1, source, body)
	plan, err := f.s.Preview(ctx, []int64{1}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.Apply(ctx, plan.ID); err != nil {
		t.Fatal(err)
	}
	stopped, cancel := context.WithCancel(ctx)
	cancel()
	if err = f.s.Tick(stopped); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled worker ignored cancellation", err)
	}
	got, err := os.ReadFile(filepath.Join(f.cfg.MediaDir, filepath.FromSlash(source)))
	if err != nil || !bytes.Equal(got, body) {
		t.Fatal("cancelled worker changed source", err)
	}
	if err = New(f.db, f.cfg).Tick(ctx); err != nil {
		t.Fatal("fresh worker did not resume persisted queued plan", err)
	}
	finished, err := f.s.Plan(ctx, plan.ID)
	if err != nil || finished.State != "completed" {
		t.Fatal("cancellation lost durable intent", finished, err)
	}
}
