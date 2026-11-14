package quality

import (
	"strings"
	"testing"
)

func TestDefaultDubRuleMatches(t *testing.T) {
	rule, err := ParseDubRule(DefaultDubRule)
	if err != nil {
		t.Fatal(err)
	}
	for _, title := range []string{
		"[English Dub] Show - 01 [1080p]",
		"[Eng Dub] Show - 01",
		"Show - 01 (English Dubs)",
		"Show S01E01 [Dual Audio]",
		"Show - 01 FuniDub",
		"Show - 01 [Dual]",
		"Show - 01 dual-audio",
		"Show Y S01E01 - Dual Audio",
		"Show Y S01E01 - Dual Audio - Japanese",
		"Show Y S01E01 - Dual Audio [1080p]",
	} {
		if !rule.Matches(title) {
			t.Fatal("expected a dub match", title)
		}
	}
	for _, title := range []string{
		"Show - 01 [1080p]",
		"Show - 01 [Japanese]",
		"Show - 01 [Sub]",
		"Show Y S01E01 - Dual Audio - French Japanese",
		"Show Y S01E01 - Dual Audio - Spanish",
		"Show Y S01E01 - Dual Audio - German",
		"Show Y S01E01 - Dual Audio - Italian",
		"Show Y S01E01 - Dual Audio - Portuguese",
		"Show Y S01E01 - Dual Audio - Korean",
		"Show Y S01E01 - Dual Audio - Mandarin",
		"Show Y S01E01 - Dual Audio - Russian",
		"Show Y S01E01 Dual Audio POR-BR",
	} {
		if rule.Matches(title) {
			t.Fatal("unexpected dub match", title)
		}
	}
}

// Qualifies is the gate every acquisition path uses for an English-dub-only
// show: the rule must match AND the title must explicitly name English audio.
// A bare "Dual Audio" claim, a Japanese-only dual audio release, or subtitle
// evidence can never satisfy the requirement.
func TestDefaultDubRuleQualifies(t *testing.T) {
	rule, err := ParseDubRule(DefaultDubRule)
	if err != nil {
		t.Fatal(err)
	}
	for _, title := range []string{
		// Explicit English audio evidence.
		"[English Dub] Show - 01 [1080p]",
		"[Eng Dub] Show - 01",
		"Show - 01 English-Dub",
		"Show - 01 FuniDub",
		"Show Y S01E01 - Dual Audio ENG/JPN",
		"Show Y S01E01 - Dual Audio JPN/ENG",
		"Show Y S01E01 - Dual Audio [Eng,Jap]",
		"Show S01E01 Dual Audio (English, Japanese)",
		"Show - 01 [Dual] [English]",
		"Show - 01 [English Audio] [1080p]",
		"Show - 01 Eng Audio",
	} {
		if !rule.Qualifies(title) {
			t.Fatal("expected English-audio qualification", title)
		}
	}
	for _, title := range []string{
		// Ambiguous or non-English: never qualify.
		"Show Y S01E01 - Dual Audio",
		"Show Y S01E01 - Dual Audio - Japanese",
		"Show S01E01 - Dual Audio JPN",
		"Show S01E01 - Dual Audio Hindi",
		"Show - 01 Multi Audio",
		// Subtitles alone are never English audio.
		"Show - 01 [Japanese] [English Subs]",
		"[SubsPlease] Show - 01 [1080p] [Eng Sub]",
		"Show Y S01E01 - Dual Audio Eng Sub",
		// No language information at all.
		"Show - 01 [1080p]",
		// RAW and Japanese-only.
		"Show - 01 [RAW]",
		"Show - 01 [Japanese]",
		// "Dubbed" without a language does not prove English.
		"Show - 01 DUBBED",
	} {
		if rule.Qualifies(title) {
			t.Fatal("unexpected qualification without English-audio proof", title)
		}
	}
	// The evidence helper itself is word-bounded and subtitle-aware.
	for _, title := range []string{
		"Dual Audio ENG/JPN",
		"jpn/eng",
		"[eng] Show - 01",
		"English Dub",
		"FuniDub",
	} {
		if !EnglishAudioEvidence(title) {
			t.Fatal("expected English-audio evidence", title)
		}
	}
	for _, title := range []string{
		"Dual Audio JPN",
		"Dual Audio",
		"Japan Engineering 1080p",
		"Dual Audio Eng Sub",
		"Show - 01 English Subs",
		"Show - 01 [1080p]",
	} {
		if EnglishAudioEvidence(title) {
			t.Fatal("unexpected English-audio evidence", title)
		}
	}
}

func TestParseDubRuleValidationAndCustomPhrases(t *testing.T) {
	if _, err := ParseDubRule(""); err == nil {
		t.Fatal("empty rule accepted")
	}
	if _, err := ParseDubRule("/([unclosed/"); err == nil {
		t.Fatal("invalid regex accepted")
	}
	rule, err := ParseDubRule("/custom[- ]dub/i\nMy Dub Phrase")
	if err != nil {
		t.Fatal(err)
	}
	if !rule.Matches("Show Custom-Dub 01") || !rule.Matches("Show my dub phrase 01") {
		t.Fatal("custom terms did not match")
	}
	if rule.Matches("Show - 01") {
		t.Fatal("custom rule matched a plain title")
	}
}

func TestParseDubRuleExclusions(t *testing.T) {
	// A !-prefixed phrase line rejects releases containing it, while the
	// positive terms keep matching everything else.
	rule, err := ParseDubRule("Dual Audio\n!french")
	if err != nil {
		t.Fatal(err)
	}
	if !rule.Matches("Show S01E01 Dual Audio") {
		t.Fatal("exclusion rule lost the positive match")
	}
	if rule.Matches("Show S01E01 Dual Audio French") {
		t.Fatal("exclusion phrase did not reject the release")
	}
	// A !-prefixed regex line uses the same /.../i form as positive terms.
	rule, err = ParseDubRule("Dual Audio\n!/\\bspanish\\b/i")
	if err != nil {
		t.Fatal(err)
	}
	if rule.Matches("Show S01E01 Dual Audio Spanish") {
		t.Fatal("exclusion regex did not reject the release")
	}
	if !rule.Matches("Show S01E01 Dual Audio") {
		t.Fatal("exclusion regex lost the positive match")
	}
	// An exclusion wins even when a positive term also matches.
	rule, err = ParseDubRule("Dual Audio\n!german")
	if err != nil {
		t.Fatal(err)
	}
	if rule.Matches("Dual Audio German Dub") {
		t.Fatal("exclusion did not win over the positive match")
	}
	// Invalid exclusion lines are refused exactly like invalid positive lines.
	for _, text := range []string{
		"!",
		"! ",
		"Dual Audio\n!",
		"Dual Audio\n! ",
		"Dual Audio\n!/([unclosed/",
		"Dual Audio\n!/pattern/x",
		strings.Repeat("term\n", 100) + "!one more",
	} {
		if _, err := ParseDubRule(text); err == nil {
			t.Fatal("invalid exclusion rule accepted", text)
		}
	}
	// The 100-line cap counts positives and exclusions together.
	if _, err := ParseDubRule(strings.Repeat("term\n", 99) + "!excluded"); err != nil {
		t.Fatal("100 mixed terms rejected", err)
	}
}
