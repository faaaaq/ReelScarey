package transfer

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// Invalid UTF-8 in a download path must fail closed at planning instead of
// being silently rewritten to U+FFFD by JSON encoding, where the review UI
// would display a name that cannot match the file on disk.
func TestSafeRelativeRejectsInvalidUTF8(t *testing.T) {
	relative, e := safeRelative("Movie.2024" + string([]byte{0xFF, 0xFE}) + ".mkv")
	if e == nil {
		t.Fatalf("invalid UTF-8 path was accepted: %q", relative)
	}
	var p *Error
	if !errors.As(e, &p) || p.Code != "unsafe_path" {
		t.Fatalf("expected unsafe_path problem, got %v", e)
	}
	// The rejection must not be lossy in either direction: no U+FFFD anywhere.
	if strings.ContainsRune(e.Error(), '\uFFFD') {
		t.Fatalf("error message contains replacement characters: %q", e.Error())
	}
	payload, _ := json.Marshal(e.Error())
	if strings.Contains(string(payload), "\uFFFD") {
		t.Fatalf("JSON encoding would misrepresent the path: %s", payload)
	}

	// Valid UTF-8 names, including non-ASCII text, keep working.
	for _, ok := range []string{"Amélie.2001.mkv", "普通のタイトル S01E01.mkv", "Plain.Title.mkv"} {
		if _, e := safeRelative(ok); e != nil {
			t.Fatalf("valid UTF-8 path %q was rejected: %v", ok, e)
		}
	}
}
