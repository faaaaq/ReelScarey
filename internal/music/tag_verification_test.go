package music

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func completeEditTags() EditTags {
	return EditTags{
		Artist: "Album Artist", AlbumArtist: "Release Artist", Album: "Album", Title: "Song",
		Track: 7, TrackTotal: 12, Disc: 2, DiscTotal: 3, Year: 1999,
		MusicBrainzArtistID: testMBID, MusicBrainzAlbumID: testReleaseID,
		MusicBrainzReleaseGroupID: testRGID, MusicBrainzTrackID: testTrackID, MusicBrainzRecordingID: testRecordingID,
	}
}

func completeAudioTags() AudioTags {
	track, trackTotal, disc, discTotal, year := 7, 12, 2, 3, 1999
	return AudioTags{
		Artist: "Album Artist", AlbumArtist: "Release Artist", Album: "Album", Title: "Song",
		Track: &track, TrackTotal: &trackTotal, Disc: &disc, DiscTotal: &discTotal, Year: &year,
		MusicBrainzArtistID: testMBID, MusicBrainzAlbumID: testReleaseID,
		MusicBrainzReleaseGroupID: testRGID, MusicBrainzTrackID: testTrackID, MusicBrainzRecordingID: testRecordingID,
	}
}

func TestNeedsRetagAndVerifyIncludeTotalsAndEveryWrittenField(t *testing.T) {
	tags := completeEditTags()
	actual := completeAudioTags()
	if err := verifyEditTags(actual, tags); err != nil {
		t.Fatal(err)
	}
	withoutTotals := actual
	withoutTotals.Version = audioTagVersion
	withoutTotals.TrackTotal, withoutTotals.DiscTotal = nil, nil
	raw, _ := json.Marshal(withoutTotals)
	if !needsRetag(string(raw), tags) {
		t.Fatal("totals-only correction did not trigger a retag")
	}
	raw, _ = json.Marshal(actual)
	if needsRetag(string(raw), tags) {
		t.Fatal("complete matching tags triggered a no-op retag")
	}
	for name, mutate := range map[string]func(*AudioTags){
		"album artist": func(v *AudioTags) { v.AlbumArtist = "Wrong" },
		"year":         func(v *AudioTags) { *v.Year = 2000 },
		"track total":  func(v *AudioTags) { *v.TrackTotal = 11 },
		"disc total":   func(v *AudioTags) { *v.DiscTotal = 4 },
		"artist id":    func(v *AudioTags) { v.MusicBrainzArtistID = "" },
		"album id":     func(v *AudioTags) { v.MusicBrainzAlbumID = "" },
		"group id":     func(v *AudioTags) { v.MusicBrainzReleaseGroupID = "" },
		"track id":     func(v *AudioTags) { v.MusicBrainzTrackID = "" },
		"recording id": func(v *AudioTags) { v.MusicBrainzRecordingID = "" },
	} {
		t.Run(name, func(t *testing.T) {
			wrong := actual
			mutate(&wrong)
			if err := verifyEditTags(wrong, tags); err == nil {
				t.Fatal("mismatch was not verified")
			}
		})
	}
}

func TestVerifyEditTagsIgnoresLocalCatalogPseudoIdentifiers(t *testing.T) {
	// Real values from a failed live import: a tag-derived local album and
	// track carry pseudo-identifiers that are not MusicBrainz UUIDs.
	tags := completeEditTags()
	tags.MusicBrainzReleaseGroupID = "local-album:8d0d8cc69f10287e4abb0947b615c4a9"
	tags.MusicBrainzTrackID = "local-track:3a056a468ffea46fa29b4784895e8835"
	tags.MusicBrainzAlbumID = ""
	tags.MusicBrainzRecordingID = ""
	actual := completeAudioTags()
	actual.MusicBrainzReleaseGroupID = ""
	actual.MusicBrainzTrackID = ""
	actual.MusicBrainzAlbumID = ""
	actual.MusicBrainzRecordingID = ""
	if err := verifyEditTags(actual, tags); err != nil {
		t.Fatalf("local pseudo-identifiers must not fail verification: %v", err)
	}
	raw, _ := json.Marshal(actual)
	if needsRetag(string(raw), tags) {
		t.Fatal("the absence of local pseudo-identifiers triggered a rewrite")
	}
	// A genuine MusicBrainz identifier is still written and verified.
	actual.MusicBrainzArtistID = ""
	if err := verifyEditTags(actual, tags); err == nil {
		t.Fatal("a genuine MBID mismatch was not verified")
	}
}

func TestWriteTagsSkipsLocalPseudoIdentifiersWebM(t *testing.T) {
	for _, tool := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip(tool + " is not installed")
		}
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "source.webm")
	stage := filepath.Join(dir, "edited.webm")
	cmd := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=1", "-c:a", "libopus", "-y", source)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("could not generate webm: %v (%s)", err, output)
	}
	tags := completeEditTags()
	tags.MusicBrainzReleaseGroupID = "local-album:8d0d8cc69f10287e4abb0947b615c4a9"
	tags.MusicBrainzTrackID = "local-track:3a056a468ffea46fa29b4784895e8835"
	if err := writeTags(context.Background(), source, stage, tags, ".webm"); err != nil {
		t.Fatal(err)
	}
	raw, err := exec.Command("ffprobe", "-v", "error", "-show_entries", "format_tags", "-of", "json", stage).Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "local-") {
		t.Fatalf("local pseudo-identifiers were written as file tags: %s", raw)
	}
	if !strings.Contains(strings.ToLower(string(raw)), "musicbrainz_artistid") {
		t.Fatalf("the genuine artist MBID was not written: %s", raw)
	}
	actual, err := (FFprobeTagger{}).ProbeAudio(context.Background(), stage)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyEditTags(actual, tags); err != nil {
		t.Fatalf("local pseudo-identifiers must not fail verification: %v", err)
	}
}

func TestWriteTagsRealFormatMatrix(t *testing.T) {
	for _, tool := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip(tool + " is not installed")
		}
	}
	for _, fixture := range []struct {
		ext   string
		codec string
	}{
		{".mp3", "libmp3lame"},
		{".flac", "flac"},
		{".m4a", "aac"},
		{".opus", "libopus"},
	} {
		t.Run(fixture.ext, func(t *testing.T) {
			dir := t.TempDir()
			source := filepath.Join(dir, "source"+fixture.ext)
			stage := filepath.Join(dir, "edited"+fixture.ext)
			cmd := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=1", "-c:a", fixture.codec, "-y", source)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Skipf("could not generate %s: %v (%s)", fixture.ext, err, output)
			}
			before, err := os.ReadFile(source)
			if err != nil {
				t.Fatal(err)
			}
			if err = writeTags(context.Background(), source, stage, completeEditTags(), fixture.ext); err != nil {
				t.Fatal(err)
			}
			actual, err := (FFprobeTagger{}).ProbeAudio(context.Background(), stage)
			if err != nil {
				t.Fatal(err)
			}
			if err = verifyEditTags(actual, completeEditTags()); err != nil {
				t.Fatalf("%s verification failed: %v; tags=%#v", fixture.ext, err, actual)
			}
			after, err := os.ReadFile(source)
			if err != nil || string(after) != string(before) {
				t.Fatalf("source changed: %v", err)
			}
		})
	}
}
