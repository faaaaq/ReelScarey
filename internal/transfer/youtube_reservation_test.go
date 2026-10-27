package transfer

import (
	"context"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/database"
)

func TestExistingMovieRegistrationRespectsYouTubeReservation(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, q := range []string{`INSERT INTO media_items(id,kind,tmdb_id,title) VALUES(1,'movie',123,'Movie')`, `INSERT INTO youtube_reviews(id,video_id,video_title,channel,target_kind,media_id,target_snapshot,expires_at) VALUES('r','abcdefghijk','Video','','movie',1,'{}',9999999999)`, `INSERT INTO youtube_jobs(id,review_id,video_id,video_title,channel,target_kind,media_id,target_snapshot,state,created_at,updated_at) VALUES('j','r','abcdefghijk','Video','','movie',1,'{}','queued',1,1)`} {
		if _, err = db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = existingTargetFree(ctx, tx, 1, "movie", nil); err == nil {
		tx.Rollback()
		t.Fatal("accepted an active YouTube reservation")
	}
	tx.Rollback()
	if _, err = db.Exec(`UPDATE youtube_jobs SET state='cancelled' WHERE id='j'`); err != nil {
		t.Fatal(err)
	}
	tx, err = db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err = existingTargetFree(ctx, tx, 1, "movie", nil); err != nil {
		t.Fatalf("cancelled reservation blocked target: %v", err)
	}
}
