package subtitle

import (
	"bytes"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

func supportedSubtitleFormat(format string) bool {
	return format == "srt" || format == "vtt" || format == "ass" || format == "ssa"
}
func candidateFormat(p Preferences, c Candidate) string {
	if p.DownloadFormat == "" || p.DownloadFormat == "srt" {
		return "srt"
	}
	if p.DownloadFormat != "original" {
		return ""
	}
	format := strings.TrimPrefix(subtitleExtension(c.FileName), ".")
	if !supportedSubtitleFormat(format) {
		return ""
	}
	return format
}

var assTime = regexp.MustCompile(`^[0-9]{1,2}:[0-5][0-9]:[0-5][0-9]\.[0-9]{1,2}$`)

// Locate VTT cue timing lines without treating comments, styles, regions or
// literal arrows in cue text as additional cues. Shared by import and edits.
func vttCueLines(lines []string) ([]int, bool) {
	if len(lines) == 0 || (lines[0] != "WEBVTT" && !strings.HasPrefix(lines[0], "WEBVTT ") && !strings.HasPrefix(lines[0], "WEBVTT\t")) {
		return nil, false
	}
	for _, line := range lines {
		if len(line) > 65536 {
			return nil, false
		}
	}
	i := 1
	for i < len(lines) && strings.TrimSpace(lines[i]) != "" {
		if strings.Contains(lines[i], "-->") {
			return nil, false
		}
		i++
	}
	out := []int{}
	for i < len(lines) {
		for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
			i++
		}
		if i >= len(lines) {
			break
		}
		start := i
		for i < len(lines) && strings.TrimSpace(lines[i]) != "" {
			i++
		}
		first := lines[start]
		if first == "NOTE" || strings.HasPrefix(first, "NOTE ") || strings.HasPrefix(first, "NOTE\t") {
			continue
		}
		if first == "STYLE" || first == "REGION" {
			if len(out) > 0 {
				return nil, false
			}
			continue
		}
		cue := start
		if !strings.Contains(first, "-->") {
			cue++
		}
		if cue+1 >= i || cueLine.FindStringSubmatch(lines[cue]) == nil {
			return nil, false
		}
		out = append(out, cue)
		if len(out) > 50000 {
			return nil, false
		}
	}
	return out, len(out) > 0
}

func parseASSTime(value string) (int64, bool) {
	if !assTime.MatchString(value) {
		return 0, false
	}
	parts := strings.Split(value, ":")
	h, _ := strconv.Atoi(parts[0])
	m, _ := strconv.Atoi(parts[1])
	s, _ := strconv.ParseFloat(parts[2], 64)
	return int64(math.Round((float64(h*3600+m*60) + s) * 1000)), true
}

// Provider bytes keep their cue text, styles, event order and timestamps. Only
// a UTF-8 BOM and CRLF are normalized, as with the existing SRT importer.
func normalizeProviderSubtitle(body []byte, format string, duration float64) ([]byte, error) {
	if format == "" || format == "srt" {
		return normalizeSRT(body, duration)
	}
	invalid := func() ([]byte, error) {
		return nil, problem("invalid_subtitle", "The provider did not return valid UTF-8 "+strings.ToUpper(format)+" cues with timing that fits this video.")
	}
	if !supportedSubtitleFormat(format) || len(body) == 0 || len(body) > MaxSubtitle || !utf8.Valid(body) || bytes.ContainsRune(body, 0) {
		return invalid()
	}
	body = bytes.ReplaceAll(bytes.TrimPrefix(body, []byte{239, 187, 191}), []byte("\r\n"), []byte("\n"))
	if bytes.ContainsRune(body, '\r') {
		return invalid()
	}
	lines := strings.Split(string(body), "\n")
	count := 0
	if format == "vtt" {
		cues, valid := vttCueLines(lines)
		if !valid {
			return invalid()
		}
		for _, i := range cues {
			m := cueLine.FindStringSubmatch(lines[i])
			a, ok := timestamp(m[1])
			b, ok2 := timestamp(m[2])
			if strings.Contains(m[1], ",") || strings.Contains(m[2], ",") || !ok || !ok2 || a < 0 || b <= a || b > 360000 || (duration > 0 && b > duration+180) || i+1 >= len(lines) || strings.TrimSpace(lines[i+1]) == "" {
				return invalid()
			}
			count++
		}
	} else {
		script, events := false, false
		start, end, text, fields := -1, -1, -1, 0
		for _, line := range lines {
			if len(line) > 65536 {
				return invalid()
			}
			trim := strings.TrimSpace(line)
			lower := strings.ToLower(trim)
			if trim == "" || strings.HasPrefix(trim, ";") {
				continue
			}
			if !script {
				if !strings.EqualFold(trim, "[Script Info]") {
					return invalid()
				}
				script = true
				continue
			}
			if strings.HasPrefix(trim, "[") {
				events = strings.EqualFold(trim, "[Events]")
				start, end, text, fields = -1, -1, -1, 0
				continue
			}
			if !events {
				continue
			}
			if strings.HasPrefix(lower, "format:") {
				names := strings.Split(strings.TrimSpace(trim[len("format:"):]), ",")
				if len(names) < 3 || len(names) > 32 {
					return invalid()
				}
				start, end, text = -1, -1, -1
				seen := map[string]bool{}
				for i, name := range names {
					name = strings.ToLower(strings.TrimSpace(name))
					if name == "" || seen[name] {
						return invalid()
					}
					seen[name] = true
					switch name {
					case "start":
						start = i
					case "end":
						end = i
					case "text":
						text = i
					}
				}
				fields = len(names)
				if start < 0 || end < 0 || text != fields-1 {
					return invalid()
				}
				continue
			}
			if !strings.HasPrefix(lower, "dialogue:") {
				continue
			}
			if start < 0 || end < 0 || text < 0 {
				return invalid()
			}
			values := strings.SplitN(strings.TrimSpace(trim[len("dialogue:"):]), ",", fields)
			if len(values) != fields || strings.TrimSpace(values[text]) == "" {
				return invalid()
			}
			a, ok := parseASSTime(strings.TrimSpace(values[start]))
			b, ok2 := parseASSTime(strings.TrimSpace(values[end]))
			if !ok || !ok2 || a < 0 || b <= a || b > 360000000 || (duration > 0 && float64(b) > 1000*(duration+180)) {
				return invalid()
			}
			count++
		}
	}
	if count == 0 || count > 50000 {
		return invalid()
	}
	return body, nil
}
