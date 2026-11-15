package music

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// This file implements the finished-file music export behind
// GET /api/v1/library/music/files/{id}/export?profile=KEY. Unlike the
// Subsonic stream path, an export must be a complete, self-contained audio
// file with its metadata and artwork intact: the stream cache encodes with
// -map_metadata -1, so stream-cache output is NEVER acceptable as an export
// and is never consulted here. An export is either a reused library variant
// (validated, not trusted) or a fresh conversion into an isolated temporary
// directory that is removed once the response is done. Nothing in this path
// writes to the media root, the downloads staging tree or the stream cache,
// and no catalog row is created or modified.

// ExportStream is one finished audio file prepared for download. File must be
// closed before Cleanup is called (on Windows an open file cannot be
// removed), and the HTTP handler documents that contract: the handler closes
// the file, then calls Cleanup. Cleanup is idempotent and best-effort; a
// failure is logged, never fatal, because the temporary directory lives in
// the OS temp area and does not endanger the media root.
type ExportStream struct {
	File        *os.File
	Name        string // download filename with the correct target extension
	ContentType string
	Size        int64
	ModTime     time.Time
	Cleanup     func() // idempotent; for reused variants it is a no-op
}

// exportBitrateTolerancePercent widens the lossy-quality acceptance band: a
// variant whose probed bitrate is at least 8% below the requested kbps still
// counts as the requested quality, because encoders round (Opus VBR lands
// slightly under its ceiling; MP3 frames round to kbps steps). Anything
// lower is never served and never labelled as the requested quality.
const exportBitrateTolerancePercent = 8

// OpenExportStream returns a finished audio file for one registered music
// file at the requested converting profile. The "original" codec is rejected
// here because the HTTP handler streams original files directly; this
// function only ever produces whole converted outputs.
//
// Order of preference:
//  1. An existing library variant of the same track whose probed codec and
//     quality actually satisfy the profile (variant_profile labels are never
//     trusted alone; every candidate is opened inside the media root through
//     os.Root and probed).
//  2. A fresh metadata-preserving conversion into an isolated temporary
//     directory (prefix "reelscarey-export-", never inside the media root,
//     never the stream cache).
func (s *Service) OpenExportStream(ctx context.Context, fileID int64, profile QualityProfile) (ExportStream, error) {
	if fileID < 1 {
		return ExportStream{}, ErrInvalidInput
	}
	switch profile.Codec {
	case "mp3", "flac", "aac", "opus":
	default:
		// "original" and any unknown codec never reach this path: the
		// handler serves original files with its existing direct-stream
		// code and rejects unresolvable profiles before calling.
		return ExportStream{}, ErrInvalidInput
	}
	if s.mediaDir == "" {
		return ExportStream{}, &Error{Code: "music_file_missing", Message: "This registered music file is not available on the server."}
	}
	src, err := s.conversionSource(ctx, fileID)
	if err != nil {
		return ExportStream{}, err
	}
	ext := conversionFileExtension(profile, path.Ext(src.RelativePath))
	name := exportFileName(src.RelativePath, ext)
	if stream, ok, err := s.reuseExportVariant(ctx, src, profile, ext); err != nil {
		return ExportStream{}, err
	} else if ok {
		return stream, nil
	}
	return s.convertForExport(ctx, src, profile, name, ext)
}

// exportFileName derives the clean download filename: the source base name
// with the target extension and no variant label suffix, e.g. "01 Song.mp3".
func exportFileName(relative, ext string) string {
	base := path.Base(relative)
	return strings.TrimSuffix(base, path.Ext(base)) + ext
}

// reuseExportVariant looks for a registered variant of the same track that
// genuinely satisfies the profile. Track identity follows the same pattern
// as variantFileRow: track_id when set, otherwise artist+album for files
// without a catalog track. Every candidate is validated, not trusted.
func (s *Service) reuseExportVariant(ctx context.Context, src conversionSource, profile QualityProfile, ext string) (ExportStream, bool, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if src.TrackID > 0 {
		rows, err = s.store.db.QueryContext(ctx, `SELECT id,relative_path FROM music_files
			WHERE variant_role='variant' AND id<>? AND track_id=? ORDER BY id`, src.ID, src.TrackID)
	} else {
		rows, err = s.store.db.QueryContext(ctx, `SELECT id,relative_path FROM music_files
			WHERE variant_role='variant' AND id<>? AND track_id IS NULL AND artist_id=? AND album_id=? ORDER BY id`,
			src.ID, src.ArtistID, src.AlbumID)
	}
	if err != nil {
		return ExportStream{}, false, err
	}
	defer rows.Close()
	type exportVariant struct {
		id    int64
		rel   string
		tags  AudioTags
		score int
	}
	var candidates []exportVariant
	for rows.Next() {
		var id int64
		var rel string
		if err := rows.Scan(&id, &rel); err != nil {
			return ExportStream{}, false, err
		}
		clean, err := cleanMediaRelative(rel)
		if err != nil {
			continue // a corrupt stored path is skipped, never served
		}
		tags, ok := s.validateExportVariant(ctx, clean, profile)
		if !ok {
			continue
		}
		score := 0
		if profile.Codec != "flac" {
			score = probedKbps(tags) - profile.BitrateKbps
			if score < 0 {
				score = -score
			}
		}
		candidates = append(candidates, exportVariant{id: id, rel: clean, tags: tags, score: score})
	}
	if err := rows.Err(); err != nil {
		return ExportStream{}, false, err
	}
	if len(candidates) == 0 {
		return ExportStream{}, false, nil
	}
	// Exact (codec+bitrate) matches win first; otherwise the nearest
	// satisfying variant. Ties prefer the older row for stable behaviour.
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score < candidates[j].score
		}
		return candidates[i].id < candidates[j].id
	})
	chosen := candidates[0]

	media, err := os.OpenRoot(s.mediaDir)
	if err != nil {
		return ExportStream{}, false, err
	}
	defer media.Close()
	file, err := media.Open(chosen.rel)
	if errors.Is(err, os.ErrNotExist) {
		// The row is stale (the variant was removed on disk); convert fresh.
		return ExportStream{}, false, nil
	}
	if err != nil {
		return ExportStream{}, false, err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = file.Close()
		return ExportStream{}, false, nil
	}
	return ExportStream{
		File:        file,
		Name:        exportFileName(src.RelativePath, ext),
		ContentType: audioContentType(chosen.tags.Format, "export"+ext),
		Size:        info.Size(),
		ModTime:     info.ModTime(),
		// A reused variant is a registered library file: export must never
		// delete it, so Cleanup is a no-op (idempotent by construction).
		Cleanup: func() {},
	}, true, nil
}

