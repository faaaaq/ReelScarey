package music

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/faaaaq/ReelScarey/internal/database"
)

func seedWanted(t *testing.T, service *Service) {
	t.Helper()
	db := service.store.db
	statements := []string{
		`INSERT INTO music_artists(id,musicbrainz_id,name,monitored,status,created_at,updated_at) VALUES
			(1,'artist-1','A Artist',1,'missing',1,1),
			(2,'artist-2','B Artist',0,'missing',1,1),
			(3,'artist-3','C Artist',1,'missing',1,1)`,
		`INSERT INTO music_albums(id,artist_id,musicbrainz_id,title,year,monitored,status,created_at,updated_at) VALUES
			(10,1,'album-10','Beta',NULL,1,'missing',1,1),
			(11,1,'album-11','Alpha',2001,1,'missing',1,1),
			(12,1,'album-12','Unmonitored Album',1999,0,'missing',1,1),
			(13,2,'album-13','Hidden',1998,1,'missing',1,1),
			(14,3,'album-14','Owned',1997,1,'available',1,1)`,
		`INSERT INTO music_tracks(id,album_id,musicbrainz_id,recording_id,disc,number,title,created_at,updated_at) VALUES
			(100,11,'track-100','recording-100',1,1,'One',1,1),
			(101,11,'track-101','recording-101',1,2,'Two',1,1)`,
		`INSERT INTO music_files(artist_id,album_id,relative_path,size_bytes,format,imported_at,origin) VALUES
			(3,14,'Music/C Artist/Owned/01.flac',1000,'FLAC',1,'existing')`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
}

func TestWantedAlbumsProjection(t *testing.T) {
	service, ctx := preferencesFixture(t)
	seedWanted(t, service)

	page, err := service.WantedAlbums(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || page.Limit != pageSize || page.Page != 1 || len(page.Items) != 2 {
		t.Fatal("unexpected wanted page", page)
	}
	alpha, beta := page.Items[0], page.Items[1]
	if alpha.Title != "Alpha" || alpha.ArtistName != "A Artist" || alpha.ArtistID != 1 || alpha.ID != 11 {
		t.Fatal("unexpected first wanted album", alpha)
	}
	if alpha.Year == nil || *alpha.Year != 2001 {
		t.Fatal("year was not projected", alpha.Year)
	}
	if alpha.TrackCount != 2 || alpha.Status != "missing" {
		t.Fatal("unexpected track count or status", alpha)
	}
	if beta.Title != "Beta" || beta.Year != nil || beta.TrackCount != 0 {
		t.Fatal("unexpected second wanted album", beta)
	}

	for _, test := range []struct {
		page int
		ok   bool
	}{{0, false}, {-1, false}, {100001, false}, {1, true}} {
		_, err := service.WantedAlbums(ctx, test.page)
		if test.ok && err != nil {
			t.Fatal("valid page rejected", err)
		}
		if !test.ok && !errors.Is(err, ErrInvalidInput) {
			t.Fatal("invalid page accepted", test.page, err)
		}
	}
}

func TestWantedAlbumsPagingAndDueLimit(t *testing.T) {
	service, ctx := preferencesFixture(t)
	seedWanted(t, service)
	for i := 1; i <= 50; i++ {
		if _, err := service.store.db.Exec(`INSERT INTO music_albums(artist_id,musicbrainz_id,title,monitored,status,created_at,updated_at) VALUES(3,?,?,1,'missing',1,1)`,
			fmt.Sprintf("bulk-%02d", i), fmt.Sprintf("Bulk %02d", i)); err != nil {
			t.Fatal(err)
		}
	}

	first, err := service.WantedAlbums(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if first.Total != 52 || len(first.Items) != pageSize {
		t.Fatal("unexpected first page", first.Total, len(first.Items))
	}
	second, err := service.WantedAlbums(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	if second.Total != 52 || len(second.Items) != 4 || second.Items[0].Title != "Bulk 47" || second.Items[3].Title != "Bulk 50" {
		t.Fatal("unexpected second page", second)
	}

	due, err := service.DueWantedAlbums(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 1 || due[0].Title != "Alpha" {
		t.Fatal("unexpected due album", due)
	}
	all, err := service.DueWantedAlbums(ctx, 10000)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 52 {
		t.Fatal("due limit was not bounded to the catalog", len(all))
	}
	if _, err := service.DueWantedAlbums(ctx, 0); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("zero due limit accepted", err)
	}
}

func TestSchedulePersistsAndChecksRevision(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := database.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(db, "")
	schedule, err := service.GetSchedule(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if schedule.Enabled || schedule.IntervalHours != 24 || schedule.Mode != "search" || schedule.NextRun != 0 || schedule.Revision != 1 {
		t.Fatal("unexpected schedule defaults", schedule)
	}

	before := time.Now().Unix()
	saved, err := service.SaveSchedule(ctx, MusicSchedule{Enabled: true, IntervalHours: 6, Mode: "download", Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !saved.Enabled || saved.IntervalHours != 6 || saved.Mode != "download" || saved.Revision != 2 {
		t.Fatal("unexpected saved schedule", saved)
	}
	if saved.NextRun < before+5*3600 || saved.NextRun > before+7*3600 {
		t.Fatal("next run was not scheduled", saved.NextRun)
	}

	if _, err := service.SaveSchedule(ctx, MusicSchedule{Enabled: true, IntervalHours: 12, Mode: "search", Revision: 1}); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale schedule revision accepted: %v", err)
	}
	for _, in := range []MusicSchedule{
		{Enabled: true, IntervalHours: 0, Mode: "search", Revision: 2},
		{Enabled: true, IntervalHours: 169, Mode: "search", Revision: 2},
		{Enabled: true, IntervalHours: 24, Mode: "automatic", Revision: 2},
	} {
		if _, err := service.SaveSchedule(ctx, in); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("invalid schedule accepted: %+v", in)
		}
	}

	disabled, err := service.SaveSchedule(ctx, MusicSchedule{Enabled: false, IntervalHours: 6, Mode: "download", Revision: 2})
	if err != nil {
		t.Fatal(err)
	}
	if disabled.Enabled || disabled.NextRun != 0 || disabled.Revision != 3 {
		t.Fatal("unexpected disabled schedule", disabled)
	}

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := database.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	persisted, err := NewService(reopened, "").GetSchedule(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Enabled || persisted.Revision != 3 || persisted.IntervalHours != 6 || persisted.Mode != "download" || persisted.NextRun != 0 {
		t.Fatal("schedule was not persisted", persisted)
	}
}
