package music

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/faaaaq/ReelScarey/internal/organization"
)

const maxLibraryEditTracks = 200
const maxCustomArtwork = 5 << 20
const libraryEditJobMask = "library_editor"

type LibraryEditorFile struct {
	ID      int64  `json:"id"`
	TrackID int64  `json:"track_id"`
	Name    string `json:"name"`
	Role    string `json:"role"`
}

type LibraryEditorItem struct {
	ID            int64             `json:"id"`
	Fields        map[string]string `json:"fields"`
	ArtworkURL    string            `json:"artwork_url,omitempty"`
	CustomArtwork bool              `json:"custom_artwork,omitempty"`
}

type LibraryEditorSnapshot struct {
	Items    []LibraryEditorItem `json:"items"`
	Files    []LibraryEditorFile `json:"files,omitempty"`
	Revision string              `json:"revision"`
}

type LibraryArtworkInput struct {
	URL   string `json:"url,omitempty"`
	Data  []byte `json:"data,omitempty"`
	Reset bool   `json:"reset,omitempty"`
}

type LibraryEditorUpdate struct {
	Kind      string               `json:"kind"`
	IDs       []int64              `json:"ids"`
	Revision  string               `json:"revision"`
	Fields    map[string]string    `json:"fields"`
	Artwork   *LibraryArtworkInput `json:"artwork,omitempty"`
	WriteTags bool                 `json:"write_tags,omitempty"`
	Filename  string               `json:"filename,omitempty"`
	FileID    int64                `json:"file_id,omitempty"`
}

type LibraryEditorResult struct {
	EditJobID int64 `json:"edit_job_id,omitempty"`
}

// Manual tags use a sparse patch. The existing edit journal serializes it
// along with the source and destination paths for recovery and rollback.
type libraryTagEdit struct {
	Fields      map[string]string `json:"fields"`
	ExpectedSHA string            `json:"expected_sha,omitempty"`
}

func libraryEditError(message string) error {
	return &Error{Code: "invalid_music", Message: message}
}

func libraryEditorIDs(kind string, ids []int64) error {
	if len(ids) == 0 || len(ids) > maxLibraryEditTracks || kind != "tracks" && len(ids) != 1 {
		return ErrInvalidInput
	}
	if kind != "artists" && kind != "albums" && kind != "tracks" && kind != "files" {
		return ErrInvalidInput
	}
	seen := make(map[int64]bool, len(ids))
	for _, id := range ids {
		if id <= 0 || seen[id] {
			return ErrInvalidInput
		}
		seen[id] = true
	}
	return nil
}

func (s *Service) LibraryEditor(ctx context.Context, kind string, ids []int64) (LibraryEditorSnapshot, error) {
	if err := libraryEditorIDs(kind, ids); err != nil {
		return LibraryEditorSnapshot{}, err
	}
	tx, err := s.store.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return LibraryEditorSnapshot{}, err
	}
	defer tx.Rollback()
	snapshot, _, err := readLibraryEditor(ctx, tx, kind, ids)
	return snapshot, err
}

type libraryEditorFile struct {
	LibraryEditorFile
	relativePath string
	tagJSON      string
	digest       string
	trackFields  string
}

func readLibraryEditor(ctx context.Context, tx *sql.Tx, kind string, ids []int64) (LibraryEditorSnapshot, []libraryEditorFile, error) {
	snapshot := LibraryEditorSnapshot{Items: make([]LibraryEditorItem, 0, len(ids))}
	var files []libraryEditorFile
	var revisions []string
	for _, id := range ids {
		item, revision, err := readLibraryEditorItem(ctx, tx, kind, id)
		if err != nil {
			return LibraryEditorSnapshot{}, nil, err
		}
		snapshot.Items = append(snapshot.Items, item)
		revisions = append(revisions, revision)
	}
	if kind == "tracks" || kind == "files" {
		var err error
		files, err = libraryEditorFiles(ctx, tx, kind, ids)
		if err != nil {
			return LibraryEditorSnapshot{}, nil, err
		}
		for _, file := range files {
			snapshot.Files = append(snapshot.Files, file.LibraryEditorFile)
			revisions = append(revisions, file.relativePath, file.tagJSON, file.digest)
		}
	}
	data, _ := json.Marshal([]any{kind, snapshot.Items, snapshot.Files, revisions})
	snapshot.Revision = fmt.Sprintf("%x", sha256.Sum256(data))
	return snapshot, files, nil
}

