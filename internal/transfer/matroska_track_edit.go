package transfer

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// MatroskaTrackEdit addresses one FFprobe stream. Nil fields are preserved.
// Empty titles are valid and clear the track name; languages use BCP 47 or und.
type MatroskaTrackEdit struct {
	Index    int     `json:"index"`
	Title    *string `json:"title,omitempty"`
	Language *string `json:"language,omitempty"`
	Default  *bool   `json:"default,omitempty"`
	Forced   *bool   `json:"forced,omitempty"`
}

var matroskaLanguage = regexp.MustCompile(`(?i)^(?:und|[a-z]{2,3}(?:-[a-z0-9]{2,8})*)$`)

func mkvFlag(value bool) string {
	if value {
		return "1"
	}
	return "0"
}

func validateMatroskaTrackEdits(edits []MatroskaTrackEdit) error {
	if len(edits) == 0 || len(edits) > 200 {
		return problem("invalid_selection", "Choose between 1 and 200 audio or subtitle track edits.")
	}
	seen := map[int]bool{}
	for _, edit := range edits {
		if edit.Index < 0 || seen[edit.Index] || edit.Title == nil && edit.Language == nil && edit.Default == nil && edit.Forced == nil {
			return problem("invalid_selection", "Each selected track needs at least one changed field.")
		}
		seen[edit.Index] = true
		if edit.Title != nil && (len(*edit.Title) > 1024 || strings.ContainsAny(*edit.Title, "\x00\r\n")) {
			return problem("invalid_selection", "Track titles must be at most 1024 bytes without control characters.")
		}
		if edit.Language != nil && !matroskaLanguage.MatchString(*edit.Language) {
			return problem("invalid_selection", "Use a BCP 47 language code such as eng, jpn, en-US, or und for an unknown language.")
		}
	}
	return nil
}

func mkvTrackByTarget(ident mkvIdentify, target mkvTarget) (mkvTrackInfo, bool) {
	ordinal := 0
	for _, track := range ident.Tracks {
		if track.Type != target.Kind {
			continue
		}
		ordinal++
		if ordinal == target.Ordinal {
			return track, true
		}
	}
	return mkvTrackInfo{}, false
}

func probeLanguage(value string) string {
	if strings.TrimSpace(value) == "" {
		return "und"
	}
	return value
}

// validateMatroskaEditSnapshot fences the review against both the recorded
// FFprobe facts and the current MKVToolNix header. Older probe rows without
// stream indexes must be inspected again before an edit is allowed.
func validateMatroskaEditSnapshot(probe Probe, before mkvIdentify, targets []mkvTarget, edits []MatroskaTrackEdit) error {
	for i, edit := range edits {
		track, ok := mkvTrackByTarget(before, targets[i])
		if !ok {
			return problem("track_ambiguous", "The reviewed Matroska track is no longer present.")
		}
		var language, title string
		var defaultFlag, forcedFlag bool
		found := false
		if targets[i].Kind == "audio" {
			for _, stream := range probe.Audio {
				if stream.Index == edit.Index {
					language, title, defaultFlag, forcedFlag = stream.Language, stream.Title, stream.Default, stream.Forced
					found = true
					break
				}
			}
		} else {
			for _, stream := range probe.Subtitles {
				if stream.Index == edit.Index {
					language, title, defaultFlag, forcedFlag = stream.Language, stream.Title, stream.Default, stream.Forced
					found = true
					break
				}
			}
		}
		if !found || !strings.EqualFold(probeLanguage(language), probeLanguage(track.Props.Language)) || title != track.Props.Name || defaultFlag != defaultOf(track) || forcedFlag != forcedOf(track) {
			return problem("source_changed", "The track metadata changed since inspection. Inspect embedded tags again and review a new edit.")
		}
	}
	return nil
}

func verifyMatroskaTrackEdits(before, after mkvIdentify, targets []mkvTarget, edits []MatroskaTrackEdit) error {
	if len(before.Tracks) != len(after.Tracks) || len(targets) != len(edits) {
		return problem("metadata_changed_streams", "The track edit changed the track list; the original header was restored.")
	}
	byKey := map[string]MatroskaTrackEdit{}
	counts := map[string]int{}
	for i, target := range targets {
		counts[target.Kind]++
		byKey[target.Kind+":"+strconv.Itoa(counts[target.Kind])] = edits[i]
	}
	counts = map[string]int{}
	for i, track := range after.Tracks {
		previous := before.Tracks[i]
		if previous.ID != track.ID || previous.Type != track.Type {
			return problem("metadata_changed_streams", "The track edit changed track identities; the original header was restored.")
		}
		counts[track.Type]++
		edit, targeted := byKey[track.Type+":"+strconv.Itoa(counts[track.Type])]
		expectedLanguage, expectedName := previous.Props.Language, previous.Props.Name
		expectedDefault, expectedForced := defaultOf(previous), forcedOf(previous)
		if targeted {
			if edit.Language != nil {
				expectedLanguage = *edit.Language
			}
			if edit.Title != nil {
				expectedName = *edit.Title
			}
			if edit.Default != nil {
				expectedDefault = *edit.Default
			}
			if edit.Forced != nil {
				expectedForced = *edit.Forced
			}
		}
		if enabledOf(previous) != enabledOf(track) || !strings.EqualFold(expectedLanguage, track.Props.Language) || expectedName != track.Props.Name || expectedDefault != defaultOf(track) || expectedForced != forcedOf(track) {
			return problem("metadata_changed_streams", "The track fields did not match the reviewed edit; the original header was restored.")
		}
	}
	return nil
}

