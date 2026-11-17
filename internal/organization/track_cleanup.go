package organization

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/faaaaq/ReelScarey/internal/metadata"
)

type TrackStream struct {
	Index    int
	Kind     string
	Language string
	Forced   bool
}

type TrackCleanupRequest struct {
	Mode    string `json:"mode"`
	Library string `json:"library"`
	MediaID int64  `json:"media_id,omitempty"`
	FileID  int64  `json:"file_id,omitempty"`
}

type cleanupTarget struct {
	ID        int64    `json:"id"`
	Path      string   `json:"path"`
	Original  string   `json:"original_language"`
	Languages []string `json:"languages"`
}

type TrackCleanup struct {
	Request   TrackCleanupRequest `json:"request"`
	Targets   []cleanupTarget     `json:"targets"`
	Processed int                 `json:"processed"`
	Changed   int                 `json:"changed"`
	Skipped   int                 `json:"skipped"`
	Examples  []string            `json:"issues,omitempty"`
	Sidecars  []Companion         `json:"sidecars,omitempty"`
	Dropped   int                 `json:"dropped"`
}

func (s *Service) SetTrackCleaner(read func(context.Context, *os.File) ([]TrackStream, error), rewrite func(context.Context, *os.File, *os.File, string, []int) (string, error)) {
	s.readTracks, s.rewriteTracks = read, rewrite
}

func (s *Service) SetOriginalLanguageLookup(lookup func(context.Context, string, int64) (string, error)) {
	s.originalLanguage = lookup
}

func (s *Service) resolveOriginalLanguage(ctx context.Context, mediaID int64) (string, error) {
	var original, kind string
	var providerID int64
	if err := s.db.QueryRowContext(ctx, `SELECT original_language,kind,tmdb_id FROM media_items WHERE id=?`, mediaID).Scan(&original, &kind, &providerID); err != nil {
		return "", err
	}
	if trackLanguage(original) != "" || s.originalLanguage == nil {
		return original, nil
	}
	original, err := s.originalLanguage(ctx, kind, providerID)
	if err != nil || trackLanguage(original) == "" {
		return "", err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE media_items SET original_language=? WHERE id=? AND tmdb_id=? AND original_language=''`, original, mediaID, providerID)
	return original, err
}

func (s *Service) PreviewTrackCleanup(ctx context.Context, request TrackCleanupRequest) (Plan, error) {
	defer ReadMedia()()
	if err := EnsureMediaAvailable(ctx, s.db); err != nil {
		return Plan{}, err
	}
	if !slices.Contains([]string{"all_subtitles", "other_subtitles", "other_non_forced", "other_audio"}, request.Mode) ||
		request.MediaID < 0 || request.FileID < 0 || request.MediaID > 0 && request.FileID > 0 {
		return Plan{}, problem("invalid_selection", "Choose a library and a track cleanup option.")
	}
	root, resolved, err := s.openRoot("")
	if err != nil {
		return Plan{}, err
	}
	root.Close()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Plan{}, err
	}
	defer tx.Rollback()
	cfg, err := s.config(ctx, tx)
	if err != nil {
		return Plan{}, err
	}
	if request.Library != "" {
		if _, ok := findLibrary(cfg, request.Library); !ok {
			return Plan{}, problem("invalid_selection", "Choose a configured video library.")
		}
	}
	cleanup := &TrackCleanup{Request: request, Targets: []cleanupTarget{}}
	// Effective language choices follow the per-item and default library profile,
	// then the existing show override and global Languages & tracks settings.
	rows, err := tx.QueryContext(ctx, `WITH registered AS (
		SELECT f.id, f.relative_path, m.id AS media_id, m.original_language,
		       COALESCE(o.library_key, CASE WHEN m.category='anime' THEN 'anime' WHEN m.kind='movie' THEN 'movies' ELSE 'tv' END) AS library,
		       CASE WHEN m.category='anime' THEN 'anime' WHEN m.kind='movie' THEN 'movies' ELSE 'tv' END AS category
		FROM media_files f JOIN media_items m ON m.id=COALESCE(f.movie_id,f.series_id)
		LEFT JOIN media_organization o ON o.media_id=m.id
		WHERE (?=0 OR m.id=?) AND (?=0 OR f.id=?)
	)
	SELECT r.id, r.relative_path, r.original_language, COALESCE(p.subtitle_tracks, so.tracks, g.tracks), COALESCE(p.audio_languages,'[]')
	FROM registered r
	CROSS JOIN subtitle_preferences g
	LEFT JOIN library_language_defaults d ON d.library_key=r.category
	LEFT JOIN item_language_profiles i ON i.media_id=r.media_id
	LEFT JOIN language_profiles p ON p.id=COALESCE(i.profile_id,d.profile_id)
	LEFT JOIN subtitle_overrides so ON so.media_id=r.media_id
	WHERE g.id=1 AND (?='' OR r.library=?) ORDER BY r.id`, request.MediaID, request.MediaID, request.FileID, request.FileID, request.Library, request.Library)
	if err != nil {
		return Plan{}, err
	}
	for rows.Next() {
		var target cleanupTarget
		var subtitles, audio string
		if err = rows.Scan(&target.ID, &target.Path, &target.Original, &subtitles, &audio); err != nil {
			rows.Close()
			return Plan{}, err
		}
		if request.Mode == "other_audio" {
			if err = json.Unmarshal([]byte(audio), &target.Languages); err != nil {
				rows.Close()
				return Plan{}, err
			}
		}
		if len(target.Languages) == 0 {
			var tracks []struct {
				Language string `json:"language"`
			}
			if err = json.Unmarshal([]byte(subtitles), &tracks); err != nil {
				rows.Close()
				return Plan{}, err
			}
			for _, track := range tracks {
				target.Languages = append(target.Languages, track.Language)
			}
		}
		cleanup.Targets = append(cleanup.Targets, target)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return Plan{}, err
	}
	if len(cleanup.Targets) == 0 {
		return Plan{}, problem("invalid_selection", "There are no registered videos in this selection.")
	}
	now := time.Now().Unix()
	p := Plan{ID: id(), State: "review", Root: resolved, ConfigRevision: cfg.Revision, Phase: "planned", Created: now, Updated: now, Files: []PlannedFile{}, TrackCleanup: cleanup}
	raw, err := json.Marshal(p)
	if err != nil {
		return Plan{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO organization_plans(id,state,snapshot,created_at,updated_at) VALUES(?,'review',?,?,?)`, p.ID, string(raw), now, now); err != nil {
		return Plan{}, err
	}
	return p, tx.Commit()
}