func readLibraryEditorItem(ctx context.Context, tx *sql.Tx, kind string, id int64) (LibraryEditorItem, string, error) {
	item := LibraryEditorItem{ID: id}
	values := make(map[string]string)
	var saved, artworkKey string
	var err error
	switch kind {
	case "files":
		err = tx.QueryRowContext(ctx, `SELECT id FROM music_files WHERE id=?`, id).Scan(&item.ID)
		saved = "{}"
	case "artists":
		var name, sortName, disambiguation, country, artistType, overview string
		err = tx.QueryRowContext(ctx, `SELECT name,sort_name,disambiguation,country,artist_type,overview,editor_fields,custom_artwork_key FROM music_artists WHERE id=?`, id).
			Scan(&name, &sortName, &disambiguation, &country, &artistType, &overview, &saved, &artworkKey)
		values = map[string]string{"name": name, "sort_name": sortName, "disambiguation": disambiguation, "country": country, "type": artistType, "overview": overview}
		item.ArtworkURL = fmt.Sprintf("/api/v1/library/music/artists/%d/image", id)
	case "albums":
		var title, year, artist, primary, secondary, date string
		err = tx.QueryRowContext(ctx, `SELECT a.title,COALESCE(CAST(a.year AS TEXT),''),ar.name,a.primary_type,a.secondary_types,a.release_date,a.editor_fields,a.custom_artwork_key
			FROM music_albums a JOIN music_artists ar ON ar.id=a.artist_id WHERE a.id=?`, id).
			Scan(&title, &year, &artist, &primary, &secondary, &date, &saved, &artworkKey)
		values = map[string]string{"title": title, "year": year, "album_artist": artist, "type": primary, "secondary_types": secondary, "release_date": date}
		item.ArtworkURL = fmt.Sprintf("/api/v1/library/music/albums/%d/cover", id)
	case "tracks":
		var title, artist, albumArtist, album, year, disc, number, tagJSON string
		err = tx.QueryRowContext(ctx, `SELECT t.title,ar.name,COALESCE(json_extract(a.editor_fields,'$.album_artist'),ar.name),a.title,
			COALESCE(CAST(a.year AS TEXT),''),t.disc,t.number,COALESCE(f.tag_json,'{}'),t.editor_fields
			FROM music_tracks t JOIN music_albums a ON a.id=t.album_id JOIN music_artists ar ON ar.id=a.artist_id
			LEFT JOIN music_files f ON f.id=(SELECT id FROM music_files WHERE track_id=t.id ORDER BY variant_role<>'primary', id LIMIT 1)
			WHERE t.id=?`, id).Scan(&title, &artist, &albumArtist, &album, &year, &disc, &number, &tagJSON, &saved)
		var observed AudioTags
		_ = json.Unmarshal([]byte(tagJSON), &observed)
		values = map[string]string{"title": title, "artist": firstNonEmpty(observed.Artist, artist), "album_artist": albumArtist, "album": album,
			"year": year, "disc": disc, "number": number, "genre": observed.Genre, "comment": observed.Comment}
	}
	if errors.Is(err, sql.ErrNoRows) {
		return item, "", ErrNotFound
	}
	if err != nil {
		return item, "", err
	}
	if err := json.Unmarshal([]byte(saved), &values); err != nil {
		return item, "", err
	}
	item.Fields = values
	item.CustomArtwork = artworkKey != ""
	if item.CustomArtwork {
		item.ArtworkURL += "?v=" + artworkKey
	}
	return item, saved, nil
}

