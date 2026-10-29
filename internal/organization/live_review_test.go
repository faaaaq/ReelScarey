package organization_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/faaaaq/ReelScarey/internal/config"
	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/library"
	"github.com/faaaaq/ReelScarey/internal/metadata"
	"github.com/faaaaq/ReelScarey/internal/organization"
	"github.com/faaaaq/ReelScarey/internal/transfer"
)

// This opt-in acceptance uses generated owned media, the real existing-library
// verifier and actual FFprobe. No account, download client or user file is used.
func TestRealOrganizationGeneratedMedia(t *testing.T) {
	if os.Getenv("REELSCAREY_LIVE_ORGANIZATION") != "1" {
		t.Skip("set REELSCAREY_LIVE_ORGANIZATION=1 for generated FFmpeg/FFprobe organization acceptance")
	}
	for _, tool := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatal("required generated-media tool missing", tool, err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	root := t.TempDir()
	cfg := config.Config{ConfigDir: filepath.Join(root, "config"), MediaDir: filepath.Join(root, "media"), DownloadsDir: filepath.Join(root, "downloads")}
	relative := "Anime/TV Shows/Owned generated series/Owned.original.mkv"
	video := filepath.Join(cfg.MediaDir, filepath.FromSlash(relative))
	for _, dir := range []string{filepath.Dir(video), cfg.DownloadsDir} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc2=size=320x180:rate=10", "-t", "4", "-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p", video)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatal("generate owned media", err, string(output))
	}
	original, err := os.ReadFile(video)
	if err != nil {
		t.Fatal(err)
	}
	sourceLink := filepath.Join(cfg.DownloadsDir, "independent-seeding-source.mkv")
	if err = os.Link(video, sourceLink); err != nil {
		t.Fatal("generated-media acceptance requires hardlink support", err)
	}
	seedHandle, err := os.Open(sourceLink)
	if err != nil {
		t.Fatal(err)
	}
	originalInfo, err := seedHandle.Stat()
	seedHandle.Close()
	if err != nil {
		t.Fatal(err)
	}
	ass := []byte("[Script Info]\nScriptType: v4.00+\nPlayResX: 320\nPlayResY: 180\n\n[V4+ Styles]\nFormat: Name,Fontname,Fontsize,PrimaryColour,SecondaryColour,OutlineColour,BackColour,Bold,Italic,Underline,StrikeOut,ScaleX,ScaleY,Spacing,Angle,BorderStyle,Outline,Shadow,Alignment,MarginL,MarginR,MarginV,Encoding\nStyle: Default,Arial,16,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,1,0,2,10,10,10,1\n\n[Events]\nFormat: Layer,Start,End,Style,Name,MarginL,MarginR,MarginV,Effect,Text\nDialogue: 0,0:00:00.00,0:00:03.50,Default,,0,0,0,,{\\an8}Owned signs subtitle\n")
	oldSubtitle := strings.TrimSuffix(relative, ".mkv") + ".eng.signs.ass"
	if err = os.WriteFile(filepath.Join(cfg.MediaDir, filepath.FromSlash(oldSubtitle)), ass, 0600); err != nil {
		t.Fatal(err)
	}
	db, err := database.Open(ctx, cfg.ConfigDir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { db.Close() }()
	catalog := library.New(db)
	year := 2026
	season := metadata.Season{SeasonSummary: metadata.SeasonSummary{TMDbID: 990002, Number: 1, Title: "Season 1", EpisodeCount: 2}, Episodes: []metadata.Episode{{TMDbID: 990003, Number: 1, Title: "First"}, {TMDbID: 990004, Number: 2, Title: "Second"}}}
	show, _, err := catalog.AddSeries(ctx, metadata.Series{Movie: metadata.Movie{TMDbID: 990001, Title: "Owned generated series", Year: &year}, Seasons: []metadata.SeasonSummary{season.SeasonSummary}}, false, "anime")
	if err != nil {
		t.Fatal(err)
	}
	if err = catalog.SaveSeason(ctx, show.ID, season, 1); err != nil {
		t.Fatal(err)
	}
	aliases, err := catalog.Anime(ctx, show.ID)
	if err != nil || len(aliases.Episodes) != 2 {
		t.Fatal(aliases, err)
	}
	a, b := 25, 26
	_, err = catalog.SaveAnime(ctx, show.ID, library.AnimeUpdate{Category: "anime", Revision: aliases.Revision, NumberingRevision: aliases.NumberingRevision, Episodes: []library.AnimeAlias{{EpisodeID: aliases.Episodes[0].EpisodeID, AbsoluteNumber: &a}, {EpisodeID: aliases.Episodes[1].EpisodeID, AbsoluteNumber: &b}}})
	if err != nil {
		t.Fatal(err)
	}
	workflow := transfer.New(db, nil, cfg, nil, nil)
	scan, err := workflow.StartLibraryScan(ctx, "series", "anime")
	if err != nil {
		t.Fatal(err)
	}
	for range 100 {
		status, err := workflow.LibraryScan(ctx, scan.ID)
		if err != nil {
			t.Fatal(err)
		}
		if status.State == "completed" {
			break
		}
		if status.State == "failed" {
			t.Fatal("generated-media discovery failed", status.Message)
		}
		if _, err = workflow.ProcessLibraryNext(ctx); err != nil {
			t.Fatal(err)
		}
	}
	files, err := workflow.DiscoveredFiles(ctx, scan.ID, 1, "")
	if err != nil || files.Scan.State != "completed" || len(files.Items) != 1 {
		t.Fatal("expected one generated video for explicit review", files, err)
	}
	if err = workflow.RegisterDiscovered(ctx, files.Items[0].ID, transfer.ExistingRegistration{MediaID: show.ID, Episodes: []int64{aliases.Episodes[0].EpisodeID, aliases.Episodes[1].EpisodeID}, Reviewed: true}); err != nil {
		t.Fatal(err)
	}
	if worked, err := workflow.ProcessLibraryNext(ctx); !worked || err != nil {
		t.Fatal("real FFprobe registration failed", worked, err)
	}
	files, err = workflow.DiscoveredFiles(ctx, scan.ID, 1, "")
	if err != nil || files.Items[0].State != "registered" || files.Items[0].MediaFileID == nil {
		t.Fatal(files, err)
	}
	fileID := *files.Items[0].MediaFileID
	subSum := sha256.Sum256(ass)
	if _, err = db.ExecContext(ctx, `INSERT INTO subtitles(media_file_id,language,variant,source,relative_path,sha256,state,updated_at) VALUES(?,'en','signs','existing',?,?,'available',unixepoch())`, fileID, oldSubtitle, hex.EncodeToString(subSum[:])); err != nil {
		t.Fatal(err)
	}
	// Reopen before reviewing and again after queuing; organization must use
	// persistent identities, not the registration service's in-memory state.
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = database.Open(ctx, cfg.ConfigDir)
	if err != nil {
		t.Fatal(err)
	}
	organizer := organization.New(db, cfg)
	settings, err := organizer.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for i := range settings.Libraries {
		if settings.Libraries[i].Key == "anime" {
			settings.Libraries[i].Preset = "custom"
			settings.Libraries[i].MovieTemplate = "{title} ({year})/{title} ({year})"
			settings.Libraries[i].SeriesTemplate = "{title} ({year})"
			settings.Libraries[i].EpisodeTemplate = "Season {season:02}/{title} - {absolute:03}{absolute_range}"
		}
	}
	if _, err = organizer.Update(ctx, settings); err != nil {
		t.Fatal(err)
	}
	plan, err := organizer.Preview(ctx, []int64{fileID}, "")
	if err != nil || len(plan.Files) != 1 || len(plan.Files[0].Companions) != 1 || !strings.Contains(plan.Files[0].Destination, "025-026.mkv") {
		t.Fatal("generated Anime review lost shared mapping or ASS companion", plan, err)
	}
	if _, err = os.Stat(video); err != nil {
		t.Fatal("preview changed original path", err)
	}
	if _, err = organizer.Apply(ctx, plan.ID); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = database.Open(ctx, cfg.ConfigDir)
	if err != nil {
		t.Fatal(err)
	}
	organizer = organization.New(db, cfg)
	if err = organizer.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	completed, err := organizer.Plan(ctx, plan.ID)
	if err != nil || completed.State != "completed" {
		t.Fatal(completed, err)
	}
	var dest, subDest, storedSHA, origin, probeJSON string
	var downloadID sql.NullString
	if err = db.QueryRowContext(ctx, `SELECT relative_path,sha256,origin,download_id,probe_json FROM media_files WHERE id=?`, fileID).Scan(&dest, &storedSHA, &origin, &downloadID, &probeJSON); err != nil || dest != plan.Files[0].Destination || origin != "existing" || downloadID.Valid || probeJSON == "{}" {
		t.Fatal("organization lost verified registration/provenance", dest, origin, downloadID, err)
	}
	if err = db.QueryRowContext(ctx, `SELECT relative_path FROM subtitles WHERE media_file_id=? AND language='en' AND variant='signs'`, fileID).Scan(&subDest); err != nil || subDest != plan.Files[0].Companions[0].Destination {
		t.Fatal("styled subtitle reference was not moved", subDest, err)
	}
	for _, check := range []struct {
		name string
		body []byte
	}{{filepath.Join(cfg.MediaDir, filepath.FromSlash(dest)), original}, {sourceLink, original}, {filepath.Join(cfg.MediaDir, filepath.FromSlash(subDest)), ass}} {
		got, err := os.ReadFile(check.name)
		if err != nil || !bytes.Equal(got, check.body) {
			t.Fatal("generated media or styled subtitle bytes changed", check.name, err)
		}
	}
	sum := sha256.Sum256(original)
	if storedSHA != hex.EncodeToString(sum[:]) {
		t.Fatal("registered checksum changed during organization", storedSHA)
	}
	for _, old := range []string{video, filepath.Join(cfg.MediaDir, filepath.FromSlash(oldSubtitle))} {
		if _, err = os.Stat(old); !os.IsNotExist(err) {
			t.Fatal("verified old library link was not cleaned", old, err)
		}
	}
	seedHandle, err = os.Open(sourceLink)
	if err != nil {
		t.Fatal(err)
	}
	seedInfo, err := seedHandle.Stat()
	seedHandle.Close()
	if err != nil || !os.SameFile(originalInfo, seedInfo) || originalInfo.ModTime().UnixNano() != seedInfo.ModTime().UnixNano() {
		t.Fatal("independent download-source identity/timestamp changed", originalInfo.ModTime().UnixNano(), seedInfo.ModTime().UnixNano(), err)
	}
	destFile, err := os.Open(filepath.Join(cfg.MediaDir, filepath.FromSlash(dest)))
	if err != nil {
		t.Fatal(err)
	}
	probe, err := (transfer.FFprobe{}).Probe(ctx, destFile)
	destFile.Close()
	if err != nil || probe.Codec != "h264" || probe.Width != 320 || probe.Height != 180 || probe.Duration < 3 {
		t.Fatal("reorganized output failed real FFprobe", probe, err)
	}
	seasonView, err := library.New(db).TVSeason(ctx, show.ID, 1)
	if err != nil || len(seasonView.Episodes) != 2 || seasonView.Episodes[0].FilePath != dest || seasonView.Episodes[1].FilePath != dest || seasonView.SeriesMonitored {
		t.Fatal("shared canonical episodes or paused parent changed", seasonView, err)
	}
	var downloads int
	if err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM downloads`).Scan(&downloads); err != nil || downloads != 0 {
		t.Fatal("reorganization fabricated download history", downloads, err)
	}
	if err = organizer.Tick(ctx); err != nil {
		t.Fatal("completed reorganization repeated on next tick", err)
	}
	t.Log("PASS: generated video was discovered, explicitly mapped to two Anime episodes, verified by real FFprobe, then reviewed and reorganized after database reopen; absolute range, SHA-256, styled ASS bytes/references and external hardlinked source survived, old library paths were cleaned, and no download history was fabricated")
}
