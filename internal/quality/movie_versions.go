package quality

import (
	"context"
	"database/sql"
)

type MovieVersionPolicy struct {
	Enabled      bool  `json:"enabled"`
	HDProfileID  int64 `json:"hd_profile_id"`
	UHDProfileID int64 `json:"uhd_profile_id"`
	Revision     int   `json:"revision"`
}

type movieVersionQuery interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func ReadMovieVersions(ctx context.Context, q movieVersionQuery, id int64) (MovieVersionPolicy, error) {
	var p MovieVersionPolicy
	err := q.QueryRowContext(ctx, `SELECT COALESCE(v.enabled,0),COALESCE(v.hd_profile_id,1),COALESCE(v.uhd_profile_id,2),COALESCE(v.revision,0) FROM media_items m LEFT JOIN movie_version_settings v ON v.media_id=m.id WHERE m.id=? AND m.kind='movie'`, id).Scan(&p.Enabled, &p.HDProfileID, &p.UHDProfileID, &p.Revision)
	return p, err
}

func GetTx(ctx context.Context, tx *sql.Tx, id int64) (Profile, error) {
	return scan(tx.QueryRowContext(ctx, `SELECT `+columns+` FROM quality_profiles WHERE id=?`, id))
}

// MovieVersionMatches uses inspected resolution for files, and explicit release
// resolution labels for selection. Separate bands prevent one file satisfying
// both download targets even if the selected quality profiles overlap.
func MovieVersionMatches(version string, resolution int) bool {
	switch version {
	case "hd":
		return resolution > 0 && resolution <= 1080
	case "uhd":
		return resolution == 2160
	default:
		return version == ""
	}
}

func MovieVersionName(version string) string {
	switch version {
	case "hd":
		return "HD"
	case "uhd":
		return "4K"
	case "additional":
		return "Additional version"
	case "replacement":
		return "Replacement"
	}
	return "Movie"
}