func libraryEditorFiles(ctx context.Context, tx *sql.Tx, kind string, ids []int64) ([]libraryEditorFile, error) {
	column := map[string]string{"artists": "f.artist_id", "albums": "f.album_id", "tracks": "f.track_id", "files": "f.id"}[kind]
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(args)), ",")
	rows, err := tx.QueryContext(ctx, `SELECT f.id,COALESCE(f.track_id,0),f.variant_role,f.relative_path,f.tag_json,f.sha256,COALESCE(t.editor_fields,'{}')
		FROM music_files f LEFT JOIN music_tracks t ON t.id=f.track_id
		WHERE `+column+` IN (`+marks+`) ORDER BY f.track_id,f.variant_role<>'primary',f.id LIMIT 501`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var files []libraryEditorFile
	for rows.Next() {
		var file libraryEditorFile
		if err := rows.Scan(&file.ID, &file.TrackID, &file.Role, &file.relativePath, &file.tagJSON, &file.digest, &file.trackFields); err != nil {
			return nil, err
		}
		file.Name = path.Base(file.relativePath)
		files = append(files, file)
	}
	if len(files) > 500 {
		return nil, libraryEditError("This selection has more than 500 registered files. Choose a smaller selection.")
	}
	return files, rows.Err()
}

func cleanLibraryFields(kind string, fields map[string]string) (map[string]string, error) {
	allowed := map[string]string{
		"artists": "name sort_name disambiguation country type overview",
		"albums":  "title year album_artist type secondary_types release_date",
		"tracks":  "title artist album_artist album year disc number genre comment",
	}
	out := make(map[string]string, len(fields))
	for field, value := range fields {
		if !strings.Contains(" "+allowed[kind]+" ", " "+field+" ") || field == "" || strings.Contains(field, " ") {
			return nil, ErrInvalidInput
		}
		value = strings.TrimSpace(value)
		maximum := 300
		if field == "overview" || field == "comment" {
			maximum = 4000
		}
		if len(value) > maximum || strings.ContainsRune(value, 0) {
			return nil, libraryEditError("One of the values exceeds its supported length or contains a null character.")
		}
		switch field {
		case "name", "title", "album", "album_artist", "artist":
			if value == "" {
				return nil, libraryEditError("Names, titles and credits need a value.")
			}
		case "year", "disc", "number":
			if field == "year" && value == "" {
				break
			}
			n, err := strconv.Atoi(value)
			if err != nil || n < 1 || n > 9999 {
				return nil, libraryEditError("Year, disc and track number must be positive numbers of up to four digits.")
			}
			value = strconv.Itoa(n)
		case "release_date":
			if value != "" && !validLibraryDate(value) {
				return nil, libraryEditError("Use YYYY, YYYY-MM or YYYY-MM-DD for the release date.")
			}
		}
		out[field] = value
	}
	return out, nil
}

func validLibraryDate(value string) bool {
	for _, layout := range []string{"2006", "2006-01", "2006-01-02"} {
		if date, err := time.Parse(layout, value); err == nil && date.Year() > 0 && date.Format(layout) == value {
			return true
		}
	}
	return false
}

