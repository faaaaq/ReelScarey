package transfer

import (
	"regexp"
	"strconv"
	"strings"
)

// ParsedMusicRelease contains information extracted from a music release title.
type ParsedMusicRelease struct {
	Artist   string
	Album    string
	Year     *int
	Format   string // "FLAC","ALAC","MP3","AAC","OGG","OPUS","WAV",""
	Bitrate  int    // 320, 256, 192, 128, or 0
	Lossless bool
}

// Package-level patterns: ParseMusicRelease runs for every candidate title, so
// compiling these per call cost thousands of allocations per search.
var (
	musicYearRE    = regexp.MustCompile(`[\[\(]?(\d{4})[\]\)]?`)
	musicV0RE      = regexp.MustCompile(`(?i)\bV0\b`)
	musicV2RE      = regexp.MustCompile(`(?i)\bV2\b`)
	musicBracketRE = regexp.MustCompile(`[\[\(].*[\]\)]`)
	musicSpaceRE   = regexp.MustCompile(`\s+`)
	musicBitrateRE = map[int]*regexp.Regexp{
		320: regexp.MustCompile(`(?i)\b320\b`),
		256: regexp.MustCompile(`(?i)\b256\b`),
		192: regexp.MustCompile(`(?i)\b192\b`),
		128: regexp.MustCompile(`(?i)\b128\b`),
	}
	musicFormatRE = map[string]*regexp.Regexp{
		"FLAC": regexp.MustCompile(`(?i)\bFLAC\b`),
		"ALAC": regexp.MustCompile(`(?i)\bALAC\b`),
		"MP3":  regexp.MustCompile(`(?i)\bMP3\b`),
		"AAC":  regexp.MustCompile(`(?i)\bAAC\b`),
		"OGG":  regexp.MustCompile(`(?i)\bOGG\b`),
		"OPUS": regexp.MustCompile(`(?i)\bOPUS\b`),
		"WAV":  regexp.MustCompile(`(?i)\bWAV\b`),
	}
	musicQualityRE = func() []*regexp.Regexp {
		tokens := []string{"WEB", "WEB-DL", "WEBRip", "CD", "Vinyl", "Remaster", "Deluxe", "EP", "LP"}
		patterns := make([]*regexp.Regexp, 0, len(tokens))
		for _, token := range tokens {
			patterns = append(patterns, regexp.MustCompile(`(?i)\b`+token+`\b`))
		}
		return patterns
	}()
)

// ParseMusicRelease parses a music release title into a ParsedMusicRelease struct.
func ParseMusicRelease(title string) ParsedMusicRelease {
	res := ParsedMusicRelease{}
	if title == "" {
		return res
	}

	// 1. Strip a trailing file extension.
	dotIdx := strings.LastIndex(title, ".")
	if dotIdx > 0 {
		ext := strings.ToLower(title[dotIdx:])
		// Common music extensions
		if ext == ".flac" || ext == ".alac" || ext == ".mp3" || ext == ".aac" || ext == ".ogg" || ext == ".opus" || ext == ".wav" {
			title = title[:dotIdx]
		}
	}

	working := title

	// 2. Extract Year (1900-2099) in () or [] or standalone
	yearMatches := musicYearRE.FindAllStringSubmatchIndex(working, -1)
	for _, m := range yearMatches {
		yearStr := working[m[2]:m[3]]
		if y, err := strconv.Atoi(yearStr); err == nil && y >= 1900 && y <= 2099 {
			res.Year = &y
			// Remove the whole match from working title to avoid interference
			working = removeSubstring(working, m[0], m[1])
			break
		}
	}

	// 3. Detect format and bitrate tokens
	// We look for them in the original title or the working title to be safe,
	// but we should remove them to clean the album later.

	// Bitrate
	bitrates := []int{320, 256, 192, 128}
	for _, b := range bitrates {
		pattern := musicBitrateRE[b]
		if pattern.MatchString(working) {
			res.Bitrate = b
			working = pattern.ReplaceAllString(working, "")
			break
		}
	}

	// V0, V2 -> MP3, Bitrate 0
	if musicV0RE.MatchString(working) {
		res.Bitrate = 0
		res.Format = "MP3"
		working = musicV0RE.ReplaceAllString(working, "")
	} else if musicV2RE.MatchString(working) {
		res.Bitrate = 0
		res.Format = "MP3"
		working = musicV2RE.ReplaceAllString(working, "")
	}

	// Formats
	formats := []string{"FLAC", "ALAC", "MP3", "AAC", "OGG", "OPUS", "WAV"}
	for _, f := range formats {
		pattern := musicFormatRE[f]
		if pattern.MatchString(working) {
			res.Format = strings.ToUpper(f)
			// If it's a known lossless format
			if res.Format == "FLAC" || res.Format == "ALAC" || res.Format == "WAV" {
				res.Lossless = true
			}
			working = pattern.ReplaceAllString(working, "")
			break
		}
	}

	// 4. Split Artist and Album
	var artist, album string
	if idx := strings.Index(working, " - "); idx != -1 {
		artist = working[:idx]
		album = working[idx+3:]
	} else {
		// Scene style: Artist-Album-Year...
		parts := strings.Split(working, "-")
		if len(parts) >= 2 {
			artist = strings.TrimSpace(parts[0])
			album = strings.TrimSpace(parts[1])
		} else {
			artist = strings.TrimSpace(working)
			album = ""
		}
	}

	// 5. Clean Album
	// Remove remaining bracketed groups [anything] or (anything)
	album = musicBracketRE.ReplaceAllString(album, "")

	// Remove quality tokens
	for _, pattern := range musicQualityRE {
		album = pattern.ReplaceAllString(album, "")
	}

	// Trim punctuation and whitespace
	album = strings.Trim(album, " \t\n\r\f\v\"'.,!?;:-")
	album = musicSpaceRE.ReplaceAllString(album, " ")
	artist = strings.TrimSpace(artist)

	res.Artist = artist
	res.Album = album

	return res
}

func removeSubstring(s string, start, end int) string {
	return s[:start] + s[end:]
}
