package config

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestDefaultsAndValidation(t *testing.T) {
	cfg, err := FromEnv(func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ListenAddr != "127.0.0.1:8787" || !filepath.IsAbs(cfg.ConfigDir) {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	for _, value := range []string{"localhost", "localhost:0", "localhost:65536", "localhost:no"} {
		t.Run(value, func(t *testing.T) {
			_, err := FromEnv(func(key string) string {
				if key == "REELSCAREY_LISTEN_ADDR" {
					return value
				}
				return ""
			})
			if err == nil {
				t.Fatal("invalid address accepted")
			}
		})
	}
	_, err = FromEnv(func(key string) string {
		if key == "REELSCAREY_LOG_LEVEL" {
			return "verbose"
		}
		return ""
	})
	if err == nil {
		t.Fatal("invalid log level accepted")
	}
}
func TestTMDbTokenFromEnvironment(t *testing.T) {
	token := "test-provider-token-for-config-tests"
	cfg, err := FromEnv(func(key string) string {
		if key == "REELSCAREY_TMDB_TOKEN" {
			return token
		}
		return ""
	})
	if err != nil || cfg.TMDbToken != token {
		t.Fatal(cfg, err)
	}
	for _, raw := range []string{"short", "has space in the token", string([]byte{1, 2, 3})} {
		if _, err := FromEnv(func(key string) string {
			if key == "REELSCAREY_TMDB_TOKEN" {
				return raw
			}
			return ""
		}); err == nil {
			t.Fatal("invalid TMDb token accepted")
		}
	}
}

func TestBrowseRootsRequireBoundedNamedAbsoluteFolders(t *testing.T) {
	for _, raw := range []string{`null`, `[]`, `{"media":"/archive"}`, `{"private":"relative"}`, `{"Bad Name":"/archive"}`} {
		if _, e := FromEnv(func(key string) string {
			if key == "REELSCAREY_BROWSE_ROOTS" {
				return raw
			}
			return ""
		}); e == nil {
			t.Fatal(raw)
		}
	}
	dir := t.TempDir()
	raw := fmt.Sprintf(`{"archive":%q}`, dir)
	cfg, e := FromEnv(func(key string) string {
		if key == "REELSCAREY_BROWSE_ROOTS" {
			return raw
		}
		return ""
	})
	if e != nil || cfg.BrowseRoots["archive"] != dir {
		t.Fatal(cfg, e)
	}
}
