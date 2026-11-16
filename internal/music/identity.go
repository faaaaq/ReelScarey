package music

import (
	"strconv"
	"strings"
)

// Identity inference combines three evidence sources for one scan file:
// embedded tags, the file base name and the parent directory structure.
//
// Tags stay authoritative for the fields they actually contain. The file name
// fills missing fields (for example "Artist - Title.mp3"), and the directory
// structure fills the album when the folder name is a real album folder rather
// than an organizational placeholder such as "Unknown" or "New Folder".
// Original tag values are never overwritten in storage; inference is only used
// for matching.

// isPlaceholderName reports whether a value is an organizational placeholder
// rather than a real name. Comparison is normalized, so "<Unknown>",
// "unknown", "-Unknown-" and "Unknown Artist" all fold together. The
// compilation marker "Various"/"Various Artists" is deliberately not treated
// here: it is a meaningful artist alias handled by the artist comparison.
func isPlaceholderName(value string) bool {
	name := normalizeMatchName(value)
	switch name {
	case "", "unknown", "unknown album", "unknown artist", "artist unknown",
		"unknown artist 2", "unknown artist 3", "untitled", "none", "no album",
		"no artist", "new folder", "new folder 2", "new folder 3", "unknown artist 4",
		"artiste inconnu", "album inconnu", "desconocido", "artista desconocido",
		"unbekannt", "unbekannter interpret", "track 1", "track 2", "track 3":
		return true
	}
	// Parenthesized or bracketed "unknown" such as "[Unknown]" or "(no album)".
	compact := compactMatchName(name)
	switch compact {
	case "unknown", "unknownalbum", "unknownartist", "untitled", "none", "noalbum",
		"noartist":
		return true
	}
	return false
}

// isVariousAlbumName reports whether an album title is really a compilation
// marker rather than a release name.
func isVariousAlbumName(value string) bool {
	return isVariousArtistsKey(normalizeMatchName(value))
}

// cdFolderPattern reports whether an album folder is really a disc label
// inside a release folder, such as "Americaz Crucify [CD1]" or "CD 2".
func cdFolderSuffix(value string) (base string, disc int, ok bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", 0, false
	}
	lower := strings.ToLower(trimmed)
	for n := 1; n <= 30; n++ {
		number := strconv.Itoa(n)
		labels := []string{"cd" + number, "disc" + number, "disk" + number, "cd " + number, "disc " + number, "disk " + number}
		for _, label := range labels {
			bracketed := []string{"[" + label + "]", "(" + label + ")", "{" + label + "}"}
			for _, pattern := range bracketed {
				if strings.HasSuffix(lower, pattern) {
					base = strings.TrimSpace(trimmed[:len(trimmed)-len(pattern)])
					return base, n, true
				}
			}
			if strings.HasSuffix(lower, label) {
				base := strings.TrimRight(trimmed[:len(trimmed)-len(label)], "-_ ([")
				return strings.TrimSpace(base), n, true
			}
		}
	}
	return "", 0, false
}

// fileIdentity is the merged, inference-aware evidence for one scan file. The
// Source fields record where each value came from so diagnostics and
// precedence decisions stay explainable.
type fileIdentity struct {
	Artist      string
	AlbumArtist string
	Album       string
	Title       string
	Track       *int
	Disc        *int
	Year        *int

	// DurationMS is the file's measured duration, used as duration evidence
	// when comparing provider candidates.
	DurationMS int

	ArtistSource      string
	AlbumSource       string
	TitleSource       string
	AlbumArtistSource string

	pathArtist  string
	pathAlbum   string
	fileArtist  string
	fileTitle   string
	fileTrackNo int
}

const (
	sourceTag      = "tag"
	sourceFilename = "filename"
	sourcePath     = "path"
)