func applyProbeTrackEdits(probe Probe, edits []MatroskaTrackEdit) Probe {
	for _, edit := range edits {
		for i := range probe.Audio {
			if probe.Audio[i].Index != edit.Index {
				continue
			}
			if edit.Title != nil {
				probe.Audio[i].Title = *edit.Title
			}
			if edit.Language != nil {
				probe.Audio[i].Language = *edit.Language
			}
			if edit.Default != nil {
				probe.Audio[i].Default = *edit.Default
			}
			if edit.Forced != nil {
				probe.Audio[i].Forced = *edit.Forced
			}
		}
		for i := range probe.Subtitles {
			if probe.Subtitles[i].Index != edit.Index {
				continue
			}
			if edit.Title != nil {
				probe.Subtitles[i].Title = *edit.Title
			}
			if edit.Language != nil {
				probe.Subtitles[i].Language = *edit.Language
			}
			if edit.Default != nil {
				probe.Subtitles[i].Default = *edit.Default
			}
			if edit.Forced != nil {
				probe.Subtitles[i].Forced = *edit.Forced
			}
		}
	}
	return probe
}

// EditMatroskaTracksInPlaceVerified changes only reviewed audio/subtitle
// header fields. The caller's organization journal owns header restoration if
// this function or a later verification fails.
func EditMatroskaTracksInPlaceVerified(ctx context.Context, path, beforeProbe string, edits []MatroskaTrackEdit, beforeSize int64) (string, error) {
	if err := validateMatroskaTrackEdits(edits); err != nil {
		return "", err
	}
	var probe Probe
	if err := json.Unmarshal([]byte(beforeProbe), &probe); err != nil {
		return "", problem("invalid_media", "The recorded media information could not be read.")
	}
	before, err := matroskaIdentify(ctx, path)
	if err != nil {
		return "", err
	}
	source, err := os.Open(path)
	if err != nil {
		return "", err
	}
	manifest, err := inspectManifest(ctx, source)
	source.Close()
	if err != nil {
		return "", err
	}
	indexes := make([]int, 0, len(edits))
	for _, edit := range edits {
		indexes = append(indexes, edit.Index)
	}
	targets, err := mapMatroskaTargets(probeTracks(manifest), before, indexes)
	if err != nil {
		return "", err
	}
	if err = validateMatroskaEditSnapshot(probe, before, targets, edits); err != nil {
		return "", err
	}
	args := []string{path}
	for i, target := range targets {
		selector := "track:a"
		if target.Kind == "subtitles" {
			selector = "track:s"
		}
		args = append(args, "--edit", selector+strconv.Itoa(target.Ordinal))
		edit := edits[i]
		if edit.Title != nil {
			args = append(args, "--set", "name="+*edit.Title)
		}
		if edit.Language != nil {
			args = append(args, "--set", "language="+*edit.Language)
		}
		if edit.Default != nil {
			args = append(args, "--set", "flag-default="+mkvFlag(*edit.Default))
		}
		if edit.Forced != nil {
			args = append(args, "--set", "flag-forced="+mkvFlag(*edit.Forced))
		}
	}
	cmd := exec.CommandContext(ctx, trackTool("REELSCAREY_MKVPROPEDIT_PATH", "mkvpropedit"), args...)
	hideProbeWindow(cmd)
	var stderr limitedBuffer
	stderr.max = 16 << 10
	cmd.Stderr = &stderr
	if err = cmd.Run(); err != nil {
		return "", problem("metadata_unsupported", "mkvpropedit could not edit these track fields in place. The original header was restored.")
	}
	after, err := matroskaIdentify(ctx, path)
	if err != nil {
		return "", err
	}
	if err = verifyMatroskaTrackEdits(before, after, targets, edits); err != nil {
		return "", err
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	actual, err := (FFprobe{}).Probe(ctx, file)
	if err != nil {
		return "", err
	}
	if err = verifyUnchangedStreams(applyProbeTrackEdits(probe, edits), actual); err != nil {
		return "", err
	}
	if !equalTitle(probe.Title, actual.Title) {
		return "", problem("metadata_changed_tags", "The track edit changed the container title. The original header was restored.")
	}
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	if math.Abs(float64(info.Size()-beforeSize)) > float64(8<<20) {
		return "", problem("metadata_changed_streams", "The track edit changed the file size unexpectedly. The original header was restored.")
	}
	data, err := json.Marshal(actual)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
