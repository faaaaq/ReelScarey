package subtitle

import (
	"context"
	"testing"
)

func TestShowSubtitlePreferencesInheritPersistAndFenceGlobalEdits(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	for _, q := range []string{`UPDATE media_items SET kind='series' WHERE id=1`, `INSERT INTO series_details(media_id) VALUES(1)`, `UPDATE media_files SET movie_id=NULL,series_id=1 WHERE id=1`, `INSERT INTO media_items(id,kind,tmdb_id,title) VALUES(2,'series',2,'Other show')`, `INSERT INTO series_details(media_id) VALUES(2)`} {
		if _, e := f.db.Exec(q); e != nil {
			t.Fatal(e)
		}
	}
	p, e := f.s.ShowPreferences(ctx, 1)
	if e != nil || !p.Inherited || p.Revision != 0 {
		t.Fatal(p, e)
	}
	in := ShowPreferenceUpdate{Enabled: true, Tracks: []Track{{"ja", "full"}, {"en", "forced"}}, HearingImpaired: "prefer", MatchPolicy: "hash", Revision: p.Revision, GlobalRevision: p.GlobalRevision}
	saved, e := f.s.SaveShowPreferences(ctx, 1, in, false)
	if e != nil || saved.Inherited || saved.Revision != 1 {
		t.Fatal(saved, e)
	}
	effective, e := f.s.effectivePreferences(ctx, 1)
	if e != nil || effective.Enabled || effective.Tracks[0].Language != "ja" {
		t.Fatal("global pause or override not honored", effective, e)
	}
	if _, e = f.s.SaveShowPreferences(ctx, 1, in, false); e == nil {
		t.Fatal("stale global/override revisions accepted")
	}
	other, e := f.s.ShowPreferences(ctx, 2)
	if e != nil || !other.Inherited || other.Preferences.Tracks[0].Language != "en" {
		t.Fatal(other, e)
	}
	enable(t, f, true)
	effective, e = f.s.filePreferences(ctx, 1)
	if e != nil || !effective.Enabled || effective.Tracks[0].Language != "ja" {
		t.Fatal(effective, e)
	}
	fresh, e := f.s.ShowPreferences(ctx, 1)
	if e != nil {
		t.Fatal(e)
	}
	in.Revision = fresh.Revision
	in.GlobalRevision = fresh.GlobalRevision
	in.Enabled = false
	saved, e = f.s.SaveShowPreferences(ctx, 1, in, false)
	if e != nil {
		t.Fatal(e)
	}
	if worked, e := f.s.ProcessNext(ctx); e != nil || worked {
		t.Fatal("paused show automatically processed", worked, e)
	}
	if _, e = f.s.SaveShowPreferences(ctx, 1, ShowPreferenceUpdate{Revision: saved.Revision, GlobalRevision: saved.GlobalRevision}, true); e != nil {
		t.Fatal(e)
	}
	got, e := f.s.ShowPreferences(ctx, 1)
	if e != nil || !got.Inherited || got.Preferences.Tracks[0].Language != "en" {
		t.Fatal(got, e)
	}
}
