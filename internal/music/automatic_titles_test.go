package music

import (
	"context"
	"strings"
	"testing"
)

func TestAutomaticMetadataCorrectsAlbumEvenWhenTitleMatchesAndRetainsFailedWork(t *testing.T) {
	s, db, _ := editFixture(t)
	insertMusicFile(t, db, 1, 3, "Music/missing.flac", strings.Replace(matchingTags(), `"album":"Album"`, `"album":"Old Album"`, 1))
	worked, err := s.processTitleUpdate(context.Background())
	if err != nil || !worked {
		t.Fatalf("worked=%v error=%v", worked, err)
	}
	var jobs, pending int
	if err := db.QueryRow(`SELECT (SELECT count(*) FROM music_edit_jobs WHERE automatic_title=1),(SELECT count(*) FROM music_title_updates WHERE music_file_id=1 AND next_attempt>unixepoch())`).Scan(&jobs, &pending); err != nil {
		t.Fatal(err)
	}
	if jobs != 1 || pending != 1 {
		t.Fatalf("metadata jobs=%d pending retry=%d; a failed write must remain queued", jobs, pending)
	}
}

func TestAutomaticMetadataClearsAlreadyCorrectTagsWithoutWriting(t *testing.T) {
	s, db, _ := editFixture(t)
	insertMusicFile(t, db, 1, 3, "Music/missing.flac", matchingTags())
	worked, err := s.processTitleUpdate(context.Background())
	if err != nil || !worked {
		t.Fatalf("worked=%v error=%v", worked, err)
	}
	var pending, jobs int
	if err := db.QueryRow(`SELECT (SELECT count(*) FROM music_title_updates),(SELECT count(*) FROM music_edit_jobs)`).Scan(&pending, &jobs); err != nil {
		t.Fatal(err)
	}
	if pending != 0 || jobs != 0 {
		t.Fatalf("pending=%d jobs=%d", pending, jobs)
	}
}
