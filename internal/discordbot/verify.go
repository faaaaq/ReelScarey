// Package discordbot contains the security boundary for Discord interactions.
// The companion bot is not enabled until its command and authorization flows
// are complete.
package discordbot

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"strconv"
	"time"
)

var ErrSignature = errors.New("invalid Discord interaction signature")

// Verify authenticates Discord's timestamp + raw body and rejects old or
// future timestamps. Callers must still fence replayed interaction IDs and
// authorize the Discord user before acting on an interaction.
func Verify(publicKey, signature, timestamp string, body []byte, now time.Time) error {
	key, err := hex.DecodeString(publicKey)
	if err != nil || len(key) != ed25519.PublicKeySize {
		return ErrSignature
	}
	sig, err := hex.DecodeString(signature)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return ErrSignature
	}
	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || seconds < 0 {
		return ErrSignature
	}
	delta := now.Unix() - seconds
	if delta < -300 || delta > 300 {
		return ErrSignature
	}
	message := make([]byte, 0, len(timestamp)+len(body))
	message = append(message, timestamp...)
	message = append(message, body...)
	if !ed25519.Verify(ed25519.PublicKey(key), message, sig) {
		return ErrSignature
	}
	return nil
}
