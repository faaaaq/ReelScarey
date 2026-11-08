package quality

import "testing"

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
	} {
		if !rule.Matches(title) {
			t.Fatal("expected a dub match", title)
		}
	}
	for _, title := range []string{
		"Show - 01 [1080p]",
		"Show - 01 [Japanese]",
		"Show - 01 [Sub]",
	} {
		if rule.Matches(title) {
			t.Fatal("unexpected dub match", title)
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
