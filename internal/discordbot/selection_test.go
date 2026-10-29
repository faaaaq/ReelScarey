package discordbot

import (
	"strings"
	"testing"
	"time"
)

func TestSelectionIsBoundToUserAndExpires(t *testing.T) {
	key := []byte(strings.Repeat("k", 32))
	now := time.Unix(1700000000, 0)
	id := "0123456789abcdef0123456789abcdef"
	value, err := SignSelection(key, id, "123456789012345678", now.Add(time.Minute), now)
	if err != nil || len(value) > 100 {
		t.Fatal(value, err)
	}
	if got, err := VerifySelection(key, value, "123456789012345678", now); err != nil || got != id {
		t.Fatal(got, err)
	}
	if _, err := VerifySelection(key, value, "123456789012345679", now); err == nil {
		t.Fatal("another user accepted")
	}
	if _, err := VerifySelection(key, value, "123456789012345678", now.Add(time.Minute)); err == nil {
		t.Fatal("expired selection accepted")
	}
	if _, err := VerifySelection([]byte(strings.Repeat("x", 32)), value, "123456789012345678", now); err == nil {
		t.Fatal("forged signature accepted")
	}
}