func trackLanguage(raw string) string { return metadata.LanguageCode(raw) }

func configuredLanguage(raw string, choices []string) bool {
	tag := trackLanguage(raw)
	if tag == "" {
		return false
	}
	base, _, hasRegion := strings.Cut(tag, "-")
	for _, choice := range choices {
		configured := trackLanguage(choice)
		want, _, precise := strings.Cut(configured, "-")
		if tag == configured || base == want && (!hasRegion || !precise) {
			return true
		}
	}
	return false
}

func dropEmbedded(mode string, target cleanupTarget, streams []TrackStream) []int {
	var drop []int
	remainingAudio := 0
	for _, stream := range streams {
		switch {
		case mode == "other_audio" && stream.Kind == "audio":
			code := trackLanguage(stream.Language)
			if code == "" || configuredLanguage(stream.Language, []string{target.Original}) || configuredLanguage(stream.Language, target.Languages) {
				remainingAudio++
			} else {
				drop = append(drop, stream.Index)
			}
		case mode != "other_audio" && stream.Kind == "subtitle":
			if mode == "all_subtitles" || !(mode == "other_non_forced" && stream.Forced) && !configuredLanguage(stream.Language, target.Languages) {
				drop = append(drop, stream.Index)
			}
		}
	}
	if mode == "other_audio" && (remainingAudio == 0 || trackLanguage(target.Original) == "") {
		return nil
	}
	return drop
}

func subtitleFileLanguage(name, video string) (code string, forced bool) {
	stem := strings.TrimSuffix(path.Base(video), path.Ext(video))
	base := strings.TrimSuffix(path.Base(name), path.Ext(name))
	if len(base) <= len(stem)+1 || !strings.EqualFold(base[:len(stem)+1], stem+".") {
		return "", false
	}
	values := strings.Split(strings.ToLower(base[len(stem)+1:]), ".")
	forced = slices.Contains(values, "forced") || slices.Contains(values, "foreign")
	if slices.Contains([]string{"forced", "foreign", "sdh", "hi", "cc", "signs", "songs", "full"}, values[0]) {
		return "", forced
	}
	return values[0], forced
}

func (s *Service) cleanupSidecars(ctx context.Context, root *os.Root, target cleanupTarget, mode, planID string) ([]Companion, error) {
	candidates, err := removableSidecars(ctx, root, s.db, target.ID, target.Path, []int64{target.ID})
	if err != nil {
		return nil, err
	}
	var out []Companion
	for _, asset := range candidates {
		code, forced := subtitleFileLanguage(asset.Source, target.Path)
		if mode != "all_subtitles" {
			rows, err := s.db.QueryContext(ctx, `SELECT language,variant FROM subtitles WHERE media_file_id=? AND relative_path=?`, target.ID, asset.Source)
			if err != nil {
				return nil, err
			}
			keep := false
			for rows.Next() {
				var found, variant string
				if err = rows.Scan(&found, &variant); err != nil {
					rows.Close()
					return nil, err
				}
				keep = keep || configuredLanguage(found, target.Languages) || mode == "other_non_forced" && variant == "forced"
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return nil, err
			}
			if keep || configuredLanguage(code, target.Languages) || mode == "other_non_forced" && forced {
				continue
			}
		}
		asset.Destination = path.Join(".reelscarey-trash", planID, asset.Source)
		out = append(out, asset)
	}
	return out, nil
}

