package config

import (
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
