package subtitle

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/securestore"
)

type syntheticReader struct{ word uint64 }

func (r syntheticReader) ReadAt(b []byte, _ int64) (int, error) {
	for i := 0; i < len(b); i += 8 {
		binary.LittleEndian.PutUint64(b[i:], r.word)
	}
	return len(b), nil
}
func TestMovieHashUses64BitSizeAndWraparound(t *testing.T) {
	for _, v := range []struct {
		size int64
		word uint64
		want string
	}{{131072, 0, "0000000000020000"}, {1 << 32, 1, "0000000100004000"}, {131072, ^uint64(0), "000000000001c000"}} {
		got, e := movieHash(syntheticReader{v.word}, v.size)
		if e != nil || got != v.want {
			t.Fatal(got, v.want, e)
		}
	}
	if got, e := movieHash(syntheticReader{}, 131071); e != nil || got != "" {
		t.Fatal(got, e)
	}
}
func TestSubtitleContentAndPathBoundaries(t *testing.T) {
	for _, bad := range [][]byte{nil, []byte("<html>" + validSRT), []byte("WEBVTT\n" + validSRT), []byte(strings.Replace(validSRT, "00:00:03,000", "00:90:03,000", 1)), []byte(strings.Replace(validSRT, "00:00:03,000", "00:10:03,000", 1)), []byte("1\n00:00:01,000 --> 00:00:00,000\nInvalid\n"), {0xff, 0x00}, bytes.Repeat([]byte("a"), MaxSubtitle+1)} {
		if _, e := normalizeSRT(bad, 90); e == nil {
			t.Fatal("invalid subtitle accepted")
		}
	}
	got, e := normalizeSRT(append([]byte{0xef, 0xbb, 0xbf}, []byte(strings.ReplaceAll(validSRT, "\n", "\r\n"))...), 90)
	if e != nil || string(got) != validSRT {
		t.Fatal(string(got), e)
	}
	for _, p := range []string{"../video.mkv", "/video.mkv", "C:/video.mkv", "Movies/../video.mkv", "Movies\\video.mkv", "Movies//video.mkv"} {
		if local(p) {
			t.Fatal("unsafe path", p)
		}
	}
	for _, v := range []struct {
		name  string
		track Track
		hi    bool
	}{{"Video.eng.signs.ass", Track{"en", "signs"}, false}, {"Video.en.forced.sdh.srt", Track{"en", "forced"}, true}, {"Video.jpn.vtt", Track{"ja", "full"}, false}} {
		track, hi, ok := sidecar(v.name, "Video")
		if !ok || track != v.track || hi != v.hi {
			t.Fatal(track, hi, ok)
		}
	}
}
func reopen(t *testing.T, f *fixture) {
	t.Helper()
	ctx := context.Background()
	if e := f.db.Close(); e != nil {
		t.Fatal(e)
	}
	db, e := database.Open(ctx, f.cfg.ConfigDir)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	secrets, e := securestore.New(ctx, db, f.cfg.ConfigDir)
	if e != nil {
		t.Fatal(e)
	}
	f.db = db
	f.s = New(db, secrets, f.cfg, f.p)
	f.s.Prober = fixtureProbe{}
}
func TestJournalRecoveryPreservesPartialFilesAndDoesNotRedownload(t *testing.T) {
	for _, phase := range []string{"partial", "staged", "linked", "published"} {
		t.Run(phase, func(t *testing.T) {
			f := setup(t)
			ctx := context.Background()
			enable(t, f, true, Track{"en", "full"})
			m, e := f.s.media(ctx, 1)
			if e != nil {
				t.Fatal(e)
			}
			plan, e := f.s.plan(ctx, m, f.p.matches[0], []byte(validSRT))
			if e != nil {
				t.Fatal(e)
			}
			folder := filepath.Dir(f.video)
			stage := filepath.Join(folder, plan.Stage)
			dest := filepath.Join(f.cfg.MediaDir, filepath.FromSlash(plan.Destination))
			body := []byte(validSRT)
			if phase == "partial" {
				body = body[:13]
			}
			if e = os.WriteFile(stage, body, 0600); e != nil {
				t.Fatal(e)
			}
			if phase == "linked" || phase == "published" {
				if e = os.Link(stage, dest); e != nil {
					t.Fatal(e)
				}
			}
			if phase == "published" {
				if _, e = f.db.Exec(`UPDATE subtitle_imports SET phase='published'`); e != nil {
					t.Fatal(e)
				}
				if e = os.Remove(stage); e != nil {
					t.Fatal(e)
				}
			}
			reopen(t, &f)
			process(t, f.s)
			v, e := f.s.File(ctx, 1, false)
			if e != nil || v.Job.State != "completed" || f.p.downloads != 0 || f.p.searches != 0 {
				t.Fatal(v, v.Job, e, f.p.downloads, f.p.searches)
			}
			actual, e := os.ReadFile(dest)
			if e != nil || string(actual) != validSRT {
				t.Fatal(e, string(actual))
			}
			var committed string
			var content []byte
			if e = f.db.QueryRow(`SELECT phase,content FROM subtitle_imports`).Scan(&committed, &content); e != nil || committed != "committed" || content != nil {
				t.Fatal(committed, len(content), e)
			}
			if phase == "partial" {
				b, e := os.ReadFile(stage)
				if e != nil || !bytes.Equal(b, body) {
					t.Fatal("partial file was modified", e)
				}
			} else if _, e = os.Stat(stage); !errors.Is(e, os.ErrNotExist) {
				t.Fatal("committed staging link remains", e)
			}
			video, _ := os.ReadFile(f.video)
			if !bytes.Equal(video, f.body) {
				t.Fatal("video changed")
			}
		})
	}
}
func TestDestinationCollisionAfterFetchNeverOverwrites(t *testing.T) {
	f := setup(t)
	enable(t, f, true, Track{"en", "full"})
	dest := strings.TrimSuffix(f.video, ".mkv") + ".en.srt"
	f.p.onFetch = func() {
		if e := os.WriteFile(dest, []byte("existing owner sidecar"), 0644); e != nil {
			t.Fatal(e)
		}
	}
	process(t, f.s)
	v, e := f.s.File(context.Background(), 1, false)
	if e != nil || v.Job.State != "blocked" {
		t.Fatal(v, v.Job, e)
	}
	b, e := os.ReadFile(dest)
	if e != nil || string(b) != "existing owner sidecar" {
		t.Fatal("collision overwritten", e)
	}
}
func TestChangedVideoStopsBeforeProviderAndCommittedCleanupIsSelective(t *testing.T) {
	f := setup(t)
	enable(t, f, true, Track{"en", "full"})
	process(t, f.s)
	var stage, dest string
	if e := f.db.QueryRow(`SELECT stage_name,destination FROM subtitle_imports`).Scan(&stage, &dest); e != nil {
		t.Fatal(e)
	}
	stage = filepath.Join(filepath.Dir(f.video), stage)
	dest = filepath.Join(f.cfg.MediaDir, filepath.FromSlash(dest))
	if e := os.Link(dest, stage); e != nil {
		t.Fatal(e)
	}
	unknown := filepath.Join(filepath.Dir(f.video), ".reelscarey-sub-unknown.part")
	if e := os.WriteFile(unknown, []byte("unknown"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := f.db.Exec(`UPDATE subtitle_jobs SET next_run=0`); e != nil {
		t.Fatal(e)
	}
	process(t, f.s)
	if _, e := os.Stat(stage); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("committed link not cleaned", e)
	}
	if b, e := os.ReadFile(unknown); e != nil || string(b) != "unknown" {
		t.Fatal("unknown file removed", e)
	}
	// A same-sized edit with a different timestamp must invalidate the inspection.
	changed := bytes.Repeat([]byte("x"), len(f.body))
	if e := os.WriteFile(f.video, changed, 0644); e != nil {
		t.Fatal(e)
	}
	if e := os.Chtimes(f.video, time.Now().Add(time.Minute), time.Now().Add(time.Minute)); e != nil {
		t.Fatal(e)
	}
	if _, e := f.db.Exec(`UPDATE subtitle_jobs SET next_run=0`); e != nil {
		t.Fatal(e)
	}
	process(t, f.s)
	v, e := f.s.File(context.Background(), 1, false)
	if e != nil || v.Job.State != "blocked" || f.p.searches != 1 || f.p.downloads != 1 {
		t.Fatal(v, v.Job, e)
	}
}
func TestQuotaAfterFirstTrackKeepsCompletedSubtitle(t *testing.T) {
	f := setup(t)
	enable(t, f, true)
	zero := 0
	f.p.remaining = &zero
	process(t, f.s)
	v, e := f.s.File(context.Background(), 1, false)
	if e != nil || v.Job.State != "waiting_provider" || f.p.downloads != 1 {
		t.Fatal(v, v.Job, e)
	}
	var phase string
	if e = f.db.QueryRow(`SELECT phase FROM subtitle_imports`).Scan(&phase); e != nil || phase != "committed" {
		t.Fatal(phase, e)
	}
	if len(v.Tracks) != 1 || v.Tracks[0].State != "available" {
		t.Fatal(v.Tracks)
	}
}

func TestLeasesAndExpiredSelectionsCannotPublish(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	enable(t, f, true, Track{"en", "full"})
	p, e := f.s.Preferences(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.s.discover(ctx, p); e != nil {
		t.Fatal(e)
	}
	j, e := f.s.claim(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.claim(ctx); !errors.Is(e, sql.ErrNoRows) {
		t.Fatal("live job claimed twice", e)
	}
	if _, e = f.db.Exec(`UPDATE subtitle_jobs SET lease_until=0`); e != nil {
		t.Fatal(e)
	}
	newJob, e := f.s.claim(ctx)
	if e != nil || newJob.owner == j.owner {
		t.Fatal("expired lease not reclaimed", e)
	}
	if e = f.s.fence(ctx, nil, j, "", false); !errors.Is(e, ErrLease) {
		t.Fatal("expired owner could commit", e)
	}
	if _, e = f.db.Exec(`UPDATE subtitle_jobs SET lease_until=0,state='queued'`); e != nil {
		t.Fatal(e)
	}
	if e = f.s.Search(ctx, 1); e != nil {
		t.Fatal(e)
	}
	process(t, f.s)
	v, e := f.s.File(ctx, 1, true)
	if e != nil || len(v.Candidates) == 0 {
		t.Fatal(v, e)
	}
	id := v.Candidates[0].ID
	if _, e = f.db.Exec(`UPDATE subtitle_candidates SET expires_at=0`); e != nil {
		t.Fatal(e)
	}
	requireCode(t, f.s.Select(ctx, id, true), "candidate_expired")
}
