package music

import (
	"strings"
	"unicode"
)

// unicodeEquivalents folds the compatibility characters that taggers and
// MusicBrainz routinely disagree on: typographic quotes and dashes, narrow and
// non-breaking spaces, and a few presentation forms. It does not translate
// words or infer aliases.
var unicodeEquivalents = strings.NewReplacer(
	"\u00a0", " ", "\u1680", " ", "\u2000", " ", "\u2001", " ", "\u2002", " ",
	"\u2003", " ", "\u2004", " ", "\u2005", " ", "\u2006", " ", "\u2007", " ",
	"\u2008", " ", "\u2009", " ", "\u200a", " ", "\u202f", " ", "\u205f", " ",
	"\u3000", " ",
	"\u2018", "'", "\u2019", "'", "\u201a", "'", "\u201b", "'", "\u2032", "'",
	"\u201c", "\"", "\u201d", "\"", "\u201e", "\"", "\u201f", "\"", "\u2033", "\"",
	"\u00ab", "\"", "\u00bb", "\"",
	"\u2010", "-", "\u2011", "-", "\u2012", "-", "\u2013", "-", "\u2014", "-",
	"\u2015", "-", "\u2212", "-",
	"\u2026", "...",
	"\u2016", "|",
)

// latinFolds maps precomposed Latin letters to their unaccented form. Combined
// marks are dropped separately, so both NFC and NFD input fold to the same key.
// This is deliberately a fixed table instead of a new Unicode dependency.
var latinFolds = map[rune]string{
	'À': "a", 'Á': "a", 'Â': "a", 'Ã': "a", 'Ä': "a", 'Å': "a", 'Æ': "ae",
	'Ç': "c", 'È': "e", 'É': "e", 'Ê': "e", 'Ë': "e", 'Ì': "i", 'Í': "i",
	'Î': "i", 'Ï': "i", 'Ð': "d", 'Ñ': "n", 'Ò': "o", 'Ó': "o", 'Ô': "o",
	'Õ': "o", 'Ö': "o", 'Ø': "o", 'Ù': "u", 'Ú': "u", 'Û': "u", 'Ü': "u",
	'Ý': "y", 'Þ': "th", 'ß': "ss",
	'à': "a", 'á': "a", 'â': "a", 'ã': "a", 'ä': "a", 'å': "a", 'æ': "ae",
	'ç': "c", 'è': "e", 'é': "e", 'ê': "e", 'ë': "e", 'ì': "i", 'í': "i",
	'î': "i", 'ï': "i", 'ð': "d", 'ñ': "n", 'ò': "o", 'ó': "o", 'ô': "o",
	'õ': "o", 'ö': "o", 'ø': "o", 'ù': "u", 'ú': "u", 'û': "u", 'ü': "u",
	'ý': "y", 'þ': "th", 'ÿ': "y",
	'Ā': "a", 'ā': "a", 'Ă': "a", 'ă': "a", 'Ą': "a", 'ą': "a",
	'Ć': "c", 'ć': "c", 'Ĉ': "c", 'ĉ': "c", 'Ċ': "c", 'ċ': "c", 'Č': "c", 'č': "c",
	'Ď': "d", 'ď': "d", 'Đ': "d", 'đ': "d",
	'Ē': "e", 'ē': "e", 'Ĕ': "e", 'ĕ': "e", 'Ė': "e", 'ė': "e",
	'Ę': "e", 'ę': "e", 'Ě': "e", 'ě': "e",
	'Ĝ': "g", 'ĝ': "g", 'Ğ': "g", 'ğ': "g", 'Ġ': "g", 'ġ': "g", 'Ģ': "g", 'ģ': "g",
	'Ĥ': "h", 'ĥ': "h", 'Ħ': "h", 'ħ': "h",
	'Ĩ': "i", 'ĩ': "i", 'Ī': "i", 'ī': "i", 'Ĭ': "i", 'ĭ': "i",
	'Į': "i", 'į': "i", 'İ': "i", 'ı': "i",
	'Ĳ': "ij", 'ĳ': "ij", 'Ĵ': "j", 'ĵ': "j", 'Ķ': "k", 'ķ': "k",
	'Ĺ': "l", 'ĺ': "l", 'Ļ': "l", 'ļ': "l", 'Ľ': "l", 'ľ': "l",
	'Ŀ': "l", 'ŀ': "l", 'Ł': "l", 'ł': "l",
	'Ń': "n", 'ń': "n", 'Ņ': "n", 'ņ': "n", 'Ň': "n", 'ň': "n",
	'ŉ': "n", 'Ŋ': "n", 'ŋ': "n",
	'Ō': "o", 'ō': "o", 'Ŏ': "o", 'ŏ': "o", 'Ő': "o", 'ő': "o",
	'Œ': "oe", 'œ': "oe",
	'Ŕ': "r", 'ŕ': "r", 'Ŗ': "r", 'ŗ': "r", 'Ř': "r", 'ř': "r",
	'Ś': "s", 'ś': "s", 'Ŝ': "s", 'ŝ': "s", 'Ş': "s", 'ş': "s", 'Š': "s", 'š': "s",
	'Ţ': "t", 'ţ': "t", 'Ť': "t", 'ť': "t", 'Ŧ': "t", 'ŧ': "t",
	'Ũ': "u", 'ũ': "u", 'Ū': "u", 'ū': "u", 'Ŭ': "u", 'ŭ': "u",
	'Ů': "u", 'ů': "u", 'Ű': "u", 'ű': "u", 'Ų': "u", 'ų': "u",
	'Ŵ': "w", 'ŵ': "w", 'Ŷ': "y", 'ŷ': "y", 'Ÿ': "y",
	'Ź': "z", 'ź': "z", 'Ż': "z", 'ż': "z", 'Ž': "z", 'ž': "z",
	'Ș': "s", 'ș': "s", 'Ț': "t", 'ț': "t",
	'Ǎ': "a", 'ǎ': "a", 'Ǐ': "i", 'ǐ': "i", 'Ǒ': "o", 'ǒ': "o",
	'Ǔ': "u", 'ǔ': "u", 'Ǖ': "u", 'ǖ': "u", 'Ǘ': "u", 'ǘ': "u",
	'Ǚ': "u", 'ǚ': "u", 'Ǜ': "u", 'ǜ': "u",
}

