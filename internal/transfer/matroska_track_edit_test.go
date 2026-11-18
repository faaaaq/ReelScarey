package transfer

import "testing"

func editText(value string) *string { return &value }
func editBool(value bool) *bool     { return &value }

func editableTrack(id int, kind, language, name string, defaultFlag, forcedFlag bool) mkvTrackInfo {
	enabled := true
	return mkvTrackInfo{ID: id, Type: kind, Props: mkvTrackProps{Enabled: &enabled, Language: language, Name: name, Default: &defaultFlag, Forced: &forcedFlag}}
}

func TestMatroskaTrackEditValidation(t *testing.T) {
	for _, edits := range [][]MatroskaTrackEdit{
		{},
		{{Index: 1}},
		{{Index: 1, Language: editText("not a language")}},
		{{Index: 1, Title: editText("bad\nname")}},
		{{Index: 1, Title: editText("one")}, {Index: 1, Default: editBool(true)}},
	} {
		if err := validateMatroskaTrackEdits(edits); problemCode(err) != "invalid_selection" {
			t.Fatalf("accepted invalid edits %#v: %v", edits, err)
		}
	}
	if err := validateMatroskaTrackEdits([]MatroskaTrackEdit{{Index: 1, Title: editText(""), Language: editText("en-US"), Default: editBool(false), Forced: editBool(true)}}); err != nil {
		t.Fatal(err)
	}
}

func TestMatroskaTrackEditSnapshotAndExactVerification(t *testing.T) {
	before := mkvIdentify{Tracks: []mkvTrackInfo{
		editableTrack(0, "video", "und", "", true, false),
		editableTrack(1, "audio", "eng", "English", true, false),
		editableTrack(2, "subtitles", "jpn", "Signs", false, true),
	}}
	probe := Probe{
		Audio:     []AudioStream{{Index: 1, Codec: "aac", Language: "eng", Title: "English", Default: true}},
		Subtitles: []SubtitleStream{{Index: 2, Language: "jpn", Title: "Signs", Forced: true}},
		Codec:     "h264",
		Width:     1920,
		Height:    1080,
	}
	targets := []mkvTarget{{Kind: "audio", ID: 1, Ordinal: 1}}
	edits := []MatroskaTrackEdit{{Index: 1, Title: editText("Commentary"), Language: editText("en-US"), Default: editBool(false), Forced: editBool(true)}}
	if err := validateMatroskaEditSnapshot(probe, before, targets, edits); err != nil {
		t.Fatal(err)
	}
	after := mkvIdentify{Tracks: []mkvTrackInfo{
		editableTrack(0, "video", "und", "", true, false),
		editableTrack(1, "audio", "en-US", "Commentary", false, true),
		editableTrack(2, "subtitles", "jpn", "Signs", false, true),
	}}
	if err := verifyMatroskaTrackEdits(before, after, targets, edits); err != nil {
		t.Fatalf("exact edit rejected: %v", err)
	}
	expected := applyProbeTrackEdits(probe, edits)
	actual := expected
	actual.Audio = append([]AudioStream(nil), expected.Audio...)
	actual.Subtitles = append([]SubtitleStream(nil), expected.Subtitles...)
	if err := verifyUnchangedStreams(expected, actual); err != nil {
		t.Fatal(err)
	}
	if actual.Audio[0].Title != "Commentary" || actual.Audio[0].Language != "en-US" || actual.Audio[0].Default || !actual.Audio[0].Forced {
		t.Fatalf("probe edit not applied: %#v", actual.Audio[0])
	}

	stale := before
	stale.Tracks = append([]mkvTrackInfo(nil), before.Tracks...)
	stale.Tracks[1].Props.Language = "spa"
	if problemCode(validateMatroskaEditSnapshot(probe, stale, targets, edits)) != "source_changed" {
		t.Fatal("stale track snapshot accepted")
	}
	wrong := after
	wrong.Tracks = append([]mkvTrackInfo(nil), after.Tracks...)
	wrong.Tracks[2].Props.Name = "Unexpected"
	if problemCode(verifyMatroskaTrackEdits(before, wrong, targets, edits)) != "metadata_changed_streams" {
		t.Fatal("unrelated subtitle title change accepted")
	}
	wrong = after
	wrong.Tracks = append([]mkvTrackInfo(nil), after.Tracks...)
	wrong.Tracks[1].Props.Default = editBool(true)
	if problemCode(verifyMatroskaTrackEdits(before, wrong, targets, edits)) != "metadata_changed_streams" {
		t.Fatal("incorrect default flag accepted")
	}
	actual.Audio[0].Codec = "flac"
	if problemCode(verifyUnchangedStreams(expected, actual)) != "metadata_changed_streams" {
		t.Fatal("codec change accepted")
	}
}
