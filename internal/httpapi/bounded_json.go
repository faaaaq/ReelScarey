package httpapi

import (
	"encoding/json"
	"io"
	"mime"
	"net/http"
)

func decodeBoundedJSON(w http.ResponseWriter, r *http.Request, out any, limit int64) bool {
	t, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || t != "application/json" {
		fail(w, 415, "unsupported_media_type", "Use application/json.")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e = d.Decode(out); e != nil {
		fail(w, 400, "invalid_json", "Send one valid object within the documented size limit.")
		return false
	}
	if d.Decode(&struct{}{}) != io.EOF {
		fail(w, 400, "invalid_json", "Send one JSON object.")
		return false
	}
	return true
}
