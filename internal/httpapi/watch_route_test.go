package httpapi

import (
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestWatchActivityDirectRoute(t *testing.T) {
	handler := frontend(fstest.MapFS{"index.html": {Data: []byte("app shell")}})
	for _, path := range []string{"/watch-activity", "/youtube"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest("GET", path, nil))
		if response.Code != 200 || response.Body.String() != "app shell" {
			t.Fatal(path, response.Code, response.Body)
		}
	}
}