// inferFileIdentity merges tags, file name and directory evidence for one
// file. Tag values win; the file name fills a missing artist or title; a real
// album folder name fills a missing album. Placeholder album names are never
// treated as album evidence.
func inferFileIdentity(file ScanFile) fileIdentity {
	out := fileIdentity{
		Artist:      strings.TrimSpace(file.Artist),
		AlbumArtist: strings.TrimSpace(file.AlbumArtist),
		Album:       strings.TrimSpace(file.Album),
		Title:       strings.TrimSpace(file.Title),
		Track:       file.Track,
		Disc:        file.Disc,
		Year:        file.Year,
	}
	out.ArtistSource, out.AlbumArtistSource, out.AlbumSource, out.TitleSource = sourceTag, sourceTag, sourceTag, sourceTag
	if out.Album != "" && (isPlaceholderName(out.Album) || isVariousAlbumName(out.Album)) {
		// A placeholder album tag is not album evidence.
		out.Album, out.AlbumSource = "", ""
	}
	if out.Album != "" {
		// A tag like "Album [CD2]" names release "Album" plus a disc; the
		// bracketed label is not part of the title.
		if baseName, disc, ok := cdFolderSuffix(out.Album); ok {
			if baseName != "" && !isPlaceholderName(baseName) {
				out.Album, out.AlbumSource = baseName, sourceTag
			} else {
				out.Album, out.AlbumSource = "", ""
			}
			if out.Disc == nil || (out.Disc != nil && *out.Disc <= 0) {
				d := disc
				out.Disc = &d
			}
		}
	}

	// File name evidence: "01 - Artist - Title", "Artist - Title", "01 Title",
	// "01. Title" and plain "Title" forms are all recognized.
	base := file.RelativePath
	if idx := strings.LastIndexAny(base, "/\\"); idx >= 0 {
		base = base[idx+1:]
	}
	if dot := strings.LastIndex(base, "."); dot > 0 {
		base = base[:dot]
	}
	fileArtist, fileTitle, fileTrack := parseAudioFileName(base)
	out.fileArtist, out.fileTitle, out.fileTrackNo = fileArtist, fileTitle, fileTrack

	// Directory evidence. A release folder structure is
	// Artist/Album/01 - Track.mp3 (or Artist/Album/file.mp3). Placeholder
	// folders are ignored entirely, and an "Album [CD n]" folder contributes
	// its release name plus a disc number instead of a bogus album title.
	segments := pathSegments(file.RelativePath)
	pathArtist, pathAlbum, pathDisc := "", "", 0
	if len(segments) >= 3 {
		// segments[0] is the music root ("music"); artist/album follow it.
		if candidate := segments[1]; !isPlaceholderName(candidate) {
			pathArtist = candidate
		}
		if candidate := segments[2]; !isPlaceholderName(candidate) {
			if baseName, disc, ok := cdFolderSuffix(candidate); ok {
				if baseName != "" && !isPlaceholderName(baseName) {
					pathAlbum = baseName
				}
				pathDisc = disc
			} else {
				pathAlbum = candidate
			}
		}
	} else if len(segments) == 2 {
		if candidate := segments[1]; !isPlaceholderName(candidate) {
			pathAlbum = candidate
		}
	}

	out.pathArtist, out.pathAlbum = pathArtist, pathAlbum
	out.DurationMS = file.DurationMS
	if pathDisc > 0 && (out.Disc == nil || *out.Disc <= 0) {
		disc := pathDisc
		out.Disc = &disc
	}

	if out.Title == "" {
		if fileTitle != "" {
			out.Title, out.TitleSource = fileTitle, sourceFilename
		} else if !isPlaceholderName(base) {
			out.Title, out.TitleSource = strings.TrimSpace(base), sourceFilename
		}
	}
	if out.Artist == "" {
		if fileArtist != "" {
			out.Artist, out.ArtistSource = fileArtist, sourceFilename
		} else if pathArtist != "" {
			out.Artist, out.ArtistSource = pathArtist, sourcePath
		}
	}
	if out.AlbumArtist == "" && out.Artist != "" {
		out.AlbumArtist, out.AlbumArtistSource = out.Artist, out.ArtistSource
	}
	if out.Album == "" && pathAlbum != "" {
		out.Album, out.AlbumSource = pathAlbum, sourcePath
	}
	if out.Track == nil || *out.Track <= 0 {
		if fileTrack > 0 {
			track := fileTrack
			out.Track = &track
		}
	}
	return out
}

