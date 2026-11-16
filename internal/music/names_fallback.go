package music

import (
	"context"
	"strings"
)

// Local names fallback. After the local catalog, AcoustID and the providers
// have had their chance, a file with a plausible artist and track title in its
// tags, file name or folder is still matched into the local catalog from its
// own names. These entries are tag-derived ("local-") identities: they never
// claim a MusicBrainz identity, never overwrite embedded tags and never move
// or rename media. Files without enough evidence stay unresolved for review.

// NamesFallbackRun bounds one local-fallback pass.
type NamesFallbackRun struct {
	onFile func(file ScanFile)
}

// NamesFallbackStats reports what one fallback pass achieved.
type NamesFallbackStats struct {
	Matched   int
	Skipped   int
	UsedITune int
}

// LocalNamesFallbackFiles matches unresolved files from their own names into
// the local catalog. It is idempotent: identities are deterministic, so a
// repeated pass reuses the same catalog rows instead of duplicating them.
func (s *Service) LocalNamesFallbackFiles(ctx context.Context, scanID int64, files []ScanFile, run NamesFallbackRun) (NamesFallbackStats, error) {
	stats := NamesFallbackStats{}
	catalog := newLocalCatalog(s)
	dbCtx := context.WithoutCancel(ctx)
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return stats, err
		}
		if cancelled, err := s.workflowCancelled(dbCtx, scanID); err != nil {
			return stats, err
		} else if cancelled {
			return stats, nil
		}
		ev := tagIdentityFor(file)
		// The fallback is the weakest evidence tier; an occupied numbered
		// position yields to the title evidence and takes the next free slot.
		ev.relaxedPosition = true
		if recovered := s.recoveredArtist(file.ID); recovered != "" &&
			(isPlaceholderName(ev.artistName) || ev.artistName == "") {
			if clean, ok := cleanText(recovered, 500); ok {
				ev.artistName = clean
				ev.artistKey = localArtistKey(clean)
				stats.UsedITune++
			}
		}
		if !ev.canCreateFromNames() {
			stats.Skipped++
			message := localNamesSkippedMessage(inferFileIdentity(file))
			if saveErr := s.updateScanFileState(dbCtx, file.ID, "unresolved", 0, 0, 0, message); saveErr != nil {
				return stats, saveErr
			}
			if run.onFile != nil {
				run.onFile(file)
			}
			continue
		}
		// A file whose album evidence MusicBrainz could still resolve is not
		// degraded to a tag-derived entry while the provider is cooling down;
		// the next run retries it instead of locking in a weaker identity.
		if (ev.albumKey != "" || ev.albumMBID != "") && s.client != nil && s.client.CoolingDown(ctx) {
			stats.Skipped++
			if run.onFile != nil {
				run.onFile(file)
			}
			continue
		}
		artistID, albumID, trackID, matchErr := catalog.matchTagCreateFromNames(ctx, file, ev)
		if matchErr != nil {
			if saveErr := s.updateScanFileState(dbCtx, file.ID, "unresolved", artistID, albumID, 0, matchErrorMessage(matchErr)); saveErr != nil {
				return stats, saveErr
			}
			if run.onFile != nil {
				run.onFile(file)
			}
			continue
		}
		if err := s.updateScanFileState(dbCtx, file.ID, "matched", artistID, albumID, trackID, ""); err != nil {
			return stats, err
		}
		stats.Matched++
		if run.onFile != nil {
			run.onFile(file)
		}
	}
	return stats, nil
}

// localNamesSkippedMessage explains why a file cannot even be matched from
// its own names, naming exactly what evidence is missing.
func localNamesSkippedMessage(identity fileIdentity) string {
	artistPresent := false
	for _, name := range identityArtistCandidates(identity) {
		if name != "" {
			artistPresent = true
			break
		}
	}
	title := strings.TrimSpace(identity.Title)
	titlePresent := title != "" && !isPlaceholderName(title)
	switch {
	case !artistPresent && !titlePresent:
		return "The file has no artist and no track title in its tags, file name or folder. There is nothing to match it against; enter the correct match manually."
	case !artistPresent:
		return "The file has no artist name in its tags, file name or folder. Enter the artist with Correct match."
	default:
		return "The file has no track title in its tags or file name. Enter the title with Correct match."
	}
}
