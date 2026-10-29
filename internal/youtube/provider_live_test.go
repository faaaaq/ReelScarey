package youtube

import (
	"context"
	"os"
	"strings"
	"testing"
)

// This opt-in public metadata check does not download audiovisual content or
// use account credentials. It depends on the local yt-dlp and YouTube access.
func TestLivePublicXGInspection(t *testing.T) {
	if os.Getenv("REELSCAREY_LIVE_YOUTUBE") != "1" {
		t.Skip("set REELSCAREY_LIVE_YOUTUBE=1 for public metadata acceptance")
	}
	i, err := NewInspector(os.Getenv("REELSCAREY_YTDLP"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ id, title string }{{"boKAdg0BS-c", "XTRA XG"}, {"limOfz2srCk", "XG DAYS"}} {
		t.Run(tc.id, func(t *testing.T) {
			got, err := i.Inspect(context.Background(), source("video", tc.id).URL)
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Entries) != 1 || got.Entries[0].ID != tc.id || !strings.Contains(strings.ToUpper(got.Title), tc.title) || got.Channel != "XG" {
				t.Fatalf("unexpected public metadata: %+v", got)
			}
		})
	}
}
