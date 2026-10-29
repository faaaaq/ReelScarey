package config

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
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
	BackupsDir   string
	LogLevel     string
	PublicURL    string
	BrowseRoots  map[string]string
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
		BackupsDir:   value("REELSCAREY_BACKUPS_DIR", filepath.Join(value("REELSCAREY_CONFIG_DIR", "./data/config"), "..", "backups")),
		LogLevel:     value("REELSCAREY_LOG_LEVEL", "info"),
		PublicURL:    value("REELSCAREY_PUBLIC_URL", ""),
	}
	if raw := strings.TrimSpace(getenv("REELSCAREY_BROWSE_ROOTS")); raw != "" {
		if len(raw) > 8192 || json.Unmarshal([]byte(raw), &c.BrowseRoots) != nil || c.BrowseRoots == nil || len(c.BrowseRoots) > 8 {
			return Config{}, fmt.Errorf("REELSCAREY_BROWSE_ROOTS must be a JSON object of at most eight named absolute folders")
		}
		for name, dir := range c.BrowseRoots {
			if name == "" || len(name) > 40 || name == "media" || name == "downloads" || strings.IndexFunc(name, func(r rune) bool { return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') }) >= 0 || !filepath.IsAbs(dir) || strings.ContainsRune(dir, 0) {
				return Config{}, fmt.Errorf("Browse roots require distinct lowercase names and absolute folder paths")
			}
			c.BrowseRoots[name] = filepath.Clean(dir)
		}
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
		{"REELSCAREY_BACKUPS_DIR", &c.BackupsDir},
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
	if c.PublicURL != "" {
		u, err := url.Parse(c.PublicURL)
		if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
			return Config{}, fmt.Errorf("REELSCAREY_PUBLIC_URL must be an http or https origin without a path, credentials or query")
		}
		c.PublicURL = strings.TrimSuffix(u.String(), "/")
	}
	return c, nil
}
