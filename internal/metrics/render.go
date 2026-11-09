package metrics

import (
	"math"
	"sort"
	"strconv"
	"strings"
)

// Render formats every recorded family in Prometheus text exposition format.
// Families and samples are sorted so output is deterministic for a quiescent
// registry; labels names are sorted when the metric is registered.
func (r *Registry) Render() string {
	if r == nil {
		return ""
	}
	r.mu.RLock()
	families := make([]*family, 0, len(r.families))
	for _, f := range r.families {
		families = append(families, f)
	}
	r.mu.RUnlock()
	sort.Slice(families, func(i, j int) bool { return families[i].name < families[j].name })

	var b strings.Builder
	for _, f := range families {
		samples := f.snapshot()
		if len(samples) == 0 {
			continue
		}
		b.WriteString("# HELP ")
		b.WriteString(f.name)
		b.WriteByte(' ')
		b.WriteString(escapeHelp(f.help))
		b.WriteByte('\n')
		b.WriteString("# TYPE ")
		b.WriteString(f.name)
		b.WriteByte(' ')
		b.WriteString(f.kind)
		b.WriteByte('\n')
		for _, sample := range samples {
			b.WriteString(f.name)
			writeLabels(&b, f.labelNames, sample.labels)
			b.WriteByte(' ')
			b.WriteString(formatFloat(sample.value))
			b.WriteByte('\n')
		}
	}
	return b.String()
}

type sample struct {
	labels []string
	value  float64
}

// snapshot copies and sorts one family's samples. Series labels are immutable
// after creation, so sharing the slice is safe.
func (f *family) snapshot() []sample {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]sample, 0, len(f.series))
	for _, s := range f.series {
		out = append(out, sample{labels: s.labels, value: s.value})
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i].labels, out[j].labels
		for k := 0; k < len(a) && k < len(b); k++ {
			if a[k] != b[k] {
				return a[k] < b[k]
			}
		}
		return len(a) < len(b)
	})
	return out
}

func writeLabels(b *strings.Builder, names, values []string) {
	if len(names) == 0 {
		return
	}
	b.WriteByte('{')
	for i, name := range names {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(name)
		b.WriteString(`="`)
		b.WriteString(escapeLabelValue(values[i]))
		b.WriteByte('"')
	}
	b.WriteByte('}')
}

var labelEscaper = strings.NewReplacer(
	`\`, `\\`,
	"\n", `\n`,
	`"`, `\"`,
)

var helpEscaper = strings.NewReplacer(
	`\`, `\\`,
	"\n", `\n`,
)

func escapeLabelValue(value string) string { return labelEscaper.Replace(value) }

func escapeHelp(help string) string { return helpEscaper.Replace(help) }

func formatFloat(value float64) string {
	switch {
	case math.IsInf(value, 1):
		return "+Inf"
	case math.IsInf(value, -1):
		return "-Inf"
	case math.IsNaN(value):
		return "NaN"
	default:
		return strconv.FormatFloat(value, 'g', -1, 64)
	}
}
