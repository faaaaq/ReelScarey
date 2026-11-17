package metadata

import "strings"

// ISO 639-2 bibliographic aliases are accepted in addition to the
// terminological codes generated from the ISO language identifiers.
var alternateLanguages = map[string]string{
	"alb": "sq", "arm": "hy", "baq": "eu", "bur": "my", "chi": "zh", "cze": "cs",
	"dut": "nl", "fre": "fr", "geo": "ka", "ger": "de", "gre": "el", "ice": "is",
	"mac": "mk", "mao": "mi", "may": "ms", "per": "fa", "rum": "ro", "slo": "sk",
	"tib": "bo", "wel": "cy", "pob": "pt-br", "por-br": "pt-br", "cht": "zh-tw",
	"english": "en", "spanish": "es", "french": "fr", "german": "de",
	"italian": "it", "japanese": "ja", "korean": "ko",
}

// LanguageCode normalizes the forms used by TMDb, TheTVDB and media tags.
// Keep an explicit region when one is available so Brazil/Portugal and
// Simplified/Traditional Chinese can be compared independently.
func LanguageCode(raw string) string {
	raw = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(raw), "_", "-"))
	if known := alternateLanguages[raw]; known != "" {
		return known
	}
	base, region, _ := strings.Cut(raw, "-")
	switch base {
	case "", "und", "mis", "mul", "zxx":
		return ""
	}
	if len(base) < 2 || len(base) > 3 {
		return ""
	}
	for _, r := range base {
		if r < 'a' || r > 'z' {
			return ""
		}
	}
	if len(base) == 3 && base >= "qaa" && base <= "qtz" {
		return ""
	}
	if known := languageShortCodes[base]; known != "" {
		base = known
	}
	if known := alternateLanguages[base]; known != "" {
		base = known
	}
	switch region {
	case "hans", "hans-cn":
		region = "cn"
	case "hant", "hant-tw":
		region = "tw"
	}
	if len(region) == 2 && region[0] >= 'a' && region[0] <= 'z' && region[1] >= 'a' && region[1] <= 'z' {
		return base + "-" + region
	}
	return base
}

var languageShortCodes = map[string]string{
	"aar": "aa",
	"afr": "af",
	"aka": "ak",
	"amh": "am",
	"ara": "ar",
	"asm": "as",
	"aze": "az",
	"bak": "ba",
	"bam": "bm",
	"bel": "be",
	"ben": "bn",
	"bod": "bo",
	"bos": "bs",
	"bre": "br",
	"bul": "bg",
	"cat": "ca",
	"ces": "cs",
	"che": "ce",
	"chu": "cu",
	"chv": "cv",
	"cor": "kw",
	"cos": "co",
	"cym": "cy",
	"dan": "da",
	"deu": "de",
	"div": "dv",
	"dzo": "dz",
	"ell": "el",
	"eng": "en",
	"epo": "eo",
	"est": "et",
	"eus": "eu",
	"ewe": "ee",
	"fao": "fo",
	"fas": "fa",
	"fin": "fi",
	"fra": "fr",
	"fry": "fy",
	"ful": "ff",
	"gla": "gd",
	"gle": "ga",
	"glg": "gl",
	"glv": "gv",
	"grn": "gn",
	"guj": "gu",
	"hau": "ha",
	"heb": "he",
	"hin": "hi",
	"hrv": "hr",
	"hun": "hu",
	"hye": "hy",
	"ibo": "ig",
	"iii": "ii",
	"iku": "iu",
	"ina": "ia",
	"ind": "id",
	"isl": "is",
	"ita": "it",
	"ivl": "iv",
	"jav": "jv",
	"jpn": "ja",
	"kal": "kl",
	"kan": "kn",
	"kas": "ks",
	"kat": "ka",
	"kau": "kr",
	"kaz": "kk",
	"khm": "km",
	"kik": "ki",
	"kin": "rw",
	"kir": "ky",
	"kor": "ko",
	"lao": "lo",
	"lat": "la",
	"lav": "lv",
	"lin": "ln",
	"lit": "lt",
	"ltz": "lb",
	"lub": "lu",
	"lug": "lg",
	"mal": "ml",
	"mar": "mr",
	"mkd": "mk",
	"mlg": "mg",
	"mlt": "mt",
	"mon": "mn",
	"mri": "mi",
	"msa": "ms",
	"mya": "my",
	"nbl": "nr",
	"nde": "nd",
	"nep": "ne",
	"nld": "nl",
	"nno": "nn",
	"nob": "nb",
	"nor": "no",
	"oci": "oc",
	"ori": "or",
	"orm": "om",
	"oss": "os",
	"pan": "pa",
	"pol": "pl",
	"por": "pt",
	"pus": "ps",
	"que": "qu",
	"roh": "rm",
	"ron": "ro",
	"run": "rn",
	"rus": "ru",
	"sag": "sg",
	"san": "sa",
	"sin": "si",
	"slk": "sk",
	"slv": "sl",
	"sme": "se",
	"sna": "sn",
	"snd": "sd",
	"som": "so",
	"sot": "st",
	"spa": "es",
	"sqi": "sq",
	"srd": "sc",
	"srp": "sr",
	"ssw": "ss",
	"sun": "su",
	"swa": "sw",
	"swe": "sv",
	"tam": "ta",
	"tat": "tt",
	"tel": "te",
	"tgk": "tg",
	"tha": "th",
	"tir": "ti",
	"ton": "to",
	"tsn": "tn",
	"tso": "ts",
	"tuk": "tk",
	"tur": "tr",
	"uig": "ug",
	"ukr": "uk",
	"urd": "ur",
	"uzb": "uz",
	"ven": "ve",
	"vie": "vi",
	"vol": "vo",
	"wol": "wo",
	"xho": "xh",
	"yid": "yi",
	"yor": "yo",
	"zho": "zh",
	"zul": "zu",
}
