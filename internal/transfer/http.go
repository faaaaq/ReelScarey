package transfer

import (
	"io"
)

// bounded reads up to max+1 bytes from r and returns an error when the
// response exceeds the limit. Every adapter HTTP call uses this guard so a
// misbehaving or compromised service cannot exhaust memory.
func bounded(r io.Reader, max int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil || int64(len(body)) > max {
		return nil, problem("invalid_response", "The service returned an unreadable or oversized response.")
	}
	return body, nil
}
