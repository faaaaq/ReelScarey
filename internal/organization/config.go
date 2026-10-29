package organization

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

var keyRE = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,39}$`)
var deviceRE = regexp.MustCompile(`(?i)^(CON|PRN|AUX|NUL|COM[0-9]|LPT[0-9])(?:\.|$)`)

func validRelative(v string) bool {
	if v == "" || len(v) > 1000 || !utf8.ValidString(v) || strings.ContainsAny(v, `\:<>"|?*`) || strings.HasPrefix(v, "/") || strings.IndexFunc(v, unicode.IsControl) >= 0 {
		return false
	}
	for _, p := range strings.Split(v, "/") {
		if p == "" || p == "." || p == ".." || len(p) > 220 || strings.Trim(p, " .") != p || deviceRE.MatchString(p) {
			return false
		}
	}
	return true
}
func defaults(movie, series string) []Library {
	return []Library{
		{Key: "movies", Name: "Movies", Folder: movie, Content: "movie", Preset: "legacy"},
		{Key: "tv", Name: "TV Shows", Folder: series, Content: "series", Preset: "legacy"},
		{Key: "anime", Name: "Anime", Folder: "Anime", Content: "mixed", Preset: "legacy"},
	}
}
func (s *Service) Get(ctx context.Context) (Config, error) { return s.config(ctx, s.db) }

