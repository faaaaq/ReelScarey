package transfer

import (
	"context"
	"database/sql"
	"errors"
	"path"
	"strings"
	"time"
)

// verifyAnimeAudio runs the post-download English-audio check at import
// planning, after ffprobe has inspected the completed source and before the
// hash or any import_operations row is written. It activates only when both
// hold: the job targets a series and the series row has
// anime_search_settings.english_dub_only = 1. Movies, ordinary TV and
// non-strict Anime return immediately, so the added cost elsewhere is one
// indexed single-row SELECT per import.
//
// The pre-download title gate (validateAnimeDubRelease /
// validateAnimeDubRequirement) is untouched; this is the structural check of
// the actual file's declared audio tracks. ffprobe failure paths never reach
// here — ffprobe_missing, invalid_media and no_video_stream already abort the
// import with their own problems.
//
// Behavior per classification (strict shows only):
//
//   - VERIFIED_ENGLISH   → import proceeds exactly as today; classification
//     rides probe_json.
//   - UNVERIFIED         → import proceeds fail-open (a legitimate dub with
//     stripped tags must not wedge forever); the classification and reason
//     are recorded and an honest activity note is written.
//   - VERIFIED_NO_ENGLISH → nothing is deleted and no plan is journaled. The
//     release is blacklisted for seven days (best-effort) and a
//     problem("anime_audio_mismatch") is returned, so the worker marks the
//     download blocked without automatic retry. Because no import_operations
//     row was written, an owner retry re-probes and re-blocks deterministically
//     until the owner removes the download, disables the requirement or the
//     file is replaced.
//
// The source parameter is used only to name the affected file in the owner
// message; it is never opened or written.
func (i *Importer) verifyAnimeAudio(ctx context.Context, j Job, source string, probe *Probe) error {
	if j.Kind != "series" {
		return nil
	}
	var dubOnly int
	e := i.service.db.QueryRowContext(ctx, `SELECT COALESCE(a.english_dub_only,0) FROM media_items m LEFT JOIN anime_search_settings a ON a.media_id=m.id WHERE m.id=? AND m.kind='series'`, j.MovieID).Scan(&dubOnly)
	if e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return nil
		}
		return e
	}
	if dubOnly == 0 {
		return nil
	}
	classification := classifyAudioStreams(probe.Audio)
	probe.AudioVerification = &classification
	switch classification.State {
	case AudioVerifiedEnglish:
		return nil
	case AudioUnverified:
		// Fail-open: import proceeds. Record one honest note; a failed note
		// never blocks the import.
		if e := i.service.noteUnverifiedAnimeAudio(ctx, j, classification); e != nil && i.service.logger != nil {
			i.service.logger.Warn("anime audio activity note failed", "download", j.ID, "error", e)
		}
		return nil
	}
	// verified_no_english: block without journaling. The candidate release is
	// blacklisted so the next automatic search skips it; the owner can remove
	// the entry from the release blacklist review.
	var indexer string
	_ = i.service.db.QueryRowContext(ctx, `SELECT indexer_name FROM downloads WHERE id=?`, j.ID).Scan(&indexer)
	reason := "anime_audio_mismatch: " + strings.Join(boundedLanguages(classification.Languages, 6), ", ")
	if err := i.service.BlockRelease(ctx, j.Protocol, j.Hash, j.ID, indexer, j.Title, reason, 7*24*time.Hour); err != nil && i.service.logger != nil {
		i.service.logger.Warn("release blacklist write failed", "download", j.ID, "error", err)
	}
	return problem("anime_audio_mismatch", animeAudioMismatchMessage(source, classification))
}

// animeAudioMismatchMessage is the actionable blocked-download message. It
// names the affected file, states what happened and what was not done, and
// lists the owner paths out of the block.
func animeAudioMismatchMessage(source string, classification AudioVerification) string {
	languages := strings.Join(boundedLanguages(classification.Languages, 6), ", ")
	if languages == "" {
		languages = "non-English"
	}
	return "The downloaded video's audio tracks are " + languages + ", but this show requires an English dub. Nothing was imported and " + path.Base(source) + " was kept in the download folder. Review the file, choose a different release, or change the show's dub requirement, then retry or remove this download."
}

// boundedLanguages copies at most n languages so a pathological stream count
// cannot produce an enormous message; the full list stays in probe JSON.
func boundedLanguages(languages []string, n int) []string {
	if len(languages) <= n {
		return languages
	}
	return append(append([]string{}, languages[:n]...), "…")
}

// noteUnverifiedAnimeAudio records one honest activity note when a strict
// show's file could not be classified from its language tags. The episode
// becomes available; the owner can remove or replace it through the ordinary
// reviewed flows.
func (s *Service) noteUnverifiedAnimeAudio(ctx context.Context, j Job, classification AudioVerification) error {
	reason := classification.Reason
	if reason == "" {
		reason = "the file has no usable language tags"
	}
	_, e := s.db.ExecContext(ctx, `INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'download',?,?,?)`,
		j.ID, j.Title, "English audio could not be verified for this download ("+reason+"); the show requires an English dub. The file was imported; review it if it is Japanese audio.")
	return e
}
