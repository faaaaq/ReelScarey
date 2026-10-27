package discordbot

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"strconv"
	"time"
)

var ErrSelection = errors.New("invalid or expired Discord selection")

// SignSelection binds a short-lived release choice to the Discord user who
// received it. The worker must also check authorization and queue reservations.
func SignSelection(key []byte, releaseID, userID string, expires time.Time, now time.Time) (string, error) {
	if len(key) < 32 || expires.Unix() <= now.Unix() || expires.Unix() > now.Unix()+900 || expires.Unix() > int64(^uint32(0)) {
		return "", ErrSelection
	}
	release, err := hex.DecodeString(releaseID)
	if err != nil || len(release) != 16 {
		return "", ErrSelection
	}
	user, err := strconv.ParseUint(userID, 10, 64)
	if err != nil || user == 0 {
		return "", ErrSelection
	}
	payload := make([]byte, 28, 44)
	copy(payload, release)
	binary.BigEndian.PutUint64(payload[16:24], user)
	binary.BigEndian.PutUint32(payload[24:28], uint32(expires.Unix()))
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(payload)
	payload = append(payload, mac.Sum(nil)[:16]...)
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func VerifySelection(key []byte, value, userID string, now time.Time) (string, error) {
	if len(key) < 32 || len(value) > 100 {
		return "", ErrSelection
	}
	payload, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(payload) != 44 {
		return "", ErrSelection
	}
	user, err := strconv.ParseUint(userID, 10, 64)
	if err != nil || user == 0 || binary.BigEndian.Uint64(payload[16:24]) != user {
		return "", ErrSelection
	}
	expires := int64(binary.BigEndian.Uint32(payload[24:28]))
	if expires <= now.Unix() || expires > now.Unix()+900 {
		return "", ErrSelection
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(payload[:28])
	if !hmac.Equal(payload[28:], mac.Sum(nil)[:16]) {
		return "", ErrSelection
	}
	return hex.EncodeToString(payload[:16]), nil
}
