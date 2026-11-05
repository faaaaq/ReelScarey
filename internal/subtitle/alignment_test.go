package subtitle

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
)

func alignmentFixture() []byte {
	var b strings.Builder
	for i := 0; i < 10; i++ {
		sec := 10 + i*20
		fmt.Fprintf(&b, "%d\n00:%02d:%02d,000 --> 00:%02d:%02d,000\nUnique generated dialogue number %d.\n\n", i+1, sec/60, sec%60, (sec+3)/60, (sec+3)%60, i)
	}
	return []byte(b.String())
}
func TestAlignmentFitsOffsetAndDriftAndRejectsUncertainEvidence(t *testing.T) {
	body := alignmentFixture()
	reference, _, e := transformTimedText(body, ".srt", func(ms int64) int64 { return ms*25/24 + 2500 })
	if e != nil {
		t.Fatal(e)
	}
	aligned, message, e := AlignTimedText(body, ".srt", reference, ".srt")
	if e != nil || string(aligned) != string(reference) || !strings.Contains(message, "10 of 10") {
		t.Fatal(string(aligned), message, e)
	}
	for _, bad := range [][]byte{[]byte(validSRT), []byte(strings.ReplaceAll(string(reference), "Unique", "Different")), []byte(strings.Repeat("<html>", 100))} {
		if _, _, e = AlignTimedText(body, ".srt", bad, ".srt"); e == nil {
			t.Fatal("weak evidence accepted")
		}
	}
	cut, _, _ := transformTimedText(body, ".srt", func(ms int64) int64 {
		if ms > 100000 {
			return ms + 9000
		}
		return ms + 2000
	})
	if _, _, e = AlignTimedText(body, ".srt", cut, ".srt"); e == nil {
		t.Fatal("different cut accepted")
	}
	dup := []byte(strings.ReplaceAll(string(body), "number 1.", "number 0."))
	if _, _, e = AlignTimedText(dup, ".srt", reference, ".srt"); e != nil {
		t.Fatal("duplicate cues should be excluded, leaving eight unique matches", e)
	}
}
func TestAlignmentUsesReviewedJournalAndRollback(t *testing.T) {
	skipWithoutDirectorySync(t)
	f, name := editedFixture(t)
	body := alignmentFixture()
	if e := os.WriteFile(name, body, 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := f.db.Exec(`UPDATE subtitles SET sha256=?`, contentSHA(body)); e != nil {
		t.Fatal(e)
	}
	reference, _, _ := ShiftTimedText(body, ".srt", 2000)
	ctx := context.Background()
	r, e := f.s.ReviewEdit(ctx, 1, EditRequest{Track: Track{"en", "full"}, Action: "align", Content: string(reference), ReferenceFormat: "srt"})
	if e != nil {
		t.Fatal(e)
	}
	if got, _ := os.ReadFile(name); string(got) != string(body) {
		t.Fatal("review wrote bytes")
	}
	if e = f.s.QueueEdit(ctx, r.ID, true); e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.ProcessEdit(ctx); e != nil {
		t.Fatal(e)
	}
	if got, _ := os.ReadFile(name); string(got) != string(reference) {
		t.Fatal("alignment not published")
	}
	rollback, e := f.s.ReviewEdit(ctx, 1, EditRequest{Track: Track{"en", "full"}, Action: "rollback", PreviousID: r.ID})
	if e != nil {
		t.Fatal(e)
	}
	if e = f.s.QueueEdit(ctx, rollback.ID, true); e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.ProcessEdit(ctx); e != nil {
		t.Fatal(e)
	}
	if got, _ := os.ReadFile(name); string(got) != string(body) {
		t.Fatal("rollback lost original")
	}
	if got, _ := os.ReadFile(f.video); string(got) != string(f.body) {
		t.Fatal("video changed")
	}
}