// pathSegments splits a slash-normalized relative path into non-empty
// segments.
func pathSegments(relativePath string) []string {
	normalized := strings.ReplaceAll(relativePath, "\\", "/")
	parts := strings.Split(normalized, "/")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// parseAudioFileName recognizes the common file naming patterns and returns
// the artist, title and optional track number carried by the base name.
// Recognized forms:
//
//	01 - Artist - Title      (track number first, then artist and title)
//	01. Title / 01 Title     (track number and title only)
//	Artist - Title           (artist and title)
//	Title                    (title only)
func parseAudioFileName(base string) (artist, title string, number int) {
	base = strings.TrimSpace(base)
	if base == "" {
		return "", "", 0
	}
	if strings.ContainsRune(base, '/') || strings.ContainsRune(base, '\\') {
		return "", "", 0
	}
	rest := base
	if lead, remainder, ok := splitLeadingTrackNumber(rest); ok {
		number = lead
		rest = remainder
	}
	parts := splitDashParts(rest)
	switch len(parts) {
	case 1:
		title = parts[0]
	case 2:
		artist, title = parts[0], parts[1]
	case 3:
		// "Artist - Album - Title" or "Artist - Title - Remix". The middle
		// segment is treated as the title when the last segment looks like a
		// version credit; otherwise it is treated as an album.
		artist = parts[0]
		title = parts[1] + " - " + parts[2]
	}
	if isPlaceholderName(artist) {
		artist = ""
	}
	if isPlaceholderName(title) {
		title = ""
	}
	return strings.TrimSpace(artist), strings.TrimSpace(title), number
}

// splitLeadingTrackNumber recognizes a leading "01", "01.", "01 -", "01_",
// "001" or "1 " track number and returns it with the remainder.
func splitLeadingTrackNumber(base string) (int, string, bool) {
	if base == "" {
		return 0, "", false
	}
	end := 0
	for end < len(base) && end < 5 && base[end] >= '0' && base[end] <= '9' {
		end++
	}
	if end == 0 || end > 4 {
		return 0, "", false
	}
	digits := base[:end]
	raw := base[end:]
	if raw == "" {
		return 0, "", false
	}
	var remainder string
	switch {
	case raw[0] == '-' || raw[0] == '.' || raw[0] == '_' || raw[0] == ')':
		remainder = strings.TrimSpace(raw[1:])
	case strings.HasPrefix(raw, " - "):
		remainder = strings.TrimSpace(raw[3:])
	case raw[0] == ' ':
		// "01 Title" is a numbered track, but a title that starts with a
		// number such as "99 Problems" or "101 Dalmatians" must stay a title.
		// Only a small leading number is accepted as a track number here.
		number, err := strconv.Atoi(digits)
		if err != nil || number < 1 || number > 40 {
			return 0, "", false
		}
		remainder = strings.TrimSpace(raw[1:])
	default:
		// "01Title" without any separator is only treated as a track number
		// when the leading group is exactly two digits and the remainder
		// starts a new word.
		if end != 2 || !isLetterByte(raw[0]) {
			return 0, "", false
		}
		remainder = raw
	}
	number, err := strconv.Atoi(digits)
	if err != nil || number < 1 || number > 999 {
		return 0, "", false
	}
	if remainder == "" {
		return 0, "", false
	}
	return number, remainder, true
}

func isLetterByte(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || b >= 0x80
}

// splitDashParts splits a base name on whitespace-padded hyphens. A hyphen
// inside a name ("Jay-Z", "AC/DC-style") has no surrounding spaces and is
// never treated as a separator.
func splitDashParts(value string) []string {
	out := []string{}
	start := 0
	for i := 0; i < len(value); i++ {
		if value[i] != '-' {
			continue
		}
		left := strings.TrimRight(value[start:i], " \t")
		right := strings.TrimLeft(value[i+1:], " \t")
		if left != "" && right != "" && i > start && value[i-1] == ' ' {
			out = append(out, strings.TrimSpace(left))
			start = i + 1
			for start < len(value) && value[start] == ' ' {
				start++
			}
		}
	}
	out = append(out, strings.TrimSpace(value[start:]))
	if len(out) == 0 {
		out = []string{strings.TrimSpace(value)}
	}
	return out
}

// identityArtistCandidates returns the plausible artist names for matching in
// evidence order. The album artist (or inferred path artist) comes first
// because it identifies the release; the track artist follows because guest
// and compilation tracks credit their own performer; a primary-without-credit
// variant covers "Artist feat. Someone" names.
func identityArtistCandidates(identity fileIdentity) []string {
	out := []string{}
	seen := map[string]bool{}
	var add func(value string)
	add = func(value string) {
		value = strings.TrimSpace(value)
		if value == "" || isPlaceholderName(value) {
			return
		}
		key := normalizeArtistName(value)
		if key == "" || seen[key] {
			return
		}
		seen[key] = true
		out = append(out, value)
		if primary, ok := primaryArtistName(value); ok {
			add(primary)
		}
	}
	if identity.AlbumArtist != "" {
		add(identity.AlbumArtist)
	}
	if identity.Artist != "" && !artistNamesEqual(identity.Artist, identity.AlbumArtist) {
		add(identity.Artist)
	}
	add(identity.pathArtist)
	add(identity.fileArtist)
	return out
}

// identityAlbumTitle returns the best album title evidence, or "" when the
// file carries no trustworthy album name.
func identityAlbumTitle(identity fileIdentity) string {
	album := strings.TrimSpace(identity.Album)
	if album == "" || isPlaceholderName(album) || isVariousAlbumName(album) {
		return ""
	}
	return album
}
