package config

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

type Config struct {
	ListenAddr   string
	ConfigDir    string
	MediaDir     string
	DownloadsDir string
	LogLevel     string
}

func Load() (Config, error) { return FromEnv(os.Getenv) }

func FromEnv(getenv func(string) string) (Config, error) {
	value := func(key, fallback string) string {
		if v := strings.TrimSpace(getenv(key)); v != "" {
			return v
		}
		return fallback
	}
	media, downloads := "/media", "/downloads"
	if runtime.GOOS == "windows" {
		media, downloads = "./data/media", "./data/downloads"
	}
	c := Config{
		ListenAddr:   value("REELSCAREY_LISTEN_ADDR", "127.0.0.1:8787"),
		ConfigDir:    value("REELSCAREY_CONFIG_DIR", "./data/config"),
		MediaDir:     value("REELSCAREY_MEDIA_DIR", media),
		DownloadsDir: value("REELSCAREY_DOWNLOADS_DIR", downloads),
		LogLevel:     value("REELSCAREY_LOG_LEVEL", "info"),
	}
	_, port, err := net.SplitHostPort(c.ListenAddr)
	if err != nil {
		return Config{}, fmt.Errorf("REELSCAREY_LISTEN_ADDR must be host:port")
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return Config{}, fmt.Errorf("REELSCAREY_LISTEN_ADDR port must be between 1 and 65535")
	}
	for _, entry := range []struct {
		name string
		path *string
	}{
		{"REELSCAREY_CONFIG_DIR", &c.ConfigDir}, {"REELSCAREY_MEDIA_DIR", &c.MediaDir}, {"REELSCAREY_DOWNLOADS_DIR", &c.DownloadsDir},
	} {
		if strings.ContainsRune(*entry.path, 0) {
			return Config{}, fmt.Errorf("%s contains an invalid character", entry.name)
		}
		absolute, err := filepath.Abs(*entry.path)
		if err != nil {
			return Config{}, fmt.Errorf("%s must be a valid path", entry.name)
		}
		*entry.path = filepath.Clean(absolute)
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return Config{}, fmt.Errorf("REELSCAREY_LOG_LEVEL must be debug, info, warn, or error")
	}
	return c, nil
}
