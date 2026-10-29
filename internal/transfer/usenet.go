package transfer

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type SABUpdate struct {
	URL          string  `json:"url"`
	APIKey       *string `json:"api_key"`
	DownloadPath string  `json:"download_path"`
}

func (s *Service) sabConfig(ctx context.Context) (ClientConfig, error) {
	var c ClientConfig
	b, e := s.secrets.GetNamed(ctx, "sabnzbd")
	if e != nil {
		return c, e
	}
	if len(b) == 0 {
		return c, problem("client_not_configured", "Connect SABnzbd in Settings to download Usenet releases.")
	}
	e = json.Unmarshal(b, &c)
	return c, e
}
func (s *Service) downloadConfig(ctx context.Context, protocol string) (ClientConfig, error) {
	if protocol == "usenet" {
		return s.sabConfig(ctx)
	}
	if protocol != "" && protocol != "torrent" {
		return ClientConfig{}, problem("invalid_release", "The download protocol is unsupported.")
	}
	return s.clientConfig(ctx)
}
func (s *Service) sabStatus(ctx context.Context) (IntegrationStatus, error) {
	c, e := s.sabConfig(ctx)
	var p *Error
	if errors.As(e, &p) && p.Code == "client_not_configured" {
		return IntegrationStatus{}, nil
	}
	return IntegrationStatus{Configured: e == nil, URL: c.URL, DownloadPath: c.DownloadPath}, e
}
func (s *Service) ConfigureSAB(ctx context.Context, in SABUpdate) error {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	u, e := endpoint(in.URL)
	if e != nil {
		return e
	}
	remote, e := remoteRoot(in.DownloadPath)
	if e != nil {
		return e
	}
	old, e := s.sabConfig(ctx)
	var p *Error
	if e != nil && !(errors.As(e, &p) && p.Code == "client_not_configured") {
		return e
	}
	key := old.APIKey
	if in.APIKey != nil {
		key = *in.APIKey
	}
	if !textOK(key, 2048) {
		return problem("invalid_settings", "Enter the full SABnzbd API key, not its NZB-only key.")
	}
	if old.URL != "" && old.URL != u.String() && in.APIKey == nil {
		return problem("invalid_settings", "Re-enter the API key when changing the SABnzbd URL.")
	}
	if old.URL != "" && (old.URL != u.String() || old.DownloadPath != remote) {
		var active int
		if e = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM downloads WHERE protocol='usenet' AND state NOT IN ('completed','cancelled')`).Scan(&active); e != nil {
			return e
		}
		if active > 0 {
			return problem("active_downloads", "Finish or stop tracking active SABnzbd downloads before changing its address or completed folder.")
		}
	}
	c := ClientConfig{URL: u.String(), APIKey: key, DownloadPath: remote}
	if e = s.sab.Test(ctx, c); e != nil {
		return e
	}
	b, e := json.Marshal(c)
	if e != nil {
		return e
	}
	return s.secrets.SetNamed(ctx, "sabnzbd", b)
}
func sourceInJob(j Job, source string) bool {
	prefix := "ReelScarey/" + j.ID + "/"
	if j.Protocol == "usenet" {
		prefix = "ReelScarey/reelscarey-" + j.ID + "/"
	}
	return strings.HasPrefix(source, prefix)
}
func (s *Service) processUsenet(ctx context.Context, j Job) error {
	c, e := s.sabConfig(ctx)
	if e != nil {
		return e
	}
	if c.URL != j.clientURL {
		return problem("client_changed", "Restore the SABnzbd address used by this download before resuming tracking.")
	}
	tag := "reelscarey-" + j.ID
	remote, e := s.sab.Find(ctx, c, j.ClientID, tag)
	if e != nil {
		return e
	}
	if remote == nil {
		if j.attempted != 0 || j.ClientID != "" {
			if time.Now().Unix()-j.submittedAt > 90 {
				return problem("usenet_submission_unknown", "The SABnzbd job is not visible. Restore its original name/category or history, then retry tracking. An uncertain NZB is never resubmitted automatically.")
			}
			return nil
		}
		plain, e := s.secrets.Open("download", j.payload)
		if e != nil {
			return e
		}
		var p Payload
		if e = json.Unmarshal(plain, &p); e != nil {
			return e
		}
		identity, e := NZBIdentity(p.NZB)
		if e != nil || identity != j.Hash || p.Magnet != "" || len(p.Torrent) > 0 {
			return problem("payload_mismatch", "The saved NZB does not match this job. No submission was made.")
		}
		if e = s.owned(ctx, j, `UPDATE downloads SET state='submitting',submission_attempted=1,submitted_at=unixepoch(),updated_at=unixepoch()`); e != nil {
			return e
		}
		id, e := s.sab.Add(ctx, c, tag, p.NZB)
		if e != nil {
			return e
		}
		return s.owned(ctx, j, `UPDATE downloads SET state='downloading',client_id=?,updated_at=unixepoch()`, id)
	}
	if e = s.owned(ctx, j, `UPDATE downloads SET state='downloading',client_id=?,progress=?,total_bytes=?,downloaded_bytes=?,client_state=?,updated_at=unixepoch()`, remote.ID, remote.Progress, remote.Total, remote.Downloaded, remote.State); e != nil {
		return e
	}
	if remote.Failed {
		return problem("client_download_error", "SABnzbd reports a failed download. Repair or retry that job in SABnzbd, retain its history, then retry tracking here.")
	}
	if !remote.Complete {
		return nil
	}
	// SABnzbd's article-file API is not its extracted output. Discover the final
	// output only after completed post-processing, inside the dedicated job root.
	mapped, e := MapSource(remote.Storage, ".reelscarey-check", c.DownloadPath)
	if e != nil {
		return e
	}
	jobRoot := "ReelScarey/reelscarey-" + j.ID
	relative := strings.TrimSuffix(mapped, "/.reelscarey-check")
	savePath := remote.Storage
	single := ""
	if relative != jobRoot && path.Dir(relative) == jobRoot {
		// SABnzbd reports the file itself as storage for a single-file job.
		single = path.Base(relative)
		savePath = path.Dir(strings.ReplaceAll(remote.Storage, "\\", "/"))
	}
	if relative != jobRoot && single == "" {
		return problem("path_mapping", "SABnzbd must finish in ReelScarey/reelscarey-<job id> under its configured completed folder. Check the reelscarey category folder and disable sorting/renaming for it.")
	}
	var files []TorrentFile
	if j.clientFiles != "" {
		if remote.Storage != j.clientStorage {
			return problem("source_changed", "SABnzbd's completed folder changed after file review. Restore the original completed folder.")
		}
		if e = json.Unmarshal([]byte(j.clientFiles), &files); e != nil {
			return e
		}
	} else {
		if single != "" {
			root, err := os.OpenRoot(s.cfg.DownloadsDir)
			if err != nil {
				return err
			}
			err = segments(root, relative, false)
			var info os.FileInfo
			if err == nil {
				info, err = root.Lstat(filepath.FromSlash(relative))
			}
			root.Close()
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() || info.Size() <= 0 || !video(single) || extraVideo.MatchString(single) {
				return problem("no_movie_file", "SABnzbd's completed output is not a supported regular video file.")
			}
			files = []TorrentFile{{Index: 0, Name: single, Size: info.Size(), Progress: 1, Priority: 1}}
		} else {
			files, e = s.usenetFiles(ctx, jobRoot)
		}
		if e != nil {
			return e
		}
		b, e := json.Marshal(files)
		if e != nil {
			return e
		}
		if e = s.owned(ctx, j, `UPDATE downloads SET client_files=?,client_storage=?`, string(b), remote.Storage); e != nil {
			return e
		}
	}
	return s.processCompleted(ctx, j, savePath, files, c.DownloadPath)
}
func (s *Service) usenetFiles(ctx context.Context, relative string) ([]TorrentFile, error) {
	root, e := os.OpenRoot(s.cfg.DownloadsDir)
	if e != nil {
		return nil, e
	}
	defer root.Close()
	if _, e = safeRelative(relative); e != nil {
		return nil, e
	}
	if e = segments(root, relative, false); e != nil {
		return nil, e
	}
	pending := []string{""}
	names := []string{}
	sizes := map[string]int64{}
	count := 0
	dirs := 0
	for len(pending) > 0 {
		if e = ctx.Err(); e != nil {
			return nil, e
		}
		dir := pending[0]
		pending = pending[1:]
		dirs++
		if dirs > 500 {
			return nil, problem("too_many_files", "The completed Usenet job has too many folders to import safely.")
		}
		location := relative
		if dir != "" {
			location += "/" + dir
		}
		if e = segments(root, location, false); e != nil {
			return nil, e
		}
		f, e := root.Open(filepath.FromSlash(location))
		if e != nil {
			return nil, e
		}
		entries, e := f.ReadDir(20001)
		f.Close()
		if e != nil && !errors.Is(e, io.EOF) {
			return nil, e
		}
		if len(entries) > 20000 {
			return nil, problem("too_many_files", "A completed Usenet folder contains too many entries.")
		}
		for _, entry := range entries {
			count++
			if count > 20000 {
				return nil, problem("too_many_files", "The completed Usenet job contains too many entries.")
			}
			name := entry.Name()
			if dir != "" {
				name = dir + "/" + name
			}
			if _, e = safeRelative(name); e != nil {
				return nil, e
			}
			info, e := root.Lstat(filepath.FromSlash(relative + "/" + name))
			if e != nil {
				return nil, e
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return nil, problem("unsafe_path", "The completed Usenet job contains symbolic links. They are not imported.")
			}
			if strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			if info.IsDir() {
				if strings.Count(name, "/") >= 12 {
					return nil, problem("too_many_files", "The completed Usenet job is too deeply nested.")
				}
				pending = append(pending, name)
				continue
			}
			if info.Mode().IsRegular() && info.Size() > 0 && video(name) && !extraVideo.MatchString(name) {
				names = append(names, name)
				sizes[name] = info.Size()
			}
			if len(names) > 500 {
				return nil, problem("too_many_files", "The completed Usenet job contains more than 500 videos.")
			}
		}
	}
	sort.Strings(names)
	out := []TorrentFile{}
	for i, n := range names {
		out = append(out, TorrentFile{Index: i, Name: n, Size: sizes[n], Progress: 1, Priority: 1})
	}
	return out, nil
}
