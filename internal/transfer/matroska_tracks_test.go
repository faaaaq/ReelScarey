package transfer

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func track(id int, kind, language string, enabled bool) mkvTrackInfo {
	return mkvTrackInfo{ID: id, Type: kind, Props: mkvTrackProps{Language: language, Enabled: &enabled}}
}

func TestMatroskaKindNormalizesSubtitle(t *testing.T) {
	if matroskaKind("subtitle") != "subtitles" {
		t.Fatal(matroskaKind("subtitle"))
	}
	for _, in := range []string{"video", "audio", "attachment"} {
		if matroskaKind(in) != in {
			t.Fatalf("%s changed", in)
		}
	}
}

func TestMapMatroskaTargetsUsesTypeAndPosition(t *testing.T) {
	// A file with an attachment between audio and subtitles: ffprobe indexes
	// include it, mkvmerge track ids do not, so a positional id mapping would
	// be wrong. Type+ordinal mapping must still pick the right tracks.
	streams := []probeTrack{
		{Index: 0, Kind: "video"},
		{Index: 1, Kind: "audio", Language: "eng"},
		{Index: 2, Kind: "audio", Language: "jpn"},
		{Index: 3, Kind: "attachment"},
		{Index: 4, Kind: "subtitles", Language: "eng"},
		{Index: 5, Kind: "subtitles", Language: "spa"},
	}
	ident := mkvIdentify{Tracks: []mkvTrackInfo{
		track(0, "video", "und", true),
		track(1, "audio", "eng", true),
		track(2, "audio", "jpn", true),
		track(3, "subtitles", "eng", true),
		track(4, "subtitles", "spa", true),
	}}
	// Disable ffprobe indexes 2 (2nd audio) and 5 (2nd subtitle).
	got, err := mapMatroskaTargets(streams, ident, []int{2, 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("targets: %+v", got)
	}
	if got[0].Kind != "audio" || got[0].Ordinal != 2 || got[0].ID != 2 {
		t.Fatalf("audio target: %+v", got[0])
	}
	if got[1].Kind != "subtitles" || got[1].Ordinal != 2 || got[1].ID != 4 {
		t.Fatalf("subtitle target: %+v", got[1])
	}
}

func TestMapMatroskaTargetsRefusesMismatchedCounts(t *testing.T) {
	streams := []probeTrack{{Index: 0, Kind: "video"}, {Index: 1, Kind: "audio"}}
	ident := mkvIdentify{Tracks: []mkvTrackInfo{track(0, "video", "und", true)}}
	if _, err := mapMatroskaTargets(streams, ident, []int{1}); problemCode(err) != "track_ambiguous" {
		t.Fatalf("expected track_ambiguous, got %v", err)
	}
}

func TestMapMatroskaTargetsRejectsNonAudioOrSubtitle(t *testing.T) {
	streams := []probeTrack{{Index: 0, Kind: "video"}, {Index: 1, Kind: "audio"}}
	ident := mkvIdentify{Tracks: []mkvTrackInfo{track(0, "video", "und", true), track(1, "audio", "eng", true)}}
	if _, err := mapMatroskaTargets(streams, ident, []int{0}); problemCode(err) != "invalid_selection" {
		t.Fatalf("expected invalid_selection, got %v", err)
	}
}

func TestVerifyDisabledRequiresExactlyTheReviewedFlags(t *testing.T) {
	before := mkvIdentify{Tracks: []mkvTrackInfo{
		track(0, "video", "und", true),
		track(1, "audio", "eng", true),
		track(2, "audio", "jpn", true),
		track(3, "subtitles", "eng", true),
		track(4, "subtitles", "jpn", true),
	}}
	targets := []mkvTarget{{Kind: "audio", ID: 2, Ordinal: 2}, {Kind: "subtitles", ID: 4, Ordinal: 2}}

	good := mkvIdentify{Tracks: []mkvTrackInfo{
		track(0, "video", "und", true),
		track(1, "audio", "eng", true),
		track(2, "audio", "jpn", false),
		track(3, "subtitles", "eng", true),
		track(4, "subtitles", "jpn", false),
	}}
	if err := verifyDisabled(before, good, targets); err != nil {
		t.Fatalf("correct disable rejected: %v", err)
	}

	// Wrong track disabled.
	wrongTrack := mkvIdentify{Tracks: []mkvTrackInfo{
		track(0, "video", "und", true),
		track(1, "audio", "eng", true),
		track(2, "audio", "jpn", true),
		track(3, "subtitles", "eng", false),
		track(4, "subtitles", "jpn", true),
	}}
	if problemCode(verifyDisabled(before, wrongTrack, targets)) != "metadata_changed_streams" {
		t.Fatal("wrong track disable accepted")
	}

	// Extra track disabled.
	extra := mkvIdentify{Tracks: []mkvTrackInfo{
		track(0, "video", "und", true),
		track(1, "audio", "eng", false),
		track(2, "audio", "jpn", false),
		track(3, "subtitles", "eng", true),
		track(4, "subtitles", "jpn", false),
	}}
	if problemCode(verifyDisabled(before, extra, targets)) != "metadata_changed_streams" {
		t.Fatal("extra disable accepted")
	}

	// A changed language (a different edit) is rejected.
	relang := mkvIdentify{Tracks: []mkvTrackInfo{
		track(0, "video", "und", true),
		track(1, "audio", "eng", true),
		track(2, "audio", "spa", false),
		track(3, "subtitles", "eng", true),
		track(4, "subtitles", "jpn", false),
	}}
	if problemCode(verifyDisabled(before, relang, targets)) != "metadata_changed_streams" {
		t.Fatal("language change accepted")
	}

	// A removed track is rejected.
	missing := mkvIdentify{Tracks: []mkvTrackInfo{
		track(0, "video", "und", true),
		track(1, "audio", "eng", true),
		track(3, "subtitles", "eng", true),
		track(4, "subtitles", "jpn", false),
	}}
	if problemCode(verifyDisabled(before, missing, targets)) != "metadata_changed_streams" {
		t.Fatal("track removal accepted")
	}
}

func TestProbeTracksDoesNotMapCoverArtAsVideo(t *testing.T) {
	var manifest tagManifest
	if err := json.Unmarshal([]byte(`{"streams":[{"index":0,"codec_type":"video"},{"index":1,"codec_type":"subtitle"},{"index":2,"codec_type":"video","disposition":{"attached_pic":1}}]}`), &manifest); err != nil {
		t.Fatal(err)
	}
	streams := probeTracks(manifest)
	ident := mkvIdentify{Tracks: []mkvTrackInfo{track(0, "video", "und", true), track(1, "subtitles", "eng", true)}}
	if _, err := mapMatroskaTargets(streams, ident, []int{1}); err != nil {
		t.Fatal(err)
	}
}
func TestVerifyDisabledPreservesAlreadyDisabledUnselectedTrack(t *testing.T) {
	before := mkvIdentify{Tracks: []mkvTrackInfo{track(0, "video", "und", true), track(1, "subtitles", "eng", false), track(2, "subtitles", "spa", true)}}
	after := mkvIdentify{Tracks: []mkvTrackInfo{track(0, "video", "und", true), track(1, "subtitles", "eng", false), track(2, "subtitles", "spa", false)}}
	if err := verifyDisabled(before, after, []mkvTarget{{Kind: "subtitles", Ordinal: 2}}); err != nil {
		t.Fatal(err)
	}
}
func TestMatroskaCommandForcesUTF8Locale(t *testing.T) {
	t.Setenv("LC_ALL", "C")
	cmd := matroskaCommand(context.Background(), "unused")
	last := ""
	for _, env := range cmd.Env {
		if strings.HasPrefix(env, "LC_ALL=") {
			last = env
		}
	}
	if last != "LC_ALL=C.UTF-8" {
		t.Fatal(cmd.Env)
	}
}
