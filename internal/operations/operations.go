// Package operations inspects local installation health. It never contacts a
// provider, scans a library, changes preferences, or reads download contents.
package operations

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/faaaaq/ReelScarey/internal/config"
	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/instance"
	"github.com/faaaaq/ReelScarey/internal/securestore"
	"github.com/faaaaq/ReelScarey/internal/transfer"
)

var ErrBusy = errors.New("installation checks are already running")

type Service struct {
	db     *sql.DB
	cfg    config.Config
	mu     sync.Mutex
	lastMu sync.Mutex
	last   *Report
}

func New(db *sql.DB, cfg config.Config) *Service { return &Service{db: db, cfg: cfg} }

type Step struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Message  string `json:"message"`
	State    string `json:"state"`
	Href     string `json:"href"`
	Optional bool   `json:"optional"`
}
type Guide struct {
	Steps       []Step `json:"steps"`
	LibraryName string `json:"library_name"`
	HasMedia    bool   `json:"has_media"`
	Recovery    bool   `json:"recovery_pending"`
}

func (s *Service) Guide(ctx context.Context) (Guide, error) {
	var out Guide
	var admin, tmdb, media int
	var importMode string
	if e := s.db.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM admin_account),(SELECT COUNT(*) FROM provider_secrets WHERE provider='tmdb'),(SELECT COUNT(*) FROM media_files),library_name,import_mode FROM settings WHERE id=1`).Scan(&admin, &tmdb, &media, &out.LibraryName, &importMode); e != nil {
		return out, e
	}
	out.HasMedia = media > 0
	out.Recovery = instance.RecoveryPending(s.cfg.ConfigDir)
	state := func(ready bool) string {
		if ready {
			return "ready"
		}
		return "pending"
	}
	importMessage := "Completed downloads are hardlinked into the library when the download and media roots share a writable mount; otherwise ReelScarey writes a verified copy."
	if importMode == "copy" {
		importMessage = "Every completed download is copied into the library. The download source remains available for seeding."
	}
	out.Steps = []Step{{"account", "Administrator", "Your local administrator controls this installation.", state(admin == 1), "/setup", false}, {"library", "Library preferences", "Choose library and destination folder names. Storage checks verify access separately.", "ready", "/settings", false}, {"imports", "File imports", importMessage, "ready", "/settings/general", false}, {"metadata", "Movie and TV metadata", "Save a TMDb connection to search and add titles.", state(tmdb > 0), "/settings#tmdb", false}}
	for _, item := range []struct{ name, title, href string }{{"torznab", "Indexers", "/indexers"}, {"qbittorrent", "Torrent download client", "/settings#qbittorrent"}, {"sabnzbd", "Usenet download client", "/settings#sabnzbd"}, {"opensubtitles", "Subtitle provider", "/subtitles"}} {
		var n int
		if e := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM integration_configs WHERE name=?`, item.name).Scan(&n); e != nil {
			return out, e
		}
		if item.name == "torznab" {
			var managed int
			if e := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM indexers WHERE enabled=1`).Scan(&managed); e != nil {
				return out, e
			}
			n += managed
		}
		if item.name == "qbittorrent" || item.name == "sabnzbd" {
			protocol := "torrent"
			if item.name == "sabnzbd" {
				protocol = "usenet"
			}
			var managed int
			if e := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM download_clients WHERE enabled=1 AND id=(SELECT CASE WHEN ?='torrent' THEN torrent ELSE usenet END FROM download_client_preferences WHERE id=1)`, protocol).Scan(&managed); e != nil {
				return out, e
			}
			n += managed
			item.href = "/download-clients"
		}
		message := "Optional for catalog setup. Connect when you are ready."
		v := "optional"
		if n > 0 {
			v = "ready"
			message = "Connection saved. Use its settings page to test the live service."
		}
		out.Steps = append(out.Steps, Step{item.name, item.title, message, v, item.href, true})
	}
	return out, nil
}

type Check struct {
	ID        string  `json:"id"`
	Title     string  `json:"title"`
	State     string  `json:"state"`
	Message   string  `json:"message"`
	FreeBytes *uint64 `json:"free_bytes,omitempty"`
}
type Resources struct {
	HeapBytes    uint64 `json:"heap_bytes"`
	ProcessBytes uint64 `json:"go_memory_bytes"`
	Goroutines   int    `json:"goroutines"`
	CPUs         int    `json:"logical_cpus"`
}
type Report struct {
	Checked   int64     `json:"checked_at"`
	Checks    []Check   `json:"checks"`
	Resources Resources `json:"resources"`
	Recovery  bool      `json:"recovery_pending"`
}