type query interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func (s *Service) config(ctx context.Context, q query) (Config, error) {
	out := Config{MainRoot: s.cfg.MediaDir}
	var raw, movie, series string
	if e := q.QueryRowContext(ctx, `SELECT o.revision,o.libraries,s.movie_folder,s.series_folder FROM organization_config o JOIN settings s ON s.id=o.id WHERE o.id=1`).Scan(&out.Revision, &raw, &movie, &series); e != nil {
		return out, e
	}
	if e := json.Unmarshal([]byte(raw), &out.Libraries); e != nil {
		return out, e
	}
	if len(out.Libraries) == 0 {
		out.Libraries = defaults(movie, series)
	}
	return out, nil
}
func (s *Service) Update(ctx context.Context, in Config) (Config, error) {
	if in.Revision < 1 || len(in.Libraries) < 3 || len(in.Libraries) > 20 {
		return Config{}, problem("invalid_organization", "Use 3–20 libraries and the current settings revision.")
	}
	seen := map[string]bool{}
	folders := []string{}
	for n := range in.Libraries {
		l := &in.Libraries[n]
		l.Name = strings.TrimSpace(l.Name)
		l.Folder = strings.TrimSpace(l.Folder)
		if !keyRE.MatchString(l.Key) || seen[l.Key] || !utf8.ValidString(l.Name) || utf8.RuneCountInString(l.Name) < 1 || utf8.RuneCountInString(l.Name) > 80 || strings.IndexFunc(l.Name, unicode.IsControl) >= 0 || !validRelative(l.Folder) {
			return Config{}, problem("invalid_organization", "Each library needs a unique stable key, a name and a safe relative folder.")
		}
		seen[l.Key] = true
		switch l.Content {
		case "movie", "series", "mixed":
		default:
			return Config{}, problem("invalid_organization", "Choose movies, series or mixed content for each library.")
		}
		if l.Key == "movies" && l.Content != "movie" || l.Key == "tv" && l.Content != "series" || l.Key == "anime" && l.Content != "mixed" {
			return Config{}, problem("invalid_organization", "The Movies, TV Shows and Anime libraries retain their content types.")
		}
		for _, f := range folders {
			a, b := strings.ToLower(f), strings.ToLower(l.Folder)
			if a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/") {
				return Config{}, problem("invalid_organization", "Library folders must be separate, non-overlapping folders.")
			}
		}
		folders = append(folders, l.Folder)
		switch l.Preset {
		case "legacy":
			l.MovieTemplate = ""
			l.SeriesTemplate = ""
			l.EpisodeTemplate = ""
		case "emby", "jellyfin", "plex":
			tag := "[tmdbid={tmdb_id}]"
			if l.Preset == "jellyfin" {
				tag = "[tmdbid-{tmdb_id}]"
			}
			if l.Preset == "plex" {
				tag = "{{tmdb-{tmdb_id}}}"
			}
			l.MovieTemplate = "{title} ({year}) " + tag + "/{title} ({year}) " + tag
			l.SeriesTemplate = "{title} ({year}) " + tag
			l.EpisodeTemplate = "Season {season:02}/{title} - S{season:02}E{episode:02}{episode_range} - {episode_title}"
		case "custom":
		default:
			return Config{}, problem("invalid_organization", "Choose an organization preset or custom templates.")
		}
		if l.Preset != "legacy" {
			if e := validateTemplates(*l); e != nil {
				return Config{}, e
			}
		}
	}
	for _, key := range []string{"movies", "tv", "anime"} {
		if !seen[key] {
			return Config{}, problem("invalid_organization", "Keep the built-in Movies, TV Shows and Anime libraries.")
		}
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return Config{}, e
	}
	defer tx.Rollback()
	rows, e := tx.QueryContext(ctx, `SELECT DISTINCT library_key FROM media_organization`)
	if e != nil {
		return Config{}, e
	}
	var assigned []string
	for rows.Next() {
		var k string
		if e = rows.Scan(&k); e != nil {
			rows.Close()
			return Config{}, e
		}
		assigned = append(assigned, k)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return Config{}, e
	}
	for _, k := range assigned {
		if k != "" && !seen[k] {
			return Config{}, problem("library_in_use", "Move the assignments out of a library before removing it.")
		}
	}
	raw, e := json.Marshal(in.Libraries)
	if e != nil {
		return Config{}, e
	}
	result, e := tx.ExecContext(ctx, `UPDATE organization_config SET libraries=?,revision=revision+1 WHERE id=1 AND revision=?`, string(raw), in.Revision)
	if e != nil {
		return Config{}, e
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return Config{}, problem("revision_conflict", "Organization settings changed. Reload before saving.")
	}
	// Existing assignments must remain compatible with their media types.
	rows, e = tx.QueryContext(ctx, `SELECT a.library_key,m.kind FROM media_organization a JOIN media_items m ON m.id=a.media_id`)
	if e != nil {
		return Config{}, e
	}
	for rows.Next() {
		var k, kind string
		if e = rows.Scan(&k, &kind); e != nil {
			rows.Close()
			return Config{}, e
		}
		if k == "" {
			continue
		}
		l, _ := findLibrary(in, k)
		if !accepts(l, kind) {
			rows.Close()
			return Config{}, problem("library_in_use", "A library's content type cannot exclude titles already assigned to it.")
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return Config{}, e
	}
	if e = tx.Commit(); e != nil {
		return Config{}, e
	}
	return s.Get(ctx)
}
func findLibrary(c Config, key string) (Library, bool) {
	for _, l := range c.Libraries {
		if l.Key == key {
			return l, true
		}
	}
	return Library{}, false
}
func accepts(l Library, kind string) bool { return l.Content == "mixed" || l.Content == kind }
func content(ctx context.Context, q query, id int64) (Content, error) {
	var m Content
	e := q.QueryRowContext(ctx, `SELECT id,kind,category,category_revision,title,year,tmdb_id FROM media_items WHERE id=?`, id).Scan(&m.ID, &m.Kind, &m.Category, &m.CategoryRevision, &m.Title, &m.Year, &m.TMDbID)
	return m, e
}
func assignment(ctx context.Context, q query, c Config, m Content) (Assignment, error) {
	a := Assignment{MediaID: m.ID}
	e := q.QueryRowContext(ctx, `SELECT library_key,revision FROM media_organization WHERE media_id=?`, m.ID).Scan(&a.LibraryKey, &a.Revision)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return a, e
	}
	if a.LibraryKey == "" {
		a.UseDefault = true
		if m.Category == "anime" {
			a.LibraryKey = "anime"
		} else if m.Kind == "movie" {
			a.LibraryKey = "movies"
		} else {
			a.LibraryKey = "tv"
		}
	}
	var ok bool
	a.EffectiveLibrary, ok = findLibrary(c, a.LibraryKey)
	if !ok || !accepts(a.EffectiveLibrary, m.Kind) {
		return a, problem("invalid_library", "This title's assigned library no longer supports its content type.")
	}
	return a, nil
}
func (s *Service) GetAssignment(ctx context.Context, id int64) (Assignment, error) {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return Assignment{}, e
	}
	defer tx.Rollback()
	c, e := s.config(ctx, tx)
	if e != nil {
		return Assignment{}, e
	}
	m, e := content(ctx, tx, id)
	if e != nil {
		return Assignment{}, e
	}
	a, e := assignment(ctx, tx, c, m)
	if e != nil {
		return a, e
	}
	return a, tx.Commit()
}
func (s *Service) PutAssignment(ctx context.Context, id int64, key string, revision int) (Assignment, error) {
	if id < 1 || revision < 0 {
		return Assignment{}, problem("invalid_organization", "Choose a saved title and current assignment revision.")
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return Assignment{}, e
	}
	defer tx.Rollback()
	c, e := s.config(ctx, tx)
	if e != nil {
		return Assignment{}, e
	}
	m, e := content(ctx, tx, id)
	if e != nil {
		return Assignment{}, e
	}
	a, e := assignment(ctx, tx, c, m)
	if e != nil {
		return a, e
	}
	if a.Revision != revision {
		return a, problem("revision_conflict", "This library assignment changed. Reload before saving.")
	}
	if key != "" {
		l, ok := findLibrary(c, key)
		if !ok || !accepts(l, m.Kind) {
			return a, problem("invalid_library", "Choose a compatible library.")
		}
	}
	// Empty key restores category-based inheritance; keep a monotonically increasing fence.
	if _, e = tx.ExecContext(ctx, `INSERT INTO media_organization(media_id,library_key,revision) VALUES(?,?,1) ON CONFLICT(media_id) DO UPDATE SET library_key=excluded.library_key,revision=media_organization.revision+1`, id, key); e != nil {
		return a, e
	}
	if e = tx.Commit(); e != nil {
		return a, e
	}
	return s.GetAssignment(ctx, id)
}
func (s *Service) Capture(ctx context.Context, id int64) (Snapshot, error) {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return Snapshot{}, e
	}
	defer tx.Rollback()
	v, e := s.capture(ctx, tx, id, "")
	if e != nil {
		return v, e
	}
	return v, tx.Commit()
}
func (s *Service) CaptureTx(ctx context.Context, tx *sql.Tx, id int64) (Snapshot, error) {
	return s.capture(ctx, tx, id, "")
}
func (s *Service) capture(ctx context.Context, q query, id int64, target string) (Snapshot, error) {
	var out Snapshot
	c, e := s.config(ctx, q)
	if e != nil {
		return out, e
	}
	m, e := content(ctx, q, id)
	if e != nil {
		return out, e
	}
	a, e := assignment(ctx, q, c, m)
	if e != nil {
		return out, e
	}
	l := a.EffectiveLibrary
	if target != "" {
		var ok bool
		l, ok = findLibrary(c, target)
		if !ok || !accepts(l, m.Kind) {
			return out, problem("invalid_library", "Choose a library compatible with every selected title.")
		}
	}
	var naming string
	if e = q.QueryRowContext(ctx, `SELECT episode_naming FROM settings WHERE id=1`).Scan(&naming); e != nil {
		return out, e
	}
	return Snapshot{Library: l, Content: m, AssignmentRevision: a.Revision, ConfigRevision: c.Revision, LegacyEpisodeNaming: naming}, nil
}
func leaf(l Library, kind string) string {
	if l.Content == "mixed" {
		if kind == "movie" {
			return path.Join(l.Folder, "Movies")
		}
		return path.Join(l.Folder, "TV Shows")
	}
	return l.Folder
}
func Directory(l Library, kind string) string { return leaf(l, kind) }
func invalidTemplate() error {
	return problem("invalid_template", fmt.Sprint("Use safe relative templates and supported tokens; filenames keep their original extension."))
}
