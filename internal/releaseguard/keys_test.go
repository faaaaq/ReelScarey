package releaseguard

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func TestTorrentKey(t *testing.T) {
	lower40 := strings.Repeat("a1", 20)
	upper64 := strings.Repeat("A1", 32)
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "40 hex", input: lower40, want: lower40},
		{name: "64 hex normalized", input: upper64, want: strings.ToLower(upper64)},
		{name: "surrounding whitespace", input: "  " + strings.Repeat("b0", 20) + "\n", want: strings.Repeat("b0", 20)},
		{name: "too short", input: strings.Repeat("a", 39), wantErr: true},
		{name: "too long", input: strings.Repeat("a", 41), wantErr: true},
		{name: "63 characters", input: strings.Repeat("a", 63), wantErr: true},
		{name: "65 characters", input: strings.Repeat("a", 65), wantErr: true},
		{name: "non hex", input: strings.Repeat("g", 40), wantErr: true},
		{name: "embedded whitespace", input: strings.Repeat("a", 20) + " " + strings.Repeat("a", 19), wantErr: true},
		{name: "empty", input: " ", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := TorrentKey(test.input)
			if test.wantErr {
				if err == nil {
					t.Fatalf("TorrentKey(%q) = %q, want error", test.input, got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("TorrentKey(%q) = %q, want %q", test.input, got, test.want)
			}
		})
	}
}

func TestUsenetKeyIsBoundedSHA256OfGuid(t *testing.T) {
	guid := "SABnzbd_nzo_AbC123"
	want := sha256.Sum256([]byte(guid))
	got, err := UsenetKey("  " + guid + "  ")
	if err != nil {
		t.Fatal(err)
	}
	if got != hex.EncodeToString(want[:]) {
		t.Fatalf("UsenetKey() = %q, want %q", got, hex.EncodeToString(want[:]))
	}
	other, err := UsenetKey(strings.ToLower(guid))
	if err != nil {
		t.Fatal(err)
	}
	if other == got {
		t.Fatal("GUID identity must remain case-sensitive")
	}
}

func TestUsenetKeyRejectsInvalidIdentity(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "empty", input: " "},
		{name: "too long", input: strings.Repeat("x", maxIdentityBytes+1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := UsenetKey(test.input); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestUsenetTitleKeyNormalizesAndSeparatesDomains(t *testing.T) {
	first, err := UsenetTitleKey("  Some   Release\tName ")
	if err != nil {
		t.Fatal(err)
	}
	second, err := UsenetTitleKey("some release name")
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("normalized title keys differ: %q != %q", first, second)
	}
	guidKey, err := UsenetKey("Some   Release\tName")
	if err != nil {
		t.Fatal(err)
	}
	if first == guidKey {
		t.Fatal("title and GUID keys must be domain-separated")
	}
	if _, err := UsenetTitleKey("  "); err == nil {
		t.Fatal("expected empty title to be rejected")
	}
	if _, err := UsenetTitleKey(strings.Repeat("x", maxIdentityBytes+1)); err == nil {
		t.Fatal("expected oversized title to be rejected")
	}
}
