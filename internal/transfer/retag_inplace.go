package transfer

import (
	"context"
	"math"
	"os"
	"os/exec"
	"strings"
)

// Matroska containers store the segment title in the header area, and
// mkvpropedit rewrites that element in place: media packets are never read or
// rewritten, so an embedded title edit takes seconds regardless of file size.
// These helpers let the organization fast path use that behavior for .mkv and
// .webm files while every other container keeps the verified remux path.

// MatroskaTitleExtension reports whether a container can take an in-place
// title edit. Only the extensions the embedded-title feature registers.
func MatroskaTitleExtension(extension string) bool {
	switch strings.ToLower(extension) {
	case ".mkv", ".webm":
		return true
	}
	return false
}

// InPlaceTitleToolAvailable reports whether mkvpropedit can be used. The
// Docker image ships it; source installs without mkvtoolnix fall back to the
// verified remux path.
func InPlaceTitleToolAvailable() bool {
	_, err := exec.LookPath(trackTool("REELSCAREY_MKVPROPEDIT_PATH", "mkvpropedit"))
	return err == nil
}

// EditTitleInPlaceVerified rewrites a Matroska file's embedded title in place
// and compares live stream information on either side of the edit. Older
// catalog inspections can lack track indexes that the current FFprobe records.
func EditTitleInPlaceVerified(ctx context.Context, path, title string, beforeSize int64) (Probe, error) {
	if strings.TrimSpace(title) == "" || len(title) > 1024 || strings.ContainsAny(title, "\x00\r\n") {
		return Probe{}, problem("invalid_metadata", "The saved title cannot be written safely.")
	}
	tool := trackTool("REELSCAREY_MKVPROPEDIT_PATH", "mkvpropedit")
	if _, e := exec.LookPath(tool); e != nil {
		return Probe{}, problem("tool_missing", "mkvpropedit is not installed on the server.")
	}
	f, e := os.Open(path)
	if e != nil {
		return Probe{}, e
	}
	defer f.Close()
	before, e := (FFprobe{}).Probe(ctx, f)
	if e != nil {
		return Probe{}, e
	}
	cmd := exec.CommandContext(ctx, tool, path, "--edit", "info", "--set", "title="+title)
	hideProbeWindow(cmd)
	var stderr limitedBuffer
	stderr.max = 16 << 10
	cmd.Stderr = &stderr
	if e := cmd.Run(); e != nil {
		return Probe{}, problem("metadata_unsupported", "mkvpropedit could not update this file's title in place. The original header was restored.")
	}
	after, e := (FFprobe{}).Probe(ctx, f)
	if e != nil {
		return Probe{}, e
	}
	if after.Title == nil || *after.Title != title {
		return Probe{}, problem("metadata_changed_tags", "The in-place title edit did not take effect. The original header was restored.")
	}
	if err := verifyUnchangedStreams(before, after); err != nil {
		return Probe{}, err
	}
	info, e := f.Stat()
	if e != nil {
		return Probe{}, e
	}
	// A header edit moves bytes only inside the header area; anything beyond a
	// small growth means the tool did something unexpected.
	if math.Abs(float64(info.Size()-beforeSize)) > float64(8<<20) {
		return Probe{}, problem("metadata_changed_streams", "The in-place title edit changed the file size unexpectedly. The original header was restored.")
	}
	return after, nil
}

// verifyUnchangedStreams compares the summary facts a title edit must never
// change: the video stream and every audio and subtitle stream identity.
func verifyUnchangedStreams(before, after Probe) error {
	if after.Codec != before.Codec || after.Width != before.Width || after.Height != before.Height || after.HDR != before.HDR {
		return problem("metadata_changed_streams", "The in-place title edit changed the video stream. The original header was restored.")
	}
	if len(after.Audio) != len(before.Audio) || len(after.Subtitles) != len(before.Subtitles) {
		return problem("metadata_changed_streams", "The in-place title edit changed the stream count. The original header was restored.")
	}
	for i := range before.Audio {
		if after.Audio[i] != before.Audio[i] {
			return problem("metadata_changed_streams", "The in-place title edit changed an audio stream. The original header was restored.")
		}
	}
	for i := range before.Subtitles {
		if after.Subtitles[i] != before.Subtitles[i] {
			return problem("metadata_changed_streams", "The in-place title edit changed a subtitle stream. The original header was restored.")
		}
	}
	return nil
}
