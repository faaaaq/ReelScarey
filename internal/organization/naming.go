package organization

import (
	"fmt"
	"path"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

var supported = map[string]bool{"title": true, "year": true, "tmdb_id": true, "season": true, "season:02": true, "episode": true, "episode:02": true, "episode_end": true, "episode_end:02": true, "episode_title": true, "episode_range": true, "absolute": true, "absolute:03": true, "absolute_end": true, "absolute_end:03": true, "absolute_range": true}

func sanitize(v string, limit int) string {
	v = strings.Trim(strings.Map(func(c rune) rune {
		if unicode.IsControl(c) || strings.ContainsRune(`<>:"/\|?*`, c) {
			return '_'
		}
		return c
	}, v), " .")
	for len(v) > limit {
		_, n := utf8.DecodeLastRuneInString(v)
		v = v[:len(v)-n]
	}
	v = strings.Trim(v, " .")
	if v == "" {
		v = "Unknown"
	}
	if deviceRE.MatchString(v) {
		v = "_" + v
	}
	return v
}

// SafeComponent applies the same portable filename rules to user-visible
// source collections without allowing a title to introduce path components.
func SafeComponent(v string, limit int) string { return sanitize(v, limit) }
func validateTemplates(l Library) error {
	for n, v := range []string{l.MovieTemplate, l.SeriesTemplate, l.EpisodeTemplate} {
		if len(v) < 1 || len(v) > 600 {
			return invalidTemplate()
		}
		stripped, e := expand(v, func(token string) (string, bool) {
			return "value", supported[token] && (n == 2 || token == "title" || token == "year" || token == "tmdb_id")
		})
		if e != nil || !validRelative(stripped) {
			return invalidTemplate()
		}
		if n == 0 && !strings.Contains(v, "{title}") || n == 1 && !strings.Contains(v, "{title}") || n == 2 && !(strings.Contains(v, "{episode}") || strings.Contains(v, "{episode:02}") || strings.Contains(v, "{absolute}") || strings.Contains(v, "{absolute:03}")) {
			return invalidTemplate()
		}
	}
	return nil
}

// Double braces escape a literal brace, allowing Plex's {tmdb-ID} tags while
// retaining a closed, allowlisted token language for user templates.
func expand(template string, resolve func(string) (string, bool)) (string, error) {
	var out strings.Builder
	for len(template) > 0 {
		if strings.HasPrefix(template, "{{") {
			out.WriteByte('{')
			template = template[2:]
			continue
		}
		if strings.HasPrefix(template, "}}") {
			out.WriteByte('}')
			template = template[2:]
			continue
		}
		if template[0] == '{' {
			end := strings.IndexByte(template, '}')
			if end < 0 {
				return "", invalidTemplate()
			}
			value, ok := resolve(template[1:end])
			if !ok {
				return "", invalidTemplate()
			}
			out.WriteString(value)
			template = template[end+1:]
			continue
		}
		if template[0] == '}' {
			return "", invalidTemplate()
		}
		out.WriteByte(template[0])
		template = template[1:]
	}
	return out.String(), nil
}
func legacyName(m Content) string {
	v := sanitize(m.Title, 100)
	if m.Year != nil {
		v += fmt.Sprintf(" (%d)", *m.Year)
	}
	return v + fmt.Sprintf(" [tmdb-%d]", m.TMDbID)
}
func Destination(s Snapshot, episodes []Episode, source string) (string, error) {
	m := s.Content
	l := s.Library
	extension := strings.ToLower(path.Ext(source))
	switch extension {
	case ".mkv", ".mp4", ".m4v", ".avi", ".mov", ".webm", ".ts", ".m2ts":
	default:
		return "", problem("invalid_file", "Choose a registered video file.")
	}
	var relative string
	if m.Kind == "movie" {
		if l.Preset == "legacy" {
			name := legacyName(m)
			relative = path.Join(name, name)
		} else {
			var e error
			relative, e = render(l.MovieTemplate, m, nil)
			if e != nil {
				return "", e
			}
		}
	} else {
		if len(episodes) < 1 || len(episodes) > 20 {
			return "", problem("episode_mapping_required", "Map the file to 1–20 canonical episodes before organizing it.")
		}
		for n, e := range episodes {
			if e.Season < 0 || e.Number < 1 || n > 0 && (e.Season != episodes[n-1].Season || e.Number != episodes[n-1].Number+1) {
				return "", problem("episode_mapping_required", "Multi-episode files require consecutive canonical episodes in one season.")
			}
		}
		if l.Preset == "legacy" {
			label := fmt.Sprintf("S%02dE%02d", episodes[0].Season, episodes[0].Number)
			if len(episodes) > 1 {
				label += fmt.Sprintf("-E%02d", episodes[len(episodes)-1].Number)
			}
			base := sanitize(m.Title, 80) + " - " + label
			if s.LegacyEpisodeNaming == "with-title" {
				var titles []string
				for _, e := range episodes {
					titles = append(titles, e.Title)
				}
				base += " - " + sanitize(strings.Join(titles, " + "), 96)
			}
			relative = path.Join(legacyName(m), fmt.Sprintf("Season %02d", episodes[0].Season), base)
		} else {
			series, e := render(l.SeriesTemplate, m, nil)
			if e != nil {
				return "", e
			}
			episode, e := render(l.EpisodeTemplate, m, episodes)
			if e != nil {
				return "", e
			}
			relative = path.Join(series, episode)
		}
	}
	if m.Kind == "movie" && s.MovieVersion != "" {
		relative += " [" + sanitize(s.MovieVersion, 70) + "]"
	}
	destination := path.Join(leaf(l, m.Kind), relative+extension)
	if !validRelative(destination) {
		return "", problem("invalid_template", "The naming template produced an unsafe or overlong path. Shorten its folders or filename.")
	}
	return destination, nil
}
func render(template string, m Content, episodes []Episode) (string, error) {
	year := "Unknown"
	if m.Year != nil {
		year = strconv.Itoa(*m.Year)
	}
	values := map[string]string{"title": sanitize(m.Title, 100), "year": year, "tmdb_id": strconv.FormatInt(m.TMDbID, 10)}
	if len(episodes) > 0 {
		first, last := episodes[0], episodes[len(episodes)-1]
		values["season"] = strconv.Itoa(first.Season)
		values["season:02"] = fmt.Sprintf("%02d", first.Season)
		values["episode"] = strconv.Itoa(first.Number)
		values["episode:02"] = fmt.Sprintf("%02d", first.Number)
		values["episode_end"] = strconv.Itoa(last.Number)
		values["episode_end:02"] = fmt.Sprintf("%02d", last.Number)
		values["episode_range"] = ""
		if len(episodes) > 1 {
			values["episode_range"] = fmt.Sprintf("-E%02d", last.Number)
		}
		var titles []string
		for _, ep := range episodes {
			titles = append(titles, ep.Title)
		}
		values["episode_title"] = sanitize(strings.Join(titles, " + "), 100)
		if strings.Contains(template, "{absolute") {
			for n, e := range episodes {
				if e.Absolute == nil || *e.Absolute < 1 || n > 0 && *e.Absolute != *episodes[n-1].Absolute+1 {
					return "", problem("absolute_mapping_required", "Set explicit consecutive absolute numbers for every episode before using an absolute-number template.")
				}
			}
			values["absolute"] = strconv.Itoa(*first.Absolute)
			values["absolute:03"] = fmt.Sprintf("%03d", *first.Absolute)
			values["absolute_end"] = strconv.Itoa(*last.Absolute)
			values["absolute_end:03"] = fmt.Sprintf("%03d", *last.Absolute)
			values["absolute_range"] = ""
			if len(episodes) > 1 {
				values["absolute_range"] = fmt.Sprintf("-%03d", *last.Absolute)
			}
		}
	}
	out, e := expand(template, func(token string) (string, bool) { v, ok := values[token]; return v, ok })
	if e != nil || !validRelative(out) {
		return "", invalidTemplate()
	}
	return out, nil
}
