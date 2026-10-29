package subtitle

import (
	"bytes"
	"context"
	"github.com/faaaaq/ReelScarey/internal/organization"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func editedFixture(t *testing.T) (fixture, string) {
	t.Helper()
	f := setup(t)
	relative := "Movies/Test/Test.en.srt"
	name := filepath.Join(f.cfg.MediaDir, filepath.FromSlash(relative))
	if e := os.WriteFile(name, []byte(validSRT), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := f.db.Exec(`INSERT INTO subtitles(media_file_id,language,variant,source,relative_path,sha256,state,updated_at) VALUES(1,'en','full','existing',?,?,'available',1)`, relative, contentSHA([]byte(validSRT))); e != nil {
		t.Fatal(e)
	}
	return f, name
}
func TestReviewedSubtitleOffsetReplacementAndRollbackPreserveOriginalAndVideo(t *testing.T) {
	f, name := editedFixture(t)
	ctx := context.Background()
	track := Track{"en", "full"}
	r, e := f.s.ReviewEdit(ctx, 1, EditRequest{Track: track, Action: "offset", Offset: 1500})
	if e != nil || r.Cues != 1 {
		t.Fatal(r, e)
	}
	if b, _ := os.ReadFile(name); string(b) != validSRT {
		t.Fatal("preview wrote a subtitle")
	}
	if e = f.s.QueueEdit(ctx, r.ID, false); e == nil {
		t.Fatal("consent bypassed")
	}
	if e = f.s.QueueEdit(ctx, r.ID, true); e != nil {
		t.Fatal(e)
	}
	if e = organization.EnsureMediaAvailable(ctx, f.db); e == nil {
		t.Fatal("pending edit did not exclude normal media work")
	}
	if done, e := f.s.ProcessEdit(ctx); e != nil || !done {
		t.Fatal(done, e)
	}
	b, e := os.ReadFile(name)
	if e != nil || !strings.Contains(string(b), "00:00:02,500 --> 00:00:04,500") {
		t.Fatal(string(b), e)
	}
	old, e := os.ReadFile(filepath.Join(f.cfg.MediaDir, filepath.FromSlash(r.Previous)))
	if e != nil || string(old) != validSRT {
		t.Fatal("original not retained", e)
	}
	video, e := os.ReadFile(f.video)
	if e != nil || !bytes.Equal(video, f.body) {
		t.Fatal("video changed", e)
	}
	rollback, e := f.s.ReviewEdit(ctx, 1, EditRequest{Track: track, Action: "rollback", PreviousID: r.ID})
	if e != nil {
		t.Fatal(e)
	}
	if e = f.s.QueueEdit(ctx, rollback.ID, true); e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.ProcessEdit(ctx); e != nil {
		t.Fatal(e)
	}
	b, _ = os.ReadFile(name)
	if string(b) != validSRT {
		t.Fatal("rollback did not restore cue bytes", string(b))
	}
	replacement := strings.ReplaceAll(validSRT, "Generated subtitle fixture.", "Reviewed replacement.")
	r, e = f.s.ReviewEdit(ctx, 1, EditRequest{Track: track, Action: "replace", Content: replacement})
	if e != nil {
		t.Fatal(e)
	}
	if e = f.s.QueueEdit(ctx, r.ID, true); e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.ProcessNext(ctx); e != nil {
		t.Fatal(e)
	}
	b, _ = os.ReadFile(name)
	if string(b) != replacement {
		t.Fatal("replacement not published")
	}
	if e = organization.EnsureMediaAvailable(ctx, f.db); e != nil {
		t.Fatal("completed edit retained reservation", e)
	}
}
func TestSubtitleJournalRecoversEveryPublicationBoundaryAndPreservesForeignOutput(t *testing.T) {
	for _, cut := range []string{"staged_original", "staged_missing", "staged_published", "published", "foreign"} {
		t.Run(cut, func(t *testing.T) {
			f, name := editedFixture(t)
			ctx := context.Background()
			r, e := f.s.ReviewEdit(ctx, 1, EditRequest{Track: Track{"en", "full"}, Action: "offset", Offset: 1000})
			if e != nil {
				t.Fatal(e)
			}
			if e = f.s.QueueEdit(ctx, r.ID, true); e != nil {
				t.Fatal(e)
			}
			v, e := scanEdit(f.db.QueryRow(`SELECT `+editColumns+` FROM subtitle_edits WHERE id=?`, r.ID))
			if e != nil {
				t.Fatal(e)
			}
			stage := filepath.Join(f.cfg.MediaDir, filepath.FromSlash(v.stage))
			previous := filepath.Join(f.cfg.MediaDir, filepath.FromSlash(v.Previous))
			if e = os.MkdirAll(filepath.Dir(previous), 0700); e != nil {
				t.Fatal(e)
			}
			if e = os.WriteFile(previous, []byte(validSRT), 0600); e != nil {
				t.Fatal(e)
			}
			if e = os.WriteFile(stage, v.content, 0600); e != nil {
				t.Fatal(e)
			}
			phase := "staged"
			if cut != "staged_original" {
				if e = os.Remove(name); e != nil {
					t.Fatal(e)
				}
			}
			if cut == "staged_published" || cut == "published" {
				if e = os.Link(stage, name); e != nil {
					t.Fatal(e)
				}
			}
			if cut == "published" {
				phase = "published"
			}
			if cut == "foreign" {
				if e = os.WriteFile(name, []byte("foreign subtitle bytes"), 0600); e != nil {
					t.Fatal(e)
				}
			}
			if _, e = f.db.Exec(`UPDATE subtitle_edits SET state='running',phase=?,lease_owner='interrupted',lease_until=0 WHERE id=?`, phase, r.ID); e != nil {
				t.Fatal(e)
			}
			done, e := f.s.ProcessEdit(ctx)
			if cut == "foreign" {
				if e == nil || !done {
					t.Fatal("foreign destination replaced", e)
				}
				b, _ := os.ReadFile(name)
				if string(b) != "foreign subtitle bytes" {
					t.Fatal("foreign file changed")
				}
				if e = os.Remove(name); e != nil {
					t.Fatal(e)
				}
				if e = f.s.RetryEdit(ctx, r.ID); e != nil {
					t.Fatal(e)
				}
				done, e = f.s.ProcessEdit(ctx)
			}
			if e != nil || !done {
				t.Fatal(done, e)
			}
			b, e := os.ReadFile(name)
			if e != nil || !strings.Contains(string(b), "00:00:02,000") {
				t.Fatal(string(b), e)
			}
			old, _ := os.ReadFile(previous)
			if string(old) != validSRT {
				t.Fatal("retained version changed")
			}
		})
	}
}
func TestChangedSubtitleAndExpiredReviewCannotPublish(t *testing.T) {
	f, name := editedFixture(t)
	ctx := context.Background()
	r, e := f.s.ReviewEdit(ctx, 1, EditRequest{Track: Track{"en", "full"}, Action: "offset", Offset: 1000})
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(name, []byte(strings.ReplaceAll(validSRT, "fixture.", "changed.")), 0600); e != nil {
		t.Fatal(e)
	}
	if e = f.s.QueueEdit(ctx, r.ID, true); e == nil {
		t.Fatal("changed source accepted")
	}
	if _, e = f.db.Exec(`UPDATE subtitle_edits SET expires_at=0 WHERE id=?`, r.ID); e != nil {
		t.Fatal(e)
	}
	if e = f.s.QueueEdit(ctx, r.ID, true); e == nil {
		t.Fatal("expired review accepted")
	}
}
func TestTimedTextFormatsRetainPayloadAndRejectUnsafeTimes(t *testing.T) {
	ass := "[Script Info]\nTitle: Fixture\n[V4+ Styles]\nFormat: Name,Fontname\nStyle: Default,Arial\n[Events]\nFormat: Layer,Start,End,Style,Name,MarginL,MarginR,MarginV,Effect,Text\nDialogue: 0,0:00:01.00,0:00:03.00,Default,,0,0,0,,{\\i1}Styled, text{\\i0}\n"
	vtt := "WEBVTT\n\ncue-one\n00:00:01.000 --> 00:00:03.000 align:start\nFixture text\n"
	for _, tc := range []struct{ ext, body, expected string }{{".srt", validSRT, "00:00:02,000"}, {".vtt", vtt, "00:00:02.000 --> 00:00:04.000 align:start"}, {".ass", ass, "0:00:02.00,0:00:04.00"}, {".ssa", ass, "0:00:02.00,0:00:04.00"}} {
		b, n, e := ShiftTimedText([]byte(tc.body), tc.ext, 1000)
		if e != nil || n != 1 || !strings.Contains(string(b), tc.expected) {
			t.Fatal(tc.ext, string(b), n, e)
		}
		if tc.ext == ".ass" && !strings.Contains(string(b), "{\\i1}Styled, text{\\i0}") {
			t.Fatal("ASS styling changed")
		}
		if _, _, e = ShiftTimedText([]byte(tc.body), tc.ext, -2000); e == nil {
			t.Fatal("negative cue accepted")
		}
	}
	for _, body := range []string{"<html>not subtitles</html>", "1\n99:99:99,000 --> 99:99:99,500\nBad\n", "1\n00:00:04,000 --> 00:00:01,000\nBad\n"} {
		if _, _, e := ShiftTimedText([]byte(body), ".srt", 1000); e == nil {
			t.Fatal("invalid cues accepted", body)
		}
	}
}
