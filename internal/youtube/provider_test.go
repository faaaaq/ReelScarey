package youtube

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNormalizeURL(t *testing.T) {
	for input, want := range map[string]string{
		"https://youtu.be/boKAdg0BS-c?t=4":                               "https://www.youtube.com/watch?v=boKAdg0BS-c",
		"https://www.youtube.com/watch?v=limOfz2srCk&list=PLabcdefghijk": "https://www.youtube.com/watch?v=limOfz2srCk",
		"https://youtube.com/playlist?list=PLabcdefghijk":                "https://www.youtube.com/playlist?list=PLabcdefghijk",
		"https://www.youtube.com/@xg_official":                           "https://www.youtube.com/@xg_official",
	} {
		got, err := NormalizeURL(input)
		if err != nil || got.URL != want {
			t.Errorf("NormalizeURL(%q) = %+v, %v", input, got, err)
		}
	}
	for _, input := range []string{
		"http://youtube.com/watch?v=boKAdg0BS-c", "https://youtube.com.evil.test/watch?v=boKAdg0BS-c",
		"https://youtube.com@evil.test/watch?v=boKAdg0BS-c", "https://youtube.com:443/watch?v=boKAdg0BS-c",
		"https://youtube.com/watch?v=boKAdg0BS-c&v=limOfz2srCk", "https://youtu.be/boKAdg0BS-c/extra",
		"https://youtube.com/redirect?q=https://evil.test", "https://localhost/", "https://youtube.com/@../../evil",
	} {
		if _, err := NormalizeURL(input); err == nil {
			t.Errorf("accepted %q", input)
		}
	}
}

func helperInspector(t *testing.T) *Inspector {
	t.Helper()
	t.Setenv("REELSCAREY_YT_HELPER", "1")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return &Inspector{executable: executable, command: func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, executable, append([]string{"-test.run=TestProviderHelper", "--"}, args...)...)
	}}
}

func TestProviderHelper(t *testing.T) {
	if os.Getenv("REELSCAREY_YT_HELPER") != "1" {
		return
	}
	if os.Getenv("REELSCAREY_YT_SLEEP") == "1" {
		time.Sleep(5 * time.Second)
		os.Exit(0)
	}
	if os.Getenv("REELSCAREY_YT_BIG") == "1" {
		fmt.Print(strings.Repeat("x", maxInspectionBytes+1))
		os.Exit(0)
	}
	dir := ""
	for index, arg := range os.Args {
		if arg == "--paths" && index+1 < len(os.Args) {
			dir = os.Args[index+1]
		}
		if arg == "-o" && index+1 < len(os.Args) {
			path := filepath.Join(dir, strings.ReplaceAll(os.Args[index+1], "%(ext)s", "mp4"))
			video := []byte("owned generated fixture")
			if input := os.Getenv("REELSCAREY_YT_VIDEO"); input != "" {
				var err error
				video, err = os.ReadFile(input)
				if err != nil {
					os.Exit(2)
				}
			}
			if err := os.WriteFile(path, video, 0600); err != nil {
				os.Exit(2)
			}
			if os.Getenv("REELSCAREY_YT_SUBS") == "1" {
				name := strings.TrimSuffix(path, ".mp4") + ".en.vtt"
				if err := os.WriteFile(name, []byte("WEBVTT\n\n00:00:00.000 --> 00:00:01.000\nGenerated\n"), 0600); err != nil {
					os.Exit(2)
				}
			}
			if os.Getenv("REELSCAREY_YT_EXTRA") == "1" {
				_ = os.WriteFile(filepath.Join(filepath.Dir(path), "extra"), []byte("x"), 0600)
			}
			os.Exit(0)
		}
	}
	fmt.Print(os.Getenv("REELSCAREY_YT_JSON"))
	os.Exit(0)
}

