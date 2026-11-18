package transfer

import (
	"context"
	"testing"
)

func TestMatroskaTitleExtensionOnlyClaimsMatroskaContainers(t *testing.T) {
	for _, ext := range []string{".mkv", ".webm", ".MKV", ".WebM"} {
		if !MatroskaTitleExtension(ext) {
			t.Fatalf("%q must use the in-place editor", ext)
		}
	}
	for _, ext := range []string{".mp4", ".m4v", ".mov", ".avi", "", ".mkv.part"} {
		if MatroskaTitleExtension(ext) {
			t.Fatalf("%q must keep the verified remux path", ext)
		}
	}
}

func TestEditTitleInPlaceVerifiedRejectsUnsafeInputBeforeRunningATool(t *testing.T) {
	ctx := context.Background()
	// A title the container cannot store safely is refused. The file is never
	// opened, so the check holds even where mkvpropedit is not installed.
	for _, title := range []string{"", "   ", "line\nbreak", "carriage\rreturn", "nul\x00byte"} {
		_, err := EditTitleInPlaceVerified(ctx, "unused.mkv", `{}`, title, 0)
		if code := problemCode(err); code != "invalid_metadata" {
			t.Fatalf("title %q: code=%q err=%v", title, code, err)
		}
	}
	long := make([]byte, 1025)
	for i := range long {
		long[i] = 'a'
	}
	if code := problemCode(mustErr(EditTitleInPlaceVerified(ctx, "unused.mkv", `{}`, string(long), 0))); code != "invalid_metadata" {
		t.Fatalf("over-long title: code=%q", code)
	}
}

func TestEditTitleInPlaceVerifiedRejectsUnreadableProbe(t *testing.T) {
	_, err := EditTitleInPlaceVerified(context.Background(), "unused.mkv", "not-json", "Title", 0)
	if code := problemCode(err); code != "invalid_media" {
		t.Fatalf("code=%q err=%v", code, err)
	}
}

func TestVerifyUnchangedStreamsRejectsAnyStructuralChange(t *testing.T) {
	title := "Old"
	base := Probe{
		Title:     &title,
		Codec:     "h264",
		Width:     1920,
		Height:    1080,
		Audio:     []AudioStream{{Codec: "aac", Language: "eng"}},
		Subtitles: []SubtitleStream{{Language: "eng"}},
	}
	if err := verifyUnchangedStreams(base, base); err != nil {
		t.Fatalf("identical probes rejected: %v", err)
	}
	cases := map[string]Probe{
		"codec":         {Codec: "hevc", Width: 1920, Height: 1080, Audio: base.Audio, Subtitles: base.Subtitles},
		"resolution":    {Codec: "h264", Width: 1280, Height: 720, Audio: base.Audio, Subtitles: base.Subtitles},
		"audio count":   {Codec: "h264", Width: 1920, Height: 1080, Audio: nil, Subtitles: base.Subtitles},
		"audio content": {Codec: "h264", Width: 1920, Height: 1080, Audio: []AudioStream{{Codec: "ac3", Language: "eng"}}, Subtitles: base.Subtitles},
		"subtitles":     {Codec: "h264", Width: 1920, Height: 1080, Audio: base.Audio},
	}
	for name, after := range cases {
		if code := problemCode(verifyUnchangedStreams(base, after)); code != "metadata_changed_streams" {
			t.Fatalf("%s: code=%q err=%v", name, code, verifyUnchangedStreams(base, after))
		}
	}
	// A title change is exactly what this function must allow.
	changed := base
	next := "New"
	changed.Title = &next
	if err := verifyUnchangedStreams(base, changed); err != nil {
		t.Fatalf("title-only change rejected: %v", err)
	}
}

func mustErr(_ Probe, err error) error { return err }
