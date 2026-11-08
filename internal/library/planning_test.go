package library

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/faaaaq/ReelScarey/internal/database"
)

var planningNow = time.Date(2026, 10, 24, 1, 0, 0, 0, time.UTC)

func planningFixture(t *testing.T) (*Store, *sql.DB) {
	t.Helper()
	db, e := database.Open(context.Background(), t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	s := New(db)
	for _, q := range []string{
		`INSERT INTO media_items(id,kind,tmdb_id,title,year,monitored,release_date) VALUES(1,'movie',1,'Missing movie',2026,1,'2026-10-01'),(2,'movie',2,'Future movie',2026,1,'2026-11-01'),(3,'movie',3,'No date',2026,1,''),(4,'movie',4,'Bad date',2026,1,'2026-02-30'),(5,'movie',5,'Available movie',2026,1,'2026-10-12'),(6,'movie',6,'Paused movie',2026,0,'2026-10-21'),(7,'movie',7,'Downloading movie',2026,1,'2026-10-20'),(8,'movie',8,'Failed movie',2026,1,'2026-10-22'),(10,'series',10,'Saved Show',2026,1,'2026-01-01'),(11,'series',11,'Paused Show',2026,0,'2026-01-01')`,
		`INSERT INTO series_details(media_id) VALUES(10),(11)`,
		`INSERT INTO seasons(id,series_id,tmdb_id,number,title,overview,air_date,episode_count,monitored,loaded_at) VALUES(101,10,101,1,'First','','',8,1,'loaded'),(102,10,102,2,'Paused','','',1,0,'loaded'),(103,10,103,3,'Not loaded','','',1,1,''),(104,10,104,0,'Specials','','',1,0,'loaded'),(111,11,111,1,'First','','',1,1,'loaded')`,
		`INSERT INTO episodes(id,season_id,tmdb_id,number,title,overview,air_date,monitored) VALUES(1001,101,1001,1,'Past','','2026-10-01',1),(1002,101,1002,2,'Today','','2026-10-24',1),(1003,101,1003,3,'Tomorrow','','2026-10-25',1),(1004,101,1004,4,'Undated','','',1),(1005,101,1005,5,'Shared first','','2026-10-05',1),(1006,101,1006,6,'Shared second','','2026-10-06',1),(1007,101,1007,7,'Review pending','','2026-10-07',1),(1008,101,1008,8,'Unselected','','2026-10-08',0),(2001,102,2001,1,'Paused season','','2026-10-24',1),(3001,103,3001,1,'Unloaded entry','','2026-10-24',1),(4001,104,4001,1,'Special','','2026-10-24',1),(11001,111,11001,1,'Paused show','','2026-10-24',1)`,
		`INSERT INTO media_files(id,series_id,relative_path,size_bytes,sha256,probe_json,imported_at,origin) VALUES(1,10,'TV/shared.mp4',100,'sha','{}',1,'existing')`,
		`INSERT INTO media_files(id,movie_id,relative_path,size_bytes,sha256,probe_json,imported_at,origin) VALUES(2,5,'Movies/registered.mp4',100,'sha','{}',1,'existing')`,
		`INSERT INTO episode_files(episode_id,media_file_id) VALUES(1005,1),(1006,1)`,
	} {
		if _, e = db.Exec(q); e != nil {
			t.Fatal(q, e)
		}
	}
	for _, j := range []struct {
		id                    string
		media                 int
		kind, state, protocol string
	}{{"movie-job", 7, "movie", "downloading", "torrent"}, {"failed-job", 8, "movie", "failed", "usenet"}, {"tv-job", 10, "series", "blocked", "usenet"}} {
		if _, e = db.Exec(`INSERT INTO release_candidates(id,movie_id,title,size_bytes,published_at,payload,expires_at) VALUES(?,?,'release',100,'',X'01',1)`, j.id, j.media); e != nil {
			t.Fatal(e)
		}
		if _, e = db.Exec(`INSERT INTO downloads(id,movie_id,release_id,title,info_hash,payload,client_url,save_path,state,kind,protocol,created_at,updated_at) VALUES(?,?,?,'private release',?,X'02','https://private.invalid','/private',?,?,?,1,1)`, j.id, j.media, j.id, j.id, j.state, j.kind, j.protocol); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = db.Exec(`INSERT INTO download_tv(download_id,series_tmdb_id,series_title,season_number,series_folder,naming) VALUES('tv-job',10,'Saved Show',1,'TV','standard');INSERT INTO episode_downloads(episode_id,download_id) VALUES(1007,'tv-job'),(1005,'tv-job')`); e != nil {
		t.Fatal(e)
	}
	return s, db
}
func TestWantedDistinguishesDatesMonitoringRegistrationAndReservations(t *testing.T) {
	s, db := planningFixture(t)
	ctx := context.Background()
	var before int
	if e := db.QueryRow(`SELECT total_changes()`).Scan(&before); e != nil {
		t.Fatal(e)
	}
	f := PlanningFilter{OnlyMonitored: true, Status: "all"}
	out, e := s.Wanted(ctx, f, planningNow)
	if e != nil {
		t.Fatal(e)
	}
	want := PlanningCounts{Missing: 3, Upcoming: 2, Undated: 3, Downloading: 1, Attention: 2}
	if out.Counts != want || out.Total != 11 || len(out.Items) != 11 || out.Coverage.UnloadedSeasons != 1 || out.Coverage.Undated != 3 {
		t.Fatalf("wrong catalog state: %+v", out)
	}
	for _, v := range out.Items {
		if v.Available || !v.Monitored || (v.EpisodeID != nil && *v.EpisodeID == 3001) {
			t.Fatal("unavailable scope was wrong", v)
		}
	}
	out, e = s.Wanted(ctx, PlanningFilter{OnlyMonitored: true}, planningNow)
	if e != nil || out.Total != 3 {
		t.Fatal("default released missing selection", out, e)
	}
	out, e = s.Wanted(ctx, PlanningFilter{Kind: "series", Status: "attention", OnlyMonitored: true}, planningNow)
	if e != nil || out.Total != 1 || out.Items[0].DownloadID != "tv-job" || out.Items[0].Protocol != "usenet" {
		t.Fatal("TV reservation not actionable", out, e)
	}
	all, e := s.Wanted(ctx, PlanningFilter{Status: "all"}, planningNow)
	if e != nil || all.Total != 16 {
		t.Fatal("paused items not included on request", all.Total, e)
	}
	reasons := map[string]bool{}
	for _, v := range all.Items {
		if !v.Monitored {
			reasons[v.MonitoringReason] = true
		}
	}
	for _, reason := range []string{"Movie monitoring is paused.", "Show monitoring is paused.", "Season monitoring is paused.", "Episode is not monitored."} {
		if !reasons[reason] {
			t.Fatal("missing parent pause explanation", reason)
		}
	}
	var after int
	if e = db.QueryRow(`SELECT total_changes()`).Scan(&after); e != nil || after != before {
		t.Fatal("planning reads mutated state", before, after, e)
	}
}
func TestPlanningTitleOrderIgnoresLeadingThe(t *testing.T) {
	s, db := planningFixture(t)
	if _, e := db.Exec(`INSERT INTO media_items(id,kind,tmdb_id,title,year,monitored,release_date) VALUES(20,'movie',20,'The Apple fruit',2026,1,'2026-10-13'),(21,'movie',21,'Banana fruit',2026,1,'2026-10-13')`); e != nil {
		t.Fatal(e)
	}
	out, e := s.Wanted(context.Background(), PlanningFilter{Status: "all", Query: "fruit"}, planningNow)
	if e != nil || len(out.Items) != 2 {
		t.Fatal(out, e)
	}
	if out.Items[0].Title != "The Apple fruit" || out.Items[1].Title != "Banana fruit" {
		t.Fatalf("planning order did not ignore the leading article: %+v", out.Items)
	}
}

func TestCalendarIncludesSharedEpisodesOnceAndUsesUTCDateOnly(t *testing.T) {
	s, _ := planningFixture(t)
	ctx := context.Background()
	local := time.Date(2026, 10, 23, 20, 0, 0, 0, time.FixedZone("test", -7*3600))
	out, e := s.Calendar(ctx, PlanningFilter{Month: "2026-10", OnlyMonitored: true}, local)
	if e != nil || out.Month != "2026-10" || out.AsOfDate != "2026-10-24" || out.Total != 10 || out.Counts.Available != 3 || out.Counts.Missing != 3 || out.Counts.Upcoming != 1 {
		t.Fatal("calendar date/state", out, e)
	}
	seen := map[int64]bool{}
	for _, v := range out.Items {
		if v.Date == "" || v.Date[:7] != "2026-10" {
			t.Fatal("date escaped month", v)
		}
		if v.EpisodeID != nil {
			if seen[*v.EpisodeID] {
				t.Fatal("episode repeated")
			}
			seen[*v.EpisodeID] = true
		}
	}
	if !seen[1005] || !seen[1006] {
		t.Fatal("shared video removed one calendar episode")
	}
	if out.Coverage.Undated != 3 || out.Coverage.UnloadedSeasons != 1 {
		t.Fatal("incomplete metadata hidden", out.Coverage)
	}
	november, e := s.Calendar(ctx, PlanningFilter{Month: "2026-11", OnlyMonitored: true}, planningNow)
	if e != nil || november.Total != 1 || november.Items[0].Title != "Future movie" {
		t.Fatal(november, e)
	}
}
func TestCalendarExpandsMovieReleaseKindsAndFiltersThem(t *testing.T) {
	s, db := planningFixture(t)
	ctx := context.Background()
	if _, e := db.Exec(`INSERT INTO media_items(id,kind,tmdb_id,title,year,monitored,release_date,theatrical_release_date,digital_release_date,physical_release_date) VALUES(30,'movie',30,'Three Date Movie',2026,1,'2026-10-05','2026-10-10','2026-11-15','2026-10-20'),(31,'movie',31,'Single Date Movie',2026,1,'2026-10-03','','','')`); e != nil {
		t.Fatal(e)
	}
	october, e := s.Calendar(ctx, PlanningFilter{Month: "2026-10", OnlyMonitored: true}, planningNow)
	if e != nil {
		t.Fatal(e)
	}
	kinds := map[string]string{}
	for _, v := range october.Items {
		if v.MediaID == 30 {
			kinds[v.ReleaseKind] = v.Date
			if v.TheatricalDate != "2026-10-10" || v.DigitalDate != "2026-11-15" || v.PhysicalDate != "2026-10-20" {
				t.Fatal("calendar rows lost the saved release dates", v)
			}
		}
		if v.MediaID == 31 && (v.ReleaseKind != "" || v.Date != "2026-10-03" || v.TheatricalDate != "" || v.DigitalDate != "" || v.PhysicalDate != "") {
			t.Fatal("a movie without detailed dates did not keep its canonical row", v)
		}
	}
	if len(kinds) != 2 || kinds["theatrical"] != "2026-10-10" || kinds["physical"] != "2026-10-20" {
		t.Fatal("calendar did not expand known release kinds", kinds)
	}
	if _, exists := kinds[""]; exists {
		t.Fatal("a movie with detailed dates also kept a canonical row")
	}
	november, e := s.Calendar(ctx, PlanningFilter{Month: "2026-11", OnlyMonitored: true}, planningNow)
	if e != nil {
		t.Fatal(e)
	}
	var digital, future bool
	for _, v := range november.Items {
		if v.MediaID == 30 && v.ReleaseKind == "digital" && v.Date == "2026-11-15" {
			digital = true
		}
		if v.MediaID == 2 && v.ReleaseKind == "" {
			future = true
		}
	}
	if !digital || !future {
		t.Fatal("november release kind missing", november.Items)
	}
	filtered, e := s.Calendar(ctx, PlanningFilter{Month: "2026-11", OnlyMonitored: true, Release: "digital"}, planningNow)
	if e != nil || filtered.Total != 1 || len(filtered.Items) != 1 || filtered.Items[0].MediaID != 30 || filtered.Items[0].ReleaseKind != "digital" {
		t.Fatal("release filter did not narrow the calendar", filtered, e)
	}
	none, e := s.Calendar(ctx, PlanningFilter{Month: "2026-11", OnlyMonitored: true, Release: "theatrical"}, planningNow)
	if e != nil || none.Total != 0 {
		t.Fatal("release filter ignored the kind", none, e)
	}
	if _, e = s.Wanted(ctx, PlanningFilter{Release: "theatrical"}, planningNow); !errors.Is(e, ErrPlanningInput) {
		t.Fatal("wanted accepted a calendar-only release filter", e)
	}
	if _, e = s.Calendar(ctx, PlanningFilter{Month: "2026-10", Release: "premiere"}, planningNow); !errors.Is(e, ErrPlanningInput) {
		t.Fatal("calendar accepted an unknown release kind", e)
	}
}

func TestPlanningLiteralSearchValidationAndStablePagination(t *testing.T) {
	s, db := planningFixture(t)
	ctx := context.Background()
	for _, q := range []string{"%", "_", `\`} {
		title := "Literal " + q + " title"
		if _, e := db.Exec(`INSERT INTO media_items(kind,tmdb_id,title,monitored,release_date) VALUES('movie',?,?,1,'2026-10-01')`, 100+len(q)+int(q[0]), title); e != nil {
			t.Fatal(e)
		}
		out, e := s.Wanted(ctx, PlanningFilter{Query: q, OnlyMonitored: true}, planningNow)
		if e != nil || out.Total != 1 || out.Items[0].Title != title {
			t.Fatal("LIKE metacharacter was not literal", q, out, e)
		}
	}
	for i := range 60 {
		if _, e := db.Exec(`INSERT INTO media_items(kind,tmdb_id,title,monitored,release_date) VALUES('movie',?,'Same name',1,'2026-10-01')`, 1000+i); e != nil {
			t.Fatal(e)
		}
	}
	a, e := s.Wanted(ctx, PlanningFilter{Query: "Same name", OnlyMonitored: true, Page: 1}, planningNow)
	if e != nil || a.Total != 60 || len(a.Items) != 50 {
		t.Fatal(a.Total, len(a.Items), e)
	}
	b, e := s.Wanted(ctx, PlanningFilter{Query: "Same name", OnlyMonitored: true, Page: 2}, planningNow)
	if e != nil || len(b.Items) != 10 || a.Items[49].MediaID >= b.Items[0].MediaID {
		t.Fatal("unstable pagination", e)
	}
	for _, f := range []PlanningFilter{{Kind: "episode"}, {Page: -1}, {Page: 100001}, {Query: "bad\x00query"}, {Status: "unknown"}, {Month: "2026-10"}, {Status: "available"}} {
		if _, e = s.Wanted(ctx, f, planningNow); !errors.Is(e, ErrPlanningInput) {
			t.Fatal("invalid wanted filter accepted", f, e)
		}
	}
	for _, month := range []string{"2026-13", "2026-1", "0000-01", "10000-01", "not-a-month"} {
		if _, e = s.Calendar(ctx, PlanningFilter{Month: month}, planningNow); !errors.Is(e, ErrPlanningInput) {
			t.Fatal("invalid month accepted", month, e)
		}
	}
	a, e = s.Wanted(ctx, PlanningFilter{Query: "Tomorrow", Status: "all", OnlyMonitored: true}, planningNow)
	if e != nil || a.Total != 1 || a.Items[0].EpisodeTitle != "Tomorrow" {
		t.Fatal("episode title search failed", a, e)
	}
}
func TestPlanningReflectsChangesAndRetainsSavedStateAfterReopen(t *testing.T) {
	s, db := planningFixture(t)
	ctx := context.Background()
	f := PlanningFilter{Kind: "movie", Status: "all", OnlyMonitored: true}
	before, e := s.Wanted(ctx, f, planningNow)
	if e != nil {
		t.Fatal(e)
	}
	var dbPath string
	var seq int
	var name string
	if e = db.QueryRow(`PRAGMA database_list`).Scan(&seq, &name, &dbPath); e != nil {
		t.Fatal(e)
	}
	db.Close()
	db, e = database.Open(ctx, filepath.Dir(dbPath))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	s = New(db)
	after, e := s.Wanted(ctx, f, planningNow)
	if e != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("saved planning changed after reopen", e)
	}
	if _, e = db.Exec(`UPDATE downloads SET state='cancelled' WHERE id='movie-job';UPDATE media_items SET monitored=0 WHERE id=1`); e != nil {
		t.Fatal(e)
	}
	after, e = s.Wanted(ctx, PlanningFilter{Kind: "movie", OnlyMonitored: true}, planningNow)
	if e != nil || after.Total != 1 || after.Items[0].MediaID != 7 {
		t.Fatal("stopped job/paused title stale", after, e)
	}
	if _, e = db.Exec(`UPDATE media_items SET release_date='2024-02-29' WHERE id=2`); e != nil {
		t.Fatal(e)
	}
	leap, e := s.Calendar(ctx, PlanningFilter{Month: "2024-02", OnlyMonitored: true}, planningNow)
	if e != nil || leap.Total != 1 {
		t.Fatal("leap day lost", leap, e)
	}
}

func TestPlanningCarriesSynopsisAndMonitorAllCoversShowsWithoutTouchingSpecials(t *testing.T) {
	ctx := context.Background()
	s, db := planningFixture(t)
	if _, e := db.Exec(`UPDATE media_items SET overview='A saved movie synopsis.' WHERE id=1;UPDATE episodes SET overview='A saved episode synopsis.' WHERE id=1001`); e != nil {
		t.Fatal(e)
	}
	page, e := s.Calendar(ctx, PlanningFilter{Kind: "all", Status: "all", Month: "2026-10"}, planningNow)
	if e != nil {
		t.Fatal(e)
	}
	var movie, episode bool
	for _, v := range page.Items {
		if v.MediaID == 1 && v.Kind == "movie" && v.Overview == "A saved movie synopsis." {
			movie = true
		}
		if v.EpisodeID != nil && *v.EpisodeID == 1001 && v.Overview == "A saved episode synopsis." {
			episode = true
		}
	}
	if !movie || !episode {
		t.Fatal("planner entries lost their synopsis", page.Items)
	}

	// Per-show monitor-all turns the show on with only today's or future
	// regular episodes and the seasons containing them. Past and undated
	// regular episodes are explicitly unmonitored; specials keep their choices.
	now := time.Now().UTC()
	today := now.Format(time.DateOnly)
	tomorrow := now.AddDate(0, 0, 1).Format(time.DateOnly)
	past := now.AddDate(0, 0, -2).Format(time.DateOnly)
	if _, e = db.Exec(`UPDATE episodes SET air_date=CASE id
		WHEN 1002 THEN ? WHEN 1003 THEN ? WHEN 4001 THEN ? WHEN 11001 THEN ? ELSE ? END`, today, tomorrow, tomorrow, tomorrow, past); e != nil {
		t.Fatal(e)
	}
	var revision int64
	if e = db.QueryRow(`SELECT revision FROM series_details WHERE media_id=10`).Scan(&revision); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(`UPDATE media_items SET monitored=0;UPDATE seasons SET monitored=0;UPDATE episodes SET monitored=0`); e != nil {
		t.Fatal(e)
	}
	if e = s.MonitorAllEpisodes(ctx, 10, revision); e != nil {
		t.Fatal(e)
	}
	for _, q := range []struct {
		sql  string
		want int
	}{
		{`SELECT monitored FROM media_items WHERE id=10`, 1},
		{`SELECT monitored FROM seasons WHERE id=101`, 1},
		{`SELECT monitored FROM seasons WHERE id=102`, 0},
		{`SELECT monitored FROM seasons WHERE id=103`, 0},
		{`SELECT monitored FROM seasons WHERE id=104`, 0},
		{`SELECT COUNT(*) FROM episodes WHERE id IN (1002,1003) AND monitored=1`, 2},
		{`SELECT COUNT(*) FROM episodes WHERE id IN (1001,1004,1005,1006,1007,1008,2001,3001,4001) AND monitored=1`, 0},
	} {
		var got int
		if e = db.QueryRow(q.sql).Scan(&got); e != nil || got != q.want {
			t.Fatal(q.sql, got, e)
		}
	}
	// Re-monitor everything, then run the action again. Past and undated
	// regular episodes must be turned off, not just left alone.
	if _, e = db.Exec(`UPDATE seasons SET monitored=1 WHERE number>0;UPDATE episodes SET monitored=1 WHERE season_id IN (SELECT id FROM seasons WHERE number>0)`); e != nil {
		t.Fatal(e)
	}
	if e = db.QueryRow(`SELECT revision FROM series_details WHERE media_id=10`).Scan(&revision); e != nil {
		t.Fatal(e)
	}
	if e = s.MonitorAllEpisodes(ctx, 10, revision); e != nil {
		t.Fatal(e)
	}
	for _, q := range []struct {
		sql  string
		want int
	}{
		{`SELECT monitored FROM seasons WHERE id=101`, 1},
		{`SELECT COUNT(*) FROM seasons WHERE id IN (102,103,104) AND monitored=1`, 0},
		{`SELECT COUNT(*) FROM episodes WHERE id IN (1002,1003) AND monitored=1`, 2},
		{`SELECT COUNT(*) FROM episodes WHERE id IN (1001,1004,1005,1006,1007,1008,2001,3001,4001) AND monitored=1`, 0},
	} {
		var got int
		if e = db.QueryRow(q.sql).Scan(&got); e != nil || got != q.want {
			t.Fatal(q.sql, got, e)
		}
	}
	if e = s.MonitorAllEpisodes(ctx, 10, revision); !errors.Is(e, ErrRevision) {
		t.Fatal("a stale revision was accepted", e)
	}

	// The library-wide action applies the same today-or-later rule in scope,
	// including turning off previously monitored past episodes.
	if _, e = db.Exec(`UPDATE media_items SET monitored=0;UPDATE seasons SET monitored=1 WHERE number>0;UPDATE episodes SET monitored=1 WHERE season_id IN (SELECT id FROM seasons WHERE number>0)`); e != nil {
		t.Fatal(e)
	}
	count, e := s.MonitorAllShows(ctx, "")
	if e != nil || count != 2 {
		t.Fatal(count, e)
	}
	var on int
	if e = db.QueryRow(`SELECT COUNT(*) FROM media_items WHERE kind='series' AND monitored=1`).Scan(&on); e != nil || on != 2 {
		t.Fatal(on, e)
	}
	for _, q := range []struct {
		sql  string
		want int
	}{
		{`SELECT COUNT(*) FROM seasons WHERE id IN (101,111) AND monitored=1`, 2},
		{`SELECT COUNT(*) FROM seasons WHERE id IN (102,103,104) AND monitored=1`, 0},
		{`SELECT COUNT(*) FROM episodes WHERE id IN (1002,1003,11001) AND monitored=1`, 3},
		{`SELECT COUNT(*) FROM episodes WHERE id IN (1001,1004,1005,1006,1007,1008,2001,3001,4001) AND monitored=1`, 0},
	} {
		var got int
		if e = db.QueryRow(q.sql).Scan(&got); e != nil || got != q.want {
			t.Fatal(q.sql, got, e)
		}
	}
	if _, e = s.MonitorAllShows(ctx, "movies"); !errors.Is(e, ErrMonitoringInput) {
		t.Fatal("an unknown scope was accepted", e)
	}
}
