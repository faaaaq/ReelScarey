package transfer

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// Matroska track handling on top of mkvtoolnix. mkvpropedit cannot add or
// remove tracks, but it flips per-track header flags in place (seconds, no
// packet rewrite), which is how an unwanted language is hidden from players
// without deleting anything. mkvmerge is used only to identify Matroska tracks
// and to verify the flag change, because its JSON track list is the only place
// the Matroska "enabled" flag is visible (ffprobe does not expose it).

// MatroskaTrackToolsAvailable reports whether mkvmerge can identify Matroska
// tracks.
func MatroskaTrackToolsAvailable() bool {
	_, e := exec.LookPath(trackTool("REELSCAREY_MKVMERGE_PATH", "mkvmerge"))
	return e == nil
}

// MatroskaDisableToolsAvailable reports whether tracks can be disabled in
// place: mkvpropedit performs the edit and mkvmerge verifies the flag change.
func MatroskaDisableToolsAvailable() bool {
	if !MatroskaTrackToolsAvailable() {
		return false
	}
	_, e := exec.LookPath(trackTool("REELSCAREY_MKVPROPEDIT_PATH", "mkvpropedit"))
	return e == nil
}

type mkvTrackProps struct {
	Enabled  *bool  `json:"enabled_track"`
	Language string `json:"language"`
	Name     string `json:"track_name"`
	Default  *bool  `json:"default_track"`
	Forced   *bool  `json:"forced_track"`
}

type mkvTrackInfo struct {
	ID    int           `json:"id"`
	Type  string        `json:"type"`
	Props mkvTrackProps `json:"properties"`
}

type mkvIdentify struct {
	Tracks []mkvTrackInfo `json:"tracks"`
}

// matroskaIdentify reads mkvmerge's JSON track list, which is the only place
// the Matroska "track enabled" flag is visible.
func matroskaIdentify(ctx context.Context, path string) (mkvIdentify, error) {
	var out mkvIdentify
	cmd := exec.CommandContext(ctx, trackTool("REELSCAREY_MKVMERGE_PATH", "mkvmerge"), "-J", path)
	hideProbeWindow(cmd)
	var stdout, stderr limitedBuffer
	stdout.max = 1 << 20
	stderr.max = 16 << 10
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if e := cmd.Run(); e != nil {
		return out, problem("invalid_media", "This Matroska file's track list could not be read.")
	}
	raw := bytes.TrimSpace(stdout.Bytes())
	if i := bytes.IndexByte(raw, '{'); i > 0 {
		raw = raw[i:]
	}
	if e := json.Unmarshal(raw, &out); e != nil {
		return out, problem("invalid_media", "This Matroska file's track list could not be read.")
	}
	return out, nil
}

type probeTrack struct {
	Index    int
	Kind     string // video, audio, subtitles, attachment
	Language string
}

// matroskaKind normalizes ffprobe's singular "subtitle" to mkvmerge's
// "subtitles" so the two tool outputs can be compared directly.
func matroskaKind(ffprobeType string) string {
	if ffprobeType == "subtitle" {
		return "subtitles"
	}
	return ffprobeType
}

func probeTracks(m tagManifest) []probeTrack {
	out := make([]probeTrack, 0, len(m.Streams))
	for _, s := range m.Streams {
		out = append(out, probeTrack{Index: s.Index, Kind: matroskaKind(s.Type), Language: s.Tags["language"]})
	}
	return out
}

// mkvTarget is one Matroska track to change, addressed within its own type.
type mkvTarget struct {
	Kind    string // audio or subtitles
	ID      int    // mkvmerge global track id
	Ordinal int    // 1-based position within the type (used by mkvpropedit)
}

// mapMatroskaTargets maps reviewed ffprobe stream indexes to Matroska tracks by
// type and position. Both tools enumerate tracks in file order, so the per-type
// counts must agree; a mismatch is refused rather than edited blindly. The
// post-edit verification still proves the intended tracks changed.
func mapMatroskaTargets(streams []probeTrack, ident mkvIdentify, drop []int) ([]mkvTarget, error) {
	probeCounts := map[string]int{}
	probeKind := map[int]string{}
	probeOrdinal := map[int]int{}
	for _, s := range streams {
		if s.Kind != "video" && s.Kind != "audio" && s.Kind != "subtitles" {
			continue
		}
		probeCounts[s.Kind]++
		probeKind[s.Index] = s.Kind
		probeOrdinal[s.Index] = probeCounts[s.Kind]
	}
	identCounts := map[string]int{}
	byKey := map[string]mkvTrackInfo{}
	for _, t := range ident.Tracks {
		if t.Type != "video" && t.Type != "audio" && t.Type != "subtitles" {
			continue
		}
		identCounts[t.Type]++
		byKey[t.Type+":"+strconv.Itoa(identCounts[t.Type])] = t
	}
	for _, kind := range []string{"video", "audio", "subtitles"} {
		if probeCounts[kind] != identCounts[kind] {
			return nil, problem("track_ambiguous", "This Matroska file's tracks could not be mapped safely; the file was left unchanged.")
		}
	}
	out := make([]mkvTarget, 0, len(drop))
	for _, index := range drop {
		kind := probeKind[index]
		if kind != "audio" && kind != "subtitles" {
			return nil, problem("invalid_selection", "Only audio and subtitle tracks can be changed.")
		}
		ordinal := probeOrdinal[index]
		track, ok := byKey[kind+":"+strconv.Itoa(ordinal)]
		if !ok {
			return nil, problem("track_ambiguous", "This Matroska file's tracks could not be mapped safely; the file was left unchanged.")
		}
		out = append(out, mkvTarget{Kind: kind, ID: track.ID, Ordinal: ordinal})
	}
	return out, nil
}

