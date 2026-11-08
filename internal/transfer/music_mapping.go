package transfer

import "context"

type MusicDownloadFile struct {
	Index  int    `json:"index"`
	Name   string `json:"name"`
	Artist string `json:"artist"`
	Album  string `json:"album"`
	Title  string `json:"title"`
	Track  *int   `json:"track"`
	Disc   *int   `json:"disc"`
}

// MusicDownloadFiles probes the completed folder for a blocked download so the
// user can assign each file to a catalog track.
func (s *Service) MusicDownloadFiles(ctx context.Context, id string) ([]MusicDownloadFile, error) {
	job, err := s.MusicJob(ctx, id)
	if err != nil {
		return nil, err
	}
	folder, err := s.musicCompletedFolderFor(ctx, job)
	if err != nil {
		return nil, err
	}
	importer := s.musicImporter()
	if importer == nil {
		return nil, problem("music_importer_unavailable", "Music importing is not available.")
	}
	files, err := importer.ListFolder(ctx, folder)
	if err != nil {
		return nil, err
	}
	out := make([]MusicDownloadFile, 0, len(files))
	for _, file := range files {
		out = append(out, MusicDownloadFile{Index: file.Index, Name: file.Name, Artist: file.Artist, Album: file.Album, Title: file.Title, Track: file.Track, Disc: file.Disc})
	}
	return out, nil
}

// MapMusicDownload imports a blocked download with explicit file-to-track
// assignments, then hands the job back to the normal import polling path.
func (s *Service) MapMusicDownload(ctx context.Context, id string, mapping map[int]int64) (MusicJob, error) {
	if len(mapping) == 0 {
		return MusicJob{}, problem("invalid_music_mapping", "Map every downloaded file to a catalog track.")
	}
	job, err := s.MusicJob(ctx, id)
	if err != nil {
		return MusicJob{}, err
	}
	if job.State != "blocked" && job.State != "failed" {
		return MusicJob{}, problem("invalid_music_mapping", "Only a blocked music download can be mapped.")
	}
	folder, err := s.musicCompletedFolderFor(ctx, job)
	if err != nil {
		return MusicJob{}, err
	}
	importer := s.musicImporter()
	if importer == nil {
		return MusicJob{}, problem("music_importer_unavailable", "Music importing is not available.")
	}
	importJob, err := importer.StartImportMapped(ctx, job.AlbumID, folder, job.Root, mapping)
	if err != nil {
		return MusicJob{}, err
	}
	if importJob.ID < 1 {
		return MusicJob{}, problem("music_import_missing", "The music importer did not create an import job.")
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE music_downloads SET state='importing',import_job_id=?,error='',updated_at=unixepoch() WHERE id=? AND state IN ('blocked','failed')`, importJob.ID, id); err != nil {
		return MusicJob{}, err
	}
	return s.MusicJob(ctx, id)
}

func (s *Service) musicCompletedFolderFor(ctx context.Context, job MusicJob) (string, error) {
	c, err := s.boundMusicClient(ctx, job)
	if err != nil {
		return "", err
	}
	if job.Protocol == "usenet" {
		tag := "reelscarey-" + job.ID
		var adapter interface {
			Find(context.Context, ClientConfig, string, string) (*SABJob, error)
		} = s.sab
		if c.kind == "nzbget" {
			adapter = s.nzbget
		}
		remote, err := adapter.Find(ctx, c, "", tag)
		if err != nil {
			return "", err
		}
		if remote == nil || !remote.Complete {
			return "", problem("music_not_complete", "The Usenet client has not completed this music download.")
		}
		return musicCompletedFolder(job, remote.Storage, c.DownloadPath, "ReelScarey/reelscarey-"+job.ID)
	}
	client := s.torrentClient(c)
	torrent, err := client.Find(ctx, c, job.InfoHash)
	if err != nil {
		return "", err
	}
	if torrent == nil || torrent.Progress < 1 {
		return "", problem("music_not_complete", "The torrent client has not completed this music download.")
	}
	return musicCompletedFolder(job, torrent.SavePath, c.DownloadPath, "ReelScarey/"+job.ID)
}