func (s *Service) Last() *Report { s.lastMu.Lock(); defer s.lastMu.Unlock(); return s.last }
func (s *Service) Run(ctx context.Context) (Report, error) {
	if !s.mu.TryLock() {
		return Report{}, ErrBusy
	}
	defer s.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	out := Report{Checked: time.Now().Unix(), Checks: []Check{}}
	out.Recovery = instance.RecoveryPending(s.cfg.ConfigDir)
	add := func(id, title, message string, e error) {
		state := "ready"
		if e != nil {
			state = "attention"
		}
		out.Checks = append(out.Checks, Check{ID: id, Title: title, State: state, Message: message})
	}
	_, e := database.Inspect(ctx, s.db)
	message := "Integrity, relationships and migration history verified."
	if e != nil {
		message = "Database verification did not finish successfully. Stop the server and check a verified backup before recovery."
	}
	add("database", "Database", message, e)
	e = securestore.Validate(ctx, s.db, s.cfg.ConfigDir)
	message = "The local encryption key matches all saved encrypted records."
	if e != nil {
		message = "The encryption key or saved encrypted data could not be verified. Restore the matching config backup; do not replace the key."
	}
	add("secrets", "Saved credentials", message, e)
	paths := []struct {
		id, title, path string
		write           bool
	}{{"config", "Configuration folder", s.cfg.ConfigDir, true}, {"media", "Media folder", s.cfg.MediaDir, true}, {"downloads", "Download folder", s.cfg.DownloadsDir, false}}
	separated := true
	canonical := []string{}
	for _, p := range paths {
		value, e := filepath.EvalSymlinks(p.path)
		if e != nil {
			separated = false
			canonical = append(canonical, "")
		} else {
			value, _ = filepath.Abs(value)
			canonical = append(canonical, value)
		}
	}
	for i := range canonical {
		for j := i + 1; j < len(canonical); j++ {
			if canonical[i] != "" && canonical[j] != "" && (overlap(canonical[i], canonical[j]) || overlap(canonical[j], canonical[i])) {
				separated = false
			}
		}
	}
	var pathError error
	if !separated {
		pathError = errors.New("overlap")
	}
	message = "Config, media and download roots are separate."
	if !separated {
		message = "Storage roots overlap or could not all be resolved. Make all three folders available and choose separate roots before importing."
	}
	add("paths", "Separate storage roots", message, pathError)
	for _, p := range paths {
		if p.write && !separated {
			add(p.id, p.title, "Access probe skipped because storage roots overlap.", pathError)
			continue
		}
		hardlinks, e := probeFolder(ctx, p.path, p.write && separated)
		message = "Folder exists and can be read."
		if p.write {
			message = "A private temporary file was created, verified and removed."
		}
		if e != nil {
			message = "The folder is missing or the required access is unavailable. Review its path and server permissions."
		}
		add(p.id, p.title, message, e)
		if free, fe := freeSpace(p.path); fe == nil {
			out.Checks[len(out.Checks)-1].FreeBytes = &free
		}
		if p.id == "media" && e == nil {
			var linkErr error
			message = "Exclusive hardlink publication is supported."
			if !hardlinks {
				linkErr = errors.New("hardlinks")
				message = "This filesystem could not publish a private hardlink. Verified video/subtitle publication requires hardlink support."
			}
			add("publication", "Safe file publication", message, linkErr)
		}
	}
	_, e = exec.LookPath("ffprobe")
	message = "FFprobe was found on the server's program path."
	if e != nil {
		message = "FFprobe was not found. Install it before importing or inspecting videos."
	}
	add("ffprobe", "Video inspection", message, e)
	message = "Local HTTP is configured. Keep the published port on loopback; use HTTPS for remote sign-in."
	var remoteErr error
	if s.cfg.PublicURL != "" {
		u, _ := url.Parse(s.cfg.PublicURL)
		if u != nil && u.Scheme == "https" {
			message = "An HTTPS browser origin is configured. TLS must be provided by your reverse proxy."
		} else if u != nil && u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost" && u.Hostname() != "::1" {
			remoteErr = errors.New("remote http")
			message = "Remote sign-in uses HTTP. Configure an HTTPS reverse proxy before sending credentials over the network."
		}
	}
	add("origin", "Browser connection", message, remoteErr)
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	out.Resources = Resources{stats.HeapAlloc, stats.Sys, runtime.NumGoroutine(), runtime.NumCPU()}
	s.lastMu.Lock()
	s.last = &out
	s.lastMu.Unlock()
	return out, nil
}
func overlap(a, b string) bool {
	r, e := filepath.Rel(a, b)
	return e == nil && (r == "." || filepath.IsLocal(r))
}
func probeFolder(ctx context.Context, dir string, write bool) (hardlinks bool, err error) {
	root, e := os.OpenRoot(dir)
	if e != nil {
		return false, e
	}
	defer root.Close()
	folder, e := root.Open(".")
	if e != nil {
		return false, e
	}
	_, e = folder.ReadDir(1)
	folder.Close()
	if e != nil && e != io.EOF {
		return false, e
	}
	if !write {
		return false, nil
	}
	if e = ctx.Err(); e != nil {
		return false, e
	}
	name := ".reelscarey-check-" + transfer.ID()
	file, e := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if e != nil {
		return false, e
	}
	info, e := file.Stat()
	if e != nil {
		file.Close()
		return false, e
	}
	link := name + ".link"
	linkedByUs := false
	cleanup := func(n string) error {
		visible, e := root.Lstat(n)
		if errors.Is(e, os.ErrNotExist) {
			return nil
		}
		if e != nil {
			return e
		}
		if !visible.Mode().IsRegular() || !os.SameFile(visible, info) {
			return errors.New("temporary probe changed and was preserved")
		}
		return root.Remove(n)
	}
	defer func() {
		err = errors.Join(err, file.Close())
		if linkedByUs {
			err = errors.Join(err, cleanup(link))
		}
		err = errors.Join(err, cleanup(name))
	}()
	body := []byte("ReelScarey temporary installation check\n")
	if _, e = file.Write(body); e != nil {
		return false, e
	}
	if e = file.Sync(); e != nil {
		return false, e
	}
	if _, e = file.Seek(0, 0); e != nil {
		return false, e
	}
	read, e := io.ReadAll(io.LimitReader(file, 128))
	if e != nil || string(read) != string(body) {
		return false, errors.New("temporary probe verification failed")
	}
	if e = root.Link(name, link); e != nil {
		return false, nil
	}
	linkedByUs = true
	linked, e := root.Lstat(link)
	if e != nil || !os.SameFile(linked, info) {
		return false, errors.New("temporary publication changed")
	}
	return true, nil
}