func (s *Service) prepareCleanupFile(ctx context.Context, root *os.Root, p *Plan, target cleanupTarget) error {
	r, owner, err := replacementMetadata(ctx, s.db, target.ID)
	if err != nil {
		return err
	}
	if r.Path != target.Path {
		return problem("source_changed", "The registered path changed after review.")
	}
	if err = idle(ctx, s.db, owner, target.ID); err != nil {
		return err
	}
	if p.TrackCleanup.Request.Mode == "other_audio" && trackLanguage(target.Original) == "" {
		target.Original, err = s.resolveOriginalLanguage(ctx, owner)
		if err != nil || trackLanguage(target.Original) == "" {
			return problem("unknown_language", "The original language is missing. Refresh the title metadata first.")
		}
	}
	if err = s.prepareChecksums(ctx, root, []int64{target.ID}, 0); err != nil {
		return err
	}
	r, owner, err = replacementMetadata(ctx, s.db, target.ID)
	if err != nil {
		return err
	}
	if r.Path != target.Path {
		return problem("source_changed", "The registered path changed after review.")
	}
	old, err := inspect(root, r.Path)
	if err != nil || old.Size != r.Size {
		return problem("source_changed", "The registered video changed before track cleanup.")
	}
	old.SHA = r.SHA
	input, err := root.Open(filepath.FromSlash(r.Path))
	if err != nil {
		return err
	}
	digest, err := hash(ctx, input)
	if err != nil || digest != r.SHA {
		input.Close()
		return problem("source_changed", "The registered video changed before track cleanup.")
	}
	var streams []TrackStream
	var trackErr error
	if s.readTracks == nil || s.rewriteTracks == nil {
		trackErr = problem("ffprobe_missing", "The embedded track tools are unavailable.")
	} else {
		streams, trackErr = s.readTracks(ctx, input)
	}
	input.Close()
	if err = checkSource(ctx, root, old, false); err != nil {
		return err
	}
	mode := p.TrackCleanup.Request.Mode
	var sidecars []Companion
	if mode != "other_audio" {
		sidecars, err = s.cleanupSidecars(ctx, root, target, mode, p.ID)
		if err != nil {
			return err
		}
	}
	if trackErr != nil {
		if len(sidecars) == 0 || ctx.Err() != nil {
			return trackErr
		}
		s.noteCleanupIssue(p, target, trackErr)
	}
	drop := dropEmbedded(mode, target, streams)
	if mode == "other_audio" && len(drop) == 0 {
		var matching, known bool
		for _, stream := range streams {
			if stream.Kind != "audio" {
				continue
			}
			code := trackLanguage(stream.Language)
			known = known || code != ""
			matching = matching || code == "" || configuredLanguage(stream.Language, []string{target.Original}) || configuredLanguage(stream.Language, target.Languages)
		}
		if known && !matching {
			return problem("all_audio", "Removing these tracks would leave the video without audio.")
		}
	}
	if len(drop) > 0 {
		episodes, err := s.episodes(ctx, s.db, target.ID)
		if err != nil {
			return err
		}
		provenance, err := youtubeVersion(ctx, s.db, target.ID)
		if err != nil {
			return err
		}
		title, err := content(ctx, s.db, owner)
		if err != nil {
			return err
		}
		p.Replacement = &Replacement{FileID: target.ID, MediaID: owner, Title: title.Title, Before: r, BeforeYouTube: provenance, Original: old, Incoming: old,
			MetadataOnly: true, TagStage: path.Join(path.Dir(r.Path), ".reelscarey-tag-"+id()), Episodes: episodes, DropStreams: drop}
	}
	p.TrackCleanup.Sidecars = sidecars
	return s.savePhase(ctx, p, "track_ready")
}