func (s *Service) UpdateLibraryEditor(ctx context.Context, update LibraryEditorUpdate) (LibraryEditorResult, error) {
	if err := libraryEditorIDs(update.Kind, update.IDs); err != nil {
		return LibraryEditorResult{}, err
	}
	fields, err := cleanLibraryFields(update.Kind, update.Fields)
	if err != nil {
		return LibraryEditorResult{}, err
	}
	isCatalogEntity := update.Kind == "artists" || update.Kind == "albums"
	if update.Artwork != nil && !isCatalogEntity || (update.Filename != "" || update.FileID != 0) && isCatalogEntity ||
		update.FileID != 0 && len(update.IDs) != 1 ||
		update.Kind == "files" && update.WriteTags || len(fields) == 0 && update.Artwork == nil && update.Filename == "" || update.Revision == "" {
		return LibraryEditorResult{}, ErrInvalidInput
	}
	art, mime, err := resolveLibraryArtwork(ctx, update.Artwork)
	if err != nil {
		return LibraryEditorResult{}, err
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return LibraryEditorResult{}, err
	}
	defer tx.Rollback()
	snapshot, files, err := readLibraryEditor(ctx, tx, update.Kind, update.IDs)
	if err != nil {
		return LibraryEditorResult{}, err
	}
	if update.Revision != snapshot.Revision {
		return LibraryEditorResult{}, ErrPlanChanged
	}
	if update.WriteTags && isCatalogEntity {
		files, err = libraryEditorFiles(ctx, tx, update.Kind, update.IDs)
		if err != nil {
			return LibraryEditorResult{}, err
		}
	}
	edits, err := s.libraryFileEdits(ctx, tx, update, fields, snapshot.Items, files)
	if err != nil {
		return LibraryEditorResult{}, err
	}
	if len(fields) != 0 || update.Artwork != nil {
		if err := saveLibraryEditorFields(ctx, tx, update, fields, art, mime); err != nil {
			return LibraryEditorResult{}, err
		}
	}
	var result LibraryEditorResult
	if len(edits) != 0 {
		if err := organization.EnsureMediaAvailableTx(ctx, tx); err != nil {
			return result, err
		}
		result.EditJobID, err = persistEditJob(ctx, tx, edits, false, libraryEditJobMask)
		if err != nil {
			return LibraryEditorResult{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return LibraryEditorResult{}, err
	}
	if result.EditJobID != 0 {
		go s.runEditJob(context.WithoutCancel(ctx), result.EditJobID)
	}
	return result, nil
}

func saveLibraryEditorFields(ctx context.Context, tx *sql.Tx, update LibraryEditorUpdate, fields map[string]string, art []byte, mime string) error {
	table := "music_" + update.Kind // Kind was validated against the three catalog tables.
	data, _ := json.Marshal(fields)
	editorFields := fields
	if update.Kind != "tracks" {
		editorFields = make(map[string]string, len(fields))
		for key, value := range fields {
			// The existing provenance flags own these fields and support
			// explicitly restoring their provider values.
			if key != "name" && key != "title" && key != "year" {
				editorFields[key] = value
			}
		}
	}
	editorData, _ := json.Marshal(editorFields)
	for _, id := range update.IDs {
		var assignments string
		switch update.Kind {
		case "artists":
			assignments = `,name=COALESCE(json_extract(?, '$.name'),name),
				name_overridden=CASE WHEN json_type(?, '$.name') IS NULL THEN name_overridden ELSE 1 END,
				sort_name=COALESCE(json_extract(?, '$.sort_name'),sort_name),
				disambiguation=COALESCE(json_extract(?, '$.disambiguation'),disambiguation),
				country=COALESCE(json_extract(?, '$.country'),country),
				artist_type=COALESCE(json_extract(?, '$.type'),artist_type),
				overview=COALESCE(json_extract(?, '$.overview'),overview)`
		case "albums":
			assignments = `,title=COALESCE(json_extract(?, '$.title'),title),
				title_overridden=CASE WHEN json_type(?, '$.title') IS NULL THEN title_overridden ELSE 1 END,
				year=CASE WHEN json_type(?, '$.year') IS NULL THEN year ELSE CAST(NULLIF(json_extract(?, '$.year'),'') AS INTEGER) END,
				year_overridden=CASE WHEN json_type(?, '$.year') IS NULL THEN year_overridden ELSE 1 END,
				primary_type=COALESCE(json_extract(?, '$.type'),primary_type),
				secondary_types=COALESCE(json_extract(?, '$.secondary_types'),secondary_types),
				release_date=COALESCE(json_extract(?, '$.release_date'),release_date)`
		}
		args := []any{string(editorData)}
		for range strings.Count(assignments, "?") {
			args = append(args, string(data))
		}
		if update.Artwork != nil {
			key := ""
			if len(art) != 0 {
				key = fmt.Sprintf("%x", sha256.Sum256(art))
			}
			assignments += ",custom_artwork=?,custom_artwork_type=?,custom_artwork_key=?"
			args = append(args, art, mime, key)
		}
		if _, err := tx.ExecContext(ctx, "UPDATE "+table+" SET editor_fields=json_patch(editor_fields,?),updated_at=unixepoch()"+assignments+" WHERE id=?", append(args, id)...); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) libraryFileEdits(ctx context.Context, tx *sql.Tx, update LibraryEditorUpdate, fields map[string]string, items []LibraryEditorItem, files []libraryEditorFile) ([]EditFile, error) {
	if !update.WriteTags && update.Filename == "" {
		return nil, nil
	}
	var media *os.Root
	var edits []EditFile
	planned := make(map[string]bool)
	seenTrack := make(map[int64]bool)
	byID := make(map[int64]map[string]string, len(items))
	for _, item := range items {
		byID[item.ID] = item.Fields
	}
	matched := update.Filename == ""
	for _, file := range files {
		target := file.relativePath
		rename := update.Filename != "" && (update.FileID == file.ID || update.FileID == 0 && !seenTrack[file.TrackID])
		seenTrack[file.TrackID] = true
		if rename {
			matched = true
			name, err := libraryFilename(update.Filename, file.Name, byID[file.TrackID], fields, len(items) > 1)
			if err != nil {
				return nil, err
			}
			target = path.Join(path.Dir(target), name)
		}
		var patch map[string]string
		if update.WriteTags {
			patch = libraryFileTags(update.Kind, fields, file, items[0].Fields)
		}
		if target == file.relativePath && len(patch) == 0 {
			continue
		}
		var pending bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM music_edit_files e JOIN music_edit_jobs j ON j.id=e.job_id
			WHERE j.state='running' AND (e.music_file_id=? OR e.destination_path=? COLLATE NOCASE))`, file.ID, target).Scan(&pending); err != nil {
			return nil, err
		}
		if pending {
			return nil, libraryEditError("A file in this selection already has an edit in progress.")
		}
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM music_conversion_jobs
			WHERE file_id=? AND kind<>'download' AND state IN ('queued','processing'))`, file.ID).Scan(&pending); err != nil {
			return nil, err
		}
		if pending {
			return nil, libraryEditError("A file in this selection has an audio conversion in progress.")
		}
		if target != file.relativePath {
			var occupied bool
			key := strings.ToLower(target)
			if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM music_files WHERE relative_path=? COLLATE NOCASE AND id<>?)`, target, file.ID).Scan(&occupied); err != nil {
				return nil, err
			}
			if media == nil {
				var err error
				media, err = os.OpenRoot(s.mediaDir)
				if err != nil {
					return nil, err
				}
				defer media.Close()
			}
			_, err := media.Lstat(target)
			if occupied || planned[key] || !errors.Is(err, os.ErrNotExist) {
				return nil, libraryEditError("A filename is already in use. Choose a unique name for each file.")
			}
			planned[key] = true
		}
		action := "move"
		if len(patch) > 0 {
			action = "move_retag"
			if target == file.relativePath {
				action = "retag"
			}
		}
		expectedSHA := ""
		if len(file.digest) == sha256.Size*2 {
			expectedSHA = file.digest
		}
		edits = append(edits, EditFile{MusicFileID: file.ID, SourcePath: file.relativePath, DestinationPath: target, Action: action,
			Tags: EditTags{LibraryEdit: &libraryTagEdit{Fields: patch, ExpectedSHA: expectedSHA}}, PreviousTagJSON: file.tagJSON})
	}
	if !matched {
		return nil, libraryEditError("The filename change needs a registered file in this selection.")
	}
	return edits, nil
}

func libraryFileTags(kind string, fields map[string]string, file libraryEditorFile, current map[string]string) map[string]string {
	if kind == "tracks" {
		return fields
	}
	patch := map[string]string{}
	trackFields := map[string]string{}
	_ = json.Unmarshal([]byte(file.trackFields), &trackFields)
	if kind == "artists" {
		if name, ok := fields["name"]; ok {
			var tags AudioTags
			_ = json.Unmarshal([]byte(file.tagJSON), &tags)
			for field, old := range map[string]string{"artist": tags.Artist, "album_artist": tags.AlbumArtist} {
				if _, edited := trackFields[field]; !edited && (old == "" || equalFold(old, current["name"])) {
					patch[field] = name
				}
			}
		}
		return patch
	}
	for key, tag := range map[string]string{"title": "album", "year": "year", "album_artist": "album_artist"} {
		_, trackOverride := trackFields[tag]
		if value, ok := fields[key]; ok && !trackOverride {
			patch[tag] = value
		}
	}
	return patch
}

func libraryFilename(input, original string, current, patch map[string]string, batch bool) (string, error) {
	ext := path.Ext(original)
	name := strings.TrimSpace(input)
	if batch {
		value := func(key string) string {
			if value, ok := patch[key]; ok {
				return safeName(value)
			}
			return safeName(current[key])
		}
		name = strings.NewReplacer("{title}", value("title"), "{artist}", value("artist"), "{album}", value("album"),
			"{track}", value("number"), "{disc}", value("disc"), "{filename}", strings.TrimSuffix(original, ext)).Replace(name)
		if strings.ContainsAny(name, "{}") {
			return "", libraryEditError("The filename pattern contains an unknown placeholder.")
		}
	} else if strings.EqualFold(path.Ext(name), ext) {
		name = strings.TrimSuffix(name, path.Ext(name))
	}
	if name == "" || strings.Trim(name, " .") != name || len(name)+len(ext) > 240 || strings.ContainsAny(name, `/\\:*?"<>|`) ||
		strings.ContainsFunc(name, unicode.IsControl) || !pathIsPortable(name) {
		return "", libraryEditError("Enter a valid filename. The file extension is kept automatically.")
	}
	return name + ext, nil
}

func pathIsPortable(name string) bool {
	base, _, _ := strings.Cut(strings.ToUpper(name), ".")
	switch base {
	case "CON", "PRN", "AUX", "NUL":
		return false
	}
	return len(base) != 4 || !(strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) || base[3] < '1' || base[3] > '9'
}

func resolveLibraryArtwork(ctx context.Context, input *LibraryArtworkInput) ([]byte, string, error) {
	if input == nil {
		return nil, "", nil
	}
	sources := 0
	for _, given := range []bool{input.Reset, input.URL != "", len(input.Data) > 0} {
		if given {
			sources++
		}
	}
	if sources != 1 {
		return nil, "", ErrInvalidInput
	}
	if input.Reset {
		return nil, "", nil
	}
	data := input.Data
	if input.URL != "" {
		if !allowedArtworkURL(input.URL) {
			return nil, "", libraryEditError("Use an image URL from a supported music provider, or upload the image.")
		}
		var err error
		data, _, err = artworkFetch(ctx, input.URL)
		if err != nil {
			return nil, "", err
		}
	}
	if len(data) == 0 || len(data) > maxCustomArtwork {
		return nil, "", libraryEditError("Choose artwork up to 5 MB.")
	}
	mime := http.DetectContentType(data)
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width <= 0 || config.Height <= 0 || config.Width > 10000 || config.Height > 10000 ||
		(mime != "image/jpeg" && mime != "image/png") {
		return nil, "", libraryEditError("Choose a JPEG or PNG image up to 10,000 pixels on each side.")
	}
	return data, mime, nil
}
