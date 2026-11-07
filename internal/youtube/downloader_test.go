package youtube

import "testing"

func TestParseProgressLineAcceptsUnknownValues(t *testing.T) {
	values, ok := parseProgressLine("reelscarey-progress 123 NA 456 NA")
	if !ok || values != [4]int64{123, 0, 456, 0} {
		t.Fatal(values, ok)
	}
	if _, ok := parseProgressLine("[download] 10%"); ok {
		t.Fatal("unrelated line accepted")
	}
	if _, ok := parseProgressLine("reelscarey-progress 1 2 oops 4"); ok {
		t.Fatal("invalid numeric field accepted")
	}
}