func (s *Service) executeTrackCleanup(ctx context.Context, p *Plan) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	job := p.TrackCleanup
	if job.Processed >= len(job.Targets) {
		_, err := s.db.ExecContext(ctx, `UPDATE organization_plans SET state='completed',message='',updated_at=unixepoch() WHERE id=?`, p.ID)
		return err
	}
	target := job.Targets[job.Processed]
	root, _, err := s.openRoot(p.Root)
	if err != nil {
		return err
	}
	defer root.Close()
	if p.Phase == "planned" && p.Replacement == nil {
		if err = s.prepareCleanupFile(ctx, root, p, target); err != nil {
			if ctx.Err() != nil {
				return err
			}
			return s.skipCleanup(ctx, p, target, err)
		}
	}
	if p.Replacement != nil && (p.Phase == "track_ready" || p.Phase == "planned" || p.Phase == "prepared" || p.Phase == "published") {
		if p.Phase == "track_ready" {
			if err = s.savePhase(ctx, p, "planned"); err != nil {
				return err
			}
		}
		if err = s.executeEmbeddedTitle(ctx, p); err != nil {
			if p.Phase != "planned" || ctx.Err() != nil || checkSource(ctx, root, p.Replacement.Original, false) != nil {
				return err
			}
			s.removeCleanupStage(ctx, root, p)
			if len(job.Sidecars) == 0 {
				return s.skipCleanup(ctx, p, target, err)
			}
			s.noteCleanupIssue(p, target, err)
			p.Replacement = nil
			if err = s.savePhase(ctx, p, "track_ready"); err != nil {
				return err
			}
		}
	}
	if len(job.Sidecars) > 0 && p.Phase != "track_sidecars_committed" {
		for i, asset := range job.Sidecars {
			if err = checkSource(ctx, root, asset, true); err != nil {
				return err
			}
			if err = s.publish(ctx, root, *p, asset, i); err != nil {
				return err
			}
			if err = s.checkOutput(ctx, root, s.db, p.ID, asset); err != nil {
				return err
			}
		}
		if err = s.commitCleanupSidecars(ctx, p, target.ID); err != nil {
			return err
		}
	}
	for i, asset := range job.Sidecars {
		if err = s.cleanPublishedSource(ctx, root, *p, asset, i); err != nil {
			return err
		}
	}
	changed := len(job.Sidecars) > 0 || p.Replacement != nil
	if changed {
		job.Changed++
		job.Dropped += len(job.Sidecars)
		if p.Replacement != nil {
			job.Dropped += len(p.Replacement.DropStreams)
		}
	}
	return s.nextCleanupFile(ctx, p)
}

func (s *Service) removeCleanupStage(ctx context.Context, root *os.Root, p *Plan) {
	name := p.Replacement.TagStage
	var saved string
	if err := s.db.QueryRowContext(ctx, `SELECT identity FROM organization_outputs WHERE plan_id=? AND destination=?`, p.ID, name).Scan(&saved); err != nil {
		return
	}
	actual, err := inspect(root, name)
	if err == nil && saved != "" && actual.Identity == saved {
		_ = root.Remove(filepath.FromSlash(name))
	}
}

func (s *Service) skipCleanup(ctx context.Context, p *Plan, target cleanupTarget, err error) error {
	s.noteCleanupIssue(p, target, err)
	return s.nextCleanupFile(ctx, p)
}

func (s *Service) noteCleanupIssue(p *Plan, target cleanupTarget, err error) {
	p.TrackCleanup.Skipped++
	if len(p.TrackCleanup.Examples) < 20 {
		message := "Could not safely inspect or rewrite this video."
		var typed *Error
		if errors.As(err, &typed) {
			message = typed.Message
		}
		p.TrackCleanup.Examples = append(p.TrackCleanup.Examples, fmt.Sprintf("%s: %s", target.Path, message))
	}
}

func (s *Service) nextCleanupFile(ctx context.Context, p *Plan) error {
	p.TrackCleanup.Processed++
	p.TrackCleanup.Sidecars = nil
	p.Replacement = nil
	return s.savePhase(ctx, p, "planned")
}

func (s *Service) commitCleanupSidecars(ctx context.Context, p *Plan, fileID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, asset := range p.TrackCleanup.Sidecars {
		for _, statement := range []string{
			`DELETE FROM subtitles WHERE relative_path=?`,
			`DELETE FROM subtitle_imports WHERE destination=? AND phase='committed'`,
			`DELETE FROM youtube_subtitle_files WHERE relative_path=?`,
			`DELETE FROM youtube_subtitle_imports WHERE destination_path=? AND phase='committed'`,
		} {
			if _, err = tx.ExecContext(ctx, statement, asset.Source); err != nil {
				return err
			}
		}
	}
	if len(p.TrackCleanup.Sidecars) > 0 {
		if _, err = tx.ExecContext(ctx, `DELETE FROM subtitle_inspections WHERE media_file_id=?`, fileID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM subtitle_candidates WHERE media_file_id=?`, fileID); err != nil {
			return err
		}
	}
	p.Phase = "track_sidecars_committed"
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE organization_plans SET snapshot=?,updated_at=unixepoch() WHERE id=?`, string(raw), p.ID); err != nil {
		return err
	}
	return tx.Commit()
}