// validateExportVariant decides whether one stored variant path may serve an
// export for the profile: the path must resolve to a regular file inside the
// media root (os.Root containment, same discipline as OpenAudioFile), the
// probed codec family must equal the profile's target family, and the probed
// quality must be at or above the requested quality within the rounding
// tolerance. An unknown bitrate is rejected: unverifiable quality is never
// labelled as the requested quality.
func (s *Service) validateExportVariant(ctx context.Context, rel string, profile QualityProfile) (AudioTags, bool) {
	media, err := os.OpenRoot(s.mediaDir)
	if err != nil {
		return AudioTags{}, false
	}
	defer media.Close()
	file, err := media.Open(rel)
	if err != nil {
		return AudioTags{}, false
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = file.Close()
		return AudioTags{}, false
	}
	_ = file.Close()
	tags, err := s.prober.ProbeAudio(ctx, filepath.Join(s.mediaDir, filepath.FromSlash(rel)))
	if err != nil {
		return AudioTags{}, false
	}
	if probeFormatFamily(tags.Format) != profileTargetFamily(profile) {
		return AudioTags{}, false
	}
	if profile.Codec == "flac" {
		// A FLAC probe is lossless by definition; nothing further to verify.
		return tags, true
	}
	kbps := probedKbps(tags)
	if kbps <= 0 {
		return AudioTags{}, false
	}
	if kbps*100 < profile.BitrateKbps*(100-exportBitrateTolerancePercent) {
		return AudioTags{}, false
	}
	return tags, true
}

// probedKbps normalizes a probe bitrate to kbps using the same >320-bps
// heuristic as ConversionNeeded, so real ffprobe output (bits per second)
// and stored kbps values compare consistently.
func probedKbps(tags AudioTags) int {
	if tags.Bitrate > 320 {
		return tags.Bitrate / 1000
	}
	return tags.Bitrate
}

// convertForExport runs a fresh metadata-preserving conversion into an
// isolated temporary directory. The output keeps title, artist, album
// artist, album, track/disc numbers, year/date, genre and the MusicBrainz
// tags the source carries: ConvertFile converts with -map_metadata 0 (never
// the stream path's -map_metadata -1), so every source tag the target
// container supports survives, including genre and MusicBrainz tags even
// though the AudioTags document itself does not model genre. Artwork is
// mapped from the source's attached picture when the target container
// supports it; for Opus the ffmpeg ogg muxer cannot attach pictures
// reliably, so those outputs keep audio plus text metadata without artwork
// and without corrupting the stream (see convertArgs).
func (s *Service) convertForExport(ctx context.Context, src conversionSource, profile QualityProfile, name, ext string) (ExportStream, error) {
	tempDir, err := os.MkdirTemp("", "reelscarey-export-")
	if err != nil {
		return ExportStream{}, err
	}
	removed := false
	cleanup := func() {
		if removed {
			return
		}
		removed = true
		if err := os.RemoveAll(tempDir); err != nil && !errors.Is(err, os.ErrNotExist) {
			slog.Default().Warn("music export temporary directory could not be removed", "error", err)
		}
	}
	result, err := s.ConvertFile(ctx, ConvertRequest{
		SourcePath:   filepath.Join(s.mediaDir, filepath.FromSlash(src.RelativePath)),
		OutputPath:   filepath.Join(tempDir, name),
		Profile:      profile,
		EmbedArtwork: true,
	})
	if err != nil {
		cleanup()
		return ExportStream{}, err
	}
	file, err := os.Open(filepath.Join(tempDir, name))
	if err != nil {
		cleanup()
		return ExportStream{}, err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 {
		_ = file.Close()
		cleanup()
		return ExportStream{}, &Error{Code: "music_output_invalid", Message: "The exported audio file could not be verified."}
	}
	return ExportStream{
		File:        file,
		Name:        name,
		ContentType: audioContentType(result.Tags.Format, "export"+ext),
		Size:        info.Size(),
		ModTime:     info.ModTime(),
		Cleanup:     cleanup,
	}, nil
}
