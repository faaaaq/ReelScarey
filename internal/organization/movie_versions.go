package organization

import (
	"context"
	"fmt"

	"github.com/faaaaq/ReelScarey/internal/quality"
)

// QualityTag renders the inspected quality label for a video: resolution plus
// an explicit HDR or SDR transfer tag. It returns "" when the video has no
// measurable resolution, so unverified files never claim a quality.
func QualityTag(width, height int, hdr bool) string {
	resolution := quality.VerifiedQuality(width, height)
	if resolution <= 0 {
		return ""
	}
	label := fmt.Sprintf("%dp", resolution)
	if hdr {
		label += " HDR"
	} else {
		label += " SDR"
	}
	return label
}

// Include a stable per-file suffix whenever multiple movie files coexist. It
// remains part of the review snapshot, so a changed version set invalidates it.
// Every registered file also projects its inspected quality tag: resolution
// plus HDR/SDR transfer characteristics when the probe recorded them, or
// resolution alone for files probed before HDR detection. Unverified files
// carry no quality facts and omit the tag entirely.
func (s *Service) captureFile(ctx context.Context, q query, mediaID, fileID int64, target string) (Snapshot, error) {
	v, err := s.capture(ctx, q, mediaID, target)
	if err != nil {
		return v, err
	}
	var count, resolution int
	var managed string
	var hdr any
	err = q.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM media_files WHERE movie_id=? OR series_id=?),f.video_resolution,f.video_hdr,COALESCE(d.movie_version,'') FROM media_files f LEFT JOIN downloads d ON d.id=f.download_id WHERE f.id=? AND (f.movie_id=? OR f.series_id=?)`, mediaID, mediaID, fileID, mediaID, mediaID).Scan(&count, &resolution, &hdr, &managed)
	if err != nil {
		return v, err
	}
	v.Quality = qualityLabel(resolution, hdr)
	if v.Content.Kind != "movie" {
		return v, nil
	}
	if count > 1 || managed != "" {
		v.MovieVersion = fmt.Sprintf("Version %d", fileID)
	}
	return v, nil
}

// qualityLabel renders the naming-engine quality tag. Resolution comes from
// the generated projection of the stored probe; the HDR/SDR suffix appears
// only when the probe recorded transfer characteristics.
func qualityLabel(resolution int, hdr any) string {
	if resolution <= 0 {
		return ""
	}
	label := fmt.Sprintf("%dp", resolution)
	if b, ok := hdr.(int); ok {
		if b == 1 {
			label += " HDR"
		} else {
			label += " SDR"
		}
	}
	return label
}
