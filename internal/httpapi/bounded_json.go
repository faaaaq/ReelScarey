package httpapi

import "net/http"

// decodeBoundedJSON decodes one JSON object with an explicit size limit. It
// delegates to decodeJSONLimit so every handler reports oversized bodies as 413
// and malformed bodies as 400 consistently.
func decodeBoundedJSON(w http.ResponseWriter, r *http.Request, out any, limit int64) bool {
	return decodeJSONLimit(w, r, out, limit)
}