// normalizeMatchName returns a comparison key for album and track titles. It
// folds case, compatibility punctuation, accents, punctuation and repeated
// whitespace. It is evidence for an exact comparison, never an identity.
func normalizeMatchName(value string) string {
	value = unicodeEquivalents.Replace(value)
	var out strings.Builder
	out.Grow(len(value))
	for _, r := range value {
		if fold, ok := latinFolds[r]; ok {
			out.WriteString(fold)
			continue
		}
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			out.WriteRune(unicode.ToLower(r))
		default:
			out.WriteByte(' ')
		}
	}
	return strings.Join(strings.Fields(out.String()), " ")
}

func compactMatchName(value string) string {
	return strings.ReplaceAll(normalizeMatchName(value), " ", "")
}

// matchNameEqual compares two already-normalized comparison keys. The compact
// form treats punctuation and spacing differences such as "AC/DC" and "ACDC"
// as equivalent.
func matchNameEqual(left, right string) bool {
	l, r := normalizeMatchName(left), normalizeMatchName(right)
	if l == "" || r == "" {
		return false
	}
	return l == r || compactMatchName(l) == compactMatchName(r)
}

func isVariousArtistsKey(name string) bool {
	switch name {
	case "various artists", "various", "v a":
		return true
	}
	return false
}

// normalizeArtistName folds the capitalization, punctuation and common aliases
// of an artist name. It is only used for comparison; the MusicBrainz name is
// always what gets stored.
func normalizeArtistName(value string) string {
	name := normalizeMatchName(value)
	if isVariousArtistsKey(name) {
		return "various artists"
	}
	return name
}

// trimArticle removes a leading English article for comparison. A name that
// would become empty keeps its original form.
func trimArticle(name string) string {
	trimmed := strings.TrimPrefix(name, "the ")
	if trimmed == "" || trimmed == "the" {
		return name
	}
	return trimmed
}

func artistNamesEqual(left, right string) bool {
	l, r := normalizeArtistName(left), normalizeArtistName(right)
	if l == "" || r == "" {
		return false
	}
	if l == r {
		return true
	}
	if l == "various artists" || r == "various artists" {
		return false
	}
	return trimArticle(l) == trimArticle(r)
}

// artistGroupKey groups files by album artist without merging distinct names.
func artistGroupKey(value string) string {
	name := normalizeArtistName(value)
	if name == "various artists" {
		return name
	}
	return trimArticle(name)
}

// primaryArtistName returns the artist before a featured credit. It is used
// only as a fallback name-search candidate when the tagged album artist is the
// same as the track artist.
func primaryArtistName(value string) (string, bool) {
	words := strings.Fields(value)
	for i := 1; i < len(words); i++ {
		switch strings.ToLower(strings.Trim(words[i], ".")) {
		case "feat", "ft", "featuring":
			primary := strings.Join(words[:i], " ")
			if normalizeMatchName(primary) != "" {
				return primary, true
			}
		}
	}
	return "", false
}