func enabledOf(t mkvTrackInfo) bool {
	return t.Props.Enabled == nil || *t.Props.Enabled
}

func defaultOf(t mkvTrackInfo) bool { return t.Props.Default == nil || *t.Props.Default }
func forcedOf(t mkvTrackInfo) bool  { return t.Props.Forced != nil && *t.Props.Forced }
func sameMkvFlag(a, b *bool) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

// verifyDisabled proves an in-place flag edit changed exactly the reviewed
// tracks and nothing else: the track list, ids, order, types and languages are
// unchanged, and the enabled flag flipped only for the targets.
func verifyDisabled(before, after mkvIdentify, targets []mkvTarget) error {
	if len(before.Tracks) != len(after.Tracks) {
		return problem("metadata_changed_streams", "Disabling tracks changed the track list; the file was left unchanged.")
	}
	want := map[string]bool{}
	for _, t := range targets {
		want[t.Kind+":"+strconv.Itoa(t.Ordinal)] = true
	}
	counts := map[string]int{}
	for i, t := range after.Tracks {
		b := before.Tracks[i]
		if b.ID != t.ID || b.Type != t.Type || !strings.EqualFold(b.Props.Language, t.Props.Language) || b.Props.Name != t.Props.Name || !sameMkvFlag(b.Props.Default, t.Props.Default) || !sameMkvFlag(b.Props.Forced, t.Props.Forced) {
			return problem("metadata_changed_streams", "Disabling tracks changed the track list; the file was left unchanged.")
		}
		counts[t.Type]++
		enabled := enabledOf(t)
		if t.Type == "audio" || t.Type == "subtitles" {
			if enabled != !want[t.Type+":"+strconv.Itoa(counts[t.Type])] {
				return problem("metadata_changed_streams", "The disabled tracks did not match the reviewed selection; the file was left unchanged.")
			}
			continue
		}
		if enabled != enabledOf(b) {
			return problem("metadata_changed_streams", "Disabling tracks changed an unrelated track; the file was left unchanged.")
		}
	}
	return nil
}

// DisableTracksMatroskaInPlaceVerified hides the reviewed tracks by clearing
// their Matroska enabled flag with mkvpropedit. Only the container header
// changes: no packet is read or rewritten, so it takes seconds. The edit is
// verified against mkvmerge's track list and the recorded ffprobe facts, and a
// bounded size change is required. It returns the (unchanged) probe JSON.
func DisableTracksMatroskaInPlaceVerified(ctx context.Context, path, beforeProbe string, drop []int, beforeSize int64) (string, error) {
	if len(drop) == 0 {
		return "", problem("invalid_selection", "Choose tracks to disable.")
	}
	var probe Probe
	if e := json.Unmarshal([]byte(beforeProbe), &probe); e != nil {
		return "", problem("invalid_media", "The recorded media information could not be read.")
	}
	beforeIdent, e := matroskaIdentify(ctx, path)
	if e != nil {
		return "", e
	}
	source, e := os.Open(path)
	if e != nil {
		return "", e
	}
	manifest, e := inspectManifest(ctx, source)
	source.Close()
	if e != nil {
		return "", e
	}
	targets, e := mapMatroskaTargets(probeTracks(manifest), beforeIdent, drop)
	if e != nil {
		return "", e
	}
	args := []string{path}
	for _, t := range targets {
		selector := "track:a"
		if t.Kind == "subtitles" {
			selector = "track:s"
		}
		args = append(args, "--edit", selector+strconv.Itoa(t.Ordinal), "--set", "flag-enabled=0")
	}
	cmd := exec.CommandContext(ctx, trackTool("REELSCAREY_MKVPROPEDIT_PATH", "mkvpropedit"), args...)
	hideProbeWindow(cmd)
	var stderr limitedBuffer
	stderr.max = 16 << 10
	cmd.Stderr = &stderr
	if e := cmd.Run(); e != nil {
		return "", problem("metadata_unsupported", "mkvpropedit could not disable these tracks in place. The original header was restored.")
	}
	afterIdent, e := matroskaIdentify(ctx, path)
	if e != nil {
		return "", e
	}
	if e := verifyDisabled(beforeIdent, afterIdent, targets); e != nil {
		return "", e
	}
	f, e := os.Open(path)
	if e != nil {
		return "", e
	}
	defer f.Close()
	after, e := (FFprobe{}).Probe(ctx, f)
	if e != nil {
		return "", e
	}
	if e := verifyUnchangedStreams(probe, after); e != nil {
		return "", e
	}
	if !equalTitle(probe.Title, after.Title) {
		return "", problem("metadata_changed_tags", "Disabling tracks changed the container title. The original header was restored.")
	}
	info, e := f.Stat()
	if e != nil {
		return "", e
	}
	if math.Abs(float64(info.Size()-beforeSize)) > float64(8<<20) {
		return "", problem("metadata_changed_streams", "Disabling tracks changed the file size unexpectedly. The original header was restored.")
	}
	data, e := json.Marshal(after)
	if e != nil {
		return "", e
	}
	return string(data), nil
}

func equalTitle(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
