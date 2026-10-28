package transfer

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestReinspectChecksContentAndPreservesMetadataOnChange(t *testing.T) {
	f := setupWorkflow(t)
	ctx := context.Background()
	j := f.queue(t)
	f.tick(t)
	f.complete(t, j, []byte("owned registered video"))
	f.tick(t)
	var id int64
	var relative, original string
	if e := f.db.QueryRow(`SELECT id,relative_path,probe_json FROM media_files WHERE download_id=?`, j.ID).Scan(&id, &relative, &original); e != nil {
		t.Fatal(e)
	}
	if _, e := f.db.Exec(`INSERT INTO youtube_reviews(id,video_id,video_title,channel,target_kind,media_id,target_snapshot,expires_at) VALUES('review','video','Title','Channel','movie',?,'{}',1)`, f.movie.ID); e != nil {
		t.Fatal(e)
	}
	if _, e := f.db.Exec(`INSERT INTO youtube_jobs(id,review_id,video_id,video_title,channel,target_kind,media_id,target_snapshot,state,created_at,updated_at) VALUES('youtube','review','video','Title','Channel','movie',?,'{}','completed',1,1)`, f.movie.ID); e != nil {
		t.Fatal(e)
	}
	if _, e := f.db.Exec(`INSERT INTO youtube_files(job_id,media_file_id,video_id,title,relative_path,size_bytes,sha256,probe_json,imported_at) SELECT 'youtube',id,'video','Title',relative_path,size_bytes,sha256,probe_json,1 FROM media_files WHERE id=?`, id); e != nil {
		t.Fatal(e)
	}
	f.s.importer.Prober = existingProbeFunc(func(context.Context, *os.File) (Probe, error) {
		return Probe{Codec: "h264", Audio: []AudioStream{{Codec: "aac", Language: "jpn"}}}, nil
	})
	p, e := f.s.RefreshMediaProbe(ctx, id)
	if e != nil || len(p.Audio) != 1 || p.Audio[0].Language != "jpn" {
		t.Fatal(p, e)
	}
	var saved string
	if e = f.db.QueryRow(`SELECT probe_json FROM media_files WHERE id=?`, id).Scan(&saved); e != nil {
		t.Fatal(e)
	}
	var stored Probe
	if e = json.Unmarshal([]byte(saved), &stored); e != nil || len(stored.Audio) != 1 {
		t.Fatal(saved, e)
	}
	var youtubeProbe string
	if e = f.db.QueryRow(`SELECT probe_json FROM youtube_files WHERE media_file_id=?`, id).Scan(&youtubeProbe); e != nil || youtubeProbe != saved {
		t.Fatal("YouTube provenance not updated", youtubeProbe, e)
	}
	name := filepath.Join(f.cfg.MediaDir, filepath.FromSlash(relative))
	f.s.importer.Prober = existingProbeFunc(func(context.Context, *os.File) (Probe, error) {
		if e := os.WriteFile(name, []byte("changed during probe"), 0600); e != nil {
			t.Fatal(e)
		}
		return Probe{Codec: "tampered"}, nil
	})
	if _, e = f.s.RefreshMediaProbe(ctx, id); e == nil {
		t.Fatal("changed media accepted")
	}
	var after string
	if e = f.db.QueryRow(`SELECT probe_json FROM media_files WHERE id=?`, id).Scan(&after); e != nil || after != saved {
		t.Fatal("failed inspection changed metadata", after, e)
	}
	if original == saved {
		t.Fatal("successful inspection did not update metadata")
	}
}