func TestDownloadSelectedSubtitleAndRejectUnexpectedLanguage(t *testing.T) {
	i := helperInspector(t)
	t.Setenv("REELSCAREY_YT_SUBS", "1")
	result, err := i.DownloadWithSubtitles(context.Background(), "boKAdg0BS-c", t.TempDir(), DownloadOptions{Languages: []string{"en", "ja"}})
	if err != nil || result.Video != "boKAdg0BS-c.mp4" || len(result.Subtitles) != 1 || result.Subtitles[0].Language != "en" || len(result.MissingLanguages) != 1 || result.MissingLanguages[0] != "ja" {
		t.Fatalf("result: %+v %v", result, err)
	}
	if _, err = i.DownloadWithSubtitles(context.Background(), "boKAdg0BS-c", t.TempDir(), DownloadOptions{Languages: []string{"ja"}}); err == nil {
		t.Fatal("accepted unexpected subtitle")
	}
	if _, err = i.DownloadWithSubtitles(context.Background(), "boKAdg0BS-c", t.TempDir(), DownloadOptions{Languages: []string{"all,-live_chat"}}); err == nil {
		t.Fatal("accepted subtitle expression")
	}
}

func TestInspectSanitizesAndBindsIdentity(t *testing.T) {
	i := helperInspector(t)
	t.Setenv("REELSCAREY_YT_JSON", `{"id":"boKAdg0BS-c","title":"XTRA XG #84\nEpisode","channel":"XG","duration":123,"upload_date":"20260422","url":"https://attacker.invalid"}`)
	got, err := i.Inspect(context.Background(), "https://youtu.be/boKAdg0BS-c")
	if err != nil || len(got.Entries) != 1 || got.Entries[0].Title != "XTRA XG #84 Episode" || got.Entries[0].URL != "https://www.youtube.com/watch?v=boKAdg0BS-c" || got.Entries[0].Published != "2026-04-22" {
		t.Fatalf("unexpected result %+v, %v", got, err)
	}
	t.Setenv("REELSCAREY_YT_JSON", `{"id":"limOfz2srCk","title":"wrong"}`)
	if _, err = i.Inspect(context.Background(), "https://youtu.be/boKAdg0BS-c"); err == nil {
		t.Fatal("accepted mismatched video ID")
	}
}

func TestInspectPlaylistBoundAndCancellation(t *testing.T) {
	i := helperInspector(t)
	entries := make([]map[string]string, 101)
	for n := range entries {
		entries[n] = map[string]string{"id": "limOfz2srCk", "title": "XG DAYS"}
	}
	raw, _ := json.Marshal(map[string]any{"id": "PLabcdefghijk", "entries": entries})
	t.Setenv("REELSCAREY_YT_JSON", string(raw))
	got, err := i.Inspect(context.Background(), "https://youtube.com/playlist?list=PLabcdefghijk")
	if err != nil || !got.Truncated || len(got.Entries) != 100 {
		t.Fatalf("unexpected bounded result: %d %v %v", len(got.Entries), got.Truncated, err)
	}
	t.Setenv("REELSCAREY_YT_BIG", "1")
	if _, err = i.Inspect(context.Background(), "https://youtu.be/boKAdg0BS-c"); err == nil {
		t.Fatal("accepted oversized output")
	}
	t.Setenv("REELSCAREY_YT_BIG", "")
	t.Setenv("REELSCAREY_YT_SLEEP", "1")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err = i.Inspect(ctx, "https://youtu.be/boKAdg0BS-c"); err == nil {
		t.Fatal("accepted cancelled process")
	}
}

func TestDownloadRequiresEmptyDedicatedDirectory(t *testing.T) {
	i := helperInspector(t)
	dir := t.TempDir()
	name, err := i.Download(context.Background(), "boKAdg0BS-c", dir)
	if err != nil || name != "boKAdg0BS-c.mp4" {
		t.Fatalf("download: %q %v", name, err)
	}
	if _, err = i.Download(context.Background(), "boKAdg0BS-c", dir); err == nil {
		t.Fatal("accepted occupied directory")
	}
	if _, err = i.Download(context.Background(), "../../unsafe", t.TempDir()); err == nil {
		t.Fatal("accepted invalid ID")
	}
	t.Setenv("REELSCAREY_YT_EXTRA", "1")
	if _, err = i.Download(context.Background(), "limOfz2srCk", t.TempDir()); err == nil {
		t.Fatal("accepted unexpected files")
	}
}
