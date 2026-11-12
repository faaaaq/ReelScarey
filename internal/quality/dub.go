package quality

import (
	"errors"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// DefaultDubRule is the editable rule used when a show is marked English-dub
// only. Regex terms use /.../i; every other line is a case-insensitive phrase.
// A !-prefixed line is an exclusion: a release naming another dub language
// never matches the rule, so a Dual Audio release without English is rejected.
const DefaultDubRule = `/([\[\(][Ee]nglish[\W]?([aA-zZ ]+)?[Dd]ubs?|FuniDub|Dual( |-|.)?Audio|\[Dual\])/i
/([\[\(][Ee]ng[\W]?([aA-zZ ]+)?[Dd]ubs?|FuniDub|Dual( |-|.)?Audio|\[Dual\])/i
Eng Dub
Eng Dubs
English Dub
English Dubs
dual-audio
Dual-Audio
!/\b(french|fra|fre)\b/i
!/\b(spanish|spa|esp)\b/i
!/\b(portuguese|portugu[eê]s|pob|por[- ]br|por)\b/i
!/\b(german|ger|deu)\b/i
!/\b(italian|ita)\b/i
!/\b(korean|kor)\b/i
!/\b(chinese|mandarin|cantonese|chi|zho|zh)\b/i
!/\b(russian|rus)\b/i`

var ErrDubRule = errors.New("The English-dub rule must contain at most 100 regex or phrase lines, each regex must compile, /.../i is the only supported regex form, and a !-prefixed line excludes matching releases.")

type DubRule struct {
	patterns         []*regexp.Regexp
	phrases          []string
	excludedPatterns []*regexp.Regexp
	excludedPhrases  []string
}

func ParseDubRule(text string) (*DubRule, error) {
	if len(text) > 8192 || !utf8.ValidString(text) {
		return nil, ErrDubRule
	}
	out := &DubRule{}
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.ContainsFunc(line, unicode.IsControl) {
			return nil, ErrDubRule
		}
		excluded := strings.HasPrefix(line, "!")
		if excluded {
			line = strings.TrimSpace(strings.TrimPrefix(line, "!"))
			if line == "" {
				return nil, ErrDubRule
			}
		}
		if strings.HasPrefix(line, "/") {
			end := strings.LastIndex(line, "/")
			if end <= 0 {
				return nil, ErrDubRule
			}
			pattern := line[1:end]
			flags := strings.TrimSpace(line[end+1:])
			if pattern == "" || (flags != "" && !strings.EqualFold(flags, "i")) {
				return nil, ErrDubRule
			}
			re, err := regexp.Compile("(?i)" + pattern)
			if err != nil {
				return nil, ErrDubRule
			}
			if excluded {
				out.excludedPatterns = append(out.excludedPatterns, re)
			} else {
				out.patterns = append(out.patterns, re)
			}
		} else {
			if excluded {
				out.excludedPhrases = append(out.excludedPhrases, strings.ToLower(line))
			} else {
				out.phrases = append(out.phrases, strings.ToLower(line))
			}
		}
		if len(out.patterns)+len(out.phrases)+len(out.excludedPatterns)+len(out.excludedPhrases) > 100 {
			return nil, ErrDubRule
		}
	}
	if len(out.patterns)+len(out.phrases)+len(out.excludedPatterns)+len(out.excludedPhrases) == 0 {
		return nil, ErrDubRule
	}
	return out, nil
}

func (r *DubRule) Matches(title string) bool {
	if r == nil {
		return false
	}
	lower := strings.ToLower(title)
	for _, phrase := range r.excludedPhrases {
		if strings.Contains(lower, phrase) {
			return false
		}
	}
	for _, re := range r.excludedPatterns {
		if re.MatchString(title) {
			return false
		}
	}
	for _, re := range r.patterns {
		if re.MatchString(title) {
			return true
		}
	}
	for _, phrase := range r.phrases {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}
