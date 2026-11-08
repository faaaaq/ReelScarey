package arrimport

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const maxCalendarBytes = 5 << 20

type calendarMovie struct {
	title string
	year  *int
	date  string
	kind  string
	mon   bool
}

var (
	calendarMovieRelease = regexp.MustCompile(`(?i)^(.*?)\s+\((theatrical|digital|physical|cinema) release\)$`)
	calendarMovieYear    = regexp.MustCompile(`^(.*?)\s+\(([0-9]{4})\)$`)
	calendarSeries       = regexp.MustCompile(`(?i)^(.+?)\s+-\s+(?:s[0-9]{1,3}e[0-9]{1,3}|[0-9]{1,3}x[0-9]{1,3})(?:\s+-\s+.*)?$`)
	calendarRadarrUID    = regexp.MustCompile(`(?i)^radarr_movie_([0-9]+)_`)
)

// ParseAny detects a calendar export by its header and otherwise parses a
// Radarr/Sonarr backup ZIP.
func ParseAny(ctx context.Context, archive string) (Backup, error) {
	f, err := os.Open(archive)
	if err != nil {
		return Backup{}, err
	}
	head := make([]byte, 512)
	n, _ := f.Read(head)
	f.Close()
	if bytes.Contains(head[:n], []byte("BEGIN:VCALENDAR")) {
		body, err := os.ReadFile(archive)
		if err != nil {
			return Backup{}, err
		}
		if len(body) > maxCalendarBytes {
			return Backup{}, fmt.Errorf("the calendar is larger than %d MiB", maxCalendarBytes>>20)
		}
		return ParseCalendar(body)
	}
	return Parse(ctx, archive)
}

// ParseCalendar reads a bounded iCalendar export. It keeps only the summary,
// start date and UID of each event; descriptions, categories and URLs are
// ignored. Radarr's release-kind suffix is stripped and duplicate release
// events for one movie are collapsed to the latest date.
func ParseCalendar(body []byte) (Backup, error) {
	if len(body) == 0 || len(body) > maxCalendarBytes {
		return Backup{}, fmt.Errorf("the calendar must be between 1 byte and %d MiB", maxCalendarBytes>>20)
	}
	if !bytes.Contains(body, []byte("BEGIN:VCALENDAR")) {
		return Backup{}, fmt.Errorf("the file is not an iCalendar export")
	}
	lines := unfoldCalendar(body)
	source := ""
	var summary string
	var date string
	var uid string
	inEvent := false
	events := 0
	movies := map[string]*calendarMovie{}
	series := map[string]*Candidate{}
	for _, line := range lines {
		colon := strings.IndexByte(line, ':')
		key, value := line, ""
		if colon >= 0 {
			key, value = line[:colon], line[colon+1:]
			if semi := strings.IndexByte(key, ';'); semi >= 0 {
				key = key[:semi]
			}
		}
		key = strings.ToUpper(strings.TrimSpace(key))
		value = unescapeCalendar(strings.TrimSpace(value))
		switch {
		case key == "X-WR-CALNAME":
			lower := strings.ToLower(value)
			if strings.Contains(lower, "radarr") {
				source = "radarr-ical"
			} else if strings.Contains(lower, "sonarr") {
				source = "sonarr-ical"
			}
		case key == "BEGIN" && strings.EqualFold(value, "VEVENT"):
			inEvent = true
			summary, date, uid = "", "", ""
		case key == "END" && strings.EqualFold(value, "VEVENT") && inEvent:
			inEvent = false
			events++
			if events > maxItems {
				return Backup{}, fmt.Errorf("the calendar has more than %d events", maxItems)
			}
			addCalendarEvent(source, summary, date, uid, movies, series)
		case inEvent && key == "SUMMARY":
			summary = value
		case inEvent && key == "DTSTART":
			date = calendarDate(value)
		case inEvent && key == "UID":
			uid = value
		}
	}
	if source == "" {
		if len(series) > 0 && len(movies) == 0 {
			source = "sonarr-ical"
		} else if len(movies) > 0 && len(series) == 0 {
			source = "radarr-ical"
		} else if len(movies) > 0 || len(series) > 0 {
			source = "ical"
		} else {
			return Backup{}, fmt.Errorf("the calendar has no movie or series events")
		}
	}
	out := make([]Candidate, 0, len(movies)+len(series))
	for _, m := range movies {
		out = append(out, Candidate{Kind: "movie", Title: m.title, Year: m.year, Monitored: m.mon, Date: m.date, ReleaseKind: m.kind})
	}
	for _, s := range series {
		out = append(out, *s)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return strings.ToLower(out[i].Title) < strings.ToLower(out[j].Title)
	})
	return Backup{Source: source, Items: out}, nil
}

func addCalendarEvent(source, summary, date, uid string, movies map[string]*calendarMovie, series map[string]*Candidate) {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return
	}
	if m := calendarSeries.FindStringSubmatch(summary); len(m) == 2 {
		title := strings.TrimSpace(m[1])
		if title != "" {
			if existing, ok := series[strings.ToLower(title)]; !ok {
				series[strings.ToLower(title)] = &Candidate{Kind: "series", Title: title, Monitored: true, Date: date}
			} else if date != "" && (existing.Date == "" || date < existing.Date) {
				existing.Date = date
			}
		}
		return
	}
	title, kind := summary, ""
	if m := calendarMovieRelease.FindStringSubmatch(summary); len(m) == 3 {
		title, kind = strings.TrimSpace(m[1]), strings.ToLower(m[2])+" release"
	}
	var year *int
	if m := calendarMovieYear.FindStringSubmatch(title); len(m) == 3 {
		title = strings.TrimSpace(m[1])
		if y, err := strconv.Atoi(m[2]); err == nil {
			year = &y
		}
	}
	if title == "" {
		return
	}
	key := strings.ToLower(title)
	if m := calendarRadarrUID.FindStringSubmatch(uid); len(m) == 2 {
		key = "radarr:" + m[1]
	}
	if year == nil && date != "" {
		if y, err := strconv.Atoi(date[:4]); err == nil {
			year = &y
		}
	}
	m, ok := movies[key]
	if !ok {
		movies[key] = &calendarMovie{title: title, year: year, date: date, kind: kind, mon: true}
		return
	}
	if date >= m.date {
		m.date, m.kind, m.year = date, kind, year
	}
}

func unfoldCalendar(body []byte) []string {
	scanner := bufio.NewScanner(bytes.NewReader(body))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lines := []string{}
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') && len(lines) > 0 {
			lines[len(lines)-1] += strings.TrimLeft(line, " \t")
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

func calendarDate(value string) string {
	value = strings.TrimSpace(value)
	if len(value) < 8 {
		return ""
	}
	value = value[:8]
	if _, err := time.Parse("20060102", value); err != nil {
		return ""
	}
	return value[:4] + "-" + value[4:6] + "-" + value[6:8]
}

func unescapeCalendar(value string) string {
	var out strings.Builder
	for i := 0; i < len(value); i++ {
		if value[i] != '\\' || i+1 >= len(value) {
			out.WriteByte(value[i])
			continue
		}
		i++
		switch value[i] {
		case 'n', 'N':
			out.WriteByte('\n')
		case ',', ';', '\\':
			out.WriteByte(value[i])
		default:
			out.WriteByte('\\')
			out.WriteByte(value[i])
		}
	}
	return out.String()
}
