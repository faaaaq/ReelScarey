package subtitle

import (
	"context"
	"testing"
)

func TestExistingFileHasNoInventedReleaseEvidence(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	if _, e := f.db.Exec(`UPDATE media_files SET origin='existing',download_id=NULL,source_name='Test.Movie.2020.1080p.WEB.mkv' WHERE id=1`); e != nil {
		t.Fatal(e)
	}
	m, e := f.s.media(ctx, 1)
	if e != nil || m.Release != "" {
		t.Fatal("existing file inherited invented release evidence", m.Release, e)
	}
	prefs := Preferences{Tracks: []Track{{Language: "en", Variant: "full"}}, MatchPolicy: "hash_or_release"}
	candidates := []Candidate{{FileID: 1, Track: prefs.Tracks[0], Identity: true, Release: "Test.Movie.2020.1080p.WEB.mkv"}}
	ranked := rank(prefs, m.Target, candidates)
	if len(ranked) != 1 || ranked[0].Automatic {
		t.Fatal("filename alone authorized automatic subtitle", ranked)
	}
	candidates[0].HashMatch = true
	ranked = rank(prefs, m.Target, candidates)
	if !ranked[0].Automatic {
		t.Fatal("confirmed provider hash could not match existing media")
	}
}
