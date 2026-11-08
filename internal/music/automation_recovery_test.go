package music

import (
	"context"
	"errors"
	"testing"
)

func TestMusicScheduleFencesReservationAndRotatesPastPendingAlbums(t *testing.T) {
	ctx := context.Background()
	s, db, _ := editFixture(t)
	if _, err := db.Exec(`WITH RECURSIVE ids(id) AS (SELECT 3 UNION ALL SELECT id+1 FROM ids WHERE id<34)
		INSERT INTO music_albums(id,artist_id,musicbrainz_id,title,monitored,status,created_at,updated_at) SELECT id,1,'fixture-'||id,'Album '||id,1,'missing',1,1 FROM ids;
		INSERT INTO music_downloads(id,album_id,release_id,title,root,state,created_at,updated_at)
		VALUES('pending',3,'candidate','Pending album','Music','blocked',1,1)`); err != nil {
		t.Fatal(err)
	}
	schedule, err := s.SaveSchedule(ctx, MusicSchedule{Enabled: true, Mode: "download", IntervalHours: 1, Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	prefs, err := s.AlbumPreferences(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	check := func() error {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		return CheckScheduledDownload(ctx, tx, 2, schedule, prefs)
	}
	if err := check(); err != nil {
		t.Fatal("eligible reservation rejected", err)
	}
	first := int64(0)
	runs := 0
	s.SetScheduleRunner(func(_ context.Context, _ MusicSchedule, _ MusicPreferences, albums []WantedAlbum) error {
		runs++
		if len(albums) != 25 {
			t.Fatal("schedule did not select a full batch", albums)
		}
		for _, a := range albums {
			if a.ID == 3 {
				t.Fatal("schedule selected an album with a blocked submission")
			}
		}
		if runs == 1 {
			first = albums[0].ID
			return errors.New("fixture search failed")
		}
		if albums[0].ID <= first {
			t.Fatal("second pass did not rotate", albums)
		}
		return nil
	})
	if err := s.RunSchedule(ctx); err == nil {
		t.Fatal("first pass hid the failure")
	}
	if err := s.RunSchedule(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveSchedule(ctx, MusicSchedule{Enabled: false, Mode: "search", IntervalHours: 1, Revision: schedule.Revision}); err != nil {
		t.Fatal(err)
	}
	if err := check(); !errors.Is(err, ErrRevisionConflict) {
		t.Fatal("an old run could submit after pause", err)
	}
}
