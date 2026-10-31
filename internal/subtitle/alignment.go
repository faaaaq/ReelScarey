package subtitle

import (
	"fmt"
	"math"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

type alignmentCue struct {
	text  string
	start float64
}

var cueMarkup = regexp.MustCompile(`<[^>]*>|\{[^}]*\}`)

func cueKey(raw string) string {
	raw = cueMarkup.ReplaceAllString(raw, "")
	raw = strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(raw, `\N`, " "), `\n`, " "), `\h`, " ")
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, raw)
}
func alignmentCues(body []byte, extension string) ([]alignmentCue, error) {
	data, e := normalizeProviderSubtitle(body, strings.TrimPrefix(extension, "."), 0)
	if e != nil {
		return nil, e
	}
	lines := strings.Split(string(data), "\n")
	out := []alignmentCue{}
	appendCue := func(text string, start float64) {
		key := cueKey(text)
		if len([]rune(key)) >= 12 {
			out = append(out, alignmentCue{key, start})
		}
	}
	if extension == ".ass" || extension == ".ssa" {
		events := false
		start, text, fields := -1, -1, 0
		for _, line := range lines {
			trim := strings.TrimSpace(line)
			lower := strings.ToLower(trim)
			if strings.HasPrefix(trim, "[") {
				events = strings.EqualFold(trim, "[Events]")
				continue
			}
			if !events {
				continue
			}
			if strings.HasPrefix(lower, "format:") {
				names := strings.Split(trim[len("format:"):], ",")
				fields = len(names)
				for i, n := range names {
					switch strings.ToLower(strings.TrimSpace(n)) {
					case "start":
						start = i
					case "text":
						text = i
					}
				}
				continue
			}
			if strings.HasPrefix(lower, "dialogue:") && start >= 0 && text >= 0 {
				v := strings.SplitN(trim[len("dialogue:"):], ",", fields)
				if len(v) == fields {
					ms, ok := parseASSTime(strings.TrimSpace(v[start]))
					if ok {
						appendCue(v[text], float64(ms))
					}
				}
			}
		}
	} else {
		indices := []int{}
		if extension == ".vtt" {
			indices, _ = vttCueLines(lines)
		} else {
			for i, line := range lines {
				if cueLine.MatchString(line) {
					indices = append(indices, i)
				}
			}
		}
		for _, i := range indices {
			match := cueLine.FindStringSubmatch(lines[i])
			start, _ := timestamp(match[1])
			end := i + 1
			for end < len(lines) && strings.TrimSpace(lines[end]) != "" {
				end++
			}
			appendCue(strings.Join(lines[i+1:end], " "), start*1000)
		}
	}
	return out, nil
}
func median(values []float64) float64 {
	slices.Sort(values)
	n := len(values)
	if n%2 == 1 {
		return values[n/2]
	}
	return (values[n/2-1] + values[n/2]) / 2
}

// AlignTimedText estimates one offset and a small uniform timing drift from
// unique identical dialogue. It never translates or uses provider/network IO.
func AlignTimedText(body []byte, extension string, reference []byte, referenceExtension string) ([]byte, string, error) {
	bad := func() ([]byte, string, error) {
		return nil, "", problem("invalid_subtitle", "Reference alignment needs at least six unique matching dialogue cues spanning a minute, with at least 80% consistent timing. Use a matching cut and language, or adjust timing manually.")
	}
	a, e := alignmentCues(body, extension)
	if e != nil {
		return nil, "", e
	}
	b, e := alignmentCues(reference, referenceExtension)
	if e != nil {
		return nil, "", e
	}
	unique := func(in []alignmentCue) map[string]float64 {
		out := map[string]float64{}
		for _, c := range in {
			if _, ok := out[c.text]; ok {
				out[c.text] = -1
			} else {
				out[c.text] = c.start
			}
		}
		return out
	}
	left, right := unique(a), unique(b)
	type pair struct{ x, y float64 }
	pairs := []pair{}
	for _, c := range a {
		if x, ok := left[c.text]; ok && x >= 0 {
			if y, ok := right[c.text]; ok && y >= 0 {
				pairs = append(pairs, pair{x, y})
			}
		}
	}
	slices.SortFunc(pairs, func(a, b pair) int {
		if a.x < b.x {
			return -1
		}
		if a.x > b.x {
			return 1
		}
		return 0
	})
	if len(pairs) < 6 || len(pairs)*5 < min(len(a), len(b))*3 || pairs[len(pairs)-1].x-pairs[0].x < 60000 {
		return bad()
	}
	sample := pairs
	if len(sample) > 100 {
		sample = make([]pair, 100)
		for i := range sample {
			sample[i] = pairs[i*(len(pairs)-1)/99]
		}
	}
	slopes := []float64{}
	for i, p := range sample {
		for _, q := range sample[i+1:] {
			if q.x-p.x >= 30000 {
				slopes = append(slopes, (q.y-p.y)/(q.x-p.x))
			}
		}
	}
	if len(slopes) == 0 {
		return bad()
	}
	scale := median(slopes)
	if math.IsNaN(scale) || math.IsInf(scale, 0) || scale < 0.95 || scale > 1.05 {
		return bad()
	}
	offsets := make([]float64, len(pairs))
	for i, p := range pairs {
		offsets[i] = p.y - scale*p.x
	}
	offset := median(offsets)
	if math.Abs(offset) > 600000 {
		return bad()
	}
	matched := 0
	first, last := -1.0, -1.0
	previous := -1.0
	for _, p := range pairs {
		if math.Abs(p.y-(scale*p.x+offset)) <= 150 {
			if p.y < previous {
				return bad()
			}
			previous = p.y
			matched++
			if first < 0 {
				first = p.x
			}
			last = p.x
		}
	}
	if matched*5 < len(pairs)*4 || matched < 6 || last-first < 60000 {
		return bad()
	}
	output, _, e := transformTimedText(body, extension, func(ms int64) int64 { return int64(math.Round(float64(ms)*scale + offset)) })
	if e != nil {
		return nil, "", e
	}
	if _, e = normalizeProviderSubtitle(output, strings.TrimPrefix(extension, "."), 0); e != nil {
		return nil, "", e
	}
	return output, fmt.Sprintf("Reference alignment: %d of %d matching cues; %.0f ms starting offset; %.6f timing scale. Review required; the original will be retained.", matched, len(pairs), offset, scale), nil
}
