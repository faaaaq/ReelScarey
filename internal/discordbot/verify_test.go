package discordbot

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"testing"
	"time"
)

func TestVerifyRejectsForgedOrExpiredInteractions(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1700000000, 0)
	stamp := strconv.FormatInt(now.Unix(), 10)
	body := []byte(`{"type":1}`)
	sig := ed25519.Sign(private, append([]byte(stamp), body...))
	if err = Verify(hex.EncodeToString(public), hex.EncodeToString(sig), stamp, body, now); err != nil {
		t.Fatal(err)
	}
	if err = Verify(hex.EncodeToString(public), hex.EncodeToString(sig), stamp, []byte(`{"type":2}`), now); err == nil {
		t.Fatal("modified body accepted")
	}
	if err = Verify(hex.EncodeToString(public), hex.EncodeToString(sig), stamp, body, now.Add(6*time.Minute)); err == nil {
		t.Fatal("expired interaction accepted")
	}
	if err = Verify(hex.EncodeToString(public), "00", stamp, body, now); err == nil {
		t.Fatal("invalid signature accepted")
	}
}
