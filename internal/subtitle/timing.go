package subtitle

import (
	"bytes"
	"fmt"
	"math"
	"path"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ShiftTimedText preserves cue payload and ASS styles; only timestamp fields
// change. It refuses negative times rather than silently clipping dialogue.
func ShiftTimedText(body []byte, extension string, offsetMS int) ([]byte, int, error) {
	if offsetMS < -600000 || offsetMS > 600000 {
		return nil, 0, problem("invalid_subtitle", "Timing adjustments are limited to ten minutes.")
	}
	return transformTimedText(body, extension, func(ms int64) int64 { return ms + int64(offsetMS) })
}

func transformTimedText(body []byte, extension string, transform func(int64) int64) ([]byte, int, error) {
	bad := func() ([]byte, int, error) {
		return nil, 0, problem("invalid_subtitle", "Choose valid UTF-8 SRT, VTT, ASS or SSA with supported cue times; shifts must keep every cue at or after zero.")
	}
	if len(body) == 0 || len(body) > MaxSubtitle || !utf8.Valid(body) || bytes.ContainsRune(body, 0) {
		return bad()
	}
	body = bytes.ReplaceAll(bytes.TrimPrefix(body, []byte{239, 187, 191}), []byte("\r\n"), []byte("\n"))
	if bytes.ContainsRune(body, '\r') {
		return bad()
	}
	ext := strings.ToLower(extension)
	if ext != ".srt" && ext != ".vtt" && ext != ".ass" && ext != ".ssa" {
		return bad()
	}
	if ext == ".vtt" && !bytes.HasPrefix(body, []byte("WEBVTT")) {
		return bad()
	}
	if ext == ".srt" && bytes.HasPrefix(body, []byte("WEBVTT")) {
		return bad()
	}
	lines := strings.Split(string(body), "\n")
	vttTimes := map[int]bool{}
	if ext == ".vtt" {
		indices, ok := vttCueLines(lines)
		if !ok {
			return bad()
		}
		for _, i := range indices {
			vttTimes[i] = true
		}
	}
	count := 0
	events := false
	startIndex, endIndex := -1, -1
	stamp := func(ms int64, ass bool) string {
		if ass {
			return fmt.Sprintf("%d:%02d:%02d.%02d", ms/3600000, ms/60000%60, ms/1000%60, ms/10%100)
		}
		separator := "."
		if ext == ".srt" {
			separator = ","
		}
		return fmt.Sprintf("%02d:%02d:%02d%s%03d", ms/3600000, ms/60000%60, ms/1000%60, separator, ms%1000)
	}
	parse := func(value string, ass bool) (int64, bool) {
		if ass {
			parts := strings.Split(value, ":")
			if len(parts) != 3 {
				return 0, false
			}
			h, e := strconv.Atoi(parts[0])
			m, e2 := strconv.Atoi(parts[1])
			sec, e3 := strconv.ParseFloat(parts[2], 64)
			if e != nil || e2 != nil || e3 != nil || h < 0 || h > 99 || m < 0 || m >= 60 || sec < 0 || sec >= 60 {
				return 0, false
			}
			return int64(math.Round((float64(h*3600+m*60) + sec) * 1000)), true
		}
		v, ok := timestamp(value)
		return int64(math.Round(v * 1000)), ok
	}
	for i, line := range lines {
		if len(line) > 65536 {
			return bad()
		}
		if ext == ".vtt" && !vttTimes[i] {
			continue
		}
		if ext == ".ass" || ext == ".ssa" {
			trim := strings.TrimSpace(line)
			if strings.HasPrefix(trim, "[") {
				events = strings.EqualFold(trim, "[Events]")
				continue
			}
			if !events {
				continue
			}
			if strings.HasPrefix(strings.ToLower(trim), "format:") {
				fields := strings.Split(strings.TrimSpace(trim[len("format:"):]), ",")
				startIndex, endIndex = -1, -1
				for n, name := range fields {
					switch strings.ToLower(strings.TrimSpace(name)) {
					case "start":
						startIndex = n
					case "end":
						endIndex = n
					}
				}
				continue
			}
			if !strings.HasPrefix(strings.ToLower(trim), "dialogue:") {
				continue
			}
			if startIndex < 0 || endIndex < 0 {
				return bad()
			}
			prefix := strings.Index(line, ":") + 1
			fields := strings.SplitN(line[prefix:], ",", max(startIndex, endIndex)+2)
			if len(fields) <= max(startIndex, endIndex) {
				return bad()
			}
			a, ok := parse(strings.TrimSpace(fields[startIndex]), true)
			b, ok2 := parse(strings.TrimSpace(fields[endIndex]), true)
			a = transform(a)
			b = transform(b)
			if !ok || !ok2 || a < 0 || b <= a || b > 360000000 {
				return bad()
			}
			fields[startIndex] = stamp(a, true)
			fields[endIndex] = stamp(b, true)
			lines[i] = line[:prefix] + strings.Join(fields, ",")
			count++
			continue
		}
		if !strings.Contains(line, "-->") {
			continue
		}
		m := cueLine.FindStringSubmatchIndex(line)
		if m == nil {
			return bad()
		}
		a, ok := parse(line[m[2]:m[3]], false)
		b, ok2 := parse(line[m[4]:m[5]], false)
		a = transform(a)
		b = transform(b)
		if !ok || !ok2 || a < 0 || b <= a || b > 360000000 || i+1 >= len(lines) || strings.TrimSpace(lines[i+1]) == "" {
			return bad()
		}
		lines[i] = line[:m[2]] + stamp(a, false) + line[m[3]:m[4]] + stamp(b, false) + line[m[5]:]
		count++
	}
	if count == 0 {
		return bad()
	}
	out := []byte(strings.Join(lines, "\n"))
	if len(out) > MaxSubtitle {
		return bad()
	}
	return out, count, nil
}

func subtitleExtension(name string) string { return strings.ToLower(path.Ext(name)) }
