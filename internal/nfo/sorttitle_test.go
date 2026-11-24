package nfo

import (
	"errors"
	"strings"
	"testing"
)

func TestRenderEpisodeWritesBlankSortTitle(t *testing.T) {
	raw, err := RenderEpisode(Episode{Title: "Pilot", Season: 1, Episode: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "<sorttitle></sorttitle>\n") {
		t.Fatalf("episode NFO does not explicitly blank its sort title: %s", raw)
	}
}

func TestClearEpisodeSortTitlePreservesOtherMetadata(t *testing.T) {
	input := []byte(`<?xml version="1.0"?>
<!-- <sorttitle>leave comments alone</sorttitle> -->
<episodedetails>
  <title>Episode &amp; title</title>
  <sorttitle>0002 - provider order</sorttitle>
  <plot>Literal &lt;sorttitle&gt;text&lt;/sorttitle&gt; stays here.</plot>
  <season>1</season>
</episodedetails>
`)
	got, changed, err := ClearEpisodeSortTitle(input)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("non-empty sort title was not changed")
	}
	want := strings.Replace(string(input), "<sorttitle>0002 - provider order</sorttitle>", blankSortTitle, 1)
	if string(got) != want {
		t.Fatalf("cleanup changed unrelated bytes:\n got: %s\nwant: %s", got, want)
	}
	again, changed, err := ClearEpisodeSortTitle(got)
	if err != nil || changed || string(again) != string(got) {
		t.Fatalf("second cleanup = (changed %v, err %v)", changed, err)
	}
}

func TestClearEpisodeSortTitleHandlesSelfClosingNestedAndMultipleFields(t *testing.T) {
	input := []byte(`<episodedetails><sorttitle/><extension><sorttitle>nested stays</sorttitle></extension><sorttitle> first </sorttitle><sorttitle>   </sorttitle><sorttitle><value>nested is not blank</value></sorttitle></episodedetails>`)
	got, changed, err := ClearEpisodeSortTitle(input)
	if err != nil || !changed {
		t.Fatalf("cleanup = (changed %v, err %v)", changed, err)
	}
	text := string(got)
	if strings.Count(text, blankSortTitle) != 2 {
		t.Fatalf("non-empty direct fields were not blanked: %s", text)
	}
	if !strings.Contains(text, "<sorttitle/>") || !strings.Contains(text, "<sorttitle>   </sorttitle>") {
		t.Fatalf("already blank fields were needlessly rewritten: %s", text)
	}
	if !strings.Contains(text, "<extension><sorttitle>nested stays</sorttitle></extension>") {
		t.Fatalf("nested metadata changed: %s", text)
	}
}

func TestClearEpisodeSortTitleLeavesMissingAndBlankValuesUntouched(t *testing.T) {
	for _, input := range []string{
		`<episodedetails><title>No sort title</title></episodedetails>`,
		`<episodedetails><sorttitle/><title>Blank</title></episodedetails>`,
		`<episodedetails><sorttitle> </sorttitle><title>Blank</title></episodedetails>`,
	} {
		got, changed, err := ClearEpisodeSortTitle([]byte(input))
		if err != nil || changed || string(got) != input {
			t.Fatalf("input %q = (%q, %v, %v)", input, got, changed, err)
		}
	}
}

func TestClearEpisodeSortTitleRejectsUnsafeDocuments(t *testing.T) {
	for _, test := range []struct {
		name  string
		input string
		want  error
	}{
		{"movie", `<movie><sorttitle>movie stays</sorttitle></movie>`, ErrNotEpisode},
		{"series", `<tvshow><sorttitle>series stays</sorttitle></tvshow>`, ErrNotEpisode},
		{"malformed", `<episodedetails><sorttitle>broken</episodedetails>`, ErrInvalid},
		{"empty", ``, ErrInvalid},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, changed, err := ClearEpisodeSortTitle([]byte(test.input))
			if !errors.Is(err, test.want) || changed || string(got) != test.input {
				t.Fatalf("got (%q, %v, %v), want original, false, %v", got, changed, err, test.want)
			}
		})
	}
}
